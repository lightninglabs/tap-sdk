package tapsdk

import (
	"context"
	"sync"

	"github.com/lightninglabs/tap-sdk/entities"
)

// TxBuilder builds address-based Taproot Asset transfers.
//
// Address-based transfers use Taproot Asset addresses, which include
// proof courier information for automatic proof delivery.
//
// For interactive transfers where the receiver provides keys directly,
// use InteractiveTxBuilder instead.
type TxBuilder struct {
	walletKit WalletKitClient

	recipients []entities.Recipient
	inputs     []entities.PrevID
	feeRate    uint64

	fundedPsbt   []byte
	passivePsbts [][]byte
	signedPsbt   []byte
	anchorPsbt   []byte

	finished bool
	mu       sync.Mutex
}

// newTxBuilder creates a new TxBuilder instance.
func newTxBuilder(wallet WalletKitClient) *TxBuilder {
	return &TxBuilder{
		walletKit: wallet,
		feeRate:   1,
	}
}

// AddRecipient adds a recipient to the transaction.
func (b *TxBuilder) AddRecipient(address string, amount uint64) *TxBuilder {
	b.recipients = append(b.recipients, entities.Recipient{
		Address: address,
		Amount:  amount,
	})
	return b
}

// SetRecipients sets the recipients for the transaction.
func (b *TxBuilder) SetRecipients(recipients []entities.Recipient) *TxBuilder {
	b.recipients = recipients
	return b
}

// AddInput adds a specific input to the transaction.
func (b *TxBuilder) AddInput(input entities.PrevID) *TxBuilder {
	b.inputs = append(b.inputs, input)
	return b
}

// SetFeeRate sets the fee rate in sat/vbyte. Default is 1 sat/vB.
func (b *TxBuilder) SetFeeRate(satPerVByte uint64) *TxBuilder {
	b.feeRate = satPerVByte
	return b
}

// SetFundedPsbt injects an externally funded PSBT into the builder.
func (b *TxBuilder) SetFundedPsbt(fundedPsbt []byte) *TxBuilder {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.fundedPsbt = append([]byte(nil), fundedPsbt...)
	return b
}

// SetSignedPsbt injects an externally signed PSBT into the builder.
func (b *TxBuilder) SetSignedPsbt(signedPsbt []byte) *TxBuilder {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.signedPsbt = append([]byte(nil), signedPsbt...)
	return b
}

// SetPassivePsbts injects externally produced passive asset PSBTs.
func (b *TxBuilder) SetPassivePsbts(passivePsbts [][]byte) *TxBuilder {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.passivePsbts = clone2Dimensional(passivePsbts)
	return b
}

// Fund funds the transaction and returns the funded transfer details.
func (b *TxBuilder) Fund(ctx context.Context) (*entities.FundedTransfer,
	error) {

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.finished {
		return nil, ErrBuilderFinished
	}

	if len(b.recipients) == 0 {
		return nil, ErrNoRecipients
	}

	resp, err := b.walletKit.FundTransfer(ctx, b.recipients, b.inputs)
	if err != nil {
		return nil, wrapErr("Fund", err)
	}

	b.fundedPsbt = append([]byte(nil), resp.FundedPsbt...)
	b.passivePsbts = clone2Dimensional(resp.PassiveAssetPsbts)

	return resp, nil
}

// Sign signs the funded transaction and returns the signed PSBT.
func (b *TxBuilder) Sign(ctx context.Context) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.finished {
		return nil, ErrBuilderFinished
	}
	if len(b.fundedPsbt) == 0 {
		return nil, ErrNotFunded
	}

	signedPsbt, err := b.walletKit.SignVirtualPsbt(ctx, b.fundedPsbt)
	if err != nil {
		return nil, wrapErr("Sign", err)
	}

	b.signedPsbt = append([]byte(nil), signedPsbt...)
	return b.signedPsbt, nil
}

