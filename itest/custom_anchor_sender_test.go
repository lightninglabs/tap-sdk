//go:build itest

package itest

import (
	"context"
	"testing"

	tapsdk "github.com/lightninglabs/tap-sdk"
)

// embeddedSenderName names the subtest whose sender is an embedded engine.
const embeddedSenderName = "embedded"

// customAnchorSender is the wallet a custom-anchor case sends from: Alice's
// tapd, or an embedded engine that receives the asset from her first.
type customAnchorSender struct {
	// name labels the sender in subtest and asset names.
	name string

	// wallet builds, commits and publishes the transfers.
	wallet *tapsdk.Wallet

	// signAnchor signs and finalizes the key-path anchor inputs whose
	// keys the sender's key ring holds.
	signAnchor tapsdk.AnchorSigner

	// node is the engine of an embedded sender.
	node *EmbeddedNode
}

// newTapdCustomAnchorSender returns Alice's tapd as the sender. lnd-alice
// holds its anchor keys.
func newTapdCustomAnchorSender(t testing.TB,
	h *TestHarness) *customAnchorSender {

	t.Helper()

	return &customAnchorSender{
		name:       string(h.Transport),
		wallet:     h.AliceWallet,
		signAnchor: newAliceLndAnchorSigner(t),
	}
}

// newEmbeddedCustomAnchorSender returns a new embedded engine as the sender.
func newEmbeddedCustomAnchorSender(t testing.TB,
	h *TestHarness) *customAnchorSender {

	t.Helper()

	node := h.NewEmbeddedNode(t)

	return &customAnchorSender{
		name:       embeddedSenderName,
		wallet:     node.Wallet,
		signAnchor: node.SignAnchorPsbt,
		node:       node,
	}
}

// customAnchorCase is a custom-anchor test case for one sender.
type customAnchorCase func(*testing.T, context.Context, *TestHarness,
	*customAnchorSender)

// runForTapdCustomAnchorSenders runs fn with Alice's tapd as the sender over
// each of the given transports.
func runForTapdCustomAnchorSenders(t *testing.T, transports []Transport,
	fn customAnchorCase) {

	t.Helper()

	for _, transport := range transports {
		t.Run(string(transport), func(t *testing.T) {
			h, ctx := newFundedHarnessFor(t, transport)
			fn(t, ctx, h, newTapdCustomAnchorSender(t, h))
		})
	}
}

// runForCustomAnchorSenders runs fn with Alice's tapd as the sender over
// each of the given transports, and with an embedded engine as the sender.
func runForCustomAnchorSenders(t *testing.T, transports []Transport,
	fn customAnchorCase) {

	t.Helper()

	runForTapdCustomAnchorSenders(t, transports, fn)

	t.Run(embeddedSenderName, func(t *testing.T) {
		h, ctx := newFundedHarness(t)
		fn(t, ctx, h, newEmbeddedCustomAnchorSender(t, h))
	})
}

// hold puts the whole supply of an asset Alice minted into the sender's
// wallet and returns the script key and anchor outpoint of the output that
// holds it. Alice's tapd holds the mint output, which its script key alone
// identifies. An embedded engine receives the supply from Alice.
func (s *customAnchorSender) hold(t testing.TB, ctx context.Context,
	h *TestHarness, minted *MintResult) (tapsdk.PubKey, *tapsdk.Outpoint) {

	t.Helper()

	if s.node == nil {
		return minted.Asset.ScriptKey.PubKey, nil
	}

	if groupKey, ok := minted.Ref.GroupKey(); ok {
		s.node.Client.TrustGroupKey(groupKey)
	}
	registered, _ := h.SendToEmbedded(
		t, ctx, s.node, minted, minted.Asset.Amount,
	)

	return registered.ScriptKey, &registered.Outpoint
}

// backendSigningWallet returns the sender's wallet for a transfer whose
// asset anchor the backend signs. It skips the test for a tapd that cannot.
func (s *customAnchorSender) backendSigningWallet(t *testing.T,
	ctx context.Context, h *TestHarness) *tapsdk.Wallet {

	t.Helper()

	if s.node == nil {
		skipBelowTapdVersion(t, ctx, h.AliceClient, 0, 8, 99)
	}

	return s.wallet
}
