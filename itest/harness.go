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
	tapgrpc "github.com/lightninglabs/tap-sdk/grpc"
	taprest "github.com/lightninglabs/tap-sdk/rest"
	"github.com/stretchr/testify/require"
)

const (
	// defaultAliceHost is the default host-side gRPC address for tapd-alice.
	defaultAliceHost = "localhost:10029"

	// defaultAliceRestHost is the default host-side REST address for
	// tapd-alice.
	defaultAliceRestHost = "https://localhost:8089"

	// defaultAliceUniverseHost is the address tapd-bob should use when syncing
	// directly from tapd-alice over the Docker network.
	defaultAliceUniverseHost = "tapd-alice:10029"

	// defaultBobHost is the default gRPC host for tapd-bob.
	defaultBobHost = "localhost:10030"

	// defaultBobRestHost is the default REST host for tapd-bob.
	defaultBobRestHost = "https://localhost:8090"

	// defaultBobProofCourierHost is the auth mailbox endpoint Bob uses for V2
	// receive addresses.
	defaultBobProofCourierHost = "tapd-bob:10029"

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

	// defaultRESTTimeout gives long-running REST calls such as full-wallet
	// compact backup import enough time on reused itest stacks.
	defaultRESTTimeout = 2 * time.Minute

	// defaultGroupBalanceTimeout gives grouped balances more slack because
	// group-key accounting settles slower than collectible balances.
	defaultGroupBalanceTimeout = 3 * time.Minute

	// defaultCollectibleBalanceTimeout is enough for collectible balances to
	// settle in regtest.
	defaultCollectibleBalanceTimeout = 60 * time.Second
)

// Transport selects the SDK client transport used by the harness.
type Transport string

const (
	// TransportGRPC creates the SDK client via the grpc package.
	TransportGRPC Transport = "grpc"

	// TransportREST creates the SDK client via the rest package.
	TransportREST Transport = "rest"
)

// tapdNodeConfig describes the connection inputs needed to open a tapd client.
type tapdNodeConfig struct {
	grpcHostEnv     string
	defaultGRPCHost string
	restHostEnv     string
	defaultRESTHost string
	tlsEnv          string
	macaroonEnv     string
	container       string
}

// TestHarness holds the SDK clients and helper state shared by the regtest
// integration tests.
type TestHarness struct {
	AliceClient tapsdk.Client
	BobClient   tapsdk.Client

	AliceWallet *tapsdk.Wallet
	BobWallet   *tapsdk.Wallet

	Transport    Transport
	bitcoindHost string
}

// NewTestHarness creates a new TestHarness over the gRPC transport.
func NewTestHarness(t testing.TB) *TestHarness {
	t.Helper()

	return NewTestHarnessWithTransport(t, TransportGRPC)
}

// NewTestHarnessWithTransport creates a harness whose SDK clients speak
// the requested transport.
func NewTestHarnessWithTransport(t testing.TB,
	transport Transport) *TestHarness {

	t.Helper()

	aliceSpec := tapdNodeConfig{
		grpcHostEnv:     "TAPD_ALICE_HOST",
		defaultGRPCHost: defaultAliceHost,
		restHostEnv:     "TAPD_ALICE_REST",
		defaultRESTHost: defaultAliceRestHost,
		tlsEnv:          "TAPD_ALICE_TLS",
		macaroonEnv:     "TAPD_ALICE_MAC",
		container:       "tap-sdk-tapd-alice",
	}

	bobSpec := tapdNodeConfig{
		grpcHostEnv:     "TAPD_BOB_HOST",
		defaultGRPCHost: defaultBobHost,
		restHostEnv:     "TAPD_BOB_REST",
		defaultRESTHost: defaultBobRestHost,
		tlsEnv:          "TAPD_BOB_TLS",
		macaroonEnv:     "TAPD_BOB_MAC",
		container:       "tap-sdk-tapd-bob",
	}

	aliceClient := newTapdClient(t, transport, aliceSpec)
	bobClient := newTapdClient(t, transport, bobSpec)

	bobWalletOpt := tapsdk.WithAuthMailboxCourier(
		defaultBobProofCourierHost,
	)

	if bobProofCourierAddr := os.Getenv(
		"TAPD_BOB_PROOF_COURIER_ADDR",
	); bobProofCourierAddr != "" {

		bobWalletOpt = tapsdk.WithDefaultProofCourierAddr(
			bobProofCourierAddr,
		)
	}

	aliceWallet := tapsdk.NewWallet(aliceClient, tapsdk.NetworkRegtest)
	bobWallet := tapsdk.NewWallet(
		bobClient,
		tapsdk.NetworkRegtest,
		bobWalletOpt,
	)

	return &TestHarness{
		AliceClient:  aliceClient,
		BobClient:    bobClient,
		AliceWallet:  aliceWallet,
		BobWallet:    bobWallet,
		Transport:    transport,
		bitcoindHost: envOr("BITCOIND_HOST", defaultBitcoindHost),
	}
}

