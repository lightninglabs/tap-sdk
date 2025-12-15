package tapsdk

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/btcsuite/btcd/wire"
	"github.com/lightninglabs/tap-sdk/entities"
	grpcClients "github.com/lightninglabs/tap-sdk/grpc"
	"github.com/lightninglabs/tap-sdk/macaroon"
	"github.com/lightninglabs/tap-sdk/vpsbt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Wallet constitutes the high level service giving access to
// Taproot Assets features.
type Wallet struct {
	Client    WalletClient
	WalletKit WalletKitClient
	Proof     ProofClient
	Universe  UniverseClient

	grpcConn  *grpc.ClientConn
	macaroons macaroon.Pouch

	// Network configuration for vPacket encoding.
	networkHRP string
	coinType   uint32
}

// NewWallet creates a new Wallet instance.
func NewWallet(cfg *Config) (*Wallet, error) {
	// Of the macaroon directory, the custom macaroon path, and the custom
	// macaroon hex, we only allow one to be set at once. If all are empty,
	// that's fine, the default behavior is to use tapd's default directory
	// to try to locate the macaroons.
	macaroonOptions := []string{
		cfg.MacaroonDir,
		cfg.MacaroonPath,
		cfg.MacaroonHex,
	}
	macOptionCount := 0
	for _, option := range macaroonOptions {
		if option != "" {
			macOptionCount++
		}
	}
	if macOptionCount > 1 {
		return nil, fmt.Errorf("must set only one: MacaroonDir, " +
			"MacaroonPath, or MacaroonHex")
	}

	// Based on the network, if the macaroon directory isn't set, then
	// we'll use the expected default locations.
	macaroonDir := cfg.MacaroonDir
	if macaroonDir == "" {
		switch cfg.Network {
		case NetworkTestnet:
			macaroonDir = filepath.Join(
				defaultTapdDir, defaultDataDir,
				defaultChainSubDir, "bitcoin", "testnet",
			)

		case NetworkTestnet4:
			macaroonDir = filepath.Join(
				defaultTapdDir, defaultDataDir,
				defaultChainSubDir, "bitcoin", "testnet4",
			)

		case NetworkMainnet:
			macaroonDir = filepath.Join(
				defaultTapdDir, defaultDataDir,
				defaultChainSubDir, "bitcoin", "mainnet",
			)

		case NetworkSimnet:
			macaroonDir = filepath.Join(
				defaultTapdDir, defaultDataDir,
				defaultChainSubDir, "bitcoin", "simnet",
			)

		case NetworkSignet:
			macaroonDir = filepath.Join(
				defaultTapdDir, defaultDataDir,
				defaultChainSubDir, "bitcoin", "signet",
			)

		case NetworkRegtest:
			macaroonDir = filepath.Join(
				defaultTapdDir, defaultDataDir,
				defaultChainSubDir, "bitcoin", "regtest",
			)

		default:
			return nil, fmt.Errorf("unsupported network: %v",
				cfg.Network)
		}
	}

	conn, err := getClientConn(cfg)
	if err != nil {
		return nil, err
	}

	if cfg.RPCTimeout == 0 {
		cfg.RPCTimeout = defaultRPCTimeout
	}

	macaroons, err := macaroon.NewPouch(
		macaroonDir, cfg.MacaroonPath, cfg.MacaroonHex,
	)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to read macaroons: %v", err)
	}

	walletClient := grpcClients.NewWalletClient(
		conn, cfg.RPCTimeout, macaroons[macaroon.AdminServiceMac],
	)
	walletKitClient := grpcClients.NewWalletKitClient(
		conn, cfg.RPCTimeout, macaroons[macaroon.WalletKitServiceMac],
	)
	proofClient := grpcClients.NewProofClient(
		conn, cfg.RPCTimeout, macaroons[macaroon.AdminServiceMac],
	)
	universeClient := grpcClients.NewUniverseClient(
		conn, cfg.RPCTimeout, macaroons[macaroon.AdminServiceMac],
	)

	// Get network parameters for vPacket encoding.
	networkHRP, coinType := getNetworkParams(cfg.Network)

	return &Wallet{
		Client:     walletClient,
		WalletKit:  walletKitClient,
		Proof:      proofClient,
		Universe:   universeClient,
		grpcConn:   conn,
		macaroons:  macaroons,
		networkHRP: networkHRP,
		coinType:   coinType,
	}, nil
}

