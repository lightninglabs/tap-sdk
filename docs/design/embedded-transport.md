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
inside the host process and takes four ports from the host:

| Port | The engine uses it to |
|------|------------------------|
| `Store` | persist keys, proof files and transfers |
| `KeyRing` | derive asset keys |
| `Signer` | sign asset witnesses and anchor inputs with those keys |
| `Chain` | read block headers, broadcast, and follow confirmations |

A host that receives at Taproot Asset addresses also sets `ReceiveConfig`,
with three more ports:

| Port | The engine uses it to |
|------|------------------------|
| `AddressStore` | persist addresses and the transfers to them |
| `ReceiveKeys` | decrypt and authenticate with the script key of an address |
| `CourierDialer` | reach the proof courier an address names |

Without it the address calls return `codes.Unimplemented`, and the engine
reads from no courier.

A host that pays Taproot Asset addresses sets `SendConfig`, with one port:

| Port | The engine uses it to |
|------|------------------------|
| `CourierDialer` | reach the proof courier the address of a recipient names |

Without it the engine refuses to publish a transfer that pays an address,
and writes to no courier.

`Signer` has the shape of lnd's `input.Signer` plus a context, so the engine
never holds a private key: a host can sign with a local key ring through
`NewKeyRingSigner`, or with a remote lnd through its signer RPC. Both
`KeyRing` and `Signer` take the context of the engine call, so a stalled
remote signer ends at the call's deadline.

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
  Addresses and the transfers to them take two more.
- The engine creates version 2 addresses only. A sender announces a transfer
  to such an address in the auth mailbox of a proof courier, so the engine
  detects it on every platform without watching the chain. The `Courier` port
  moves opaque bytes: the engine decrypts the messages with tapd's
  `proof.DecryptSendFragment`, walks the proof chain with
  `proof.FetchProofProvenance` and verifies the file itself.
- A receive is recorded when it is detected and updated once its proofs
  verify, so the engine resumes it after a restart from the store. Each
  announcement of an output has a record of its own, since anyone who knows
  an address can announce one. The engine follows the anchor transaction of
  a confirmed receive as it follows a transfer: a receive whose block leaves
  the chain returns to detected with its proofs not owned, and its proofs
  are given the new block once the transaction confirms again. A sender
  announces a transfer once, so a receive whose block left the chain before
  the engine read the announcement is followed in the same way.

- The engine pays version 2 addresses with caller-funded transfers.
  `FundTransfer` selects the outputs to spend, across every issuance of a
  group, and builds one virtual transaction per issuance with the outputs
  tapd's `prepareV2Outputs` builds. The client answers that call itself,
  because the response of tapd's `FundVirtualPsbt` holds one virtual
  transaction. After the anchor transaction confirms, the
  engine stores the proofs in the universe of the recipient's courier and
  then posts the announcement that `proof.EncryptSendFragment` encrypts to
  the recipient's mailbox, in the order tapd uses.
- A delivery is recorded in the payload of its transfer, so the store needs
  no new table, and the engine resumes it after a restart. The recipient
  names the courier, so the dialer the host supplies decides which hosts the
  wallet contacts.

- The engine pays version 2 addresses with caller-funded transfers.
  `FundTransfer` selects the outputs to spend, across every issuance of a
  group, and builds one virtual transaction per issuance with the outputs
  tapd's `prepareV2Outputs` builds. The client answers that call itself,
  because the response of tapd's `FundVirtualPsbt` holds one virtual
  transaction. After the anchor transaction confirms, the
  engine stores the proofs in the universe of the recipient's courier and
  then posts the announcement that `proof.EncryptSendFragment` encrypts to
  the recipient's mailbox, in the order tapd uses.
- A delivery is recorded in the payload of its transfer, so the store needs
  no new table, and the engine resumes it after a restart. The recipient
  names the courier, so the dialer the host supplies decides which hosts the
  wallet contacts.

## Non-Goals

- Minting, universe serving and syncing, burns, events, RFQ and channels.
- `SendAsset`, which funds the anchor transaction of an address send in the
  backend.
- Funding anchor transactions from an engine-owned Bitcoin wallet.
- Recovering proofs from a seed. A store backup is the host's concern.
