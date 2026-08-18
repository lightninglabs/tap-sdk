package tapsdk

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAssetProofPathV2RoundTrip proves a path with recursive co-input paths
// survives the binary codec, derives a distinct content ID domain, and
// deep-clones its co-input tree.
func TestAssetProofPathV2RoundTrip(t *testing.T) {
	t.Parallel()

	merge := newAssetProofPathMergeFixture(t)
	path := &AssetProofPath{
		Version:            AssetProofPathVersionV2,
		ConfirmedBaseProof: merge.firstBaseFile,
		Steps: []AssetProofPathStep{{
			TransitionProof: merge.transitionProof,
			CoInputPaths: []*AssetProofPath{{
				Version:            AssetProofPathVersionV0,
				ConfirmedBaseProof: merge.secondBaseFile,
			}},
		}},
	}

	require.NoError(t, path.Validate())

	encoded, err := path.MarshalBinary()
	require.NoError(t, err)

	var decoded AssetProofPath
	require.NoError(t, decoded.UnmarshalBinary(encoded))
	require.Equal(t, path, &decoded)

	// Re-encoding the decoded path must reproduce the exact bytes.
	reEncoded, err := decoded.MarshalBinary()
	require.NoError(t, err)
	require.Equal(t, encoded, reEncoded)

	// A clone must not share co-input path bytes with the original.
	clone := decoded.Clone()
	require.Equal(t, &decoded, clone)
	clone.Steps[0].CoInputPaths[0].ConfirmedBaseProof[0] ^= 1
	require.NotEqual(
		t, clone.Steps[0].CoInputPaths[0].ConfirmedBaseProof,
		decoded.Steps[0].CoInputPaths[0].ConfirmedBaseProof,
	)

	// Domain separation is a property of the version tag alone, so it is
	// checked over one single-base path expressed all three ways.
	single := newAssetProofPathFixture(t)
	steps := []AssetProofPathStep{{
		TransitionProof: single.transitionProof,
	}}
	v0ID, err := (&AssetProofPath{
		Version:            AssetProofPathVersionV0,
		ConfirmedBaseProof: single.baseProofFile,
		Steps:              steps,
	}).ContentID()
	require.NoError(t, err)
	v1ID, err := (&AssetProofPath{
		Version:            AssetProofPathVersionV1,
		ConfirmedBaseProof: single.baseProofFile,
		Steps:              steps,
	}).ContentID()
	require.NoError(t, err)
	v2ID, err := (&AssetProofPath{
		Version:            AssetProofPathVersionV2,
		ConfirmedBaseProof: single.baseProofFile,
		Steps:              steps,
	}).ContentID()
	require.NoError(t, err)
	require.NotEqual(t, v0ID, v2ID)
	require.NotEqual(t, v1ID, v2ID)
}

// TestAssetProofPathV2ValidateRejections walks the structural rules for
// recursive co-input paths.
func TestAssetProofPathV2ValidateRejections(t *testing.T) {
	t.Parallel()

	merge := newAssetProofPathMergeFixture(t)
	coPath := func() *AssetProofPath {
		return &AssetProofPath{
			Version:            AssetProofPathVersionV0,
			ConfirmedBaseProof: merge.secondBaseFile,
		}
	}
	mergePath := func() *AssetProofPath {
		return &AssetProofPath{
			Version:            AssetProofPathVersionV2,
			ConfirmedBaseProof: merge.firstBaseFile,
			Steps: []AssetProofPathStep{{
				TransitionProof: merge.transitionProof,
				CoInputPaths: []*AssetProofPath{
					coPath(),
				},
			}},
		}
	}

	t.Run("v1 rejects co-input paths", func(t *testing.T) {
		t.Parallel()

		path := mergePath()
		path.Version = AssetProofPathVersionV1
		require.ErrorContains(t, path.Validate(), "need a v2 path")
	})

	t.Run("v2 rejects additional bases", func(t *testing.T) {
		t.Parallel()

		path := mergePath()
		path.Steps[0].CoInputPaths = nil
		path.AdditionalBaseProofs = [][]byte{merge.secondBaseFile}
		require.ErrorContains(t, path.Validate(), "need a v1 path")
	})

	t.Run("co-input path count over bound", func(t *testing.T) {
		t.Parallel()

		path := mergePath()
		for range AssetProofPathMaxStepCoPaths {
			path.Steps[0].CoInputPaths = append(
				path.Steps[0].CoInputPaths, coPath(),
			)
		}
		require.ErrorContains(t, path.Validate(), "co-input paths")
		require.ErrorContains(t, path.Validate(), "limit is 15")
	})

	t.Run("co-input path depth over bound", func(t *testing.T) {
		t.Parallel()

		// Nest one level beyond the depth bound. The inner levels use
		// garbage transition bytes: the shape pass must reject the
		// depth before decoding any content.
		path := coPath()
		for range AssetProofPathMaxCoPathDepth + 1 {
			path = &AssetProofPath{
				Version:            AssetProofPathVersionV2,
				ConfirmedBaseProof: merge.firstBaseFile,
				Steps: []AssetProofPathStep{{
					TransitionProof: []byte{1},
					CoInputPaths: []*AssetProofPath{
						path,
					},
				}},
			}
		}
		require.ErrorContains(
			t, path.Validate(), "co-input path depth exceeds 3",
		)
	})

	t.Run("witness count pinned to co-input paths", func(t *testing.T) {
		t.Parallel()

		single := newAssetProofPathFixture(t)
		path := &AssetProofPath{
			Version:            AssetProofPathVersionV2,
			ConfirmedBaseProof: single.baseProofFile,
			Steps: []AssetProofPathStep{{
				TransitionProof: single.transitionProof,
				CoInputPaths: []*AssetProofPath{
					coPath(),
				},
			}},
		}
		require.ErrorContains(
			t, path.Validate(),
			"must contain 2 complete asset witnesses, found 1",
		)
	})

	t.Run("whole-tree size budget", func(t *testing.T) {
		t.Parallel()

		// Three co-input paths of 22 MiB stay under every per-blob
		// bound but blow the shared 64 MiB tree budget. The bases are
		// garbage: the budget must trip before content is decoded.
		hugeBase := make([]byte, 22*1024*1024)
		path := mergePath()
		path.Steps[0].CoInputPaths = nil
		for range 3 {
			path.Steps[0].CoInputPaths = append(
				path.Steps[0].CoInputPaths, &AssetProofPath{
					Version: AssetProofPathVersionV0,
					ConfirmedBaseProof: hugeBase,
				},
			)
		}
		require.ErrorContains(
			t, path.Validate(), "encoded path exceeds",
		)
	})
}

