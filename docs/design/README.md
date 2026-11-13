# Design Decisions

This directory contains durable design records for the SDK. These are not
implementation plans. They document decisions that should guide future API
changes and future SDKs in other languages.

- [Asset Reference and Issuance API](asset-reference-and-issuance-api.md):
  use `AssetRef` as the developer-facing asset handle and distinguish assets,
  collections, and issuances.
- [Package and Transport Boundaries](package-and-transport-boundaries.md):
  keep business types in the root package and wire details in transport
  packages.
- [Fee Rate API](fee-rate-api.md): represent on-chain fee rates as a typed
  fixed-point value while keeping sat/vB as the user-facing unit.
- [Advanced Custom-Anchor Transactions][advanced-custom-anchor]: define
  the advanced builder, runtime, and transport boundary model.

- [Embedded Transport](embedded-transport.md): answer the client calls a
  wallet needs inside the host process, by calling tapd's packages on
  host-supplied ports.

[advanced-custom-anchor]: advanced-custom-anchor-transactions.md
