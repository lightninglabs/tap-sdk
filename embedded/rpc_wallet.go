package embedded

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/wire/v2"
	"github.com/lightninglabs/taproot-assets/address"
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightninglabs/taproot-assets/fn"
	"github.com/lightninglabs/taproot-assets/proof"
	"github.com/lightninglabs/taproot-assets/rpcutils"
	"github.com/lightninglabs/taproot-assets/taprpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// walletServer answers the calls of tapd's TaprootAssets service that the
// engine supports.
type walletServer struct {
	taprpc.UnimplementedTaprootAssetsServer

	engine *engine
}

// GetInfo returns the engine's network and chain tip.
func (s *walletServer) GetInfo(ctx context.Context,
	_ *taprpc.GetInfoRequest) (*taprpc.GetInfoResponse, error) {

	height, hash, err := s.engine.chain.BestBlock(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to fetch best block: %w", err)
	}

	return &taprpc.GetInfoResponse{
		Version:     Version,
		Network:     s.engine.params.Name,
		BlockHeight: height,
		BlockHash:   hash.String(),
		SyncToChain: true,
	}, nil
}

// VerifyProof verifies a proof file against the chain source and the trusted
// asset groups.
func (s *walletServer) VerifyProof(ctx context.Context,
	req *taprpc.ProofFile) (*taprpc.VerifyProofResponse, error) {

	if !proof.IsProofFile(req.RawProofFile) {
		return nil, fmt.Errorf("invalid raw proof, expect file, not " +
			"single encoded mint or transition proof")
	}
	if err := proof.CheckMaxFileSize(req.RawProofFile); err != nil {
		return nil, fmt.Errorf("invalid proof file: %w", err)
	}

	proofFile, err := proof.DecodeFile(req.RawProofFile)
	if err != nil {
		return nil, fmt.Errorf("unable to decode proof file: %w", err)
	}

	_, err = proofFile.Verify(ctx, s.engine.verifierCtx(ctx))
	if err != nil {
		// A verification failure is an answer, not a failed call.
		return &taprpc.VerifyProofResponse{Valid: false}, nil
	}

	lastProof, err := proofFile.LastProof()
	if err != nil {
		return nil, err
	}
	decodedProof, err := s.engine.marshalProof(ctx, lastProof, false, false)
	if err != nil {
		return nil, fmt.Errorf("unable to marshal proof: %w", err)
	}

	decodedProof.ProofAtDepth = 0
	decodedProof.NumberOfProofs = uint32(proofFile.NumProofs())

	return &taprpc.VerifyProofResponse{
		Valid:        true,
		DecodedProof: decodedProof,
	}, nil
}

// DecodeProof decodes a single proof or one proof of a proof file.
func (s *walletServer) DecodeProof(ctx context.Context,
	req *taprpc.DecodeProofRequest) (*taprpc.DecodeProofResponse, error) {

	var rpcProof *taprpc.DecodedProof
	switch {
	case proof.IsSingleProof(req.RawProof):
		p, err := proof.Decode(req.RawProof)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument,
				"unable to decode proof: %v", err)
		}

		rpcProof, err = s.engine.marshalProof(
			ctx, p, req.WithPrevWitnesses, req.WithMetaReveal,
		)
		if err != nil {
			return nil, fmt.Errorf("unable to marshal proof: %w",
				err)
		}

		rpcProof.NumberOfProofs = 1

	case proof.IsProofFile(req.RawProof):
		if err := proof.CheckMaxFileSize(req.RawProof); err != nil {
			return nil, status.Errorf(codes.InvalidArgument,
				"invalid proof file: %v", err)
		}

		proofFile, err := proof.DecodeFile(req.RawProof)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument,
				"unable to decode proof file: %v", err)
		}

		latestProofIndex := uint32(proofFile.NumProofs() - 1)
		if req.ProofAtDepth > latestProofIndex {
			return nil, status.Errorf(codes.InvalidArgument,
				"invalid depth %d is greater than latest "+
					"proof index of %d", req.ProofAtDepth,
				latestProofIndex)
		}

		p, err := proofFile.ProofAt(latestProofIndex - req.ProofAtDepth)
		if err != nil {
			return nil, err
		}

		rpcProof, err = s.engine.marshalProof(
			ctx, p, req.WithPrevWitnesses, req.WithMetaReveal,
		)
		if err != nil {
			return nil, fmt.Errorf("unable to marshal proof: %w",
				err)
		}

		rpcProof.ProofAtDepth = req.ProofAtDepth
		rpcProof.NumberOfProofs = uint32(proofFile.NumProofs())

	default:
		return nil, status.Error(codes.InvalidArgument,
			"invalid raw proof, could not identify decoding format")
	}

	return &taprpc.DecodeProofResponse{DecodedProof: rpcProof}, nil
}

