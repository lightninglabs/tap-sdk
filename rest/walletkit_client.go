package rest

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/lightninglabs/tap-sdk/anchor"
	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/lightninglabs/tap-sdk/macaroon"
)

// walletKitClient implements tapsdk.WalletKitClient over REST.
type walletKitClient struct {
	transport *transport
}

func newWalletKitClient(tp *transport) *walletKitClient {
	return &walletKitClient{transport: tp}
}

type jsonExportAssetWalletBackupRequest struct {
	Mode string `json:"mode"`
}

type jsonImportAssetsFromBackupRequest struct {
	Backup string `json:"backup"`
}

func marshalBackupMode(mode entities.BackupMode) string {
	switch mode {
	case entities.BackupModeCompact:
		return "COMPACT"

	case entities.BackupModeOptimistic:
		return "OPTIMISTIC"

	default:
		return "RAW"
	}
}

// jsonNextScriptKeyRequest is the JSON body for NextScriptKey.
type jsonNextScriptKeyRequest struct {
	KeyFamily uint32 `json:"key_family"`
}

// DeriveScriptKey derives a new script key for receiving assets.
func (w *walletKitClient) DeriveScriptKey(
	ctx context.Context) (*entities.ScriptKey, error) {

	body := &jsonNextScriptKeyRequest{
		KeyFamily: entities.TaprootAssetsKeyFamily,
	}

	var resp jsonNextScriptKeyResponse
	err := w.transport.doPost(
		ctx, "/v1/taproot-assets/wallet/script-key/next",
		macaroon.WalletKitServiceMac, body, &resp,
	)
	if err != nil {
		return nil, err
	}

	if resp.ScriptKey == nil {
		return nil, fmt.Errorf("invalid script key response")
	}

	return unmarshalScriptKey(resp.ScriptKey)
}

// jsonNextInternalKeyRequest is the JSON body for
// NextInternalKey.
type jsonNextInternalKeyRequest struct {
	KeyFamily uint32 `json:"key_family"`
}

// DeriveInternalKey derives a new internal key for anchor outputs.
func (w *walletKitClient) DeriveInternalKey(
	ctx context.Context) (*entities.InternalKey, error) {

	body := &jsonNextInternalKeyRequest{
		KeyFamily: entities.TaprootAssetsKeyFamily,
	}

	var resp jsonNextInternalKeyResponse
	err := w.transport.doPost(
		ctx,
		"/v1/taproot-assets/wallet/internal-key/next",
		macaroon.WalletKitServiceMac, body, &resp,
	)
	if err != nil {
		return nil, err
	}

	if resp.InternalKey == nil {
		return nil, fmt.Errorf("invalid internal key response")
	}

	keyDesc, err := unmarshalKeyDescriptor(resp.InternalKey)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to unmarshal internal key: %w", err,
		)
	}

	return &entities.InternalKey{
		PubKey:     keyDesc.RawKeyBytes,
		KeyLocator: keyDesc.KeyLocator,
	}, nil
}

// jsonFundVirtualPsbtRequest is the JSON body for
// FundVirtualPsbt (raw template variant).
type jsonFundVirtualPsbtRequest struct {
	Raw  *jsonTxTemplate `json:"raw,omitempty"`
	Psbt string          `json:"psbt,omitempty"`
}

// jsonTxTemplate is the JSON shape of
// assetwalletrpc.TxTemplate.
type jsonTxTemplate struct {
	Inputs []*jsonPrevIDReq `json:"inputs,omitempty"`

	AddressesWithAmounts []*jsonAddressWithAmount `json:"addresses_with_amounts,omitempty"` //nolint:lll
}

// jsonPrevIDReq is the JSON shape of assetwalletrpc.PrevId.
type jsonPrevIDReq struct {
	Outpoint  *jsonOutpointReq `json:"outpoint"`
	ID        string           `json:"id"`
	ScriptKey string           `json:"script_key"`
}

