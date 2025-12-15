package tapsdk

import (
	"context"
	"sync"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/lightninglabs/tap-sdk/vpsbt"
)

// InteractiveTxBuilder builds interactive Taproot Asset transfers where the
// receiver provides their keys directly (rather than a Taproot Asset address).
//
// Interactive transfers require manual proof delivery after completion.
// The returned SendResult contains the proofs that must be sent to the receiver.
type InteractiveTxBuilder struct {
	wallet *Wallet

	// Transfer parameters
	assetID          [32]byte
	amount           uint64
	receiverKeys     *entities.DerivedKeys
	lockTime         uint64
	relativeLockTime uint64
	altLeaves        map[[33]byte][][]byte

	// Internal state
	vPacketBytes []byte
	fundedPsbt   []byte
	signedPsbt   []byte

	finished bool
	mu       sync.Mutex
}

// newInteractiveTxBuilder creates a new InteractiveTxBuilder.
func newInteractiveTxBuilder(wallet *Wallet) *InteractiveTxBuilder {
	return &InteractiveTxBuilder{
		wallet: wallet,
	}
}

// SetAsset specifies which asset and how much to send.
func (b *InteractiveTxBuilder) SetAsset(assetID [32]byte, amount uint64) *InteractiveTxBuilder {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.assetID = assetID
	b.amount = amount
	return b
}

// SetReceiverKeys sets the keys derived by the receiver.
// These keys are obtained by the receiver calling Wallet.DeriveKeys().
func (b *InteractiveTxBuilder) SetReceiverKeys(keys entities.DerivedKeys) *InteractiveTxBuilder {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.receiverKeys = &keys
	return b
}

// SetLockTime sets the optional lock time for the output.
func (b *InteractiveTxBuilder) SetLockTime(lockTime uint64) *InteractiveTxBuilder {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.lockTime = lockTime
	return b
}

// SetRelativeLockTime sets the optional relative lock time for the output.
func (b *InteractiveTxBuilder) SetRelativeLockTime(relativeLockTime uint64) *InteractiveTxBuilder {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.relativeLockTime = relativeLockTime
	return b
}

// WithAltLeaves attaches auxiliary Taproot leaves that should be committed to
// the receiver's output.
func (b *InteractiveTxBuilder) WithAltLeaves(scriptKey [33]byte,
	leaves [][]byte) *InteractiveTxBuilder {

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.altLeaves == nil {
		b.altLeaves = make(map[[33]byte][][]byte)
	}

	cloned := make([][]byte, len(leaves))
	for i := range leaves {
		cloned[i] = append([]byte(nil), leaves[i]...)
	}

	b.altLeaves[scriptKey] = cloned
	return b
}

// Execute builds and sends the interactive transfer.
// Returns SendResult with proofs that must be delivered to the receiver.
func (b *InteractiveTxBuilder) Execute(ctx context.Context) (*entities.SendResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.finished {
		return nil, ErrBuilderFinished
	}

	// Validate required fields.
	if err := b.validate(); err != nil {
		return nil, err
	}

	// Build the vPacket.
	if err := b.buildVPacket(); err != nil {
		return nil, wrapErr("Execute", err)
	}

	// Fund the vPacket.
	if err := b.fund(ctx); err != nil {
		return nil, err
	}

	// Sign the funded PSBT.
	if err := b.sign(ctx); err != nil {
		return nil, err
	}

	// Anchor and complete the transfer.
	result, err := b.complete(ctx)
	if err != nil {
		return nil, err
	}

	b.finished = true
	return result, nil
}

// validate checks that all required fields are set.
func (b *InteractiveTxBuilder) validate() error {
	if b.receiverKeys == nil {
		return &Error{Op: "Execute", Err: ErrNoReceiverKeys}
	}

	var zeroID [32]byte
	if b.assetID == zeroID {
		return &Error{Op: "Execute", Err: ErrNoAssetID}
	}

	if b.amount == 0 {
		return &Error{Op: "Execute", Err: ErrZeroAmount}
	}

	return nil
}

// buildVPacket creates the virtual PSBT for the interactive send.
func (b *InteractiveTxBuilder) buildVPacket() error {
	var leaves [][]byte
	if b.altLeaves != nil && b.receiverKeys != nil {
		leaves = b.altLeaves[b.receiverKeys.ScriptKey.PubKey]
	}

	vPkt := &vpsbt.InteractiveVPacket{
		AssetID:           b.assetID,
		Amount:            b.amount,
		ScriptKey:         b.receiverKeys.ScriptKey.PubKey,
		AnchorInternalKey: b.receiverKeys.InternalKey.PubKey,
		AnchorKeyLocator:  b.receiverKeys.InternalKey.KeyLocator,
		AltLeaves:         leaves,
		LockTime:          b.lockTime,
		RelativeLockTime:  b.relativeLockTime,
		AnchorOutputIndex: 0,
		AssetVersion:      vpsbt.AssetVersionV0,
		NetworkHRP:        b.wallet.networkHRP,
		CoinType:          b.wallet.coinType,
	}

	encoded, err := vPkt.Encode()
	if err != nil {
		return err
	}

	b.vPacketBytes = encoded
	return nil
}

// fund funds the virtual PSBT.
func (b *InteractiveTxBuilder) fund(ctx context.Context) error {
	resp, err := b.wallet.WalletKit.FundInteractivePsbt(ctx, b.vPacketBytes)
	if err != nil {
		return wrapErr("Fund", err)
	}

	b.fundedPsbt = append([]byte(nil), resp.FundedPsbt...)
	return nil
}

// sign signs the funded PSBT.
func (b *InteractiveTxBuilder) sign(ctx context.Context) error {
	signedPsbt, err := b.wallet.WalletKit.SignVirtualPsbt(ctx, b.fundedPsbt)
	if err != nil {
		return wrapErr("Sign", err)
	}

	b.signedPsbt = append([]byte(nil), signedPsbt...)
	return nil
}

// complete anchors the signed PSBTs and completes the transfer.
func (b *InteractiveTxBuilder) complete(ctx context.Context) (*entities.SendResult, error) {
	result, err := b.wallet.WalletKit.AnchorVirtualPsbts(
		ctx, [][]byte{b.signedPsbt},
	)
	if err != nil {
		return nil, wrapErr("Complete", err)
	}

	return result, nil
}
