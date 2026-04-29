package tapsdk

import (
	"context"
	"fmt"
	"strings"

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

const authMailboxUniverseCourierScheme = "authmailbox+universerpc://"

type transferRegistrarWithIssuance interface {
	RegisterTransferWithIssuance(ctx context.Context,
		assetRef entities.AssetRef, issuanceID entities.AssetID,
		scriptKey entities.PubKey, outpoint entities.Outpoint) (
		*entities.RegisteredAsset, error)
}

// WithDefaultProofCourierAddr sets the default proof courier address used by
// high-level V2 receive address helpers.
func WithDefaultProofCourierAddr(addr string) WalletOption {
	return func(w *Wallet) {
		w.defaultProofCourierAddr = addr
	}
}

// WithAuthMailboxCourier configures the default proof courier for high-level
// V2 receive address helpers using the auth mailbox transport. The host should
// include the port, for example "tapd.example:10029".
func WithAuthMailboxCourier(host string) WalletOption {
	return WithDefaultProofCourierAddr(
		authMailboxUniverseCourierScheme + host,
	)
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
// Collectible/NFT addresses always receive exactly one unit; the SDK
// automatically supplies that amount when tapd rejects the default
// sender-chosen amount shape.
//
// For more control (custom keys, V0/V1 addresses, explicit amounts),
// use the lower-level NewAddr method on the client directly. If your
// tapd does not expose a suitable default proof courier for V2
// addresses, configure the wallet with WithAuthMailboxCourier or
// WithDefaultProofCourierAddr.
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
		if shouldRetryCollectibleAmount(ref, err) {
			req.Amount = 1

			addr, retryErr := s.NewAddr(ctx, req)
			if retryErr == nil {
				return addr, nil
			}

			err = retryErr
		}

		if shouldRetryExactGroupRef(ref, err) {
			exactRef := s.resolveExactGroupRef(ctx, ref)
			if exactRef != ref {
				req.AssetRef = exactRef

				addr, retryErr := s.NewAddr(ctx, req)
				if retryErr == nil {
					return addr, nil
				}
			}
		}

		return nil, wrapErr("NewReceiveAddress", err)
	}

	return addr, nil
}

func shouldRetryCollectibleAmount(ref entities.AssetRef, err error) bool {
	if !ref.IsAssetIDRef() || err == nil {
		return false
	}

	return strings.Contains(err.Error(), "collectible asset amount not one")
}

func shouldRetryExactGroupRef(ref entities.AssetRef, err error) bool {
	if !ref.IsGroupRef() || err == nil {
		return false
	}

	errMsg := err.Error()
	return strings.Contains(errMsg, "unable to find asset or group") ||
		strings.Contains(errMsg, "asset lookup failed")
}

func (s *Wallet) resolveExactGroupRef(ctx context.Context,
	ref entities.AssetRef) entities.AssetRef {

	if !ref.IsGroupRef() {
		return ref
	}

	groups, err := s.ListGroups(ctx)
	if err != nil {
		return ref
	}

	for _, group := range groups {
		if group.AssetRef.Equivalent(ref) {
			return group.AssetRef
		}
	}

	return ref
}

// GetBalance returns the confirmed balance for the given asset. If the
// wallet holds units across multiple issuances, the total is aggregated
// into a single sum.
//
// If the wallet has no record of the asset ref, GetBalance returns an
// error wrapping ErrAssetUnknown; detect it with errors.Is. "Known"
// means the local universe has an issuance or transfer root for the
// ref, which tapd populates for assets the wallet minted, received,
// or bootstrapped via SyncUniverse. A known asset with zero confirmed
// units returns (0, nil).
//
// The universe probe fires only on the cold path where ListBalances
// returns an empty map, so the common "have I been paid?" poll still
// costs a single RPC.
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

	roots, err := s.QueryAssetRoots(ctx, &entities.UniverseID{
		AssetRef:  ref,
		ProofType: entities.ProofTypeIssuance,
	})
	if err != nil {
		return 0, wrapErr("GetBalance", err)
	}
	if roots != nil &&
		(roots.IssuanceRoot != nil || roots.TransferRoot != nil) {

		return 0, nil
	}

	return 0, wrapErr("GetBalance", fmt.Errorf(
		"%w: %s", ErrAssetUnknown, ref,
	))
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