// FundTransfer funds a virtual transaction using addresses.
func (w *walletKitClient) FundTransfer(ctx context.Context,
	recipients []entities.Recipient,
	inputs []entities.PrevID) (*entities.FundedTransfer, error) {

	rpcInputs := make([]*jsonPrevIDReq, 0, len(inputs))
	for _, input := range inputs {
		rpcInputs = append(rpcInputs, &jsonPrevIDReq{
			Outpoint: &jsonOutpointReq{
				Txid: hex.EncodeToString(
					input.Outpoint.Txid[:],
				),
				OutputIndex: input.Outpoint.Index,
			},
			ID: hex.EncodeToString(
				input.IssuanceID[:],
			),
			ScriptKey: hex.EncodeToString(
				input.ScriptKey[:],
			),
		})
	}

	rpcRecipients := make(
		[]*jsonAddressWithAmount, 0, len(recipients),
	)
	for _, r := range recipients {
		amount, ok := r.Amount()
		if !ok || amount == 0 {
			return nil, fmt.Errorf("FundTransfer recipient "+
				"%q requires an explicit amount", r.Address)
		}
		rpcRecipients = append(rpcRecipients,
			&jsonAddressWithAmount{
				TapAddr: r.Address,
				Amount:  fmt.Sprintf("%d", amount),
			},
		)
	}

	body := &jsonFundVirtualPsbtRequest{
		Raw: &jsonTxTemplate{
			Inputs:               rpcInputs,
			AddressesWithAmounts: rpcRecipients,
		},
	}

	var resp jsonFundVirtualPsbtResponse
	err := w.transport.doPost(
		ctx, "/v1/taproot-assets/wallet/virtual-psbt/fund",
		macaroon.WalletKitServiceMac, body, &resp,
	)
	if err != nil {
		return nil, err
	}

	fundedPsbt, err := parseHexBytes(resp.FundedPsbt)
	if err != nil {
		return nil, fmt.Errorf(
			"invalid funded_psbt: %w", err,
		)
	}

	passivePsbts := make([][]byte, 0, len(resp.PassiveAssetPsbts))
	for _, p := range resp.PassiveAssetPsbts {
		psbt, err := parseHexBytes(p)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid passive_asset_psbt: %w", err,
			)
		}

		passivePsbts = append(passivePsbts, psbt)
	}

	return &entities.FundedTransfer{
		FundedPsbt:        fundedPsbt,
		PassiveAssetPsbts: passivePsbts,
	}, nil
}

// FundInteractivePsbt funds a virtual PSBT for interactive sends.
func (w *walletKitClient) FundInteractivePsbt(ctx context.Context,
	psbt []byte) (*entities.FundedTransfer, error) {

	body := &jsonFundVirtualPsbtRequest{
		Psbt: hex.EncodeToString(psbt),
	}

	var resp jsonFundVirtualPsbtResponse
	err := w.transport.doPost(
		ctx, "/v1/taproot-assets/wallet/virtual-psbt/fund",
		macaroon.WalletKitServiceMac, body, &resp,
	)
	if err != nil {
		return nil, err
	}

	fundedPsbt, err := parseHexBytes(resp.FundedPsbt)
	if err != nil {
		return nil, fmt.Errorf(
			"invalid funded_psbt: %w", err,
		)
	}

	passivePsbts := make([][]byte, 0, len(resp.PassiveAssetPsbts))
	for _, p := range resp.PassiveAssetPsbts {
		psbt, err := parseHexBytes(p)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid passive_asset_psbt: %w", err,
			)
		}

		passivePsbts = append(passivePsbts, psbt)
	}

	return &entities.FundedTransfer{
		FundedPsbt:        fundedPsbt,
		PassiveAssetPsbts: passivePsbts,
	}, nil
}

// jsonSignVirtualPsbtRequest is the JSON body for
// SignVirtualPsbt.
type jsonSignVirtualPsbtRequest struct {
	FundedPsbt string `json:"funded_psbt"`
}

// SignVirtualPsbt signs a virtual transaction.
func (w *walletKitClient) SignVirtualPsbt(ctx context.Context,
	fundedPsbt []byte) ([]byte, error) {

	body := &jsonSignVirtualPsbtRequest{
		FundedPsbt: hex.EncodeToString(
			fundedPsbt,
		),
	}

	var resp jsonSignVirtualPsbtResponse
	err := w.transport.doPost(
		ctx, "/v1/taproot-assets/wallet/virtual-psbt/sign",
		macaroon.WalletKitServiceMac, body, &resp,
	)
	if err != nil {
		return nil, err
	}

	return parseHexBytes(resp.SignedPsbt)
}

// jsonCommitVirtualPsbtsRequest is the JSON body for
// CommitVirtualPsbts.
type jsonCommitVirtualPsbtsRequest struct {
	VirtualPsbts      []string `json:"virtual_psbts"`
	PassiveAssetPsbts []string `json:"passive_asset_psbts,omitempty"`
	AnchorPsbt        string   `json:"anchor_psbt"`
	SatPerVByte       string   `json:"sat_per_vbyte,omitempty"`
	Add               bool     `json:"add,omitempty"`
}

