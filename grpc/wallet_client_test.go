package grpc

import (
	"testing"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/lightninglabs/taproot-assets/taprpc"
	"github.com/stretchr/testify/require"
)

const zeroGenesisPoint = "00000000000000000000000000000000" +
	"00000000000000000000000000000000:1"

func TestMarshalListBalancesRequest(t *testing.T) {
	var assetID entities.AssetID
	copy(assetID[:], testAssetID)
	assetIDRef := entities.AssetRefFromAssetID(assetID)

	var groupPubKey entities.PubKey
	copy(groupPubKey[:], testPubKey)
	groupKeyRef := entities.AssetRefFromGroupKey(groupPubKey)

	explicitType := entities.ScriptKeyTypeBurn

	tests := []struct {
		name     string
		req      *entities.ListBalancesRequest
		validate func(*testing.T, *taprpc.ListBalancesRequest)
	}{
		{
			name: "nil request groups by asset_id",
			req:  nil,
			validate: func(t *testing.T,
				rpcReq *taprpc.ListBalancesRequest) {

				require.True(
					t, rpcReq.GetAssetId(),
				)
				require.Empty(t, rpcReq.AssetFilter)
				require.Empty(t, rpcReq.GroupKeyFilter)
				require.Nil(t, rpcReq.ScriptKeyType)
			},
		},
		{
			name: "asset ID ref sets asset filter",
			req: &entities.ListBalancesRequest{
				AssetRef: &assetIDRef,
			},
			validate: func(t *testing.T,
				rpcReq *taprpc.ListBalancesRequest) {

				require.True(
					t, rpcReq.GetAssetId(),
				)
				require.Equal(
					t, assetID[:],
					rpcReq.AssetFilter,
				)
				require.Empty(
					t, rpcReq.GroupKeyFilter,
				)
			},
		},
		{
			name: "group key ref filters client side",
			req: &entities.ListBalancesRequest{
				AssetRef: &groupKeyRef,
			},
			validate: func(t *testing.T,
				rpcReq *taprpc.ListBalancesRequest) {

				require.True(
					t, rpcReq.GetAssetId(),
				)
				require.Empty(t, rpcReq.AssetFilter)
				require.Empty(t, rpcReq.GroupKeyFilter)
			},
		},
		{
			name: "explicit script key type",
			req: &entities.ListBalancesRequest{
				IncludeLeased: true,
				ScriptKeyType: &entities.ScriptKeyTypeQuery{
					ExplicitType: &explicitType,
				},
			},
			validate: func(t *testing.T,
				rpcReq *taprpc.ListBalancesRequest) {

				require.True(t, rpcReq.IncludeLeased)
				require.NotNil(
					t, rpcReq.ScriptKeyType,
				)
				require.Equal(
					t,
					taprpc.ScriptKeyType_SCRIPT_KEY_BURN,
					rpcReq.ScriptKeyType.GetExplicitType(),
				)
			},
		},
		{
			name: "all script key types",
			req: &entities.ListBalancesRequest{
				ScriptKeyType: &entities.ScriptKeyTypeQuery{
					AllTypes: true,
				},
			},
			validate: func(t *testing.T,
				rpcReq *taprpc.ListBalancesRequest) {

				require.NotNil(
					t, rpcReq.ScriptKeyType,
				)
				require.True(
					t,
					rpcReq.ScriptKeyType.GetAllTypes(),
				)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rpcReq := marshalListBalancesRequest(tc.req)
			require.NotNil(t, rpcReq)
			tc.validate(t, rpcReq)
		})
	}
}

