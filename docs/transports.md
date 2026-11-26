# Transports and Auth

`tap-sdk` supports gRPC and REST transports to a tapd, and an embedded
transport that runs without one. All three satisfy the same root
`tapsdk.Client` interface and return the same SDK business types.

## gRPC

```go
client, err := tapgrpc.NewClient(&tapgrpc.Config{
    Host:     "localhost:10029",
    Network:  tapsdk.NetworkRegtest,
    TLS:      tapgrpc.TLSFromPath("/path/to/tls.cert"),
    Macaroon: tapsdk.MacaroonFromPath("/path/to/admin.macaroon"),
})
```

Use gRPC for native Go services and when you want tapd's native streaming RPCs.

## REST

```go
client, err := taprest.NewClient(&taprest.Config{
    BaseURL:  "https://localhost:8089",
    Network:  tapsdk.NetworkRegtest,
    TLS:      taprest.TLSFromPath("/path/to/tls.cert"),
    Macaroon: tapsdk.MacaroonFromPath("/path/to/admin.macaroon"),
})
```

REST requires `https://` URLs. Event subscriptions use tapd's grpc-gateway
WebSocket bridge internally.

## Embedded

```go
client, err := embedded.NewClient(&embedded.Config{
    Network: tapsdk.NetworkRegtest,
    Store:   store,   // keys, proofs and transfers
    KeyRing: keyRing, // embedded.NewLocalKeyRing, or lnd below
    Signer:  signer,  // embedded.NewKeyRingSigner, or lnd below
    Chain:   chain,   // headers, broadcast, confirmations
})
if err != nil {
    return err
}
if err := client.ResumeTransfers(ctx); err != nil {
    return err
}
defer client.Close()

wallet := tapsdk.NewWallet(client, tapsdk.NetworkRegtest)
```

The embedded transport answers the client calls inside the host process. There
is no tapd: the host supplies a key ring, a signer, a chain source and a
store, and the engine applies the asset rules by calling tapd's packages.
`embedded.NewLocalKeyRing` and `embedded.NewKeyRingSigner` derive and sign
with a local key ring. Both ports take the context of the engine call, so a
remote key ring or signer stops at the call's deadline.

A host whose keys stay in a standalone lnd adapts lnd's wallet kit and signer
RPCs. lnd computes a Taproot sighash over every previous output, and finds a
key outside its default family only by its key locator, which
`SignOutputRawKeyLocator` forwards beside the public key:

```go
type lndKeys struct {
    walletKit lndclient.WalletKitClient
    signer    lndclient.SignerClient
}

func (k *lndKeys) DeriveNextKey(ctx context.Context,
    family keychain.KeyFamily) (keychain.KeyDescriptor, error) {

    desc, err := k.walletKit.DeriveNextKey(ctx, int32(family))
    if err != nil {
        return keychain.KeyDescriptor{}, err
    }

    return *desc, nil
}

func (k *lndKeys) SignOutputRaw(ctx context.Context, tx *wire.MsgTx,
    desc *input.SignDescriptor) (input.Signature, error) {

    prevOuts := make([]*wire.TxOut, len(tx.TxIn))
    for idx, txIn := range tx.TxIn {
        prevOuts[idx] = desc.PrevOutputFetcher.FetchPrevOutput(
            txIn.PreviousOutPoint,
        )
    }

    sigs, err := k.signer.SignOutputRawKeyLocator(
        ctx, tx, []*lndclient.SignDescriptor{{
            KeyDesc:       desc.KeyDesc,
            SingleTweak:   desc.SingleTweak,
            TapTweak:      desc.TapTweak,
            WitnessScript: desc.WitnessScript,
            SignMethod:    desc.SignMethod,
            Output:        desc.Output,
            HashType:      desc.HashType,
            InputIndex:    desc.InputIndex,
        }}, prevOuts,
    )
    if err != nil {
        return nil, err
    }

    return schnorr.ParseSignature(sigs[0])
}
```

A `*lndKeys` then serves as both `KeyRing` and `Signer`.

`NewClient` opens no listener and starts no background work, and the client
answers calls at once. The engine follows a transfer it publishes until the
confirmation is final. `ResumeTransfers` does the same for the transfers an
earlier run left unconfirmed, so a host calls it once its chain source is
ready. `Close` stops following transfers and receives.

It covers what a wallet needs to hold and move assets:

- verify, decode, export and import proof files;
- derive, declare and look up script keys and anchor internal keys;
- list owned asset outputs, balances and recorded transfers;
- sign, commit and publish caller-funded custom-anchor transfers, and write
  the final proofs once the anchor transaction confirms;