// CommitVirtualPsbts commits virtual transactions.
func (w *walletKitClient) CommitVirtualPsbts(ctx context.Context,
	virtualPsbts [][]byte, passivePsbts [][]byte,
	feeRate uint64) (*entities.CommittedTransfer, error) {

	anchorPsbt, err := anchor.PreparePsbt(virtualPsbts, passivePsbts)
	if err != nil {
		return nil, fmt.Errorf("prepare anchor PSBT: %w", err)
	}

	vPsbts := make([]string, 0, len(virtualPsbts))
	for _, p := range virtualPsbts {
		vPsbts = append(
			vPsbts,
			hex.EncodeToString(p),
		)
	}

	pPsbts := make([]string, 0, len(passivePsbts))
	for _, p := range passivePsbts {
		pPsbts = append(
			pPsbts,
			hex.EncodeToString(p),
		)
	}

	body := &jsonCommitVirtualPsbtsRequest{
		VirtualPsbts:      vPsbts,
		PassiveAssetPsbts: pPsbts,
		AnchorPsbt:        hex.EncodeToString(anchorPsbt),
		SatPerVByte:       fmt.Sprintf("%d", feeRate),
		Add:               true,
	}

	var resp jsonCommitVirtualPsbtsResponse
	err = w.transport.doPost(
		ctx,
		"/v1/taproot-assets/wallet/virtual-psbt/commit",
		macaroon.WalletKitServiceMac, body, &resp,
	)
	if err != nil {
		return nil, err
	}

	respAnchorPsbt, err := parseHexBytes(resp.AnchorPsbt)
	if err != nil {
		return nil, fmt.Errorf(
			"invalid anchor_psbt: %w", err,
		)
	}

	vPsbtBytes := make([][]byte, 0, len(resp.VirtualPsbts))
	for _, p := range resp.VirtualPsbts {
		psbt, err := parseHexBytes(p)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid virtual_psbt: %w", err,
			)
		}

		vPsbtBytes = append(vPsbtBytes, psbt)
	}

	pPsbtBytes := make(
		[][]byte, 0, len(resp.PassiveAssetPsbts),
	)
	for _, p := range resp.PassiveAssetPsbts {
		psbt, err := parseHexBytes(p)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid passive_psbt: %w", err,
			)
		}

		pPsbtBytes = append(pPsbtBytes, psbt)
	}

	return &entities.CommittedTransfer{
		AnchorPsbt:        respAnchorPsbt,
		VirtualPsbts:      vPsbtBytes,
		PassiveAssetPsbts: pPsbtBytes,
	}, nil
}

// jsonAnchorVirtualPsbtsRequest is the JSON body for
// AnchorVirtualPsbts.
type jsonAnchorVirtualPsbtsRequest struct {
	VirtualPsbts []string `json:"virtual_psbts"`
}

// AnchorVirtualPsbts anchors signed virtual PSBTs in a single
// call.
func (w *walletKitClient) AnchorVirtualPsbts(ctx context.Context,
	signedPsbts [][]byte) (*entities.AssetTransfer, error) {

	psbts := make([]string, 0, len(signedPsbts))
	for _, p := range signedPsbts {
		psbts = append(
			psbts,
			hex.EncodeToString(p),
		)
	}

	body := &jsonAnchorVirtualPsbtsRequest{
		VirtualPsbts: psbts,
	}

	var resp jsonSendAssetResponse
	err := w.transport.doPost(
		ctx,
		"/v1/taproot-assets/wallet/virtual-psbt/anchor",
		macaroon.WalletKitServiceMac, body, &resp,
	)
	if err != nil {
		return nil, err
	}

	return unmarshalAssetTransfer(resp.Transfer)
}

// jsonPublishAndLogRequest is the JSON body for
// PublishAndLogTransfer.
type jsonPublishAndLogRequest struct {
	AnchorPsbt        string   `json:"anchor_psbt"`
	VirtualPsbts      []string `json:"virtual_psbts"`
	PassiveAssetPsbts []string `json:"passive_asset_psbts,omitempty"`

	SkipAnchorTxBroadcast bool `json:"skip_anchor_tx_broadcast,omitempty"` //nolint:lll
}

