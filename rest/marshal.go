package rest

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/lightninglabs/tap-sdk/entities"
)

// parseHexBytes decodes a hex string into a byte slice, returning nil
// for empty input.
func parseHexBytes(s string) ([]byte, error) {
	if s == "" {
		return nil, nil
	}

	return hex.DecodeString(s)
}

// parseBase64Bytes decodes a base64 string (standard encoding with
// padding) into a byte slice.
func parseBase64Bytes(s string) ([]byte, error) {
	if s == "" {
		return nil, nil
	}

	return base64.StdEncoding.DecodeString(s)
}

// parseUint64 parses a JSON string-encoded uint64. The gRPC-gateway
// encodes uint64 values as strings in JSON.
func parseUint64(s string) (uint64, error) {
	if s == "" {
		return 0, nil
	}

	return strconv.ParseUint(s, 10, 64)
}

// parseInt64 parses a JSON string-encoded int64.
func parseInt64(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}

	return strconv.ParseInt(s, 10, 64)
}

// parseAssetType converts a proto enum string to entities.AssetType.
func parseAssetType(s string) entities.AssetType {
	switch s {
	case "COLLECTIBLE":
		return entities.AssetTypeCollectible
	default:
		return entities.AssetTypeNormal
	}
}

// parseAssetVersion converts a proto enum string to
// entities.AssetVersion.
func parseAssetVersion(s string) entities.AssetVersion {
	switch s {
	case "ASSET_VERSION_V1":
		return entities.AssetVersionV1
	default:
		return entities.AssetVersionV0
	}
}

// parseAddressVersion converts a proto enum string to
// entities.AddressVersion.
func parseAddressVersion(s string) entities.AddressVersion {
	switch s {
	case "ADDR_VERSION_V1":
		return entities.AddressVersionV1
	case "ADDR_VERSION_V2":
		return entities.AddressVersionV2
	default:
		return entities.AddressVersionV0
	}
}

// parseAddressEventStatus converts a proto enum string to
// entities.AddressEventStatus.
func parseAddressEventStatus(s string) entities.AddressEventStatus {
	switch s {
	case "ADDR_EVENT_STATUS_TRANSACTION_DETECTED":
		return entities.AddressEventStatusTransactionDetected
	case "ADDR_EVENT_STATUS_TRANSACTION_CONFIRMED":
		return entities.AddressEventStatusTransactionConfirmed
	case "ADDR_EVENT_STATUS_PROOF_RECEIVED":
		return entities.AddressEventStatusProofReceived
	case "ADDR_EVENT_STATUS_COMPLETED":
		return entities.AddressEventStatusCompleted
	default:
		return entities.AddressEventStatusUnknown
	}
}

// parseBatchState converts a proto enum string to entities.BatchState.
func parseBatchState(s string) entities.BatchState {
	switch s {
	case "BATCH_STATE_PENDING":
		return entities.BatchStatePending
	case "BATCH_STATE_FROZEN":
		return entities.BatchStateFrozen
	case "BATCH_STATE_COMMITTED":
		return entities.BatchStateCommitted
	case "BATCH_STATE_BROADCAST":
		return entities.BatchStateBroadcast
	case "BATCH_STATE_CONFIRMED":
		return entities.BatchStateConfirmed
	case "BATCH_STATE_FINALIZED":
		return entities.BatchStateFinalized
	case "BATCH_STATE_SEEDLING_CANCELLED":
		return entities.BatchStateSeedlingCancelled
	case "BATCH_STATE_SPROUT_CANCELLED":
		return entities.BatchStateSproutCancelled
	default:
		return entities.BatchStatePending
	}
}

// parseAssetMetaType converts a proto enum string to
// entities.AssetMetaType.
func parseAssetMetaType(s string) entities.AssetMetaType {
	switch s {
	case "META_TYPE_JSON":
		return entities.AssetMetaTypeJSON
	default:
		return entities.AssetMetaTypeOpaque
	}
}

// unmarshalInfo converts a JSON info response to entities.Info.
func unmarshalInfo(resp *jsonGetInfoResponse) (*entities.Info, error) {
	pubKeyBytes, err := parseHexBytes(resp.LndIdentityPubkey)
	if err != nil {
		return nil, fmt.Errorf("invalid lnd_identity_pubkey: %w", err)
	}

	var pubKey [33]byte
	copy(pubKey[:], pubKeyBytes)

	return &entities.Info{
		Version:           resp.Version,
		LndVersion:        resp.LndVersion,
		Network:           resp.Network,
		LndIdentityPubkey: pubKey,
		NodeAlias:         resp.NodeAlias,
		BlockHeight:       resp.BlockHeight,
		SyncedToChain:     resp.SyncToChain,
	}, nil
}

