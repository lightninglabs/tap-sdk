package grpc

import (
	"context"
	"encoding/hex"
	"fmt"
	"maps"
	"time"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/lightninglabs/tap-sdk/macaroon"
	"github.com/lightninglabs/taproot-assets/taprpc"
	"google.golang.org/grpc"
)

type walletClient struct {
	client   taprpc.TaprootAssetsClient
	timeout  time.Duration
	adminMac macaroon.SerializedMacaroon
}

// NewWalletClient creates a new Wallet client.
func NewWalletClient(conn grpc.ClientConnInterface, timeout time.Duration,
	adminMac macaroon.SerializedMacaroon) *walletClient {

	return &walletClient{
		client:   taprpc.NewTaprootAssetsClient(conn),
		timeout:  timeout,
		adminMac: adminMac,
	}
}

func (s *walletClient) GetInfo(ctx context.Context) (*entities.Info, error) {
	rpcCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	rpcCtx = s.adminMac.WithMacaroonAuth(rpcCtx)
	resp, err := s.client.GetInfo(rpcCtx, &taprpc.GetInfoRequest{})
	if err != nil {
		return nil, err
	}

	pubKey, err := hex.DecodeString(resp.LndIdentityPubkey)
	if err != nil {
		return nil, err
	}

	var pubKeyArray [33]byte
	copy(pubKeyArray[:], pubKey)

	return &entities.Info{
		Version:           resp.Version,
		LndVersion:        resp.LndVersion,
		Network:           resp.Network,
		LndIdentityPubkey: pubKeyArray,
		BlockHeight:       resp.BlockHeight,
		NodeAlias:         resp.NodeAlias,
		SyncedToChain:     resp.SyncToChain,
	}, nil
}

func (s *walletClient) ListAssets(ctx context.Context,
	req *entities.ListAssetsRequest) ([]*entities.Asset, error) {

	rpcCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	rpcCtx = s.adminMac.WithMacaroonAuth(rpcCtx)

	rpcReq := &taprpc.ListAssetRequest{}
	if req != nil {
		rpcReq.WithWitness = req.WithWitness
		rpcReq.IncludeSpent = req.IncludeSpent
		rpcReq.IncludeLeased = req.IncludeLeased
		rpcReq.IncludeUnconfirmedMints = req.IncludeUnconfirmedMints
		rpcReq.MinAmount = req.MinAmount
		rpcReq.MaxAmount = req.MaxAmount

		if req.GroupKey != nil {
			rpcReq.GroupKey = req.GroupKey[:]
		}

		if req.AnchorOutpoint != nil {
			anchor := req.AnchorOutpoint
			rpcReq.AnchorOutpoint = &taprpc.OutPoint{
				Txid:        anchor.Txid[:],
				OutputIndex: anchor.Index,
			}
		}

		rpcReq.ScriptKeyType = marshalScriptKeyTypeQuery(
			req.ScriptKeyType,
		)
	}

	resp, err := s.client.ListAssets(rpcCtx, rpcReq)
	if err != nil {
		return nil, err
	}

	assets := make([]*entities.Asset, 0, len(resp.Assets))
	for _, rpcAsset := range resp.Assets {
		asset, err := unmarshalAsset(rpcAsset)
		if err != nil {
			return nil, err
		}

		assets = append(assets, asset)
	}

	return assets, nil
}

func (s *walletClient) ListBalances(ctx context.Context,
	req *entities.ListBalancesRequest) (*entities.ListBalancesResponse,
	error) {

	rpcCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	rpcCtx = s.adminMac.WithMacaroonAuth(rpcCtx)

	rpcReq := marshalListBalancesRequest(req)
	resp, err := s.client.ListBalances(rpcCtx, rpcReq)
	if err != nil {
		return nil, err
	}

	result := &entities.ListBalancesResponse{
		AssetBalances: make(map[string]*entities.AssetBalance),
		AssetGroupBalances: make(
			map[string]*entities.AssetGroupBalance,
		),
		UnconfirmedTransfers: resp.UnconfirmedTransfers,
	}

	for key, rpcBalance := range resp.AssetBalances {
		balance, err := unmarshalAssetBalance(rpcBalance)
		if err != nil {
			return nil, err
		}

		result.AssetBalances[key] = balance
	}

	for key, rpcBalance := range resp.AssetGroupBalances {
		balance, err := unmarshalAssetGroupBalance(rpcBalance)
		if err != nil {
			return nil, err
		}

		result.AssetGroupBalances[key] = balance
	}

	return result, nil
}

func (s *walletClient) ListTransfers(ctx context.Context,
	req *entities.ListTransfersRequest) ([]*entities.AssetTransfer, error) {

	rpcCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	rpcCtx = s.adminMac.WithMacaroonAuth(rpcCtx)

	rpcReq := &taprpc.ListTransfersRequest{}
	if req != nil {
		rpcReq.AnchorTxid = req.AnchorTxid
	}

	resp, err := s.client.ListTransfers(rpcCtx, rpcReq)
	if err != nil {
		return nil, err
	}

	transfers := make([]*entities.AssetTransfer, 0, len(resp.Transfers))
	for _, rpcTransfer := range resp.Transfers {
		transfer, err := unmarshalAssetTransfer(rpcTransfer)
		if err != nil {
			return nil, err
		}

		transfers = append(transfers, transfer)
	}

	return transfers, nil
}

