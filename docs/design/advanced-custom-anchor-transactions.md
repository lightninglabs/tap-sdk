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
- script, leaf, control-block, signature, and witness bytes carried inside
  named SDK DTOs

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

When an input source is a proof file, the decoded proof must be checked against
the requested `AssetRef`. A group-key ref may match any concrete issuance in
that group. An asset-ID ref must match that exact issuance. The first
implementation should support proof-file-selected concrete inputs before adding
more complex wallet-selected multi-tranche selection.

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

Passive assets must also have an explicit policy. The safe default is to reject
unknown passive assets. Advanced callers may opt into preserving or re-anchoring
passive assets when the backend returns the required packets and proof metadata.

### Anchor template

The caller may supply a BTC anchor PSBT template. The template can include
BTC-only inputs, BTC-only outputs, asset-bearing output placeholders, change
outputs, and fee-bump outputs.

The SDK should treat the anchor PSBT as serialized bytes at the public API
boundary. Inspectable SDK DTOs may describe the expected output indices,
values, scripts, funding mode, and lock metadata, but the root package should
not expose btcd or taproot-assets PSBT internals as required public inputs.

Anchor funding should be represented as a named plan rather than a collection
of booleans:

- wallet-funded with fee rate or target confirmation, change handling, custom
  lock ID, and lock expiration
- caller-funded with no backend funding step
- no-change when the caller expects the template to be exactly balanced
- externally fee-bumped when the parent transaction relies on a separate fee
  child, such as a P2A output where supported by the backend

### Script, key, and witness plans

The advanced builder should expose SDK-owned plans for:

- wallet-derived asset script keys
- externally supplied script keys
- OP_TRUE-style asset scripts protected by BTC-level conditions
- direct anchor internal keys
- externally signed key-path spends
- script-path witness material

The plan should not make callers re-derive taproot math that the builder already
computed. It should return typed signing and witness requests with named byte
fields. For example, a key-path request can carry the anchor input index,
sighash, internal key, taproot root, and required signer keys. A script-path
request can carry the anchor input index, leaf script, leaf version, control
block bytes, sighash, taproot root, and required signer keys.

The exact field names may change during implementation, but the public contract
must be structured enough that callers can perform MuSig2/key-path signing and
script-path witness construction without importing Taproot Assets internals.

### Transfer package

After commit, the SDK should return a persistable transfer package. This
package is the recovery boundary between commit, external signing, optional
external broadcast, publish/log, and later proof handling.

The package should include:

- committed anchor PSBT bytes
- committed active virtual PSBT bytes
- committed passive virtual PSBT bytes
- change output index
- locked BTC UTXO metadata when the backend funded the anchor
- committed input and output summaries
- proof update metadata required for import, register, export, or delivery
- enough publish/log metadata to retry after a process restart

Callers that publish the anchor transaction outside tapd should persist this
package before handing the final transaction to their broadcaster.

### Verification model

Verification has three checkpoints:

1. **Pre-commit plan verification** checks proof sources, selected amounts,
   `AssetRef` equivalence, output scripts, output values, output indices,
   passive-asset policy, and loss policy before any signer is asked to sign.
2. **Post-commit verification** checks the committed anchor PSBT, committed
   virtual PSBTs, proof update metadata, change output, and locked UTXO metadata
   against the inspected plan.
3. **Pre-publish verification** checks the final signed anchor PSBT still
   matches the committed asset-bearing output scripts, values, and indices
   before publish/log.

Verification results should be structured. A result can be valid while also
carrying warnings. Failures should identify the scope, such as input proof,
asset identity, amount, output commitment, anchor output, passive asset, loss
policy, capability, or backend trust.

Some checks can run locally in the SDK and some checks require a tapd-compatible
backend in the first implementation. The API must make that distinction visible
so remote or hosted backends are not treated as an implicit trust boundary.

## Lifecycle

The advanced lifecycle has explicit checkpoints:

1. **Collect inputs and outputs.** The caller adds asset inputs, asset outputs,
   passive asset handling, proof sources, and an optional anchor PSBT template.
