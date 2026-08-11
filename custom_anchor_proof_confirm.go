package tapsdk

import (
	"bytes"
	"fmt"

	"github.com/btcsuite/btcd/wire/v2"
	"github.com/lightninglabs/taproot-assets/proof"
)

// AnchorConfirmation carries the on-chain inclusion of one anchor
// transaction: the raw serialized block that contains it and the block's
// height.
type AnchorConfirmation struct {
	// BlockHeight is the height of Block in the chain.
	BlockHeight uint32

	// Block is the serialized Bitcoin block containing the anchor
	// transaction.
	Block []byte
}

// ConfirmProofFile assembles a standard confirmed proof file for the path's
// final output once every step's anchor transaction has confirmed on chain.
// Confirmations are keyed by anchor transaction ID in natural byte order.
//
// The result is the confirmed base proof file extended with one proof per
// step, each completed with its block header, height, and merkle inclusion
// proof. A merging first step permanently embeds the path's additional base
// proofs as its input files, the layout a confirmed multi-input transition
// proof carries. The caller's backend performs full validation, including
// chain anchoring, when the file is imported or spent; this assembly only
// guarantees structural completeness.
func (p *AssetProofPath) ConfirmProofFile(
	confirmations map[Hash]AnchorConfirmation) ([]byte, error) {

	if err := p.Validate(); err != nil {
		return nil, err
	}
	if len(p.Steps) == 0 {
		return cloneBytes(p.ConfirmedBaseProof), nil
	}

	file, err := proof.DecodeFile(p.ConfirmedBaseProof)
	if err != nil {
		return nil, fmt.Errorf("decode confirmed base proof file: %w",
			err)
	}

	// A merging first step needs each co-input's full lineage embedded,
	// exactly as a backend-produced multi-input transition proof would
	// carry it.
	coInputs := make([]proof.File, 0, len(p.AdditionalBaseProofs))
	for i, base := range p.AdditionalBaseProofs {
		var coInput proof.File
		if err := coInput.Decode(bytes.NewReader(base)); err != nil {
			return nil, fmt.Errorf("decode additional base "+
				"proof %d: %w", i, err)
		}
		coInputs = append(coInputs, coInput)
	}

	for i := range p.Steps {
		transition, err := decodeAssetProofPathStep(
			&p.Steps[i], p.stepWitnessCount(i),
		)
		if err != nil {
			return nil, fmt.Errorf("step %d: %w", i, err)
		}
		if i == 0 && len(coInputs) > 0 {
			transition.AdditionalInputs = coInputs
		}

		if err := confirmTransitionProof(
			transition, confirmations,
		); err != nil {
			return nil, fmt.Errorf("confirm step %d: %w", i, err)
		}

		if err := file.AppendProof(*transition); err != nil {
			return nil, fmt.Errorf("append step %d proof: %w", i,
				err)
		}
	}

	var encoded bytes.Buffer
	if err := file.Encode(&encoded); err != nil {
		return nil, fmt.Errorf("encode confirmed proof file: %w", err)
	}

	return encoded.Bytes(), nil
}

// confirmTransitionProof fills a transition proof's block inclusion fields
// from the raw block that confirmed its anchor transaction.
func confirmTransitionProof(transition *proof.Proof,
	confirmations map[Hash]AnchorConfirmation) error {

	anchorTxid := transition.AnchorTx.TxHash()
	confirmation, ok := confirmations[Hash(anchorTxid)]
	if !ok {
		return fmt.Errorf("missing block confirmation for anchor "+
			"transaction %s", anchorTxid)
	}
	if len(confirmation.Block) == 0 {
		return fmt.Errorf("empty block for anchor transaction %s",
			anchorTxid)
	}

	var block wire.MsgBlock
	if err := block.Deserialize(
		bytes.NewReader(confirmation.Block),
	); err != nil {
		return fmt.Errorf("decode block for anchor transaction %s: %w",
			anchorTxid, err)
	}

	txIndex := -1
	for idx, blockTx := range block.Transactions {
		if blockTx.TxHash() == anchorTxid {
			txIndex = idx

			break
		}
	}
	if txIndex < 0 {
		return fmt.Errorf("anchor transaction %s is not in the "+
			"supplied block", anchorTxid)
	}

	merkleProof, err := proof.NewTxMerkleProof(
		block.Transactions, txIndex,
	)
	if err != nil {
		return fmt.Errorf("build merkle proof for anchor "+
			"transaction %s: %w", anchorTxid, err)
	}

	transition.BlockHeader = block.Header
	transition.BlockHeight = confirmation.BlockHeight
	transition.TxMerkleProof = *merkleProof

	return nil
}
