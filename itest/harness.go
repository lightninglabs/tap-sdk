//go:build itest

package itest

import (
	"context"
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
	// defaultAliceHost is the default host-side gRPC address for tapd-alice.
	defaultAliceHost = "localhost:10029"

	// defaultAliceUniverseHost is the address tapd-bob should use when syncing
	// directly from tapd-alice over the Docker network.
	defaultAliceUniverseHost = "tapd-alice:10029"

	// defaultBobHost is the default gRPC host for tapd-bob.
	defaultBobHost = "localhost:10030"

	// defaultBobProofCourierAddr is the default proof courier address Bob uses
	// for V2 receive addresses.
	defaultBobProofCourierAddr = "authmailbox+universerpc://tapd-bob:10029"

	// defaultBitcoindHost is the default RPC host for bitcoind.
	defaultBitcoindHost = "localhost:18443"

	// bitcoindUser is the RPC user for bitcoind.
	bitcoindUser = "devuser"

	// bitcoindPass is the RPC password for bitcoind.
	bitcoindPass = "devpass"

	// defaultMineBlocks is the number of blocks to mine for confirmation.
	defaultMineBlocks = 6

	// defaultSyncTimeout is the short sync window we use after mining.
	defaultSyncTimeout = 30 * time.Second

	// defaultWaitTimeout is the generic regtest wait window for bootstrap and
	// visibility checks.
	defaultWaitTimeout = 60 * time.Second

	// defaultGroupBalanceTimeout gives grouped balances more slack because
	// group-key accounting settles slower than collectible balances.
	defaultGroupBalanceTimeout = 3 * time.Minute

	// defaultCollectibleBalanceTimeout is enough for collectible balances to
	// settle in regtest.
	defaultCollectibleBalanceTimeout = 60 * time.Second
)

// tapdNodeConfig describes the connection inputs needed to open a tapd client.
type tapdNodeConfig struct {
	hostEnv     string
	defaultHost string
	tlsEnv      string
	macaroonEnv string
	container   string
}

// TestHarness holds the SDK clients and helper state shared by the regtest
// integration tests.
type TestHarness struct {
	AliceClient tapsdk.Client
	BobClient   tapsdk.Client

	AliceWallet *tapsdk.Wallet
	BobWallet   *tapsdk.Wallet

	bitcoindHost string
}

// NewTestHarness creates a new TestHarness by connecting to the regtest
// services described in docker-compose.yml.
func NewTestHarness(t testing.TB) *TestHarness {
	t.Helper()

	aliceClient := newTapdClient(t, tapdNodeConfig{
		hostEnv:     "TAPD_ALICE_HOST",
		defaultHost: defaultAliceHost,
		tlsEnv:      "TAPD_ALICE_TLS",
		macaroonEnv: "TAPD_ALICE_MAC",
		container:   "tap-sdk-tapd-alice",
	})

	bobProofCourierAddr := envOr(
		"TAPD_BOB_PROOF_COURIER_ADDR",
		defaultBobProofCourierAddr,
	)
	bobClient := newTapdClient(t, tapdNodeConfig{
		hostEnv:     "TAPD_BOB_HOST",
		defaultHost: defaultBobHost,
		tlsEnv:      "TAPD_BOB_TLS",
		macaroonEnv: "TAPD_BOB_MAC",
		container:   "tap-sdk-tapd-bob",
	})

	aliceWallet := tapsdk.NewWallet(aliceClient, entities.NetworkRegtest)
	bobWallet := tapsdk.NewWallet(
		bobClient,
		entities.NetworkRegtest,
		tapsdk.WithDefaultProofCourierAddr(bobProofCourierAddr),
	)

	return &TestHarness{
		AliceClient:  aliceClient,
		BobClient:    bobClient,
		AliceWallet:  aliceWallet,
		BobWallet:    bobWallet,
		bitcoindHost: envOr("BITCOIND_HOST", defaultBitcoindHost),
	}
}

