package entities

// ProveOwnershipRequest specifies parameters for proving asset ownership.
type ProveOwnershipRequest struct {
	// AssetRef is the SDK's user-facing identifier for the asset.
	AssetRef AssetRef

	// IssuanceID is the 32-byte protocol-level identifier for the specific
	// issuance/tranche being proven.
	IssuanceID AssetID

	// ScriptKey is the script key used to spend the asset.
	ScriptKey PubKey

	// Outpoint is the UTXO outpoint being proven.
	Outpoint Outpoint

	// Challenge is an optional 32-byte challenge to bind the proof.
	Challenge []byte
}

// OwnershipProof is the result of proving asset ownership.
type OwnershipProof struct {
	// ProofWithWitness is the full ownership proof with witness data.
	ProofWithWitness []byte
}

// VerifyOwnershipRequest specifies parameters for verifying an
// ownership proof.
type VerifyOwnershipRequest struct {
	// ProofWithWitness is the full ownership proof to verify.
	ProofWithWitness []byte

	// Challenge is the optional 32-byte challenge that the prover
	// was expected to include.
	Challenge []byte
}

// VerifyOwnershipResponse is the result of verifying an ownership
// proof.
type VerifyOwnershipResponse struct {
	// Valid indicates whether the ownership proof is valid.
	Valid bool

	// Outpoint is the outpoint the proof commits to.
	Outpoint Outpoint

	// BlockHash is the hash of the block containing the output.
	BlockHash Hash

	// BlockHeight is the block height of the output.
	BlockHeight uint32
}

// DeclareScriptKeyRequest specifies a script key to declare to the
// wallet.
type DeclareScriptKeyRequest struct {
	// ScriptKey is the script key to declare.
	ScriptKey ScriptKey
}
