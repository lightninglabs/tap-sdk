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

// unmarshalManagedUtxo converts a JSON managed UTXO to an entity.
func unmarshalManagedUtxo(
	u *jsonManagedUtxo) (*entities.ManagedUtxo, error) {

	if u == nil {
		return nil, fmt.Errorf("nil managed utxo")
	}

	amtSat, err := parseUint64(u.AmtSat)
	if err != nil {
		return nil, fmt.Errorf("invalid amt_sat: %w", err)
	}

	internalKey, err := parseHexBytes(u.InternalKey)
	if err != nil {
		return nil, fmt.Errorf("invalid internal_key: %w",
			err)
	}

	assets := make([]*entities.Asset, 0, len(u.Assets))
	for _, a := range u.Assets {
		asset, err := unmarshalAsset(a)
		if err != nil {
			return nil, fmt.Errorf("unmarshal utxo asset: %w",
				err)
		}
		assets = append(assets, asset)
	}

	taprootRoot, err := parseHexBytes(u.TaprootAssetRoot)
	if err != nil {
		return nil, fmt.Errorf("invalid taproot_root: %w", err)
	}

	merkleRoot, err := parseHexBytes(u.MerkleRoot)
	if err != nil {
		return nil, fmt.Errorf("invalid merkle_root: %w", err)
	}

	outpoint, err := entities.NewOutpointFromStr(u.Outpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid outpoint: %w", err)
	}

	taprootRootHash, _ := entities.ParseHash(taprootRoot)
	merkleRootHash, _ := entities.ParseHash(merkleRoot)

	var pubKey entities.PubKey
	copy(pubKey[:], internalKey)

	return &entities.ManagedUtxo{
		OutPoint:         outpoint,
		AmtSat:           int64(amtSat),
		InternalKey:      pubKey,
		TaprootAssetRoot: taprootRootHash,
		MerkleRoot:       merkleRootHash,
		Assets:           assets,
	}, nil
}

// unmarshalGroupedAssets converts a JSON grouped assets to an entity.
func unmarshalGroupedAssets(
	g *jsonGroupedAssets) (*entities.GroupedAssets, error) {

	if g == nil {
		return nil, fmt.Errorf("nil grouped assets")
	}

	assets := make(
		[]*entities.AssetHumanReadable, 0, len(g.Assets),
	)
	for _, a := range g.Assets {
		asset, err := unmarshalAssetHumanReadable(a)
		if err != nil {
			return nil, fmt.Errorf(
				"unmarshal grouped asset: %w", err)
		}
		assets = append(assets, asset)
	}

	return &entities.GroupedAssets{
		Assets: assets,
	}, nil
}

// unmarshalAssetHumanReadable converts a JSON asset to a simplified
// AssetHumanReadable entity used in group listings.
func unmarshalAssetHumanReadable(
	a *jsonAsset) (*entities.AssetHumanReadable, error) {

	if a == nil {
		return nil, fmt.Errorf("nil asset")
	}

	amount, err := parseUint64(a.Amount)
	if err != nil {
		return nil, fmt.Errorf("invalid amount: %w", err)
	}

	var assetID entities.AssetID
	var metaHash entities.Hash
	var tag string
	var assetType entities.AssetType

	if a.AssetGenesis != nil {
		idBytes, err := parseHexBytes(a.AssetGenesis.AssetID)
		if err == nil {
			copy(assetID[:], idBytes)
		}

		hashBytes, err := parseHexBytes(a.AssetGenesis.MetaHash)
		if err == nil {
			metaHash, _ = entities.ParseHash(hashBytes)
		}

		tag = a.AssetGenesis.Name
		assetType = parseAssetType(a.AssetGenesis.AssetType)
	}

	return &entities.AssetHumanReadable{
		ID:               assetID,
		Amount:           amount,
		LockTime:         a.LockTime,
		RelativeLockTime: a.RelativeLockTime,
		Tag:              tag,
		MetaHash:         metaHash,
		Type:             assetType,
		Version:          uint8(a.Version),
	}, nil
}