// UnpackProofFile splits a proof file into its raw proofs.
func (s *walletServer) UnpackProofFile(_ context.Context,
	req *taprpc.UnpackProofFileRequest) (*taprpc.UnpackProofFileResponse,
	error) {

	file, err := proof.Blob(req.RawProofFile).AsFile()
	if err != nil {
		return nil, fmt.Errorf("unable to decode proof file: %w", err)
	}

	proofBlobs := make([][]byte, file.NumProofs())
	for i := range file.NumProofs() {
		proofBlobs[i], err = file.RawProofAt(uint32(i))
		if err != nil {
			return nil, fmt.Errorf("unable to extract proof: %w",
				err)
		}
	}

	return &taprpc.UnpackProofFileResponse{RawProofs: proofBlobs}, nil
}

// ExportProof returns the stored proof file of an asset output.
func (s *walletServer) ExportProof(ctx context.Context,
	req *taprpc.ExportProofRequest) (*taprpc.ProofFile, error) {

	if len(req.ScriptKey) == 0 {
		return nil, status.Error(codes.InvalidArgument,
			"a valid script key must be specified")
	}
	scriptKey, err := rpcutils.ParseUserKey(req.ScriptKey)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument,
			"invalid script key: %v", err)
	}
	if len(req.AssetId) != 32 {
		return nil, status.Error(codes.InvalidArgument,
			"asset ID must be 32 bytes")
	}

	var (
		assetID  asset.ID
		outpoint *wire.OutPoint
	)
	copy(assetID[:], req.AssetId)
	if req.Outpoint != nil {
		op, err := rpcutils.UnmarshalOutPoint(req.Outpoint)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument,
				"unmarshalling outpoint: %v", err)
		}
		outpoint = &op
	}

	proofs, err := s.engine.store.FetchProofs(
		ctx, assetID, scriptKeyBytes(scriptKey), outpoint,
	)
	if err != nil {
		return nil, err
	}
	switch len(proofs) {
	case 0:
		return nil, proof.ErrProofNotFound

	case 1:
		return &taprpc.ProofFile{
			RawProofFile: proofs[0].ProofFile,
		}, nil

	default:
		return nil, proof.ErrMultipleProofs
	}
}

// ListUtxos lists the anchor outputs holding the wallet's assets.
func (s *walletServer) ListUtxos(ctx context.Context,
	req *taprpc.ListUtxosRequest) (*taprpc.ListUtxosResponse, error) {

	scriptKeyType, includeSpent, err := rpcutils.ParseScriptKeyTypeQuery(
		req.ScriptKeyType,
	)
	if err != nil {
		return nil, fmt.Errorf("unable to parse script key type "+
			"query: %w", err)
	}

	held, err := s.engine.ownedAssets(ctx, includeSpent, scriptKeyType)
	if err != nil {
		return nil, err
	}

	utxos := make(map[string]*taprpc.ManagedUtxo)
	for _, holding := range held {
		rpcAsset, err := s.engine.marshalHolding(ctx, holding)
		if err != nil {
			return nil, err
		}

		op := holding.chainAsset.AnchorOutpoint.String()
		utxo, ok := utxos[op]
		if !ok {
			utxo = &taprpc.ManagedUtxo{
				OutPoint: op,
				AmtSat:   holding.anchorValue,
				InternalKey: holding.chainAsset.
					AnchorInternalKey.
					SerializeCompressed(),
				TaprootAssetRoot: holding.taprootAssetRoot,
				MerkleRoot: holding.chainAsset.
					AnchorMerkleRoot,
			}
			utxos[op] = utxo
		}
		utxo.Assets = append(utxo.Assets, rpcAsset)
	}

	return &taprpc.ListUtxosResponse{ManagedUtxos: utxos}, nil
}

