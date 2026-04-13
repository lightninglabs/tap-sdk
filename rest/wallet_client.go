package rest

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/lightninglabs/tap-sdk/macaroon"
)

// walletClient implements tapsdk.WalletClient over REST.
type walletClient struct {
	transport *transport
}

func newWalletClient(tp *transport) *walletClient {
	return &walletClient{transport: tp}
}

// GetInfo returns general information about the tapd instance.
func (w *walletClient) GetInfo(
	ctx context.Context) (*entities.Info, error) {

	var resp jsonGetInfoResponse
	err := w.transport.doGet(
		ctx, "/v1/taproot-assets/getinfo",
		macaroon.AdminServiceMac, &resp,
	)
	if err != nil {
		return nil, err
	}

	return unmarshalInfo(&resp)
}

// ListAssets lists wallet assets with optional filtering.
func (w *walletClient) ListAssets(ctx context.Context,
	req *entities.ListAssetsRequest) ([]*entities.Asset, error) {

	params := url.Values{}
	var assetRefFilter *entities.AssetRef
	if req != nil {
		if req.WithWitness {
			params.Set("with_witness", "true")
		}
		if req.IncludeSpent {
			params.Set("include_spent", "true")
		}
		if req.IncludeLeased {
			params.Set("include_leased", "true")
		}
		if req.IncludeUnconfirmedMints {
			params.Set("include_unconfirmed_mints", "true")
		}
		if req.AssetRef != nil {
			if err := req.AssetRef.Validate(); err != nil {
				return nil, err
			}

			assetRefFilter = req.AssetRef
			if groupKey, ok := req.AssetRef.GroupKey(); ok {
				params.Set(
					"group_key",
					hex.EncodeToString(groupKey[:]),
				)
			}
		}
	}

	path := "/v1/taproot-assets/assets"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var resp jsonListAssetsResponse
	err := w.transport.doGet(
		ctx, path, macaroon.AdminServiceMac, &resp,
	)
	if err != nil {
		return nil, err
	}

	assets := make([]*entities.Asset, 0, len(resp.Assets))
	for _, jsonAsset := range resp.Assets {
		asset, err := unmarshalAsset(jsonAsset)
		if err != nil {
			return nil, err
		}

		if assetRefFilter != nil && asset.AssetRef != *assetRefFilter {
			continue
		}

		assets = append(assets, asset)
	}

	return assets, nil
}

// ListBalances returns asset balances keyed by AssetRef.
func (w *walletClient) ListBalances(ctx context.Context,
	req *entities.ListBalancesRequest) (
	*entities.ListBalancesResponse, error) {

	params := url.Values{}
	if req != nil {
		if req.IncludeLeased {
			params.Set("include_leased", "true")
		}
	}

	path := "/v1/taproot-assets/assets/balance"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var resp jsonListBalancesResponse
	err := w.transport.doGet(
		ctx, path, macaroon.AdminServiceMac, &resp,
	)
	if err != nil {
		return nil, err
	}

	assets, err := w.ListAssets(ctx, &entities.ListAssetsRequest{
		IncludeLeased: req != nil && req.IncludeLeased,
		AssetRef: func() *entities.AssetRef {
			if req == nil {
				return nil
			}

			return req.AssetRef
		}(),
		ScriptKeyType: func() *entities.ScriptKeyTypeQuery {
			if req == nil {
				return nil
			}

			return req.ScriptKeyType
		}(),
	})
	if err != nil {
		return nil, err
	}

	result := &entities.ListBalancesResponse{
		Balances:             make(map[string]*entities.AssetBalance),
		UnconfirmedTransfers: resp.UnconfirmedTransfers,
	}

	for _, asset := range assets {
		key := asset.AssetRef.String()
		balance, ok := result.Balances[key]
		if !ok {
			balance = &entities.AssetBalance{
				AssetRef:     asset.AssetRef,
				AssetGenesis: asset.Genesis,
			}

			if asset.GroupKey != nil {
				groupKey := asset.GroupKey.RawKey
				balance.GroupKey = &groupKey
			}

			result.Balances[key] = balance
		}

		balance.Balance += asset.Amount
	}

	return result, nil
}

