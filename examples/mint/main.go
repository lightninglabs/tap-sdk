// Example: mint
//
// Demonstrates the full minting lifecycle using tap-sdk:
//  1. Stage a fungible asset in a mint batch.
//  2. Finalize the batch (fund + seal + broadcast).
//  3. Wait for on-chain confirmation.
//  4. Verify the minted asset appears in the wallet.
//
// Prerequisites:
//   - A running tapd instance on regtest with a funded LND wallet.
//   - If using regtest, mine blocks after finalization to confirm.
//
// Usage:
//
//	export TAPD_HOST=localhost:10029
//	export TAPD_MACAROON_PATH=$HOME/.tapd/data/regtest/admin.macaroon
//	go run .
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/lightninglabs/tap-sdk/examples/internal"
)

func main() {
	client := internal.MustConnect()
	defer client.Close()

	network := internal.ParseNetwork(
		internal.EnvOr("TAPD_NETWORK", "regtest"),
	)
	wallet := tapsdk.NewWallet(client, network)

	ctx, cancel := context.WithTimeout(
		context.Background(), 2*time.Minute,
	)
	defer cancel()

	// -------------------------------------------------------
	// Step 1: Stage a fungible asset in the pending batch.
	// -------------------------------------------------------
	fmt.Fprintln(os.Stdout, "Staging asset in mint batch...")
	batch, err := wallet.MintAsset(ctx,
		&entities.MintAssetRequest{
			Asset: &entities.MintAsset{
				PendingMintAsset: entities.PendingMintAsset{
					AssetType: entities.AssetTypeNormal,
					Name:      "example-token",
					Amount:    10_000,
					// NewGroupedAsset creates a new
					// asset group, giving us a group
					// key for future re-issuance.
					NewGroupedAsset: true,
				},
			},
			ShortResponse: true,
		},
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "MintAsset: %v\n", err)
		return
	}
	fmt.Fprintf(os.Stdout, "Batch key: %x\n", batch.BatchKey[:])

	// -------------------------------------------------------
	// Step 2: Finalize the batch.
	// This funds the anchor transaction from LND's wallet,
	// seals the batch, and broadcasts the genesis TX.
	// -------------------------------------------------------
	fmt.Fprintln(os.Stdout, "Finalizing batch...")
	finalized, err := wallet.FinalizeBatch(ctx,
		&entities.FinalizeBatchRequest{
			ShortResponse: true,
		},
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FinalizeBatch: %v\n", err)
		return
	}
	fmt.Fprintf(os.Stdout, "Batch state: %d\n", finalized.State)

	// -------------------------------------------------------
	// Step 3: Wait for on-chain confirmation.
	// On regtest you need to mine blocks manually (e.g. via
	// bitcoin-cli generatetoaddress 6 <addr>). On mainnet /
	// testnet, just wait.
	// -------------------------------------------------------
	fmt.Fprintln(os.Stdout, "Waiting for confirmation (mine blocks on regtest)...")

	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		batches, err := wallet.ListBatches(ctx,
			&entities.ListBatchesRequest{
				BatchKey: &batch.BatchKey,
			},
		)
		if err == nil {
			for _, b := range batches {
				if b.Batch.State ==
					entities.BatchStateFinalized {

					fmt.Fprintf(os.Stdout,
						"Mint confirmed! txid=%s\n",
						b.Batch.BatchTxid,
					)
					goto confirmed
				}
			}
		}
		time.Sleep(2 * time.Second)
	}
	fmt.Fprintln(os.Stderr, "Timed out waiting for confirmation")
	return

confirmed:

	// -------------------------------------------------------
	// Step 4: Verify the asset in the wallet.
	// -------------------------------------------------------
	assets, err := wallet.ListAssets(ctx,
		&entities.ListAssetsRequest{},
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ListAssets: %v\n", err)
		return
	}

	for _, a := range assets {
		if a.Genesis.Tag == "example-token" {
			fmt.Fprintf(os.Stdout,
				"Asset found: id=%s amount=%d\n",
				a.Genesis.AssetID, a.Amount,
			)
			return
		}
	}

	fmt.Fprintln(os.Stderr, "Minted asset not found in wallet")
	return
}
