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
		return
	}

	fmt.Fprintln(os.Stdout, "Connected to tapd!")
	fmt.Fprintf(os.Stdout, "  Version:      %s\n", info.Version)
	fmt.Fprintf(os.Stdout, "  LND version:  %s\n", info.LndVersion)
	fmt.Fprintf(os.Stdout, "  Network:      %s\n", info.Network)
	fmt.Fprintf(os.Stdout, "  Block height: %d\n", info.BlockHeight)
	fmt.Fprintf(os.Stdout, "  Synced:       %v\n", info.SyncedToChain)
	fmt.Fprintf(os.Stdout, "  Node alias:   %s\n", info.NodeAlias)
}