- publish a transfer that spends an asset output the wallet does not hold,
  when the publish request carries the output's proof file
  (`customanchor.WithInputProofFiles`). The engine verifies the file back to
  the issuance and keeps it without owning the output;
- create, decode and list V2 addresses, when the host sets `Config.Receive`.
  An address takes the next two asset keys of the wallet, names the raw
  script key and requests asset version 1. Its proof courier is the one the
  request names, or `ReceiveConfig.DefaultCourier`;
- receive the transfers sent to those addresses, and list them with
  `AddrReceives`;
- fund a transfer to V2 addresses with `FundTransfer`, and deliver its proofs
  to the recipients once it confirms, when the host sets `Config.Send`.

Every other call returns `codes.Unimplemented`: minting, universe queries,
`SendAsset`, burns and event streams. The engine
holds no Bitcoin, so `CommitVirtualPsbts` accepts caller-funded anchor
transactions only, and `customanchor.QueryCapabilities` reports wallet funding
as unsupported.

The engine has no universe, so `Wallet.GetBalance` and `Wallet.ListBalances`
treat an asset as known when the wallet holds or has spent an output of it.

A proof of a grouped asset verifies only if its group key is trusted. Trust
comes from `Config.GroupKeys`, from `Client.TrustGroupKey`, from the assets
the wallet already holds, and from the addresses it created for a group key.

### Receiving at an address

A sender pays a V2 address into an output the wallet cannot predict, and
announces the transfer in the auth mailbox of the address's proof courier.
The engine reads that mailbox, so it detects a transfer without help from a
Bitcoin wallet. `ReceiveConfig` takes three ports for it:

| Port | The engine uses it to |
|------|------------------------|
| `AddressStore` | persist addresses and the transfers to them |
| `ReceiveKeys` | decrypt a mailbox message and sign the mailbox challenge with the script key of an address |
| `CourierDialer` | reach the courier of an address |

`embedded.NewKeyRingReceiveKeys` uses local private keys. A host on lnd
answers both calls with lnd's `DeriveSharedKey` and `SignMessage` signer
RPCs. A `Courier` moves opaque bytes: one universe leaf per query, and the
messages of a mailbox after a signed challenge.

`courier.NewDialer`, in the `embedded/courier` package, connects to a tapd
courier over gRPC, with a keepalive that ends the streams of a dead
connection. A courier serves its universe and its mailbox to the public, so
the client sends no macaroon. An `Endpoint` reaches a courier at another host
and port than its address names, or with a TLS configuration of its own. The
package is separate from the engine, so a host that reaches its couriers
another way does not link it.

```go
client, err := embedded.NewClient(&embedded.Config{
    // ...
    Receive: &embedded.ReceiveConfig{
        Store:          store,
        Keys:           embedded.NewKeyRingReceiveKeys(keys),
        Dial:           courier.NewDialer(nil),
        DefaultCourier: courierURL,
    },
})
```

The engine reads a mailbox in two ways:

- `ResumeReceives` holds the mailbox of every address open, and works on the
  receives an earlier run left unfinished. A host calls it once the wallet is
  unlocked. The courier delivers its stored messages first, which covers the
  transfers sent while the host was not running.
- `SyncReceives` runs one pass and disconnects: it reads the stored messages
  and works once on every unfinished receive. It suits an app that returns to
  the foreground.

Both calls also follow the confirmed receives that are not final.

For each transfer the engine checks the block the sender names against the
chain source, fetches the proofs from the courier's universe back to the
issuance, and verifies the file. It then checks that the last proof creates
the announced output under the keys of the address: the script key derived
for the asset, the internal key of the address, and no tapscript sibling. The
output must also be one the wallet can spend: of the asset version of the
address, and without a time lock. A transfer that passes is stored as an owned
asset output. One that does not is recorded as failed, and the wallet takes
none of its proofs.

Anyone who knows an address can announce a transfer to it, so the engine
records each announcement on its own. A wrong announcement of an output fails
or stays unfinished, and does not keep the sender's announcement from being
confirmed. An announcement that names no asset beyond those the wallet
received through other announcements of the same output is recorded as
failed, so that one transfer is not listed as two confirmed receives.

The engine leaves its messages on the courier, and a message it reads again
maps to the receive it already recorded.

The engine follows the anchor transaction of a confirmed receive with
`Chain.RegisterConfirmation` until the chain source reports the confirmation
final, which moves the receive to `ReceiveFinal`. When the confirming block
leaves the chain, the receive returns to `ReceiveDetected` and its proofs are
kept as not owned, so the wallet does not list or spend the assets. When the
transaction confirms again, the engine writes the new block into the proofs,
verifies the files and stores them as owned. A client that starts compares
the block of each confirmed receive with the best chain first. `AddrReceives`
lists a final receive as completed, and a receive whose block left the chain
as a detected transaction.