// unmarshalBurnAssetResponse converts a JSON burn response to an
// entity.
func unmarshalBurnAssetResponse(
	r *jsonBurnAssetResponse) (*entities.BurnAssetResponse,
	error) {

	if r == nil {
		return nil, fmt.Errorf("nil burn response")
	}

	var transfer *entities.AssetTransfer
	if r.BurnTransfer != nil {
		var err error
		transfer, err = unmarshalAssetTransfer(r.BurnTransfer)
		if err != nil {
			return nil, fmt.Errorf(
				"unmarshal burn transfer: %w", err)
		}
	}

	return &entities.BurnAssetResponse{
		BurnTransfer: transfer,
	}, nil
}

// unmarshalAssetBurn converts a JSON asset burn to an entity.
func unmarshalAssetBurn(
	b *jsonAssetBurn) (*entities.AssetBurn, error) {

	if b == nil {
		return nil, fmt.Errorf("nil asset burn")
	}

	amount, err := parseUint64(b.Amount)
	if err != nil {
		return nil, fmt.Errorf("invalid burn amount: %w", err)
	}

	var assetID entities.AssetID
	idBytes, err := parseHexBytes(b.AssetID)
	if err != nil {
		return nil, fmt.Errorf("invalid burn asset_id: %w",
			err)
	}
	copy(assetID[:], idBytes)

	groupKeyBytes, err := parseHexBytes(b.GroupKey)
	if err != nil {
		return nil, fmt.Errorf("invalid burn group_key: %w",
			err)
	}

	var groupKey *entities.PubKey
	if len(groupKeyBytes) > 0 {
		var pk entities.PubKey
		copy(pk[:], groupKeyBytes)
		groupKey = &pk
	}

	txidBytes, err := parseHexBytes(b.TransferTxid)
	if err != nil {
		return nil, fmt.Errorf("invalid burn txid: %w", err)
	}
	anchorTxid, _ := entities.ParseHash(txidBytes)

	return &entities.AssetBurn{
		Note:            b.Note,
		AssetID:         assetID,
		TweakedGroupKey: groupKey,
		Amount:          amount,
		AnchorTxid:      anchorTxid,
	}, nil
}

// unmarshalFetchAssetMetaResponse converts a JSON fetch meta response
// to an entity.
func unmarshalFetchAssetMetaResponse(
	r *jsonFetchAssetMetaResponse) (*entities.AssetMeta, error) {

	return unmarshalAssetMeta(&jsonAssetMeta{
		Data:     r.Data,
		Type:     r.Type,
		MetaHash: r.MetaHash,
	})
}

// unmarshalVerifyProofResponse converts a JSON verify proof response
// to an entity.
func unmarshalVerifyProofResponse(
	r *jsonVerifyProofResponse) (
	*entities.VerifyProofResponse, error) {

	if r == nil {
		return nil, fmt.Errorf("nil verify proof response")
	}

	return &entities.VerifyProofResponse{
		Valid: r.Valid,
	}, nil
}

// --- Universe unmarshal helpers ---

// parseProofType converts a proto enum string to entities.ProofType.
func parseProofType(s string) entities.ProofType {
	switch s {
	case proofTypeIssuance:
		return entities.ProofTypeIssuance
	case proofTypeTransfer:
		return entities.ProofTypeTransfer
	default:
		return entities.ProofTypeUnspecified
	}
}

// unmarshalMerkleSumNode converts a JSON merkle sum node to an
// entity.
func unmarshalMerkleSumNode(
	n *jsonMerkleSumNode) (*entities.MerkleSumNode, error) {

	if n == nil {
		return nil, nil
	}

	rootHashBytes, err := parseHexBytes(n.RootHash)
	if err != nil {
		return nil, fmt.Errorf("invalid root_hash: %w", err)
	}

	rootHash, err := entities.ParseHash(rootHashBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid root hash: %w", err)
	}

	rootSum, err := parseInt64(n.RootSum)
	if err != nil {
		return nil, fmt.Errorf("invalid root_sum: %w", err)
	}

	return &entities.MerkleSumNode{
		RootHash: rootHash,
		RootSum:  rootSum,
	}, nil
}