// unmarshalAssetGenesis converts a JSON genesis to
// entities.AssetGenesis.
func unmarshalAssetGenesis(
	g *jsonGenesisInfo) (*entities.AssetGenesis, error) {

	if g == nil {
		return nil, fmt.Errorf("nil asset genesis")
	}

	if g.GenesisPoint == "" {
		return nil, fmt.Errorf("missing genesis point")
	}

	firstPrevOut, err := entities.NewOutpointFromStr(g.GenesisPoint)
	if err != nil {
		return nil, fmt.Errorf("invalid genesis point: %w", err)
	}

	assetIDBytes, err := parseHexBytes(g.AssetID)
	if err != nil {
		return nil, fmt.Errorf("invalid asset ID: %w", err)
	}

	if len(assetIDBytes) != 32 {
		return nil, fmt.Errorf("invalid asset ID length: %d",
			len(assetIDBytes))
	}

	var assetID [32]byte
	copy(assetID[:], assetIDBytes)

	var metaHash [32]byte
	metaHashBytes, err := parseHexBytes(g.MetaHash)
	if err != nil {
		return nil, fmt.Errorf("invalid meta hash: %w", err)
	}

	if len(metaHashBytes) == 32 {
		copy(metaHash[:], metaHashBytes)
	}

	return &entities.AssetGenesis{
		FirstPrevOut: firstPrevOut,
		Tag:          g.Name,
		MetaHash:     metaHash,
		AssetID:      assetID,
		OutputIndex:  g.OutputIndex,
		Type:         parseAssetType(g.AssetType),
	}, nil
}

// unmarshalAsset converts a JSON asset to entities.Asset.
func unmarshalAsset(a *jsonAsset) (*entities.Asset, error) {
	if a == nil {
		return nil, fmt.Errorf("nil asset")
	}

	genesis, err := unmarshalAssetGenesis(a.AssetGenesis)
	if err != nil {
		return nil, err
	}

	scriptKeyBytes, err := parseHexBytes(a.ScriptKey)
	if err != nil {
		return nil, fmt.Errorf("invalid script key: %w", err)
	}

	if len(scriptKeyBytes) != 33 {
		return nil, fmt.Errorf("invalid script key length: %d",
			len(scriptKeyBytes))
	}

	var scriptKeyPub [33]byte
	copy(scriptKeyPub[:], scriptKeyBytes)

	amount, err := parseUint64(a.Amount)
	if err != nil {
		return nil, fmt.Errorf("invalid amount: %w", err)
	}

	asset := &entities.Asset{
		Version:          uint8(a.Version),
		Genesis:          *genesis,
		Amount:           amount,
		LockTime:         uint64(a.LockTime),
		RelativeLockTime: uint64(a.RelativeLockTime),
		ScriptVersion:    uint16(a.ScriptVersion),
		ScriptKey: entities.ScriptKey{
			PubKey: scriptKeyPub,
		},
	}

	if a.AssetGroup != nil && a.AssetGroup.RawGroupKey != "" {
		rawKeyBytes, err := parseHexBytes(a.AssetGroup.RawGroupKey)
		if err != nil {
			return nil, fmt.Errorf("invalid raw group key: %w",
				err)
		}

		if len(rawKeyBytes) == 33 {
			var rawKey [33]byte
			copy(rawKey[:], rawKeyBytes)

			tapscriptRoot, err := parseHexBytes(
				a.AssetGroup.TapscriptRoot,
			)
			if err != nil {
				return nil, fmt.Errorf(
					"invalid tapscript root: %w", err,
				)
			}

			asset.GroupKey = &entities.GroupKey{
				RawKey:        rawKey,
				TapscriptRoot: tapscriptRoot,
			}
		}
	}

	return asset, nil
}