// ListTransfers lists outgoing transfers with optional filtering.
func (w *walletClient) ListTransfers(ctx context.Context,
	req *entities.ListTransfersRequest) (
	[]*entities.AssetTransfer, error) {

	params := url.Values{}
	if req != nil && req.AnchorTxid != "" {
		params.Set("anchor_txid", req.AnchorTxid)
	}

	path := "/v1/taproot-assets/assets/transfers"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var resp jsonListTransfersResponse
	err := w.transport.doGet(
		ctx, path, macaroon.AdminServiceMac, &resp,
	)
	if err != nil {
		return nil, err
	}

	transfers := make(
		[]*entities.AssetTransfer, 0, len(resp.Transfers),
	)
	for _, jsonTransfer := range resp.Transfers {
		transfer, err := unmarshalAssetTransfer(jsonTransfer)
		if err != nil {
			return nil, err
		}

		transfers = append(transfers, transfer)
	}

	return transfers, nil
}

// jsonSendAssetRequest is the JSON body for the SendAsset
// RPC.
type jsonSendAssetRequest struct {
	TapAddrs []string `json:"tap_addrs,omitempty"`
	FeeRate  uint32   `json:"fee_rate,omitempty"`
	Label    string   `json:"label,omitempty"`

	Recipients           []*jsonAddressWithAmount `json:"addresses_with_amounts,omitempty"`        //nolint:lll
	SkipProofCourierPing bool                     `json:"skip_proof_courier_ping_check,omitempty"` //nolint:lll
}

// jsonAddressWithAmount is the JSON shape of taprpc.AddressWithAmount.
type jsonAddressWithAmount struct {
	TapAddr string `json:"tap_addr"`
	Amount  string `json:"amount"`
}

// SendAsset performs a one-shot address-based send.
func (w *walletClient) SendAsset(ctx context.Context,
	req *entities.SendAssetRequest) (
	*entities.AssetTransfer, error) {

	body := &jsonSendAssetRequest{}
	if req != nil {
		body.TapAddrs = req.TapAddresses
		body.FeeRate = req.FeeRate
		body.Label = req.Label
		body.SkipProofCourierPing = req.SkipProofCourierPingCheck

		if len(req.Recipients) > 0 {
			body.TapAddrs = nil
			body.Recipients = make(
				[]*jsonAddressWithAmount, 0,
				len(req.Recipients),
			)
			for _, r := range req.Recipients {
				body.Recipients = append(
					body.Recipients,
					&jsonAddressWithAmount{
						TapAddr: r.Address,
						Amount: fmt.Sprintf(
							"%d", r.Amount,
						),
					},
				)
			}
		}
	}

	var resp jsonSendAssetResponse
	err := w.transport.doPost(
		ctx, "/v1/taproot-assets/send",
		macaroon.AdminServiceMac, body, &resp,
	)
	if err != nil {
		return nil, err
	}

	return unmarshalAssetTransfer(resp.Transfer)
}

// jsonNewAddrRequest is the JSON body for the NewAddr RPC.
type jsonNewAddrRequest struct {
	AssetID  string `json:"asset_id,omitempty"`
	Amount   string `json:"amt,omitempty"`
	GroupKey string `json:"group_key,omitempty"`

	ScriptKey   *jsonScriptKey     `json:"script_key,omitempty"`
	InternalKey *jsonKeyDescriptor `json:"internal_key,omitempty"`

	TapscriptSibling          string `json:"tapscript_sibling,omitempty"`             //nolint:lll
	ProofCourierAddr          string `json:"proof_courier_addr,omitempty"`            //nolint:lll
	SkipProofCourierConnCheck bool   `json:"skip_proof_courier_conn_check,omitempty"` //nolint:lll

	AssetVersion   string `json:"asset_version,omitempty"`
	AddressVersion string `json:"address_version,omitempty"`
}

// NewAddr creates a new Taproot Asset address.
func (w *walletClient) NewAddr(ctx context.Context,
	req *entities.NewAddressRequest) (*entities.Address, error) {

	body := &jsonNewAddrRequest{}
	if req != nil {
		assetID, groupKey, err := req.AssetRef.Specifier()
		if err != nil && !req.AssetRef.IsZero() {
			return nil, err
		}
		if assetID != nil {
			body.AssetID = base64.StdEncoding.EncodeToString(
				(*assetID)[:],
			)
		}
		if req.Amount > 0 {
			body.Amount = fmt.Sprintf("%d", req.Amount)
		}
		if groupKey != nil {
			body.GroupKey = base64.StdEncoding.EncodeToString(
				(*groupKey)[:],
			)
		}
		if req.TapscriptSibling != nil {
			body.TapscriptSibling = hex.EncodeToString(
				req.TapscriptSibling,
			)
		}
		body.ProofCourierAddr = req.ProofCourierAddr
		body.SkipProofCourierConnCheck = req.SkipProofCourierConnCheck

		if req.AssetVersion != nil {
			body.AssetVersion = marshalAssetVersionJSON(
				*req.AssetVersion,
			)
		}
		if req.AddressVersion != nil {
			body.AddressVersion = marshalAddressVersionJSON(
				*req.AddressVersion,
			)
		}

		if req.ScriptKey != nil {
			body.ScriptKey = marshalScriptKeyJSON(req.ScriptKey)
		}
		if req.InternalKey != nil {
			body.InternalKey = marshalKeyDescriptorJSON(
				req.InternalKey,
			)
		}
	}

	var resp jsonAddr
	err := w.transport.doPost(
		ctx, "/v1/taproot-assets/addrs",
		macaroon.AdminServiceMac, body, &resp,
	)
	if err != nil {
		return nil, err
	}

	return unmarshalAddr(&resp)
}