func newTapdClient(t testing.TB, spec tapdNodeConfig) tapsdk.Client {
	t.Helper()

	tlsPath := os.Getenv(spec.tlsEnv)
	if tlsPath == "" {
		tlsPath = extractDockerFile(t, spec.container, "/root/.tapd/tls.cert")
	}

	macaroonPath := os.Getenv(spec.macaroonEnv)
	if macaroonPath == "" {
		macaroonPath = extractDockerFile(
			t,
			spec.container,
			"/root/.tapd/data/regtest/admin.macaroon",
		)
	}

	cfg := &tapgrpc.Config{
		Host:         envOr(spec.hostEnv, spec.defaultHost),
		Network:      entities.NetworkRegtest,
		TLSPath:      tlsPath,
		MacaroonPath: macaroonPath,
	}

	client, err := tapgrpc.NewClient(cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = client.Close()
	})

	return client
}

// WaitForSync polls GetInfo until the node reports synced_to_chain.
func (h *TestHarness) WaitForSync(t testing.TB, ctx context.Context,
	client tapsdk.Client, timeout time.Duration) {

	t.Helper()

	require.Eventually(t, func() bool {
		info, err := client.GetInfo(ctx)
		return err == nil && info.SyncedToChain
	}, timeout, 500*time.Millisecond)
}

// WaitForMint polls ListBatches until the given batch key reaches FINALIZED.
func (h *TestHarness) WaitForMint(
	t testing.TB, ctx context.Context, client tapsdk.Client,
	batchKey entities.PubKey, timeout time.Duration,
) *entities.VerboseMintingBatch {

	t.Helper()

	var finalized *entities.VerboseMintingBatch
	require.Eventually(t, func() bool {
		batches, err := client.ListBatches(ctx,
			&entities.ListBatchesRequest{
				BatchKey: &batchKey,
			},
		)
		if err != nil || len(batches) != 1 {
			return false
		}

		batch := batches[0]
		if batch == nil || batch.Batch.State !=
			entities.BatchStateFinalized {

			return false
		}

		finalized = batch
		return true
	}, timeout, time.Second)

	return finalized
}

// WaitForAssetByTag polls ListAssets until the asset with the given tag is
// visible in the wallet.
func (h *TestHarness) WaitForAssetByTag(t testing.TB, ctx context.Context,
	client tapsdk.Client, tag string,
	timeout time.Duration) *entities.Asset {

	t.Helper()

	var found *entities.Asset
	require.Eventually(t, func() bool {
		assets, err := client.ListAssets(ctx,
			&entities.ListAssetsRequest{},
		)
		if err != nil {
			return false
		}

		for _, candidate := range assets {
			if candidate != nil && candidate.Genesis.Tag == tag {
				found = candidate
				return true
			}
		}

		return false
	}, timeout, time.Second)

	return found
}

// WaitForBalance polls Wallet.GetBalance until the given asset reaches the
// expected amount.
func (h *TestHarness) WaitForBalance(t testing.TB, ctx context.Context,
	wallet *tapsdk.Wallet, ref entities.AssetRef, amount uint64,
	timeout time.Duration) uint64 {

	t.Helper()

	var balance uint64
	require.Eventually(t, func() bool {
		resp, err := wallet.ListBalances(ctx, &entities.ListBalancesRequest{
			AssetRef: &ref,
		})
		if err != nil {
			t.Logf("balance lookup not ready for %s: %v", ref, err)
			return false
		}

		assetBalance, ok := resp.Balances[ref.String()]
		if !ok {
			t.Logf("balance for %s not visible yet (want %d, "+
				"unconfirmed=%d)", ref, amount,
				resp.UnconfirmedTransfers)
			balance = 0
			return false
		}

		balance = assetBalance.Balance
		if balance != amount {
			t.Logf("balance for %s = %d, want %d (unconfirmed=%d)",
				ref, balance, amount,
				resp.UnconfirmedTransfers)
		}

		return balance == amount
	}, timeout, time.Second)

	return balance
}

// WaitForReceiveAddress retries Wallet.NewReceiveAddress until the receiver is
// ready to bootstrap the requested asset.
func (h *TestHarness) WaitForReceiveAddress(
	t testing.TB, ctx context.Context, wallet *tapsdk.Wallet,
	ref entities.AssetRef, timeout time.Duration,
) *entities.Address {

	t.Helper()

	var addr *entities.Address
	require.Eventually(t, func() bool {
		candidate, err := wallet.NewReceiveAddress(ctx, ref)
		if err != nil {
			t.Logf("receive address bootstrap not ready for %s: %v",
				ref, err)
			return false
		}

		addr = candidate
		return true
	}, timeout, time.Second)

	return addr
}