// unmarshalAddr converts a JSON addr to entities.Address.
func unmarshalAddr(a *jsonAddr) (*entities.Address, error) {
	if a == nil {
		return nil, fmt.Errorf("nil address")
	}

	scriptKeyBytes, err := parseHexBytes(a.ScriptKey)
	if err != nil {
		return nil, fmt.Errorf("invalid script key: %w", err)
	}

	if len(scriptKeyBytes) != 33 {
		return nil, fmt.Errorf("invalid script key length: %d",
			len(scriptKeyBytes))
	}

	internalKeyBytes, err := parseHexBytes(a.InternalKey)
	if err != nil {
		return nil, fmt.Errorf("invalid internal key: %w", err)
	}

	if len(internalKeyBytes) != 33 {
		return nil, fmt.Errorf("invalid internal key length: %d",
			len(internalKeyBytes))
	}

	taprootOutputKeyBytes, err := parseHexBytes(a.TaprootOutputKey)
	if err != nil {
		return nil, fmt.Errorf("invalid taproot output key: %w", err)
	}

	if len(taprootOutputKeyBytes) != 32 {
		return nil, fmt.Errorf(
			"invalid taproot output key length: %d",
			len(taprootOutputKeyBytes),
		)
	}

	addr := &entities.Address{
		Encoded:          a.Encoded,
		AssetType:        parseAssetType(a.AssetType),
		ProofCourierAddr: a.ProofCourierAddr,
		AssetVersion:     parseAssetVersion(a.AssetVersion),
		AddressVersion:   parseAddressVersion(a.AddressVersion),
	}

	amount, err := parseUint64(a.Amount)
	if err != nil {
		return nil, fmt.Errorf("invalid amount: %w", err)
	}

	addr.Amount = amount

	copy(addr.ScriptKey[:], scriptKeyBytes)
	copy(addr.InternalKey[:], internalKeyBytes)
	copy(addr.TaprootOutputKey[:], taprootOutputKeyBytes)

	assetIDBytes, err := parseHexBytes(a.AssetID)
	if err != nil {
		return nil, fmt.Errorf("invalid asset ID: %w", err)
	}

	if len(assetIDBytes) == 32 {
		copy(addr.AssetID[:], assetIDBytes)
	}

	groupKeyBytes, err := parseHexBytes(a.GroupKey)
	if err != nil {
		return nil, fmt.Errorf("invalid group key: %w", err)
	}

	if len(groupKeyBytes) == 33 {
		var gk entities.PubKey
		copy(gk[:], groupKeyBytes)
		addr.GroupKey = &gk
	}

	tapscriptSibling, err := parseHexBytes(a.TapscriptSibling)
	if err != nil {
		return nil, fmt.Errorf("invalid tapscript sibling: %w", err)
	}

	addr.TapscriptSibling = tapscriptSibling

	return addr, nil
}

