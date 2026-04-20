package rest

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/lightninglabs/tap-sdk/macaroon"
)

// proofClient implements tapsdk.ProofClient over REST.
type proofClient struct {
	transport *transport
}

func newProofClient(tp *transport) *proofClient {
	return &proofClient{transport: tp}
}

// jsonExportProofRequest is the JSON body for ExportProof.
type jsonExportProofRequest struct {
	AssetID   string           `json:"asset_id"`
	ScriptKey string           `json:"script_key"`
	Outpoint  *jsonOutpointReq `json:"outpoint,omitempty"`
}

// jsonOutpointReq is the JSON shape of taprpc.OutPoint.
type jsonOutpointReq struct {
	Txid        string `json:"txid"`
	OutputIndex uint32 `json:"output_index"`
}

// ExportProof exports a proof file for a specific asset output.
func (p *proofClient) ExportProof(ctx context.Context,
	issuanceID entities.AssetID, scriptKey entities.PubKey,
	outpoint *entities.Outpoint) (*entities.ProofFile, error) {

	body := &jsonExportProofRequest{
		AssetID: hex.EncodeToString(
			issuanceID[:],
		),
		ScriptKey: hex.EncodeToString(
			scriptKey[:],
		),
	}

	if outpoint != nil {
		body.Outpoint = &jsonOutpointReq{
			Txid: hex.EncodeToString(
				outpoint.Txid[:],
			),
			OutputIndex: outpoint.Index,
		}
	}

	var resp jsonProofFile
	err := p.transport.doPost(
		ctx, "/v1/taproot-assets/proofs/export",
		macaroon.ProofServiceMac, body, &resp,
	)
	if err != nil {
		return nil, err
	}

	rawProof, err := parseHexBytes(resp.RawProofFile)
	if err != nil {
		return nil, fmt.Errorf("invalid raw_proof_file: %w", err)
	}

	proofFile := &entities.ProofFile{RawProofFile: rawProof}
	if resp.GenesisPoint != "" {
		genesisPoint, err := entities.NewOutpointFromStr(
			resp.GenesisPoint,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid genesis point: %w", err,
			)
		}

		proofFile.GenesisPoint = genesisPoint
	}

	return proofFile, nil
}

// jsonUnpackProofFileRequest is the JSON body for
// UnpackProofFile.
type jsonUnpackProofFileRequest struct {
	RawProofFile string `json:"raw_proof_file"`
}

// UnpackProofFile unpacks a proof file into individual proofs.
func (p *proofClient) UnpackProofFile(ctx context.Context,
	rawProofFile []byte) ([][]byte, error) {

	body := &jsonUnpackProofFileRequest{
		RawProofFile: hex.EncodeToString(
			rawProofFile,
		),
	}

	var resp jsonUnpackProofFileResponse
	err := p.transport.doPost(
		ctx, "/v1/taproot-assets/proofs/unpack-file",
		macaroon.ProofServiceMac, body, &resp,
	)
	if err != nil {
		return nil, err
	}

	proofs := make([][]byte, 0, len(resp.RawProofs))
	for _, rawProof := range resp.RawProofs {
		proofBytes, err := parseHexBytes(rawProof)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid raw proof: %w", err,
			)
		}

		proofs = append(proofs, proofBytes)
	}

	return proofs, nil
}

// jsonDecodeProofRequest is the JSON body for DecodeProof.
type jsonDecodeProofRequest struct {
	RawProof          string `json:"raw_proof"`
	WithMetaReveal    bool   `json:"with_meta_reveal"`
	WithPrevWitnesses bool   `json:"with_prev_witnesses"`
}

// DecodeProof decodes a raw proof and returns details about it.
func (p *proofClient) DecodeProof(ctx context.Context,
	rawProof []byte) (*entities.DecodedProof, error) {

	body := &jsonDecodeProofRequest{
		RawProof: hex.EncodeToString(
			rawProof,
		),
		WithMetaReveal:    true,
		WithPrevWitnesses: true,
	}

	var resp jsonDecodeProofResponse
	err := p.transport.doPost(
		ctx, "/v1/taproot-assets/proofs/decode",
		macaroon.ProofServiceMac, body, &resp,
	)
	if err != nil {
		return nil, err
	}

	if resp.DecodedProof == nil || resp.DecodedProof.Asset == nil {
		return nil, fmt.Errorf("invalid decoded proof response")
	}

	return unmarshalDecodedProof(resp.DecodedProof)
}