// unmarshalUniverseID converts a JSON universe ID to an entity.
func unmarshalUniverseID(
	id *jsonUniverseID) (*entities.UniverseID, error) {

	if id == nil {
		return nil, fmt.Errorf("nil universe ID")
	}

	uniID := &entities.UniverseID{
		ProofType: parseProofType(id.ProofType),
	}

	if id.AssetID != "" {
		assetIDBytes, err := parseHexBytes(id.AssetID)
		if err != nil {
			return nil, fmt.Errorf("invalid asset_id: %w",
				err)
		}

		if len(assetIDBytes) == 32 {
			var assetID entities.AssetID
			copy(assetID[:], assetIDBytes)
			uniID.AssetID = &assetID
		}
	}

	if id.GroupKey != "" {
		groupKeyBytes, err := parseHexBytes(id.GroupKey)
		if err != nil {
			return nil, fmt.Errorf("invalid group_key: %w",
				err)
		}

		if len(groupKeyBytes) == 33 {
			var groupKey entities.PubKey
			copy(groupKey[:], groupKeyBytes)
			uniID.GroupKey = &groupKey
		}
	}

	return uniID, nil
}

// unmarshalUniverseRoot converts a JSON universe root to an entity.
func unmarshalUniverseRoot(
	r *jsonUniverseRoot) (*entities.UniverseRoot, error) {

	if r == nil {
		return nil, nil
	}

	id, err := unmarshalUniverseID(r.ID)
	if err != nil {
		return nil, fmt.Errorf("invalid universe ID: %w", err)
	}

	mssmtRoot, err := unmarshalMerkleSumNode(r.MSSMTRoot)
	if err != nil {
		return nil, fmt.Errorf("invalid mssmt_root: %w", err)
	}

	root := &entities.UniverseRoot{
		ID:        *id,
		MSSMTRoot: mssmtRoot,
		AssetName: r.AssetName,
	}

	if len(r.AmountsByAssetID) > 0 {
		root.AmountsByAssetID = make(map[string]uint64)
		for k, v := range r.AmountsByAssetID {
			amount, err := parseUint64(v)
			if err != nil {
				return nil, fmt.Errorf(
					"invalid amount for %s: %w", k, err,
				)
			}
			root.AmountsByAssetID[k] = amount
		}
	}

	return root, nil
}

// unmarshalAssetLeafKey converts a JSON asset key to an entity.
func unmarshalAssetLeafKey(
	k *jsonAssetKey) (*entities.AssetLeafKey, error) {

	if k == nil {
		return nil, fmt.Errorf("nil asset key")
	}

	if k.Outpoint == nil {
		return nil, fmt.Errorf("nil outpoint")
	}

	outpoint := entities.Outpoint{
		Index: k.Outpoint.OutputIndex,
	}

	txidBytes, err := parseHexBytes(k.Outpoint.Txid)
	if err != nil {
		return nil, fmt.Errorf("invalid txid: %w", err)
	}

	if len(txidBytes) == 32 {
		copy(outpoint.Txid[:], txidBytes)
	}

	scriptKeyBytes, err := parseHexBytes(k.ScriptKey)
	if err != nil {
		return nil, fmt.Errorf("invalid script_key: %w", err)
	}

	if len(scriptKeyBytes) != 33 {
		return nil, fmt.Errorf("invalid script_key length: %d",
			len(scriptKeyBytes))
	}

	var scriptKey entities.PubKey
	copy(scriptKey[:], scriptKeyBytes)

	return &entities.AssetLeafKey{
		Outpoint:  outpoint,
		ScriptKey: scriptKey,
	}, nil
}