func TestFilterSemanticBalances(t *testing.T) {
	var groupKey entities.PubKey
	copy(groupKey[:], testPubKey)
	groupRef := entities.AssetRefFromGroupKey(groupKey)

	var assetID entities.AssetID
	copy(assetID[:], testAssetID)
	assetRef := entities.AssetRefFromAssetID(assetID)

	tests := []struct {
		name    string
		req     *entities.ListBalancesRequest
		resp    *entities.ListBalancesResponse
		wantLen int
		wantKey string
	}{
		{
			name: "nil request keeps all balances",
			resp: &entities.ListBalancesResponse{
				Balances: map[string]*entities.AssetBalance{
					groupRef.String(): {AssetRef: groupRef, Balance: 5},
					assetRef.String(): {AssetRef: assetRef, Balance: 1},
				},
			},
			wantLen: 2,
		},
		{
			name: "matching ref keeps single balance",
			req:  &entities.ListBalancesRequest{AssetRef: &groupRef},
			resp: &entities.ListBalancesResponse{
				Balances: map[string]*entities.AssetBalance{
					groupRef.String(): {AssetRef: groupRef, Balance: 5},
					assetRef.String(): {AssetRef: assetRef, Balance: 1},
				},
			},
			wantLen: 1,
			wantKey: groupRef.String(),
		},
		{
			name: "missing ref returns empty set",
			req:  &entities.ListBalancesRequest{AssetRef: &groupRef},
			resp: &entities.ListBalancesResponse{
				Balances: map[string]*entities.AssetBalance{
					assetRef.String(): {AssetRef: assetRef, Balance: 1},
				},
			},
			wantLen: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			filterSemanticBalances(tc.resp, tc.req)
			require.Len(t, tc.resp.Balances, tc.wantLen)
			if tc.wantKey != "" {
				require.Contains(t, tc.resp.Balances, tc.wantKey)
			}
		})
	}
}

func TestShouldFallbackToGroupBalance(t *testing.T) {
	var groupKey entities.PubKey
	copy(groupKey[:], testPubKey)
	groupRef := entities.AssetRefFromGroupKey(groupKey)

	var assetID entities.AssetID
	copy(assetID[:], testAssetID)
	assetRef := entities.AssetRefFromAssetID(assetID)

	tests := []struct {
		name string
		req  *entities.ListBalancesRequest
		resp *entities.ListBalancesResponse
		want bool
	}{
		{
			name: "group ref with empty result falls back",
			req:  &entities.ListBalancesRequest{AssetRef: &groupRef},
			resp: &entities.ListBalancesResponse{
				Balances: map[string]*entities.AssetBalance{},
			},
			want: true,
		},
		{
			name: "group ref with populated result skips fallback",
			req:  &entities.ListBalancesRequest{AssetRef: &groupRef},
			resp: &entities.ListBalancesResponse{
				Balances: map[string]*entities.AssetBalance{
					groupRef.String(): {
						AssetRef: groupRef,
						Balance:  5,
					},
				},
			},
			want: false,
		},
		{
			name: "asset id refs never fall back",
			req:  &entities.ListBalancesRequest{AssetRef: &assetRef},
			resp: &entities.ListBalancesResponse{
				Balances: map[string]*entities.AssetBalance{},
			},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want,
				shouldFallbackToGroupBalance(tc.req, tc.resp))
		})
	}
}

func TestFindGroupBalance(t *testing.T) {
	var groupKey entities.PubKey
	copy(groupKey[:], testPubKey)

	balance, ok := findGroupBalance(
		map[string]*taprpc.AssetGroupBalance{
			groupKey.String(): {Balance: 21},
		},
		groupKey,
	)
	require.True(t, ok)
	require.Equal(t, uint64(21), balance.Balance)

	balance, ok = findGroupBalance(
		map[string]*taprpc.AssetGroupBalance{
			"unexpected": {Balance: 34},
		},
		groupKey,
	)
	require.True(t, ok)
	require.Equal(t, uint64(34), balance.Balance)

	_, ok = findGroupBalance(
		map[string]*taprpc.AssetGroupBalance{
			"first":  {Balance: 1},
			"second": {Balance: 2},
		},
		groupKey,
	)
	require.False(t, ok)
}

