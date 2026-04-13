package tapsdk

import (
	"context"
	"testing"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewReceiveAddress_DefaultsToV2GroupKey(t *testing.T) {
	ctx := context.Background()
	mc := new(mockClient)

	var groupKey entities.PubKey
	copy(
		groupKey[:],
		[]byte("group_key_pubkey_33_bytes_longgg!!"),
	)
	ref := entities.AssetRefFromGroupKey(groupKey)

	expectedAddr := &entities.Address{Encoded: "tap1example"}
	mc.On("NewAddr", ctx, mock.MatchedBy(func(
		req *entities.NewAddressRequest) bool {

		if req == nil || req.AddressVersion == nil {
			return false
		}

		return req.AssetRef == ref &&
			*req.AddressVersion ==
				entities.AddressVersionV2 &&
			req.ProofCourierAddr == ""
	})).Return(expectedAddr, nil)

	wallet := NewWallet(mc, entities.NetworkRegtest)

	addr, err := wallet.NewReceiveAddress(ctx, ref)
	require.NoError(t, err)
	require.Equal(t, expectedAddr, addr)

	mc.AssertExpectations(t)
}

func TestNewReceiveAddress_UsesDefaultProofCourierAddr(t *testing.T) {
	ctx := context.Background()
	mc := new(mockClient)

	var groupKey entities.PubKey
	copy(
		groupKey[:],
		[]byte("group_key_pubkey_33_bytes_longgg!!"),
	)
	ref := entities.AssetRefFromGroupKey(groupKey)

	courierAddr := "authmailbox+universerpc://tapd.example:10029"
	expectedAddr := &entities.Address{Encoded: "tap1example"}
	mc.On("NewAddr", ctx, mock.MatchedBy(func(
		req *entities.NewAddressRequest) bool {

		if req == nil || req.AddressVersion == nil {
			return false
		}

		return req.AssetRef == ref &&
			*req.AddressVersion ==
				entities.AddressVersionV2 &&
			req.ProofCourierAddr == courierAddr
	})).Return(expectedAddr, nil)

	wallet := NewWallet(
		mc, entities.NetworkRegtest,
		WithDefaultProofCourierAddr(courierAddr),
	)

	addr, err := wallet.NewReceiveAddress(ctx, ref)
	require.NoError(t, err)
	require.Equal(t, expectedAddr, addr)

	mc.AssertExpectations(t)
}
