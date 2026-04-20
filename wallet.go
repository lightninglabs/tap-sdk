package tapsdk

import (
	"context"
	"fmt"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/lightninglabs/tap-sdk/vpsbt"
)

// Wallet constitutes the high level service giving access to
// Taproot Assets features.
type Wallet struct {
	Client

	networkHRP              string
	coinType                uint32
	defaultProofCourierAddr string
}

// WalletOption configures optional Wallet behavior.
type WalletOption func(*Wallet)

// WithDefaultProofCourierAddr sets the default proof courier address used by
// high-level V2 receive address helpers.
func WithDefaultProofCourierAddr(addr string) WalletOption {
	return func(w *Wallet) {
		w.defaultProofCourierAddr = addr
	}
}

// NewWallet creates a new Wallet instance.
func NewWallet(client Client, network entities.Network,
	opts ...WalletOption) *Wallet {
	// Get network parameters for vPacket encoding.
	networkHRP, coinType := getNetworkParams(network)

	wallet := &Wallet{
		Client:     client,
		networkHRP: networkHRP,
		coinType:   coinType,
	}

	for _, opt := range opts {
		opt(wallet)
	}

	return wallet
}

// NewTxBuilder returns a new transaction builder for address-based transfers.
func (s *Wallet) NewTxBuilder() *TxBuilder {
	return newTxBuilder(s)
}

// NewInteractiveTxBuilder returns a new builder for interactive transfers
// where the receiver provides their keys directly.
func (s *Wallet) NewInteractiveTxBuilder() *InteractiveTxBuilder {
	return newInteractiveTxBuilder(s, s.networkHRP, s.coinType)
}

// NewReceiveAddress creates a V2 address for receiving the given
// asset. The sender chooses the specific units and amount to send.
//
// For more control (custom keys, V0/V1 addresses, explicit amounts),
// use the lower-level NewAddr method on the client directly. If your
// tapd does not expose a suitable default proof courier for V2
// addresses, configure the wallet with WithDefaultProofCourierAddr.
func (s *Wallet) NewReceiveAddress(ctx context.Context,
	ref entities.AssetRef) (*entities.Address, error) {

	v2 := entities.AddressVersionV2
	req := &entities.NewAddressRequest{
		AssetRef:         ref,
		ProofCourierAddr: s.defaultProofCourierAddr,
		AddressVersion:   &v2,
	}

	addr, err := s.NewAddr(ctx, req)
	if err != nil {
		return nil, wrapErr("NewReceiveAddress", err)
	}

	return addr, nil
}

// GetBalance returns the confirmed balance for the given asset. If the
// wallet holds units across multiple issuances, the total is aggregated
// into a single sum.
func (s *Wallet) GetBalance(ctx context.Context,
	ref entities.AssetRef) (uint64, error) {

	resp, err := s.ListBalances(ctx, &entities.ListBalancesRequest{
		AssetRef: &ref,
	})
	if err != nil {
		return 0, wrapErr("GetBalance", err)
	}

	if balance, ok := resp.Balances[ref.String()]; ok {
		return balance.Balance, nil
	}

	return 0, nil
}

// DeriveKeys derives a new script key and internal key for receiving assets.
// The receiver calls this method and shares the result with the sender for
// interactive transfers.
//
// This is a convenience method that combines DeriveScriptKey and
// DeriveInternalKey into a single call.
func (s *Wallet) DeriveKeys(ctx context.Context) (*entities.DerivedKeys,
	error) {

	scriptKey, err := s.DeriveScriptKey(ctx)
	if err != nil {
		return nil, wrapErr("DeriveKeys", err)
	}

	internalKey, err := s.DeriveInternalKey(ctx)
	if err != nil {
		return nil, wrapErr("DeriveKeys", err)
	}

	return &entities.DerivedKeys{
		ScriptKey:   *scriptKey,
		InternalKey: *internalKey,
	}, nil
}