// SendAsset performs a one-shot address-based send.
func (s *walletClient) SendAsset(ctx context.Context,
	req *entities.SendAssetRequest) (*entities.AssetTransfer, error) {

	rpcCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	rpcCtx = s.adminMac.WithMacaroonAuth(rpcCtx)

	rpcReq := marshalSendAssetRequest(req)
	resp, err := s.client.SendAsset(rpcCtx, rpcReq)
	if err != nil {
		return nil, err
	}

	return unmarshalAssetTransfer(resp.Transfer)
}

func unmarshalAsset(rpcAsset *taprpc.Asset) (*entities.Asset, error) {
	if rpcAsset == nil {
		return nil, fmt.Errorf("nil asset")
	}
	if rpcAsset.AssetGenesis == nil {
		return nil, fmt.Errorf("missing asset genesis")
	}
	if len(rpcAsset.ScriptKey) != 33 {
		return nil, fmt.Errorf("invalid script key length: %d",
			len(rpcAsset.ScriptKey))
	}

	genesis, err := unmarshalAssetGenesis(rpcAsset.AssetGenesis)
	if err != nil {
		return nil, err
	}

	var scriptKeyPub [33]byte
	copy(scriptKeyPub[:], rpcAsset.ScriptKey)

	asset := &entities.Asset{
		Version:          uint8(rpcAsset.Version),
		Genesis:          *genesis,
		Amount:           rpcAsset.Amount,
		LockTime:         uint64(rpcAsset.LockTime),
		RelativeLockTime: uint64(rpcAsset.RelativeLockTime),
		ScriptVersion:    uint16(rpcAsset.ScriptVersion),
		ScriptKey: entities.ScriptKey{
			PubKey: scriptKeyPub,
		},
	}

	if rpcAsset.AssetGroup != nil {
		if len(rpcAsset.AssetGroup.RawGroupKey) == 33 {
			var rawKey [33]byte
			copy(rawKey[:], rpcAsset.AssetGroup.RawGroupKey)
			asset.GroupKey = &entities.GroupKey{
				RawKey:        rawKey,
				TapscriptRoot: rpcAsset.AssetGroup.TapscriptRoot,
			}
		}
	}

	return asset, nil
}

func unmarshalAssetGenesis(rpcGenesis *taprpc.GenesisInfo) (
	*entities.AssetGenesis, error) {

	if rpcGenesis == nil {
		return nil, fmt.Errorf("nil asset genesis")
	}
	if rpcGenesis.GenesisPoint == "" {
		return nil, fmt.Errorf("missing genesis point")
	}
	if len(rpcGenesis.AssetId) != 32 {
		return nil, fmt.Errorf("invalid asset ID length: %d",
			len(rpcGenesis.AssetId))
	}
	if len(rpcGenesis.MetaHash) != 0 && len(rpcGenesis.MetaHash) != 32 {
		return nil, fmt.Errorf("invalid meta hash length: %d",
			len(rpcGenesis.MetaHash))
	}

	firstPrevOut, err := entities.NewOutpointFromStr(rpcGenesis.GenesisPoint)
	if err != nil {
		return nil, fmt.Errorf("invalid genesis point: %w",
			err)
	}

	var assetID [32]byte
	copy(assetID[:], rpcGenesis.AssetId)

	var metaHash [32]byte
	if len(rpcGenesis.MetaHash) == 32 {
		copy(metaHash[:], rpcGenesis.MetaHash)
	}

	return &entities.AssetGenesis{
		FirstPrevOut: firstPrevOut,
		Tag:          rpcGenesis.Name,
		MetaHash:     metaHash,
		AssetID:      assetID,
		OutputIndex:  rpcGenesis.OutputIndex,
		Type:         entities.AssetType(rpcGenesis.AssetType),
	}, nil
}

