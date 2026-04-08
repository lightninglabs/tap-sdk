//go:build itest

package itest

import (
	"context"
	"testing"
	"time"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/require"
)

// TestAddressSend verifies the full address-based send flow:
// mint → create address on Bob → send from Alice → mine → verify
// receipt.
func TestAddressSend(t *testing.T) {
	h := NewTestHarness(t)
	ctx := context.Background()

	// Fund Alice so she can mint and send.
	h.FundLndWallet()

	// --- Step 1: Alice mints a fungible asset with a group key ---
	batch, err := h.AliceClient.MintAsset(ctx,
		&entities.MintAssetRequest{
			Asset: &entities.MintAsset{
				PendingMintAsset: entities.PendingMintAsset{
					AssetType:       entities.AssetTypeNormal,
					Name:            "send-token",
					Amount:          5000,
					NewGroupedAsset: true,
				},
			},
			ShortResponse: true,
		},
	)
	require.NoError(t, err)

	_, err = h.AliceClient.FinalizeBatch(ctx,
		&entities.FinalizeBatchRequest{ShortResponse: true})
	require.NoError(t, err)

	h.MineBlocks(defaultMineBlocks)
	h.WaitForMint(ctx, h.AliceClient, batch.BatchKey,
		60*time.Second)

	// Find the minted asset to get its group key.
	assets, err := h.AliceClient.ListAssets(ctx,
		&entities.ListAssetsRequest{})
	require.NoError(t, err)

	var mintedAsset *entities.Asset
	for _, a := range assets {
		if a.Genesis.Tag == "send-token" {
			mintedAsset = a
			break
		}
	}
	require.NotNil(t, mintedAsset, "minted asset not found")
	require.NotNil(t, mintedAsset.GroupKey,
		"minted asset should have group key")

	assetID := mintedAsset.Genesis.AssetID
	groupKey := mintedAsset.GroupKey.RawKey
	t.Logf("Minted asset: id=%s, group=%x, amount=%d",
		assetID, groupKey[:], mintedAsset.Amount)

	// Allow issuance proof sync on both nodes, then add Alice as Bob's
	// universe federation peer so the normal group-key bootstrap path can
	// resolve the fungible asset before Bob creates a V2 address.
	issuanceSync := []entities.GlobalFederationSyncConfig{
		{
			ProofType:       entities.ProofTypeIssuance,
			AllowSyncInsert: true,
			AllowSyncExport: true,
		},
	}
	err = h.AliceClient.SetFederationSyncConfig(ctx, issuanceSync, nil)
	require.NoError(t, err)

	err = h.BobClient.SetFederationSyncConfig(ctx, issuanceSync, nil)
	require.NoError(t, err)

	aliceUniverseHost := envOr(
		"TAPD_ALICE_UNIVERSE_HOST",
		defaultAliceUniverseHost,
	)
	err = h.BobClient.AddFederationServer(ctx, []entities.FederationServer{{
		Host: aliceUniverseHost,
	}})
	require.NoError(t, err)

	// Address creation can still lag slightly behind federation bootstrap,
	// so keep triggering the targeted issuance sync until Bob can observe
	// the grouped universe root and create the V2 receive address.
	syncReq := &entities.SyncRequest{
		UniverseHost: aliceUniverseHost,
		SyncMode:     entities.SyncIssuanceOnly,
		SyncTargets: []entities.SyncTarget{{
			ID: entities.UniverseID{
				GroupKey:  &groupKey,
				ProofType: entities.ProofTypeIssuance,
			},
		}},
	}
	uniID := entities.UniverseID{
		GroupKey:  &groupKey,
		ProofType: entities.ProofTypeIssuance,
	}

	v2 := entities.AddressVersionV2
	var bobAddr *entities.Address
	require.Eventually(t, func() bool {
		synced, syncErr := h.BobClient.SyncUniverse(ctx, syncReq)
		if syncErr != nil {
			t.Logf("Bob universe sync failed: %v", syncErr)
			return false
		}

		if len(synced) == 0 {
			t.Log("Bob universe sync returned no diff")
		}

		roots, rootsErr := h.BobClient.QueryAssetRoots(ctx, &uniID)
		if rootsErr != nil {
			t.Logf("Bob group root not ready yet: %v", rootsErr)
			return false
		}
		if roots.IssuanceRoot == nil {
			t.Log("Bob group issuance root not ready yet")
			return false
		}

		bobAddr, err = h.BobClient.NewAddr(ctx,
			&entities.NewAddressRequest{
				GroupKey:       &groupKey,
				AddressVersion: &v2,
			},
		)
		if err != nil {
			t.Logf("Bob address bootstrap not ready yet: %v", err)
			return false
		}

		return true
	}, 45*time.Second, time.Second)
	require.NotNil(t, bobAddr)
	require.NotEmpty(t, bobAddr.Encoded)
	t.Logf("Bob address: %s", bobAddr.Encoded)

	// --- Step 3: Alice sends 200 units to Bob's address ---
	transfer, err := h.AliceClient.SendAsset(ctx,
		&entities.SendAssetRequest{
			Recipients: []entities.Recipient{{
				Address: bobAddr.Encoded,
				Amount:  200,
			}},
		},
	)
	require.NoError(t, err)
	require.NotEmpty(t, transfer.AnchorTxid)
	t.Logf("Send tx: %s", transfer.AnchorTxid)

	// --- Step 4: Mine blocks to confirm ---
	h.MineBlocks(defaultMineBlocks)

	// Wait for both nodes to sync.
	h.WaitForSync(h.AliceClient, 30*time.Second)
	h.WaitForSync(h.BobClient, 30*time.Second)

	// --- Step 5: Verify balances ---
	// Bob should have received 200 units. Give some time for proof
	// delivery.
	var bobAssets []*entities.Asset
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		bobAssets, err = h.BobClient.ListAssets(ctx,
			&entities.ListAssetsRequest{})
		if err == nil {
			for _, a := range bobAssets {
				if a.Genesis.AssetID == assetID &&
					a.Amount == 200 {

					goto verified
				}
			}
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("Bob did not receive the asset within timeout")

verified:
	t.Logf("Bob received 200 units of %s", assetID)

	// Alice should have change: 5000 - 200 = 4800.
	aliceAssets, err := h.AliceClient.ListAssets(ctx,
		&entities.ListAssetsRequest{})
	require.NoError(t, err)

	var aliceBalance uint64
	for _, a := range aliceAssets {
		if a.Genesis.AssetID == assetID {
			aliceBalance += a.Amount
		}
	}
	require.Equal(t, uint64(4800), aliceBalance,
		"Alice should have 4800 units remaining")
	t.Logf("Alice balance: %d", aliceBalance)
}