func TestNewSemanticGroupBalanceResponse(t *testing.T) {
	var groupKey entities.PubKey
	copy(groupKey[:], testPubKey)
	ref := entities.AssetRefFromGroupKey(groupKey)

	var issuanceID entities.AssetID
	copy(issuanceID[:], testAssetID)

	asset := &entities.Asset{
		AssetRef: ref,
		Genesis: entities.AssetGenesis{
			Tag:        "test-group",
			IssuanceID: issuanceID,
		},
	}

	resp, err := newSemanticGroupBalanceResponse(
		ref, groupKey, asset, 55, 2,
	)
	require.NoError(t, err)
	require.Len(t, resp.Balances, 1)
	require.Equal(t, uint64(2), resp.UnconfirmedTransfers)

	balance := resp.Balances[ref.String()]
	require.NotNil(t, balance)
	require.Equal(t, uint64(55), balance.Balance)
	require.Equal(t, asset.Genesis, balance.AssetGenesis)
	require.NotNil(t, balance.GroupKey)
	require.Equal(t, groupKey, *balance.GroupKey)
}

func TestMarshalSendAssetRequest(t *testing.T) {
	tests := []struct {
		name     string
		req      *entities.SendAssetRequest
		validate func(*testing.T, *taprpc.SendAssetRequest)
	}{
		{
			name: "nil request",
			req:  nil,
			validate: func(t *testing.T, rpcReq *taprpc.SendAssetRequest) {
				require.NotNil(t, rpcReq)
				require.Empty(t, rpcReq.TapAddrs)
				require.Empty(t, rpcReq.AddressesWithAmounts)
			},
		},
		{
			name: "address encoded amounts",
			req: &entities.SendAssetRequest{
				TapAddresses: []string{"tap1first", "tap1second"},
				FeeRate:      250,
				Label:        "batch-send",
			},
			validate: func(t *testing.T, rpcReq *taprpc.SendAssetRequest) {
				require.Equal(
					t,
					[]string{"tap1first", "tap1second"},
					rpcReq.TapAddrs,
				)
				require.Equal(t, uint32(250), rpcReq.FeeRate)
				require.Equal(t, "batch-send", rpcReq.Label)
				require.Empty(t, rpcReq.AddressesWithAmounts)
			},
		},
		{
			name: "explicit recipient amounts override tap addresses",
			req: &entities.SendAssetRequest{
				TapAddresses: []string{"tap1legacy"},
				Recipients: []entities.Recipient{
					{
						Address: "tap1amountless",
						Amount:  150,
					},
					{
						Address: "tap1fixed",
						Amount:  0,
					},
				},
				SkipProofCourierPingCheck: true,
			},
			validate: func(t *testing.T, rpcReq *taprpc.SendAssetRequest) {
				require.Empty(t, rpcReq.TapAddrs)
				require.True(t, rpcReq.SkipProofCourierPingCheck)
				require.Len(t, rpcReq.AddressesWithAmounts, 2)
				require.Equal(
					t,
					"tap1amountless",
					rpcReq.AddressesWithAmounts[0].TapAddr,
				)
				require.Equal(
					t,
					uint64(150),
					rpcReq.AddressesWithAmounts[0].Amount,
				)
				require.Equal(
					t,
					"tap1fixed",
					rpcReq.AddressesWithAmounts[1].TapAddr,
				)
				require.Zero(t, rpcReq.AddressesWithAmounts[1].Amount)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rpcReq := marshalSendAssetRequest(tc.req)
			require.NotNil(t, rpcReq)
			tc.validate(t, rpcReq)
		})
	}
}

func TestUnmarshalAssetBalance(t *testing.T) {
	tests := []struct {
		name       string
		rpcBalance *taprpc.AssetBalance
		wantErr    string
	}{
		{
			name:       "nil balance",
			rpcBalance: nil,
			wantErr:    "nil asset balance",
		},
		{
			name: "missing genesis",
			rpcBalance: &taprpc.AssetBalance{
				Balance: 10,
			},
			wantErr: "missing asset genesis",
		},
		{
			name: "invalid group key length",
			rpcBalance: &taprpc.AssetBalance{
				AssetGenesis: &taprpc.GenesisInfo{
					GenesisPoint: zeroGenesisPoint,
					Name:         "test",
					AssetId:      testAssetID,
					OutputIndex:  1,
					AssetType:    taprpc.AssetType_NORMAL,
				},
				GroupKey: []byte{0x01},
			},
			wantErr: "invalid group key length",
		},
		{
			name: "valid asset balance",
			rpcBalance: &taprpc.AssetBalance{
				AssetGenesis: &taprpc.GenesisInfo{
					GenesisPoint: zeroGenesisPoint,
					Name:         "test",
					AssetId:      testAssetID,
					OutputIndex:  1,
					AssetType:    taprpc.AssetType_NORMAL,
				},
				Balance:  42,
				GroupKey: testPubKey,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			balance, err := unmarshalAssetBalance(tc.rpcBalance)

			if tc.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.wantErr)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, balance)
			require.Equal(t, uint64(42), balance.Balance)
			require.NotNil(t, balance.GroupKey)
			require.Equal(t, testPubKey, balance.GroupKey[:])
			require.Equal(t, "test", balance.AssetGenesis.Tag)
		})
	}
}