func unmarshalAssetTransfer(rpcTransfer *taprpc.AssetTransfer) (
	*entities.AssetTransfer, error) {

	if rpcTransfer == nil {
		return nil, fmt.Errorf("nil transfer")
	}

	// Unmarshal transfer txid and anchor txid
	var transferTxid [32]byte
	var anchorTxid string
	if len(rpcTransfer.AnchorTxHash) == 32 {
		copy(transferTxid[:], rpcTransfer.AnchorTxHash)

		var h chainhash.Hash
		copy(h[:], rpcTransfer.AnchorTxHash)
		anchorTxid = h.String()
	}

	// Unmarshal outputs
	outputs := make([]entities.TransferOutput, 0, len(rpcTransfer.Outputs))
	for _, out := range rpcTransfer.Outputs {
		output := entities.TransferOutput{
			Amount:    out.Amount,
			ProofBlob: out.NewProofBlob,
		}

		// Copy the script key.
		if len(out.ScriptKey) == 33 {
			copy(output.ScriptKey[:], out.ScriptKey)
		}

		// Copy the outpoint from anchor.
		if out.Anchor != nil {
			op, err := entities.NewOutpointFromStr(out.Anchor.Outpoint)
			if err != nil {
				return nil, fmt.Errorf("invalid anchor outpoint: %w", err)
			}

			output.AnchorOutpoint = op
			output.AnchorValue = out.Anchor.Value
		}

		outputs = append(outputs, output)
	}

	// Unmarshal inputs
	inputs := make([]entities.TransferInput, 0, len(rpcTransfer.Inputs))
	for _, in := range rpcTransfer.Inputs {
		if in == nil {
			return nil, fmt.Errorf("nil transfer input")
		}
		if len(in.AssetId) != 32 {
			return nil, fmt.Errorf(
				"invalid input asset ID length: %d", len(in.AssetId),
			)
		}
		if len(in.ScriptKey) != 33 {
			return nil, fmt.Errorf(
				"invalid input script key length: %d",
				len(in.ScriptKey),
			)
		}

		var assetID [32]byte
		copy(assetID[:], in.AssetId)

		var scriptKey [33]byte
		copy(scriptKey[:], in.ScriptKey)

		anchorPoint, err := entities.NewOutpointFromStr(in.AnchorPoint)
		if err != nil {
			return nil, fmt.Errorf("invalid anchor point: %w", err)
		}

		inputs = append(inputs, entities.TransferInput{
			AnchorPoint: anchorPoint,
			AssetID:     assetID,
			ScriptKey:   scriptKey,
			Amount:      in.Amount,
		})
	}

	var blockHash [32]byte
	var blockHashStr string
	if rpcTransfer.AnchorTxBlockHash != nil {
		blockHashStr = rpcTransfer.AnchorTxBlockHash.HashStr
		if len(rpcTransfer.AnchorTxBlockHash.Hash) == 32 {
			copy(blockHash[:], rpcTransfer.AnchorTxBlockHash.Hash)
		}
	}

	return &entities.AssetTransfer{
		TransferTimestamp:    rpcTransfer.TransferTimestamp,
		TransferTxid:         transferTxid,
		AnchorTxid:           anchorTxid,
		AnchorTxHeightHint:   rpcTransfer.AnchorTxHeightHint,
		AnchorTxChainFees:    rpcTransfer.AnchorTxChainFees,
		Inputs:               inputs,
		Outputs:              outputs,
		AnchorTxBlockHash:    blockHash,
		AnchorTxBlockHashStr: blockHashStr,
		AnchorTxBlockHeight:  rpcTransfer.AnchorTxBlockHeight,
		Label:                rpcTransfer.Label,
		AnchorTx:             rpcTransfer.AnchorTx,
	}, nil
}

// NewAddr creates a new Taproot Asset address for receiving assets.
func (s *walletClient) NewAddr(ctx context.Context,
	req *entities.NewAddressRequest) (*entities.Address, error) {

	rpcCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	rpcCtx = s.adminMac.WithMacaroonAuth(rpcCtx)

	rpcReq := marshalNewAddrRequest(req)
	resp, err := s.client.NewAddr(rpcCtx, rpcReq)
	if err != nil {
		return nil, err
	}

	return unmarshalAddr(resp)
}

// DecodeAddr decodes a bech32m Taproot Asset address string into its
// components.
func (s *walletClient) DecodeAddr(ctx context.Context,
	addr string) (*entities.Address, error) {

	rpcCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	rpcCtx = s.adminMac.WithMacaroonAuth(rpcCtx)

	resp, err := s.client.DecodeAddr(rpcCtx, &taprpc.DecodeAddrRequest{
		Addr: addr,
	})
	if err != nil {
		return nil, err
	}

	return unmarshalAddr(resp)
}

// QueryAddrs returns addresses that were previously created by this tapd
// instance.
func (s *walletClient) QueryAddrs(ctx context.Context,
	query *entities.AddressQuery) ([]*entities.Address, error) {

	rpcCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	rpcCtx = s.adminMac.WithMacaroonAuth(rpcCtx)

	rpcReq := &taprpc.QueryAddrRequest{}
	if query != nil {
		rpcReq.CreatedAfter = query.CreatedAfter
		rpcReq.CreatedBefore = query.CreatedBefore
		rpcReq.Limit = query.Limit
		rpcReq.Offset = query.Offset
	}

	resp, err := s.client.QueryAddrs(rpcCtx, rpcReq)
	if err != nil {
		return nil, err
	}

	addrs := make([]*entities.Address, 0, len(resp.Addrs))
	for _, rpcAddr := range resp.Addrs {
		addr, err := unmarshalAddr(rpcAddr)
		if err != nil {
			return nil, err
		}
		addrs = append(addrs, addr)
	}

	return addrs, nil
}

