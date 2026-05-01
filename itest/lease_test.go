//go:build itest

package itest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

// TestUTXOLeaseLifecycle verifies that funding a transfer leases the selected
// asset UTXO and that RemoveUTXOLease releases it again.
func TestUTXOLeaseLifecycle(t *testing.T) {
	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		name := uniqueEventLabel(fmt.Sprintf("lease-token-%s", transport))
		minted, err := h.CreateFungibleAndConfirm(t, ctx, name, 1000)
		require.NoError(t, err)

		addr := h.CreateGroupedReceiveAddress(t, ctx, minted.Ref)
		amount := uint64(100)

		funded, err := h.AliceClient.FundTransfer(ctx,
			[]entities.Recipient{{
				Address: addr.Encoded,
				Amount:  &amount,
			}},
			nil,
		)
		require.NoError(t, err)
		require.NotEmpty(t, funded.FundedPsbt)

		leased := h.WaitForLeasedUTXO(
			t, ctx, minted.Ref, defaultWaitTimeout,
		)
		require.NotEmpty(t, leased.LeaseOwner)
		require.NotZero(t, leased.LeaseExpiryUnix)

		require.NoError(t,
			h.AliceClient.RemoveUTXOLease(ctx, leased.OutPoint),
		)

		require.Eventually(t, func() bool {
			utxos, err := h.AliceClient.ListUtxos(ctx,
				&entities.ListUtxosRequest{
					IncludeLeased: true,
				},
			)
			if err != nil {
				return false
			}

			utxo, ok := utxos[leased.OutPoint.String()]
			if !ok || utxo == nil {
				return false
			}

			return len(utxo.LeaseOwner) == 0 &&
				utxo.LeaseExpiryUnix == 0
		}, defaultWaitTimeout, time.Second)
	})
}

// WaitForLeasedUTXO returns the leased UTXO that contains the requested asset.
func (h *TestHarness) WaitForLeasedUTXO(t testing.TB,
	ctx context.Context, ref entities.AssetRef,
	timeout time.Duration) *entities.ManagedUtxo {

	t.Helper()

	var leased *entities.ManagedUtxo
	require.Eventually(t, func() bool {
		utxos, err := h.AliceClient.ListUtxos(ctx,
			&entities.ListUtxosRequest{
				IncludeLeased: true,
			},
		)
		if err != nil {
			return false
		}

		for _, utxo := range utxos {
			if utxo == nil || len(utxo.LeaseOwner) == 0 {
				continue
			}

			for _, asset := range utxo.Assets {
				if asset != nil && asset.AssetRef == ref {
					leased = utxo
					return true
				}
			}
		}

		return false
	}, timeout, time.Second)

	return leased
}