func TestUnmarshalAssetGroupBalance(t *testing.T) {
	tests := []struct {
		name       string
		rpcBalance *taprpc.AssetGroupBalance
		wantErr    string
	}{
		{
			name:       "nil balance",
			rpcBalance: nil,
			wantErr:    "nil asset group balance",
		},
		{
			name: "invalid group key length",
			rpcBalance: &taprpc.AssetGroupBalance{
				GroupKey: []byte{0x01},
				Balance:  5,
			},
			wantErr: "invalid group key length",
		},
		{
			name: "valid grouped balance",
			rpcBalance: &taprpc.AssetGroupBalance{
				GroupKey: testPubKey,
				Balance:  21,
			},
		},
		{
			name: "ungrouped balance",
			rpcBalance: &taprpc.AssetGroupBalance{
				Balance: 9,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			balance, err := unmarshalAssetGroupBalance(tc.rpcBalance)

			if tc.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.wantErr)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, balance)
			require.Equal(t, tc.rpcBalance.Balance, balance.Balance)
			if len(tc.rpcBalance.GroupKey) == 0 {
				require.Nil(t, balance.GroupKey)
			} else {
				require.NotNil(t, balance.GroupKey)
				require.Equal(t, testPubKey, balance.GroupKey[:])
			}
		})
	}
}

func TestScriptKeyTypeConstants(t *testing.T) {
	require.Equal(t,
		int(taprpc.ScriptKeyType_SCRIPT_KEY_UNKNOWN),
		int(entities.ScriptKeyTypeUnknown),
	)
	require.Equal(t,
		int(taprpc.ScriptKeyType_SCRIPT_KEY_BIP86),
		int(entities.ScriptKeyTypeBIP86),
	)
	require.Equal(t,
		int(taprpc.ScriptKeyType_SCRIPT_KEY_SCRIPT_PATH_EXTERNAL),
		int(entities.ScriptKeyTypeScriptPathExternal),
	)
	require.Equal(t,
		int(taprpc.ScriptKeyType_SCRIPT_KEY_BURN),
		int(entities.ScriptKeyTypeBurn),
	)
	require.Equal(t,
		int(taprpc.ScriptKeyType_SCRIPT_KEY_TOMBSTONE),
		int(entities.ScriptKeyTypeTombstone),
	)
	require.Equal(t,
		int(taprpc.ScriptKeyType_SCRIPT_KEY_CHANNEL),
		int(entities.ScriptKeyTypeChannel),
	)
	require.Equal(t,
		int(taprpc.ScriptKeyType_SCRIPT_KEY_UNIQUE_PEDERSEN),
		int(entities.ScriptKeyTypeUniquePedersen),
	)
}

// validPubKeyBytes is the compressed secp256k1 generator point G.
// Tests that call ParsePubKey need a point actually on the curve.
var validPubKeyBytes = []byte{
	0x02,
	0x79, 0xbe, 0x66, 0x7e, 0xf9, 0xdc, 0xbb, 0xac,
	0x55, 0xa0, 0x62, 0x95, 0xce, 0x87, 0x0b, 0x07,
	0x02, 0x9b, 0xfc, 0xdb, 0x2d, 0xce, 0x28, 0xd9,
	0x59, 0xf2, 0x81, 0x5b, 0x16, 0xf8, 0x17, 0x98,
}

