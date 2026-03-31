// Example: universe
//
// Demonstrates universe operations using tap-sdk:
//  1. Query universe info.
//  2. List asset roots (all known assets in the universe).
//  3. Get universe statistics.
//  4. List federation servers.
//
// Prerequisites:
//   - A running tapd instance. Works best after minting at least
//     one asset so the universe has data.
//
// Usage:
//
//	export TAPD_HOST=localhost:10029
//	go run .
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/lightninglabs/tap-sdk/examples/internal"
)

func main() {
	client := internal.MustConnect()
	defer client.Close()

	ctx, cancel := context.WithTimeout(
		context.Background(), 30*time.Second,
	)
	defer cancel()

	// -------------------------------------------------------
	// Step 1: Query universe info.
	// -------------------------------------------------------
	fmt.Println("Universe info:")
	info, err := client.Info(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Info: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  Runtime ID: %d\n", info.RuntimeID)

	// -------------------------------------------------------
	// Step 2: List asset roots.
	// Each root represents an asset known to the universe.
	// -------------------------------------------------------
	fmt.Println("\nAsset roots:")
	roots, err := client.AssetRoots(ctx,
		&entities.AssetRootRequest{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "AssetRoots: %v\n", err)
		os.Exit(1)
	}

	if len(roots) == 0 {
		fmt.Println("  (no assets in universe)")
	}
	for key, root := range roots {
		fmt.Printf("  %s:\n", key)
		fmt.Printf("    Asset name: %s\n", root.AssetName)
		if root.MSSMTRoot != nil {
			fmt.Printf("    Root hash:  %s\n",
				root.MSSMTRoot.RootHash)
			fmt.Printf("    Root sum:   %d\n",
				root.MSSMTRoot.RootSum)
		}
	}

	// -------------------------------------------------------
	// Step 3: Get universe statistics.
	// -------------------------------------------------------
	fmt.Println("\nUniverse statistics:")
	stats, err := client.UniverseStats(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "UniverseStats: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  Total syncs:   %d\n", stats.NumTotalSyncs)
	fmt.Printf("  Total proofs:  %d\n", stats.NumTotalProofs)
	fmt.Printf("  Total groups:  %d\n", stats.NumTotalGroups)

	// -------------------------------------------------------
	// Step 4: List federation servers.
	// -------------------------------------------------------
	fmt.Println("\nFederation servers:")
	servers, err := client.ListFederationServers(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"ListFederationServers: %v\n", err)
		os.Exit(1)
	}

	if len(servers) == 0 {
		fmt.Println("  (no federation peers)")
	}
	for _, s := range servers {
		fmt.Printf("  %s (id=%d)\n", s.Host, s.ID)
	}

	fmt.Println("\nDone!")
}
