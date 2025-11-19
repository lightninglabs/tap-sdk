package tapsdk

import (
	"context"
	"fmt"
	"sync"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/lightninglabs/taproot-assets/taprpc"
	"github.com/lightninglabs/taproot-assets/taprpc/assetwalletrpc"
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

	// Map inputs to RPC inputs
	rpcInputs := make([]*assetwalletrpc.PrevId, len(b.inputs))
	for i, input := range b.inputs {
		rpcInputs[i] = &assetwalletrpc.PrevId{
			Outpoint: &taprpc.OutPoint{
				Txid:        input.OutPoint.Hash[:],
				OutputIndex: input.OutPoint.Index,
			},
			Id:        input.ID,
			ScriptKey: input.ScriptKey,
		}
	}

	// Map recipients to RPC recipients
	rpcRecipients := make([]*taprpc.AddressWithAmount, len(b.recipients))
	for i, recipient := range b.recipients {
		rpcRecipients[i] = &taprpc.AddressWithAmount{
			TapAddr: recipient.Address,
			Amount:  recipient.Amount,
		}
	}

	// Create the raw template.
	rawTemplate := &assetwalletrpc.TxTemplate{
		Recipients:           nil, // Deprecated
		Inputs:               rpcInputs,
		AddressesWithAmounts: rpcRecipients,
	}

	req := &assetwalletrpc.FundVirtualPsbtRequest{
		Template: &assetwalletrpc.FundVirtualPsbtRequest_Raw{
			Raw: rawTemplate,
		},
	}

	// Get the raw client to make the call.
	walletKit := b.wallet.WalletKit
	authCtx, _, client := walletKit.RawClientWithMacAuth(ctx)

	resp, err := client.FundVirtualPsbt(authCtx, req)
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

	req := &assetwalletrpc.SignVirtualPsbtRequest{
		FundedPsbt: b.fundedPsbt,
	}

	walletKit := b.wallet.WalletKit
	authCtx, _, client := walletKit.RawClientWithMacAuth(ctx)

	resp, err := client.SignVirtualPsbt(authCtx, req)
	if err != nil {
		return fmt.Errorf("failed to sign virtual psbt: %w", err)
	}

	b.signedPsbt = resp.SignedPsbt
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

	req := &assetwalletrpc.CommitVirtualPsbtsRequest{
		VirtualPsbts:      [][]byte{b.signedPsbt},
		PassiveAssetPsbts: b.passivePsbts,
		Fees: &assetwalletrpc.CommitVirtualPsbtsRequest_SatPerVbyte{
			SatPerVbyte: b.feeRate,
		},
		AnchorChangeOutput: &assetwalletrpc.CommitVirtualPsbtsRequest_Add{
			Add: true,
		},
	}

	walletKit := b.wallet.WalletKit
	authCtx, _, client := walletKit.RawClientWithMacAuth(ctx)

	resp, err := client.CommitVirtualPsbts(authCtx, req)
	if err != nil {
		return fmt.Errorf("failed to commit virtual psbts: %w", err)
	}

	b.anchorPsbt = resp.AnchorPsbt

	// CommitVirtualPsbts returns updated virtual PSBTs.
	// These are the "proofs" effectively.
	b.signedPsbt = resp.VirtualPsbts[0]

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

	req := &assetwalletrpc.PublishAndLogRequest{
		AnchorPsbt:            b.anchorPsbt,
		VirtualPsbts:          [][]byte{b.signedPsbt},
		PassiveAssetPsbts:     b.passivePsbts,
		SkipAnchorTxBroadcast: skipBroadcast,
	}

	walletKit := b.wallet.WalletKit
	authCtx, _, client := walletKit.RawClientWithMacAuth(ctx)

	resp, err := client.PublishAndLogTransfer(authCtx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to publish and log transfer: %w", err)
	}

	b.finished = true

	return &entities.AssetPacket{
		AnchorTransaction:        resp.Transfer.AnchorTx,
		VirtualTransactions:      [][]byte{b.signedPsbt},
		PassiveAssetTransactions: b.passivePsbts,
	}, nil
}
