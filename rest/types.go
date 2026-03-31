package rest

// types.go contains the JSON wire types returned by tapd's gRPC-gateway
// REST proxy. These are intermediate structures used only within the rest
// package to unmarshal HTTP responses. They are converted to
// entities.* types before being returned to callers.
//
// Field names match the proto JSON encoding (snake_case).

// jsonGetInfoResponse is the JSON shape of taprpc.GetInfoResponse.
type jsonGetInfoResponse struct {
	Version           string `json:"version"`
	LndVersion        string `json:"lnd_version"`
	Network           string `json:"network"`
	LndIdentityPubkey string `json:"lnd_identity_pubkey"`
	NodeAlias         string `json:"node_alias"`
	BlockHeight       uint32 `json:"block_height"`
	BlockHash         string `json:"block_hash"`
	SyncToChain       bool   `json:"sync_to_chain"`
}

// jsonGenesisInfo is the JSON shape of taprpc.GenesisInfo.
type jsonGenesisInfo struct {
	GenesisPoint string `json:"genesis_point"`
	Name         string `json:"name"`
	MetaHash     string `json:"meta_hash"`
	AssetID      string `json:"asset_id"`
	AssetType    string `json:"asset_type"`
	OutputIndex  uint32 `json:"output_index"`
}

// jsonAssetGroup is the JSON shape of taprpc.AssetGroup.
type jsonAssetGroup struct {
	RawGroupKey     string `json:"raw_group_key"`
	TweakedGroupKey string `json:"tweaked_group_key"`
	TapscriptRoot   string `json:"tapscript_root"`
}

// jsonAsset is the JSON shape of taprpc.Asset.
type jsonAsset struct {
	Version          uint32           `json:"version"`
	AssetGenesis     *jsonGenesisInfo `json:"asset_genesis"`
	Amount           string           `json:"amount"`
	LockTime         int32            `json:"lock_time"`
	RelativeLockTime int32            `json:"relative_lock_time"`
	ScriptVersion    int32            `json:"script_version"`
	ScriptKey        string           `json:"script_key"`
	AssetGroup       *jsonAssetGroup  `json:"asset_group"`
}

// jsonListAssetsResponse is the JSON shape of taprpc.ListAssetResponse.
type jsonListAssetsResponse struct {
	Assets []*jsonAsset `json:"assets"`
}

// jsonAssetBalance is the JSON shape of taprpc.AssetBalance.
type jsonAssetBalance struct {
	AssetGenesis *jsonGenesisInfo `json:"asset_genesis"`
	Balance      string           `json:"balance"`
	GroupKey     string           `json:"group_key"`
}

// jsonAssetGroupBalance is the JSON shape of taprpc.AssetGroupBalance.
type jsonAssetGroupBalance struct {
	GroupKey string `json:"group_key"`
	Balance  string `json:"balance"`
}

// jsonListBalancesResponse is the JSON shape of
// taprpc.ListBalancesResponse.
type jsonListBalancesResponse struct {
	AssetBalances        map[string]*jsonAssetBalance      `json:"asset_balances"`        //nolint:lll
	AssetGroupBalances   map[string]*jsonAssetGroupBalance `json:"asset_group_balances"`  //nolint:lll
	UnconfirmedTransfers uint64                            `json:"unconfirmed_transfers"` //nolint:lll
}

// jsonAnchorInfo is the JSON shape of taprpc.AnchorInfo.
type jsonAnchorInfo struct {
	Outpoint string `json:"anchor_outpoint"`
	Value    int64  `json:"anchor_value"`
}

// jsonTransferOutput is the JSON shape of taprpc.TransferOutput.
type jsonTransferOutput struct {
	Amount       string          `json:"amount"`
	ScriptKey    string          `json:"script_key"`
	NewProofBlob string          `json:"new_proof_blob"`
	Anchor       *jsonAnchorInfo `json:"anchor"`
}

// jsonTransferInput is the JSON shape of taprpc.TransferInput.
type jsonTransferInput struct {
	AnchorPoint string `json:"anchor_point"`
	AssetID     string `json:"asset_id"`
	ScriptKey   string `json:"script_key"`
	Amount      string `json:"amount"`
}