// ExportProof exports all wallet-known proof files for the given user-facing
// AssetRef.
//
// For a grouped fungible asset this enumerates each wallet-known
// issuance/tranche and exports one proof entry per asset output. For a single
// NFT/collectible or ungrouped asset-ID ref this normally returns one entry.
func (s *Wallet) ExportProof(ctx context.Context,
	ref entities.AssetRef) (*entities.ProofBundle, error) {

	if err := ref.Validate(); err != nil {
		return nil, wrapErr("ExportProof", err)
	}

	assets, err := s.ListAssets(ctx, &entities.ListAssetsRequest{
		AssetRef: &ref,
	})
	if err != nil {
		return nil, wrapErr("ExportProof", err)
	}

	if len(assets) == 0 {
		return nil, wrapErr("ExportProof", fmt.Errorf(
			"%w: %s", ErrAssetUnknown, ref,
		))
	}

	bundle := &entities.ProofBundle{
		AssetRef: ref,
		Entries:  make([]entities.ProofEntry, 0, len(assets)),
	}

	for _, asset := range assets {
		if asset == nil {
			continue
		}

		issuanceID := asset.Genesis.IssuanceID
		proofRef := entities.AssetRefFromAssetID(issuanceID)
		proof, err := s.exportProofFile(
			ctx, proofRef, asset.ScriptKey.PubKey, nil,
			"ExportProof",
		)
		if err != nil {
			return nil, err
		}
		if proof == nil || len(proof.RawProofFile) == 0 {
			return nil, wrapErr("ExportProof", fmt.Errorf(
				"%w: empty proof for issuance %s",
				ErrNoProofs, issuanceID,
			))
		}

		bundle.Entries = append(bundle.Entries, entities.ProofEntry{
			AssetRef:   proofBundleEntryRef(ref, asset),
			IssuanceID: issuanceID,
			ScriptKey:  asset.ScriptKey.PubKey,
			Amount:     asset.Amount,
			ProofFile:  proof.RawProofFile,
		})
	}

	if len(bundle.Entries) == 0 {
		return nil, wrapErr("ExportProof", fmt.Errorf(
			"%w: %s", ErrNoProofs, ref,
		))
	}

	return bundle, nil
}

// ExportProofFile exports a raw proof file for a specific asset output.
//
// This is the advanced/legacy escape hatch that maps closely to tapd's proof
// RPC. Most application code should use ExportProof with an AssetRef instead.
func (s *Wallet) ExportProofFile(ctx context.Context,
	ref entities.AssetRef, scriptKey entities.PubKey,
	outpoint *entities.Outpoint) (*entities.ProofFile, error) {

	return s.exportProofFile(
		ctx, ref, scriptKey, outpoint, "ExportProofFile",
	)
}

func (s *Wallet) exportProofFile(ctx context.Context,
	ref entities.AssetRef, scriptKey entities.PubKey,
	outpoint *entities.Outpoint, op string) (*entities.ProofFile, error) {

	proof, err := s.Client.ExportProof(ctx, ref, scriptKey, outpoint)
	if err != nil {
		return nil, wrapErr(op, err)
	}

	return proof, nil
}

// ImportProof imports every proof file in a ProofBundle and registers the
// resulting wallet transfers. The caller only supplies the bundle; concrete
// issuance IDs needed by tapd are decoded and registered internally.
func (s *Wallet) ImportProof(ctx context.Context,
	bundle *entities.ProofBundle) ([]*entities.RegisteredAsset, error) {

	if bundle == nil || len(bundle.Entries) == 0 {
		return nil, wrapErr("ImportProof", ErrIncompleteProofBundle)
	}

	for idx := range bundle.Entries {
		entry := bundle.Entries[idx]
		if len(entry.ProofFile) == 0 {
			return nil, wrapErr("ImportProof", fmt.Errorf(
				"%w: entry %d has empty proof file",
				ErrIncompleteProofBundle, idx,
			))
		}
	}

	registered := make([]*entities.RegisteredAsset, 0, len(bundle.Entries))
	for idx := range bundle.Entries {
		entry := bundle.Entries[idx]
		reg, err := s.importProofFile(ctx, &entities.ProofFile{
			RawProofFile: entry.ProofFile,
		}, "ImportProof")
		if err != nil {
			return nil, err
		}

		registered = append(registered, reg)
	}

	return registered, nil
}

