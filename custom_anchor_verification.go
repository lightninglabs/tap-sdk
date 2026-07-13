package tapsdk

// Stable, machine-readable issue codes for custom-anchor verification
// failures. Concrete verifiers own their code vocabulary; these cover the plan
// and package verification path.
const (
	customAnchorIssueRequestInvalid    = "request_invalid"
	customAnchorIssueInputProofInvalid = "input_proof_invalid"
	customAnchorIssueInputTimelock     = "input_timelock_unsupported"
	customAnchorIssueAssetIdentity     = "asset_identity_mismatch"
	customAnchorIssueAmountMismatch    = "amount_mismatch"
	customAnchorIssueOutputCommitment  = "output_commitment_invalid"
	customAnchorIssueAnchorOutput      = "anchor_output_invalid"
	customAnchorIssueDuplicateInput    = "duplicate_input_predecessor"
)

// CustomAnchorVerificationError is a structured, machine-branchable
// custom-anchor verification failure. It carries a
// CustomAnchorVerificationIssue so callers can branch on Scope, Code, Origin,
// and Severity via errors.As even when Build or Commit return a nil plan, and
// it unwraps to the underlying cause.
type CustomAnchorVerificationError struct {
	// Issue is the structured verification finding.
	Issue CustomAnchorVerificationIssue

	// cause is the wrapped underlying error, if any.
	cause error
}

// Error implements the error interface.
func (e *CustomAnchorVerificationError) Error() string {
	if e == nil {
		return "<nil custom anchor verification error>"
	}

	return e.Issue.Message
}

// Unwrap returns the wrapped cause so errors.Is/As can inspect it.
func (e *CustomAnchorVerificationError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.cause
}

// newCustomAnchorVerificationError builds an error-severity verification error
// for scope with the given code and origin, optional input/output locators, and
// the wrapped cause. The cause message becomes the issue's diagnostic text.
func newCustomAnchorVerificationError(scope CustomAnchorVerificationScope,
	code CustomAnchorVerificationCode, origin CustomAnchorVerificationOrigin,
	inputIndex, outputIndex *uint32, outputID string,
	cause error) *CustomAnchorVerificationError {

	return &CustomAnchorVerificationError{
		Issue: CustomAnchorVerificationIssue{
			Code:        code,
			Scope:       scope,
			Origin:      origin,
			Severity:    CustomAnchorVerificationSeverityError,
			InputIndex:  cloneUint32(inputIndex),
			OutputIndex: cloneUint32(outputIndex),
			OutputID:    outputID,
			Message:     cause.Error(),
		},
		cause: cause,
	}
}

// customAnchorInputFailure is a convenience constructor for input-scoped
// verification failures.
func customAnchorInputFailure(idx uint32,
	scope CustomAnchorVerificationScope, code CustomAnchorVerificationCode,
	origin CustomAnchorVerificationOrigin,
	cause error) *CustomAnchorVerificationError {

	inputIndex := idx

	return newCustomAnchorVerificationError(
		scope, code, origin, &inputIndex, nil, "", cause,
	)
}

// addCustomAnchorIssue records a structured verification finding on the result.
// It is the failure and warning counterpart to addCustomAnchorCheck.
func addCustomAnchorIssue(result *CustomAnchorVerificationResult,
	issue CustomAnchorVerificationIssue) {

	issue.InputIndex = cloneUint32(issue.InputIndex)
	issue.OutputIndex = cloneUint32(issue.OutputIndex)
	result.Issues = append(result.Issues, issue)
}

// OK reports whether the result contains no error-severity issues.
func (r CustomAnchorVerificationResult) OK() bool {
	return len(r.issuesBySeverity(CustomAnchorVerificationSeverityError)) == 0
}

// Errors returns the error-severity issues, which invalidate the result.
func (r CustomAnchorVerificationResult) Errors() (
	issues []CustomAnchorVerificationIssue) {

	return r.issuesBySeverity(CustomAnchorVerificationSeverityError)
}

// Warnings returns the warning-severity issues, which do not invalidate the
// result.
func (r CustomAnchorVerificationResult) Warnings() (
	issues []CustomAnchorVerificationIssue) {

	return r.issuesBySeverity(CustomAnchorVerificationSeverityWarning)
}

func (r CustomAnchorVerificationResult) issuesBySeverity(
	severity CustomAnchorVerificationSeverity) []CustomAnchorVerificationIssue {

	var out []CustomAnchorVerificationIssue
	for idx := range r.Issues {
		if r.Issues[idx].Severity == severity {
			out = append(out, r.Issues[idx])
		}
	}

	return out
}