// unmarshalAssetTransfer converts a JSON transfer to
// entities.AssetTransfer.
func unmarshalAssetTransfer(
	t *jsonAssetTransfer) (*entities.AssetTransfer, error) {

	if t == nil {
		return nil, fmt.Errorf("nil transfer")
	}

	var transferTxid [32]byte
	var anchorTxid string

	txHashBytes, err := parseHexBytes(t.AnchorTxHash)
	if err != nil {
		return nil, fmt.Errorf("invalid anchor_tx_hash: %w", err)
	}

	if len(txHashBytes) == 32 {
		copy(transferTxid[:], txHashBytes)

		var h chainhash.Hash
		copy(h[:], txHashBytes)
		anchorTxid = h.String()
	}

	outputs := make([]entities.TransferOutput, 0, len(t.Outputs))
	for _, out := range t.Outputs {
		amount, err := parseUint64(out.Amount)
		if err != nil {
			return nil, fmt.Errorf("invalid output amount: %w",
				err)
		}

		proofBlob, err := parseBase64Bytes(out.NewProofBlob)
		if err != nil {
			return nil, fmt.Errorf("invalid proof blob: %w", err)
		}

		output := entities.TransferOutput{
			Amount:    amount,
			ProofBlob: proofBlob,
		}

		scriptKeyBytes, err := parseHexBytes(out.ScriptKey)
		if err != nil {
			return nil, fmt.Errorf("invalid output script key: "+
				"%w", err)
		}

		if len(scriptKeyBytes) == 33 {
			copy(output.ScriptKey[:], scriptKeyBytes)
		}

		if out.Anchor != nil {
			op, err := entities.NewOutpointFromStr(
				out.Anchor.Outpoint,
			)
			if err != nil {
				return nil, fmt.Errorf("invalid anchor "+
					"outpoint: %w", err)
			}

			output.AnchorOutpoint = op
			output.AnchorValue = out.Anchor.Value
		}

		outputs = append(outputs, output)
	}

	inputs := make([]entities.TransferInput, 0, len(t.Inputs))
	for _, in := range t.Inputs {
		if in == nil {
			return nil, fmt.Errorf("nil transfer input")
		}

		assetIDBytes, err := parseHexBytes(in.AssetID)
		if err != nil {
			return nil, fmt.Errorf("invalid input asset ID: %w",
				err)
		}

		if len(assetIDBytes) != 32 {
			return nil, fmt.Errorf(
				"invalid input asset ID length: %d",
				len(assetIDBytes),
			)
		}

		scriptKeyBytes, err := parseHexBytes(in.ScriptKey)
		if err != nil {
			return nil, fmt.Errorf("invalid input script key: "+
				"%w", err)
		}

		if len(scriptKeyBytes) != 33 {
			return nil, fmt.Errorf(
				"invalid input script key length: %d",
				len(scriptKeyBytes),
			)
		}

		amount, err := parseUint64(in.Amount)
		if err != nil {
			return nil, fmt.Errorf("invalid input amount: %w",
				err)
		}

		anchorPoint, err := entities.NewOutpointFromStr(
			in.AnchorPoint,
		)
		if err != nil {
			return nil, fmt.Errorf("invalid anchor point: %w",
				err)
		}

		var assetID [32]byte
		copy(assetID[:], assetIDBytes)

		var scriptKey [33]byte
		copy(scriptKey[:], scriptKeyBytes)

		inputs = append(inputs, entities.TransferInput{
			AnchorPoint: anchorPoint,
			AssetID:     assetID,
			ScriptKey:   scriptKey,
			Amount:      amount,
		})
	}

	var blockHash [32]byte
	var blockHashStr string

	if t.AnchorTxBlockHash != nil {
		blockHashStr = t.AnchorTxBlockHash.HashStr

		blockHashBytes, err := parseHexBytes(
			t.AnchorTxBlockHash.Hash,
		)
		if err != nil {
			return nil, fmt.Errorf("invalid block hash: %w", err)
		}

		if len(blockHashBytes) == 32 {
			copy(blockHash[:], blockHashBytes)
		}
	}

	anchorTxBytes, err := parseBase64Bytes(t.AnchorTx)
	if err != nil {
		return nil, fmt.Errorf("invalid anchor_tx: %w", err)
	}

	return &entities.AssetTransfer{
		TransferTimestamp:    t.TransferTimestamp,
		TransferTxid:         transferTxid,
		AnchorTxid:           anchorTxid,
		AnchorTxHeightHint:   t.AnchorTxHeightHint,
		AnchorTxChainFees:    t.AnchorTxChainFees,
		Inputs:               inputs,
		Outputs:              outputs,
		AnchorTxBlockHash:    blockHash,
		AnchorTxBlockHashStr: blockHashStr,
		AnchorTxBlockHeight:  t.AnchorTxBlockHeight,
		Label:                t.Label,
		AnchorTx:             anchorTxBytes,
	}, nil
}

// unmarshalAssetBalance converts a JSON balance to
// entities.AssetBalance.
func unmarshalAssetBalance(
	b *jsonAssetBalance) (*entities.AssetBalance, error) {

	if b == nil {
		return nil, fmt.Errorf("nil asset balance")
	}

	genesis, err := unmarshalAssetGenesis(b.AssetGenesis)
	if err != nil {
		return nil, err
	}

	balance, err := parseUint64(b.Balance)
	if err != nil {
		return nil, fmt.Errorf("invalid balance: %w", err)
	}

	result := &entities.AssetBalance{
		AssetGenesis: *genesis,
		Balance:      balance,
	}

	groupKeyBytes, err := parseHexBytes(b.GroupKey)
	if err != nil {
		return nil, fmt.Errorf("invalid group key: %w", err)
	}

	if len(groupKeyBytes) == 33 {
		var gk entities.PubKey
		copy(gk[:], groupKeyBytes)
		result.GroupKey = &gk
	}

	return result, nil
}

