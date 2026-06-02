# Advanced Custom-Anchor Asset Transactions

## Status

Proposed.

## Context

Normal Taproot Assets sends should stay simple: callers receive addresses,
choose amounts, pay an on-chain fee rate, and let tapd manage the BTC anchor
transaction, asset proofs, logging, and proof delivery.

Some integrations need lower-level control. They may already own the BTC anchor
transaction, need BTC-only inputs or outputs in the same PSBT, require exact
asset-bearing output values, or need to inspect asset commitments before any
external signer authorizes the anchor transaction. They still should not import
`taprpc`, `tappsbt`, `tapfreighter`, proof, commitment, or other
Taproot Assets implementation packages.

## Decision

Add a separate advanced builder for custom-anchor asset transactions. The
normal wallet APIs remain the default path. The advanced builder is an explicit
opt-in surface for callers that need to control the BTC anchor PSBT and inspect
the asset plan before signing.

The public model uses SDK-owned request and response types plus serialized byte
blobs for wire formats that are already standard external artifacts:

- virtual PSBT bytes
- BTC anchor PSBT bytes
- raw BTC transaction bytes
- proof file bytes
- script or witness bytes when the SDK cannot name them safely yet

The public model must not expose tapd proto types or Taproot Assets internal
implementation types.

## Public Concepts

### Asset identity

Every caller-supplied asset field uses `AssetRef`.

A grouped fungible asset is identified by a group-key `AssetRef`. An ungrouped
fungible asset is identified by an asset-ID `AssetRef`. NFT outputs use their
concrete item asset-ID `AssetRef`. Concrete issuance IDs may appear in decoded
plans, verification results, and diagnostics, but callers should not need to
choose issuance-vs-group fields in requests.

### Asset inputs

An advanced input identifies spendable asset units and where their proof data
comes from. Supported input sources should be:

- a raw proof file byte blob
- a proof locator such as asset ref, script key, and outpoint
- a future SDK proof DTO produced by proof import/export helpers
- a wallet-selected input source for managed tapd wallets

The request uses `AssetRef` and amount. The builder resolves the concrete
issuance/tranche when tapd or local proof decoding requires it.

### Asset outputs

An advanced output describes the asset units to create and where their Taproot
Asset commitment will be anchored. It uses:

- `AssetRef`
- amount
- anchor output index
- required anchor output value in sats, when known
- script/key plan
- proof delivery metadata

Any explicit asset burn or loss must be represented by a named loss policy. The
builder must not silently drop asset units as a side effect of balancing.

### Anchor template

The caller may supply a BTC anchor PSBT template. The template can include
BTC-only inputs, BTC-only outputs, asset-bearing output placeholders, change
outputs, and fee-bump outputs.

The SDK should treat the anchor PSBT as serialized bytes at the public API
boundary. Inspectable SDK DTOs may describe the expected output indices,
values, scripts, funding mode, and lock metadata, but the root package should
not expose btcd or taproot-assets PSBT internals as required public inputs.

### Script, key, and witness plans

The advanced builder should expose SDK-owned plans for:

- wallet-derived asset script keys
- externally supplied script keys
- OP_TRUE-style asset scripts protected by BTC-level conditions
- direct anchor internal keys
- externally signed key-path spends
- script-path witness material

Where a plan needs raw witness, control-block-like, or leaf data before the SDK
has a stable domain name for it, expose opaque bytes and document their
ownership. Follow-up API work can replace opaque fields with richer SDK DTOs
once the invariant is stable.

## Lifecycle

The advanced lifecycle has explicit checkpoints:

1. **Collect inputs and outputs.** The caller adds asset inputs, asset outputs,
   passive asset handling, proof sources, and an optional anchor PSBT template.
2. **Build a plan.** The builder validates the request, resolves `AssetRef`
   values, decodes enough proof data to identify concrete inputs, and returns
   an inspectable plan. No caller signature should be requested before this
   point.
3. **Verify the plan.** The caller verifies input proofs, selected amounts,
   asset identities, output commitments, anchor output scripts, anchor output
   values, and loss policy.
4. **Commit asset transactions.** The SDK asks the configured backend to bind
   virtual asset transactions to the anchor PSBT. The response returns the
   committed anchor PSBT, committed virtual PSBTs, passive PSBTs, and proof
   update metadata as SDK DTOs or byte blobs.
5. **Sign and finalize.** The caller or wallet signs any required BTC anchor
   inputs and any external asset-signing material. Finalized PSBT bytes are
   handed back to the SDK.
6. **Publish and log.** The SDK publishes and/or logs the transfer. A
   `SkipBroadcast` option keeps the final anchor transaction unbroadcast while
   still logging the transfer when the backend supports that mode.
7. **Handle proofs.** The SDK exposes enough proof metadata for receivers and
   storage systems to import, register, export, or deliver complete proofs.

## API Sketch

Names are provisional. The important shape is the separation of the advanced
custom-anchor path from the normal send path.