// TestAssetProofPathV2RejectsHostileEncodings proves decode-time bounds trip
// before any nested content is parsed, and that unknown future versions stay
// rejected.
func TestAssetProofPathV2RejectsHostileEncodings(t *testing.T) {
	t.Parallel()

	merge := newAssetProofPathMergeFixture(t)

	t.Run("deep nesting fails fast", func(t *testing.T) {
		t.Parallel()

		// Fifty nested levels of garbage payloads: decoding must stop
		// at the depth bound without parsing the deeper levels.
		blob := encodeTestAssetProofPathBlob(
			t, AssetProofPathVersionV0, merge.secondBaseFile,
			nil, nil,
		)
		for range 50 {
			blob = encodeTestAssetProofPathBlob(
				t, AssetProofPathVersionV2,
				merge.firstBaseFile, []byte{1},
				[][]byte{blob},
			)
		}

		var decoded AssetProofPath
		err := decoded.UnmarshalBinary(blob)
		require.ErrorIs(t, err, ErrAssetProofPathInvalid)
		require.ErrorContains(t, err, "co-input path depth exceeds 3")
		require.Nil(t, decoded.ConfirmedBaseProof)
	})

	t.Run("co-input count checked before blobs", func(t *testing.T) {
		t.Parallel()

		// The declared count exceeds the bound and the garbage blobs
		// after it could never parse: the count check must trip first.
		garbage := make([][]byte, AssetProofPathMaxStepCoPaths+1)
		for i := range garbage {
			garbage[i] = []byte{0xff}
		}
		blob := encodeTestAssetProofPathBlob(
			t, AssetProofPathVersionV2, merge.firstBaseFile,
			[]byte{1}, garbage,
		)

		var decoded AssetProofPath
		err := decoded.UnmarshalBinary(blob)
		require.ErrorIs(t, err, ErrAssetProofPathInvalid)
		require.ErrorContains(t, err, "limit is 15")
	})

	t.Run("version three rejected", func(t *testing.T) {
		t.Parallel()

		path := &AssetProofPath{
			Version:            AssetProofPathVersionV2,
			ConfirmedBaseProof: merge.firstBaseFile,
			Steps: []AssetProofPathStep{{
				TransitionProof: merge.transitionProof,
				CoInputPaths: []*AssetProofPath{{
					Version: AssetProofPathVersionV0,
					ConfirmedBaseProof: merge.
						secondBaseFile,
				}},
			}},
		}
		encoded, err := path.MarshalBinary()
		require.NoError(t, err)
		binary.BigEndian.PutUint16(
			encoded[len(assetProofPathMagic):], 3,
		)
		recomputeAssetProofPathChecksum(encoded)

		var decoded AssetProofPath
		err = decoded.UnmarshalBinary(encoded)
		require.ErrorIs(t, err, ErrAssetProofPathUnknownVersion)
	})
}

// encodeTestAssetProofPathBlob writes the raw wire layout directly so
// hostile shapes that MarshalBinary refuses to produce can be exercised.
// A nil transitionProof encodes a stepless path; coBlobs only apply to V2.
func encodeTestAssetProofPathBlob(t *testing.T, version AssetProofPathVersion,
	base, transitionProof []byte, coBlobs [][]byte) []byte {

	t.Helper()

	var body bytes.Buffer
	body.Write(assetProofPathMagic[:])
	require.NoError(
		t, binary.Write(&body, binary.BigEndian, uint16(version)),
	)
	require.NoError(t, writeAssetProofPathBytes(&body, base))
	if version == AssetProofPathVersionV1 {
		require.NoError(
			t, binary.Write(&body, binary.BigEndian, uint16(0)),
		)
	}

	stepCount := uint16(0)
	if transitionProof != nil {
		stepCount = 1
	}
	require.NoError(
		t, binary.Write(&body, binary.BigEndian, stepCount),
	)
	if transitionProof != nil {
		require.NoError(
			t, writeAssetProofPathBytes(&body, transitionProof),
		)
		if version == AssetProofPathVersionV2 {
			require.NoError(t, binary.Write(
				&body, binary.BigEndian,
				uint16(len(coBlobs)),
			))
			for _, blob := range coBlobs {
				require.NoError(
					t, writeAssetProofPathBytes(
						&body, blob,
					),
				)
			}
		}
	}

	checksum := sha256.Sum256(body.Bytes())

	return append(body.Bytes(), checksum[:]...)
}
