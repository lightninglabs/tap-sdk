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
	assetFilter := func() *entities.AssetID {
		var id entities.AssetID
		copy(id[:], testAssetID)
		return &id
	}()

	groupKeyFilter := func() *entities.PubKey {
		var key entities.PubKey
		copy(key[:], testPubKey)
		return &key
	}()

	explicitType := entities.ScriptKeyTypeBurn

	tests := []struct {
		name     string
		req      *entities.ListBalancesRequest
		validate func(*testing.T, *taprpc.ListBalancesRequest)
	}{
		{
			name: "nil request defaults to asset grouping",
			req:  nil,
			validate: func(t *testing.T,
				rpcReq *taprpc.ListBalancesRequest) {

				require.True(t, rpcReq.GetAssetId())
				require.False(t, rpcReq.GetGroupKey())
				require.Nil(t, rpcReq.ScriptKeyType)
			},
		},
		{
			name: "asset balance query with explicit type",
			req: &entities.ListBalancesRequest{
				GroupBy:       entities.BalanceGroupByAssetID,
				AssetFilter:   assetFilter,
				IncludeLeased: true,
				ScriptKeyType: &entities.ScriptKeyTypeQuery{
					ExplicitType: &explicitType,
				},
			},
			validate: func(t *testing.T,
				rpcReq *taprpc.ListBalancesRequest) {

				require.True(t, rpcReq.GetAssetId())
				require.Equal(t, testAssetID, rpcReq.AssetFilter)
				require.True(t, rpcReq.IncludeLeased)
				require.NotNil(t, rpcReq.ScriptKeyType)
				require.Equal(
					t,
					taprpc.ScriptKeyType_SCRIPT_KEY_BURN,
					rpcReq.ScriptKeyType.GetExplicitType(),
				)
			},
		},
		{
			name: "group balance query with all types",
			req: &entities.ListBalancesRequest{
				GroupBy:        entities.BalanceGroupByGroupKey,
				GroupKeyFilter: groupKeyFilter,
				ScriptKeyType: &entities.ScriptKeyTypeQuery{
					AllTypes: true,
				},
			},
			validate: func(t *testing.T,
				rpcReq *taprpc.ListBalancesRequest) {

				require.False(t, rpcReq.GetAssetId())
				require.True(t, rpcReq.GetGroupKey())
				require.Equal(t, testPubKey, rpcReq.GroupKeyFilter)
				require.NotNil(t, rpcReq.ScriptKeyType)
				require.True(t, rpcReq.ScriptKeyType.GetAllTypes())
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