2. **Build a plan.** The builder validates the request, resolves `AssetRef`
   values, decodes enough proof data to identify concrete inputs, and returns
   an inspectable plan. No caller signature should be requested before this
   point.
3. **Verify the plan.** The caller verifies input proofs, selected amounts,
   asset identities, anchor output scripts, anchor output values, passive asset
   policy, and loss policy. The result distinguishes local checks from
   backend-trusted checks.
4. **Commit asset transactions.** The SDK asks the configured backend to bind
   virtual asset transactions to the anchor PSBT. The response returns a
   persistable transfer package containing the committed anchor PSBT, committed
   virtual PSBTs, passive PSBTs, locked UTXO metadata, and proof update metadata
   as SDK DTOs or byte blobs.
5. **Sign and finalize.** The caller or wallet signs any required BTC anchor
   inputs and any external asset-signing material. Finalized PSBT bytes are
   handed back to the SDK and checked against the committed plan before
   publish/log.
6. **Publish and log.** The SDK publishes and/or logs the transfer. A
   `SkipBroadcast` option keeps the final anchor transaction unbroadcast while
   still logging the transfer when the backend supports that mode.
7. **Handle proofs.** The SDK exposes enough proof metadata for receivers and
   storage systems to import, register, export, or deliver complete proofs.

Plans are immutable snapshots. Builders may be mutable while collecting inputs
and outputs, but `Build` should return a plan that does not change under the
caller. Commit consumes a plan and returns a committed package. Publish/log
consumes a committed package plus final anchor PSBT bytes. If publish/log fails
after commit, callers should be able to retry from the persisted committed
package when the backend operation is idempotent or exposes a duplicate-safe
result.

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
    Funding: tapsdk.AnchorFundingPlan{
        Mode: tapsdk.AnchorFundingCallerFunded,
    },
}).Build(ctx)

verification, err := plan.Verify(ctx)
if err != nil || !verification.Valid {
    return err
}

committed, err := plan.Commit(ctx)
if err := committed.VerifyFinalAnchorPSBT(finalAnchorPSBT); err != nil {
    return err
}

packet, err := committed.PublishAndLog(
    ctx, finalAnchorPSBT, tapsdk.PublishOptions{
        SkipBroadcast: true,
    },
)
```

Signing and witness material should also be inspectable through typed requests:

```go
signing, err := committed.SigningRequests()
for _, req := range signing.KeyPath {
    sig, err := signer.SignMuSig2(ctx, req.Sighash, req.Signers)
    if err != nil {
        return err
    }

    committed, err = committed.ApplyKeyPathSignature(req.ID, sig)
    if err != nil {
        return err
    }
}

for _, req := range signing.ScriptPath {
    witness, err := scriptSigner.BuildWitness(ctx, req)
    if err != nil {
        return err
    }

    committed, err = committed.ApplyScriptPathWitness(req.ID, witness)
    if err != nil {
        return err
    }
}
```

A transport-backed client method should mirror the backend operation names and
take one SDK request type rather than a long positional argument list. The
advanced builder can wrap these primitives without introducing custom-anchor
terminology at the WalletKit transport boundary. For example:

```go
type CommitVirtualPsbtsRequest struct {
    AnchorPSBT         []byte
    VirtualPSBTs       [][]byte
    PassiveAssetPSBTs  [][]byte
    Funding            AnchorFundingPlan
}

type CommitVirtualPsbtsResponse struct {
    AnchorPSBT         []byte
    VirtualPSBTs       [][]byte
    PassiveAssetPSBTs  [][]byte
    ChangeOutputIndex  int32
    LockedUTXOs        []LockedUTXO
}

type PublishAndLogTransferRequest struct {
    AnchorPSBT            []byte
    VirtualPSBTs          [][]byte
    PassiveAssetPSBTs     [][]byte
    ChangeOutputIndex     int32
    LockedUTXOs           []LockedUTXO
    SkipAnchorTxBroadcast bool
    Label                 string
}