// AddrReceives returns incoming transfer events for addresses created by this
// tapd instance.
func (s *walletClient) AddrReceives(ctx context.Context,
	query *entities.AddressReceivesQuery) ([]*entities.AddressEvent, error) {

	rpcCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	rpcCtx = s.adminMac.WithMacaroonAuth(rpcCtx)

	rpcReq := &taprpc.AddrReceivesRequest{}
	if query != nil {
		rpcReq.FilterAddr = query.FilterAddr
		rpcReq.FilterStatus = taprpc.AddrEventStatus(query.FilterStatus)
		rpcReq.StartTimestamp = query.StartTimestamp
		rpcReq.EndTimestamp = query.EndTimestamp
		rpcReq.Offset = query.Offset
		rpcReq.Limit = query.Limit
		rpcReq.Direction = taprpc.SortDirection(query.Direction)
	}

	resp, err := s.client.AddrReceives(rpcCtx, rpcReq)
	if err != nil {
		return nil, err
	}

	events := make([]*entities.AddressEvent, 0, len(resp.Events))
	for _, rpcEvent := range resp.Events {
		event, err := unmarshalAddrEvent(rpcEvent)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}

	return events, nil
}

func marshalListBalancesRequest(
	req *entities.ListBalancesRequest) *taprpc.ListBalancesRequest {

	rpcReq := &taprpc.ListBalancesRequest{}
	if req == nil {
		return rpcReq
	}

	rpcReq.IncludeLeased = req.IncludeLeased
	rpcReq.ScriptKeyType = marshalScriptKeyTypeQuery(req.ScriptKeyType)

	switch req.GroupBy {
	case entities.BalanceGroupByAssetID:
		rpcReq.GroupBy = &taprpc.ListBalancesRequest_AssetId{
			AssetId: true,
		}

	case entities.BalanceGroupByGroupKey:
		rpcReq.GroupBy = &taprpc.ListBalancesRequest_GroupKey{
			GroupKey: true,
		}
	}

	if req.AssetFilter != nil {
		rpcReq.AssetFilter = req.AssetFilter[:]
	}

	if req.GroupKeyFilter != nil {
		rpcReq.GroupKeyFilter = req.GroupKeyFilter[:]
	}

	return rpcReq
}

func marshalScriptKeyTypeQuery(
	query *entities.ScriptKeyTypeQuery) *taprpc.ScriptKeyTypeQuery {

	if query == nil {
		return nil
	}

	if query.ExplicitType != nil {
		return &taprpc.ScriptKeyTypeQuery{
			Type: &taprpc.ScriptKeyTypeQuery_ExplicitType{
				ExplicitType: taprpc.ScriptKeyType(*query.ExplicitType),
			},
		}
	}

	if query.AllTypes {
		return &taprpc.ScriptKeyTypeQuery{
			Type: &taprpc.ScriptKeyTypeQuery_AllTypes{
				AllTypes: true,
			},
		}
	}

	return nil
}

func unmarshalAssetBalance(
	rpcBalance *taprpc.AssetBalance) (*entities.AssetBalance, error) {

	if rpcBalance == nil {
		return nil, fmt.Errorf("nil asset balance")
	}
	if rpcBalance.AssetGenesis == nil {
		return nil, fmt.Errorf("missing asset genesis")
	}

	genesis, err := unmarshalAssetGenesis(rpcBalance.AssetGenesis)
	if err != nil {
		return nil, err
	}

	balance := &entities.AssetBalance{
		AssetGenesis: *genesis,
		Balance:      rpcBalance.Balance,
	}

	if len(rpcBalance.GroupKey) != 0 {
		if len(rpcBalance.GroupKey) != 33 {
			return nil, fmt.Errorf("invalid group key length: %d",
				len(rpcBalance.GroupKey))
		}

		var groupKey entities.PubKey
		copy(groupKey[:], rpcBalance.GroupKey)
		balance.GroupKey = &groupKey
	}

	return balance, nil
}

func unmarshalAssetGroupBalance(
	rpcBalance *taprpc.AssetGroupBalance) (*entities.AssetGroupBalance,
	error) {

	if rpcBalance == nil {
		return nil, fmt.Errorf("nil asset group balance")
	}

	balance := &entities.AssetGroupBalance{
		Balance: rpcBalance.Balance,
	}

	if len(rpcBalance.GroupKey) != 0 {
		if len(rpcBalance.GroupKey) != 33 {
			return nil, fmt.Errorf("invalid group key length: %d",
				len(rpcBalance.GroupKey))
		}

		var groupKey entities.PubKey
		copy(groupKey[:], rpcBalance.GroupKey)
		balance.GroupKey = &groupKey
	}

	return balance, nil
}

func marshalSendAssetRequest(
	req *entities.SendAssetRequest) *taprpc.SendAssetRequest {

	if req == nil {
		return &taprpc.SendAssetRequest{}
	}

	rpcReq := &taprpc.SendAssetRequest{
		TapAddrs:                  req.TapAddresses,
		FeeRate:                   req.FeeRate,
		Label:                     req.Label,
		SkipProofCourierPingCheck: req.SkipProofCourierPingCheck,
	}

	if len(req.Recipients) == 0 {
		return rpcReq
	}

	rpcReq.TapAddrs = nil
	rpcReq.AddressesWithAmounts = make(
		[]*taprpc.AddressWithAmount, 0, len(req.Recipients),
	)
	for _, recipient := range req.Recipients {
		rpcReq.AddressesWithAmounts = append(
			rpcReq.AddressesWithAmounts, &taprpc.AddressWithAmount{
				TapAddr: recipient.Address,
				Amount:  recipient.Amount,
			},
		)
	}

	return rpcReq
}