// unmarshalAssetLeaf converts a JSON asset leaf to an entity.
func unmarshalAssetLeaf(
	l *jsonAssetLeafResp) (*entities.AssetLeaf, error) {

	if l == nil {
		return nil, fmt.Errorf("nil asset leaf")
	}

	leaf := &entities.AssetLeaf{}

	if l.Asset != nil {
		asset, err := unmarshalAsset(l.Asset)
		if err != nil {
			return nil, fmt.Errorf("invalid asset: %w", err)
		}
		leaf.Asset = asset
	}

	if l.Proof != "" {
		proofBytes, err := parseHexBytes(l.Proof)
		if err != nil {
			return nil, fmt.Errorf("invalid proof: %w", err)
		}
		leaf.Proof = proofBytes
	}

	return leaf, nil
}

// unmarshalAssetProofResponse converts a JSON query proof response to
// an entity.
func unmarshalAssetProofResponse(
	r *jsonQueryProofResponse) (*entities.AssetProofResponse,
	error) {

	if r == nil {
		return nil, fmt.Errorf("nil proof response")
	}

	resp := &entities.AssetProofResponse{}

	if r.Req != nil {
		id, err := unmarshalUniverseID(r.Req.ID)
		if err != nil {
			return nil, fmt.Errorf("invalid req ID: %w",
				err)
		}

		key := entities.UniverseKey{ID: *id}

		if r.Req.LeafKey != nil {
			leafKey, err := unmarshalAssetLeafKey(
				&jsonAssetKey{
					Outpoint: &jsonOutpoint{
						Txid: r.Req.LeafKey.OpStr,
					},
					ScriptKey: r.Req.LeafKey.ScriptKeyBytes, //nolint:lll
				},
			)
			if err != nil {
				return nil, fmt.Errorf(
					"invalid leaf_key: %w", err,
				)
			}
			key.LeafKey = *leafKey
		}

		resp.Key = key
	}

	if r.UniverseRoot != nil {
		root, err := unmarshalUniverseRoot(r.UniverseRoot)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid universe_root: %w", err,
			)
		}
		resp.UniverseRoot = root
	}

	if r.UniverseInclusionProof != "" {
		proof, err := parseHexBytes(r.UniverseInclusionProof)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid universe_inclusion_proof: %w", err,
			)
		}
		resp.UniverseInclusionProof = proof
	}

	if r.AssetLeaf != nil {
		leaf, err := unmarshalAssetLeaf(r.AssetLeaf)
		if err != nil {
			return nil, fmt.Errorf("invalid asset_leaf: %w",
				err)
		}
		resp.AssetLeaf = leaf
	}

	if r.MultiverseRoot != nil {
		root, err := unmarshalMerkleSumNode(r.MultiverseRoot)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid multiverse_root: %w", err,
			)
		}
		resp.MultiverseRoot = root
	}

	if r.MultiverseInclusionProof != "" {
		proof, err := parseHexBytes(r.MultiverseInclusionProof)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid multiverse_inclusion_proof: %w",
				err,
			)
		}
		resp.MultiverseInclusionProof = proof
	}

	return resp, nil
}

// unmarshalUniverseStats converts a JSON universe stats response to an
// entity.
func unmarshalUniverseStats(
	r *jsonUniverseStatsResponse) (*entities.UniverseStats,
	error) {

	if r == nil {
		return nil, fmt.Errorf("nil universe stats")
	}

	numTotalAssets, err := parseInt64(r.NumTotalAssets)
	if err != nil {
		return nil, fmt.Errorf("invalid num_total_assets: %w",
			err)
	}

	numTotalGroups, err := parseInt64(r.NumTotalGroups)
	if err != nil {
		return nil, fmt.Errorf("invalid num_total_groups: %w",
			err)
	}

	numTotalSyncs, err := parseInt64(r.NumTotalSyncs)
	if err != nil {
		return nil, fmt.Errorf("invalid num_total_syncs: %w",
			err)
	}

	numTotalProofs, err := parseInt64(r.NumTotalProofs)
	if err != nil {
		return nil, fmt.Errorf("invalid num_total_proofs: %w",
			err)
	}

	return &entities.UniverseStats{
		NumTotalAssets: numTotalAssets,
		NumTotalGroups: numTotalGroups,
		NumTotalSyncs:  numTotalSyncs,
		NumTotalProofs: numTotalProofs,
	}, nil
}

