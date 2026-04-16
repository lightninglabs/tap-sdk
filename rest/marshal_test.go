package rest

import (
	"testing"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

func testGroupKey(t *testing.T) entities.PubKey {
	t.Helper()

	key, err := entities.ParsePubKeyHex(
		"0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798",
	)
	require.NoError(t, err)

	return key
}

func TestParseHexBytes(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantLen int
		wantErr bool
	}{
		{
			name:    "empty string",
			input:   "",
			wantLen: 0,
		},
		{
			name:    "valid hex",
			input:   "abcdef0123456789",
			wantLen: 8,
		},
		{
			name:    "invalid hex",
			input:   "not-hex",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseHexBytes(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Len(t, got, tc.wantLen)
		})
	}
}

func TestParseBase64Bytes(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantLen int
		wantErr bool
	}{
		{
			name:    "empty string",
			input:   "",
			wantLen: 0,
		},
		{
			name:    "valid base64",
			input:   "SGVsbG8=",
			wantLen: 5,
		},
		{
			name:    "invalid base64",
			input:   "not-valid!!!",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseBase64Bytes(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Len(t, got, tc.wantLen)
		})
	}
}

func TestFilterSemanticBalances(t *testing.T) {
	groupKey := testGroupKey(t)
	groupRef := entities.AssetRefFromGroupKey(groupKey)

	var assetID entities.AssetID
	for i := range assetID {
		assetID[i] = byte(i + 1)
	}
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
	groupKey := testGroupKey(t)
	groupRef := entities.AssetRefFromGroupKey(groupKey)

	var assetID entities.AssetID
	for i := range assetID {
		assetID[i] = byte(i + 1)
	}
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
	groupKey := testGroupKey(t)

	balance, ok := findGroupBalance(
		map[string]*jsonAssetGroupBalance{
			groupKey.String(): {Balance: "21"},
		},
		groupKey,
	)
	require.True(t, ok)
	require.Equal(t, "21", balance.Balance)

	balance, ok = findGroupBalance(
		map[string]*jsonAssetGroupBalance{
			"unexpected": {Balance: "34"},
		},
		groupKey,
	)
	require.True(t, ok)
	require.Equal(t, "34", balance.Balance)

	_, ok = findGroupBalance(
		map[string]*jsonAssetGroupBalance{
			"first":  {Balance: "1"},
			"second": {Balance: "2"},
		},
		groupKey,
	)
	require.False(t, ok)
}

func TestNewSemanticGroupBalanceResponse(t *testing.T) {
	groupKey := testGroupKey(t)
	ref := entities.AssetRefFromGroupKey(groupKey)

	var issuanceID entities.AssetID
	for i := range issuanceID {
		issuanceID[i] = byte(i + 1)
	}

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

func TestParseUint64(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    uint64
		wantErr bool
	}{
		{
			name:  "empty string",
			input: "",
			want:  0,
		},
		{
			name:  "zero",
			input: "0",
			want:  0,
		},
		{
			name:  "positive value",
			input: "42",
			want:  42,
		},
		{
			name:  "large value",
			input: "18446744073709551615",
			want:  18446744073709551615,
		},
		{
			name:    "negative value",
			input:   "-1",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseUint64(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestParseAssetType(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{
			name:  "normal",
			input: "NORMAL",
			want:  0,
		},
		{
			name:  "collectible",
			input: "COLLECTIBLE",
			want:  1,
		},
		{
			name:  "unknown defaults to normal",
			input: "UNKNOWN",
			want:  0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseAssetType(tc.input)
			require.Equal(t, tc.want, int(got))
		})
	}
}

func TestParseAssetVersion(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{
			name:  "v0",
			input: "ASSET_VERSION_V0",
			want:  0,
		},
		{
			name:  "v1",
			input: "ASSET_VERSION_V1",
			want:  1,
		},
		{
			name:  "unknown defaults to v0",
			input: "SOMETHING_ELSE",
			want:  0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseAssetVersion(tc.input)
			require.Equal(t, tc.want, int(got))
		})
	}
}

func TestParseAddressVersion(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  entities.AddressVersion
	}{
		{
			name:  "v0",
			input: "ADDR_VERSION_V0",
			want:  entities.AddressVersionV0,
		},
		{
			name:  "v1",
			input: "ADDR_VERSION_V1",
			want:  entities.AddressVersionV1,
		},
		{
			name:  "v2",
			input: "ADDR_VERSION_V2",
			want:  entities.AddressVersionV2,
		},
		{
			name:  "unknown defaults to v0",
			input: "ADDR_VERSION_UNKNOWN",
			want:  entities.AddressVersionV0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseAddressVersion(tc.input)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestParseBackupMode(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  entities.BackupMode
	}{
		{
			name:  "raw",
			input: "RAW",
			want:  entities.BackupModeRaw,
		},
		{
			name:  "compact",
			input: "COMPACT",
			want:  entities.BackupModeCompact,
		},
		{
			name:  "optimistic",
			input: "OPTIMISTIC",
			want:  entities.BackupModeOptimistic,
		},
		{
			name:  "unknown defaults to raw",
			input: "SOMETHING_ELSE",
			want:  entities.BackupModeRaw,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, parseBackupMode(tc.input))
		})
	}
}

