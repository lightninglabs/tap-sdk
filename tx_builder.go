package tapsdk

import (
	"context"
	"fmt"
	"sync"

	"github.com/lightninglabs/tap-sdk/internal/anchor"
)

// AnchorFunder adds the caller's Bitcoin funding inputs and any Bitcoin change
// output to the anchor PSBT template before Commit, when the caller funds the
// anchor transaction instead of the backend. The callback returns the funded
// PSBT passed to CommitVirtualPsbtsWithRequest with SkipFunding set.
type AnchorFunder func(ctx context.Context, anchorTemplate []byte) ([]byte, error)

// AnchorSigner signs and finalizes the BTC anchor PSBT returned by the backend
// after Commit. The callback should return the PSBT that PublishAndLogTransfer
// can extract and broadcast (or finalize with SignAnchorInputs).
type AnchorSigner func(ctx context.Context, anchorPsbt []byte) ([]byte, error)

type txBuilderOptions struct {
	skipBroadcast bool
}

// TxBuilderOption configures TxBuilder finish/execute behavior.
type TxBuilderOption func(*txBuilderOptions)

// WithSkipBroadcast leaves the finalized anchor transaction unbroadcast.
func WithSkipBroadcast() TxBuilderOption {
	return func(o *txBuilderOptions) {
		o.skipBroadcast = true
	}
}

func applyTxBuilderOptions(opts []TxBuilderOption) *txBuilderOptions {
	o := &txBuilderOptions{}
	for _, opt := range opts {
		opt(o)
	}

	return o
}

// TxBuilder builds address-based Taproot Asset transfers.
//
// Address-based transfers use Taproot Asset addresses, which include
// proof courier information for automatic proof delivery.
//
// For interactive transfers where the receiver provides keys directly,
// use InteractiveTxBuilder instead.
type TxBuilder struct {
	walletKit WalletKitClient

	recipients []Recipient
	inputs     []PrevID
	feeRate    FeeRate

	fundedPsbts  [][]byte
	passivePsbts [][]byte
	signedPsbts  [][]byte
	anchorPsbt   []byte

	changeOutputIndex int32
	lockedUTXOs       []Outpoint

	anchorFunder     AnchorFunder
	anchorSigner     AnchorSigner
	signAnchorInputs bool
	label            string
	finished         bool
	mu               sync.Mutex
}

// newTxBuilder creates a new TxBuilder instance.
func newTxBuilder(wallet WalletKitClient) *TxBuilder {
	feeRate, _ := NewFeeRateSatPerVByte(1)

	return &TxBuilder{
		walletKit:         wallet,
		feeRate:           feeRate,
		changeOutputIndex: -1,
	}
}

// AddRecipient adds a recipient with an explicit send amount.
func (b *TxBuilder) AddRecipient(address string, amount uint64) *TxBuilder {
	b.recipients = append(
		b.recipients, RecipientWithAmount(address, amount),
	)
	return b
}

// AddTapAddress adds a recipient that uses the amount embedded in the address.
func (b *TxBuilder) AddTapAddress(address string) *TxBuilder {
	b.recipients = append(
		b.recipients, RecipientWithEmbeddedAmount(address),
	)
	return b
}

// SetRecipients sets the recipients for the transaction.
func (b *TxBuilder) SetRecipients(recipients []Recipient) *TxBuilder {
	b.recipients = recipients
	return b
}

// AddInput adds a specific input to the transaction.
func (b *TxBuilder) AddInput(input PrevID) *TxBuilder {
	b.inputs = append(b.inputs, input)
	return b
}

// SetFeeRate sets the fee rate. Default is 1 sat/vB.
func (b *TxBuilder) SetFeeRate(feeRate FeeRate) *TxBuilder {
	b.feeRate = feeRate
	return b
}