// ListAssets lists the wallet's assets.
func (s *walletServer) ListAssets(ctx context.Context,
	req *taprpc.ListAssetRequest) (*taprpc.ListAssetResponse, error) {

	scriptKeyType, includeSpent, err := rpcutils.ParseScriptKeyTypeQuery(
		req.ScriptKeyType,
	)
	if err != nil {
		return nil, fmt.Errorf("unable to parse script key type "+
			"query: %w", err)
	}

	held, err := s.engine.ownedAssets(
		ctx, includeSpent || req.IncludeSpent, scriptKeyType,
	)
	if err != nil {
		return nil, err
	}

	resp := &taprpc.ListAssetResponse{}
	for _, holding := range held {
		rpcAsset, err := s.engine.marshalHolding(ctx, holding)
		if err != nil {
			return nil, err
		}
		resp.Assets = append(resp.Assets, rpcAsset)
	}

	return resp, nil
}

// ListTransfers lists the transfers the engine recorded.
func (s *walletServer) ListTransfers(ctx context.Context,
	req *taprpc.ListTransfersRequest) (*taprpc.ListTransfersResponse,
	error) {

	var anchorTxid *chainhash.Hash
	if len(req.AnchorTxid) != 0 {
		hash, err := chainhash.NewHashFromStr(req.AnchorTxid)
		if err != nil {
			return nil, fmt.Errorf("invalid anchor tx hash: %w",
				err)
		}
		anchorTxid = hash
	}

	transfers, err := s.engine.store.ListTransfers(ctx, anchorTxid)
	if err != nil {
		return nil, fmt.Errorf("failed to query transfers: %w", err)
	}

	resp := &taprpc.ListTransfersResponse{
		Transfers: make([]*taprpc.AssetTransfer, len(transfers)),
	}
	for idx := range transfers {
		payload, err := decodeTransferPayload(transfers[idx].Payload)
		if err != nil {
			return nil, err
		}
		resp.Transfers[idx] = payload.transfer
	}

	return resp, nil
}

// RegisterTransfer adds a received asset output to the wallet. Its proofs
// must have been passed to the universe InsertProof call before, and its
// script key must be one the wallet derived or declared.
func (s *walletServer) RegisterTransfer(ctx context.Context,
	req *taprpc.RegisterTransferRequest) (*taprpc.RegisterTransferResponse,
	error) {

	switch {
	case len(req.AssetId) != 32:
		return nil, fmt.Errorf("asset ID must be 32 bytes, was %d",
			len(req.AssetId))

	case len(req.GroupKey) > 0 && len(req.GroupKey) != 33:
		return nil, fmt.Errorf("group key must be 33 bytes, was %d",
			len(req.GroupKey))

	case len(req.ScriptKey) != 33:
		return nil, fmt.Errorf("script key must be 33 bytes, was %d",
			len(req.ScriptKey))

	case req.Outpoint == nil:
		return nil, fmt.Errorf("outpoint must be specified")
	}

	scriptPubKey, err := btcec.ParsePubKey(req.ScriptKey)
	if err != nil {
		return nil, fmt.Errorf("error parsing script key: %w", err)
	}
	outpoint, err := rpcutils.UnmarshalOutPoint(req.Outpoint)
	if err != nil {
		return nil, fmt.Errorf("unmarshalling outpoint: %w", err)
	}

	locator := ProofLocator{
		ScriptKey: scriptKeyBytes(scriptPubKey),
		Outpoint:  outpoint,
	}
	copy(locator.AssetID[:], req.AssetId)

	// The wallet only takes assets it can spend, so the script key must
	// be known before the proof is imported.
	_, err = s.engine.fetchScriptKey(ctx, scriptPubKey)
	if err != nil {
		return nil, fmt.Errorf("error fetching script key, consider "+
			"declaring it with DeclareScriptKey if it does "+
			"belong to this node: %w", err)
	}

	blob, err := s.engine.registeredProofFile(ctx, locator)
	if err != nil {
		return nil, err
	}

	proofFile, err := proof.DecodeFile(blob)
	if err != nil {
		return nil, fmt.Errorf("error decoding proof file: %w", err)
	}
	lastProof, err := proofFile.LastProof()
	if err != nil {
		return nil, fmt.Errorf("error getting last proof: %w", err)
	}

	rpcAsset, err := s.engine.marshalProofAsset(ctx, lastProof, false)
	if err != nil {
		return nil, fmt.Errorf("unable to marshal chain asset: %w",
			err)
	}

	return &taprpc.RegisterTransferResponse{
		RegisteredAsset: rpcAsset,
	}, nil
}

