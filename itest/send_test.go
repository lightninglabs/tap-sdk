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

	// Find the minted asset and resolve its canonical group key. The raw
	// group key returned by ListAssets is not the identifier Bob's address
	// bootstrap path looks up.
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
	groups, err := h.AliceClient.ListGroups(ctx)
	require.NoError(t, err)

	var groupKey entities.PubKey
	var foundGroupKey bool
	for groupKeyHex, group := range groups {
		for _, asset := range group.Assets {
			if asset.ID != assetID {
				continue
			}

			groupKey, err = entities.ParsePubKeyHex(groupKeyHex)
			require.NoError(t, err)
			foundGroupKey = true
			break
		}
		if foundGroupKey {
			break
		}
	}
	require.True(t, foundGroupKey,
		"minted asset group key not found in group listing")
	t.Logf("Minted asset: id=%s, group=%x, amount=%d",
		assetID, groupKey[:], mintedAsset.Amount)

	// Enable issuance federation sync and let Bob bootstrap the canonical
	// group key from Alice when creating the V2 receive address.
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

	bobProofCourierAddr := envOr(
		"TAPD_BOB_PROOF_COURIER_ADDR",
		defaultBobProofCourierAddr,
	)

	v2 := entities.AddressVersionV2
	var bobAddr *entities.Address
	require.Eventually(t, func() bool {
		bobAddr, err = h.BobClient.NewAddr(ctx,
			&entities.NewAddressRequest{
				GroupKey:         &groupKey,
				ProofCourierAddr: bobProofCourierAddr,
				AddressVersion:   &v2,
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
	// Bob should have received 200 units for the minted asset group. Give
	// some time for proof delivery and balance reconciliation.
	balanceReq := &entities.ListBalancesRequest{
		GroupBy:        entities.BalanceGroupByGroupKey,
		GroupKeyFilter: &groupKey,
	}

	var bobBalance uint64
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		balances, err := h.BobClient.ListBalances(ctx, balanceReq)
		if err == nil {
			for _, balance := range balances.AssetGroupBalances {
				if balance != nil && balance.Balance == 200 {
					bobBalance = balance.Balance
					goto verified
				}
			}
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("Bob did not receive the asset group balance within timeout")

verified:
	t.Logf("Bob received %d units for group %x", bobBalance, groupKey)

	// Alice should have change: 5000 - 200 = 4800.
	aliceBalances, err := h.AliceClient.ListBalances(ctx, balanceReq)
	require.NoError(t, err)

	var aliceBalance uint64
	for _, balance := range aliceBalances.AssetGroupBalances {
		if balance != nil {
			aliceBalance += balance.Balance
		}
	}
	require.Equal(t, uint64(4800), aliceBalance,
		"Alice should have 4800 units remaining in the asset group")
	t.Logf("Alice group balance: %d", aliceBalance)
}