// ImportProofFile imports a raw proof file received from a sender during an
// interactive transfer. This method handles the full import flow:
// 1. Unpacks the proof file into individual proofs
// 2. Inserts each proof into the local universe
// 3. Registers the transfer so the wallet recognizes the new asset
//
// Returns the registered asset details.
func (s *Wallet) ImportProofFile(ctx context.Context,
	proofFile *entities.ProofFile) (*entities.RegisteredAsset, error) {

	return s.importProofFile(ctx, proofFile, "ImportProofFile")
}

func (s *Wallet) importProofFile(ctx context.Context,
	proofFile *entities.ProofFile, op string) (*entities.RegisteredAsset,
	error) {

	if proofFile == nil || len(proofFile.RawProofFile) == 0 {
		return nil, wrapErr(op, ErrNoProofs)
	}

	// Step 1: Unpack the proof file into individual proofs.
	rawProofs, err := s.UnpackProofFile(ctx, proofFile.RawProofFile)
	if err != nil {
		return nil, wrapErr(op, err)
	}

	if len(rawProofs) == 0 {
		return nil, wrapErr(op,
			fmt.Errorf("proof file contains no proofs"))
	}

	// Step 2: Decode and insert each proof into the universe.
	var lastDecoded *entities.DecodedProof
	for _, rawProof := range rawProofs {
		// TODO: Decode the proof locally without using the RPC client.
		decoded, err := s.DecodeProof(ctx, rawProof)
		if err != nil {
			return nil, wrapErr(op, err)
		}

		err = s.InsertProof(ctx, rawProof, decoded)
		if err != nil {
			return nil, wrapErr(op, err)
		}

		lastDecoded = decoded
	}

	// Step 3: Register the transfer using the last proof's details.
	registrar, ok := s.Client.(transferRegistrarWithIssuance)
	if ok {
		registered, err := registrar.RegisterTransferWithIssuance(
			ctx, lastDecoded.AssetRef, lastDecoded.IssuanceID,
			lastDecoded.ScriptKey, lastDecoded.Outpoint,
		)
		if err != nil {
			return nil, wrapErr(op, err)
		}

		return registered, nil
	}

	registered, err := s.RegisterTransfer(
		ctx, lastDecoded.AssetRef, lastDecoded.ScriptKey,
		lastDecoded.Outpoint,
	)
	if err != nil {
		return nil, wrapErr(op, err)
	}

	return registered, nil
}

func proofBundleEntryRef(ref entities.AssetRef,
	asset *entities.Asset) entities.AssetRef {

	if asset == nil {
		return ref
	}

	if ref.IsGroupRef() && asset.Genesis.Type == entities.AssetTypeCollectible {
		return entities.AssetRefFromAssetID(asset.Genesis.IssuanceID)
	}

	return ref
}

