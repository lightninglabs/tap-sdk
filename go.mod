module github.com/lightninglabs/tap-sdk

go 1.25.7

require (
	github.com/btcsuite/btcd v0.24.3-0.20250318170759-4f4ea81776d6
	github.com/btcsuite/btcd/btcec/v2 v2.3.4
	github.com/btcsuite/btcd/btcutil v1.1.5
	github.com/btcsuite/btcd/btcutil/psbt v1.1.8
	github.com/btcsuite/btcd/chaincfg/chainhash v1.1.0
	github.com/lightninglabs/taproot-assets/taprpc v1.0.12-0.20260205145325-6a06206f86f3
	github.com/stretchr/testify v1.10.0
	google.golang.org/grpc v1.76.0
)

require (
	github.com/btcsuite/btclog v0.0.0-20241003133417-09c4e92e319c // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/decred/dcrd/crypto/blake256 v1.0.1 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.3.0 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.20.0 // indirect
	github.com/juju/loggo v1.0.0 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/rogpeppe/fastuuid v1.2.0 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	golang.org/x/crypto v0.45.0 // indirect
	golang.org/x/net v0.47.0 // indirect
	golang.org/x/sys v0.38.0 // indirect
	golang.org/x/text v0.31.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20250804133106-a7a43d27e69b // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250804133106-a7a43d27e69b // indirect
	google.golang.org/protobuf v1.36.10 // indirect
	gopkg.in/errgo.v1 v1.0.1 // indirect
	gopkg.in/macaroon-bakery.v2 v2.1.0 // indirect
	gopkg.in/macaroon.v2 v2.1.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

// We want to format raw bytes as hex instead of base64. The forked version
// allows us to specify that as an option.
replace google.golang.org/protobuf => github.com/lightninglabs/protobuf-go-hex-display v1.34.2-hex-display