// jsonBlockHash is the JSON shape of taprpc.ProtoHash.
type jsonBlockHash struct {
	Hash    string `json:"hash"`
	HashStr string `json:"hash_str"`
}

// jsonAssetTransfer is the JSON shape of
// taprpc.AssetTransfer.
type jsonAssetTransfer struct {
	TransferTimestamp  int64  `json:"transfer_timestamp"`
	AnchorTxHash       string `json:"anchor_tx_hash"`
	AnchorTxHeightHint uint32 `json:"anchor_tx_height_hint"`
	AnchorTxChainFees  int64  `json:"anchor_tx_chain_fees"`

	Inputs  []*jsonTransferInput  `json:"inputs"`
	Outputs []*jsonTransferOutput `json:"outputs"`

	AnchorTxBlockHash   *jsonBlockHash `json:"anchor_tx_block_hash"`
	AnchorTxBlockHeight uint32         `json:"anchor_tx_block_height"`
	Label               string         `json:"label"`
	AnchorTx            string         `json:"anchor_tx"`
}

// jsonListTransfersResponse is the JSON shape of
// taprpc.ListTransfersResponse.
type jsonListTransfersResponse struct {
	Transfers []*jsonAssetTransfer `json:"transfers"`
}

// jsonSendAssetResponse is the JSON shape of taprpc.SendAssetResponse.
type jsonSendAssetResponse struct {
	Transfer *jsonAssetTransfer `json:"transfer"`
}

// jsonAddr is the JSON shape of taprpc.Addr.
type jsonAddr struct {
	Encoded          string `json:"encoded"`
	AssetID          string `json:"asset_id"`
	AssetType        string `json:"asset_type"`
	Amount           string `json:"amount"`
	GroupKey         string `json:"group_key"`
	ScriptKey        string `json:"script_key"`
	InternalKey      string `json:"internal_key"`
	TapscriptSibling string `json:"tapscript_sibling"`
	TaprootOutputKey string `json:"taproot_output_key"`
	ProofCourierAddr string `json:"proof_courier_addr"`
	AssetVersion     string `json:"asset_version"`
	AddressVersion   string `json:"address_version"`
}

// jsonQueryAddrsResponse is the JSON shape of taprpc.QueryAddrResponse.
type jsonQueryAddrsResponse struct {
	Addrs []*jsonAddr `json:"addrs"`
}

// jsonAddrEvent is the JSON shape of taprpc.AddrEvent.
type jsonAddrEvent struct {
	CreationTimeUnixSeconds string    `json:"creation_time_unix_seconds"`
	Addr                    *jsonAddr `json:"addr"`
	Status                  string    `json:"status"`
	Outpoint                string    `json:"outpoint"`
	UtxoAmtSat              string    `json:"utxo_amt_sat"`
	TaprootSibling          string    `json:"taproot_sibling"`
	ConfirmationHeight      uint32    `json:"confirmation_height"`
	HasProof                bool      `json:"has_proof"`
}

// jsonAddrReceivesResponse is the JSON shape of
// taprpc.AddrReceivesResponse.
type jsonAddrReceivesResponse struct {
	Events []*jsonAddrEvent `json:"events"`
}

// jsonProofFile is the JSON shape of taprpc.ProofFile.
type jsonProofFile struct {
	RawProofFile string `json:"raw_proof_file"`
	GenesisPoint string `json:"genesis_point"`
}

// jsonUnpackProofFileResponse is the JSON shape of
// taprpc.UnpackProofFileResponse.
type jsonUnpackProofFileResponse struct {
	RawProofs []string `json:"raw_proofs"`
}

// jsonChainAnchor is the JSON shape of taprpc.AnchorInfo in proof
// context.
type jsonChainAnchor struct {
	AnchorOutpoint string `json:"anchor_outpoint"`
}

// jsonPrevWitness is the JSON shape of taprpc.PrevWitness.
type jsonPrevWitness struct {
	PrevID *jsonPrevID `json:"prev_id"`
}

// jsonPrevID is the JSON shape of taprpc.PrevInputAsset.
type jsonPrevID struct {
	AnchorPoint string `json:"anchor_point"`
	AssetID     string `json:"asset_id"`
	ScriptKey   string `json:"script_key"`
}

