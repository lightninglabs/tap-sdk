package macaroon

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"

	"google.golang.org/grpc/metadata"
)

// TaprpcServiceMac is the name of a macaroon that can be used to authenticate
// with a specific taprpc service.
type TaprpcServiceMac string

//nolint:revive
const (
	AdminServiceMac     TaprpcServiceMac = "admin.macaroon"
	WalletKitServiceMac TaprpcServiceMac = "walletkit.macaroon"
	ProofServiceMac     TaprpcServiceMac = "proof.macaroon"
	UniverseServiceMac  TaprpcServiceMac = "universe.macaroon"
	ReadOnlyServiceMac  TaprpcServiceMac = "readonly.macaroon"
)

var (
	// macaroonServices is the default list of macaroon file names
	// that the SDK will attempt to load if a macaroon directory is given
	// instead of a single custom macaroon.
	macaroonServices = []TaprpcServiceMac{
		WalletKitServiceMac,
		AdminServiceMac,
		ProofServiceMac,
		UniverseServiceMac,
		ReadOnlyServiceMac,
	}
)

// LoadMacaroon tries to load a macaroon file either from the default macaroon
// dir and the default filename or, if specified, from the custom macaroon path
// that overwrites the former two parameters.
func LoadMacaroon(defaultMacDir, defaultMacFileName,
	customMacPath string) (SerializedMacaroon, error) {

	// If a custom macaroon path is set, we ignore the macaroon dir and
	// default filename and always just load the custom macaroon, assuming
	// it contains all permissions needed to use the subservers.
	if customMacPath != "" {
		return newSerializedMacaroon(customMacPath)
	}

	return newSerializedMacaroon(filepath.Join(
		defaultMacDir, defaultMacFileName,
	))
}

// SerializedMacaroon is a type that represents a hex-encoded macaroon. We'll
// use this primarily vs the raw binary format as the gRPC metadata feature
// requires that all keys and values be strings.
type SerializedMacaroon string

// newSerializedMacaroon reads a new serializedMacaroon from that target
// macaroon path. If the file can't be found, then an error is returned.
func newSerializedMacaroon(macaroonPath string) (SerializedMacaroon, error) {
	macBytes, err := os.ReadFile(macaroonPath)
	if err != nil {
		return "", err
	}

	return SerializedMacaroon(hex.EncodeToString(macBytes)), nil
}

// WithMacaroonAuth modifies the passed context to include the macaroon KV
// metadata of the target macaroon. This method can be used to add the macaroon
// at call time, rather than when the connection to the gRPC server is created.
func (s SerializedMacaroon) WithMacaroonAuth(
	ctx context.Context) context.Context {

	return metadata.AppendToOutgoingContext(ctx, "macaroon", string(s))
}

// Pouch holds the set of macaroons we need to interact with tapd.
// Each sub-server has its own macaroon, and for the remaining temporary
// calls that directly hit tapd, we'll use the admin macaroon.
type Pouch map[TaprpcServiceMac]SerializedMacaroon

// NewPouch returns a new instance of a fully populated pouch
// given the directory where all the macaroons are stored.
func NewPouch(macaroonDir, customMacPath, customMacHex string) (Pouch, error) {
	// If a custom macaroon is specified, we assume it contains all
	// permissions needed for the different subservers to function and we
	// use it for all of them.
	var (
		mac SerializedMacaroon
		err error
	)

	if customMacPath != "" {
		mac, err = LoadMacaroon("", "", customMacPath)
		if err != nil {
			return nil, err
		}
	} else if customMacHex != "" {
		mac = SerializedMacaroon(customMacHex)
	}

	if mac != "" {
		return Pouch{
			WalletKitServiceMac: mac,
			AdminServiceMac:     mac,
			ProofServiceMac:     mac,
			UniverseServiceMac:  mac,
			ReadOnlyServiceMac:  mac,
		}, nil
	}

	var (
		m = make(Pouch)
	)

	for _, macName := range macaroonServices {
		m[macName], err = LoadMacaroon(
			macaroonDir, string(macName), customMacPath,
		)
		if err != nil {
			return nil, err
		}
	}

	return m, nil
}
