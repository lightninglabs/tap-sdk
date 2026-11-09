package customanchor

import (
	"context"
	"testing"

	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/stretchr/testify/require"
)

func TestCustomAnchorCapabilityStatusString(t *testing.T) {
	tests := []struct {
		name   string
		status CapabilityStatus
		want   string
	}{
		{
			name:   "supported",
			status: CapabilitySupported,
			want:   "supported",
		},
		{
			name:   "unsupported",
			status: CapabilityUnsupported,
			want:   "unsupported",
		},
		{
			name:   "unknown",
			status: CapabilityUnknown,
			want:   "unknown",
		},
		{
			name:   "unrecognized",
			status: CapabilityStatus(99),
			want:   "unknown",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.status.String())
			require.Equal(
				t, tc.status == CapabilitySupported,
				tc.status.Supported(),
			)
		})
	}
}

func TestCustomAnchorCapabilitiesSupport(t *testing.T) {
	caps := DefaultTapdCapabilities()

	tests := []struct {
		name       string
		capability Capability
		want       CapabilityStatus
	}{
		{
			name:       "custom anchor commit",
			capability: CapabilityCommit,
			want:       CapabilitySupported,
		},
		{
			name:       "caller anchor psbt",
			capability: CapabilityCallerAnchorPSBT,
			want:       CapabilitySupported,
		},
		{
			name:       "skip funding",
			capability: CapabilitySkipFunding,
			want:       CapabilitySupported,
		},
		{
			name:       "no change output",
			capability: CapabilityNoChangeOutput,
			want:       CapabilityUnknown,
		},
		{
			name:       "local verification",
			capability: CapabilityLocalVerification,
			want:       CapabilityUnsupported,
		},
		{
			name:       "backend anchor signing",
			capability: CapabilityBackendAnchorSigning,
			want:       CapabilityUnknown,
		},
		{
			name:       "unknown capability",
			capability: Capability("future"),
			want:       CapabilityUnknown,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, caps.Support(tc.capability))
		})
	}
}

func TestCustomAnchorCapabilitiesRequire(t *testing.T) {
	caps := Capabilities{
		BackendMode:              BackendHosted,
		CustomAnchorCommit:       CapabilitySupported,
		CallerSuppliedAnchorPSBT: CapabilitySupported,
		SkipBroadcast:            CapabilityUnsupported,
	}

	err := caps.Require(
		CapabilityCommit,
		CapabilityCallerAnchorPSBT,
	)
	require.NoError(t, err)

	err = caps.Require(CapabilitySkipBroadcast)
	require.ErrorIs(t, err, ErrUnsupportedCapability)

	var unsupported *UnsupportedCapabilityError
	require.ErrorAs(t, err, &unsupported)
	require.Equal(t, BackendHosted, unsupported.BackendMode)
	require.Equal(
		t, CapabilitySkipBroadcast,
		unsupported.Capability,
	)
	require.Equal(
		t, CapabilityUnsupported,
		unsupported.Status,
	)
	require.Contains(t, err.Error(), "skip_broadcast")
}

func TestCustomAnchorCapabilitiesRequireUnknown(t *testing.T) {
	caps := Capabilities{
		BackendMode:        BackendRemoteTapd,
		CustomAnchorCommit: CapabilitySupported,
	}

	err := caps.Require(CapabilityProofRegistration)
	require.ErrorIs(t, err, ErrUnsupportedCapability)

	var unsupported *UnsupportedCapabilityError
	require.ErrorAs(t, err, &unsupported)
	require.Equal(
		t, CapabilityProofRegistration,
		unsupported.Capability,
	)
	require.Equal(t, CapabilityUnknown, unsupported.Status)
}

func TestQueryCapabilities(t *testing.T) {
	want := DefaultTapdCapabilities()
	client := &capableCustomAnchorBuilderTestClient{
		customAnchorBuilderTestClient: &customAnchorBuilderTestClient{},
		capabilities:                  &want,
	}

	wallet := tapsdk.NewWallet(client, tapsdk.NetworkRegtest)
	got, err := QueryCapabilities(context.Background(), wallet)
	require.NoError(t, err)
	require.Equal(t, &want, got)
}

func TestQueryCapabilitiesUnknown(t *testing.T) {
	wallet := tapsdk.NewWallet(
		&customAnchorBuilderTestClient{}, tapsdk.NetworkRegtest,
	)

	got, err := QueryCapabilities(context.Background(), wallet)
	require.NoError(t, err)
	require.Equal(t, &Capabilities{}, got)

	err = got.Require(CapabilityCommit)
	require.ErrorIs(t, err, ErrUnsupportedCapability)
}