// jsonGenesisReveal is the JSON shape of taprpc.GenesisReveal.
type jsonGenesisReveal struct {
	GenesisBaseReveal any `json:"genesis_base_reveal"`
}

// jsonDecodedAsset is the inner asset of a decoded proof.
type jsonDecodedAsset struct {
	AssetGenesis  *jsonGenesisInfo   `json:"asset_genesis"`
	Amount        string             `json:"amount"`
	ScriptKey     string             `json:"script_key"`
	ChainAnchor   *jsonChainAnchor   `json:"chain_anchor"`
	AssetGroup    *jsonAssetGroup    `json:"asset_group"`
	PrevWitnesses []*jsonPrevWitness `json:"prev_witnesses"`
}

// jsonDecodedProof is the JSON shape of taprpc.DecodedProof.
type jsonDecodedProof struct {
	ProofAtDepth   uint32             `json:"proof_at_depth"`
	NumberOfProofs uint32             `json:"number_of_proofs"`
	Asset          *jsonDecodedAsset  `json:"asset"`
	GenesisReveal  *jsonGenesisReveal `json:"genesis_reveal"`
	AltLeaves      []any              `json:"alt_leaves"`
}

// jsonDecodeProofResponse is the JSON shape of
// taprpc.DecodeProofResponse.
type jsonDecodeProofResponse struct {
	DecodedProof *jsonDecodedProof `json:"decoded_proof"`
}

// jsonRegisteredAsset is the JSON shape of the inner asset in
// RegisterTransferResponse.
type jsonRegisteredAsset struct {
	AssetGenesis *jsonGenesisInfo `json:"asset_genesis"`
	Amount       string           `json:"amount"`
	ScriptKey    string           `json:"script_key"`
	ChainAnchor  *jsonChainAnchor `json:"chain_anchor"`
}

// jsonRegisterTransferResponse is the JSON shape of
// taprpc.RegisterTransferResponse.
type jsonRegisterTransferResponse struct {
	RegisteredAsset *jsonRegisteredAsset `json:"registered_asset"`
}

// jsonScriptKey is the JSON shape of taprpc.ScriptKey.
type jsonScriptKey struct {
	PubKey   string             `json:"pub_key"`
	KeyDesc  *jsonKeyDescriptor `json:"key_desc"`
	TapTweak string             `json:"tap_tweak"`
}

// jsonKeyDescriptor is the JSON shape of taprpc.KeyDescriptor.
type jsonKeyDescriptor struct {
	RawKeyBytes string          `json:"raw_key_bytes"`
	KeyLoc      *jsonKeyLocator `json:"key_loc"`
}

// jsonKeyLocator is the JSON shape of taprpc.KeyLocator.
type jsonKeyLocator struct {
	KeyFamily int32 `json:"key_family"`
	KeyIndex  int32 `json:"key_index"`
}

// jsonNextScriptKeyResponse is the JSON shape of
// assetwalletrpc.NextScriptKeyResponse.
type jsonNextScriptKeyResponse struct {
	ScriptKey *jsonScriptKey `json:"script_key"`
}

// jsonNextInternalKeyResponse is the JSON shape of
// assetwalletrpc.NextInternalKeyResponse.
type jsonNextInternalKeyResponse struct {
	InternalKey *jsonKeyDescriptor `json:"internal_key"`
}

// jsonFundVirtualPsbtResponse is the JSON shape of
// assetwalletrpc.FundVirtualPsbtResponse.
type jsonFundVirtualPsbtResponse struct {
	FundedPsbt        string   `json:"funded_psbt"`
	PassiveAssetPsbts []string `json:"passive_asset_psbts"`
}

// jsonSignVirtualPsbtResponse is the JSON shape of
// assetwalletrpc.SignVirtualPsbtResponse.
type jsonSignVirtualPsbtResponse struct {
	SignedPsbt string `json:"signed_psbt"`
}

// jsonCommitVirtualPsbtsResponse is the JSON shape of
// assetwalletrpc.CommitVirtualPsbtsResponse.
type jsonCommitVirtualPsbtsResponse struct {
	AnchorPsbt        string   `json:"anchor_psbt"`
	VirtualPsbts      []string `json:"virtual_psbts"`
	PassiveAssetPsbts []string `json:"passive_asset_psbts"`
}

