package entities

// ProofFile represents an exported proof file for an asset.
type ProofFile struct {
	// RawProofFile is the serialized proof file containing the full
	// provenance chain for the asset.
	RawProofFile []byte

	// GenesisPoint is the outpoint of the asset's genesis transaction.
	GenesisPoint Outpoint
}

// DecodedProof contains information extracted from a decoded proof.
type DecodedProof struct {
	// ProofAtDepth is the index depth of this proof (0 = latest).
	ProofAtDepth uint32

	// NumberOfProofs is the total number of proofs in the chain.
	NumberOfProofs uint32

	// AssetID is the 32-byte asset identifier.
	AssetID [32]byte

	// ScriptKey is the 33-byte script key.
	ScriptKey [33]byte

	// Amount is the number of asset units.
	Amount uint64

	// Outpoint is the output location in "txid:index" format.
	Outpoint Outpoint

	// GroupKey is the optional tweaked group key (if asset is grouped).
	GroupKey []byte

	// AltLeaves are auxiliary Taproot leaves committed alongside the asset.
	// Each entry is the raw per-leaf TLV stream bytes (opaque).
	AltLeaves [][]byte

	// PrevIDs are the asset-level identifiers of the inputs referenced by this
	// proof's asset witnesses. These are required to derive STXO alt leaves for
	// v1 transfer proofs.
	PrevIDs []PrevID

	// IsIssuance is true if this is a genesis/issuance proof.
	IsIssuance bool
}

// RegisteredAsset represents an asset that has been registered after
// importing a proof. This is returned when a receiver successfully imports
// a proof from an interactive transfer.
type RegisteredAsset struct {
	// AssetID is the 32-byte asset identifier.
	AssetID [32]byte

	// ScriptKey is the 33-byte script key for this asset.
	ScriptKey [33]byte

	// Amount is the number of asset units.
	Amount uint64

	// Outpoint is the output location where this asset resides.
	Outpoint Outpoint
}
