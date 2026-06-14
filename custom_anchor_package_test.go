package tapsdk

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCustomAnchorTransferPackageValidate(t *testing.T) {
	valid := validCustomAnchorTransferPackage()
	require.NoError(t, valid.Validate())

	tests := []struct {
		name    string
		mutate  func(*CustomAnchorTransferPackage)
		wantErr string
	}{
		{
			name: "missing anchor psbt",
			mutate: func(pkg *CustomAnchorTransferPackage) {
				pkg.AnchorPsbt = nil
			},
			wantErr: "anchor PSBT is required",
		},
		{
			name: "missing active psbt",
			mutate: func(pkg *CustomAnchorTransferPackage) {
				pkg.ActiveVirtualPsbts = nil
			},
			wantErr: "at least one active virtual PSBT is required",
		},
		{
			name: "empty active psbt",
			mutate: func(pkg *CustomAnchorTransferPackage) {
				pkg.ActiveVirtualPsbts[0] = nil
			},
			wantErr: "active virtual PSBT 0 is empty",
		},
		{
			name: "empty passive psbt",
			mutate: func(pkg *CustomAnchorTransferPackage) {
				pkg.PassiveVirtualPsbts[0] = nil
			},
			wantErr: "passive virtual PSBT 0 is empty",
		},
		{
			name: "change index below sentinel",
			mutate: func(pkg *CustomAnchorTransferPackage) {
				pkg.ChangeOutputIndex = -2
			},
			wantErr: "change output index must be -1 or greater",
		},
		{
			name: "invalid input ref",
			mutate: func(pkg *CustomAnchorTransferPackage) {
				pkg.Inputs[0].AssetRef = AssetRef("invalid")
			},
			wantErr: "input 0 asset ref",
		},
		{
			name: "zero input amount",
			mutate: func(pkg *CustomAnchorTransferPackage) {
				pkg.Inputs[0].Amount = 0
			},
			wantErr: "input 0 amount is required",
		},
		{
			name: "invalid output ref",
			mutate: func(pkg *CustomAnchorTransferPackage) {
				pkg.Outputs[0].AssetRef = AssetRef("invalid")
			},
			wantErr: "output 0 asset ref",
		},
		{
			name: "zero output amount",
			mutate: func(pkg *CustomAnchorTransferPackage) {
				pkg.Outputs[0].Amount = 0
			},
			wantErr: "output 0 amount is required",
		},
		{
			name: "negative output anchor value",
			mutate: func(pkg *CustomAnchorTransferPackage) {
				pkg.Outputs[0].AnchorValueSat = -1
			},
			wantErr: "output 0 anchor value is negative",
		},
		{
			name: "invalid proof update ref",
			mutate: func(pkg *CustomAnchorTransferPackage) {
				pkg.ProofUpdates[0].AssetRef = AssetRef(
					"invalid",
				)
			},
			wantErr: "proof update 0 asset ref",
		},
		{
			name: "negative proof output index",
			mutate: func(pkg *CustomAnchorTransferPackage) {
				pkg.ProofUpdates[0].OutputIndex = -1
			},
			wantErr: "proof update 0 output index is negative",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pkg := valid.Clone()
			tc.mutate(pkg)

			err := pkg.Validate()
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestCustomAnchorTransferPackageValidateNil(t *testing.T) {
	var pkg *CustomAnchorTransferPackage

	require.ErrorContains(
		t, pkg.Validate(), "nil custom anchor transfer package",
	)
}

func TestCustomAnchorTransferPackageClone(t *testing.T) {
	pkg := validCustomAnchorTransferPackage()
	clone := pkg.Clone()

	pkg.AnchorPsbt[0] = 9
	pkg.ActiveVirtualPsbts[0][0] = 9
	pkg.PassiveVirtualPsbts[0][0] = 9
	pkg.LockedUTXOs[0].LockID[0] = 9
	pkg.ProofUpdates[0].ProofBlob[0] = 9

	require.Equal(t, []byte{1, 2}, clone.AnchorPsbt)
	require.Equal(t, []byte{3, 4}, clone.ActiveVirtualPsbts[0])
	require.Equal(t, []byte{5, 6}, clone.PassiveVirtualPsbts[0])
	require.Equal(t, []byte{7, 8}, clone.LockedUTXOs[0].LockID)
	require.Equal(t, []byte{9, 10}, clone.ProofUpdates[0].ProofBlob)
}

func TestCustomAnchorTransferPackageCloneNil(t *testing.T) {
	var pkg *CustomAnchorTransferPackage

	require.Nil(t, pkg.Clone())
}

func validCustomAnchorTransferPackage() *CustomAnchorTransferPackage {
	assetID := AssetID{1}
	assetRef := AssetRefFromAssetID(assetID)
	outpoint := Outpoint{
		Txid:  [32]byte{2},
		Index: 1,
	}

	return &CustomAnchorTransferPackage{
		AnchorPsbt:          []byte{1, 2},
		ActiveVirtualPsbts:  [][]byte{{3, 4}},
		PassiveVirtualPsbts: [][]byte{{5, 6}},
		ChangeOutputIndex:   1,
		LockedUTXOs: []CustomAnchorLockedUTXO{
			{
				Outpoint:              outpoint,
				ValueSat:              10_000,
				LockID:                []byte{7, 8},
				ExpirationUnixSeconds: 1_800,
			},
		},
		Inputs: []CustomAnchorAssetInputSummary{
			{
				AssetRef:       assetRef,
				IssuanceID:     assetID,
				AssetType:      AssetTypeFungible,
				AnchorOutpoint: outpoint,
				Amount:         100,
			},
		},
		Outputs: []CustomAnchorAssetOutputSummary{
			{
				AssetRef:          assetRef,
				IssuanceID:        assetID,
				AssetType:         AssetTypeFungible,
				AnchorOutpoint:    outpoint,
				AnchorOutputIndex: 2,
				AnchorValueSat:    330,
				Amount:            100,
			},
		},
		ProofUpdates: []CustomAnchorProofUpdate{
			{
				OutputIndex:    1,
				AssetRef:       assetRef,
				IssuanceID:     assetID,
				AnchorOutpoint: outpoint,
				ProofBlob:      []byte{9, 10},
			},
		},
		Publish: CustomAnchorPublishMetadata{
			SkipAnchorTxBroadcast: true,
			Label:                 "custom-anchor",
			ExternalBroadcast:     true,
		},
	}
}
