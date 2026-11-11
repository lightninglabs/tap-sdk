# Embedded Transport

## Status

Accepted.

## Context

The `grpc` and `rest` transports forward every call to a tapd, and tapd needs
an lnd. A wallet that runs inside a phone app or a browser tab can run neither
daemon. The asset rules themselves are plain Go: tapd's `asset`, `proof`,
`commitment`, `tappsbt`, `tapsend` and `vm` packages build for every platform
Go targets, js/wasm included. What a wallet lacks without tapd is a store, a
key ring and a way to publish and confirm a transfer.

## Decision

The `embedded` package is a third transport. It implements `tapsdk.Client`
inside the host process and takes three ports from the host:

| Port | The engine uses it to |
|------|------------------------|
| `Store` | persist keys, proof files and transfers |
| `KeyRing` | derive asset keys and their private keys |
| `Chain` | read block headers, broadcast, and follow confirmations |

The engine implements the subset of tapd's gRPC services a wallet needs and
serves it over an in-process connection. The existing gRPC client code runs
unchanged on top, so both backends share request validation, marshalling and
error statuses. A call outside the subset returns `codes.Unimplemented`.

Every asset rule is a call into tapd's packages: `proof.File.Verify`,
`tapsend.SignVirtualTransaction`, `tapsend.ValidateAnchoredPackets`,
`tapsend.CommitPackets`, `tapsend.SignAnchorInputs`, `proof.VerifyProofSuffix`
and `proof.AppendConfirmedSuffix`. The engine adds storage and the order of
the calls.

## Consequences

- Code written against `tapsdk.Client`, including the `customanchor` builder,
  runs on a tapd and on the engine.
- The engine holds no Bitcoin. It commits caller-funded anchor transactions
  only, and the host wallet pays and signs the Bitcoin inputs.
- The engine runs no universe. A grouped asset verifies when its group key is
  trusted through configuration, an explicit call, or an asset the wallet
  already holds. `InsertProof` stages proofs in memory for the following
  `RegisterTransfer`, which verifies the assembled file before storing it.
- A transfer is recorded before its anchor transaction is broadcast. The
  engine follows it through re-orgs until the chain source reports the
  confirmation final, and resumes after a restart from the store.
- The store keeps a transfer as an opaque payload that the engine encodes, so
  a host schema needs four tables: keys, script keys, proofs and transfers.

## Non-Goals

- Minting, universe serving and syncing, address sends, burns, events, RFQ
  and channels.
- Funding anchor transactions from an engine-owned Bitcoin wallet.
- Recovering proofs from a seed. A store backup is the host's concern.