func newTapdClient(t testing.TB, transport Transport,
	spec tapdNodeConfig) tapsdk.Client {

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

	switch transport {
	case TransportGRPC:
		cfg := &tapgrpc.Config{
			Host:     envOr(spec.grpcHostEnv, spec.defaultGRPCHost),
			Network:  tapsdk.NetworkRegtest,
			TLS:      tapgrpc.TLSFromPath(tlsPath),
			Macaroon: tapsdk.MacaroonFromPath(macaroonPath),
		}

		client, err := tapgrpc.NewClient(cfg)
		require.NoError(t, err)
		t.Cleanup(func() { _ = client.Close() })

		return client

	case TransportREST:
		cfg := &taprest.Config{
			BaseURL:  envOr(spec.restHostEnv, spec.defaultRESTHost),
			Network:  tapsdk.NetworkRegtest,
			TLS:      taprest.TLSFromPath(tlsPath),
			Macaroon: tapsdk.MacaroonFromPath(macaroonPath),
			Timeout:  defaultRESTTimeout,
		}

		client, err := taprest.NewClient(cfg)
		require.NoError(t, err)
		t.Cleanup(func() { _ = client.Close() })

		return client

	default:
		require.FailNowf(t, "unsupported transport",
			"transport %q", transport)
		return nil
	}
}

// WaitForSync polls GetInfo until the node reports synced_to_chain.
// tapd has no chain-sync event yet (#81 item 5), so polling is the only
// option until upstream ships an edge-triggered notification.
func (h *TestHarness) WaitForSync(t testing.TB, ctx context.Context,
	client tapsdk.Client, timeout time.Duration) {

	t.Helper()

	require.Eventually(t, func() bool {
		info, err := client.GetInfo(ctx)
		return err == nil && info.SyncedToChain
	}, timeout, 500*time.Millisecond)
}

