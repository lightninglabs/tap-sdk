//go:build itest

package itest

import (
	"context"
	"testing"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

// TestErrorHandling verifies the SDK returns proper errors for invalid input.
func TestErrorHandling(t *testing.T) {
	h := NewTestHarness(t)
	ctx := context.Background()

	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "invalid send address",
			run: func() error {
				_, err := h.AliceWallet.Send(ctx, "not-a-valid-address", 1)
				return err
			},
		},
		{
			name: "non-existent proof",
			run: func() error {
				fakeID := entities.AssetID{}
				fakePubKey := entities.PubKey{}
				_, err := h.AliceClient.ExportProof(ctx, fakeID, fakePubKey,
					nil)
				return err
			},
		},
		{
			name: "invalid proof decode",
			run: func() error {
				_, err := h.AliceClient.DecodeProof(ctx, []byte{0x00, 0x01})
				return err
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			require.Error(t, tc.run())
		})
	}
}
