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

	networkHRP string
	coinType   uint32
}

// NewWallet creates a new Wallet instance.
func NewWallet(client Client, network entities.Network) *Wallet {
	// Get network parameters for vPacket encoding.
	networkHRP, coinType := getNetworkParams(network)

	return &Wallet{
		Client:     client,
		networkHRP: networkHRP,
		coinType:   coinType,
	}
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

// NewReceiveAddress creates a V2 address for receiving any asset from a
// group. This is the recommended way to receive assets, as it allows the
// sender to choose which specific asset and amount to send from the group.
//
// For more control (specific asset ID, custom keys, V0/V1 addresses), use
// the lower-level NewAddr method on the client directly.
func (s *Wallet) NewReceiveAddress(ctx context.Context,
	groupKey entities.PubKey) (*entities.Address, error) {

	v2 := entities.AddressVersionV2
	addr, err := s.NewAddr(ctx, &entities.NewAddressRequest{
		GroupKey:       &groupKey,
		AddressVersion: &v2,
	})
	if err != nil {
		return nil, wrapErr("NewReceiveAddress", err)
	}

	return addr, nil
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
		lastDecoded.AssetID,
		lastDecoded.GroupKey,
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
// The addr must be a valid bech32m-encoded Taproot Asset address. For
// fungible assets with V2 addresses (which omit amounts), the amount
// parameter specifies how many units to send. For V0/V1 addresses where
// the amount is already embedded, pass 0 and the address amount is used.
//
// For multi-recipient sends in a single anchor transaction, use SendMulti.
// For fine-grained control over the Fund → Sign → Commit → Publish
// pipeline, use NewTxBuilder.
func (s *Wallet) Send(ctx context.Context, addr string,
	amount uint64, opts ...SendOption) (*entities.AssetTransfer, error) {

	o := applySendOptions(opts)

	req := &entities.SendAssetRequest{
		FeeRate:                   o.feeRate,
		Label:                     o.label,
		SkipProofCourierPingCheck: o.skipProofCourierPingCheck,
	}

	if amount > 0 {
		req.Recipients = []entities.Recipient{{
			Address: addr,
			Amount:  amount,
		}}
	} else {
		req.TapAddresses = []string{addr}
	}

	transfer, err := s.SendAsset(ctx, req)
	if err != nil {
		return nil, wrapErr("Send", err)
	}

	return transfer, nil
}

// SendMulti sends to multiple recipients in a single anchor transaction.
//
// Each recipient must include a valid Taproot Asset address and the amount
// to send. This is required for V2 addresses that omit amounts. For V0/V1
// addresses, set Amount to 0 to use the address-embedded amount.
//
// For single-recipient sends, prefer Send for simplicity.
func (s *Wallet) SendMulti(ctx context.Context,
	recipients []entities.Recipient,
	opts ...SendOption) (*entities.AssetTransfer, error) {

	if len(recipients) == 0 {
		return nil, wrapErr("SendMulti", ErrNoRecipients)
	}

	o := applySendOptions(opts)

	// Split recipients into address-only (amount=0, V0/V1 embedded) and
	// address-with-amount (V2 explicit) groups.
	var (
		addrOnly    []string
		withAmounts []entities.Recipient
	)
	for _, r := range recipients {
		if r.Amount > 0 {
			withAmounts = append(withAmounts, r)
		} else {
			addrOnly = append(addrOnly, r.Address)
		}
	}

	// If all recipients use embedded amounts, use the simple TapAddresses
	// field. Otherwise, require all to have explicit amounts since tapd
	// does not mix both modes.
	req := &entities.SendAssetRequest{
		FeeRate:                   o.feeRate,
		Label:                     o.label,
		SkipProofCourierPingCheck: o.skipProofCourierPingCheck,
	}
	if len(withAmounts) == 0 {
		req.TapAddresses = addrOnly
	} else {
		req.Recipients = recipients
	}

	transfer, err := s.SendAsset(ctx, req)
	if err != nil {
		return nil, wrapErr("SendMulti", err)
	}

	return transfer, nil
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
