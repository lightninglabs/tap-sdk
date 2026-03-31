// Example: connect
//
// Demonstrates the simplest possible tap-sdk usage: connect to a
// running tapd instance and print basic node information.
//
// Usage:
//
//	export TAPD_HOST=localhost:10029
//	export TAPD_TLS_PATH=$HOME/.tapd/tls.cert
//	export TAPD_MACAROON_PATH=$HOME/.tapd/data/regtest/admin.macaroon
//	go run .
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/lightninglabs/tap-sdk/examples/internal"
)

func main() {
	// Connect to tapd using environment-based configuration.
	client := internal.MustConnect()
	defer client.Close()

	// Create a context with a reasonable timeout.
	ctx, cancel := context.WithTimeout(
		context.Background(), 10*time.Second,
	)
	defer cancel()

	// Fetch node info — this is the equivalent of
	// `tapcli getinfo`.
	info, err := client.GetInfo(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "GetInfo failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Connected to tapd!")
	fmt.Printf("  Version:      %s\n", info.Version)
	fmt.Printf("  LND version:  %s\n", info.LndVersion)
	fmt.Printf("  Network:      %s\n", info.Network)
	fmt.Printf("  Block height: %d\n", info.BlockHeight)
	fmt.Printf("  Synced:       %v\n", info.SyncedToChain)
	fmt.Printf("  Node alias:   %s\n", info.NodeAlias)
}