// unmarshalAssetGroupBalance converts a JSON group balance to
// entities.AssetGroupBalance.
func unmarshalAssetGroupBalance(
	b *jsonAssetGroupBalance) (*entities.AssetGroupBalance, error) {

	if b == nil {
		return nil, fmt.Errorf("nil asset group balance")
	}

	balance, err := parseUint64(b.Balance)
	if err != nil {
		return nil, fmt.Errorf("invalid balance: %w", err)
	}

	result := &entities.AssetGroupBalance{Balance: balance}

	groupKeyBytes, err := parseHexBytes(b.GroupKey)
	if err != nil {
		return nil, fmt.Errorf("invalid group key: %w", err)
	}

	if len(groupKeyBytes) == 33 {
		var gk entities.PubKey
		copy(gk[:], groupKeyBytes)
		result.GroupKey = &gk
	}

	return result, nil
}

// unmarshalAddrEvent converts a JSON addr event to
// entities.AddressEvent.
func unmarshalAddrEvent(
	e *jsonAddrEvent) (*entities.AddressEvent, error) {

	if e == nil {
		return nil, fmt.Errorf("nil address event")
	}

	creationTime, err := parseUint64(e.CreationTimeUnixSeconds)
	if err != nil {
		return nil, fmt.Errorf("invalid creation time: %w", err)
	}

	utxoAmtSat, err := parseUint64(e.UtxoAmtSat)
	if err != nil {
		return nil, fmt.Errorf("invalid utxo amount: %w", err)
	}

	event := &entities.AddressEvent{
		CreationTime:       creationTime,
		Status:             parseAddressEventStatus(e.Status),
		Outpoint:           e.Outpoint,
		UTXOAmountSat:      utxoAmtSat,
		ConfirmationHeight: e.ConfirmationHeight,
		HasProof:           e.HasProof,
	}

	taprootSibling, err := parseHexBytes(e.TaprootSibling)
	if err != nil {
		return nil, fmt.Errorf("invalid taproot sibling: %w", err)
	}

	event.TaprootSibling = taprootSibling

	if e.Addr != nil {
		addr, err := unmarshalAddr(e.Addr)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid address in event: %w", err,
			)
		}

		event.Address = addr
	}

	return event, nil
}

// unmarshalScriptKey converts a JSON script key to
// entities.ScriptKey.
func unmarshalScriptKey(k *jsonScriptKey) (*entities.ScriptKey, error) {
	if k == nil {
		return nil, fmt.Errorf("nil script key")
	}

	pubKeyBytes, err := parseHexBytes(k.PubKey)
	if err != nil {
		return nil, fmt.Errorf("invalid script key pub_key: %w", err)
	}

	pubKey, err := entities.ParseTaprootPubKey(pubKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid script key public key: %w",
			err)
	}

	tapTweak, err := parseHexBytes(k.TapTweak)
	if err != nil {
		return nil, fmt.Errorf("invalid tap_tweak: %w", err)
	}

	scriptKey := &entities.ScriptKey{
		PubKey:   pubKey,
		TapTweak: tapTweak,
	}

	if k.KeyDesc != nil {
		keyDesc, err := unmarshalKeyDescriptor(k.KeyDesc)
		if err != nil {
			return nil, err
		}

		scriptKey.KeyDesc = *keyDesc
	}

	return scriptKey, nil
}

// unmarshalKeyDescriptor converts a JSON key descriptor to
// entities.KeyDescriptor.
func unmarshalKeyDescriptor(
	d *jsonKeyDescriptor) (*entities.KeyDescriptor, error) {

	if d == nil {
		return nil, fmt.Errorf("nil key descriptor")
	}

	rawKeyBytes, err := parseHexBytes(d.RawKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid raw_key_bytes: %w", err)
	}

	parsedKey, err := entities.ParsePubKey(rawKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid raw key bytes: %w", err)
	}

	keyDesc := &entities.KeyDescriptor{RawKeyBytes: parsedKey}

	if d.KeyLoc != nil {
		keyDesc.KeyLocator = entities.KeyLocator{
			Family: uint32(d.KeyLoc.KeyFamily),
			Index:  uint32(d.KeyLoc.KeyIndex),
		}
	}

	return keyDesc, nil
}