// jsonRegisterTransferRequest is the JSON body for
// RegisterTransfer.
type jsonRegisterTransferRequest struct {
	AssetID   string           `json:"asset_id"`
	GroupKey  string           `json:"group_key,omitempty"`
	ScriptKey string           `json:"script_key"`
	Outpoint  *jsonOutpointReq `json:"outpoint"`
}

// RegisterTransfer registers an inbound transfer for an
// interactive send.
func (p *proofClient) RegisterTransfer(ctx context.Context,
	assetRef entities.AssetRef, scriptKey entities.PubKey,
	outpoint entities.Outpoint) (*entities.RegisteredAsset, error) {

	if err := assetRef.Validate(); err != nil {
		return nil, err
	}

	body := &jsonRegisterTransferRequest{
		ScriptKey: hex.EncodeToString(
			scriptKey[:],
		),
		Outpoint: &jsonOutpointReq{
			Txid: hex.EncodeToString(
				outpoint.Txid[:],
			),
			OutputIndex: outpoint.Index,
		},
	}

	if assetID, ok := assetRef.AssetID(); ok {
		body.AssetID = hex.EncodeToString(assetID[:])
	}

	if groupKey, ok := assetRef.GroupKey(); ok {
		body.GroupKey = hex.EncodeToString(groupKey[:])
	}

	var resp jsonRegisterTransferResponse
	err := p.transport.doPost(
		ctx, "/v1/taproot-assets/assets/transfers/register",
		macaroon.ProofServiceMac, body, &resp,
	)
	if err != nil {
		return nil, err
	}

	if resp.RegisteredAsset == nil {
		return nil, fmt.Errorf("invalid registered asset response")
	}

	registered, err := unmarshalRegisteredAsset(resp.RegisteredAsset)
	if err != nil {
		return nil, err
	}

	if registered.AssetRef.IsZero() {
		registered.AssetRef = assetRef
	}

	return registered, nil
}

