//go:build itest

package itest

import (
	"testing"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

func requireAssetRefsContain(t testing.TB, refs []entities.AssetRef,
	want entities.AssetRef) {

	t.Helper()

	for _, ref := range refs {
		if ref.Equivalent(want) {
			return
		}
	}

	require.FailNowf(t, "asset ref not found",
		"want %s in %v", want, refs)
}

func requireTransferUsesAssetRef(t testing.TB,
	transfer *entities.Transfer, want entities.AssetRef) {

	t.Helper()

	require.NotNil(t, transfer)
	require.NotEmpty(t, transfer.Inputs)
	for idx, input := range transfer.Inputs {
		require.Truef(t, input.AssetRef.Equivalent(want),
			"input %d uses %s, want %s", idx, input.AssetRef,
			want)
	}

	require.NotEmpty(t, transfer.Outputs)
	for idx, output := range transfer.Outputs {
		require.Truef(t, output.AssetRef.Equivalent(want),
			"output %d uses %s, want %s", idx,
			output.AssetRef, want)
	}
}

func requireRawTransferUsesGroupRef(t testing.TB,
	transfer *entities.AssetTransfer, want entities.AssetRef) {

	t.Helper()

	wantGroupKey, ok := want.GroupKey()
	require.True(t, ok, "expected group AssetRef")

	require.NotNil(t, transfer)
	require.NotEmpty(t, transfer.Inputs)
	for idx, input := range transfer.Inputs {
		require.NotNilf(t, input.GroupKey,
			"input %d missing group key", idx)
		require.Equalf(t, wantGroupKey, *input.GroupKey,
			"input %d group key mismatch", idx)
	}

	require.NotEmpty(t, transfer.Outputs)
	for idx, output := range transfer.Outputs {
		require.NotNilf(t, output.GroupKey,
			"output %d missing group key", idx)
		require.Equalf(t, wantGroupKey, *output.GroupKey,
			"output %d group key mismatch", idx)
	}
}

func requireRawTransferUsesTypedAssetRef(t testing.TB,
	transfer *entities.AssetTransfer, want entities.AssetRef,
	wantType entities.AssetType) {

	t.Helper()

	require.NotNil(t, transfer)
	require.NotEmpty(t, transfer.Inputs)
	for idx, input := range transfer.Inputs {
		ref := entities.AssetRefFromTypedAsset(
			input.IssuanceID, input.GroupKey, input.AssetType,
		)
		require.Truef(t, ref.Equivalent(want),
			"input %d uses %s, want %s", idx, ref, want)
		require.Equalf(t, wantType, input.AssetType,
			"input %d asset type mismatch", idx)
	}

	require.NotEmpty(t, transfer.Outputs)
	for idx, output := range transfer.Outputs {
		ref := entities.AssetRefFromTypedAsset(
			output.IssuanceID, output.GroupKey, output.AssetType,
		)
		require.Truef(t, ref.Equivalent(want),
			"output %d uses %s, want %s", idx, ref, want)
		require.Equalf(t, wantType, output.AssetType,
			"output %d asset type mismatch", idx)
	}
}