// DecodeAddr decodes a bech32m Taproot Asset address string.
func (w *walletClient) DecodeAddr(ctx context.Context,
	addr string) (*entities.Address, error) {

	body := map[string]string{"addr": addr}

	var resp jsonAddr
	err := w.transport.doPost(
		ctx, "/v1/taproot-assets/addrs/decode",
		macaroon.AdminServiceMac, body, &resp,
	)
	if err != nil {
		return nil, err
	}

	return unmarshalAddr(&resp)
}

// QueryAddrs returns addresses previously created by this tapd
// instance.
func (w *walletClient) QueryAddrs(ctx context.Context,
	query *entities.AddressQuery) ([]*entities.Address, error) {

	params := url.Values{}
	if query != nil {
		if query.CreatedAfter > 0 {
			params.Set("created_after", fmt.Sprintf(
				"%d", query.CreatedAfter,
			))
		}
		if query.CreatedBefore > 0 {
			params.Set("created_before", fmt.Sprintf(
				"%d", query.CreatedBefore,
			))
		}
		if query.Limit > 0 {
			params.Set("limit", fmt.Sprintf(
				"%d", query.Limit,
			))
		}
		if query.Offset > 0 {
			params.Set("offset", fmt.Sprintf(
				"%d", query.Offset,
			))
		}
	}

	path := "/v1/taproot-assets/addrs"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var resp jsonQueryAddrsResponse
	err := w.transport.doGet(
		ctx, path, macaroon.AdminServiceMac, &resp,
	)
	if err != nil {
		return nil, err
	}

	addrs := make([]*entities.Address, 0, len(resp.Addrs))
	for _, jsonAddr := range resp.Addrs {
		addr, err := unmarshalAddr(jsonAddr)
		if err != nil {
			return nil, err
		}

		addrs = append(addrs, addr)
	}

	return addrs, nil
}

// jsonAddrReceivesRequest is the JSON body for the AddrReceives RPC.
type jsonAddrReceivesRequest struct {
	FilterAddr     string `json:"filter_addr,omitempty"`
	FilterStatus   string `json:"filter_status,omitempty"`
	StartTimestamp uint64 `json:"start_timestamp,omitempty"`
	EndTimestamp   uint64 `json:"end_timestamp,omitempty"`
	Offset         int32  `json:"offset,omitempty"`
	Limit          int32  `json:"limit,omitempty"`
	Direction      string `json:"direction,omitempty"`
}

// AddrReceives returns incoming transfer events for addresses.
func (w *walletClient) AddrReceives(ctx context.Context,
	query *entities.AddressReceivesQuery) (
	[]*entities.AddressEvent, error) {

	body := &jsonAddrReceivesRequest{}
	if query != nil {
		body.FilterAddr = query.FilterAddr
		body.FilterStatus = marshalAddrEventStatusJSON(
			query.FilterStatus,
		)
		body.StartTimestamp = query.StartTimestamp
		body.EndTimestamp = query.EndTimestamp
		body.Offset = query.Offset
		body.Limit = query.Limit
	}

	var resp jsonAddrReceivesResponse
	err := w.transport.doPost(
		ctx, "/v1/taproot-assets/addrs/receives",
		macaroon.AdminServiceMac, body, &resp,
	)
	if err != nil {
		return nil, err
	}

	events := make([]*entities.AddressEvent, 0, len(resp.Events))
	for _, jsonEvent := range resp.Events {
		event, err := unmarshalAddrEvent(jsonEvent)
		if err != nil {
			return nil, err
		}

		events = append(events, event)
	}

	return events, nil
}