func marshalNewAddrRequest(
	req *entities.NewAddressRequest) *taprpc.NewAddrRequest {

	if req == nil {
		return &taprpc.NewAddrRequest{}
	}

	rpcReq := &taprpc.NewAddrRequest{
		Amt:                       req.Amount,
		TapscriptSibling:          req.TapscriptSibling,
		ProofCourierAddr:          req.ProofCourierAddr,
		SkipProofCourierConnCheck: req.SkipProofCourierConnCheck,
	}

	if req.AssetID != nil {
		rpcReq.AssetId = req.AssetID[:]
	}

	if req.GroupKey != nil {
		rpcReq.GroupKey = req.GroupKey[:]
	}

	if req.ScriptKey != nil {
		rpcReq.ScriptKey = &taprpc.ScriptKey{
			PubKey:   req.ScriptKey.PubKey[:],
			TapTweak: req.ScriptKey.TapTweak,
		}
		if req.ScriptKey.KeyDesc.RawKeyBytes != (entities.PubKey{}) {
			rpcReq.ScriptKey.KeyDesc = &taprpc.KeyDescriptor{
				RawKeyBytes: req.ScriptKey.KeyDesc.RawKeyBytes[:],
				KeyLoc: &taprpc.KeyLocator{
					KeyFamily: int32(req.ScriptKey.KeyDesc.KeyLocator.Family),
					KeyIndex:  int32(req.ScriptKey.KeyDesc.KeyLocator.Index),
				},
			}
		}
	}

	if req.InternalKey != nil {
		rpcReq.InternalKey = &taprpc.KeyDescriptor{
			RawKeyBytes: req.InternalKey.RawKeyBytes[:],
			KeyLoc: &taprpc.KeyLocator{
				KeyFamily: int32(req.InternalKey.KeyLocator.Family),
				KeyIndex:  int32(req.InternalKey.KeyLocator.Index),
			},
		}
	}

	if req.AssetVersion != nil {
		rpcReq.AssetVersion = taprpc.AssetVersion(*req.AssetVersion)
	}

	if req.AddressVersion != nil {
		rpcReq.AddressVersion = taprpc.AddrVersion(*req.AddressVersion)
	}

	return rpcReq
}

func unmarshalAddr(rpcAddr *taprpc.Addr) (*entities.Address, error) {
	if rpcAddr == nil {
		return nil, fmt.Errorf("nil address")
	}

	addr := &entities.Address{
		Encoded:          rpcAddr.Encoded,
		AssetType:        entities.AssetType(rpcAddr.AssetType),
		Amount:           rpcAddr.Amount,
		TapscriptSibling: rpcAddr.TapscriptSibling,
		ProofCourierAddr: rpcAddr.ProofCourierAddr,
		AssetVersion:     entities.AssetVersion(rpcAddr.AssetVersion),
		AddressVersion:   entities.AddressVersion(rpcAddr.AddressVersion),
	}

	// Parse asset ID (may be empty for V2 group addresses).
	if len(rpcAddr.AssetId) == 32 {
		copy(addr.AssetID[:], rpcAddr.AssetId)
	}

	// Parse group key if present.
	if len(rpcAddr.GroupKey) == 33 {
		var gk entities.PubKey
		copy(gk[:], rpcAddr.GroupKey)
		addr.GroupKey = &gk
	}

	// Parse script key (required).
	if len(rpcAddr.ScriptKey) != 33 {
		return nil, fmt.Errorf("invalid script key length: %d",
			len(rpcAddr.ScriptKey))
	}
	copy(addr.ScriptKey[:], rpcAddr.ScriptKey)

	// Parse internal key (required).
	if len(rpcAddr.InternalKey) != 33 {
		return nil, fmt.Errorf("invalid internal key length: %d",
			len(rpcAddr.InternalKey))
	}
	copy(addr.InternalKey[:], rpcAddr.InternalKey)

	// Parse taproot output key (required).
	if len(rpcAddr.TaprootOutputKey) != 32 {
		return nil, fmt.Errorf("invalid taproot output key length: %d",
			len(rpcAddr.TaprootOutputKey))
	}
	copy(addr.TaprootOutputKey[:], rpcAddr.TaprootOutputKey)

	return addr, nil
}

func unmarshalAddrEvent(rpcEvent *taprpc.AddrEvent) (*entities.AddressEvent,
	error) {

	if rpcEvent == nil {
		return nil, fmt.Errorf("nil address event")
	}

	event := &entities.AddressEvent{
		CreationTime:       rpcEvent.CreationTimeUnixSeconds,
		Status:             entities.AddressEventStatus(rpcEvent.Status),
		Outpoint:           rpcEvent.Outpoint,
		UTXOAmountSat:      rpcEvent.UtxoAmtSat,
		TaprootSibling:     rpcEvent.TaprootSibling,
		ConfirmationHeight: rpcEvent.ConfirmationHeight,
		HasProof:           rpcEvent.HasProof,
	}

	// Unmarshal the embedded address if present.
	if rpcEvent.Addr != nil {
		addr, err := unmarshalAddr(rpcEvent.Addr)
		if err != nil {
			return nil, fmt.Errorf("invalid address in event: %w", err)
		}
		event.Address = addr
	}

	return event, nil
}