// getNetworkParams returns the HRP and coin type for a given network.
func getNetworkParams(network Network) (string, uint32) {
	switch network {
	case NetworkMainnet:
		return vpsbt.HRPMainnet, 0 // BIP-44 coin type 0 for mainnet
	case NetworkTestnet:
		return vpsbt.HRPTestnet, 1 // BIP-44 coin type 1 for testnet
	case NetworkTestnet4:
		return vpsbt.HRPTestnet4, 1
	case NetworkSignet:
		return vpsbt.HRPSignet, 1
	case NetworkSimnet:
		return vpsbt.HRPSimnet, 1
	case NetworkRegtest:
		return vpsbt.HRPRegtest, 1
	default:
		return vpsbt.HRPRegtest, 1 // Default to regtest
	}
}

// NewTxBuilder returns a new transaction builder for address-based transfers.
func (s *Wallet) NewTxBuilder() *TxBuilder {
	return NewTxBuilder(s)
}

// NewInteractiveTxBuilder returns a new builder for interactive transfers
// where the receiver provides their keys directly.
func (s *Wallet) NewInteractiveTxBuilder() *InteractiveTxBuilder {
	return newInteractiveTxBuilder(s)
}

// DeriveKeys derives a new script key and internal key for receiving assets.
// The receiver calls this method and shares the result with the sender for
// interactive transfers.
//
// This is a convenience method that combines DeriveScriptKey and
// DeriveInternalKey into a single call.
func (s *Wallet) DeriveKeys(ctx context.Context) (*entities.DerivedKeys, error) {
	scriptKey, err := s.WalletKit.DeriveScriptKey(ctx)
	if err != nil {
		return nil, wrapErr("DeriveKeys", err)
	}

	internalKey, err := s.WalletKit.DeriveInternalKey(ctx)
	if err != nil {
		return nil, wrapErr("DeriveKeys", err)
	}

	return &entities.DerivedKeys{
		ScriptKey:   *scriptKey,
		InternalKey: *internalKey,
	}, nil
}

// ExportProof exports a proof file for a specific asset output.
// This is used by the sender in interactive transfers to export proofs
// that must be delivered to the receiver out-of-band.
func (s *Wallet) ExportProof(ctx context.Context, assetID [32]byte,
	scriptKey [33]byte, outpoint *entities.Outpoint) (*entities.ProofFile,
	error) {

	proofFile, err := s.Proof.ExportProof(
		ctx, assetID[:], scriptKey[:], outpoint,
	)
	if err != nil {
		return nil, wrapErr("ExportProof", err)
	}

	return proofFile, nil
}

// ExportProofLatest exports the latest proof for the given asset/script key.
// This is equivalent to calling ExportProof with a nil outpoint.
func (s *Wallet) ExportProofLatest(ctx context.Context, assetID [32]byte,
	scriptKey [33]byte) (*entities.ProofFile, error) {

	return s.ExportProof(ctx, assetID, scriptKey, nil)
}

// ImportProof imports a proof file received from a sender during an
// interactive transfer. This method handles the full import flow:
// 1. Unpacks the proof file into individual proofs
// 2. Inserts each proof into the local universe
// 3. Registers the transfer so the wallet recognizes the new asset
//
// Returns the registered asset details.
func (s *Wallet) ImportProof(ctx context.Context,
	proofFile *entities.ProofFile) (*entities.RegisteredAsset, error) {

	// Step 1: Unpack the proof file into individual proofs.
	rawProofs, err := s.Proof.UnpackProofFile(ctx, proofFile.RawProofFile)
	if err != nil {
		return nil, wrapErr("ImportProof", err)
	}

	if len(rawProofs) == 0 {
		return nil, wrapErr("ImportProof",
			fmt.Errorf("proof file contains no proofs"))
	}

	// Step 2: Decode and insert each proof into the universe.
	var lastDecoded *entities.DecodedProof
	for _, rawProof := range rawProofs {
		decoded, err := s.Proof.DecodeProof(ctx, rawProof)
		if err != nil {
			return nil, wrapErr("ImportProof", err)
		}

		err = s.Universe.InsertProof(ctx, rawProof, decoded)
		if err != nil {
			return nil, wrapErr("ImportProof", err)
		}

		lastDecoded = decoded
	}

	// Step 3: Register the transfer using the last proof's details.
	// Parse the outpoint from the decoded proof.
	wireOutpoint, err := wire.NewOutPointFromString(lastDecoded.Outpoint)
	if err != nil {
		return nil, wrapErr("ImportProof", err)
	}

	outpoint := entities.Outpoint{
		Txid:  wireOutpoint.Hash,
		Index: wireOutpoint.Index,
	}

	registered, err := s.Proof.RegisterTransfer(
		ctx,
		lastDecoded.AssetID[:],
		lastDecoded.GroupKey,
		lastDecoded.ScriptKey[:],
		outpoint,
	)
	if err != nil {
		return nil, wrapErr("ImportProof", err)
	}

	return registered, nil
}