// jsonPublishAndLogResponse is the JSON shape of
// assetwalletrpc.PublishAndLogResponse.
type jsonPublishAndLogResponse struct {
	Transfer *jsonPublishTransfer `json:"transfer"`
}

// jsonPublishTransfer is the anchor transfer from PublishAndLog.
type jsonPublishTransfer struct {
	AnchorTx string `json:"anchor_tx"`
}

// jsonInsertProofResponse is the JSON shape of
// universerpc.AssetProofResponse.
type jsonInsertProofResponse struct {
	// We don't need the response fields; just check for errors.
}

// jsonAssetMeta is the JSON shape of taprpc.AssetMeta.
type jsonAssetMeta struct {
	Data     string `json:"data"`
	Type     string `json:"type"`
	MetaHash string `json:"meta_hash"`
}

// jsonMintAsset is the JSON shape of mintrpc.MintAsset.
type jsonMintAsset struct {
	AssetVersion       string `json:"asset_version"`
	AssetType          string `json:"asset_type"`
	Name               string `json:"name"`
	Amount             string `json:"amount"`
	NewGroupedAsset    bool   `json:"new_grouped_asset"`
	GroupedAsset       bool   `json:"grouped_asset"`
	GroupKey           string `json:"group_key"`
	GroupAnchor        string `json:"group_anchor"`
	GroupTapscriptRoot string `json:"group_tapscript_root"`

	ScriptKey        *jsonScriptKey     `json:"script_key"`
	DecimalDisplay   uint32             `json:"decimal_display"`
	AssetMeta        *jsonAssetMeta     `json:"asset_meta"`
	GroupInternalKey *jsonKeyDescriptor `json:"group_internal_key"`

	EnableSupplyCommitments bool `json:"enable_supply_commitments"` //nolint:lll
}

// jsonPendingAsset is the JSON shape of mintrpc.PendingAsset.
type jsonPendingAsset struct {
	AssetVersion       string             `json:"asset_version"`
	AssetType          string             `json:"asset_type"`
	Name               string             `json:"name"`
	Amount             string             `json:"amount"`
	NewGroupedAsset    bool               `json:"new_grouped_asset"`
	GroupKey           string             `json:"group_key"`
	GroupAnchor        string             `json:"group_anchor"`
	GroupTapscriptRoot string             `json:"group_tapscript_root"`
	AssetMeta          *jsonAssetMeta     `json:"asset_meta"`
	ScriptKey          *jsonScriptKey     `json:"script_key"`
	GroupInternalKey   *jsonKeyDescriptor `json:"group_internal_key"`
}

// jsonMintingBatch is the JSON shape of mintrpc.MintingBatch.
type jsonMintingBatch struct {
	BatchKey   string              `json:"batch_key"`
	BatchTxid  string              `json:"batch_txid"`
	State      string              `json:"state"`
	CreatedAt  string              `json:"created_at"`
	HeightHint uint32              `json:"height_hint"`
	BatchPsbt  string              `json:"batch_psbt"`
	Assets     []*jsonPendingAsset `json:"assets"`
}

// jsonMintAssetResponse is the JSON shape of
// mintrpc.MintAssetResponse.
type jsonMintAssetResponse struct {
	PendingBatch *jsonMintingBatch `json:"pending_batch"`
}

// jsonVerboseBatch is the JSON shape of mintrpc.VerboseBatch.
type jsonVerboseBatch struct {
	Batch *jsonMintingBatch `json:"batch"`
}

// jsonFundBatchResponse is the JSON shape of
// mintrpc.FundBatchResponse.
type jsonFundBatchResponse struct {
	Batch *jsonVerboseBatch `json:"batch"`
}

// jsonSealBatchResponse is the JSON shape of
// mintrpc.SealBatchResponse.
type jsonSealBatchResponse struct {
	Batch *jsonMintingBatch `json:"batch"`
}

// jsonFinalizeBatchResponse is the JSON shape of
// mintrpc.FinalizeBatchResponse.
type jsonFinalizeBatchResponse struct {
	Batch *jsonMintingBatch `json:"batch"`
}