// SetFundedPsbts injects externally funded virtual PSBTs into the builder,
// one per asset issuance the transfer spends.
func (b *TxBuilder) SetFundedPsbts(fundedPsbts ...[]byte) *TxBuilder {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.fundedPsbts = clone2Dimensional(fundedPsbts)
	return b
}

// SetSignedPsbts injects externally signed virtual PSBTs into the builder,
// one per asset issuance the transfer spends.
func (b *TxBuilder) SetSignedPsbts(signedPsbts ...[]byte) *TxBuilder {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.signedPsbts = clone2Dimensional(signedPsbts)
	return b
}

// SetAnchorPsbt injects an externally finalized anchor PSBT into the builder.
func (b *TxBuilder) SetAnchorPsbt(anchorPsbt []byte) *TxBuilder {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.anchorPsbt = append([]byte(nil), anchorPsbt...)
	return b
}

// SetPassivePsbts injects externally produced passive asset PSBTs.
func (b *TxBuilder) SetPassivePsbts(passivePsbts [][]byte) *TxBuilder {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.passivePsbts = clone2Dimensional(passivePsbts)
	return b
}

// SetAnchorFunder sets the callback that funds the BTC anchor PSBT template
// before Commit, skipping backend anchor funding.
func (b *TxBuilder) SetAnchorFunder(funder AnchorFunder) *TxBuilder {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.anchorFunder = funder
	return b
}

// SetAnchorSigner sets the callback used to finalize the BTC anchor PSBT
// between Commit and Finish.
func (b *TxBuilder) SetAnchorSigner(signer AnchorSigner) *TxBuilder {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.anchorSigner = signer
	return b
}

// SetSignAnchorInputs asks the backend to sign and finalize the asset anchor
// inputs it owns during PublishAndLogTransfer.
func (b *TxBuilder) SetSignAnchorInputs(sign bool) *TxBuilder {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.signAnchorInputs = sign
	return b
}

// SetLabel sets the transfer label recorded by PublishAndLogTransfer.
func (b *TxBuilder) SetLabel(label string) *TxBuilder {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.label = label
	return b
}

// Fund funds the transaction and returns the funded transfer details.
func (b *TxBuilder) Fund(ctx context.Context) (*FundedTransfer,
	error) {

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.finished {
		return nil, ErrBuilderFinished
	}

	if len(b.recipients) == 0 {
		return nil, ErrNoRecipients
	}

	recipients, err := normaliseSendRecipients(b.recipients, true)
	if err != nil {
		return nil, wrapErr("Fund", err)
	}

	resp, err := b.walletKit.FundTransfer(ctx, recipients, b.inputs)
	if err != nil {
		return nil, wrapErr("Fund", err)
	}

	b.fundedPsbts = clone2Dimensional(resp.FundedPsbts)
	b.passivePsbts = clone2Dimensional(resp.PassiveAssetPsbts)

	return resp, nil
}

// Sign signs the funded virtual transactions and returns the signed PSBTs,
// in the order of the funded ones.
func (b *TxBuilder) Sign(ctx context.Context) ([][]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.finished {
		return nil, ErrBuilderFinished
	}
	if len(b.fundedPsbts) == 0 {
		return nil, ErrNotFunded
	}

	if err := b.sign(ctx); err != nil {
		return nil, wrapErr("Sign", err)
	}

	return clone2Dimensional(b.signedPsbts), nil
}

// sign signs every funded and passive virtual transaction. The caller holds
// the lock.
func (b *TxBuilder) sign(ctx context.Context) error {
	signedPsbts := make([][]byte, len(b.fundedPsbts))
	for idx, fundedPsbt := range b.fundedPsbts {
		signedPsbt, err := b.walletKit.SignVirtualPsbt(ctx, fundedPsbt)
		if err != nil {
			return err
		}

		signedPsbts[idx] = append([]byte(nil), signedPsbt...)
	}

	var signedPassive [][]byte
	if len(b.passivePsbts) > 0 {
		signedPassive = make([][]byte, len(b.passivePsbts))
		for idx, passivePsbt := range b.passivePsbts {
			signedPsbt, err := b.walletKit.SignVirtualPsbt(
				ctx, passivePsbt,
			)
			if err != nil {
				return err
			}

			signedPassive[idx] = append([]byte(nil), signedPsbt...)
		}
	}

	b.signedPsbts = signedPsbts
	if signedPassive != nil {
		b.passivePsbts = signedPassive
	}

	return nil
}

