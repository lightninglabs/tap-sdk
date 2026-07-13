package tapsdk

import (
	"context"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightninglabs/taproot-assets/proof"
	"github.com/lightninglabs/taproot-assets/tappsbt"
	"github.com/lightninglabs/taproot-assets/tapsend"
	"github.com/stretchr/testify/require"
)

// TestCustomAnchorPathBuilderConsumesUnconfirmedTip verifies the complete Ark
// edge case: the confirmed base is delegated to an injected verifier, the
// unconfirmed step is verified locally despite its BTC-only connector input,
// and the path tip becomes the virtual transaction predecessor.
func TestCustomAnchorPathBuilderConsumesUnconfirmedTip(t *testing.T) {
	t.Parallel()

	fixture := newCustomAnchorPathBuilderFixture(t)
	builder := NewWallet(fixture.client, NetworkRegtest).
		NewCustomAnchorTxBuilder()
	builder.SetConfirmedProofVerifier(fixture.verifier)

	plan, err := builder.Build(
		context.Background(), fixture.request,
	)
	require.NoError(t, err)
	require.Equal(t, 1, fixture.verifier.calls)
	require.Len(t, fixture.transition.AnchorTx.TxIn, 2,
		"the path step should retain its BTC-only connector input")

	var baseCheck, hostCheck, localCheck bool
	for _, check := range plan.Verification().Checks {
		switch check.Code {
		case customAnchorCheckProofChain:
			baseCheck = check.Origin == CustomAnchorVerificationOriginHost

		case customAnchorCheckBTCPath:
			hostCheck = check.Origin == CustomAnchorVerificationOriginHost

		case customAnchorCheckAssetPath:
			localCheck = check.Origin == CustomAnchorVerificationOriginLocal
		}
	}
	require.True(t, baseCheck,
		"the confirmed proof base must retain injected verifier trust")
	require.True(t, hostCheck,
		"the injected Bitcoin graph attestation must be host-trusted")
	require.True(t, localCheck,
		"the asset transition verification must remain SDK-local")

	packets := plan.ActiveVirtualPSBTs()
	require.Len(t, packets, 1)
	packet, err := tappsbt.Decode(packets[0])
	require.NoError(t, err)
	require.Len(t, packet.Inputs, 1)
	require.Equal(t, fixture.transition.OutPoint(),
		packet.Inputs[0].PrevID.OutPoint)
	require.Equal(t, fixture.transition.Asset.ID(),
		packet.Inputs[0].PrevID.ID)
	require.Equal(t, asset.ToSerialized(
		fixture.transition.Asset.ScriptKey.PubKey,
	), packet.Inputs[0].PrevID.ScriptKey)
	require.NotNil(t, packet.Inputs[0].Proof)
	require.Equal(t, fixture.transition.OutPoint(),
		packet.Inputs[0].Proof.OutPoint())
}

// TestCustomAnchorPathBuilderRequiresOneProofSource keeps confirmed proofs and
// compact paths a disjoint input union.
func TestCustomAnchorPathBuilderRequiresOneProofSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*CustomAssetInput, *AssetProofPath)
	}{
		{
			name: "neither source",
			mutate: func(input *CustomAssetInput, _ *AssetProofPath) {
				input.ProofFile = nil
				input.ProofPath = nil
			},
		},
		{
			name: "both sources",
			mutate: func(input *CustomAssetInput,
				path *AssetProofPath) {

				input.ProofFile = cloneBytes(path.ConfirmedBaseProof)
				input.ProofPath = path.Clone()
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			fixture := newCustomAnchorPathBuilderFixture(t)
			request := fixture.request.Clone()
			test.mutate(&request.Inputs[0], fixture.path)
			builder := NewWallet(fixture.client, NetworkRegtest).
				NewCustomAnchorTxBuilder()
			builder.SetConfirmedProofVerifier(fixture.verifier)

			_, err := builder.Build(context.Background(), request)
			require.ErrorContains(t, err, "exactly one")
		})
	}
}

// TestCustomAnchorPathBuilderRequiresVerifier proves a compact path cannot
// silently downgrade the confirmed base to skip-chain verification.
func TestCustomAnchorPathBuilderRequiresVerifier(t *testing.T) {
	t.Parallel()

	fixture := newCustomAnchorPathBuilderFixture(t)
	builder := NewWallet(fixture.client, NetworkRegtest).
		NewCustomAnchorTxBuilder()

	_, err := builder.Build(context.Background(), fixture.request)
	require.ErrorContains(t, err, "confirmed proof verifier is required")
}