func TestUnmarshalManagedUtxo(t *testing.T) {
	tests := []struct {
		name    string
		rpcUtxo *taprpc.ManagedUtxo
		wantErr string
	}{
		{
			name:    "nil utxo",
			rpcUtxo: nil,
			wantErr: "nil managed utxo",
		},
		{
			name: "valid utxo without assets",
			rpcUtxo: &taprpc.ManagedUtxo{
				OutPoint:         zeroGenesisPoint,
				AmtSat:           100000,
				InternalKey:      validPubKeyBytes,
				TaprootAssetRoot: testAssetID,
				MerkleRoot:       testAssetID,
			},
		},
		{
			name: "invalid outpoint",
			rpcUtxo: &taprpc.ManagedUtxo{
				OutPoint:    "invalid",
				InternalKey: validPubKeyBytes,
			},
			wantErr: "invalid outpoint",
		},
		{
			name: "invalid internal key",
			rpcUtxo: &taprpc.ManagedUtxo{
				OutPoint:    zeroGenesisPoint,
				InternalKey: []byte{0x01},
			},
			wantErr: "invalid internal key",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := unmarshalManagedUtxo(tc.rpcUtxo)
			if tc.wantErr != "" {
				require.Error(t, err)
				require.Contains(
					t, err.Error(), tc.wantErr,
				)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(
				t, tc.rpcUtxo.AmtSat, result.AmtSat,
			)
		})
	}
}

func TestUnmarshalGroupedAssets(t *testing.T) {
	tests := []struct {
		name     string
		rpcGroup *taprpc.GroupedAssets
		wantErr  string
	}{
		{
			name:     "nil group",
			rpcGroup: nil,
			wantErr:  "nil grouped assets",
		},
		{
			name: "valid group with one asset",
			rpcGroup: &taprpc.GroupedAssets{
				Assets: []*taprpc.AssetHumanReadable{
					{
						Id:       testAssetID,
						Amount:   1000,
						Tag:      "test-asset",
						MetaHash: testAssetID,
						Type:     taprpc.AssetType_NORMAL,
					},
				},
			},
		},
		{
			name: "nil asset in group",
			rpcGroup: &taprpc.GroupedAssets{
				Assets: []*taprpc.AssetHumanReadable{nil},
			},
			wantErr: "nil asset in group",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := unmarshalGroupedAssets(tc.rpcGroup)
			if tc.wantErr != "" {
				require.Error(t, err)
				require.Contains(
					t, err.Error(), tc.wantErr,
				)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Len(
				t, result.Assets,
				len(tc.rpcGroup.Assets),
			)
		})
	}
}

func TestUnmarshalAssetBurn(t *testing.T) {
	tests := []struct {
		name    string
		rpcBurn *taprpc.AssetBurn
		wantErr string
	}{
		{
			name:    "nil burn",
			rpcBurn: nil,
			wantErr: "nil asset burn",
		},
		{
			name: "valid burn without group key",
			rpcBurn: &taprpc.AssetBurn{
				Note:       "test burn",
				AssetId:    testAssetID,
				Amount:     500,
				AnchorTxid: testAssetID,
			},
		},
		{
			name: "valid burn with group key",
			rpcBurn: &taprpc.AssetBurn{
				AssetId:         testAssetID,
				TweakedGroupKey: validPubKeyBytes,
				Amount:          100,
				AnchorTxid:      testAssetID,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := unmarshalAssetBurn(tc.rpcBurn)
			if tc.wantErr != "" {
				require.Error(t, err)
				require.Contains(
					t, err.Error(), tc.wantErr,
				)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, tc.rpcBurn.Amount, result.Amount)
		})
	}
}

