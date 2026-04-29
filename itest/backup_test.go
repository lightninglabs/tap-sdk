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

// TestBackupRestore verifies that SDK backup blobs round-trip through tapd and
// surface restored assets through the normal ListAssets API. The backup RPCs
// are tapd-main only relative to the pinned CI image.
func TestBackupRestore(t *testing.T) {
	requireTapdMain(t)

	runForTransports(t, func(t *testing.T, transport Transport) {
		h, ctx := newFundedHarnessFor(t, transport)

		name := uniqueEventLabel(fmt.Sprintf("backup-token-%s", transport))
		minted, err := h.MintAssetAndConfirm(t, ctx,
			&entities.CreateAsset{
				AssetType:     entities.AssetTypeNormal,
				Name:          name,
				InitialSupply: 321,
			},
		)
		require.NoError(t, err)
		require.True(t, minted.Ref.IsAssetIDRef())

		h.SyncAliceGroupsForBackupImport(t, ctx)

		backup, err := h.AliceClient.ExportBackup(
			ctx, entities.BackupModeCompact,
		)
		require.NoError(t, err)
		require.NotEmpty(t, backup)

		imported, err := h.BobClient.ImportBackup(ctx, backup)
		require.NoError(t, err)
		require.Greater(t, imported, uint32(0))

		restored := h.WaitForAssetByTag(
			t, ctx, h.BobClient, name, defaultWaitTimeout,
		)
		require.Equal(t, minted.Asset.Genesis.IssuanceID,
			restored.Genesis.IssuanceID)
		require.Equal(t, minted.Asset.Amount, restored.Amount)

		importedAgain, err := h.BobClient.ImportBackup(ctx, backup)
		require.NoError(t, err)
		require.Zero(t, importedAgain)
	})
}

// SyncAliceGroupsForBackupImport prepares Bob's verifier state for Alice's
// full-wallet backup. ExportBackup includes every unspent Alice asset, not only
// the asset minted in this test, so a reused regtest stack may contain older
// grouped proofs that need their group issuances bootstrapped before import.
func (h *TestHarness) SyncAliceGroupsForBackupImport(t testing.TB,
	ctx context.Context) {

	t.Helper()

	h.EnableUniverseBootstrap(t, ctx)

	groups, err := h.AliceClient.ListGroups(ctx)
	require.NoError(t, err)

	for _, group := range groups {
		if !group.AssetRef.IsGroupRef() {
			continue
		}

		id := entities.UniverseIDFromRef(
			group.AssetRef, entities.ProofTypeIssuance,
		)
		require.Eventuallyf(t, func() bool {
			return h.syncUniverseTarget(ctx, id) == nil
		}, defaultWaitTimeout, time.Second,
			"group universe %s never synced before backup import",
			group.AssetRef)
	}
}