// ImportProof imports a proof file received from a sender during an
// interactive transfer. This method handles the full import flow:
// 1. Unpacks the proof file into individual proofs
// 2. Inserts each proof into the local universe
// 3. Registers the transfer so the wallet recognizes the new asset
//
// Returns the registered asset details.
func (s *Wallet) ImportProof(ctx context.Context,
	proofFile *entities.ProofFile) (*entities.RegisteredAsset, error) {

	// Step 1: Unpack the proof file into individual proofs.
	rawProofs, err := s.UnpackProofFile(ctx, proofFile.RawProofFile)
	if err != nil {
		return nil, wrapErr("ImportProof", err)
	}

	if len(rawProofs) == 0 {
		return nil, wrapErr("ImportProof",
			fmt.Errorf("proof file contains no proofs"))
	}

	// Step 2: Decode and insert each proof into the universe.
	var lastDecoded *entities.DecodedProof
	for _, rawProof := range rawProofs {
		// TODO: Decode the proof locally without using the RPC client.
		decoded, err := s.DecodeProof(ctx, rawProof)
		if err != nil {
			return nil, wrapErr("ImportProof", err)
		}

		err = s.InsertProof(ctx, rawProof, decoded)
		if err != nil {
			return nil, wrapErr("ImportProof", err)
		}

		lastDecoded = decoded
	}

	// Step 3: Register the transfer using the last proof's details.
	registered, err := s.RegisterTransfer(
		ctx,
		lastDecoded.AssetRef,
		lastDecoded.ScriptKey,
		lastDecoded.Outpoint,
	)
	if err != nil {
		return nil, wrapErr("ImportProof", err)
	}

	return registered, nil
}

// Send performs a simple one-shot address-based asset transfer.
//
// The addr must be a valid bech32m-encoded Taproot Asset address. The
// SDK decodes it up-front to decide how to frame the request:
//
//   - If the address embeds an amount (all V0/V1 addresses and V2
//     addresses that bake one in), the caller may pass 0 or the exact
//     matching amount. Any other value returns ErrAmountMismatch.
//   - If the address embeds no amount (only possible for V2
//     addresses), the caller MUST pass amount > 0. Otherwise Send
//     returns ErrAmountRequired.
//
// For multi-recipient sends in a single anchor transaction, use
// SendMulti. For fine-grained control over the Fund → Sign → Commit →
// Publish pipeline, use NewTxBuilder.
func (s *Wallet) Send(ctx context.Context, addr string,
	amount uint64, opts ...SendOption) (*entities.AssetTransfer, error) {

	decoded, err := s.DecodeAddr(ctx, addr)
	if err != nil {
		return nil, wrapErr("Send", err)
	}

	if err := validateSendAmount(decoded, amount); err != nil {
		return nil, wrapErr("Send", err)
	}

	o := applySendOptions(opts)
	req := &entities.SendAssetRequest{
		FeeRate:                   o.feeRate,
		Label:                     o.label,
		SkipProofCourierPingCheck: o.skipProofCourierPingCheck,
	}

	if decoded.Amount > 0 {
		// Address embeds the authoritative amount; tapd uses it.
		req.TapAddresses = []string{addr}
	} else {
		// V2 address without an embedded amount; caller provided one.
		req.Recipients = []entities.Recipient{{
			Address: addr,
			Amount:  amount,
		}}
	}

	transfer, err := s.SendAsset(ctx, req)
	if err != nil {
		return nil, wrapErr("Send", err)
	}

	return transfer, nil
}

// validateSendAmount enforces the amount vs. address-embedded-amount
// invariant used by Send and SendMulti. The caller passes the decoded
// destination address and the amount argument they intend to send.
func validateSendAmount(addr *entities.Address, amount uint64) error {
	switch {
	case addr.Amount == 0 && amount == 0:
		return ErrAmountRequired

	case addr.Amount > 0 && amount > 0 && amount != addr.Amount:
		return fmt.Errorf(
			"%w: address embeds %d, caller passed %d",
			ErrAmountMismatch, addr.Amount, amount,
		)
	}

	return nil
}

