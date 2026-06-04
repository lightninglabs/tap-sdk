package tapsdk

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type statusError struct {
	code codes.Code
	msg  string
}

func (e statusError) Error() string {
	return fmt.Sprintf("%s: %s", e.code, e.msg)
}

func (e statusError) GRPCStatus() *status.Status {
	return status.New(e.code, e.msg)
}

func TestWrapErrNormalizesGRPCStatus(t *testing.T) {
	tests := []struct {
		name   string
		op     string
		err    error
		target error
	}{
		{
			name:   "permission denied",
			err:    status.Error(codes.PermissionDenied, "denied"),
			target: ErrPermissionDenied,
		},
		{
			name:   "unauthenticated",
			err:    status.Error(codes.Unauthenticated, "missing"),
			target: ErrUnauthenticated,
		},
		{
			name: "proof not found",
			op:   "ExportProofFile",
			err: status.Error(
				codes.NotFound, "proof file not found",
			),
			target: ErrProofNotFound,
		},
		{
			name:   "asset unknown",
			op:     "GetBalance",
			err:    status.Error(codes.NotFound, "unknown asset"),
			target: ErrAssetUnknown,
		},
		{
			name: "invalid argument",
			err: status.Error(
				codes.InvalidArgument, "bad field",
			),
			target: ErrTapdPrecondition,
		},
		{
			name: "resource exhausted",
			err: status.Error(
				codes.ResourceExhausted,
				"not enough funds to create funding "+
					"transaction",
			),
			target: ErrInsufficientAnchorFunds,
		},
		{
			name:   "unavailable",
			err:    status.Error(codes.Unavailable, "wallet down"),
			target: ErrTapdUnavailable,
		},
		{
			name: "unimplemented",
			err: status.Error(
				codes.Unimplemented, "missing rpc",
			),
			target: ErrUnsupportedByTapd,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := wrapErr(tc.op, tc.err)
			require.ErrorIs(t, err, tc.target)
			require.ErrorIs(t, err, tc.err)

			var sdkErr *Error
			require.ErrorAs(t, err, &sdkErr)
			require.Equal(t, tc.op, sdkErr.Op)
		})
	}
}

func TestWrapErrNormalizesRESTStatus(t *testing.T) {
	restErr := statusError{
		code: codes.PermissionDenied,
		msg:  "permission denied",
	}

	err := wrapErr("GetInfo", restErr)
	require.ErrorIs(t, err, ErrPermissionDenied)

	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.PermissionDenied, st.Code())
}

func TestWrapErrNormalizesTapdUnknownMessages(t *testing.T) {
	tests := []struct {
		name        string
		op          string
		msg         string
		target      error
		wantFeeRate *FeeRateTooLowError
	}{
		{
			name: "mixed send",
			op:   "SendMulti",
			msg: "all addrs must be of the same asset ID " +
				"or group key",
			target: ErrMixedAssetBatchUnsupported,
		},
		{
			name:   "insufficient balance",
			op:     "Send",
			msg:    "insufficient asset balance for send",
			target: ErrInsufficientBalance,
		},
		{
			name: "insufficient anchor funds",
			op:   "FundBatch",
			msg: "not enough funds to create funding " +
				"transaction",
			target: ErrInsufficientAnchorFunds,
		},
		{
			name:   "insufficient wallet balance",
			op:     "CommitVirtualPsbts",
			msg:    "insufficient wallet balance for anchor tx",
			target: ErrInsufficientAnchorFunds,
		},
		{
			name:   "proof missing",
			op:     "ExportProof",
			msg:    "proof not found in archive",
			target: ErrProofNotFound,
		},
		{
			name: "fee rate too low",
			op:   "CreateFungible",
			msg: "manual fee rate below floor: " +
				"(fee_rate=2 sat/kw, floor=253 sat/kw)",
			target: ErrFeeRateTooLow,
			wantFeeRate: &FeeRateTooLowError{
				RequestedSatKw: 2,
				MinimumSatKw:   253,
			},
		},
		{
			name:   "fee rate too low without structured values",
			op:     "CreateFungible",
			msg:    "fee rate below minimum relay policy",
			target: ErrFeeRateTooLow,
		},
		{
			name:   "mint batch active",
			op:     "CreateNFT",
			msg:    "mint batch already active for key abc",
			target: ErrMintBatchActive,
		},
		{
			name:   "external issuance request missing",
			op:     "CreateFungible",
			msg:    "external issuance signing request missing",
			target: ErrExternalIssuanceRequestNotFound,
		},
		{
			name:   "asset ref not issuable",
			op:     "IssueFungible",
			msg:    "asset ref is not issuable",
			target: ErrAssetNotIssuable,
		},
		{
			name:   "chain backend unavailable",
			op:     "FundBatch",
			msg:    "wallet chain backend unavailable",
			target: ErrTapdUnavailable,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := wrapErr(
				tc.op, status.Error(codes.Unknown, tc.msg),
			)
			require.ErrorIs(t, err, tc.target)

			if tc.wantFeeRate == nil {
				return
			}

			var feeErr *FeeRateTooLowError
			require.ErrorAs(t, err, &feeErr)
			require.Equal(t, tc.wantFeeRate, feeErr)
		})
	}
}

func TestErrorStatusHelpers(t *testing.T) {
	t.Parallel()

	notFound := &Error{
		Op:  "GetBalance",
		Err: status.Error(codes.NotFound, "unknown asset"),
	}
	require.True(t, notFound.IsNotFound())
	require.False(t, notFound.IsUnavailable())
	require.False(t, notFound.IsInvalidArgument())
	require.Equal(t, codes.NotFound, notFound.GRPCCode())

	unavailable := &Error{
		Err: status.Error(codes.Unavailable, "down"),
	}
	require.True(t, unavailable.IsUnavailable())
	require.Equal(t, codes.Unavailable, unavailable.GRPCCode())

	invalid := &Error{
		Err: status.Error(codes.InvalidArgument, "bad request"),
	}
	require.True(t, invalid.IsInvalidArgument())
	require.Equal(t, codes.InvalidArgument, invalid.GRPCCode())

	plain := &Error{Err: ErrAssetUnknown}
	require.False(t, plain.IsNotFound())
	require.False(t, plain.IsUnavailable())
	require.False(t, plain.IsInvalidArgument())
	require.Equal(t, codes.Unknown, plain.GRPCCode())
}
