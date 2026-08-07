package tapsdk

import (
	"context"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/txscript/v2"
	"github.com/btcsuite/btcd/wire/v2"
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightninglabs/taproot-assets/commitment"
	"github.com/lightninglabs/taproot-assets/proof"
	"github.com/lightninglabs/taproot-assets/tapscript"
	"github.com/lightningnetwork/lnd/keychain"
	"github.com/stretchr/testify/require"
)

// TestAssetProofPathV1RoundTrip proves a multi-base path survives the
// binary codec with its additional bases intact and derives a distinct
// content ID domain.
func TestAssetProofPathV1RoundTrip(t *testing.T) {
	t.Parallel()

	merge := newAssetProofPathMergeFixture(t)
	path := &AssetProofPath{
		Version:            AssetProofPathVersionV1,
		ConfirmedBaseProof: merge.firstBaseFile,
		AdditionalBaseProofs: [][]byte{
			merge.secondBaseFile,
		},
		Steps: []AssetProofPathStep{{
			TransitionProof: merge.transitionProof,
		}},
	}

	require.NoError(t, path.Validate())

	encoded, err := path.MarshalBinary()
	require.NoError(t, err)

	var decoded AssetProofPath
	require.NoError(t, decoded.UnmarshalBinary(encoded))
	require.Equal(t, path, &decoded)

	// Domain separation is a property of the version tag alone, so it is
	// checked over one single-base path expressed both ways.
	single := newAssetProofPathFixture(t)
	v0 := &AssetProofPath{
		Version:            AssetProofPathVersionV0,
		ConfirmedBaseProof: single.baseProofFile,
		Steps: []AssetProofPathStep{{
			TransitionProof: single.transitionProof,
		}},
	}
	v1 := &AssetProofPath{
		Version:            AssetProofPathVersionV1,
		ConfirmedBaseProof: single.baseProofFile,
		Steps:              v0.Steps,
	}
	v0ID, err := v0.ContentID()
	require.NoError(t, err)
	v1ID, err := v1.ContentID()
	require.NoError(t, err)
	require.NotEqual(t, v0ID, v1ID)
}

// TestAssetProofPathV1RejectsUnmergedFirstStep proves a path that
// declares two bases but whose first transition spends only one is
// rejected. Accepting it would let a caller claim units the transition
// never actually consumed.
func TestAssetProofPathV1RejectsUnmergedFirstStep(t *testing.T) {
	t.Parallel()

	fixture := newAssetProofPathFixture(t)
	second, _, _ := newAssetProofPathSecondBase(t)

	path := &AssetProofPath{
		Version:            AssetProofPathVersionV1,
		ConfirmedBaseProof: fixture.baseProofFile,
		AdditionalBaseProofs: [][]byte{
			second,
		},
		Steps: []AssetProofPathStep{{
			TransitionProof: fixture.transitionProof,
		}},
	}
	require.ErrorContains(
		t, path.Validate(),
		"must contain 2 complete asset witnesses, found 1",
	)
}

// TestAssetProofPathV1ValidateRejections walks the structural rules for
// additional base proofs.
func TestAssetProofPathV1ValidateRejections(t *testing.T) {
	t.Parallel()

	fixture := newAssetProofPathFixture(t)

	t.Run("v0 rejects additional bases", func(t *testing.T) {
		t.Parallel()

		path := &AssetProofPath{
			Version:            AssetProofPathVersionV0,
			ConfirmedBaseProof: fixture.baseProofFile,
			AdditionalBaseProofs: [][]byte{
				fixture.baseProofFile,
			},
			Steps: []AssetProofPathStep{{
				TransitionProof: fixture.transitionProof,
			}},
		}
		require.ErrorContains(t, path.Validate(), "need a v1 path")
	})

	t.Run("additional bases need steps", func(t *testing.T) {
		t.Parallel()

		path := &AssetProofPath{
			Version:            AssetProofPathVersionV1,
			ConfirmedBaseProof: fixture.baseProofFile,
			AdditionalBaseProofs: [][]byte{
				fixture.baseProofFile,
			},
		}
		require.ErrorContains(
			t, path.Validate(), "multi-input transition",
		)
	})

	t.Run("identity mismatch rejected", func(t *testing.T) {
		t.Parallel()

		groupedBase, _, _ := newGroupedAssetProofPathBase(t)
		path := &AssetProofPath{
			Version:            AssetProofPathVersionV1,
			ConfirmedBaseProof: fixture.baseProofFile,
			AdditionalBaseProofs: [][]byte{
				groupedBase,
			},
			Steps: []AssetProofPathStep{{
				TransitionProof: fixture.transitionProof,
			}},
		}
		require.ErrorContains(t, path.Validate(), "additional base")
	})
}

// TestAssetProofPathV1BindingFailsClosed proves an additional base that
// the first transition does not spend is rejected at verification: the
// declared co-inputs must be exactly the transition's previous
// witnesses.
func TestAssetProofPathV1BindingFailsClosed(t *testing.T) {
	t.Parallel()

	merge := newAssetProofPathMergeFixture(t)

	// Declaring the same base twice dedupes to a single outpoint, so
	// the declared set can no longer account for both of the
	// transition's witnesses.
	duplicate := &AssetProofPath{
		Version:            AssetProofPathVersionV1,
		ConfirmedBaseProof: merge.firstBaseFile,
		AdditionalBaseProofs: [][]byte{
			merge.firstBaseFile,
		},
		Steps: []AssetProofPathStep{{
			TransitionProof: merge.transitionProof,
		}},
	}
	verifier := &testConfirmedProofVerifier{
		result: &ConfirmedProofVerification{
			AnchorAssetInventoryComplete: true,
		},
	}
	_, err := duplicate.Verify(context.Background(), verifier)
	require.ErrorContains(t, err, "duplicate base proof outpoints")
}