Only the anchor transaction of the received output is given its new block. A
proof file whose earlier proofs name a block that also left the chain does
not verify, and its receive stays detected.

A sender needs one confirmed transaction to announce a transfer whose proofs
never reach the courier. The engine therefore fetches the proofs of a few
receives at a time, and a client works on one receive twelve times at most on
its own, with a delay that doubles from five seconds. `SyncReceives` tries
such a receive again.

### Sending to an address

A host pays a V2 address with the calls of a caller-funded transfer.
`SendConfig` enables it, with one port: the dialer of the couriers that
recipients name in their addresses.

```go
client, err := embedded.NewClient(&embedded.Config{
    // ...
    Send: &embedded.SendConfig{
        Dial: courier.NewDialer(nil),
    },
})
```

`FundTransfer` selects the outputs to spend and returns the virtual
transactions, one per issuance the transfer spends. It selects among the
unspent outputs of the asset whose script key and anchor key the wallet
holds, largest first, across every issuance of a group. The engine leases no
output, so a host funds one transfer at a time.

The output of each recipient follows tapd's rules for a V2 address: its
script key is derived from the script key of the address for the asset, its
anchor output is under the internal key of the address, and a random alt
leaf makes two equal payments to one address differ on chain. The surplus
returns to the wallet in a change output of asset version 1. An asset that a
spent anchor output holds beside the spent one moves to the anchor output of
the change in a passive packet.

The host signs every packet with `SignVirtualPsbt`, commits them to one
anchor transaction it funds with `CommitVirtualPsbtsWithRequest`, and
publishes with `PublishAndLogTransferWithRequest`. `SendAsset` does all of
this in one call on a tapd, which pays for the anchor from lnd. The engine
holds no Bitcoin, so that call stays unimplemented.

A mailbox takes a message only with the proof that its output confirmed, so
the engine delivers each output that pays an address once the anchor
transaction confirms, to the courier the address names:

1. It stores every proof of the output's proof file in the universe of the
   asset, and for a grouped asset also in the universe of the group.
2. It posts the encrypted announcement of the output to the mailbox of the
   address.

The announcement comes last, because a recipient fetches the proofs when it
reads it. The engine records each delivery with the transfer, and
`ListTransfers` reports the proof delivery status of the output as pending
until the courier took the proofs and the announcement. A delivery that
fails is repeated after 30 seconds, and the wait doubles up to 10 minutes.
`ResumeTransfers` continues the deliveries an earlier run left unfinished.
Repeating a delivery is safe: a universe takes a proof it holds, and a
mailbox keeps one message per output.

The recipient chooses the courier, so `SendConfig.Dial` decides which hosts
the wallet contacts. `courier.NewDialer` dials any host an address names.

`embedded.NewMemoryStore` is a store for tests. The package builds for js/wasm,
and `make unit-wasm` runs its tests there.

## TLS Sources

Both transports expose the same TLS source pattern:

| Source | Use |
|--------|-----|
| `nil` | Load tapd's default `~/.tapd/tls.cert` |
| `TLSFromPath(path)` | Trust a certificate from disk |
| `TLSFromData(pem)` | Trust PEM data supplied by config |
| `TLSSystemCert()` | Use the host system certificate pool |
| `TLSInsecure()` | Disable verification for local tests only |

Both transport configs support:

- `TLSMinVersion`
- `TLSPinnedCertFingerprint`

`TLSPinnedCertFingerprint` is the SHA-256 digest of the server leaf
certificate DER bytes. Colons are accepted.

```bash
openssl x509 -in ~/.tapd/tls.cert -outform DER | \
  sha256sum | awk '{print $1}'
```

## Macaroon Sources

Most callers can use root helpers:

```go
tapsdk.MacaroonFromPath("/path/to/admin.macaroon")
tapsdk.MacaroonFromDir("/path/to/network/macaroons")
tapsdk.MacaroonFromHex(hexMacaroon)
```

`MacaroonFromPath` and `MacaroonFromHex` reuse one macaroon for every tapd
sub-service. `MacaroonFromDir` loads tapd's per-service macaroon files.

The transport clients load macaroons during client construction, so invalid or
missing credentials fail early.

## Error Mapping

Both transports preserve gRPC status codes where tapd provides them. REST
errors from grpc-gateway are exposed as typed errors, so callers can continue
using `status.FromError`, `codes.Code`, and SDK sentinel errors.

## Production Notes

- Prefer certificate verification or certificate pinning.
- Treat `TLSInsecure()` as a test-only option.
- Store macaroons with the least privilege needed for the service.
- Keep universe sync hosts in trusted configuration.
