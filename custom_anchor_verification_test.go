package tapsdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCustomAnchorVerificationResultHelpers covers OK, Errors, Warnings, and
// that addCustomAnchorIssue deep-copies index pointers.
func TestCustomAnchorVerificationResultHelpers(t *testing.T) {
	t.Parallel()

	var result CustomAnchorVerificationResult
	require.True(t, result.OK())
	require.Empty(t, result.Errors())
	require.Empty(t, result.Warnings())

	inputIndex := uint32(1)
	addCustomAnchorIssue(&result, CustomAnchorVerificationIssue{
		Code:       customAnchorIssueAmountMismatch,
		Scope:      CustomAnchorVerificationScopeAmount,
		Origin:     CustomAnchorVerificationOriginLocal,
		Severity:   CustomAnchorVerificationSeverityError,
		InputIndex: &inputIndex,
	})
	addCustomAnchorIssue(&result, CustomAnchorVerificationIssue{
		Code:     "passive_reanchor_dropped",
		Scope:    CustomAnchorVerificationScopePassiveAssets,
		Origin:   CustomAnchorVerificationOriginLocal,
		Severity: CustomAnchorVerificationSeverityWarning,
	})

	require.False(t, result.OK())
	require.Len(t, result.Errors(), 1)
	require.Len(t, result.Warnings(), 1)
	require.Equal(
		t, CustomAnchorVerificationScopeAmount, result.Errors()[0].Scope,
	)

	// A later caller mutation of the source pointer must not change the
	// recorded issue.
	inputIndex = 9
	require.Equal(t, uint32(1), *result.Errors()[0].InputIndex)
}

// TestCustomAnchorVerificationErrorUnwrap confirms the typed error wraps its
// cause and reports the issue message.
func TestCustomAnchorVerificationErrorUnwrap(t *testing.T) {
	t.Parallel()

	cause := context.Canceled
	verr := newCustomAnchorVerificationError(
		CustomAnchorVerificationScopeCapability,
		customAnchorIssueRequestInvalid,
		CustomAnchorVerificationOriginBackend, nil, nil, "", cause,
	)

	require.Equal(t, cause.Error(), verr.Error())
	require.Equal(t, cause.Error(), verr.Issue.Message)
	require.ErrorIs(t, verr, context.Canceled)
}

// TestCustomAnchorBuildStructuredFailures asserts that each Build-time
// verification failure returns a machine-branchable
// *CustomAnchorVerificationError carrying the expected scope, code, and origin.
func TestCustomAnchorBuildStructuredFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		mutate     func(t *testing.T, f *customAnchorBuilderFixture)
		wantScope  CustomAnchorVerificationScope
		wantCode   CustomAnchorVerificationCode
		wantOrigin CustomAnchorVerificationOrigin
		wantInput  bool
	}{
		{
			name: "request invalid",
			mutate: func(_ *testing.T,
				f *customAnchorBuilderFixture) {

				f.request.Inputs = nil
			},
			wantScope:  CustomAnchorVerificationScopeRequest,
			wantCode:   customAnchorIssueRequestInvalid,
			wantOrigin: CustomAnchorVerificationOriginLocal,
		},
		{
			name: "input proof decode",
			mutate: func(_ *testing.T,
				f *customAnchorBuilderFixture) {

				f.request.Inputs[0].ProofFile = []byte{0xde, 0xad}
			},
			wantScope:  CustomAnchorVerificationScopeInputProof,
			wantCode:   customAnchorIssueInputProofInvalid,
			wantOrigin: CustomAnchorVerificationOriginLocal,
			wantInput:  true,
		},
		{
			name: "backend proof invalid",
			mutate: func(_ *testing.T,
				f *customAnchorBuilderFixture) {

				f.client.verifyProof = func(context.Context,
					[]byte) (*VerifyProofResponse, error) {

					return &VerifyProofResponse{
						Valid: false,
					}, nil
				}
			},
			wantScope:  CustomAnchorVerificationScopeInputProof,
			wantCode:   customAnchorIssueInputProofInvalid,
			wantOrigin: CustomAnchorVerificationOriginBackend,
			wantInput:  true,
		},
		{
			name: "asset identity mismatch",
			mutate: func(_ *testing.T,
				f *customAnchorBuilderFixture) {

				// Point both sides at the same unrelated ref so the
				// request stays internally consistent and the
				// mismatch surfaces against the decoded proof.
				ref := AssetRefFromAssetID(AssetID{0x09})
				f.request.Inputs[0].AssetRef = ref
				f.request.Outputs[0].AssetRef = ref
			},
			wantScope:  CustomAnchorVerificationScopeAssetIdentity,
			wantCode:   customAnchorIssueAssetIdentity,
			wantOrigin: CustomAnchorVerificationOriginLocal,
			wantInput:  true,
		},
		{
			name: "amount mismatch",
			mutate: func(_ *testing.T,
				f *customAnchorBuilderFixture) {

				bumped := f.request.Inputs[0].Amount + 1
				f.request.Inputs[0].Amount = bumped
				f.request.Outputs[0].Amount = bumped
			},
			wantScope:  CustomAnchorVerificationScopeAmount,
			wantCode:   customAnchorIssueAmountMismatch,
			wantOrigin: CustomAnchorVerificationOriginLocal,
			wantInput:  true,
		},
		{
			name: "anchor output invalid",
			mutate: func(t *testing.T,
				f *customAnchorBuilderFixture) {

				anchor := mustDecodeAnchorPSBT(
					t, f.request.AnchorPSBT,
				)
				anchor.Inputs[0].WitnessUtxo.Value++
				f.request.AnchorPSBT = mustSerializeAnchorPSBT(
					t, anchor,
				)
			},
			wantScope:  CustomAnchorVerificationScopeAnchorOutput,
			wantCode:   customAnchorIssueAnchorOutput,
			wantOrigin: CustomAnchorVerificationOriginLocal,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			fixture := newCustomAnchorBuilderFixture(t)
			test.mutate(t, fixture)

			plan, err := NewWallet(fixture.client, NetworkRegtest).
				NewCustomAnchorTxBuilder().Build(
				context.Background(), fixture.request,
			)
			require.Nil(t, plan)

			var verr *CustomAnchorVerificationError
			require.ErrorAs(t, err, &verr)
			require.Equal(t, test.wantScope, verr.Issue.Scope)
			require.Equal(t, test.wantCode, verr.Issue.Code)
			require.Equal(t, test.wantOrigin, verr.Issue.Origin)
			require.Equal(
				t, CustomAnchorVerificationSeverityError,
				verr.Issue.Severity,
			)
			require.NotEmpty(t, verr.Error())

			if test.wantInput {
				require.NotNil(t, verr.Issue.InputIndex)
				require.Equal(
					t, uint32(0), *verr.Issue.InputIndex,
				)
			} else {
				require.Nil(t, verr.Issue.InputIndex)
			}
		})
	}
}
