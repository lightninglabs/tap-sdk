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
		wallet: wallet,
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

// SetFeeRate sets the fee rate in sat/vbyte.
func (b *TxBuilder) SetFeeRate(satPerVByte uint64) *TxBuilder {
	b.feeRate = satPerVByte
	return b
}

// Fund funds the transaction.
func (b *TxBuilder) Fund(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.finished {
		return fmt.Errorf("builder already finished")
	}

	resp, err := b.wallet.WalletKit.FundTransfer(ctx, b.recipients, b.inputs)
	if err != nil {
		return fmt.Errorf("failed to fund virtual psbt: %w", err)
	}

	b.fundedPsbt = resp.FundedPsbt
	b.passivePsbts = resp.PassiveAssetPsbts

	return nil
}

// Sign signs the funded transaction.
func (b *TxBuilder) Sign(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.finished {
		return fmt.Errorf("builder already finished")
	}
	if len(b.fundedPsbt) == 0 {
		return fmt.Errorf("transaction not funded")
	}

	signedPsbt, err := b.wallet.WalletKit.SignVirtualPsbt(ctx, b.fundedPsbt)
	if err != nil {
		return fmt.Errorf("failed to sign virtual psbt: %w", err)
	}

	b.signedPsbt = signedPsbt
	return nil
}

// Commit commits the signed transaction.
func (b *TxBuilder) Commit(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.finished {
		return fmt.Errorf("builder already finished")
	}
	if len(b.signedPsbt) == 0 {
		return fmt.Errorf("transaction not signed")
	}

	resp, err := b.wallet.WalletKit.CommitVirtualPsbts(
		ctx, [][]byte{b.signedPsbt}, b.passivePsbts, b.feeRate,
	)
	if err != nil {
		return fmt.Errorf("failed to commit virtual psbts: %w", err)
	}

	b.anchorPsbt = resp.AnchorPsbt

	// CommitVirtualPsbts returns updated virtual PSBTs.
	// These are the "proofs" effectively.
	if len(resp.VirtualPsbts) > 0 {
		b.signedPsbt = resp.VirtualPsbts[0]
	}

	// And passive assets might be updated too.
	if len(resp.PassiveAssetPsbts) > 0 {
		b.passivePsbts = resp.PassiveAssetPsbts
	}

	return nil
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

	packet, err := b.wallet.WalletKit.PublishAndLogTransfer(
		ctx, b.anchorPsbt, [][]byte{b.signedPsbt}, b.passivePsbts,
		skipBroadcast,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to publish and log transfer: %w", err)
	}

	b.finished = true

	return packet, nil
}
