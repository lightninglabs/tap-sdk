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
	fmt.Fprintln(os.Stdout, "Universe info:")
	info, err := client.Info(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Info: %v\n", err)
		return
	}
	fmt.Fprintf(os.Stdout, "  Runtime ID: %d\n", info.RuntimeID)

	// -------------------------------------------------------
	// Step 2: List asset roots.
	// Each root represents an asset known to the universe.
	// -------------------------------------------------------
	fmt.Fprintln(os.Stdout, "\nAsset roots:")
	roots, err := client.AssetRoots(ctx,
		&entities.AssetRootRequest{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "AssetRoots: %v\n", err)
		return
	}

	if len(roots) == 0 {
		fmt.Fprintln(os.Stdout, "  (no assets in universe)")
	}
	for key, root := range roots {
		fmt.Fprintf(os.Stdout, "  %s:\n", key)
		fmt.Fprintf(os.Stdout, "    Asset name: %s\n", root.AssetName)
		if root.MSSMTRoot != nil {
			fmt.Fprintf(os.Stdout, "    Root hash:  %s\n",
				root.MSSMTRoot.RootHash)
			fmt.Fprintf(os.Stdout, "    Root sum:   %d\n",
				root.MSSMTRoot.RootSum)
		}
	}

	// -------------------------------------------------------
	// Step 3: Get universe statistics.
	// -------------------------------------------------------
	fmt.Fprintln(os.Stdout, "\nUniverse statistics:")
	stats, err := client.UniverseStats(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "UniverseStats: %v\n", err)
		return
	}
	fmt.Fprintf(os.Stdout, "  Total syncs:   %d\n", stats.NumTotalSyncs)
	fmt.Fprintf(os.Stdout, "  Total proofs:  %d\n", stats.NumTotalProofs)
	fmt.Fprintf(os.Stdout, "  Total groups:  %d\n", stats.NumTotalGroups)

	// -------------------------------------------------------
	// Step 4: List federation servers.
	// -------------------------------------------------------
	fmt.Fprintln(os.Stdout, "\nFederation servers:")
	servers, err := client.ListFederationServers(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"ListFederationServers: %v\n", err)
		return
	}

	if len(servers) == 0 {
		fmt.Fprintln(os.Stdout, "  (no federation peers)")
	}
	for _, s := range servers {
		fmt.Fprintf(os.Stdout, "  %s (id=%d)\n", s.Host, s.ID)
	}

	fmt.Fprintln(os.Stdout, "\nDone!")
}
