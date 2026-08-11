package tapsdk

import (
	"bytes"
	"testing"

	"github.com/btcsuite/btcd/wire/v2"
	"github.com/lightninglabs/taproot-assets/proof"
	"github.com/stretchr/testify/require"
)

// confirmationBlockFor wraps the given transactions into a minimal
// serialized block. Header fields other than structure are irrelevant:
// chain anchoring is validated by the importing backend, not by the
// assembler.
func confirmationBlockFor(t *testing.T, txs ...*wire.MsgTx) []byte {
	t.Helper()

	block := wire.MsgBlock{Header: wire.BlockHeader{Version: 2}}
	for _, tx := range txs {
		require.NoError(t, block.AddTransaction(tx))
	}

	// A single-transaction block's merkle root is that transaction's
	// txid, which is all the assembled proof's Verify needs here.
	if len(txs) == 1 {
		block.Header.MerkleRoot = txs[0].TxHash()
	}

	var encoded bytes.Buffer
	require.NoError(t, block.Serialize(&encoded))

	return encoded.Bytes()
}

// TestConfirmProofFile verifies the confirmed proof file assembly: the base
// file is extended by one proof per step, each carrying the block header,
// height, and merkle inclusion proof of its anchor transaction.
func TestConfirmProofFile(t *testing.T) {
	t.Parallel()

	fixture := newAssetProofPathFixture(t)
	path := &AssetProofPath{
		Version:            AssetProofPathVersionV0,
		ConfirmedBaseProof: fixture.baseProofFile,
		Steps: []AssetProofPathStep{{
			TransitionProof: fixture.transitionProof,
		}},
	}
	require.NoError(t, path.Validate())

	baseFile, err := proof.DecodeFile(fixture.baseProofFile)
	require.NoError(t, err)

	anchorTx := &fixture.transition.AnchorTx
	anchorTxid := anchorTx.TxHash()
	confirmations := map[Hash]AnchorConfirmation{
		Hash(anchorTxid): {
			BlockHeight: 321,
			Block:       confirmationBlockFor(t, anchorTx),
		},
	}

	encoded, err := path.ConfirmProofFile(confirmations)
	require.NoError(t, err)

	file, err := proof.DecodeFile(encoded)
	require.NoError(t, err)
	require.Equal(t, baseFile.NumProofs()+1, file.NumProofs())

	last, err := file.LastProof()
	require.NoError(t, err)
	require.EqualValues(t, 321, last.BlockHeight)
	require.Equal(t, anchorTxid, last.AnchorTx.TxHash())
	require.True(t, last.TxMerkleProof.Verify(
		anchorTx, last.BlockHeader.MerkleRoot,
	))
}

// TestConfirmProofFileMergeEmbedsCoInputs verifies a merging first step
// permanently embeds the additional base proofs as its input files, the
// layout a confirmed multi-input transition proof carries.
func TestConfirmProofFileMergeEmbedsCoInputs(t *testing.T) {
	t.Parallel()

	fixture := newAssetProofPathMergeFixture(t)
	path := &AssetProofPath{
		Version:            AssetProofPathVersionV1,
		ConfirmedBaseProof: fixture.firstBaseFile,
		AdditionalBaseProofs: [][]byte{
			fixture.secondBaseFile,
		},
		Steps: []AssetProofPathStep{{
			TransitionProof: fixture.transitionProof,
		}},
	}
	require.NoError(t, path.Validate())

	transition, err := proof.Decode(fixture.transitionProof)
	require.NoError(t, err)
	anchorTxid := transition.AnchorTx.TxHash()

	encoded, err := path.ConfirmProofFile(map[Hash]AnchorConfirmation{
		Hash(anchorTxid): {
			BlockHeight: 99,
			Block: confirmationBlockFor(
				t, &transition.AnchorTx,
			),
		},
	})
	require.NoError(t, err)

	file, err := proof.DecodeFile(encoded)
	require.NoError(t, err)
	last, err := file.LastProof()
	require.NoError(t, err)
	require.Len(t, last.AdditionalInputs, 1)
	require.EqualValues(t, 99, last.BlockHeight)
}

// TestConfirmProofFileFailsClosed covers the assembly's rejection paths.
func TestConfirmProofFileFailsClosed(t *testing.T) {
	t.Parallel()

	fixture := newAssetProofPathFixture(t)
	path := &AssetProofPath{
		Version:            AssetProofPathVersionV0,
		ConfirmedBaseProof: fixture.baseProofFile,
		Steps: []AssetProofPathStep{{
			TransitionProof: fixture.transitionProof,
		}},
	}
	anchorTxid := fixture.transition.AnchorTx.TxHash()

	// No confirmation for the step's anchor transaction.
	_, err := path.ConfirmProofFile(nil)
	require.ErrorContains(t, err, "missing block confirmation")

	// A block that does not contain the anchor transaction.
	stranger := wire.NewMsgTx(2)
	stranger.AddTxIn(&wire.TxIn{})
	stranger.AddTxOut(&wire.TxOut{Value: 1, PkScript: []byte{0x51}})
	_, err = path.ConfirmProofFile(map[Hash]AnchorConfirmation{
		Hash(anchorTxid): {
			BlockHeight: 1,
			Block:       confirmationBlockFor(t, stranger),
		},
	})
	require.ErrorContains(t, err, "not in the supplied block")

	// A path without steps is already a confirmed file.
	stepless := &AssetProofPath{
		Version:            AssetProofPathVersionV0,
		ConfirmedBaseProof: fixture.baseProofFile,
	}
	encoded, err := stepless.ConfirmProofFile(nil)
	require.NoError(t, err)
	require.Equal(t, fixture.baseProofFile, encoded)
}
