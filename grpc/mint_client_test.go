package grpc

import (
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/lightninglabs/taproot-assets/taprpc"
	"github.com/lightninglabs/taproot-assets/taprpc/mintrpc"
	"github.com/stretchr/testify/require"
)

func TestMarshalMintAssetRequest(t *testing.T) {
	assetMetaHash := func() entities.Hash {
		var hash entities.Hash
		copy(hash[:], testAssetID)
		return hash
	}()

	tests := []struct {
		name     string
		req      *entities.MintAssetRequest
		validate func(*testing.T, *mintrpc.MintAssetRequest)
	}{
		{
			name: "nil request",
			req:  nil,
			validate: func(t *testing.T, rpcReq *mintrpc.MintAssetRequest) {
				require.NotNil(t, rpcReq)
				require.Nil(t, rpcReq.Asset)
				require.False(t, rpcReq.ShortResponse)
			},
		},
		{
			name: "full request",
			req: &entities.MintAssetRequest{
				ShortResponse: true,
				Asset: &entities.MintAsset{
					PendingMintAsset: entities.PendingMintAsset{
						AssetVersion:    entities.AssetVersionV1,
						AssetType:       entities.AssetTypeNormal,
						Name:            "usd-test",
						Amount:          42,
						NewGroupedAsset: true,
						AssetMeta: &entities.AssetMeta{
							Data:     []byte(`{"ticker":"USDt"}`),
							Type:     entities.AssetMetaTypeJSON,
							MetaHash: assetMetaHash,
						},
						GroupKey: func() *entities.PubKey {
							var key entities.PubKey
							copy(key[:], testPubKey)
							return &key
						}(),
						GroupAnchor: "anchor-asset",
						GroupInternalKey: &entities.KeyDescriptor{
							RawKeyBytes: func() entities.PubKey {
								var key entities.PubKey
								copy(key[:], testPubKey)
								return key
							}(),
							KeyLocator: entities.KeyLocator{
								Family: 7,
								Index:  9,
							},
						},
						GroupTapscriptRoot: []byte{0xaa, 0xbb},
						ScriptKey: &entities.ScriptKey{
							PubKey: func() entities.PubKey {
								var key entities.PubKey
								copy(key[:], testPubKey)
								return key
							}(),
							TapTweak: []byte{0x01, 0x02},
						},
					},
					GroupedAsset:   true,
					DecimalDisplay: 2,
					ExternalGroupKey: &entities.ExternalKey{
						XPub:           "xpub-test",
						DerivationPath: "m/86'/0'/0'/0/7",
						MasterFingerprint: [4]byte{
							0xde, 0xad, 0xbe, 0xef,
						},
					},
					EnableSupplyCommitments: true,
				},
			},
			validate: func(t *testing.T, rpcReq *mintrpc.MintAssetRequest) {
				require.True(t, rpcReq.ShortResponse)
				require.NotNil(t, rpcReq.Asset)
				require.Equal(
					t,
					taprpc.AssetVersion_ASSET_VERSION_V1,
					rpcReq.Asset.AssetVersion,
				)
				require.Equal(t, taprpc.AssetType_NORMAL,
					rpcReq.Asset.AssetType)
				require.Equal(t, "usd-test", rpcReq.Asset.Name)
				require.Equal(t, uint64(42), rpcReq.Asset.Amount)
				require.True(t, rpcReq.Asset.NewGroupedAsset)
				require.True(t, rpcReq.Asset.GroupedAsset)
				require.Equal(t, testPubKey, rpcReq.Asset.GroupKey)
				require.Equal(t, "anchor-asset",
					rpcReq.Asset.GroupAnchor)
				require.Equal(
					t,
					[]byte{0xaa, 0xbb},
					rpcReq.Asset.GroupTapscriptRoot,
				)
				require.NotNil(t, rpcReq.Asset.AssetMeta)
				require.Equal(
					t,
					taprpc.AssetMetaType_META_TYPE_JSON,
					rpcReq.Asset.AssetMeta.Type,
				)
				require.Equal(t, assetMetaHash[:],
					rpcReq.Asset.AssetMeta.MetaHash)
				require.NotNil(t, rpcReq.Asset.GroupInternalKey)
				require.Equal(t, int32(7),
					rpcReq.Asset.GroupInternalKey.KeyLoc.KeyFamily)
				require.NotNil(t, rpcReq.Asset.ScriptKey)
				require.Equal(t, testPubKey,
					rpcReq.Asset.ScriptKey.PubKey)
				require.Equal(t, uint32(2),
					rpcReq.Asset.DecimalDisplay)
				require.NotNil(t, rpcReq.Asset.ExternalGroupKey)
				require.Equal(t, "xpub-test",
					rpcReq.Asset.ExternalGroupKey.Xpub)
				require.True(t,
					rpcReq.Asset.EnableSupplyCommitments)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rpcReq := marshalMintAssetRequest(tc.req)
			require.NotNil(t, rpcReq)
			tc.validate(t, rpcReq)
		})
	}
}

