package grpc

import (
	"context"
	"encoding/hex"
	"fmt"
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

// RawClientWithMacAuth returns a context with the proper macaroon
// authentication, the default RPC timeout, and the raw client.
func (s *walletClient) RawClientWithMacAuth(
	parentCtx context.Context) (context.Context, time.Duration,
	taprpc.TaprootAssetsClient) {

	return s.adminMac.WithMacaroonAuth(parentCtx), s.timeout, s.client
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

		if req.ScriptKeyType != nil && req.ScriptKeyType.AllTypes {
			rpcReq.ScriptKeyType = &taprpc.ScriptKeyTypeQuery{
				Type: &taprpc.ScriptKeyTypeQuery_AllTypes{
					AllTypes: true,
				},
			}
		}
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