// jsonCancelBatchResponse is the JSON shape of
// mintrpc.CancelBatchResponse.
type jsonCancelBatchResponse struct {
	BatchKey string `json:"batch_key"`
}

// jsonListBatchesResponse is the JSON shape of
// mintrpc.ListBatchResponse.
type jsonListBatchesResponse struct {
	Batches []*jsonVerboseBatch `json:"batches"`
}

// --- WalletClient stub types ---

// jsonManagedUtxo is the JSON shape of taprpc.ManagedUtxo.
type jsonManagedUtxo struct {
	Outpoint         string       `json:"out_point"`
	AmtSat           string       `json:"amt_sat"`
	InternalKey      string       `json:"internal_key"`
	TaprootAssetRoot string       `json:"taproot_asset_root"`
	MerkleRoot       string       `json:"merkle_root"`
	Assets           []*jsonAsset `json:"assets"`
}

// jsonListUtxosResponse is the JSON shape of
// taprpc.ListUtxosResponse.
type jsonListUtxosResponse struct {
	ManagedUtxos map[string]*jsonManagedUtxo `json:"managed_utxos"`
}

// jsonGroupedAssets is the JSON shape of taprpc.GroupedAssets.
type jsonGroupedAssets struct {
	Assets []*jsonAsset `json:"assets"`
}

// jsonListGroupsResponse is the JSON shape of
// taprpc.ListGroupsResponse.
type jsonListGroupsResponse struct {
	Groups map[string]*jsonGroupedAssets `json:"groups"`
}

// jsonBurnAssetResponse is the JSON shape of
// taprpc.BurnAssetResponse.
type jsonBurnAssetResponse struct {
	BurnTransfer *jsonAssetTransfer `json:"burn_transfer"`
	BurnProof    *jsonDecodedProof  `json:"burn_proof"`
}

// jsonAssetBurn is the JSON shape of taprpc.AssetBurn.
type jsonAssetBurn struct {
	Note         string `json:"note"`
	AssetID      string `json:"asset_id"`
	GroupKey     string `json:"group_key"`
	Amount       string `json:"amount"`
	TransferTxid string `json:"transfer_txid"`
	AnchorPoint  string `json:"anchor_point"`
}

// jsonListBurnsResponse is the JSON shape of
// taprpc.ListBurnsResponse.
type jsonListBurnsResponse struct {
	Burns []*jsonAssetBurn `json:"burns"`
}

// jsonFetchAssetMetaResponse is the JSON shape of
// taprpc.FetchAssetMetaResponse (same as taprpc.AssetMeta).
type jsonFetchAssetMetaResponse struct {
	Data     string `json:"data"`
	Type     string `json:"type"`
	MetaHash string `json:"meta_hash"`
}

// jsonVerifyProofResponse is the JSON shape of
// taprpc.VerifyProofResponse.
type jsonVerifyProofResponse struct {
	Valid        bool              `json:"valid"`
	DecodedProof *jsonDecodedProof `json:"decoded_proof"`
}

// --- WalletKitClient types ---

// jsonQueryInternalKeyResponse is the JSON shape of
// assetwalletrpc.QueryInternalKeyResponse.
type jsonQueryInternalKeyResponse struct {
	InternalKey *jsonKeyDescriptor `json:"internal_key"`
}

// jsonQueryScriptKeyResponse is the JSON shape of
// assetwalletrpc.QueryScriptKeyResponse.
type jsonQueryScriptKeyResponse struct {
	ScriptKey *jsonScriptKey `json:"script_key"`
}

// jsonOwnershipProof is the JSON shape of
// assetwalletrpc.ProveAssetOwnershipResponse.
type jsonOwnershipProof struct {
	ProofWithWitness string `json:"proof_with_witness"`
}

// jsonVerifyOwnershipResponse is the JSON shape of
// assetwalletrpc.VerifyAssetOwnershipResponse.
type jsonVerifyOwnershipResponse struct {
	ValidProof bool `json:"valid_proof"`
}

// jsonDeclareScriptKeyResponse is the JSON shape of
// assetwalletrpc.DeclareScriptKeyResponse.
type jsonDeclareScriptKeyResponse struct {
	ScriptKey *jsonScriptKey `json:"script_key"`
}
