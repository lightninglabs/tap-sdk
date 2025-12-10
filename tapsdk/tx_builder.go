package tapsdk

import (
	"context"
	"sync"

	"github.com/lightninglabs/tap-sdk/entities"
)

// TxBuilder is a builder for creating Taproot Asset transactions.
type TxBuilder struct {
	wallet *Wallet

	// For address-based (non-interactive) sends.
	recipients []entities.Recipient
	inputs     []entities.AssetInput
	feeRate    uint64

	// For interactive sends.
	interactivePsbt []byte
	isInteractive   bool

	// Internal state for both flows.
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
// with Bitcoin Core's default relay policy.
func (b *TxBuilder) SetFeeRate(satPerVByte uint64) *TxBuilder {
	b.feeRate = satPerVByte
	return b
}

// SetInteractivePsbt sets a pre-built virtual PSBT for interactive sends.
// The PSBT should be created using tappsbt.ForInteractiveSend() and serialized.
// This is used when the receiver has provided their keys directly rather than
// a Taproot Asset address.
//
// When using this method, the builder will use the interactive send flow:
// Fund → Sign → Complete.
func (b *TxBuilder) SetInteractivePsbt(psbt []byte) *TxBuilder {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.interactivePsbt = append([]byte(nil), psbt...)
	b.isInteractive = true
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
// For address-based sends, this uses the configured recipients.
// For interactive sends (when SetInteractivePsbt was called), this funds
// the pre-built virtual PSBT.
func (b *TxBuilder) Fund(ctx context.Context) (*entities.FundedTransfer, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.finished {
		return nil, ErrBuilderFinished
	}

	var resp *entities.FundedTransfer
	var err error

	if b.isInteractive && len(b.interactivePsbt) > 0 {
		// Interactive send: fund the pre-built PSBT.
		resp, err = b.wallet.WalletKit.FundInteractivePsbt(
			ctx, b.interactivePsbt,
		)
	} else {
		// Address-based send: use recipients.
		if len(b.recipients) == 0 {
			return nil, ErrNoRecipients
		}
		resp, err = b.wallet.WalletKit.FundTransfer(
			ctx, b.recipients, b.inputs,
		)
	}

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

	signedPsbt, err := b.wallet.WalletKit.SignVirtualPsbt(ctx, b.fundedPsbt)
	if err != nil {
		return nil, wrapErr("Sign", err)
	}

	b.signedPsbt = append([]byte(nil), signedPsbt...)
	return b.signedPsbt, nil
}

// Commit commits the signed transaction and returns the committed transfer.
// This is used in the non-interactive (address-based) send flow.
// For interactive sends, use Complete() instead.
func (b *TxBuilder) Commit(ctx context.Context) (*entities.CommittedTransfer, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.finished {
		return nil, ErrBuilderFinished
	}
	if len(b.signedPsbt) == 0 {
		return nil, ErrNotSigned
	}

	resp, err := b.wallet.WalletKit.CommitVirtualPsbts(
		ctx, [][]byte{b.signedPsbt}, b.passivePsbts, b.feeRate,
	)
	if err != nil {
		return nil, wrapErr("Commit", err)
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

// Complete anchors, broadcasts, and finalizes an interactive transfer.
// This combines the commit and publish steps into a single operation using
// AnchorVirtualPsbts.
//
// This method is the preferred way to finalize interactive sends as it
// handles all the complexity of anchoring the virtual PSBTs in a single call.
// The returned SendResult contains the transfer details including proofs
// that must be delivered to the receiver.
func (b *TxBuilder) Complete(ctx context.Context) (*entities.SendResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.finished {
		return nil, ErrBuilderFinished
	}
	if len(b.signedPsbt) == 0 {
		return nil, ErrNotSigned
	}

	resp, err := b.wallet.WalletKit.AnchorVirtualPsbts(
		ctx, [][]byte{b.signedPsbt},
	)
	if err != nil {
		return nil, wrapErr("Complete", err)
	}

	b.finished = true

	return resp, nil
}

// Finish publishes the transaction and returns the finalized packet.
// If skipBroadcast is true, the anchor transaction is not broadcast.
// This is used in the non-interactive (address-based) send flow after Commit().
// For interactive sends, use Complete() instead.
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

	resp, err := b.wallet.WalletKit.PublishAndLogTransfer(
		ctx, b.anchorPsbt, [][]byte{b.signedPsbt}, b.passivePsbts,
		skipBroadcast,
	)
	if err != nil {
		return nil, wrapErr("Finish", err)
	}

	b.finished = true

	return resp, nil
}

// Execute executes a non-interactive (address-based) transaction.
// It signs, commits, and publishes the transaction in sequence.
// If skipBroadcast is true, the anchor transaction is not broadcast.
//
// For interactive sends, use ExecuteInteractive() instead.
func (b *TxBuilder) Execute(ctx context.Context, skipBroadcast bool) (*entities.AssetPacket, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.finished {
		return nil, ErrBuilderFinished
	}

	_, err := b.signLocked(ctx)
	if err != nil {
		return nil, err
	}

	_, err = b.commitLocked(ctx)
	if err != nil {
		return nil, err
	}

	resp, err := b.finishLocked(ctx, skipBroadcast)
	if err != nil {
		return nil, err
	}

	b.finished = true

	return resp, nil
}

// ExecuteInteractive executes an interactive send transaction.
// It funds (if not already funded), signs, and completes the transfer using
// AnchorVirtualPsbts.
//
// The returned SendResult contains the transfer details including proofs
// that must be delivered to the receiver.
func (b *TxBuilder) ExecuteInteractive(ctx context.Context) (*entities.SendResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.finished {
		return nil, ErrBuilderFinished
	}

	// Fund if not already funded.
	if len(b.fundedPsbt) == 0 {
		_, err := b.fundLocked(ctx)
		if err != nil {
			return nil, err
		}
	}

	// Sign the funded PSBT.
	_, err := b.signLocked(ctx)
	if err != nil {
		return nil, err
	}

	// Complete using AnchorVirtualPsbts.
	resp, err := b.completeLocked(ctx)
	if err != nil {
		return nil, err
	}

	b.finished = true

	return resp, nil
}

// fundLocked is the internal implementation of Fund without locking.
func (b *TxBuilder) fundLocked(ctx context.Context) (*entities.FundedTransfer, error) {
	var resp *entities.FundedTransfer
	var err error

	if b.isInteractive && len(b.interactivePsbt) > 0 {
		resp, err = b.wallet.WalletKit.FundInteractivePsbt(
			ctx, b.interactivePsbt,
		)
	} else {
		if len(b.recipients) == 0 {
			return nil, ErrNoRecipients
		}
		resp, err = b.wallet.WalletKit.FundTransfer(
			ctx, b.recipients, b.inputs,
		)
	}

	if err != nil {
		return nil, wrapErr("Fund", err)
	}

	b.fundedPsbt = append([]byte(nil), resp.FundedPsbt...)
	b.passivePsbts = clone2Dimensional(resp.PassiveAssetPsbts)

	return resp, nil
}

// signLocked is the internal implementation of Sign without locking.
func (b *TxBuilder) signLocked(ctx context.Context) ([]byte, error) {
	if len(b.fundedPsbt) == 0 {
		return nil, ErrNotFunded
	}

	signedPsbt, err := b.wallet.WalletKit.SignVirtualPsbt(ctx, b.fundedPsbt)
	if err != nil {
		return nil, wrapErr("Sign", err)
	}

	b.signedPsbt = append([]byte(nil), signedPsbt...)
	return b.signedPsbt, nil
}

// commitLocked is the internal implementation of Commit without locking.
func (b *TxBuilder) commitLocked(ctx context.Context) (*entities.CommittedTransfer, error) {
	if len(b.signedPsbt) == 0 {
		return nil, ErrNotSigned
	}

	resp, err := b.wallet.WalletKit.CommitVirtualPsbts(
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

// finishLocked is the internal implementation of Finish without locking.
func (b *TxBuilder) finishLocked(ctx context.Context, skipBroadcast bool) (
	*entities.AssetPacket, error) {

	if len(b.anchorPsbt) == 0 {
		return nil, ErrNotCommitted
	}

	resp, err := b.wallet.WalletKit.PublishAndLogTransfer(
		ctx, b.anchorPsbt, [][]byte{b.signedPsbt}, b.passivePsbts,
		skipBroadcast,
	)
	if err != nil {
		return nil, wrapErr("Finish", err)
	}

	return resp, nil
}

// completeLocked is the internal implementation of Complete without locking.
func (b *TxBuilder) completeLocked(ctx context.Context) (*entities.SendResult, error) {
	if len(b.signedPsbt) == 0 {
		return nil, ErrNotSigned
	}

	resp, err := b.wallet.WalletKit.AnchorVirtualPsbts(
		ctx, [][]byte{b.signedPsbt},
	)
	if err != nil {
		return nil, wrapErr("Complete", err)
	}

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