// registeredProofFile returns the stored proof file at the locator. A file
// that is not stored yet is assembled from the staged proofs, verified and
// stored as an owned asset output.
func (e *engine) registeredProofFile(ctx context.Context,
	locator ProofLocator) ([]byte, error) {

	e.transferMu.Lock()
	defer e.transferMu.Unlock()

	stored, err := e.store.FetchProofs(
		ctx, locator.AssetID, locator.ScriptKey, &locator.Outpoint,
	)
	if err != nil {
		return nil, err
	}

	blob, err := e.stagedProofFile(locator)
	switch {
	// A retry of a completed registration finds the stored file.
	case errors.Is(err, proof.ErrProofNotFound) && len(stored) == 1:
		return stored[0].ProofFile, nil

	case err != nil:
		return nil, fmt.Errorf("error fetching full proof: %w", err)
	}

	proofFile, err := proof.DecodeFile(blob)
	if err != nil {
		return nil, fmt.Errorf("error decoding proof file: %w", err)
	}
	if _, err := proofFile.Verify(ctx, e.verifierCtx(ctx)); err != nil {
		return nil, fmt.Errorf("error verifying received proof: %w",
			err)
	}

	err = e.store.InsertProof(ctx, AssetProof{
		ProofLocator: locator,
		ProofFile:    blob,
		Owned:        true,
	})
	if err != nil {
		return nil, fmt.Errorf("error storing received proof: %w", err)
	}
	e.dropStagedProofs(proofFile)

	return blob, nil
}

// holding is one owned asset output decoded from its proof file.
type holding struct {
	chainAsset       asset.ChainAsset
	anchorValue      int64
	taprootAssetRoot []byte
	proofFile        []byte
	lastProof        *proof.Proof
}

// ownedAssets decodes the wallet's asset outputs, keeping those whose script
// key is of the wanted type.
func (e *engine) ownedAssets(ctx context.Context, includeSpent bool,
	scriptKeyType fn.Option[asset.ScriptKeyType]) ([]*holding, error) {

	records, err := e.store.ListOwnedProofs(ctx, includeSpent)
	if err != nil {
		return nil, fmt.Errorf("unable to read assets: %w", err)
	}

	held := make([]*holding, 0, len(records))
	for idx := range records {
		record := &records[idx]

		h, err := e.decodeHolding(ctx, record)
		if err != nil {
			return nil, fmt.Errorf("unable to decode asset at "+
				"%v: %w", record.Outpoint, err)
		}

		keyType := asset.ScriptKeyUnknown
		if h.chainAsset.ScriptKey.TweakedScriptKey != nil {
			keyType = h.chainAsset.ScriptKey.Type
		}
		excluded := fn.MapOptionZ(
			scriptKeyType, func(t asset.ScriptKeyType) bool {
				return keyType != t
			},
		)
		if excluded {
			continue
		}

		held = append(held, h)
	}

	return held, nil
}