// SendMulti sends to multiple recipients in a single anchor
// transaction. Each recipient's amount is validated against the
// embedded amount on the decoded address, following the same rules as
// Send: embedded-amount addresses accept amount 0 or a matching value,
// while V2 addresses without an embedded amount require an explicit
// non-zero amount.
//
// For single-recipient sends, prefer Send for simplicity.
func (s *Wallet) SendMulti(ctx context.Context,
	recipients []entities.Recipient,
	opts ...SendOption) (*entities.AssetTransfer, error) {

	if len(recipients) == 0 {
		return nil, wrapErr("SendMulti", ErrNoRecipients)
	}

	// Decode once per recipient so we can validate amounts and pick the
	// right request mode without assuming the caller knows each
	// address's version.
	decoded := make([]*entities.Address, len(recipients))
	for i, r := range recipients {
		addr, err := s.DecodeAddr(ctx, r.Address)
		if err != nil {
			return nil, wrapErr("SendMulti", err)
		}

		if err := validateSendAmount(addr, r.Amount); err != nil {
			return nil, wrapErr("SendMulti", err)
		}

		decoded[i] = addr
	}

	o := applySendOptions(opts)
	req := &entities.SendAssetRequest{
		FeeRate:                   o.feeRate,
		Label:                     o.label,
		SkipProofCourierPingCheck: o.skipProofCourierPingCheck,
	}

	// tapd does not accept a mix of TapAddresses and Recipients in a
	// single call. If every recipient has an embedded amount we can use
	// the simpler TapAddresses path; otherwise every recipient must go
	// through Recipients (and the ones with embedded amounts echo the
	// embedded value into the explicit amount field).
	allEmbedded := true
	for _, addr := range decoded {
		if addr.Amount == 0 {
			allEmbedded = false
			break
		}
	}

	if allEmbedded {
		req.TapAddresses = make([]string, len(recipients))
		for i, r := range recipients {
			req.TapAddresses[i] = r.Address
		}
	} else {
		req.Recipients = make([]entities.Recipient, len(recipients))
		for i, r := range recipients {
			req.Recipients[i] = entities.Recipient{
				Address: r.Address,
				Amount:  recipientAmount(decoded[i], r),
			}
		}
	}

	transfer, err := s.SendAsset(ctx, req)
	if err != nil {
		return nil, wrapErr("SendMulti", err)
	}

	return transfer, nil
}

// recipientAmount returns the amount tapd needs in the
// AddressesWithAmounts path: the caller's explicit amount if set,
// otherwise the amount embedded in the address (which
// validateSendAmount already confirmed is consistent).
func recipientAmount(addr *entities.Address, r entities.Recipient) uint64 {
	if r.Amount > 0 {
		return r.Amount
	}

	return addr.Amount
}

// Close tears down the underlying client connection if it exists.
func (s *Wallet) Close() error {
	return s.Client.Close()
}

// getNetworkParams returns the HRP and coin type for a given network.
func getNetworkParams(network entities.Network) (string, uint32) {
	switch network {
	case entities.NetworkMainnet:
		return vpsbt.MainnetHRP, 0 // BIP-44 coin type 0 for mainnet
	case entities.NetworkTestnet:
		return vpsbt.TestnetHRP, 1 // BIP-44 coin type 1 for testnet
	case entities.NetworkTestnet4:
		return vpsbt.Testnet4HRP, 1
	case entities.NetworkSignet:
		return vpsbt.SigNetHRP, 1
	case entities.NetworkSimnet:
		return vpsbt.SimNetHRP, 1
	case entities.NetworkRegtest:
		return vpsbt.RegTestHRP, 1
	default:
		return vpsbt.RegTestHRP, 1 // Default to regtest
	}
}
