package tapsdk

import (
	"context"
	"fmt"
	"sync"

	"github.com/lightninglabs/tap-sdk/entities"
)

// TxBuilder is a builder for creating Taproot Asset transactions.
type TxBuilder struct {
	wallet *Wallet

	recipients []entities.Recipient
	inputs     []entities.AssetInput
	feeRate    uint64

	fundedPsbt   []byte
	passivePsbts [][]byte
	signedPsbt   []byte
	anchorPsbt   []byte

	finished bool
	mu       sync.Mutex
}

// NewTxBuilder creates a new TxBuilder instance.
func NewTxBuilder(wallet *Wallet) *TxBuilder {
	return &TxBuilder{
		wallet:  wallet,
		feeRate: 1,
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
func (b *TxBuilder) AddInput(input entities.AssetInput) *TxBuilder {
	b.inputs = append(b.inputs, input)
	return b
}

// SetFeeRate sets the fee rate in sat/vbyte. Default is 1 sat/vB in accordance
// with Bitcoin Core’s default relay policy.
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

// SetPassivePsbts injects externally produced passive asset PSBTs into the
// builder.
func (b *TxBuilder) SetPassivePsbts(passivePsbts [][]byte) *TxBuilder {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.passivePsbts = clone2Dimensional(passivePsbts)
	return b
}

// Fund funds the transaction and returns the funded transfer details.
func (b *TxBuilder) Fund(ctx context.Context) (*entities.FundedTransfer, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.finished {
		return nil, fmt.Errorf("builder already finished")
	}

	resp, err := b.wallet.WalletKit.FundTransfer(ctx, b.recipients, b.inputs)
	if err != nil {
		return nil, fmt.Errorf("failed to fund virtual psbt: %w", err)
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
		return nil, fmt.Errorf("builder already finished")
	}
	if len(b.fundedPsbt) == 0 {
		return nil, fmt.Errorf("transaction not funded")
	}

	signedPsbt, err := b.wallet.WalletKit.SignVirtualPsbt(ctx, b.fundedPsbt)
	if err != nil {
		return nil, fmt.Errorf("failed to sign virtual psbt: %w", err)
	}

	b.signedPsbt = append([]byte(nil), signedPsbt...)
	return b.signedPsbt, nil
}

// Commit commits the signed transaction and returns the committed transfer.
func (b *TxBuilder) Commit(ctx context.Context) (*entities.CommittedTransfer, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.finished {
		return nil, fmt.Errorf("builder already finished")
	}
	if len(b.signedPsbt) == 0 {
		return nil, fmt.Errorf("transaction not signed")
	}

	resp, err := b.wallet.WalletKit.CommitVirtualPsbts(
		ctx, [][]byte{b.signedPsbt}, b.passivePsbts, b.feeRate,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to commit virtual psbts: %w", err)
	}

	b.anchorPsbt = append([]byte(nil), resp.AnchorPsbt...)

	// CommitVirtualPsbts returns updated virtual PSBTs.
	// These are the "proofs" effectively.
	if len(resp.VirtualPsbts) > 0 {
		b.signedPsbt = append([]byte(nil), resp.VirtualPsbts[0]...)
	}

	// And passive assets might be updated too.
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
		return nil, fmt.Errorf("builder already finished")
	}
	if len(b.anchorPsbt) == 0 {
		return nil, fmt.Errorf("transaction not committed")
	}

	resp, err := b.wallet.WalletKit.PublishAndLogTransfer(
		ctx, b.anchorPsbt, [][]byte{b.signedPsbt}, b.passivePsbts,
		skipBroadcast,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to publish and log transfer: %w", err)
	}

	b.finished = true

	return resp, nil
}

// Execute executes the transaction by signing, committing, and publishing it.
// If skipBroadcast is true, the anchor transaction is not broadcast.
func (b *TxBuilder) Execute(ctx context.Context, skipBroadcast bool) (*entities.AssetPacket, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.finished {
		return nil, fmt.Errorf("builder already finished")
	}

	_, err := b.Sign(ctx)
	if err != nil {
		return nil, err
	}

	_, err = b.Commit(ctx)
	if err != nil {
		return nil, err
	}

	resp, err := b.Finish(ctx, skipBroadcast)
	if err != nil {
		return nil, err
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