// decodeHolding decodes the asset output a stored proof file creates.
func (e *engine) decodeHolding(ctx context.Context,
	record *AssetProof) (*holding, error) {

	proofFile, err := proof.DecodeFile(record.ProofFile)
	if err != nil {
		return nil, err
	}
	lastProof, err := proofFile.LastProof()
	if err != nil {
		return nil, err
	}

	chainAsset, err := e.chainAsset(ctx, lastProof)
	if err != nil {
		return nil, err
	}
	chainAsset.IsSpent = record.Spent

	commitmentKeys, err := lastProof.InclusionProof.DeriveByAssetInclusion(
		&lastProof.Asset, new(false),
	)
	if err != nil {
		return nil, err
	}
	tapCommitment, err := commitmentKeys.GetCommitment()
	if err != nil {
		return nil, err
	}
	assetRoot := tapCommitment.TapscriptRoot(nil)

	outIdx := lastProof.InclusionProof.OutputIndex
	if int(outIdx) >= len(lastProof.AnchorTx.TxOut) {
		return nil, fmt.Errorf("anchor output %d out of range", outIdx)
	}

	return &holding{
		chainAsset:       chainAsset,
		anchorValue:      lastProof.AnchorTx.TxOut[outIdx].Value,
		taprootAssetRoot: assetRoot[:],
		proofFile:        record.ProofFile,
		lastProof:        lastProof,
	}, nil
}

// chainAsset converts a proof to the asset output it creates, with the full
// script key when the wallet knows it.
func (e *engine) chainAsset(ctx context.Context,
	p *proof.Proof) (asset.ChainAsset, error) {

	chainAsset, err := p.ToChainAsset()
	if err != nil {
		return asset.ChainAsset{}, fmt.Errorf("unable to convert "+
			"proof to chain asset: %w", err)
	}

	// The proof holds the tweaked key only. Copy the asset before
	// attaching the wallet's key data, so the proof stays as decoded.
	chainAsset.Asset = chainAsset.Asset.Copy()
	tweaked, err := e.fetchScriptKey(ctx, chainAsset.ScriptKey.PubKey)
	switch {
	case errors.Is(err, address.ErrScriptKeyNotFound):

	case err != nil:
		return asset.ChainAsset{}, err

	default:
		chainAsset.ScriptKey.TweakedScriptKey = tweaked
	}

	return chainAsset, nil
}

// marshalHolding converts an owned asset output to its RPC form.
func (e *engine) marshalHolding(ctx context.Context,
	h *holding) (*taprpc.Asset, error) {

	decDisplay, err := decDisplayOption(h.lastProof.MetaReveal)
	if err != nil {
		return nil, err
	}

	return rpcutils.MarshalChainAsset(
		ctx, h.chainAsset, decDisplay, false, e,
	)
}

// marshalProofAsset converts the asset output of a proof to its RPC form.
func (e *engine) marshalProofAsset(ctx context.Context, p *proof.Proof,
	withWitness bool) (*taprpc.Asset, error) {

	chainAsset, err := e.chainAsset(ctx, p)
	if err != nil {
		return nil, err
	}
	decDisplay, err := decDisplayOption(p.MetaReveal)
	if err != nil {
		return nil, err
	}

	return rpcutils.MarshalChainAsset(
		ctx, chainAsset, decDisplay, withWitness, e,
	)
}

// decDisplayOption returns the decimal display of revealed asset metadata.
// The engine keeps no metadata, so a proof without a reveal has none.
func decDisplayOption(meta *proof.MetaReveal) (fn.Option[uint32], error) {
	if meta == nil {
		return fn.None[uint32](), nil
	}

	return meta.DecDisplayOption()
}