func TestMarshalBackupMode(t *testing.T) {
	tests := []struct {
		name string
		mode entities.BackupMode
		want string
	}{
		{
			name: "raw",
			mode: entities.BackupModeRaw,
			want: "RAW",
		},
		{
			name: "compact",
			mode: entities.BackupModeCompact,
			want: "COMPACT",
		},
		{
			name: "optimistic",
			mode: entities.BackupModeOptimistic,
			want: "OPTIMISTIC",
		},
		{
			name: "unknown defaults to raw",
			mode: entities.BackupMode(99),
			want: "RAW",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, marshalBackupMode(tc.mode))
		})
	}
}

func TestParseBatchState(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  entities.BatchState
	}{
		{
			name:  "pending",
			input: "BATCH_STATE_PENDING",
			want:  entities.BatchStatePending,
		},
		{
			name:  "frozen",
			input: "BATCH_STATE_FROZEN",
			want:  entities.BatchStateFrozen,
		},
		{
			name:  "confirmed",
			input: "BATCH_STATE_CONFIRMED",
			want:  entities.BatchStateConfirmed,
		},
		{
			name:  "finalized",
			input: "BATCH_STATE_FINALIZED",
			want:  entities.BatchStateFinalized,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseBatchState(tc.input)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestUnmarshalInfo(t *testing.T) {
	resp := &jsonGetInfoResponse{
		Version:    "0.4.0-beta",
		LndVersion: "0.18.0",
		Network:    "testnet",
		LndIdentityPubkey: "02" + "ab" + "cd" + "ef" +
			"0123456789abcdef0123456789" +
			"abcdef0123456789abcdef01",
		NodeAlias:   "testnode",
		BlockHeight: 800000,
		SyncToChain: true,
	}

	info, err := unmarshalInfo(resp)
	require.NoError(t, err)
	require.Equal(t, "0.4.0-beta", info.Version)
	require.Equal(t, "0.18.0", info.LndVersion)
	require.Equal(t, "testnet", info.Network)
	require.Equal(t, "testnode", info.NodeAlias)
	require.Equal(t, uint32(800000), info.BlockHeight)
	require.True(t, info.SyncedToChain)
}

func TestMarshalAssetVersionJSON(t *testing.T) {
	tests := []struct {
		name  string
		input int
		want  string
	}{
		{
			name:  "v0",
			input: 0,
			want:  "ASSET_VERSION_V0",
		},
		{
			name:  "v1",
			input: 1,
			want:  "ASSET_VERSION_V1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := marshalAssetVersionJSON(
				entities.AssetVersion(tc.input),
			)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestMarshalAddressVersionJSON(t *testing.T) {
	tests := []struct {
		name  string
		input entities.AddressVersion
		want  string
	}{
		{
			name:  "v0",
			input: entities.AddressVersionV0,
			want:  "ADDR_VERSION_V0",
		},
		{
			name:  "v1",
			input: entities.AddressVersionV1,
			want:  "ADDR_VERSION_V1",
		},
		{
			name:  "v2",
			input: entities.AddressVersionV2,
			want:  "ADDR_VERSION_V2",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := marshalAddressVersionJSON(tc.input)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestMarshalAssetTypeJSON(t *testing.T) {
	tests := []struct {
		name  string
		input int
		want  string
	}{
		{
			name:  "normal",
			input: 0,
			want:  "NORMAL",
		},
		{
			name:  "collectible",
			input: 1,
			want:  "COLLECTIBLE",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := marshalAssetTypeJSON(
				entities.AssetType(tc.input),
			)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestSplitOutpoint(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  [2]string
	}{
		{
			name:  "standard outpoint",
			input: "abc123:0",
			want:  [2]string{"abc123", "0"},
		},
		{
			name:  "with index",
			input: "deadbeef:42",
			want:  [2]string{"deadbeef", "42"},
		},
		{
			name:  "no colon",
			input: "nocolon",
			want:  [2]string{"nocolon", "0"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := splitOutpoint(tc.input)
			require.Equal(t, tc.want, got)
		})
	}
}