// Commit commits the signed transaction and returns the committed transfer.
func (b *TxBuilder) Commit(ctx context.Context) (
	*entities.CommittedTransfer, error) {

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.finished {
		return nil, ErrBuilderFinished
	}
	if len(b.signedPsbt) == 0 {
		return nil, ErrNotSigned
	}

	resp, err := b.walletKit.CommitVirtualPsbts(
		ctx, [][]byte{b.signedPsbt}, b.passivePsbts, b.feeRate,
	)
	if err != nil {
		return nil, wrapErr("Commit", err)
	}

	b.anchorPsbt = append([]byte(nil), resp.AnchorPsbt...)

	if len(resp.VirtualPsbts) > 0 {
		b.signedPsbt = append([]byte(nil), resp.VirtualPsbts[0]...)
	}

	if len(resp.PassiveAssetPsbts) > 0 {
		b.passivePsbts = clone2Dimensional(resp.PassiveAssetPsbts)
	}

	return resp, nil
}

// Finish publishes the transaction and returns the finalized packet.
// If skipBroadcast is true, the anchor transaction is not broadcast.
func (b *TxBuilder) Finish(ctx context.Context, skipBroadcast bool) (
	*entities.AssetPacket, error) {

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.finished {
		return nil, ErrBuilderFinished
	}
	if len(b.anchorPsbt) == 0 {
		return nil, ErrNotCommitted
	}

	resp, err := b.walletKit.PublishAndLogTransfer(
		ctx, b.anchorPsbt, [][]byte{b.signedPsbt}, b.passivePsbts,
		skipBroadcast,
	)
	if err != nil {
		return nil, wrapErr("Finish", err)
	}

	b.finished = true

	return resp, nil
}

// Execute funds, signs, commits, and publishes the transaction.
// If skipBroadcast is true, the anchor transaction is not broadcast.
func (b *TxBuilder) Execute(ctx context.Context, skipBroadcast bool) (
	*entities.AssetPacket, error) {

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.finished {
		return nil, ErrBuilderFinished
	}

	// Fund if not already funded.
	if len(b.fundedPsbt) == 0 {
		if len(b.recipients) == 0 {
			return nil, ErrNoRecipients
		}

		resp, err := b.walletKit.FundTransfer(
			ctx, b.recipients, b.inputs,
		)
		if err != nil {
			return nil, wrapErr("Fund", err)
		}

		b.fundedPsbt = append([]byte(nil), resp.FundedPsbt...)
		b.passivePsbts = clone2Dimensional(resp.PassiveAssetPsbts)
	}

	// Sign.
	signedPsbt, err := b.walletKit.SignVirtualPsbt(ctx, b.fundedPsbt)
	if err != nil {
		return nil, wrapErr("Sign", err)
	}
	b.signedPsbt = append([]byte(nil), signedPsbt...)

	// Commit.
	commitResp, err := b.walletKit.CommitVirtualPsbts(
		ctx, [][]byte{b.signedPsbt}, b.passivePsbts, b.feeRate,
	)
	if err != nil {
		return nil, wrapErr("Commit", err)
	}

	b.anchorPsbt = append([]byte(nil), commitResp.AnchorPsbt...)
	if len(commitResp.VirtualPsbts) > 0 {
		b.signedPsbt = append([]byte(nil), commitResp.VirtualPsbts[0]...)
	}
	if len(commitResp.PassiveAssetPsbts) > 0 {
		b.passivePsbts = clone2Dimensional(commitResp.PassiveAssetPsbts)
	}

	// Finish.
	resp, err := b.walletKit.PublishAndLogTransfer(
		ctx, b.anchorPsbt, [][]byte{b.signedPsbt}, b.passivePsbts,
		skipBroadcast,
	)
	if err != nil {
		return nil, wrapErr("Finish", err)
	}

	b.finished = true

	return resp, nil
}

// clone2Dimensional performs a deep copy of a slice of byte slices.
func clone2Dimensional(src [][]byte) [][]byte {
	if src == nil {
		return nil
	}

	clone := make([][]byte, len(src))
	for i := range src {
		clone[i] = append([]byte(nil), src[i]...)
	}

	return clone
}