// marshalProof turns a proof into an RPC DecodedProof.
func (e *engine) marshalProof(ctx context.Context, p *proof.Proof,
	withPrevWitnesses, withMetaReveal bool) (*taprpc.DecodedProof, error) {

	var txMerkleProofBuf bytes.Buffer
	if err := p.TxMerkleProof.Encode(&txMerkleProofBuf); err != nil {
		return nil, fmt.Errorf("unable to encode serialized Bitcoin "+
			"merkle proof: %w", err)
	}

	var inclusionProofBuf bytes.Buffer
	if err := p.InclusionProof.Encode(&inclusionProofBuf); err != nil {
		return nil, fmt.Errorf("unable to encode inclusion proof: %w",
			err)
	}

	var exclusionProofs [][]byte
	for _, exclusionProof := range p.ExclusionProofs {
		var exclusionProofBuf bytes.Buffer
		err := exclusionProof.Encode(&exclusionProofBuf)
		if err != nil {
			return nil, fmt.Errorf("unable to encode exclusion "+
				"proofs: %w", err)
		}
		exclusionProofs = append(
			exclusionProofs, exclusionProofBuf.Bytes(),
		)
	}

	var splitRootProofBuf bytes.Buffer
	if p.SplitRootProof != nil {
		err := p.SplitRootProof.Encode(&splitRootProofBuf)
		if err != nil {
			return nil, fmt.Errorf("unable to encode split root "+
				"proof: %w", err)
		}
	}

	var altLeavesBuf bytes.Buffer
	if len(p.AltLeaves) > 0 {
		var scratch [8]byte

		altLeaves := p.AltLeaves
		err := asset.AltLeavesEncoder(
			&altLeavesBuf, &altLeaves, &scratch,
		)
		if err != nil {
			return nil, fmt.Errorf("unable to encode alt leaves: "+
				"%w", err)
		}
	}

	rpcAsset, err := e.marshalProofAsset(ctx, p, withPrevWitnesses)
	if err != nil {
		return nil, fmt.Errorf("unable to marshal chain asset: %w",
			err)
	}

	var rpcMeta *taprpc.AssetMeta
	if withMetaReveal && p.MetaReveal != nil {
		rpcMeta = &taprpc.AssetMeta{
			Data:     p.MetaReveal.Data,
			Type:     taprpc.AssetMetaType(p.MetaReveal.Type),
			MetaHash: fn.ByteSlice(p.MetaReveal.MetaHash()),
		}
	}

	decodedAssetID := p.Asset.ID()
	var genesisReveal *taprpc.GenesisReveal
	if rpcGenesis := p.GenesisReveal; rpcGenesis != nil {
		genesisReveal = &taprpc.GenesisReveal{
			GenesisBaseReveal: &taprpc.GenesisInfo{
				GenesisPoint: rpcGenesis.FirstPrevOut.String(),
				Name:         rpcGenesis.Tag,
				MetaHash:     rpcGenesis.MetaHash[:],
				AssetId:      decodedAssetID[:],
				OutputIndex:  rpcGenesis.OutputIndex,
				AssetType:    taprpc.AssetType(p.Asset.Type),
			},
		}
	}

	var groupKeyReveal taprpc.GroupKeyReveal
	if rpcGroupKey := p.GroupKeyReveal; rpcGroupKey != nil {
		rawKey := rpcGroupKey.RawKey()
		groupKeyReveal = taprpc.GroupKeyReveal{
			RawGroupKey:   rawKey[:],
			TapscriptRoot: rpcGroupKey.TapscriptRoot(),
		}
	}

	return &taprpc.DecodedProof{
		Asset:               rpcAsset,
		MetaReveal:          rpcMeta,
		TxMerkleProof:       txMerkleProofBuf.Bytes(),
		InclusionProof:      inclusionProofBuf.Bytes(),
		ExclusionProofs:     exclusionProofs,
		SplitRootProof:      splitRootProofBuf.Bytes(),
		NumAdditionalInputs: uint32(len(p.AdditionalInputs)),
		ChallengeWitness:    p.ChallengeWitness,
		IsBurn:              p.Asset.IsBurn(),
		GenesisReveal:       genesisReveal,
		GroupKeyReveal:      &groupKeyReveal,
		AltLeaves:           altLeavesBuf.Bytes(),
	}, nil
}

// scriptKeyBytes serializes a script key with the even parity asset outputs
// commit to.
func scriptKeyBytes(scriptKey *btcec.PublicKey) [33]byte {
	var serialized [33]byte
	serialized[0] = 0x02
	copy(serialized[1:], schnorr.SerializePubKey(scriptKey))

	return serialized
}