// TestCustomAnchorPathBuilderRejectsBackendSigner ensures tapd is never asked
// to sign an input state it has not imported from a confirmed proof file.
func TestCustomAnchorPathBuilderRejectsBackendSigner(t *testing.T) {
	t.Parallel()

	fixture := newCustomAnchorPathBuilderFixture(t)
	request := fixture.request.Clone()
	request.Inputs[0].Witness = CustomAssetWitnessPlan{
		Mode: CustomAssetWitnessBackendSigner,
	}
	builder := NewWallet(fixture.client, NetworkRegtest).
		NewCustomAnchorTxBuilder()
	builder.SetConfirmedProofVerifier(fixture.verifier)

	_, err := builder.Build(context.Background(), request)
	require.ErrorContains(t, err,
		"proof path inputs require caller-provided witnesses")
}

// TestCustomAnchorPathBuilderRejectsTipMismatches verifies the request binds
// the locally derived path-tip identity and exact amount.
func TestCustomAnchorPathBuilderRejectsTipMismatches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*CustomAnchorRequest)
		wantErr string
	}{
		{
			name: "asset identity",
			mutate: func(request *CustomAnchorRequest) {
				var otherID AssetID
				otherID[0] = 99
				otherRef := AssetRefFromAssetID(otherID)
				request.Inputs[0].AssetRef = otherRef
				request.Outputs[0].AssetRef = otherRef
			},
			wantErr: "path asset ref",
		},
		{
			name: "amount",
			mutate: func(request *CustomAnchorRequest) {
				request.Inputs[0].Amount = 99
				request.Outputs[0].Amount = 99
			},
			wantErr: "path amount 100 does not match requested 99",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			fixture := newCustomAnchorPathBuilderFixture(t)
			request := fixture.request.Clone()
			test.mutate(request)
			builder := NewWallet(fixture.client, NetworkRegtest).
				NewCustomAnchorTxBuilder()
			builder.SetConfirmedProofVerifier(fixture.verifier)

			_, err := builder.Build(context.Background(), request)
			require.ErrorContains(t, err, test.wantErr)
		})
	}
}

// TestCustomAnchorPathBuilderRejectsDuplicateTip prevents counting the same
// concrete unconfirmed state twice even when logical input IDs differ.
func TestCustomAnchorPathBuilderRejectsDuplicateTip(t *testing.T) {
	t.Parallel()

	fixture := newCustomAnchorPathBuilderFixture(t)
	request := fixture.request.Clone()
	duplicate := request.Inputs[0]
	duplicate.ID = "duplicate-path-tip"
	duplicate.ProofPath = duplicate.ProofPath.Clone()
	request.Inputs = append(request.Inputs, duplicate)
	request.Outputs[0].Amount *= 2
	builder := NewWallet(fixture.client, NetworkRegtest).
		NewCustomAnchorTxBuilder()
	builder.SetConfirmedProofVerifier(fixture.verifier)

	_, err := builder.Build(context.Background(), request)
	require.ErrorContains(t, err, "duplicates asset predecessor")
}

type customAnchorPathBuilderFixture struct {
	request    *CustomAnchorRequest
	path       *AssetProofPath
	transition *proof.Proof
	verifier   *testConfirmedProofVerifier
	client     *customAnchorBuilderTestClient
}

