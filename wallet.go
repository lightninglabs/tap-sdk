package tapsdk

import (
	"context"
	"fmt"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/lightninglabs/tap-sdk/vpsbt"
)

// Wallet constitutes the high level service giving access to
// Taproot Assets features.
type Wallet struct {
	Client

	networkHRP string
	coinType   uint32
}

// NewWallet creates a new Wallet instance.
func NewWallet(client Client, network entities.Network) *Wallet {
	// Get network parameters for vPacket encoding.
	networkHRP, coinType := getNetworkParams(network)

	return &Wallet{
		Client:     client,
		networkHRP: networkHRP,
		coinType:   coinType,
	}
}

// NewTxBuilder returns a new transaction builder for address-based transfers.
func (s *Wallet) NewTxBuilder() *TxBuilder {
	return newTxBuilder(s)
}

// NewInteractiveTxBuilder returns a new builder for interactive transfers
// where the receiver provides their keys directly.
func (s *Wallet) NewInteractiveTxBuilder() *InteractiveTxBuilder {
	return newInteractiveTxBuilder(s, s.networkHRP, s.coinType)
}

// DeriveKeys derives a new script key and internal key for receiving assets.
// The receiver calls this method and shares the result with the sender for
// interactive transfers.
//
// This is a convenience method that combines DeriveScriptKey and
// DeriveInternalKey into a single call.
func (s *Wallet) DeriveKeys(ctx context.Context) (*entities.DerivedKeys,
	error) {

	scriptKey, err := s.DeriveScriptKey(ctx)
	if err != nil {
		return nil, wrapErr("DeriveKeys", err)
	}

	internalKey, err := s.DeriveInternalKey(ctx)
	if err != nil {
		return nil, wrapErr("DeriveKeys", err)
	}

	return &entities.DerivedKeys{
		ScriptKey:   *scriptKey,
		InternalKey: *internalKey,
	}, nil
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
	rawProofs, err := s.UnpackProofFile(ctx, proofFile.RawProofFile)
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
		// TODO: Decode the proof locally without using the RPC client.
		decoded, err := s.DecodeProof(ctx, rawProof)
		if err != nil {
			return nil, wrapErr("ImportProof", err)
		}

		err = s.InsertProof(ctx, rawProof, decoded)
		if err != nil {
			return nil, wrapErr("ImportProof", err)
		}

		lastDecoded = decoded
	}

	// Step 3: Register the transfer using the last proof's details.
	registered, err := s.RegisterTransfer(
		ctx,
		lastDecoded.AssetID,
		lastDecoded.GroupKey,
		lastDecoded.ScriptKey,
		lastDecoded.Outpoint,
	)
	if err != nil {
		return nil, wrapErr("ImportProof", err)
	}

	return registered, nil
}

// Close tears down the underlying client connection if it exists.
func (s *Wallet) Close() error {
	return s.Client.Close()
}

// getNetworkParams returns the HRP and coin type for a given network.
func getNetworkParams(network entities.Network) (string, uint32) {
	switch network {
	case entities.NetworkMainnet:
		return vpsbt.MainnetHRP, 0 // BIP-44 coin type 0 for mainnet
	case entities.NetworkTestnet:
		return vpsbt.TestnetHRP, 1 // BIP-44 coin type 1 for testnet
	case entities.NetworkTestnet4:
		return vpsbt.Testnet4HRP, 1
	case entities.NetworkSignet:
		return vpsbt.SigNetHRP, 1
	case entities.NetworkSimnet:
		return vpsbt.SimNetHRP, 1
	case entities.NetworkRegtest:
		return vpsbt.RegTestHRP, 1
	default:
		return vpsbt.RegTestHRP, 1 // Default to regtest
	}
}
