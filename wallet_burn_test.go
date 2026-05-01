package tapsdk

import (
	"context"
	"testing"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestBurn(t *testing.T) {
	mc := new(mockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	ref := entities.AssetRefFromAssetID(testAssetID())
	expected := &entities.BurnAssetResponse{
		BurnTransfer: &entities.AssetTransfer{
			AnchorTxid: "burn-anchor",
		},
		BurnProof: &entities.DecodedProof{
			AssetRef: ref,
			Amount:   100,
		},
	}

	mc.On("BurnAsset", ctx, mock.MatchedBy(
		func(req *entities.BurnAssetRequest) bool {
			return req.AssetRef == ref &&
				req.AmountToBurn == 100 &&
				req.ConfirmationText == burnConfirmationText &&
				req.Note == "accounting"
		}),
	).Return(expected, nil)

	burn, err := w.Burn(
		ctx, ref, 100, WithBurnNote("accounting"),
	)
	require.NoError(t, err)
	require.Equal(t, ref, burn.AssetRef)
	require.Equal(t, uint64(100), burn.Amount)
	require.Equal(t, "accounting", burn.Note)
	require.Equal(t, expected.BurnTransfer, burn.Transfer)
	require.Equal(t, expected.BurnProof, burn.Proof)

	mc.AssertExpectations(t)
}

func TestBurnRejectsZeroAmount(t *testing.T) {
	mc := new(mockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	_, err := w.Burn(ctx, entities.AssetRefFromAssetID(testAssetID()), 0)
	require.ErrorIs(t, err, ErrZeroAmount)

	mc.AssertExpectations(t)
}
