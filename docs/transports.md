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
ready. `Close` stops following transfers.

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
  request names, or `ReceiveConfig.DefaultCourier`. The engine does not
  detect a transfer to an address.

Every other call returns `codes.Unimplemented`: minting, universe queries,
address receive events, address sends, burns and event streams. The engine
holds no Bitcoin, so `CommitVirtualPsbts` accepts caller-funded anchor
transactions only, and `customanchor.QueryCapabilities` reports wallet funding
as unsupported.

The engine has no universe, so `Wallet.GetBalance` and `Wallet.ListBalances`
treat an asset as known when the wallet holds or has spent an output of it.

A proof of a grouped asset verifies only if its group key is trusted. Trust
comes from `Config.GroupKeys`, from `Client.TrustGroupKey`, and from the assets
the wallet already holds.

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