// WaitForAssetByTag polls ListAssets until the asset with the given tag is
// visible in the wallet. Polling because tapd has no asset-visibility
// event yet (#81 item 1).
func (h *TestHarness) WaitForAssetByTag(t testing.TB, ctx context.Context,
	client tapsdk.Client, tag string,
	timeout time.Duration) *tapsdk.AssetRecord {

	t.Helper()

	var found *tapsdk.AssetRecord
	require.Eventually(t, func() bool {
		assets, err := client.ListAssetRecords(ctx,
			&tapsdk.ListAssetsRequest{},
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
	wallet *tapsdk.Wallet, ref tapsdk.AssetRef, amount uint64,
	timeout time.Duration) uint64 {

	t.Helper()

	var balance uint64
	var lastStatus string
	require.Eventuallyf(t, func() bool {
		current, err := wallet.GetBalance(ctx, ref)
		if err != nil {
			lastStatus = fmt.Sprintf(
				"balance lookup not ready for %s: %v",
				ref, err,
			)
			verboseLogf(t, "%s", lastStatus)
			return false
		}

		balance = current
		if balance != amount {
			lastStatus = fmt.Sprintf(
				"balance for %s = %d, want %d",
				ref, balance, amount,
			)
			verboseLogf(t, "%s", lastStatus)
		}

		return balance == amount
	}, timeout, time.Second,
		"balance for %s never reached %d; last observation: %s",
		ref, amount, lastObservation(lastStatus),
	)

	return balance
}

// WaitForNoActiveMintBatch polls until tapd has no in-flight mint batch.
func (h *TestHarness) WaitForNoActiveMintBatch(t testing.TB,
	ctx context.Context, client tapsdk.Client, timeout time.Duration) {

	t.Helper()

	var lastStatus string
	require.Eventuallyf(t, func() bool {
		batches, err := client.ListBatches(ctx,
			&tapsdk.ListBatchesRequest{},
		)
		if err != nil {
			lastStatus = fmt.Sprintf(
				"list batches failed: %v", err,
			)
			return false
		}

		for _, batch := range batches {
			if batch == nil {
				continue
			}

			if mintBatchActive(batch.Batch.State) {
				lastStatus = fmt.Sprintf(
					"batch %s still active in state %d",
					batch.Batch.BatchKey, batch.Batch.State,
				)
				return false
			}
		}

		return true
	}, timeout, time.Second,
		"mint batch never became inactive; last observation: %s",
		lastObservation(lastStatus),
	)
}

func mintBatchActive(state tapsdk.BatchState) bool {
	switch state {
	case tapsdk.BatchStatePending,
		tapsdk.BatchStateFrozen,
		tapsdk.BatchStateCommitted,
		tapsdk.BatchStateBroadcast:

		return true

	default:
		return false
	}
}

// WaitForReceiveAddress retries Wallet.NewReceiveAddress until the receiver is
// ready to bootstrap the requested asset.
func (h *TestHarness) WaitForReceiveAddress(
	t testing.TB, ctx context.Context, wallet *tapsdk.Wallet,
	ref tapsdk.AssetRef, timeout time.Duration,
) *tapsdk.Address {

	t.Helper()

	var addr *tapsdk.Address
	var lastStatus string
	require.Eventuallyf(t, func() bool {
		candidate, err := wallet.NewReceiveAddress(ctx, ref)
		if err != nil {
			lastStatus = fmt.Sprintf(
				"receive address bootstrap not ready for %s: %v",
				ref, err,
			)
			verboseLogf(t, "%s", lastStatus)
			return false
		}

		addr = candidate
		return true
	}, timeout, time.Second,
		"receive address for %s never became available; "+
			"last observation: %s",
		ref, lastObservation(lastStatus),
	)

	return addr
}

// EnableUniverseBootstrap configures both tapd nodes for the regtest universe
// sync path used by the V2 receive-address flow.
func (h *TestHarness) EnableUniverseBootstrap(t testing.TB,
	ctx context.Context) {
	t.Helper()

	globalSync := []tapsdk.GlobalFederationSyncConfig{
		{
			ProofType:       tapsdk.ProofTypeIssuance,
			AllowSyncInsert: true,
			AllowSyncExport: true,
		},
		{
			ProofType:       tapsdk.ProofTypeTransfer,
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
	err := h.BobClient.AddFederationServer(ctx,
		[]tapsdk.FederationServer{{Host: aliceUniverseHost}},
	)
	// AddFederationServer is not idempotent: a stack kept alive
	// across tests (as compose does) will have Alice already
	// registered. Swallow that specific error so the rest of the
	// harness flow can proceed.
	if err != nil && !strings.Contains(
		err.Error(), "universe server already added",
	) {
		require.NoError(t, err)
	}
}

func (h *TestHarness) syncUniverseAsset(ctx context.Context,
	ref tapsdk.AssetRef) error {

	_, err := h.BobWallet.NewUniverse().SyncAsset(
		ctx, ref,
		envOr("TAPD_ALICE_UNIVERSE_HOST", defaultAliceUniverseHost),
		tapsdk.WithUniverseSyncMode(tapsdk.SyncIssuanceOnly),
	)
	return err
}

// CreateGroupedReceiveAddress bootstraps Bob for a grouped fungible receive
// flow and only returns once the actual V2 receive address can be created.
// The retry loop polls because tapd has no universe-sync-completion or
// federation-membership event yet (#81 items 3 and 4) — there is no
// edge-triggered signal that tells Bob "you can now build addresses for
// this group".
func (h *TestHarness) CreateGroupedReceiveAddress(t testing.TB,
	ctx context.Context, ref tapsdk.AssetRef) *tapsdk.Address {

	t.Helper()
	require.True(t, ref.IsGroupRef(),
		"grouped receive requires a fungible group ref")

	return h.CreateReceiveAddress(t, ctx, ref)
}

// CreateReceiveAddress bootstraps Bob for any V2 receive flow and only returns
// once the actual receive address can be created. Fungible assets use a group
// ref, while collectibles use an asset-ID ref.
func (h *TestHarness) CreateReceiveAddress(t testing.TB,
	ctx context.Context, ref tapsdk.AssetRef) *tapsdk.Address {

	t.Helper()

	h.EnableUniverseBootstrap(t, ctx)

	var addr *tapsdk.Address
	var lastStatus string
	require.Eventuallyf(t, func() bool {
		err := h.syncUniverseAsset(ctx, ref)
		if err != nil {
			lastStatus = fmt.Sprintf(
				"asset bootstrap sync not ready for %s: %v",
				ref, err,
			)
			verboseLogf(t, "%s", lastStatus)
			return false
		}

		candidate, err := h.BobWallet.NewReceiveAddress(ctx, ref)
		if err != nil {
			lastStatus = fmt.Sprintf(
				"receive address not ready for %s: %v",
				ref, err,
			)
			verboseLogf(t, "%s", lastStatus)
			return false
		}

		addr = candidate
		return true
	}, defaultWaitTimeout, time.Second,
		"receive address for %s never became available; "+
			"last observation: %s",
		ref, lastObservation(lastStatus),
	)

	return addr
}

// CreateV2EmbeddedReceiveAddress builds a V2 Taproot Asset address on
// Bob that bakes in the given amount. This is the modern replacement
// for the legacy V1 embedded-amount flow and is used in tests that
// need to exercise the SDK's embedded-amount code path. Like
// CreateGroupedReceiveAddress, this polls because tapd lacks a
// universe-sync-completion event (#81 item 3).
func (h *TestHarness) CreateV2EmbeddedReceiveAddress(t testing.TB,
	ctx context.Context, ref tapsdk.AssetRef,
	amount uint64) *tapsdk.Address {

	t.Helper()
	require.True(t, ref.IsGroupRef(),
		"V2 embedded receive uses a fungible group ref")
	require.NotZero(t, amount,
		"embedded receive requires a non-zero amount")

	h.EnableUniverseBootstrap(t, ctx)

	// Mirror the courier the harness configures on Bob's Wallet via
	// WithAuthMailboxCourier so the address is usable end-to-end.
	courier := envOr(
		"TAPD_BOB_PROOF_COURIER_ADDR",
		"authmailbox+universerpc://"+defaultBobProofCourierHost,
	)

	v2 := tapsdk.AddressVersionV2
	req := &tapsdk.NewAddressRequest{
		AssetRef:         ref,
		Amount:           amount,
		AddressVersion:   &v2,
		ProofCourierAddr: courier,
	}

	var addr *tapsdk.Address
	var lastStatus string
	require.Eventuallyf(t, func() bool {
		if err := h.syncUniverseAsset(ctx, ref); err != nil {
			lastStatus = fmt.Sprintf(
				"V2 embedded bootstrap not ready for %s: %v",
				ref, err,
			)
			verboseLogf(t, "%s", lastStatus)
			return false
		}

		candidate, err := h.BobClient.NewAddr(ctx, req)
		if err != nil {
			lastStatus = fmt.Sprintf(
				"V2 embedded receive address not ready "+
					"for %s: %v", ref, err,
			)
			verboseLogf(t, "%s", lastStatus)
			return false
		}

		addr = candidate
		return true
	}, defaultWaitTimeout, time.Second,
		"V2 embedded receive address for %s never became "+
			"available; last observation: %s",
		ref, lastObservation(lastStatus),
	)

	require.Equal(t, tapsdk.AddressVersionV2, addr.AddressVersion)
	require.Equal(t, amount, addr.Amount)

	return addr
}

// WaitForProofFile waits until tapd exposes a proof file for a concrete asset
// output.
func (h *TestHarness) WaitForProofFile(t testing.TB, ctx context.Context,
	wallet *tapsdk.Wallet, ref tapsdk.AssetRef,
	scriptKey tapsdk.PubKey,
	outpoint *tapsdk.Outpoint) *tapsdk.ProofFile {

	t.Helper()

	var proof *tapsdk.ProofFile
	var lastErr error
	require.Eventuallyf(t, func() bool {
		proof, lastErr = wallet.ExportProofFile(
			ctx, ref, scriptKey, outpoint,
		)
		return lastErr == nil && proof != nil &&
			len(proof.RawProofFile) > 0
	}, defaultWaitTimeout, time.Second,
		"proof file for %s never became available: %v",
		ref, lastErr,
	)

	return proof
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
			verboseLogf(t, "Retry %d: docker cp %s:%s",
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

// balanceTimeoutFor returns the timeout we use when waiting for a confirmed
// wallet balance after minting.
func balanceTimeoutFor(ref tapsdk.AssetRef) time.Duration {
	if ref.IsGroupRef() {
		return defaultGroupBalanceTimeout
	}

	return defaultCollectibleBalanceTimeout
}