// ListUtxos lists managed UTXOs with optional filtering.
func (s *walletClient) ListUtxos(ctx context.Context,
	req *entities.ListUtxosRequest) (map[string]*entities.ManagedUtxo,
	error) {

	rpcCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	rpcCtx = s.adminMac.WithMacaroonAuth(rpcCtx)

	rpcReq := &taprpc.ListUtxosRequest{}
	if req != nil {
		rpcReq.IncludeLeased = req.IncludeLeased
		rpcReq.ScriptKeyType = marshalScriptKeyTypeQuery(
			req.ScriptKeyType,
		)
	}

	resp, err := s.client.ListUtxos(rpcCtx, rpcReq)
	if err != nil {
		return nil, err
	}

	result := make(
		map[string]*entities.ManagedUtxo, len(resp.ManagedUtxos),
	)

	for key, rpcUtxo := range resp.ManagedUtxos {
		utxo, err := unmarshalManagedUtxo(rpcUtxo)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal "+
				"utxo %s: %w", key, err)
		}

		result[key] = utxo
	}

	return result, nil
}

// ListGroups lists all known asset groups.
func (s *walletClient) ListGroups(
	ctx context.Context) (map[string]*entities.GroupedAssets, error) {

	rpcCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	rpcCtx = s.adminMac.WithMacaroonAuth(rpcCtx)

	resp, err := s.client.ListGroups(
		rpcCtx, &taprpc.ListGroupsRequest{},
	)
	if err != nil {
		return nil, err
	}

	result := make(
		map[string]*entities.GroupedAssets, len(resp.Groups),
	)

	for key, rpcGroup := range resp.Groups {
		group, err := unmarshalGroupedAssets(rpcGroup)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal "+
				"group %s: %w", key, err)
		}

		result[key] = group
	}

	return result, nil
}

// BurnAsset burns asset units. The confirmation text must be set to
// "assets will be destroyed" for the burn to succeed.
func (s *walletClient) BurnAsset(ctx context.Context,
	req *entities.BurnAssetRequest) (*entities.BurnAssetResponse,
	error) {

	rpcCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	rpcCtx = s.adminMac.WithMacaroonAuth(rpcCtx)

	rpcReq := marshalBurnAssetRequest(req)

	resp, err := s.client.BurnAsset(rpcCtx, rpcReq)
	if err != nil {
		return nil, err
	}

	result := &entities.BurnAssetResponse{}

	if resp.BurnTransfer != nil {
		transfer, err := unmarshalAssetTransfer(resp.BurnTransfer)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal "+
				"burn transfer: %w", err)
		}

		result.BurnTransfer = transfer
	}

	if resp.BurnProof != nil {
		proof, err := unmarshalDecodedProof(resp.BurnProof)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal "+
				"burn proof: %w", err)
		}

		result.BurnProof = proof
	}

	return result, nil
}

// ListBurns lists asset burns with optional filtering.
func (s *walletClient) ListBurns(ctx context.Context,
	req *entities.ListBurnsRequest) ([]*entities.AssetBurn, error) {

	rpcCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	rpcCtx = s.adminMac.WithMacaroonAuth(rpcCtx)

	rpcReq := &taprpc.ListBurnsRequest{}
	if req != nil {
		if req.AssetID != nil {
			rpcReq.AssetId = req.AssetID[:]
		}
		if req.TweakedGroupKey != nil {
			rpcReq.TweakedGroupKey = req.TweakedGroupKey[:]
		}
		if req.AnchorTxid != nil {
			rpcReq.AnchorTxid = req.AnchorTxid[:]
		}
	}

	resp, err := s.client.ListBurns(rpcCtx, rpcReq)
	if err != nil {
		return nil, err
	}

	burns := make([]*entities.AssetBurn, 0, len(resp.Burns))
	for _, rpcBurn := range resp.Burns {
		burn, err := unmarshalAssetBurn(rpcBurn)
		if err != nil {
			return nil, err
		}

		burns = append(burns, burn)
	}

	return burns, nil
}

// FetchAssetMeta fetches the metadata for an asset by ID or meta hash.
func (s *walletClient) FetchAssetMeta(ctx context.Context,
	req *entities.FetchAssetMetaRequest) (*entities.AssetMeta, error) {

	rpcCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	rpcCtx = s.adminMac.WithMacaroonAuth(rpcCtx)

	rpcReq := marshalFetchAssetMetaRequest(req)

	resp, err := s.client.FetchAssetMeta(rpcCtx, rpcReq)
	if err != nil {
		return nil, err
	}

	return unmarshalFetchAssetMetaResponse(resp)
}