// PublishAndLogTransfer publishes the anchor transaction and logs
// the transfer.
func (w *walletKitClient) PublishAndLogTransfer(ctx context.Context,
	anchorPsbt []byte, virtualPsbts [][]byte,
	passivePsbts [][]byte,
	skipAnchorTxBroadcast bool) (
	*entities.AssetPacket, error) {

	vPsbts := make([]string, 0, len(virtualPsbts))
	for _, p := range virtualPsbts {
		vPsbts = append(
			vPsbts,
			hex.EncodeToString(p),
		)
	}

	pPsbts := make([]string, 0, len(passivePsbts))
	for _, p := range passivePsbts {
		pPsbts = append(
			pPsbts,
			hex.EncodeToString(p),
		)
	}

	body := &jsonPublishAndLogRequest{
		AnchorPsbt: hex.EncodeToString(
			anchorPsbt,
		),
		VirtualPsbts:          vPsbts,
		PassiveAssetPsbts:     pPsbts,
		SkipAnchorTxBroadcast: skipAnchorTxBroadcast,
	}

	var resp jsonPublishAndLogResponse
	err := w.transport.doPost(
		ctx,
		"/v1/taproot-assets/wallet/virtual-psbt/log-transfer",
		macaroon.WalletKitServiceMac, body, &resp,
	)
	if err != nil {
		return nil, err
	}

	if resp.Transfer == nil {
		return nil, fmt.Errorf("invalid transfer response")
	}

	anchorTx, err := parseHexBytes(resp.Transfer.AnchorTx)
	if err != nil {
		return nil, fmt.Errorf(
			"invalid anchor_tx: %w", err,
		)
	}

	// Return the raw bytes from the original call for
	// virtual and passive transactions.
	return &entities.AssetPacket{
		AnchorTransaction:        anchorTx,
		VirtualTransactions:      virtualPsbts,
		PassiveAssetTransactions: passivePsbts,
	}, nil
}

// QueryInternalKey looks up an internal key by its raw public key.
func (w *walletKitClient) QueryInternalKey(ctx context.Context,
	internalKey []byte) (*entities.KeyDescriptor, error) {

	keyHex := hex.EncodeToString(internalKey)
	path := fmt.Sprintf(
		"/v1/taproot-assets/wallet/internal-key/%s", keyHex,
	)

	var resp jsonQueryInternalKeyResponse
	err := w.transport.doGet(
		ctx, path, macaroon.AdminServiceMac, &resp,
	)
	if err != nil {
		return nil, err
	}

	if resp.InternalKey == nil {
		return nil, fmt.Errorf("empty internal key response")
	}

	return unmarshalKeyDescriptor(resp.InternalKey)
}

// QueryScriptKey looks up a script key by its tweaked public key.
func (w *walletKitClient) QueryScriptKey(ctx context.Context,
	tweakedScriptKey []byte) (*entities.ScriptKey, error) {

	keyHex := hex.EncodeToString(tweakedScriptKey)
	path := fmt.Sprintf(
		"/v1/taproot-assets/wallet/script-key/%s", keyHex,
	)

	var resp jsonQueryScriptKeyResponse
	err := w.transport.doGet(
		ctx, path, macaroon.AdminServiceMac, &resp,
	)
	if err != nil {
		return nil, err
	}

	if resp.ScriptKey == nil {
		return nil, fmt.Errorf("empty script key response")
	}

	return unmarshalScriptKey(resp.ScriptKey)
}

// ProveAssetOwnership generates a proof of ownership for an asset.
func (w *walletKitClient) ProveAssetOwnership(ctx context.Context,
	req *entities.ProveOwnershipRequest) (
	*entities.OwnershipProof, error) {

	if err := req.AssetRef.Validate(); err != nil {
		return nil, err
	}

	assetID, ok := req.AssetRef.AssetID()
	if !ok {
		return nil, fmt.Errorf("prove ownership requires an " +
			"asset-ID ref; group-key refs span multiple " +
			"tranches (see issue #85)")
	}
	if err := entities.ValidateOwnershipChallenge(req.Challenge); err != nil {
		return nil, err
	}

	body := map[string]any{
		"asset_id": hex.EncodeToString(
			assetID[:],
		),
		"script_key": hex.EncodeToString(
			req.ScriptKey[:],
		),
		"outpoint": map[string]any{
			"txid":         hex.EncodeToString(req.Outpoint.Txid[:]),
			"output_index": req.Outpoint.Index,
		},
	}

	if len(req.Challenge) > 0 {
		body["challenge"] = hex.EncodeToString(
			req.Challenge,
		)
	}

	var resp jsonOwnershipProof
	err := w.transport.doPost(
		ctx, "/v1/taproot-assets/wallet/ownership/prove",
		macaroon.AdminServiceMac, body, &resp,
	)
	if err != nil {
		return nil, err
	}

	proofBytes, err := parseHexBytes(resp.ProofWithWitness)
	if err != nil {
		return nil, fmt.Errorf("decode ownership proof: %w",
			err)
	}

	return &entities.OwnershipProof{
		AssetRef:         req.AssetRef,
		IssuanceID:       assetID,
		ScriptKey:        req.ScriptKey,
		Outpoint:         req.Outpoint,
		ProofWithWitness: proofBytes,
	}, nil
}

