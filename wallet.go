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

// NewReceiveAddress creates a V2 address for receiving assets identified by
// the given AssetRef.
//
// For fungible assets (AssetRef from group key), the address accepts any
// tranche within the group and lets the sender choose the amount. For
// collectibles (AssetRef from asset ID), the address targets the specific
// asset.
//
// For more control (custom keys, V0/V1 addresses, explicit amounts), use
// the lower-level NewAddr method on the client directly.
func (s *Wallet) NewReceiveAddress(ctx context.Context,
	ref entities.AssetRef) (*entities.Address, error) {

	v2 := entities.AddressVersionV2
	req := &entities.NewAddressRequest{
		AddressVersion: &v2,
	}

	if groupKey, ok := ref.GroupKey(); ok {
		req.GroupKey = &groupKey
	} else if assetID, ok := ref.AssetID(); ok {
		req.AssetID = &assetID
	}

	addr, err := s.NewAddr(ctx, req)
	if err != nil {
		return nil, wrapErr("NewReceiveAddress", err)
	}

	return addr, nil
}

// GetBalance returns the confirmed balance for the asset identified by ref.
//
// For fungible assets (group key), it returns the aggregate balance across
// all tranches in the group. For collectibles (asset ID), it returns the
// balance of the specific asset.
func (s *Wallet) GetBalance(ctx context.Context,
	ref entities.AssetRef) (uint64, error) {

	if groupKey, ok := ref.GroupKey(); ok {
		resp, err := s.ListBalances(ctx, &entities.ListBalancesRequest{
			GroupBy:        entities.BalanceGroupByGroupKey,
			GroupKeyFilter: &groupKey,
		})
		if err != nil {
			return 0, wrapErr("GetBalance", err)
		}

		for _, gb := range resp.AssetGroupBalances {
			return gb.Balance, nil
		}

		return 0, nil
	}

	assetID, _ := ref.AssetID()
	resp, err := s.ListBalances(ctx, &entities.ListBalancesRequest{
		GroupBy:     entities.BalanceGroupByAssetID,
		AssetFilter: &assetID,
	})
	if err != nil {
		return 0, wrapErr("GetBalance", err)
	}

	for _, ab := range resp.AssetBalances {
		return ab.Balance, nil
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

// ListAssetsByRef returns wallet assets matching the given AssetRef.
//
// For fungible assets (group key), it filters by group key and returns
// all UTXOs in the group. For collectibles (asset ID), it returns
// assets with that specific asset ID.
func (s *Wallet) ListAssetsByRef(ctx context.Context,
	ref entities.AssetRef) ([]*entities.Asset, error) {

	req := &entities.ListAssetsRequest{}

	if gk, ok := ref.GroupKey(); ok {
		req.GroupKey = &gk
	}

	assets, err := s.ListAssets(ctx, req)
	if err != nil {
		return nil, wrapErr("ListAssetsByRef", err)
	}

	// For collectible refs, the low-level ListAssets doesn't
	// filter by asset ID natively, so we filter client-side.
	if aid, ok := ref.AssetID(); ok {
		var filtered []*entities.Asset
		for _, a := range assets {
			if a.Genesis.AssetID == aid {
				filtered = append(filtered, a)
			}
		}

		return filtered, nil
	}

	return assets, nil
}

// Burn destroys asset units identified by the given AssetRef. Only
// collectible (asset-ID) references are supported because the tapd
// burn RPC requires a specific asset ID.
//
// The confirmation text "assets will be destroyed" is set
// automatically.
func (s *Wallet) Burn(ctx context.Context,
	ref entities.AssetRef, amount uint64,
	note string) (*entities.BurnAssetResponse, error) {

	assetID, ok := ref.AssetID()
	if !ok {
		return nil, wrapErr("Burn", ErrGroupKeyNotSupported)
	}

	resp, err := s.BurnAsset(ctx, &entities.BurnAssetRequest{
		AssetID:          &assetID,
		AmountToBurn:     amount,
		ConfirmationText: "assets will be destroyed",
		Note:             note,
	})
	if err != nil {
		return nil, wrapErr("Burn", err)
	}

	return resp, nil
}

// ListBurnsByRef returns burn events for the asset identified by ref.
//
// For fungible assets (group key), it filters by tweaked group key.
// For collectibles (asset ID), it filters by asset ID.
func (s *Wallet) ListBurnsByRef(ctx context.Context,
	ref entities.AssetRef) ([]*entities.AssetBurn, error) {

	req := &entities.ListBurnsRequest{}

	if gk, ok := ref.GroupKey(); ok {
		req.TweakedGroupKey = &gk
	} else if aid, ok := ref.AssetID(); ok {
		req.AssetID = &aid
	}

	burns, err := s.ListBurns(ctx, req)
	if err != nil {
		return nil, wrapErr("ListBurnsByRef", err)
	}

	return burns, nil
}

// FetchMetaByRef fetches the metadata for the asset identified by
// ref. Only collectible (asset-ID) references are supported because
// the tapd metadata RPC requires a specific asset ID.
func (s *Wallet) FetchMetaByRef(ctx context.Context,
	ref entities.AssetRef) (*entities.AssetMeta, error) {

	assetID, ok := ref.AssetID()
	if !ok {
		return nil, wrapErr(
			"FetchMetaByRef", ErrGroupKeyNotSupported,
		)
	}

	meta, err := s.FetchAssetMeta(ctx, &entities.FetchAssetMetaRequest{
		AssetID: &assetID,
	})
	if err != nil {
		return nil, wrapErr("FetchMetaByRef", err)
	}

	return meta, nil
}

// Send performs a simple one-shot address-based asset transfer.
//
// The addr must be a valid bech32m-encoded Taproot Asset address. For
// fungible assets with V2 addresses, which omit amounts, the amount
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