// ListUtxos lists managed UTXOs with optional filtering.
func (w *walletClient) ListUtxos(ctx context.Context,
	req *entities.ListUtxosRequest) (
	map[string]*entities.ManagedUtxo, error) {

	params := url.Values{}
	if req != nil && req.IncludeLeased {
		params.Set("include_leased", "true")
	}

	path := "/v1/taproot-assets/assets/utxos"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var resp jsonListUtxosResponse
	err := w.transport.doGet(
		ctx, path, macaroon.AdminServiceMac, &resp,
	)
	if err != nil {
		return nil, err
	}

	result := make(
		map[string]*entities.ManagedUtxo, len(resp.ManagedUtxos),
	)
	for k, v := range resp.ManagedUtxos {
		utxo, err := unmarshalManagedUtxo(v)
		if err != nil {
			return nil, fmt.Errorf("unmarshal utxo %s: %w",
				k, err)
		}
		result[k] = utxo
	}

	return result, nil
}

// ListGroups lists all known asset groups.
func (w *walletClient) ListGroups(
	ctx context.Context) (map[string]*entities.GroupedAssets,
	error) {

	var resp jsonListGroupsResponse
	err := w.transport.doGet(
		ctx, "/v1/taproot-assets/assets/groups",
		macaroon.AdminServiceMac, &resp,
	)
	if err != nil {
		return nil, err
	}

	result := make(
		map[string]*entities.GroupedAssets, len(resp.Groups),
	)
	for k, v := range resp.Groups {
		group, err := unmarshalGroupedAssets(v)
		if err != nil {
			return nil, fmt.Errorf("unmarshal group %s: %w",
				k, err)
		}
		result[k] = group
	}

	return result, nil
}

// BurnAsset burns asset units.
func (w *walletClient) BurnAsset(ctx context.Context,
	req *entities.BurnAssetRequest) (
	*entities.BurnAssetResponse, error) {

	if req.AssetRef.IsZero() {
		return nil, fmt.Errorf("asset ref is required")
	}

	assetID, groupKey, err := req.AssetRef.Specifier()
	if err != nil {
		return nil, err
	}

	body := map[string]any{
		"amount_to_burn":    fmt.Sprintf("%d", req.AmountToBurn),
		"confirmation_text": req.ConfirmationText,
	}

	specifier := map[string]string{}
	switch {
	case groupKey != nil:
		specifier["group_key_str"] = hex.EncodeToString(
			(*groupKey)[:],
		)
	case assetID != nil:
		specifier["asset_id_str"] = hex.EncodeToString(
			(*assetID)[:],
		)
	}
	body["asset_specifier"] = specifier

	if req.Note != "" {
		body["note"] = req.Note
	}

	var resp jsonBurnAssetResponse
	err = w.transport.doPost(
		ctx, "/v1/taproot-assets/burn",
		macaroon.AdminServiceMac, body, &resp,
	)
	if err != nil {
		return nil, err
	}

	return unmarshalBurnAssetResponse(&resp)
}

// ListBurns lists asset burns with optional filtering.
func (w *walletClient) ListBurns(ctx context.Context,
	req *entities.ListBurnsRequest) ([]*entities.AssetBurn,
	error) {

	params := url.Values{}
	if req != nil {
		if req.AssetRef != nil {
			assetID, groupKey, err := req.AssetRef.Specifier()
			if err != nil {
				return nil, err
			}

			if assetID != nil {
				params.Set("asset_id",
					hex.EncodeToString((*assetID)[:]))
			}

			if groupKey != nil {
				params.Set("group_key",
					hex.EncodeToString((*groupKey)[:]))
			}
		}
		if req.AnchorTxid != nil {
			params.Set("anchor_txid",
				hex.EncodeToString(
					req.AnchorTxid[:],
				))
		}
	}

	path := "/v1/taproot-assets/burns"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var resp jsonListBurnsResponse
	err := w.transport.doGet(
		ctx, path, macaroon.AdminServiceMac, &resp,
	)
	if err != nil {
		return nil, err
	}

	burns := make([]*entities.AssetBurn, 0, len(resp.Burns))
	for _, b := range resp.Burns {
		burn, err := unmarshalAssetBurn(b)
		if err != nil {
			return nil, err
		}
		burns = append(burns, burn)
	}

	return burns, nil
}