// VerifyProof verifies a proof file and returns the decoded last proof
// if valid.
func (s *walletClient) VerifyProof(ctx context.Context,
	rawProofFile []byte) (*entities.VerifyProofResponse, error) {

	rpcCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	rpcCtx = s.adminMac.WithMacaroonAuth(rpcCtx)

	resp, err := s.client.VerifyProof(rpcCtx, &taprpc.ProofFile{
		RawProofFile: rawProofFile,
	})
	if err != nil {
		return nil, err
	}

	result := &entities.VerifyProofResponse{
		Valid: resp.Valid,
	}

	if resp.DecodedProof != nil {
		decoded, err := unmarshalDecodedProof(resp.DecodedProof)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal "+
				"decoded proof: %w", err)
		}

		result.DecodedProof = decoded
	}

	return result, nil
}

// unmarshalManagedUtxo converts an RPC ManagedUtxo to an
// entities.ManagedUtxo.
func unmarshalManagedUtxo(
	rpcUtxo *taprpc.ManagedUtxo) (*entities.ManagedUtxo, error) {

	if rpcUtxo == nil {
		return nil, fmt.Errorf("nil managed utxo")
	}

	outPoint, err := entities.NewOutpointFromStr(rpcUtxo.OutPoint)
	if err != nil {
		return nil, fmt.Errorf("invalid outpoint: %w", err)
	}

	internalKey, err := entities.ParsePubKey(rpcUtxo.InternalKey)
	if err != nil {
		return nil, fmt.Errorf("invalid internal key: %w", err)
	}

	taprootAssetRoot, err := entities.ParseHash(
		rpcUtxo.TaprootAssetRoot,
	)
	if err != nil {
		return nil, fmt.Errorf("invalid taproot asset root: %w",
			err)
	}

	merkleRoot, err := entities.ParseHash(rpcUtxo.MerkleRoot)
	if err != nil {
		return nil, fmt.Errorf("invalid merkle root: %w", err)
	}

	assets := make([]*entities.Asset, 0, len(rpcUtxo.Assets))
	for _, rpcAsset := range rpcUtxo.Assets {
		asset, err := unmarshalAsset(rpcAsset)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal "+
				"asset: %w", err)
		}

		assets = append(assets, asset)
	}

	return &entities.ManagedUtxo{
		OutPoint:         outPoint,
		AmtSat:           rpcUtxo.AmtSat,
		InternalKey:      internalKey,
		TaprootAssetRoot: taprootAssetRoot,
		MerkleRoot:       merkleRoot,
		Assets:           assets,
		LeaseOwner:       rpcUtxo.LeaseOwner,
		LeaseExpiryUnix:  rpcUtxo.LeaseExpiryUnix,
	}, nil
}

// unmarshalGroupedAssets converts an RPC GroupedAssets to an
// entities.GroupedAssets.
func unmarshalGroupedAssets(
	rpcGroup *taprpc.GroupedAssets) (*entities.GroupedAssets, error) {

	if rpcGroup == nil {
		return nil, fmt.Errorf("nil grouped assets")
	}

	assets := make(
		[]*entities.AssetHumanReadable, 0, len(rpcGroup.Assets),
	)

	for _, rpcAsset := range rpcGroup.Assets {
		if rpcAsset == nil {
			return nil, fmt.Errorf("nil asset in group")
		}

		assetID, err := entities.ParseAssetID(rpcAsset.Id)
		if err != nil {
			return nil, fmt.Errorf("invalid asset ID: %w",
				err)
		}

		metaHash, err := entities.ParseHash(rpcAsset.MetaHash)
		if err != nil {
			return nil, fmt.Errorf("invalid meta hash: %w",
				err)
		}

		assets = append(assets, &entities.AssetHumanReadable{
			ID:               assetID,
			Amount:           rpcAsset.Amount,
			LockTime:         rpcAsset.LockTime,
			RelativeLockTime: rpcAsset.RelativeLockTime,
			Tag:              rpcAsset.Tag,
			MetaHash:         metaHash,
			Type:             entities.AssetType(rpcAsset.Type),
			Version:          uint8(rpcAsset.Version),
		})
	}

	return &entities.GroupedAssets{
		Assets: assets,
	}, nil
}

// unmarshalAssetBurn converts an RPC AssetBurn to an entities.AssetBurn.
func unmarshalAssetBurn(
	rpcBurn *taprpc.AssetBurn) (*entities.AssetBurn, error) {

	if rpcBurn == nil {
		return nil, fmt.Errorf("nil asset burn")
	}

	assetID, err := entities.ParseAssetID(rpcBurn.AssetId)
	if err != nil {
		return nil, fmt.Errorf("invalid asset ID: %w", err)
	}

	anchorTxid, err := entities.ParseHash(rpcBurn.AnchorTxid)
	if err != nil {
		return nil, fmt.Errorf("invalid anchor txid: %w", err)
	}

	burn := &entities.AssetBurn{
		Note:       rpcBurn.Note,
		AssetID:    assetID,
		Amount:     rpcBurn.Amount,
		AnchorTxid: anchorTxid,
	}

	if len(rpcBurn.TweakedGroupKey) > 0 {
		groupKey, err := entities.ParsePubKey(
			rpcBurn.TweakedGroupKey,
		)
		if err != nil {
			return nil, fmt.Errorf("invalid tweaked group "+
				"key: %w", err)
		}

		burn.TweakedGroupKey = &groupKey
	}

	return burn, nil
}