// VerifyAssetOwnership verifies an asset ownership proof.
func (w *walletKitClient) VerifyAssetOwnership(ctx context.Context,
	req *entities.VerifyOwnershipRequest) (
	*entities.VerifyOwnershipResponse, error) {

	if err := entities.ValidateOwnershipChallenge(req.Challenge); err != nil {
		return nil, err
	}

	body := map[string]any{
		"proof_with_witness": hex.EncodeToString(
			req.ProofWithWitness,
		),
	}
	if len(req.Challenge) > 0 {
		body["challenge"] = hex.EncodeToString(req.Challenge)
	}

	var resp jsonVerifyOwnershipResponse
	err := w.transport.doPost(
		ctx, "/v1/taproot-assets/wallet/ownership/verify",
		macaroon.AdminServiceMac, body, &resp,
	)
	if err != nil {
		return nil, err
	}

	return unmarshalVerifyOwnershipResponse(&resp)
}

// RemoveUTXOLease removes a lease on a UTXO.
func (w *walletKitClient) RemoveUTXOLease(ctx context.Context,
	outpoint entities.Outpoint) error {

	body := map[string]any{
		"outpoint": map[string]any{
			"txid":         hex.EncodeToString(outpoint.Txid[:]),
			"output_index": outpoint.Index,
		},
	}

	return w.transport.doPost(
		ctx, "/v1/taproot-assets/wallet/utxo-lease/delete",
		macaroon.AdminServiceMac, body, nil,
	)
}

// DeclareScriptKey informs the wallet about an externally derived
// script key.
func (w *walletKitClient) DeclareScriptKey(ctx context.Context,
	req *entities.DeclareScriptKeyRequest) (
	*entities.ScriptKey, error) {

	keyDesc := map[string]any{
		"raw_key_bytes": hex.EncodeToString(
			req.ScriptKey.KeyDesc.RawKeyBytes[:],
		),
		"key_loc": map[string]any{
			"key_family": req.ScriptKey.KeyDesc.KeyLocator.Family,
			"key_index":  req.ScriptKey.KeyDesc.KeyLocator.Index,
		},
	}

	scriptKeyMap := map[string]any{
		"pub_key": hex.EncodeToString(
			req.ScriptKey.PubKey[:],
		),
		"key_desc": keyDesc,
	}

	if len(req.ScriptKey.TapTweak) > 0 {
		scriptKeyMap["tap_tweak"] = hex.EncodeToString(
			req.ScriptKey.TapTweak,
		)
	}

	body := map[string]any{
		"script_key": scriptKeyMap,
	}

	var resp jsonDeclareScriptKeyResponse
	err := w.transport.doPost(
		ctx, "/v1/taproot-assets/wallet/script-key/declare",
		macaroon.AdminServiceMac, body, &resp,
	)
	if err != nil {
		return nil, err
	}

	if resp.ScriptKey == nil {
		return nil, fmt.Errorf("empty script key response")
	}

	return unmarshalScriptKey(resp.ScriptKey)
}

// ExportBackup exports an asset wallet backup blob.
func (w *walletKitClient) ExportBackup(ctx context.Context,
	mode entities.BackupMode) ([]byte, error) {

	body := &jsonExportAssetWalletBackupRequest{
		Mode: marshalBackupMode(mode),
	}

	var resp jsonExportAssetWalletBackupResponse
	err := w.transport.doPost(
		ctx, "/v1/taproot-assets/wallet/backup/export",
		macaroon.WalletKitServiceMac, body, &resp,
	)
	if err != nil {
		return nil, err
	}

	backup, err := parseHexBytes(resp.Backup)
	if err != nil {
		return nil, fmt.Errorf("invalid backup: %w", err)
	}

	return backup, nil
}

// ImportBackup imports assets from a previously exported wallet backup blob.
func (w *walletKitClient) ImportBackup(ctx context.Context,
	backup []byte) (uint32, error) {

	body := &jsonImportAssetsFromBackupRequest{
		Backup: hex.EncodeToString(backup),
	}

	var resp jsonImportAssetsFromBackupResponse
	err := w.transport.doPost(
		ctx, "/v1/taproot-assets/wallet/backup/import",
		macaroon.WalletKitServiceMac, body, &resp,
	)
	if err != nil {
		return 0, err
	}

	return resp.NumImported, nil
}