func TestMarshalFinalizeBatchRequest(t *testing.T) {
	leftHash := func() entities.Hash {
		var hash entities.Hash
		copy(hash[:], testAssetID)
		return hash
	}()
	rightHash := func() entities.Hash {
		var hash entities.Hash
		copy(hash[:], testXOnlyPubKey)
		return hash
	}()

	tests := []struct {
		name     string
		req      *entities.FinalizeBatchRequest
		wantErr  string
		validate func(*testing.T, *mintrpc.FinalizeBatchRequest)
	}{
		{
			name: "nil request",
			req:  nil,
			validate: func(t *testing.T,
				rpcReq *mintrpc.FinalizeBatchRequest) {

				require.NotNil(t, rpcReq)
				require.False(t, rpcReq.ShortResponse)
				require.Zero(t, rpcReq.FeeRate)
			},
		},
		{
			name: "full tree sibling",
			req: &entities.FinalizeBatchRequest{
				ShortResponse: true,
				FeeRate:       321,
				BatchSibling: &entities.BatchSibling{
					FullTree: &entities.TapscriptFullTree{
						Leaves: []entities.TapLeaf{
							{Script: []byte{0x51}},
							{Script: []byte{0x52}},
						},
					},
				},
			},
			validate: func(t *testing.T,
				rpcReq *mintrpc.FinalizeBatchRequest) {

				require.True(t, rpcReq.ShortResponse)
				require.Equal(t, uint32(321), rpcReq.FeeRate)
				require.NotNil(t, rpcReq.GetFullTree())
				require.Len(t, rpcReq.GetFullTree().AllLeaves, 2)
				require.Equal(t, []byte{0x51},
					rpcReq.GetFullTree().AllLeaves[0].Script)
			},
		},
		{
			name: "branch sibling",
			req: &entities.FinalizeBatchRequest{
				BatchSibling: &entities.BatchSibling{
					Branch: &entities.TapBranch{
						LeftTapHash:  leftHash,
						RightTapHash: rightHash,
					},
				},
			},
			validate: func(t *testing.T,
				rpcReq *mintrpc.FinalizeBatchRequest) {

				require.NotNil(t, rpcReq.GetBranch())
				require.Equal(t, leftHash[:],
					rpcReq.GetBranch().LeftTaphash)
				require.Equal(t, rightHash[:],
					rpcReq.GetBranch().RightTaphash)
			},
		},
		{
			name: "reject both sibling variants",
			req: &entities.FinalizeBatchRequest{
				BatchSibling: &entities.BatchSibling{
					FullTree: &entities.TapscriptFullTree{},
					Branch:   &entities.TapBranch{},
				},
			},
			wantErr: "batch sibling must set exactly one variant",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rpcReq, err := marshalFinalizeBatchRequest(tc.req)
			if tc.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.wantErr)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, rpcReq)
			tc.validate(t, rpcReq)
		})
	}
}