// Close tears down the underlying gRPC connection.
func (s *Wallet) Close() error {
	if s.grpcConn != nil {
		return s.grpcConn.Close()
	}

	return nil
}

// getClientConn gets a client connection to the tapd host.
func getClientConn(cfg *Config) (*grpc.ClientConn, error) {
	creds, err := getTLSCredentials(
		cfg.TLSData, cfg.TLSPath, cfg.Insecure, cfg.SystemCert,
	)
	if err != nil {
		return nil, fmt.Errorf("unable to get tls creds: %v", err)
	}

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(creds),
		grpc.WithDefaultCallOptions(maxMsgRecvSize),
	}

	conn, err := grpc.NewClient(cfg.Host, opts...)
	if err != nil {
		return nil, err
	}

	return conn, nil
}

// getTLSCredentials gets the tls credentials, whether provided as straight-up
// data or a path to a certificate file.
func getTLSCredentials(tlsData, tlsPath string, insecure,
	systemCert bool) (credentials.TransportCredentials, error) {

	// We'll determine if the tls certificate is passed in directly as
	// data, by a path, or try the system's certificate chain, and then
	// load it.
	var creds credentials.TransportCredentials
	switch {
	case tlsPath != "" && tlsData != "":
		return nil, fmt.Errorf("must set only one: TLSPath or TLSData")

	case insecure && systemCert:
		return nil, fmt.Errorf("cannot set insecure and system cert " +
			"at the same time")

	case insecure:
		// If we don't need to use tls, such as if we're connecting to
		// tapd via a bufconn, then we'll skip verification.
		creds = credentials.NewTLS(&tls.Config{
			InsecureSkipVerify: true, // nolint:gosec
		})

	case systemCert:
		// Fallback to the system pool. Using an empty tls config is an
		// alternative to x509.SystemCertPool(), which is not supported
		// on Windows.
		creds = credentials.NewTLS(&tls.Config{})

	case tlsData != "":
		tlsBytes := []byte(tlsData)

		block, _ := pem.Decode(tlsBytes)
		if block == nil || block.Type != "CERTIFICATE" {
			return nil, errors.New("failed to decode PEM block " +
				"containing tls certificate")
		}

		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}

		pool := x509.NewCertPool()
		pool.AddCert(cert)

		// Load the specified TLS certificate and build transport
		// credentials.
		creds = credentials.NewClientTLSFromCert(pool, "")

	case tlsPath != "":
		var err error
		creds, err = credentials.NewClientTLSFromFile(tlsPath, "")
		if err != nil {
			return nil, err
		}

	default:
		// If neither tlsData nor tlsPath were set, we'll try the
		// default tls cert path.
		_, err := os.Stat(defaultTLSCertPath)
		if err != nil {
			return nil, fmt.Errorf("couldn't find out if default "+
				"TLS cert at %s exists: %v",
				defaultTLSCertPath, err)
		}
		creds, err = credentials.NewClientTLSFromFile(
			defaultTLSCertPath, "",
		)
		if err != nil {
			return nil, fmt.Errorf("couldn't load default "+
				"TLS cert at %s: %v", defaultTLSCertPath, err)
		}
	}

	return creds, nil
}
