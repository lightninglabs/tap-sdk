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

		assets = append(assets, asset)
	}

	return assets, nil
}

// ListBalances returns asset balances with optional grouping.
func (w *walletClient) ListBalances(ctx context.Context,
	req *entities.ListBalancesRequest) (
	*entities.ListBalancesResponse, error) {

	params := url.Values{}
	if req != nil {
		if req.IncludeLeased {
			params.Set("include_leased", "true")
		}

		switch req.GroupBy {
		case entities.BalanceGroupByAssetID:
			params.Set("asset_id", "true")
		case entities.BalanceGroupByGroupKey:
			params.Set("group_key", "true")
		}

		if req.AssetFilter != nil {
			params.Set(
				"asset_filter",
				hex.EncodeToString(req.AssetFilter[:]),
			)
		}
		if req.GroupKeyFilter != nil {
			params.Set(
				"group_key_filter",
				hex.EncodeToString(req.GroupKeyFilter[:]),
			)
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

	result := &entities.ListBalancesResponse{
		AssetBalances: make(
			map[string]*entities.AssetBalance,
		),
		AssetGroupBalances: make(
			map[string]*entities.AssetGroupBalance,
		),
		UnconfirmedTransfers: resp.UnconfirmedTransfers,
	}

	for key, bal := range resp.AssetBalances {
		balance, err := unmarshalAssetBalance(bal)
		if err != nil {
			return nil, err
		}

		result.AssetBalances[key] = balance
	}

	for key, bal := range resp.AssetGroupBalances {
		balance, err := unmarshalAssetGroupBalance(bal)
		if err != nil {
			return nil, err
		}

		result.AssetGroupBalances[key] = balance
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

// jsonSendAssetRequest is the JSON body for the SendAsset RPC.
type jsonSendAssetRequest struct {
	TapAddrs             []string                 `json:"tap_addrs,omitempty"`
	FeeRate              uint32                   `json:"fee_rate,omitempty"`
	Label                string                   `json:"label,omitempty"`
	Recipients           []*jsonAddressWithAmount `json:"addresses_with_amounts,omitempty"`
	SkipProofCourierPing bool                     `json:"skip_proof_courier_ping_check,omitempty"`
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
	AssetID                   string             `json:"asset_id,omitempty"`
	Amount                    string             `json:"amt,omitempty"`
	GroupKey                  string             `json:"group_key,omitempty"`
	ScriptKey                 *jsonScriptKey     `json:"script_key,omitempty"`
	InternalKey               *jsonKeyDescriptor `json:"internal_key,omitempty"`
	TapscriptSibling          string             `json:"tapscript_sibling,omitempty"`
	ProofCourierAddr          string             `json:"proof_courier_addr,omitempty"`
	SkipProofCourierConnCheck bool               `json:"skip_proof_courier_conn_check,omitempty"`
	AssetVersion              string             `json:"asset_version,omitempty"`
	AddressVersion            string             `json:"address_version,omitempty"`
}

// NewAddr creates a new Taproot Asset address.
func (w *walletClient) NewAddr(ctx context.Context,
	req *entities.NewAddressRequest) (*entities.Address, error) {

	body := &jsonNewAddrRequest{}
	if req != nil {
		if req.AssetID != nil {
			body.AssetID = base64.StdEncoding.EncodeToString(
				req.AssetID[:],
			)
		}
		if req.Amount > 0 {
			body.Amount = fmt.Sprintf("%d", req.Amount)
		}
		if req.GroupKey != nil {
			body.GroupKey = base64.StdEncoding.EncodeToString(
				req.GroupKey[:],
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