type VerifyCustomAnchorResult struct {
    Valid    bool
    Issues   []VerificationIssue
    Warnings []VerificationIssue
}

type VerificationIssue struct {
    Code        VerificationCode
    Scope       VerificationScope
    InputIndex  int
    OutputIndex int
    Message     string
    Local       bool
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

The advanced builder may reuse simple-builder concepts such as
`AnchorSigner` and `WithSkipBroadcast` where the meaning is identical. Its main
handoff should still be the explicit plan/package flow above, because advanced
callers need to inspect, persist, externally sign, externally broadcast, and
retry at boundaries that the simple builders intentionally hide.

## Transport Boundary

Every advanced operation that needs a backend call must be available through
both gRPC and REST. The root package owns the SDK DTOs. The `grpc` and `rest`
packages own:

- proto and JSON marshal/unmarshal code
- TLS and macaroon behavior
- large proof and PSBT byte encoding
- timeout behavior
- any daemon-version compatibility shims

Because custom-anchor support depends on backend capabilities that may not be
available on every tapd-compatible service, transports should expose capability
discovery or return a structured unsupported-capability error before the caller
reaches a signing step. Capabilities should cover at least custom-anchor commit,
skip-broadcast publish/log, skip-funding commit, P2A or external fee-bump
support, proof registration, and local-vs-backend verification support.

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
local-only proof stores, or local-only signer behavior.

The caller decides how much trust to place in the backend, but the SDK must not
present backend-verified data as if it were locally verified. The first
implementation should classify checks as follows:

- request validation, duplicate output indices, amount conservation, passive
  policy, and loss policy: local SDK in the first implementation and long term
- anchor PSBT output scripts, values, indices, and final-PSBT drift: local SDK
  in the first implementation and long term
- proof file decoding and `AssetRef` equivalence: backend or local SDK where
  available in the first implementation, local SDK long term
- input proof chain validity: backend in the first implementation, local SDK
  verifier long term
- output commitment inclusion and proof suffix correctness: backend plus local
  structural checks where available in the first implementation, local SDK
  verifier long term
- transfer logging, proof registration, and wallet inventory mutation: backend
  in the first implementation and long term

Over time, pure proof decoding, proof verification, vPSBT assembly, anchor PSBT
inspection, and commitment checks can move into SDK/library code so lightweight
clients can verify more locally.

## Safety Invariants

- Caller requests use `AssetRef`, not parallel asset ID and group key fields.
- Proof-file inputs must be checked for equivalence with the requested
  `AssetRef`.
- Public signatures use SDK DTOs and bytes, not tapd or taproot-assets types.
- Callers can inspect and verify the plan before signing any BTC anchor input.
- Verification results distinguish local checks from backend-trusted checks.
- The final anchor PSBT must match planned asset-bearing output scripts, values,
  and indices before publish/log.
- Asset loss requires an explicit loss policy.
- Passive assets must be either preserved, explicitly re-anchored, or rejected.
- A committed transfer package must be persistable before external signing or
  broadcast.
- Proof metadata required for recovery must be returned before the flow is
  considered complete.
- Publish/log must expose whether the anchor transaction was broadcast or only
  logged for external broadcast.
- Unsupported backend capabilities must fail before a caller is asked to sign.

## Follow-Up Work

Implementation issues should cover:

- builder DTO validation and state transitions
- immutable plan and persistable transfer-package lifecycle
- wallet-selected and proof-file-selected asset inputs
- caller-supplied anchor PSBT templates
- exact anchor output value handling
- wallet-funded, no-change, zero-fee, and externally fee-bumped paths
- P2A anchor output policy where supported by the backend
- typed script/key/witness signing requests and application of signatures or
  witnesses
- pre-signing verification results and failure classes
- proof import/export/register flows after publish/log
- path-oriented proof metadata for deep transaction graphs
- backend capability discovery and unsupported-capability errors
- REST and gRPC parity wrappers
- unit, transport, and regtest coverage
- examples that keep normal users on the simple send APIs