// marshalBurnAssetRequest converts a BurnAssetRequest to an RPC request.
func marshalBurnAssetRequest(
	req *entities.BurnAssetRequest) *taprpc.BurnAssetRequest {

	rpcReq := &taprpc.BurnAssetRequest{
		AmountToBurn:     req.AmountToBurn,
		ConfirmationText: req.ConfirmationText,
		Note:             req.Note,
	}

	if req.AssetID != nil {
		rpcReq.Asset = &taprpc.BurnAssetRequest_AssetId{
			AssetId: req.AssetID[:],
		}
	} else if req.AssetIDStr != "" {
		rpcReq.Asset = &taprpc.BurnAssetRequest_AssetIdStr{
			AssetIdStr: req.AssetIDStr,
		}
	}

	return rpcReq
}

// marshalFetchAssetMetaRequest converts a FetchAssetMetaRequest to an
// RPC request.
func marshalFetchAssetMetaRequest(
	req *entities.FetchAssetMetaRequest) *taprpc.FetchAssetMetaRequest {

	rpcReq := &taprpc.FetchAssetMetaRequest{}

	if req.AssetID != nil {
		rpcReq.Asset = &taprpc.FetchAssetMetaRequest_AssetId{
			AssetId: req.AssetID[:],
		}
	} else if req.MetaHash != nil {
		rpcReq.Asset = &taprpc.FetchAssetMetaRequest_MetaHash{
			MetaHash: req.MetaHash[:],
		}
	}

	return rpcReq
}

// unmarshalFetchAssetMetaResponse converts an RPC FetchAssetMetaResponse
// to an entities.AssetMeta.
func unmarshalFetchAssetMetaResponse(
	resp *taprpc.FetchAssetMetaResponse) (*entities.AssetMeta, error) {

	if resp == nil {
		return nil, fmt.Errorf("nil asset meta response")
	}

	metaHash, err := entities.ParseHash(resp.MetaHash)
	if err != nil {
		return nil, fmt.Errorf("invalid meta hash: %w", err)
	}

	unknownOddTypes := make(
		map[uint64][]byte, len(resp.UnknownOddTypes),
	)
	maps.Copy(unknownOddTypes, resp.UnknownOddTypes)

	return &entities.AssetMeta{
		Data:                  resp.Data,
		Type:                  entities.AssetMetaType(resp.Type),
		MetaHash:              metaHash,
		UnknownOddTypes:       unknownOddTypes,
		DecimalDisplay:        resp.DecimalDisplay,
		UniverseCommitments:   resp.UniverseCommitments,
		CanonicalUniverseURLs: resp.CanonicalUniverseUrls,
		DelegationKey:         resp.DelegationKey,
	}, nil
}

// unmarshalDecodedProof converts an RPC DecodedProof to an
// entities.DecodedProof. This is extracted from proof_client.go for
// reuse in BurnAsset and VerifyProof.
func unmarshalDecodedProof(
	rpcProof *taprpc.DecodedProof) (*entities.DecodedProof, error) {

	if rpcProof == nil {
		return nil, fmt.Errorf("nil decoded proof")
	}

	if rpcProof.Asset == nil {
		return nil, fmt.Errorf("nil proof asset")
	}

	assetID, err := entities.ParseAssetID(
		rpcProof.Asset.AssetGenesis.AssetId,
	)
	if err != nil {
		return nil, fmt.Errorf("invalid asset ID: %w", err)
	}

	scriptKey, err := entities.ParseTaprootPubKey(
		rpcProof.Asset.ScriptKey,
	)
	if err != nil {
		return nil, fmt.Errorf("invalid script key: %w", err)
	}

	proof := &entities.DecodedProof{
		ProofAtDepth:   rpcProof.ProofAtDepth,
		NumberOfProofs: rpcProof.NumberOfProofs,
		AssetID:        assetID,
		ScriptKey:      scriptKey,
		Amount:         rpcProof.Asset.Amount,
	}

	if rpcProof.Asset.ChainAnchor != nil {
		op, err := entities.NewOutpointFromStr(
			rpcProof.Asset.ChainAnchor.AnchorOutpoint,
		)
		if err != nil {
			return nil, fmt.Errorf("invalid anchor "+
				"outpoint: %w", err)
		}

		proof.Outpoint = op
	}

	if rpcProof.Asset.AssetGroup != nil &&
		len(rpcProof.Asset.AssetGroup.TweakedGroupKey) > 0 {

		groupKey, err := entities.ParsePubKey(
			rpcProof.Asset.AssetGroup.TweakedGroupKey,
		)
		if err != nil {
			return nil, fmt.Errorf("invalid group key: %w",
				err)
		}

		proof.GroupKey = &groupKey
	}

	return proof, nil
}