// FetchAssetMeta fetches the metadata for an asset.
func (w *walletClient) FetchAssetMeta(ctx context.Context,
	req *entities.FetchAssetMetaRequest) (
	*entities.AssetMeta, error) {

	var path string
	switch {
	case req.AssetRef != nil:
		assetID, _, err := req.AssetRef.Specifier()
		if err != nil {
			return nil, err
		}

		if assetID == nil {
			return nil, fmt.Errorf("metadata lookup " +
				"requires an asset-ID ref; tapd does " +
				"not support group-key metadata lookup")
		}

		path = fmt.Sprintf(
			"/v1/taproot-assets/assets/meta/asset-id/%s",
			hex.EncodeToString((*assetID)[:]),
		)

	case req.MetaHash != nil:
		path = fmt.Sprintf(
			"/v1/taproot-assets/assets/meta/hash/%s",
			hex.EncodeToString(req.MetaHash[:]),
		)

	default:
		return nil, fmt.Errorf("either asset_ref or " +
			"meta_hash must be set")
	}

	var resp jsonFetchAssetMetaResponse
	err := w.transport.doGet(
		ctx, path, macaroon.AdminServiceMac, &resp,
	)
	if err != nil {
		return nil, err
	}

	return unmarshalFetchAssetMetaResponse(&resp)
}

// VerifyProof verifies a proof file.
func (w *walletClient) VerifyProof(ctx context.Context,
	rawProofFile []byte) (
	*entities.VerifyProofResponse, error) {

	body := map[string]any{
		"raw_proof_file": base64.StdEncoding.EncodeToString(
			rawProofFile,
		),
	}

	var resp jsonVerifyProofResponse
	err := w.transport.doPost(
		ctx, "/v1/taproot-assets/proofs/verify",
		macaroon.AdminServiceMac, body, &resp,
	)
	if err != nil {
		return nil, err
	}

	return unmarshalVerifyProofResponse(&resp)
}

// marshalAssetVersionJSON converts an AssetVersion to a proto JSON
// enum string.
func marshalAssetVersionJSON(v entities.AssetVersion) string {
	switch v {
	case entities.AssetVersionV1:
		return "ASSET_VERSION_V1"
	default:
		return "ASSET_VERSION_V0"
	}
}

// marshalAddressVersionJSON converts an AddressVersion to a proto
// JSON enum string.
func marshalAddressVersionJSON(v entities.AddressVersion) string {
	switch v {
	case entities.AddressVersionV1:
		return "ADDR_VERSION_V1"
	case entities.AddressVersionV2:
		return "ADDR_VERSION_V2"
	default:
		return "ADDR_VERSION_V0"
	}
}

// marshalAddrEventStatusJSON converts an AddressEventStatus to a
// proto JSON enum string.
func marshalAddrEventStatusJSON(
	s entities.AddressEventStatus) string {

	switch s {
	case entities.AddressEventStatusTransactionDetected:
		return "ADDR_EVENT_STATUS_TRANSACTION_DETECTED"
	case entities.AddressEventStatusTransactionConfirmed:
		return "ADDR_EVENT_STATUS_TRANSACTION_CONFIRMED"
	case entities.AddressEventStatusProofReceived:
		return "ADDR_EVENT_STATUS_PROOF_RECEIVED"
	case entities.AddressEventStatusCompleted:
		return "ADDR_EVENT_STATUS_COMPLETED"
	default:
		return "ADDR_EVENT_STATUS_UNKNOWN"
	}
}

// marshalScriptKeyJSON converts a ScriptKey to its JSON
// representation.
func marshalScriptKeyJSON(
	key *entities.ScriptKey) *jsonScriptKey {

	if key == nil {
		return nil
	}

	result := &jsonScriptKey{
		PubKey:   hex.EncodeToString(key.PubKey[:]),
		TapTweak: hex.EncodeToString(key.TapTweak),
	}

	if key.KeyDesc != (entities.KeyDescriptor{}) {
		result.KeyDesc = marshalKeyDescriptorJSON(&key.KeyDesc)
	}

	return result
}

// marshalKeyDescriptorJSON converts a KeyDescriptor to its JSON
// representation.
func marshalKeyDescriptorJSON(
	desc *entities.KeyDescriptor) *jsonKeyDescriptor {

	if desc == nil {
		return nil
	}

	return &jsonKeyDescriptor{
		RawKeyBytes: hex.EncodeToString(desc.RawKeyBytes[:]),
		KeyLoc: &jsonKeyLocator{
			KeyFamily: int32(desc.KeyLocator.Family),
			KeyIndex:  int32(desc.KeyLocator.Index),
		},
	}
}
