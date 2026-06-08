# Changelog

`tap-sdk` is pre-v1. Until `v1.0.0`, public APIs may change while the SDK
settles around Taproot Assets v0.8 and the planned multi-language model.

## v0.1.0 - 2026-06-08

Initial public Go SDK release for building Taproot Assets applications against
tapd v0.8.0 or newer.

### Features

- Typed `Wallet`, `Issuer`, and `Universe` surfaces over tapd.
- SDK-owned asset, collection, issuance, proof, burn, balance, transfer, and
  event types.
- gRPC and REST transports with TLS and macaroon authentication helpers.
- Address-based sends, low-level virtual PSBT builders, minting, proofs,
  universe sync, burns, ownership proofs, and event subscriptions.
- Regtest integration suite and remote-signing coordinator demo.

### Compatibility

- Taproot Assets / tapd v0.8.0 or newer.
- lnd v0.21.0-beta or newer for SDK validation.
- Go 1.25.10 or newer.

`v0.1.0` is intentionally pre-v1 because the SDK is still missing some planned
workflows and has not yet been broadly tested by external developers.