```go
builder := wallet.NewCustomAnchorTxBuilder()

plan, err := builder.AddInput(tapsdk.CustomAssetInput{
    AssetRef: ref,
    Amount:   100,
    Source: tapsdk.ProofFileSource{
        ProofFile: proofFileBytes,
    },
}).AddOutput(tapsdk.CustomAssetOutput{
    AssetRef:          ref,
    Amount:            100,
    AnchorOutputIndex: 2,
    AnchorValueSat:    330,
    ScriptPlan: tapsdk.ExternalScriptKeyPlan{
        ScriptKey: receiverScriptKey,
    },
}).SetAnchorTemplate(tapsdk.AnchorTemplate{
    PSBT: anchorTemplateBytes,
}).Build(ctx)

verification, err := plan.Verify(ctx)
if err != nil || !verification.Valid {
    return err
}

committed, err := plan.Commit(ctx)
finalized := committed.WithFinalAnchorPSBT(finalAnchorPSBT)
packet, err := finalized.PublishAndLog(ctx, tapsdk.PublishOptions{
    SkipBroadcast: true,
})
```

A transport-backed client method should take one SDK request type rather than a
long positional argument list. For example:

```go
type CommitCustomAnchorRequest struct {
    PlanID             string
    AnchorPSBT         []byte
    VirtualPSBTs       [][]byte
    PassiveAssetPSBTs  [][]byte
    Funding            AnchorFundingPlan
}

type CommitCustomAnchorResponse struct {
    AnchorPSBT         []byte
    VirtualPSBTs       [][]byte
    PassiveAssetPSBTs  [][]byte
    Outputs            []CommittedAssetOutput
    ProofUpdates       []ProofUpdate
    LockedUTXOs        []LockedUTXO
}
```

The concrete request names can change during implementation. The invariant is
that request and response types live in the root package, while conversion to
gRPC or REST wire formats stays inside the transport packages.

## Existing Builder Evolution

Keep the simple surfaces:

- `Wallet.Send` and `Wallet.SendMulti` stay the default address-based flow.
- `TxBuilder` remains the low-level address-based Fund/Sign/Commit/Finish
  helper.
- `InteractiveTxBuilder` remains the receiver-key flow for manual proof
  delivery.

Add `CustomAnchorTxBuilder` instead of forcing custom-anchor controls into the
existing builders. After the advanced path stabilizes, the simple builders may
share an internal core with it, but their public API should remain small and
address/key-flow specific.

## Transport Boundary

Every advanced operation that needs a backend call must be available through
both gRPC and REST. The root package owns the SDK DTOs. The `grpc` and `rest`
packages own:

- proto and JSON marshal/unmarshal code
- TLS and macaroon behavior
- large proof and PSBT byte encoding
- timeout behavior
- any daemon-version compatibility shims

Tests should exercise equivalent SDK requests through both transports or a
shared conformance harness so advanced features cannot land as gRPC-only.

## Runtime Model

The first implementation may still require a tapd-compatible backend for
wallet inventory, proof decoding or verification, virtual PSBT funding, asset
signing, commitment generation, transfer logging, and proof registration.

That backend does not have to be a full local daemon for every end user. The
SDK should support the same public API when the configured transport points at:

- a local tapd
- a remote tapd controlled by the application
- a hosted tapd-compatible service
- a future smaller service that exposes only wallet, proof, and builder calls

The API should avoid local-daemon assumptions such as hard-coded file paths,
local-only proof stores, or local-only signer behavior. The caller decides how
much trust to place in the backend. Over time, pure proof decoding,
verification, vPSBT assembly, anchor PSBT inspection, and commitment checks can
move into SDK/library code so lightweight clients can verify more locally.

## Safety Invariants

- Caller requests use `AssetRef`, not parallel asset ID and group key fields.
- Public signatures use SDK DTOs and bytes, not tapd or taproot-assets types.
- Callers can inspect and verify the plan before signing any BTC anchor input.
- The final anchor PSBT must match planned asset-bearing output scripts, values,
  and indices before publish/log.
- Asset loss requires an explicit loss policy.
- Passive assets must be either preserved, explicitly re-anchored, or rejected.
- Proof metadata required for recovery must be returned before the flow is
  considered complete.
- Publish/log must expose whether the anchor transaction was broadcast or only
  logged for external broadcast.

## Follow-Up Work

Implementation issues should cover:

- builder DTO validation and state transitions
- wallet-selected and proof-file-selected asset inputs
- caller-supplied anchor PSBT templates
- exact anchor output value handling
- wallet-funded, no-change, zero-fee, and externally fee-bumped paths
- P2A anchor output policy where supported by the backend
- script/key/witness planning
- pre-signing verification results and failure classes
- proof import/export/register flows after publish/log
- REST and gRPC parity wrappers
- unit, transport, and regtest coverage
- examples that keep normal users on the simple send APIs
