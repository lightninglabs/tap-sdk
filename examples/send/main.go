// Example: send
//
// Demonstrates an address-based asset send using tap-sdk:
//  1. Mint a fungible asset on Alice.
//  2. Create a receive address on Bob (or same node for self-send).
//  3. Send the asset to the address.
//  4. Verify receipt via ListAssets.
//
// This example uses the high-level Wallet.NewReceiveAddress() which
// creates V2 addresses identified by group key — the recommended
// approach for fungible assets.
//
// Prerequisites:
//   - Two tapd instances (Alice and Bob) on regtest.
//   - Both LND wallets funded.
//   - Set TAPD_HOST / TAPD_BOB_HOST environment variables.
//
// For a simple self-send demo, you can use the same tapd for both.
//
// Usage:
//
//	export TAPD_HOST=localhost:10029        # Alice
//	export TAPD_BOB_HOST=localhost:10030    # Bob (optional)
//	go run .
package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/lightninglabs/tap-sdk/examples/internal"
	tapgrpc "github.com/lightninglabs/tap-sdk/grpc"
)

func main() {
	// Connect to Alice (sender).
	aliceClient := internal.MustConnect()
	defer aliceClient.Close()

	network := internal.ParseNetwork(
		internal.EnvOr("TAPD_NETWORK", "regtest"),
	)
	alice := tapsdk.NewWallet(aliceClient, network)

	// Connect to Bob (receiver). Falls back to Alice for a
	// self-send demo.
	bobHost := internal.EnvOr("TAPD_BOB_HOST", "")
	var bob *tapsdk.Wallet
	if bobHost != "" {
		bobCfg := internal.ConfigFromEnv()
		bobCfg.Host = bobHost

		bobClient, err := tapgrpc.NewClient(bobCfg)
		if err != nil {
			fmt.Fprintf(os.Stderr,
				"Connect to Bob: %v\n", err)
			os.Exit(1)
		}
		defer bobClient.Close()

		bob = tapsdk.NewWallet(bobClient, network)
	} else {
		fmt.Println("No TAPD_BOB_HOST set; using self-send.")
		bob = alice
	}

	ctx, cancel := context.WithTimeout(
		context.Background(), 3*time.Minute,
	)
	defer cancel()

	// -------------------------------------------------------
	// Step 1: Mint a fungible asset on Alice.
	// -------------------------------------------------------
	fmt.Println("Minting asset on Alice...")
	batch, err := alice.MintAsset(ctx,
		&entities.MintAssetRequest{
			Asset: &entities.MintAsset{
				PendingMintAsset: entities.PendingMintAsset{
					AssetType:       entities.AssetTypeNormal,
					Name:            "send-example",
					Amount:          5000,
					NewGroupedAsset: true,
				},
			},
			ShortResponse: true,
		},
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "MintAsset: %v\n", err)
		os.Exit(1)
	}

	_, err = alice.FinalizeBatch(ctx,
		&entities.FinalizeBatchRequest{ShortResponse: true})
	if err != nil {
		fmt.Fprintf(os.Stderr, "FinalizeBatch: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(
		"Batch finalized. Mine blocks on regtest to confirm.",
	)
	waitForMint(ctx, alice, batch.BatchKey)

	// Find the group key of the minted asset. For fungible
	// assets, the group key is the primary identifier.
	assets, err := alice.ListAssets(ctx,
		&entities.ListAssetsRequest{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ListAssets: %v\n", err)
		os.Exit(1)
	}

	var groupKey entities.PubKey
	for _, a := range assets {
		if a.Genesis.Tag == "send-example" && a.GroupKey != nil {
			groupKey = a.GroupKey.RawKey
			break
		}
	}
	if groupKey == (entities.PubKey{}) {
		fmt.Fprintln(os.Stderr, "Minted asset has no group key")
		os.Exit(1)
	}
	fmt.Printf("Group key: %s\n", hex.EncodeToString(groupKey[:]))

	// -------------------------------------------------------
	// Step 2: Create a receive address on Bob.
	// Uses V2 address with group key (recommended for fungible
	// assets).
	// -------------------------------------------------------
	fmt.Println("Creating receive address on Bob...")
	addr, err := bob.NewReceiveAddress(ctx, groupKey)
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"NewReceiveAddress: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Receive address: %s\n", addr.Encoded)

	// -------------------------------------------------------
	// Step 3: Send 1000 units from Alice to Bob's address.
	// -------------------------------------------------------
	fmt.Println("Sending 1000 units...")
	transfer, err := alice.SendAsset(ctx,
		&entities.SendAssetRequest{
			Recipients: []entities.Recipient{
				{
					Address: addr.Encoded,
					Amount:  1000,
				},
			},
		},
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SendAsset: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Transfer anchor TX: %s\n", transfer.AnchorTxid)

	// -------------------------------------------------------
	// Step 4: Mine blocks and verify receipt on Bob.
	// -------------------------------------------------------
	fmt.Println(
		"Mine blocks on regtest, then checking Bob's balance...",
	)
	time.Sleep(5 * time.Second) // Wait for propagation.

	bobAssets, err := bob.ListAssets(ctx,
		&entities.ListAssetsRequest{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Bob ListAssets: %v\n", err)
		os.Exit(1)
	}

	for _, a := range bobAssets {
		if a.Genesis.Tag == "send-example" {
			fmt.Printf("Bob received: amount=%d\n", a.Amount)
		}
	}

	fmt.Println("Done!")
}

// waitForMint polls ListBatches until the batch is finalized.
func waitForMint(ctx context.Context, w *tapsdk.Wallet,
	batchKey entities.PubKey) {

	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		batches, err := w.ListBatches(ctx,
			&entities.ListBatchesRequest{
				BatchKey: &batchKey,
			},
		)
		if err == nil {
			for _, b := range batches {
				if b.Batch.State ==
					entities.BatchStateFinalized {

					fmt.Printf(
						"Mint confirmed: %s\n",
						b.Batch.BatchTxid,
					)
					return
				}
			}
		}
		time.Sleep(2 * time.Second)
	}
	fmt.Fprintln(os.Stderr,
		"Warning: timed out waiting for mint confirmation")
}