// Commit commits the signed transaction and returns the committed transfer.
func (b *TxBuilder) Commit(ctx context.Context) (
	*CommittedTransfer, error) {

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.finished {
		return nil, ErrBuilderFinished
	}
	if len(b.signedPsbts) == 0 {
		return nil, ErrNotSigned
	}

	resp, err := b.commitVirtualPsbts(ctx)
	if err != nil {
		return nil, wrapErr("Commit", err)
	}

	b.applyCommitResponse(resp)

	return committedTransferFromResponse(resp), nil
}

// Finish publishes the transaction and returns the finalized packet.
func (b *TxBuilder) Finish(ctx context.Context, opts ...TxBuilderOption) (
	*AssetPacket, error) {

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.finished {
		return nil, ErrBuilderFinished
	}
	if len(b.anchorPsbt) == 0 {
		return nil, ErrNotCommitted
	}

	if err := b.signAnchor(ctx); err != nil {
		return nil, wrapErr("Finish", err)
	}

	o := applyTxBuilderOptions(opts)
	resp, err := b.publishAndLog(ctx, o.skipBroadcast)
	if err != nil {
		return nil, wrapErr("Finish", err)
	}

	b.finished = true

	return resp, nil
}

// Execute funds, signs, commits, and publishes the transaction.
func (b *TxBuilder) Execute(ctx context.Context, opts ...TxBuilderOption) (
	*AssetPacket, error) {

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.finished {
		return nil, ErrBuilderFinished
	}

	// Fund if not already funded.
	if len(b.fundedPsbts) == 0 {
		if len(b.recipients) == 0 {
			return nil, ErrNoRecipients
		}

		recipients, err := normaliseSendRecipients(b.recipients, true)
		if err != nil {
			return nil, wrapErr("Fund", err)
		}

		resp, err := b.walletKit.FundTransfer(
			ctx, recipients, b.inputs,
		)
		if err != nil {
			return nil, wrapErr("Fund", err)
		}

		b.fundedPsbts = clone2Dimensional(resp.FundedPsbts)
		b.passivePsbts = clone2Dimensional(resp.PassiveAssetPsbts)
	}

	// Sign.
	if err := b.sign(ctx); err != nil {
		return nil, wrapErr("Sign", err)
	}

	// Commit.
	commitResp, err := b.commitVirtualPsbts(ctx)
	if err != nil {
		return nil, wrapErr("Commit", err)
	}

	b.applyCommitResponse(commitResp)

	if err := b.signAnchor(ctx); err != nil {
		return nil, wrapErr("Finish", err)
	}

	// Finish.
	o := applyTxBuilderOptions(opts)
	resp, err := b.publishAndLog(ctx, o.skipBroadcast)
	if err != nil {
		return nil, wrapErr("Finish", err)
	}

	b.finished = true

	return resp, nil
}

func (b *TxBuilder) publishAndLog(ctx context.Context,
	skipBroadcast bool) (*AssetPacket, error) {

	if advanced, ok := b.walletKit.(CustomAnchorWalletKitClient); ok {
		return advanced.PublishAndLogTransferWithRequest(
			ctx, &PublishAndLogTransferRequest{
				AnchorPsbt:            b.anchorPsbt,
				VirtualPsbts:          b.signedPsbts,
				PassiveAssetPsbts:     b.passivePsbts,
				ChangeOutputIndex:     b.changeOutputIndex,
				LockedUTXOs:           b.lockedUTXOs,
				SkipAnchorTxBroadcast: skipBroadcast,
				Label:                 b.label,
				SignAnchorInputs:      b.signAnchorInputs,
			},
		)
	}
	if b.signAnchorInputs {
		return nil, fmt.Errorf("signing anchor inputs requires a " +
			"CustomAnchorWalletKitClient")
	}

	return b.walletKit.PublishAndLogTransfer(
		ctx, b.anchorPsbt, b.signedPsbts, b.passivePsbts,
		skipBroadcast,
	)
}

