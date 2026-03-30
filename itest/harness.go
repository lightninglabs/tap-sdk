//go:build itest

package itest

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/tap-sdk/entities"
	tapgrpc "github.com/lightninglabs/tap-sdk/grpc"
	"github.com/stretchr/testify/require"
)

const (
	// defaultAliceHost is the default gRPC host for tapd-alice.
	defaultAliceHost = "localhost:10029"

	// defaultBobHost is the default gRPC host for tapd-bob.
	defaultBobHost = "localhost:10030"

	// defaultBitcoindHost is the default RPC host for bitcoind.
	defaultBitcoindHost = "localhost:18443"

	// bitcoindUser is the RPC user for bitcoind.
	bitcoindUser = "devuser"

	// bitcoindPass is the RPC password for bitcoind.
	bitcoindPass = "devpass"

	// miningAddr is a throw-away regtest address for mining rewards.
	// Generated once and hard-coded for simplicity.
	miningAddr = ""

	// defaultMineBlocks is the number of blocks to mine for
	// confirmation.
	defaultMineBlocks = 6
)

// TestHarness holds the two SDK clients (Alice and Bob) and provides
// helper methods for common test operations.
type TestHarness struct {
	t *testing.T

	AliceClient tapsdk.Client
	BobClient   tapsdk.Client

	AliceWallet *tapsdk.Wallet
	BobWallet   *tapsdk.Wallet

	bitcoindHost string
}

// NewTestHarness creates a new TestHarness by connecting to the two
// tapd instances described in docker-compose.yml. It expects the
// containers to already be running and healthy.
func NewTestHarness(t *testing.T) *TestHarness {
	t.Helper()

	aliceHost := envOr("TAPD_ALICE_HOST", defaultAliceHost)
	bobHost := envOr("TAPD_BOB_HOST", defaultBobHost)
	btcHost := envOr("BITCOIND_HOST", defaultBitcoindHost)

	aliceTLSPath := envOr(
		"TAPD_ALICE_TLS",
		extractDockerFile(t, "tap-sdk-tapd-alice",
			"/root/.tapd/tls.cert"),
	)
	bobTLSPath := envOr(
		"TAPD_BOB_TLS",
		extractDockerFile(t, "tap-sdk-tapd-bob",
			"/root/.tapd/tls.cert"),
	)

	aliceMacPath := envOr(
		"TAPD_ALICE_MAC",
		extractDockerFile(t, "tap-sdk-tapd-alice",
			"/root/.tapd/data/regtest/admin.macaroon"),
	)
	bobMacPath := envOr(
		"TAPD_BOB_MAC",
		extractDockerFile(t, "tap-sdk-tapd-bob",
			"/root/.tapd/data/regtest/admin.macaroon"),
	)

	aliceCfg := &tapgrpc.Config{
		Host:        aliceHost,
		Network:     entities.NetworkRegtest,
		TLSPath:     aliceTLSPath,
		MacaroonDir: macDir(aliceMacPath),
	}
	bobCfg := &tapgrpc.Config{
		Host:        bobHost,
		Network:     entities.NetworkRegtest,
		TLSPath:     bobTLSPath,
		MacaroonDir: macDir(bobMacPath),
	}

	aliceClient, err := tapgrpc.NewClient(aliceCfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = aliceClient.Close() })

	bobClient, err := tapgrpc.NewClient(bobCfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = bobClient.Close() })

	aliceWallet := tapsdk.NewWallet(aliceClient, entities.NetworkRegtest)
	bobWallet := tapsdk.NewWallet(bobClient, entities.NetworkRegtest)

	h := &TestHarness{
		t:            t,
		AliceClient:  aliceClient,
		BobClient:    bobClient,
		AliceWallet:  aliceWallet,
		BobWallet:    bobWallet,
		bitcoindHost: btcHost,
	}

	return h
}

// MineBlocks mines the specified number of blocks via bitcoind.
func (h *TestHarness) MineBlocks(n int) {
	h.t.Helper()

	// Get a new address from bitcoind for the mining reward.
	addr := h.bitcoindRPC("getnewaddress", `""`, `"bech32"`)
	addr = strings.TrimSpace(addr)
	addr = strings.Trim(addr, `"`)

	result := h.bitcoindRPC(
		"generatetoaddress", fmt.Sprintf("%d", n),
		fmt.Sprintf(`"%s"`, addr),
	)
	h.t.Logf("Mined %d blocks: %s", n, truncate(result, 120))
}

// FundLndWallet mines blocks to fund the LND wallet and waits for
// synchronization.
func (h *TestHarness) FundLndWallet() {
	h.t.Helper()

	// Mine enough blocks for coinbase maturity (100) + some extra.
	h.MineBlocks(110)

	// Wait for tapd to sync.
	h.WaitForSync(h.AliceClient, 30*time.Second)
	h.WaitForSync(h.BobClient, 30*time.Second)
}