func TestUnmarshalMintingBatch(t *testing.T) {
	metaHash := append([]byte(nil), testAssetID...)
	_, pubKey := btcec.PrivKeyFromBytes(testAssetID)
	validPubKey := pubKey.SerializeCompressed()
	expectedScriptKey, err := entities.ParseTaprootPubKey(validPubKey)
	require.NoError(t, err)

	tests := []struct {
		name     string
		rpcBatch *mintrpc.MintingBatch
		wantErr  string
		validate func(*testing.T, *entities.MintingBatch)
	}{
		{
			name:     "nil batch",
			rpcBatch: nil,
			wantErr:  "nil minting batch",
		},
		{
			name: "invalid batch key",
			rpcBatch: &mintrpc.MintingBatch{
				BatchKey: []byte{0x01},
			},
			wantErr: "invalid batch key",
		},
		{
			name: "invalid group key",
			rpcBatch: &mintrpc.MintingBatch{
				Assets: []*mintrpc.PendingAsset{
					{
						Name:     "broken",
						GroupKey: []byte{0x01},
					},
				},
			},
			wantErr: "invalid group key",
		},
		{
			name: "valid batch",
			rpcBatch: &mintrpc.MintingBatch{
				BatchKey:   append([]byte(nil), validPubKey...),
				BatchTxid:  "abc123",
				State:      mintrpc.BatchState_BATCH_STATE_PENDING,
				CreatedAt:  1234,
				HeightHint: 99,
				BatchPsbt:  []byte{0xaa, 0xbb},
				Assets: []*mintrpc.PendingAsset{
					{
						AssetVersion:    taprpc.AssetVersion_ASSET_VERSION_V1,
						AssetType:       taprpc.AssetType_NORMAL,
						Name:            "usd-test",
						Amount:          123,
						NewGroupedAsset: true,
						AssetMeta: &taprpc.AssetMeta{
							Data:     []byte(`{"ticker":"USDt"}`),
							Type:     taprpc.AssetMetaType_META_TYPE_JSON,
							MetaHash: metaHash,
						},
						GroupKey: append([]byte(nil), validPubKey...),
						GroupAnchor: "anchor-asset",
						GroupInternalKey: &taprpc.KeyDescriptor{
							RawKeyBytes: append([]byte(nil), validPubKey...),
							KeyLoc: &taprpc.KeyLocator{
								KeyFamily: 7,
								KeyIndex:  9,
							},
						},
						GroupTapscriptRoot: []byte{0xaa, 0xbb},
						ScriptKey: &taprpc.ScriptKey{
							PubKey:   append([]byte(nil), validPubKey...),
							TapTweak: []byte{0x01, 0x02},
						},
					},
				},
			},
			validate: func(t *testing.T, batch *entities.MintingBatch) {
				require.Equal(t, "abc123", batch.BatchTxid)
				require.Equal(t,
					entities.BatchStatePending, batch.State)
				require.Equal(t, int64(1234), batch.CreatedAt)
				require.Equal(t, uint32(99), batch.HeightHint)
				require.Equal(t, []byte{0xaa, 0xbb}, batch.BatchPSBT)
				require.Equal(t, validPubKey, batch.BatchKey[:])
				require.Len(t, batch.Assets, 1)
				asset := batch.Assets[0]
				require.Equal(t, "usd-test", asset.Name)
				require.Equal(t,
					entities.AssetVersionV1, asset.AssetVersion)
				require.Equal(t,
					entities.AssetTypeNormal, asset.AssetType)
				require.Equal(t, uint64(123), asset.Amount)
				require.True(t, asset.NewGroupedAsset)
				require.Equal(t, "anchor-asset", asset.GroupAnchor)
				require.NotNil(t, asset.AssetMeta)
				require.Equal(t,
					entities.AssetMetaTypeJSON, asset.AssetMeta.Type)
				require.NotNil(t, asset.GroupKey)
				require.Equal(t, validPubKey, (*asset.GroupKey)[:])
				require.NotNil(t, asset.GroupInternalKey)
				require.Equal(t, uint32(7),
					asset.GroupInternalKey.KeyLocator.Family)
				require.NotNil(t, asset.ScriptKey)
				require.Equal(t, expectedScriptKey[:],
					asset.ScriptKey.PubKey[:])
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			batch, err := unmarshalMintingBatch(tc.rpcBatch)
			if tc.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.wantErr)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, batch)
			tc.validate(t, batch)
		})
	}
}
