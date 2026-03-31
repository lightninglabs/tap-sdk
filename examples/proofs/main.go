// Example: proofs
//
// Demonstrates proof operations using tap-sdk:
//  1. Mint an asset.
//  2. Export the proof file for the minted asset.
//  3. Unpack the proof file into individual proofs.
//  4. Decode a proof to inspect its contents.
//  5. Verify the proof file.
//
// Prerequisites:
//   - A running tapd instance on regtest with a funded LND wallet.
//   - Mine blocks after minting to confirm.
//
// Usage:
//
//	export TAPD_HOST=localhost:10029
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
)

func main() {
	client := internal.MustConnect()
	defer client.Close()

	network := internal.ParseNetwork(
		internal.EnvOr("TAPD_NETWORK", "regtest"),
	)
	wallet := tapsdk.NewWallet(client, network)

	ctx, cancel := context.WithTimeout(
		context.Background(), 3*time.Minute,
	)
	defer cancel()

	// -------------------------------------------------------
	// Step 1: Mint an asset so we have something to prove.
	// -------------------------------------------------------
	fmt.Println("Minting asset...")
	batch, err := wallet.MintAsset(ctx,
		&entities.MintAssetRequest{
			Asset: &entities.MintAsset{
				PendingMintAsset: entities.PendingMintAsset{
					AssetType: entities.AssetTypeNormal,
					Name:      "proof-example",
					Amount:    100,
				},
			},
			ShortResponse: true,
		},
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "MintAsset: %v\n", err)
		os.Exit(1)
	}

	_, err = wallet.FinalizeBatch(ctx,
		&entities.FinalizeBatchRequest{ShortResponse: true})
	if err != nil {
		fmt.Fprintf(os.Stderr, "FinalizeBatch: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Waiting for mint confirmation...")
	waitForMint(ctx, wallet, batch.BatchKey)

	// Find the minted asset.
	assets, err := wallet.ListAssets(ctx,
		&entities.ListAssetsRequest{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ListAssets: %v\n", err)
		os.Exit(1)
	}

	var target *entities.Asset
	for _, a := range assets {
		if a.Genesis.Tag == "proof-example" {
			target = a
			break
		}
	}
	if target == nil {
		fmt.Fprintln(os.Stderr, "Minted asset not found")
		os.Exit(1)
	}

	fmt.Printf("Asset: id=%s scriptKey=%s\n",
		target.Genesis.AssetID,
		hex.EncodeToString(target.ScriptKey.PubKey[:]),
	)

	// -------------------------------------------------------
	// Step 2: Export the proof file.
	// ExportProof takes the asset ID, the script key's public
	// key, and an optional outpoint filter.
	// -------------------------------------------------------
	fmt.Println("\nExporting proof file...")
	proofFile, err := wallet.ExportProof(
		ctx, target.Genesis.AssetID,
		target.ScriptKey.PubKey, nil,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ExportProof: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Proof file: %d bytes\n",
		len(proofFile.RawProofFile))

	// -------------------------------------------------------
	// Step 3: Unpack the proof file into individual proofs.
	// -------------------------------------------------------
	fmt.Println("\nUnpacking proof file...")
	rawProofs, err := wallet.UnpackProofFile(
		ctx, proofFile.RawProofFile,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "UnpackProofFile: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Contains %d proof(s)\n", len(rawProofs))

	// -------------------------------------------------------
	// Step 4: Decode the last proof (most recent state).
	// -------------------------------------------------------
	if len(rawProofs) > 0 {
		fmt.Println("\nDecoding last proof...")
		decoded, err := wallet.DecodeProof(
			ctx, rawProofs[len(rawProofs)-1],
		)
		if err != nil {
			fmt.Fprintf(os.Stderr,
				"DecodeProof: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("  Asset ID:    %s\n", decoded.AssetID)
		fmt.Printf("  Amount:      %d\n", decoded.Amount)
		fmt.Printf("  Outpoint:    %s\n", decoded.Outpoint)
		fmt.Printf("  Script key:  %s\n",
			hex.EncodeToString(decoded.ScriptKey[:]))

		if decoded.GroupKey != nil {
			fmt.Printf("  Group key:   %s\n",
				hex.EncodeToString(
					decoded.GroupKey[:],
				))
		}
	}

	// -------------------------------------------------------
	// Step 5: Verify the proof file.
	// -------------------------------------------------------
	fmt.Println("\nVerifying proof file...")
	verifyResp, err := wallet.VerifyProof(
		ctx, proofFile.RawProofFile,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "VerifyProof: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Proof valid: %v\n", verifyResp.Valid)

	fmt.Println("\nDone!")
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
						"Confirmed: %s\n",
						b.Batch.BatchTxid,
					)
					return
				}
			}
		}
		time.Sleep(2 * time.Second)
	}
	fmt.Fprintln(os.Stderr,
		"Warning: timed out waiting for confirmation")
}