// assetProofPathMergeFixture is a two-base merge: the shape a round's
// commitment transition has when it batches two boarded outputs into one
// asset output.
type assetProofPathMergeFixture struct {
	firstBaseFile   []byte
	secondBaseFile  []byte
	transitionProof []byte
	firstOutPoint   wire.OutPoint
	secondOutPoint  wire.OutPoint
}

// newAssetProofPathMergeFixture builds a transition whose asset carries a
// complete witness for each of two distinct predecessors.
func newAssetProofPathMergeFixture(t *testing.T) *assetProofPathMergeFixture {
	t.Helper()

	firstFile, firstProof, firstKey := newAssetProofPathBase(t)
	secondFile, secondProof, secondKey := newAssetProofPathSecondBase(t)

	merged := newAssetProofPathMergeTransition(
		t, firstProof, secondProof, firstKey, secondKey,
		testPrivateKey(t, 3),
	)
	mergedBytes, err := merged.Bytes()
	require.NoError(t, err)

	return &assetProofPathMergeFixture{
		firstBaseFile:   firstFile,
		secondBaseFile:  secondFile,
		transitionProof: mergedBytes,
		firstOutPoint:   firstProof.OutPoint(),
		secondOutPoint:  secondProof.OutPoint(),
	}
}

// newAssetProofPathMergeTransition spends two predecessors of the same
// asset into a single output, signing each input independently.
func newAssetProofPathMergeTransition(t *testing.T, first, second *proof.Proof,
	firstKey, secondKey, recipientKey *btcec.PrivateKey) *proof.Proof {

	t.Helper()

	newAsset := first.Asset.Copy()
	newAsset.Amount = first.Asset.Amount + second.Asset.Amount
	newAsset.ScriptKey = asset.NewScriptKeyBip86(keychain.KeyDescriptor{
		PubKey: recipientKey.PubKey(),
	})

	prevIDs := make([]*asset.PrevID, 2)
	sources := []*proof.Proof{first, second}
	inputs := commitment.InputSet{}
	for i, source := range sources {
		prevIDs[i] = &asset.PrevID{
			OutPoint: source.OutPoint(),
			ID:       source.Asset.ID(),
			ScriptKey: asset.ToSerialized(
				source.Asset.ScriptKey.PubKey,
			),
		}
		inputs[*prevIDs[i]] = &source.Asset
	}
	newAsset.PrevWitnesses = []asset.Witness{
		{PrevID: prevIDs[0]},
		{PrevID: prevIDs[1]},
	}

	virtualTx, _, err := tapscript.VirtualTx(newAsset, inputs)
	require.NoError(t, err)

	keys := []*btcec.PrivateKey{firstKey, secondKey}
	for i, source := range sources {
		inputTx := asset.VirtualTxWithInput(
			virtualTx, newAsset.LockTime,
			newAsset.RelativeLockTime, uint32(i), nil,
		)
		sigHash, hashErr := tapscript.InputKeySpendSigHash(
			inputTx, &source.Asset, newAsset, uint32(i),
			txscript.SigHashDefault,
		)
		require.NoError(t, hashErr)

		signature, signErr := schnorr.Sign(
			txscript.TweakTaprootPrivKey(*keys[i], nil), sigHash,
		)
		require.NoError(t, signErr)
		newAsset.PrevWitnesses[i].TxWitness = wire.TxWitness{
			signature.Serialize(),
		}
	}

	assetCommitment, err := commitment.NewAssetCommitment(newAsset)
	require.NoError(t, err)
	commitmentVersion := commitment.TapCommitmentV2
	tapCommitment, err := commitment.NewTapCommitment(
		&commitmentVersion, assetCommitment,
	)
	require.NoError(t, err)

	spentAssets := make([]*asset.Asset, 0, len(newAsset.PrevWitnesses))
	for i := range newAsset.PrevWitnesses {
		spent, spentErr := asset.MakeSpentAsset(
			newAsset.PrevWitnesses[i],
		)
		require.NoError(t, spentErr)
		spentAssets = append(spentAssets, spent)
	}
	require.NoError(t, tapCommitment.MergeAltLeaves(
		asset.ToAltLeaves(spentAssets),
	))

	anchorTx := &wire.MsgTx{
		Version: 3,
		TxIn: []*wire.TxIn{
			{PreviousOutPoint: first.OutPoint()},
			{PreviousOutPoint: second.OutPoint()},
		},
		TxOut: []*wire.TxOut{
			assetProofPathAnchorOutput(
				t, recipientKey.PubKey(), tapCommitment,
			),
		},
	}

	transition, err := proof.CreateTransitionProof(
		first.OutPoint(), &proof.TransitionParams{
			BaseProofParams: proof.BaseProofParams{
				Block:            assetProofPathBlock(anchorTx),
				Tx:               anchorTx,
				TxIndex:          0,
				OutputIndex:      0,
				InternalKey:      recipientKey.PubKey(),
				TaprootAssetRoot: tapCommitment,
			},
			NewAsset: newAsset,
		}, proof.WithVersion(proof.TransitionV1),
	)
	require.NoError(t, err)

	return transition
}