// unmarshalAssetStatsAsset converts a JSON asset stats asset to an
// entity.
func unmarshalAssetStatsAsset(
	a *jsonAssetStatsAsset) (*entities.AssetStatsAsset, error) {

	if a == nil {
		return nil, nil
	}

	assetIDBytes, err := parseHexBytes(a.AssetID)
	if err != nil {
		return nil, fmt.Errorf("invalid asset_id: %w", err)
	}

	var assetID entities.AssetID
	if len(assetIDBytes) == 32 {
		copy(assetID[:], assetIDBytes)
	}

	totalSupply, err := parseInt64(a.TotalSupply)
	if err != nil {
		return nil, fmt.Errorf("invalid total_supply: %w", err)
	}

	genesisTimestamp, err := parseInt64(a.GenesisTimestamp)
	if err != nil {
		return nil, fmt.Errorf(
			"invalid genesis_timestamp: %w", err,
		)
	}

	return &entities.AssetStatsAsset{
		AssetID:          assetID,
		GenesisPoint:     a.GenesisPoint,
		TotalSupply:      totalSupply,
		AssetName:        a.AssetName,
		AssetType:        parseAssetType(a.AssetType),
		GenesisHeight:    a.GenesisHeight,
		GenesisTimestamp: genesisTimestamp,
		AnchorPoint:      a.AnchorPoint,
		DecimalDisplay:   a.DecimalDisplay,
	}, nil
}

// unmarshalAssetStatsSnapshot converts a JSON asset stats snapshot to
// an entity.
func unmarshalAssetStatsSnapshot(
	s *jsonAssetStatsSnapshot) (*entities.AssetStatsSnapshot,
	error) {

	if s == nil {
		return nil, fmt.Errorf("nil asset stats snapshot")
	}

	snapshot := &entities.AssetStatsSnapshot{}

	if s.GroupKey != "" {
		groupKeyBytes, err := parseHexBytes(s.GroupKey)
		if err != nil {
			return nil, fmt.Errorf("invalid group_key: %w",
				err)
		}

		if len(groupKeyBytes) == 33 {
			var groupKey entities.PubKey
			copy(groupKey[:], groupKeyBytes)
			snapshot.GroupKey = &groupKey
		}
	}

	groupSupply, err := parseInt64(s.GroupSupply)
	if err != nil {
		return nil, fmt.Errorf("invalid group_supply: %w", err)
	}
	snapshot.GroupSupply = groupSupply

	if s.GroupAnchor != nil {
		anchor, err := unmarshalAssetStatsAsset(s.GroupAnchor)
		if err != nil {
			return nil, fmt.Errorf("invalid group_anchor: %w",
				err)
		}
		snapshot.GroupAnchor = anchor
	}

	if s.Asset != nil {
		asset, err := unmarshalAssetStatsAsset(s.Asset)
		if err != nil {
			return nil, fmt.Errorf("invalid asset: %w", err)
		}
		snapshot.Asset = asset
	}

	totalSyncs, err := parseInt64(s.TotalSyncs)
	if err != nil {
		return nil, fmt.Errorf("invalid total_syncs: %w", err)
	}
	snapshot.TotalSyncs = totalSyncs

	totalProofs, err := parseInt64(s.TotalProofs)
	if err != nil {
		return nil, fmt.Errorf("invalid total_proofs: %w", err)
	}
	snapshot.TotalProofs = totalProofs

	return snapshot, nil
}