func newCustomAnchorPathBuilderFixture(
	t *testing.T) *customAnchorPathBuilderFixture {

	t.Helper()

	baseProofFile, baseProof, baseSpendKey := newAssetProofPathBase(t)
	tipSpendKey := testPrivateKey(t, 53)
	transition := newAssetProofPathTransition(
		t, baseProof, baseSpendKey, tipSpendKey,
	)
	transitionBytes, err := transition.Bytes()
	require.NoError(t, err)
	transitionBytes = mutateAssetProofPathTransition(
		t, transitionBytes, func(p *proof.Proof) {
			var connectorHash chainhash.Hash
			connectorHash[0] = 0x44
			p.AnchorTx.TxIn = append(p.AnchorTx.TxIn, &wire.TxIn{
				PreviousOutPoint: wire.OutPoint{
					Hash:  connectorHash,
					Index: 3,
				},
				Sequence: 0xfffffffd,
			})
		},
	)
	transition, err = proof.Decode(transitionBytes)
	require.NoError(t, err)

	path := &AssetProofPath{
		Version:            AssetProofPathVersionV0,
		ConfirmedBaseProof: baseProofFile,
		Steps: []AssetProofPathStep{{
			TransitionProof: transitionBytes,
		}},
	}
	assetRef, _, _, err := assetIdentity(&transition.Asset)
	require.NoError(t, err)

	btcInputKey := testPrivateKey(t, 7)
	anchor := customAnchorCallerTemplate(
		t, []*proof.Proof{transition}, 3, 42,
		[]uint32{0x11111111, 0x22222222}, []wire.TxOut{
			{Value: 12_345, PkScript: []byte{txscript.OP_TRUE}},
			{Value: 777, PkScript: []byte{txscript.OP_FALSE}},
			{Value: 0, PkScript: cloneBytes(canonicalP2AScript)},
		},
	)
	request := &CustomAnchorRequest{
		Inputs: []CustomAssetInput{{
			ID:        "unconfirmed-path-tip",
			AssetRef:  assetRef,
			Amount:    transition.Asset.Amount,
			ProofPath: path,
		}},
		Outputs: []CustomAssetOutput{{
			ID:                "next-ark-edge",
			AssetRef:          assetRef,
			Amount:            transition.Asset.Amount,
			AnchorOutputIndex: 1,
			AnchorValueSat:    777,
			Script: CustomAssetScriptPlan{
				Mode: CustomAssetScriptExternal,
				External: &CustomAssetExternalScriptPlan{
					ScriptKey: customAnchorExternalScriptKey(
						t, testPrivateKey(t, 54),
					),
				},
			},
			Anchor: CustomAnchorOutputPlan{
				InternalKey: InternalKey{
					PubKey: mustCustomAnchorPubKey(
						t, testPrivateKey(t, 55).PubKey(),
					),
				},
			},
		}},
		AnchorPSBT: anchor,
		Funding: CustomAnchorFundingPlan{
			Mode: CustomAnchorFundingExternalP2AFeeBump,
			ExternalP2AFeeBump: &CustomAnchorExternalP2AFeeBump{
				P2AOutputIndex: 2,
			},
		},
		PassiveAssets: CustomAnchorPassiveAssets{
			Policy: CustomAnchorPassiveReject,
		},
		SigningPlans: []CustomAnchorInputSigningPlan{
			{
				InputIndex: 0,
				KeyPath: &CustomAnchorKeyPathSigningPlan{
					Signer: mustCustomAnchorXOnly(
						t, btcInputKey.PubKey(),
					),
				},
			},
			{
				InputIndex: 1,
				KeyPath: &CustomAnchorKeyPathSigningPlan{
					Signer: mustCustomAnchorXOnly(
						t,
						transition.InclusionProof.InternalKey,
					),
				},
			},
		},
	}

	client := &customAnchorBuilderTestClient{}
	builder := NewWallet(client, NetworkRegtest).
		NewCustomAnchorTxBuilder()
	request.Inputs[0].Witness = CustomAssetWitnessPlan{
		Mode: CustomAssetWitnessCallerProvided,
		Stack: customAnchorPathSpendWitness(
			t, builder, transition, request.Outputs[0], tipSpendKey,
		),
	}

	return &customAnchorPathBuilderFixture{
		request:    request,
		path:       path,
		transition: transition,
		verifier: &testConfirmedProofVerifier{
			result: &ConfirmedProofVerification{
				AnchorAssetInventoryComplete: true,
			},
		},
		client: client,
	}
}

func customAnchorPathSpendWitness(t *testing.T,
	builder *CustomAnchorTxBuilder, inputProof *proof.Proof,
	output CustomAssetOutput, spendKey *btcec.PrivateKey) [][]byte {

	t.Helper()

	internalKey := mustParseCustomAnchorPubKey(
		t, output.Anchor.InternalKey.PubKey,
	)
	scriptKey, err := tapAssetScriptKey(output.Script.External.ScriptKey)
	require.NoError(t, err)
	packets, err := tapsend.DistributeCoins(
		[]*proof.Proof{inputProof}, []*tapsend.Allocation{{
			Type:         tapsend.CommitAllocationToRemote,
			OutputIndex:  output.AnchorOutputIndex,
			InternalKey:  internalKey,
			GenScriptKey: tapsend.StaticScriptKeyGen(scriptKey),
			Amount:       output.Amount,
			AssetVersion: asset.V1,
			BtcAmount:    btcutil.Amount(output.AnchorValueSat),
		}}, builder.params, true, tappsbt.V1,
	)
	require.NoError(t, err)
	require.Len(t, packets, 1)
	require.NoError(t, tapsend.PrepareOutputAssets(
		context.Background(), packets[0],
	))
	encoded, err := tappsbt.Encode(packets[0])
	require.NoError(t, err)

	signed := customAnchorSignVirtualPacket(t, spendKey, encoded)
	signedPacket, err := tappsbt.Decode(signed)
	require.NoError(t, err)
	require.Len(t, signedPacket.Outputs, 1)
	newAsset := signedPacket.Outputs[0].Asset
	require.NotNil(t, newAsset)
	require.Len(t, newAsset.PrevWitnesses, 1)
	require.NotEmpty(t, newAsset.PrevWitnesses[0].TxWitness)

	return cloneWitness(newAsset.PrevWitnesses[0].TxWitness)
}
