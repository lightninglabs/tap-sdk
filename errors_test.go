package tapsdk

import (
	"testing"

	"github.com/lightninglabs/tap-sdk/rest"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

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
			name:   "invalid argument",
			err:    status.Error(codes.InvalidArgument, "bad field"),
			target: ErrTapdPrecondition,
		},
		{
			name:   "unimplemented",
			err:    status.Error(codes.Unimplemented, "missing rpc"),
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
	restErr := &rest.APIError{
		StatusCode: 403,
		GRPCCode:   codes.PermissionDenied,
		Message:    "permission denied",
	}

	err := wrapErr("GetInfo", restErr)
	require.ErrorIs(t, err, ErrPermissionDenied)

	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.PermissionDenied, st.Code())
}

func TestWrapErrNormalizesTapdUnknownMessages(t *testing.T) {
	tests := []struct {
		name   string
		op     string
		msg    string
		target error
	}{
		{
			name:   "mixed send",
			op:     "SendMulti",
			msg:    "all addrs must be of the same asset ID or group key",
			target: ErrMixedAssetBatchUnsupported,
		},
		{
			name:   "insufficient balance",
			op:     "Send",
			msg:    "insufficient asset balance for send",
			target: ErrInsufficientBalance,
		},
		{
			name:   "proof missing",
			op:     "ExportProof",
			msg:    "proof not found in archive",
			target: ErrProofNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := wrapErr(
				tc.op, status.Error(codes.Unknown, tc.msg),
			)
			require.ErrorIs(t, err, tc.target)
		})
	}
}