// unmarshalGroupedUniverseEvents converts a JSON grouped universe
// events to an entity.
func unmarshalGroupedUniverseEvents(
	e *jsonGroupedUniverseEvents) (
	*entities.GroupedUniverseEvents, error) {

	if e == nil {
		return nil, fmt.Errorf("nil grouped universe events")
	}

	syncEvents, err := parseUint64(e.SyncEvents)
	if err != nil {
		return nil, fmt.Errorf("invalid sync_events: %w", err)
	}

	newProofEvents, err := parseUint64(e.NewProofEvents)
	if err != nil {
		return nil, fmt.Errorf(
			"invalid new_proof_events: %w", err,
		)
	}

	return &entities.GroupedUniverseEvents{
		Date:           e.Date,
		SyncEvents:     syncEvents,
		NewProofEvents: newProofEvents,
	}, nil
}

// unmarshalFederationServer converts a JSON federation server to an
// entity.
func unmarshalFederationServer(
	s *jsonUniverseFederationServer) (
	*entities.FederationServer, error) {

	if s == nil {
		return nil, fmt.Errorf("nil federation server")
	}

	return &entities.FederationServer{
		Host: s.Host,
		ID:   s.ID,
	}, nil
}

// unmarshalGlobalFederationSyncConfig converts a JSON global
// federation sync config to an entity.
func unmarshalGlobalFederationSyncConfig(
	c *jsonGlobalFederationSyncConfig) (
	*entities.GlobalFederationSyncConfig, error) {

	if c == nil {
		return nil, fmt.Errorf("nil global federation sync config")
	}

	return &entities.GlobalFederationSyncConfig{
		ProofType:       parseProofType(c.ProofType),
		AllowSyncInsert: c.AllowSyncInsert,
		AllowSyncExport: c.AllowSyncExport,
	}, nil
}

// unmarshalAssetFederationSyncConfig converts a JSON asset federation
// sync config to an entity.
func unmarshalAssetFederationSyncConfig(
	c *jsonAssetFederationSyncConfig) (
	*entities.AssetFederationSyncConfig, error) {

	if c == nil {
		return nil, fmt.Errorf("nil asset federation sync config")
	}

	id, err := unmarshalUniverseID(c.ID)
	if err != nil {
		return nil, fmt.Errorf("invalid ID: %w", err)
	}

	return &entities.AssetFederationSyncConfig{
		ID:              *id,
		AllowSyncInsert: c.AllowSyncInsert,
		AllowSyncExport: c.AllowSyncExport,
	}, nil
}

// unmarshalSyncedUniverse converts a JSON synced universe to an
// entity.
func unmarshalSyncedUniverse(
	s *jsonSyncedUniverse) (*entities.SyncedUniverse, error) {

	if s == nil {
		return nil, fmt.Errorf("nil synced universe")
	}

	synced := &entities.SyncedUniverse{}

	if s.OldAssetRoot != nil {
		oldRoot, err := unmarshalUniverseRoot(s.OldAssetRoot)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid old_asset_root: %w", err,
			)
		}
		synced.OldAssetRoot = oldRoot
	}

	if s.NewAssetRoot != nil {
		newRoot, err := unmarshalUniverseRoot(s.NewAssetRoot)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid new_asset_root: %w", err,
			)
		}
		synced.NewAssetRoot = newRoot
	}

	if len(s.NewAssetLeaves) > 0 {
		leaves := make(
			[]entities.AssetLeaf, 0, len(s.NewAssetLeaves),
		)
		for _, l := range s.NewAssetLeaves {
			leaf, err := unmarshalAssetLeaf(l)
			if err != nil {
				return nil, fmt.Errorf(
					"invalid asset leaf: %w", err,
				)
			}
			leaves = append(leaves, *leaf)
		}
		synced.NewAssetLeaves = leaves
	}

	return synced, nil
}