func TestMarshalBurnAssetRequest(t *testing.T) {
	assetID := entities.AssetID{}
	copy(assetID[:], testAssetID)

	tests := []struct {
		name     string
		req      *entities.BurnAssetRequest
		wantErr  string
		validate func(*testing.T, *taprpc.BurnAssetRequest)
	}{
		{
			name: "burn by asset ID ref",
			req: &entities.BurnAssetRequest{
				AssetRef:         entities.AssetRefFromAssetID(assetID),
				AmountToBurn:     100,
				ConfirmationText: "assets will be destroyed",
				Note:             "test",
			},
			validate: func(t *testing.T,
				rpcReq *taprpc.BurnAssetRequest) {

				require.NotNil(t, rpcReq.AssetSpecifier)
				require.Equal(
					t, testAssetID,
					rpcReq.AssetSpecifier.GetAssetId(),
				)
				require.Equal(
					t, uint64(100),
					rpcReq.AmountToBurn,
				)
			},
		},
		{
			name: "burn by group key ref",
			req: &entities.BurnAssetRequest{
				AssetRef: func() entities.AssetRef {
					var gk entities.PubKey
					copy(gk[:], testPubKey)
					return entities.AssetRefFromGroupKey(gk)
				}(),
				AmountToBurn:     200,
				ConfirmationText: "assets will be destroyed",
			},
			validate: func(t *testing.T,
				rpcReq *taprpc.BurnAssetRequest) {

				require.NotNil(t, rpcReq.AssetSpecifier)
				require.Equal(
					t, testPubKey,
					rpcReq.AssetSpecifier.GetGroupKey(),
				)
				require.Equal(
					t, uint64(200),
					rpcReq.AmountToBurn,
				)
			},
		},
		{
			name: "missing asset ref",
			req: &entities.BurnAssetRequest{
				AmountToBurn:     50,
				ConfirmationText: "assets will be destroyed",
			},
			wantErr: "asset ref is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rpcReq, err := marshalBurnAssetRequest(tc.req)
			if tc.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.wantErr)
				return
			}

			require.NoError(t, err)
			tc.validate(t, rpcReq)
		})
	}
}

func TestMarshalFetchAssetMetaRequest(t *testing.T) {
	assetID := entities.AssetID{}
	copy(assetID[:], testAssetID)

	metaHash := entities.Hash{}
	copy(metaHash[:], testAssetID)

	tests := []struct {
		name     string
		req      *entities.FetchAssetMetaRequest
		validate func(*testing.T, *taprpc.FetchAssetMetaRequest)
	}{
		{
			name: "fetch by asset ref",
			req: &entities.FetchAssetMetaRequest{
				AssetRef: func() *entities.AssetRef {
					ref := entities.AssetRefFromAssetID(assetID)
					return &ref
				}(),
			},
			validate: func(t *testing.T,
				rpcReq *taprpc.FetchAssetMetaRequest) {

				require.Equal(
					t, testAssetID,
					rpcReq.GetAssetId(),
				)
			},
		},
		{
			name: "fetch by meta hash",
			req: &entities.FetchAssetMetaRequest{
				MetaHash: &metaHash,
			},
			validate: func(t *testing.T,
				rpcReq *taprpc.FetchAssetMetaRequest) {

				require.Equal(
					t, testAssetID,
					rpcReq.GetMetaHash(),
				)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rpcReq, err := marshalFetchAssetMetaRequest(tc.req)
			require.NoError(t, err)
			tc.validate(t, rpcReq)
		})
	}
}

func TestUnmarshalAssetMeta(t *testing.T) {
	tests := []struct {
		name    string
		resp    *taprpc.FetchAssetMetaResponse
		wantErr string
	}{
		{
			name:    "nil response",
			resp:    nil,
			wantErr: "nil asset meta response",
		},
		{
			name: "valid meta",
			resp: &taprpc.FetchAssetMetaResponse{
				Data:                  []byte("test data"),
				Type:                  taprpc.AssetMetaType_META_TYPE_JSON,
				MetaHash:              testAssetID,
				DecimalDisplay:        8,
				UniverseCommitments:   true,
				CanonicalUniverseUrls: []string{"https://uni.example.com"},
				DelegationKey:         testPubKey,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := unmarshalFetchAssetMetaResponse(tc.resp)
			if tc.wantErr != "" {
				require.Error(t, err)
				require.Contains(
					t, err.Error(), tc.wantErr,
				)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(
				t, entities.AssetMetaTypeJSON,
				result.Type,
			)
			require.Equal(
				t, uint32(8), result.DecimalDisplay,
			)
			require.True(t, result.UniverseCommitments)
			require.Len(
				t, result.CanonicalUniverseURLs, 1,
			)
		})
	}
}