// WaitForSync polls GetInfo until the node reports synced_to_chain.
func (h *TestHarness) WaitForSync(client tapsdk.Client,
	timeout time.Duration) {

	h.t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for {
		info, err := client.GetInfo(ctx)
		if err == nil && info.SyncedToChain {
			return
		}

		select {
		case <-ctx.Done():
			h.t.Fatalf("timed out waiting for sync: %v",
				ctx.Err())
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// WaitForMint polls ListBatches until the given batch key shows
// state FINALIZED, then returns the finalized batch.
func (h *TestHarness) WaitForMint(ctx context.Context,
	client tapsdk.Client, batchKey entities.PubKey,
	timeout time.Duration) []*entities.VerboseMintingBatch {

	h.t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		batches, err := client.ListBatches(ctx,
			&entities.ListBatchesRequest{
				BatchKey: &batchKey,
			},
		)
		if err == nil {
			for _, b := range batches {
				if b.Batch.State ==
					entities.BatchStateFinalized {

					return batches
				}
			}
		}

		select {
		case <-ctx.Done():
			h.t.Fatalf("context cancelled waiting for mint")
		case <-time.After(1 * time.Second):
		}
	}

	h.t.Fatalf("timed out waiting for mint finalization")
	return nil
}

// bitcoindRPC executes a bitcoin-cli RPC command against the regtest
// bitcoind. Returns stdout.
func (h *TestHarness) bitcoindRPC(method string,
	params ...string) string {

	h.t.Helper()

	args := []string{
		"-regtest",
		fmt.Sprintf("-rpcconnect=%s",
			strings.Split(h.bitcoindHost, ":")[0]),
		fmt.Sprintf("-rpcport=%s",
			strings.Split(h.bitcoindHost, ":")[1]),
		fmt.Sprintf("-rpcuser=%s", bitcoindUser),
		fmt.Sprintf("-rpcpassword=%s", bitcoindPass),
		method,
	}
	args = append(args, params...)

	cmd := exec.Command("bitcoin-cli", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		// Fallback: try curl-based JSON-RPC if bitcoin-cli is not
		// available.
		return h.bitcoindCurlRPC(method, params...)
	}

	return stdout.String()
}

// bitcoindCurlRPC performs a JSON-RPC call to bitcoind via curl.
func (h *TestHarness) bitcoindCurlRPC(method string,
	params ...string) string {

	h.t.Helper()

	// Build the JSON-RPC request body.
	jsonParams := make([]json.RawMessage, 0, len(params))
	for _, p := range params {
		jsonParams = append(jsonParams, json.RawMessage(p))
	}

	type rpcRequest struct {
		JSONRPC string            `json:"jsonrpc"`
		ID      string            `json:"id"`
		Method  string            `json:"method"`
		Params  []json.RawMessage `json:"params"`
	}

	reqBody, err := json.Marshal(rpcRequest{
		JSONRPC: "1.0",
		ID:      "itest",
		Method:  method,
		Params:  jsonParams,
	})
	require.NoError(h.t, err)

	url := fmt.Sprintf("http://%s:%s@%s/",
		bitcoindUser, bitcoindPass, h.bitcoindHost)

	cmd := exec.Command("curl", "-s", "-X", "POST",
		"-H", "Content-Type: application/json",
		"-d", string(reqBody), url,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	require.NoError(h.t, err,
		"bitcoind RPC %s failed: %s", method, stderr.String())

	// Parse the JSON-RPC response and return the result field.
	type rpcResponse struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	var resp rpcResponse
	err = json.Unmarshal(stdout.Bytes(), &resp)
	require.NoError(h.t, err)

	if string(resp.Error) != "null" && string(resp.Error) != "" {
		h.t.Fatalf("bitcoind RPC %s error: %s", method,
			string(resp.Error))
	}

	return string(resp.Result)
}

// extractDockerFile copies a file from a running Docker container to a
// temporary directory and returns the local path. The temp file is
// cleaned up when the test finishes. It retries a few times with a
// short delay to handle slow container startup.
func extractDockerFile(t *testing.T, container,
	containerPath string) string {

	t.Helper()

	tmpDir := t.TempDir()
	localPath := tmpDir + "/" + containerPath[strings.LastIndex(
		containerPath, "/")+1:]

	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			t.Logf("Retry %d: docker cp %s:%s",
				attempt, container, containerPath)
			time.Sleep(3 * time.Second)
		}

		cmd := exec.Command("docker", "cp",
			container+":"+containerPath, localPath)
		out, err := cmd.CombinedOutput()
		if err == nil {
			return localPath
		}

		lastErr = fmt.Errorf(
			"docker cp %s:%s failed: %s",
			container, containerPath, string(out),
		)
	}

	require.NoError(t, lastErr,
		"docker cp %s:%s failed after retries",
		container, containerPath)

	return localPath
}

// macDir returns the directory portion of a macaroon file path.
func macDir(path string) string {
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return "."
	}
	return path[:idx]
}

// envOr returns the environment variable value or the fallback.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// truncate shortens a string to at most n characters.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// hexEncode returns the hex encoding of the given bytes.
func hexEncode(b []byte) string {
	return hex.EncodeToString(b)
}