// unmarshalMintingBatch converts a JSON minting batch to
// entities.MintingBatch.
func unmarshalMintingBatch(
	b *jsonMintingBatch) (*entities.MintingBatch, error) {

	if b == nil {
		return nil, fmt.Errorf("nil minting batch")
	}

	createdAt, err := parseInt64(b.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("invalid created_at: %w", err)
	}

	batchPsbt, err := parseBase64Bytes(b.BatchPsbt)
	if err != nil {
		return nil, fmt.Errorf("invalid batch_psbt: %w", err)
	}

	batch := &entities.MintingBatch{
		BatchTxid:  b.BatchTxid,
		State:      parseBatchState(b.State),
		CreatedAt:  createdAt,
		HeightHint: b.HeightHint,
		BatchPSBT:  batchPsbt,
	}

	if b.BatchKey != "" {
		batchKeyBytes, err := parseHexBytes(b.BatchKey)
		if err != nil {
			return nil, fmt.Errorf("invalid batch_key: %w", err)
		}

		batchKey, err := entities.ParsePubKey(batchKeyBytes)
		if err != nil {
			return nil, fmt.Errorf("invalid batch key: %w", err)
		}

		batch.BatchKey = batchKey
	}

	batch.Assets = make(
		[]entities.PendingMintAsset, 0, len(b.Assets),
	)
	for _, rpcAsset := range b.Assets {
		asset, err := unmarshalPendingMintAsset(rpcAsset)
		if err != nil {
			return nil, err
		}

		batch.Assets = append(batch.Assets, *asset)
	}

	return batch, nil
}

// unmarshalPendingMintAsset converts a JSON pending asset to
// entities.PendingMintAsset.
func unmarshalPendingMintAsset(
	a *jsonPendingAsset) (*entities.PendingMintAsset, error) {

	if a == nil {
		return nil, fmt.Errorf("nil pending mint asset")
	}

	amount, err := parseUint64(a.Amount)
	if err != nil {
		return nil, fmt.Errorf("invalid amount: %w", err)
	}

	tapscriptRoot, err := parseHexBytes(a.GroupTapscriptRoot)
	if err != nil {
		return nil, fmt.Errorf("invalid group tapscript root: %w",
			err)
	}

	asset := &entities.PendingMintAsset{
		AssetVersion:       parseAssetVersion(a.AssetVersion),
		AssetType:          parseAssetType(a.AssetType),
		Name:               a.Name,
		Amount:             amount,
		NewGroupedAsset:    a.NewGroupedAsset,
		GroupAnchor:        a.GroupAnchor,
		GroupTapscriptRoot: tapscriptRoot,
	}

	if a.GroupKey != "" {
		groupKeyBytes, err := parseHexBytes(a.GroupKey)
		if err != nil {
			return nil, fmt.Errorf("invalid group key: %w", err)
		}

		groupKey, err := entities.ParsePubKey(groupKeyBytes)
		if err != nil {
			return nil, fmt.Errorf("invalid group key: %w", err)
		}

		asset.GroupKey = &groupKey
	}

	if a.GroupInternalKey != nil {
		groupInternalKey, err := unmarshalKeyDescriptor(
			a.GroupInternalKey,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid group internal key: %w", err,
			)
		}

		asset.GroupInternalKey = groupInternalKey
	}

	if a.ScriptKey != nil {
		scriptKey, err := unmarshalScriptKey(a.ScriptKey)
		if err != nil {
			return nil, fmt.Errorf("invalid script key: %w", err)
		}

		asset.ScriptKey = scriptKey
	}

	if a.AssetMeta != nil {
		meta, err := unmarshalAssetMeta(a.AssetMeta)
		if err != nil {
			return nil, err
		}

		asset.AssetMeta = meta
	}

	return asset, nil
}

// unmarshalAssetMeta converts a JSON asset meta to
// entities.AssetMeta.
func unmarshalAssetMeta(
	m *jsonAssetMeta) (*entities.AssetMeta, error) {

	if m == nil {
		return nil, fmt.Errorf("nil asset meta")
	}

	data, err := parseBase64Bytes(m.Data)
	if err != nil {
		return nil, fmt.Errorf("invalid meta data: %w", err)
	}

	metaHashBytes, err := parseHexBytes(m.MetaHash)
	if err != nil {
		return nil, fmt.Errorf("invalid meta hash: %w", err)
	}

	metaHash, err := entities.ParseHash(metaHashBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid asset meta hash: %w", err)
	}

	return &entities.AssetMeta{
		Data:     data,
		Type:     parseAssetMetaType(m.Type),
		MetaHash: metaHash,
	}, nil
}