// unmarshalDecodedProof converts a JSON decoded proof to the entity
// type.
func unmarshalDecodedProof(
	d *jsonDecodedProof) (*entities.DecodedProof, error) {

	if d == nil {
		return nil, fmt.Errorf("nil decoded proof")
	}

	result := &entities.DecodedProof{
		ProofAtDepth:   d.ProofAtDepth,
		NumberOfProofs: d.NumberOfProofs,
		IsIssuance:     d.GenesisReveal != nil,
	}

	asset := d.Asset
	if asset == nil {
		return nil, fmt.Errorf("nil decoded asset")
	}

	if asset.AssetGenesis != nil {
		assetIDBytes, err := parseHexBytes(
			asset.AssetGenesis.AssetID,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid asset ID: %w", err,
			)
		}

		if len(assetIDBytes) == 32 {
			copy(result.IssuanceID[:], assetIDBytes)
		}
	}

	scriptKeyBytes, err := parseHexBytes(asset.ScriptKey)
	if err != nil {
		return nil, fmt.Errorf(
			"invalid script key: %w", err,
		)
	}

	if len(scriptKeyBytes) == 33 {
		copy(result.ScriptKey[:], scriptKeyBytes)
	}

	amount, err := parseUint64(asset.Amount)
	if err != nil {
		return nil, fmt.Errorf("invalid amount: %w", err)
	}

	result.Amount = amount

	if asset.ChainAnchor != nil {
		op, err := entities.NewOutpointFromStr(
			asset.ChainAnchor.AnchorOutpoint,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid anchor outpoint: %w", err,
			)
		}

		result.Outpoint = op
	}

	if asset.AssetGroup != nil &&
		asset.AssetGroup.TweakedGroupKey != "" {

		groupKeyBytes, err := parseHexBytes(
			asset.AssetGroup.TweakedGroupKey,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid group key: %w", err,
			)
		}

		groupKey, err := entities.ParsePubKey(groupKeyBytes)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid group key: %w", err,
			)
		}

		result.AssetRef = entities.AssetRefFromGroupKey(groupKey)
	}

	if result.AssetRef.IsZero() {
		result.AssetRef = entities.AssetRefFromAssetID(
			result.IssuanceID,
		)
	}

	// Populate prev IDs.
	if len(asset.PrevWitnesses) > 0 {
		prevIDs := make(
			[]entities.PrevID, 0,
			len(asset.PrevWitnesses),
		)
		for idx, witness := range asset.PrevWitnesses {
			if witness == nil || witness.PrevID == nil {
				return nil, fmt.Errorf(
					"missing prev_id for witness %d",
					idx,
				)
			}

			prev := witness.PrevID
			prevOutpoint, err := entities.NewOutpointFromStr(
				prev.AnchorPoint,
			)
			if err != nil {
				return nil, fmt.Errorf(
					"invalid prev_id outpoint for "+
						"witness %d: %w",
					idx, err,
				)
			}

			assetIDBytes, err := parseHexBytes(prev.AssetID)
			if err != nil {
				return nil, fmt.Errorf(
					"invalid prev_id asset_id for "+
						"witness %d: %w",
					idx, err,
				)
			}

			if len(assetIDBytes) != 32 {
				return nil, fmt.Errorf(
					"invalid prev_id asset_id "+
						"length for witness "+
						"%d: %d",
					idx, len(assetIDBytes),
				)
			}

			scriptKeyBytes, err := parseHexBytes(
				prev.ScriptKey,
			)
			if err != nil {
				return nil, fmt.Errorf(
					"invalid prev_id script_key "+
						"for witness %d: %w",
					idx, err,
				)
			}

			if len(scriptKeyBytes) != 33 {
				return nil, fmt.Errorf(
					"invalid prev_id "+
						"script_key length "+
						"for witness %d: %d",
					idx, len(scriptKeyBytes),
				)
			}

			var decodedPrev entities.PrevID
			decodedPrev.Outpoint = prevOutpoint
			copy(
				decodedPrev.IssuanceID[:],
				assetIDBytes,
			)
			copy(
				decodedPrev.ScriptKey[:],
				scriptKeyBytes,
			)

			prevIDs = append(prevIDs, decodedPrev)
		}

		result.PrevIDs = prevIDs
	}

	return result, nil
}

// unmarshalRegisteredAsset converts a JSON registered asset to the
// entity type.
func unmarshalRegisteredAsset(
	a *jsonRegisteredAsset) (*entities.RegisteredAsset, error) {

	if a == nil {
		return nil, fmt.Errorf("nil registered asset")
	}

	amount, err := parseUint64(a.Amount)
	if err != nil {
		return nil, fmt.Errorf("invalid amount: %w", err)
	}

	result := &entities.RegisteredAsset{Amount: amount}

	if a.AssetGenesis != nil {
		assetIDBytes, err := parseHexBytes(
			a.AssetGenesis.AssetID,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid asset ID: %w", err,
			)
		}

		if len(assetIDBytes) == 32 {
			copy(result.IssuanceID[:], assetIDBytes)
			result.AssetRef = entities.AssetRefFromAssetID(
				result.IssuanceID,
			)
		}
	}

	scriptKeyBytes, err := parseHexBytes(a.ScriptKey)
	if err != nil {
		return nil, fmt.Errorf(
			"invalid script key: %w", err,
		)
	}

	if len(scriptKeyBytes) == 33 {
		copy(result.ScriptKey[:], scriptKeyBytes)
	}

	if a.ChainAnchor != nil {
		op, err := entities.NewOutpointFromStr(
			a.ChainAnchor.AnchorOutpoint,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid anchor outpoint: %w", err,
			)
		}

		result.Outpoint = op
	}

	return result, nil
}
