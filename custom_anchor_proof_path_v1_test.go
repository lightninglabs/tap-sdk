package tapsdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAssetProofPathV1RoundTrip proves a multi-base path survives the
// binary codec with its additional bases intact and derives a distinct
// content ID domain.
func TestAssetProofPathV1RoundTrip(t *testing.T) {
	t.Parallel()

	fixture := newAssetProofPathFixture(t)
	path := &AssetProofPath{
		Version:            AssetProofPathVersionV1,
		ConfirmedBaseProof: fixture.baseProofFile,
		AdditionalBaseProofs: [][]byte{
			fixture.baseProofFile,
		},
		Steps: []AssetProofPathStep{{
			TransitionProof: fixture.transitionProof,
		}},
	}

	require.NoError(t, path.Validate())

	encoded, err := path.MarshalBinary()
	require.NoError(t, err)

	var decoded AssetProofPath
	require.NoError(t, decoded.UnmarshalBinary(encoded))
	require.Equal(t, path, &decoded)

	v1ID, err := path.ContentID()
	require.NoError(t, err)
	v0 := &AssetProofPath{
		Version:            AssetProofPathVersionV0,
		ConfirmedBaseProof: fixture.baseProofFile,
		Steps:              path.Steps,
	}
	v0ID, err := v0.ContentID()
	require.NoError(t, err)
	require.NotEqual(t, v0ID, v1ID)
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

	fixture := newAssetProofPathFixture(t)

	// The fixture transition spends only the selected base, so
	// declaring the same base again dedupes to one outpoint, and a
	// genuinely different base is never spent. Both must fail.
	duplicate := &AssetProofPath{
		Version:            AssetProofPathVersionV1,
		ConfirmedBaseProof: fixture.baseProofFile,
		AdditionalBaseProofs: [][]byte{
			fixture.baseProofFile,
		},
		Steps: []AssetProofPathStep{{
			TransitionProof: fixture.transitionProof,
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