// EnableUniverseBootstrap configures both tapd nodes for the regtest universe
// sync path used by the V2 receive-address flow.
func (h *TestHarness) EnableUniverseBootstrap(t testing.TB,
	ctx context.Context) {
	t.Helper()

	globalSync := []entities.GlobalFederationSyncConfig{
		{
			ProofType:       entities.ProofTypeIssuance,
			AllowSyncInsert: true,
			AllowSyncExport: true,
		},
		{
			ProofType:       entities.ProofTypeTransfer,
			AllowSyncInsert: true,
			AllowSyncExport: true,
		},
	}

	require.NoError(t,
		h.AliceClient.SetFederationSyncConfig(ctx, globalSync, nil),
	)
	require.NoError(t,
		h.BobClient.SetFederationSyncConfig(ctx, globalSync, nil),
	)

	aliceUniverseHost := envOr(
		"TAPD_ALICE_UNIVERSE_HOST",
		defaultAliceUniverseHost,
	)
	require.NoError(t, h.BobClient.AddFederationServer(ctx,
		[]entities.FederationServer{{Host: aliceUniverseHost}}),
	)
}

// WaitForGroupBootstrap waits for Bob to learn the issuance universe for a
// grouped asset before creating a V2 receive address.
func (h *TestHarness) WaitForGroupBootstrap(t testing.TB,
	ctx context.Context,
	ref entities.AssetRef, timeout time.Duration) {

	t.Helper()
	require.True(t, ref.IsGroupRef(),
		"group bootstrap requires a fungible group ref")

	issuanceID := entities.UniverseIDFromRef(ref,
		entities.ProofTypeIssuance)

	require.Eventually(t, func() bool {
		_, err := h.BobClient.SyncUniverse(ctx,
			&entities.SyncRequest{
				UniverseHost: envOr(
					"TAPD_ALICE_UNIVERSE_HOST",
					defaultAliceUniverseHost,
				),
				SyncMode: entities.SyncIssuanceOnly,
				SyncTargets: []entities.SyncTarget{{
					ID: issuanceID,
				}},
			},
		)
		if err != nil {
			t.Logf("group bootstrap sync not ready for %s: %v",
				ref, err)
			return false
		}

		roots, err := h.BobClient.QueryAssetRoots(ctx,
			&issuanceID)
		if err != nil {
			t.Logf("group bootstrap roots not ready for %s: %v",
				ref, err)
			return false
		}

		return roots.IssuanceRoot != nil
	}, timeout, time.Second)
}

// CreateGroupedReceiveAddress bootstraps Bob for a grouped fungible receive
// flow, then creates the V2 receive address.
func (h *TestHarness) CreateGroupedReceiveAddress(t testing.TB,
	ctx context.Context, ref entities.AssetRef) *entities.Address {

	t.Helper()

	h.EnableUniverseBootstrap(t, ctx)
	h.WaitForGroupBootstrap(t, ctx, ref, defaultWaitTimeout)

	return h.WaitForReceiveAddress(
		t, ctx, h.BobWallet, ref, defaultWaitTimeout,
	)
}

// envOr returns the environment variable value or the fallback.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// extractDockerFile copies a file from a running Docker container to a
// temporary directory and returns the local path. The temp file is cleaned up
// when the test finishes. It retries a few times with a short delay to handle
// slow container startup.
func extractDockerFile(t testing.TB, container,
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

		lastErr = fmt.Errorf("docker cp %s:%s failed: %s",
			container, containerPath, string(out))
	}

	require.NoError(t, lastErr,
		"docker cp %s:%s failed after retries",
		container, containerPath)

	return localPath
}

// truncate shortens a string to at most n characters.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// balanceTimeoutFor returns the timeout we use when waiting for a confirmed
// wallet balance after minting.
func balanceTimeoutFor(ref entities.AssetRef) time.Duration {
	if ref.IsGroupRef() {
		return defaultGroupBalanceTimeout
	}

	return defaultCollectibleBalanceTimeout
}