// Send performs a simple one-shot address-based asset transfer.
//
// The addr must be a valid bech32m-encoded Taproot Asset address. The
// SDK decodes it up-front to decide how to frame the request:
//
//   - If the address embeds an amount (all V0/V1 addresses and V2
//     addresses that bake one in), omit WithAmount or pass the exact
//     matching value. Any other value returns ErrAmountMismatch.
//   - If the address embeds no amount (only possible for V2
//     addresses), the caller MUST pass WithAmount with a non-zero
//     value. Otherwise Send returns ErrAmountRequired.
//
// For multi-recipient sends in a single anchor transaction, use
// SendMulti. For fine-grained control over the Fund → Sign → Commit →
// Publish pipeline, use NewTxBuilder.
func (s *Wallet) Send(ctx context.Context, addr string,
	opts ...SendOption) (*entities.AssetTransfer, error) {

	// Decode locally: the SDK can read a bech32m Tap address from the
	// string alone, so Send does not spend an RPC round-trip just to
	// learn the embedded amount or address version.
	decoded, err := entities.DecodeAddress(addr)
	if err != nil {
		return nil, wrapErr("Send", err)
	}

	o := applySendOptions(opts)

	if err := validateSendAmount(decoded, o.amount); err != nil {
		return nil, wrapErr("Send", err)
	}

	// WithAmount set: route through the explicit-amount path to
	// preserve caller intent on the wire. Otherwise rely on the
	// amount embedded in the address.
	recipient := entities.Recipient{Address: addr}
	if o.amount > 0 {
		amt := o.amount
		recipient.Amount = &amt
	}

	req := &entities.SendAssetRequest{
		Recipients:                []entities.Recipient{recipient},
		FeeRate:                   o.feeRate,
		Label:                     o.label,
		SkipProofCourierPingCheck: o.skipProofCourierPingCheck,
	}

	transfer, err := s.SendAsset(ctx, req)
	if err != nil {
		return nil, wrapErr("Send", err)
	}

	return transfer, nil
}

// validateSendAmount enforces the amount vs. address-embedded-amount
// invariant used by Send. The caller passes the decoded destination
// address and the amount argument they intend to send.
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
// transaction. Each Recipient.Amount is optional: nil means "use the
// amount embedded in the address" (works for any address that embeds
// an amount). A non-nil Amount must be positive; if the address also
// embeds an amount, the two must match.
//
// Mixing explicit and embedded amounts in a single call is supported
// at this level: SendMulti decodes each address and echoes embedded
// values into the wire request so tapd sees a uniform shape.
//
// For single-recipient sends, prefer Send for simplicity.
func (s *Wallet) SendMulti(ctx context.Context,
	recipients []entities.Recipient,
	opts ...SendOption) (*entities.AssetTransfer, error) {

	if len(recipients) == 0 {
		return nil, wrapErr("SendMulti", ErrNoRecipients)
	}

	// Validate, collecting whether any Recipient carries an explicit
	// amount. If any does, every Recipient needs an explicit amount
	// on the wire; echo the embedded value for the nil ones.
	decoded := make([]*entities.Address, len(recipients))
	anyExplicit := false
	for i, r := range recipients {
		addr, err := entities.DecodeAddress(r.Address)
		if err != nil {
			return nil, wrapErr("SendMulti", err)
		}
		decoded[i] = addr

		if r.Amount == nil {
			if addr.Amount == 0 {
				return nil, wrapErr(
					"SendMulti", ErrAmountRequired,
				)
			}
			continue
		}

		anyExplicit = true
		if *r.Amount == 0 {
			return nil, wrapErr("SendMulti", fmt.Errorf(
				"%w: recipient %q has explicit Amount == 0",
				ErrAmountRequired, r.Address,
			))
		}
		if addr.Amount > 0 && addr.Amount != *r.Amount {
			return nil, wrapErr("SendMulti", fmt.Errorf(
				"%w: address embeds %d, caller passed %d",
				ErrAmountMismatch, addr.Amount, *r.Amount,
			))
		}
	}

	// If any recipient is explicit, the low-level SendAsset demands
	// every recipient be explicit too — so fill in the embedded
	// amount for the nil ones.
	if anyExplicit {
		normalised := make([]entities.Recipient, len(recipients))
		for i, r := range recipients {
			if r.Amount != nil {
				normalised[i] = r
				continue
			}
			amt := decoded[i].Amount
			normalised[i] = entities.Recipient{
				Address: r.Address,
				Amount:  &amt,
			}
		}
		recipients = normalised
	}

	o := applySendOptions(opts)
	req := &entities.SendAssetRequest{
		Recipients:                recipients,
		FeeRate:                   o.feeRate,
		Label:                     o.label,
		SkipProofCourierPingCheck: o.skipProofCourierPingCheck,
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
