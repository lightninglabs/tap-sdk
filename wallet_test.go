package tapsdk

import (
	"context"
	"errors"
	"testing"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewReceiveAddress_UsesGroupKeyAndV2(t *testing.T) {
	mc := new(mockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	groupKey := entities.PubKey{2, 1, 2, 3}
	expectedAddr := &entities.Address{
		Encoded:        "taprt1qqtestaddress",
		GroupKey:       &groupKey,
		AddressVersion: entities.AddressVersionV2,
	}

	mc.On("NewAddr", ctx, mock.MatchedBy(
		func(req *entities.NewAddressRequest) bool {
			if req == nil || req.GroupKey == nil ||
				req.AddressVersion == nil {

				return false
			}

			return req.AssetID == nil &&
				req.Amount == 0 &&
				*req.GroupKey == groupKey &&
				*req.AddressVersion ==
					entities.AddressVersionV2
		},
	)).Return(expectedAddr, nil)

	addr, err := w.NewReceiveAddress(ctx, groupKey)
	require.NoError(t, err)
	require.Equal(t, expectedAddr, addr)

	mc.AssertExpectations(t)
}

func TestNewReceiveAddress_Error(t *testing.T) {
	mc := new(mockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	groupKey := entities.PubKey{2, 9, 9, 9}
	expectedErr := errors.New("new address failed")

	mc.On("NewAddr", ctx, mock.Anything).Return(nil, expectedErr)

	_, err := w.NewReceiveAddress(ctx, groupKey)
	require.Error(t, err)
	require.ErrorIs(t, err, expectedErr)

	var sdkErr *Error
	require.ErrorAs(t, err, &sdkErr)
	require.Equal(t, "NewReceiveAddress", sdkErr.Op)

	mc.AssertExpectations(t)
}

func TestDeriveKeys(t *testing.T) {
	ctx := context.Background()

	scriptKey := &entities.ScriptKey{
		PubKey: entities.PubKey{2, 3, 4, 5},
		KeyDesc: entities.KeyDescriptor{
			RawKeyBytes: entities.PubKey{2, 6, 7, 8},
			KeyLocator: entities.KeyLocator{
				Family: entities.TaprootAssetsKeyFamily,
				Index:  7,
			},
		},
	}
	internalKey := &entities.InternalKey{
		PubKey: entities.PubKey{2, 10, 11, 12},
		KeyLocator: entities.KeyLocator{
			Family: entities.TaprootAssetsKeyFamily,
			Index:  8,
		},
	}

	tests := []struct {
		name        string
		scriptKey   *entities.ScriptKey
		scriptErr   error
		internalKey *entities.InternalKey
		internalErr error
	}{
		{
			name:        "success",
			scriptKey:   scriptKey,
			internalKey: internalKey,
		},
		{
			name:      "script key error",
			scriptErr: errors.New("derive script key failed"),
		},
		{
			name:        "internal key error",
			scriptKey:   scriptKey,
			internalErr: errors.New("derive internal key failed"),
		},
	}

	for _, testCase := range tests {
		tc := testCase
		t.Run(tc.name, func(t *testing.T) {
			mc := new(mockClient)
			w := NewWallet(mc, entities.NetworkRegtest)

			mc.On("DeriveScriptKey", ctx).Return(
				tc.scriptKey, tc.scriptErr,
			)
			if tc.scriptErr == nil {
				mc.On("DeriveInternalKey", ctx).Return(
					tc.internalKey, tc.internalErr,
				)
			}

			derived, err := w.DeriveKeys(ctx)
			if tc.scriptErr != nil || tc.internalErr != nil {
				require.Error(t, err)

				var sdkErr *Error
				require.ErrorAs(t, err, &sdkErr)
				require.Equal(t, "DeriveKeys", sdkErr.Op)

				if tc.scriptErr != nil {
					require.ErrorIs(t, err, tc.scriptErr)
				} else {
					require.ErrorIs(t, err, tc.internalErr)
				}
			} else {
				require.NoError(t, err)
				require.Equal(t, &entities.DerivedKeys{
					ScriptKey:   *scriptKey,
					InternalKey: *internalKey,
				}, derived)
			}

			mc.AssertExpectations(t)
		})
	}
}

func TestWalletClose(t *testing.T) {
	mc := new(mockClient)
	w := NewWallet(mc, entities.NetworkRegtest)

	expectedErr := errors.New("close failed")
	mc.On("Close").Return(expectedErr)

	err := w.Close()
	require.ErrorIs(t, err, expectedErr)

	mc.AssertExpectations(t)
}