func (b *TxBuilder) commitVirtualPsbts(ctx context.Context) (
	*CommitVirtualPsbtsResponse, error) {

	virtualPsbts := b.signedPsbts
	advanced, ok := b.walletKit.(CustomAnchorWalletKitClient)
	if !ok {
		if b.anchorFunder != nil {
			return nil, fmt.Errorf("anchor funder requires a " +
				"CustomAnchorWalletKitClient")
		}

		legacy, err := b.walletKit.CommitVirtualPsbts(
			ctx, virtualPsbts, b.passivePsbts, b.feeRate,
		)
		if err != nil {
			return nil, err
		}

		return &CommitVirtualPsbtsResponse{
			AnchorPsbt:        legacy.AnchorPsbt,
			VirtualPsbts:      legacy.VirtualPsbts,
			PassiveAssetPsbts: legacy.PassiveAssetPsbts,
			ChangeOutputIndex: -1,
		}, nil
	}

	anchorPsbt, err := anchor.PreparePsbt(virtualPsbts, b.passivePsbts)
	if err != nil {
		return nil, err
	}

	funding := AnchorFundingPlan{
		ChangeOutput: AnchorChangeOutput{
			Mode: AnchorChangeOutputAdd,
		},
		Fee: AnchorFee{
			Mode:    AnchorFeeSatPerVByte,
			FeeRate: b.feeRate,
		},
	}
	if b.anchorFunder != nil {
		anchorPsbt, err = b.anchorFunder(ctx, anchorPsbt)
		if err != nil {
			return nil, err
		}
		if len(anchorPsbt) == 0 {
			return nil, fmt.Errorf("anchor funder returned empty " +
				"anchor PSBT")
		}
		funding = AnchorFundingPlan{SkipFunding: true}
	}

	return advanced.CommitVirtualPsbtsWithRequest(
		ctx, &CommitVirtualPsbtsRequest{
			AnchorPsbt:        anchorPsbt,
			VirtualPsbts:      virtualPsbts,
			PassiveAssetPsbts: b.passivePsbts,
			Funding:           funding,
		},
	)
}

func (b *TxBuilder) applyCommitResponse(resp *CommitVirtualPsbtsResponse) {
	b.anchorPsbt = append([]byte(nil), resp.AnchorPsbt...)
	b.changeOutputIndex = resp.ChangeOutputIndex
	b.lockedUTXOs = append([]Outpoint(nil), resp.LockedUTXOs...)

	if len(resp.VirtualPsbts) > 0 {
		b.signedPsbts = clone2Dimensional(resp.VirtualPsbts)
	}
	if len(resp.PassiveAssetPsbts) > 0 {
		b.passivePsbts = clone2Dimensional(resp.PassiveAssetPsbts)
	}
}

func committedTransferFromResponse(
	resp *CommitVirtualPsbtsResponse) *CommittedTransfer {

	return &CommittedTransfer{
		AnchorPsbt:        resp.AnchorPsbt,
		VirtualPsbts:      resp.VirtualPsbts,
		PassiveAssetPsbts: resp.PassiveAssetPsbts,
	}
}

func (b *TxBuilder) signAnchor(ctx context.Context) error {
	if b.anchorSigner == nil {
		return nil
	}

	signedAnchorPsbt, err := b.anchorSigner(ctx, b.anchorPsbt)
	if err != nil {
		return err
	}

	b.anchorPsbt = append([]byte(nil), signedAnchorPsbt...)
	return nil
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
