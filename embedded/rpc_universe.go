package embedded

import (
	"bytes"
	"context"
	"fmt"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightninglabs/taproot-assets/proof"
	unirpc "github.com/lightninglabs/taproot-assets/taprpc/universerpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// maxProofChainLength bounds the proofs RegisterTransfer walks back when it
// assembles a staged proof file.
const maxProofChainLength = 100_000

// universeServer answers the one call of tapd's Universe service that the
// engine supports.
type universeServer struct {
	unirpc.UnimplementedUniverseServer

	engine *engine
}

// InsertProof stages one proof of a received asset. The engine runs no
// universe: staged proofs are held in memory until RegisterTransfer assembles
// them into a proof file, verifies it and stores it.
func (s *universeServer) InsertProof(_ context.Context,
	req *unirpc.AssetProof) (*unirpc.AssetProofResponse, error) {

	if req.AssetLeaf == nil || len(req.AssetLeaf.Proof) == 0 {
		return nil, status.Error(codes.InvalidArgument,
			"asset leaf proof is required")
	}

	rawProof := req.AssetLeaf.Proof
	p, err := proof.Decode(rawProof)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument,
			"unable to decode proof: %v", err)
	}
	locator := proofLocator(p)

	e := s.engine
	e.stagedMu.Lock()
	defer e.stagedMu.Unlock()

	_, known := e.staged[locator]
	if !known && len(e.staged) >= maxStagedProofs {
		return nil, status.Error(codes.ResourceExhausted,
			"too many proofs await RegisterTransfer")
	}
	e.staged[locator] = append([]byte(nil), rawProof...)

	return &unirpc.AssetProofResponse{Req: req.Key}, nil
}

// proofLocator returns the locator of the asset output a proof creates.
func proofLocator(p *proof.Proof) ProofLocator {
	return ProofLocator{
		AssetID:   p.Asset.ID(),
		ScriptKey: scriptKeyBytes(p.Asset.ScriptKey.PubKey),
		Outpoint:  p.OutPoint(),
	}
}

// prevIDLocator returns the locator of the asset output a previous ID names.
func prevIDLocator(prevID asset.PrevID) (ProofLocator, error) {
	scriptKey, err := btcec.ParsePubKey(prevID.ScriptKey[:])
	if err != nil {
		return ProofLocator{}, fmt.Errorf("parse input script key: %w",
			err)
	}

	return ProofLocator{
		AssetID:   prevID.ID,
		ScriptKey: scriptKeyBytes(scriptKey),
		Outpoint:  prevID.OutPoint,
	}, nil
}

// stagedProofFile assembles the proof file that ends at the locator from the
// staged proofs, walking back each proof's previous output to the issuance.
// It returns proof.ErrProofNotFound when a proof of the chain is not staged.
func (e *engine) stagedProofFile(locator ProofLocator) ([]byte, error) {
	e.stagedMu.Lock()
	defer e.stagedMu.Unlock()

	var chain []proof.Proof
	for current := locator; ; {
		rawProof, ok := e.staged[current]
		if !ok {
			return nil, proof.ErrProofNotFound
		}
		p, err := proof.Decode(rawProof)
		if err != nil {
			return nil, err
		}
		chain = append(chain, *p)

		if p.Asset.IsGenesisAsset() {
			break
		}
		if len(chain) > maxProofChainLength {
			return nil, fmt.Errorf("proof chain is too long")
		}

		// The file continues with the proof of the input anchored at
		// the previous output; further inputs are embedded in the
		// proof itself.
		var next *ProofLocator
		for _, witness := range p.Asset.Witnesses() {
			if witness.PrevID == nil ||
				witness.PrevID.OutPoint != p.PrevOut {

				continue
			}

			prev, err := prevIDLocator(*witness.PrevID)
			if err != nil {
				return nil, err
			}
			next = &prev

			break
		}
		if next == nil {
			return nil, fmt.Errorf("proof at %v spends no asset "+
				"of its previous output", p.OutPoint())
		}
		current = *next
	}

	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}

	file, err := proof.NewFile(proof.V0, chain...)
	if err != nil {
		return nil, err
	}
	var encoded bytes.Buffer
	if err := file.Encode(&encoded); err != nil {
		return nil, err
	}

	return encoded.Bytes(), nil
}

// dropStagedProofs forgets the staged proofs of an imported file.
func (e *engine) dropStagedProofs(file *proof.File) {
	e.stagedMu.Lock()
	defer e.stagedMu.Unlock()

	for i := range file.NumProofs() {
		p, err := file.ProofAt(uint32(i))
		if err != nil {
			continue
		}
		delete(e.staged, proofLocator(p))
	}
}
