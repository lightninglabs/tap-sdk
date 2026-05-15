package grpc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFeeRateSatPerKWeight(t *testing.T) {
	tests := []struct {
		name                string
		satPerVByte         uint32
		satPerKWeight       uint32
		legacySatPerKWeight uint32
		wantSatPerKWeight   uint32
		wantErr             string
	}{
		{
			name:              "zero",
			wantSatPerKWeight: 0,
		},
		{
			name:              "sat per vbyte",
			satPerVByte:       2,
			wantSatPerKWeight: 500,
		},
		{
			name:              "sat per kweight",
			satPerKWeight:     321,
			wantSatPerKWeight: 321,
		},
		{
			name:                "legacy sat per kweight",
			legacySatPerKWeight: 123,
			wantSatPerKWeight:   123,
		},
		{
			name:          "ambiguous",
			satPerVByte:   2,
			satPerKWeight: 500,
			wantErr:       "at most one",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := feeRateSatPerKWeight(
				tc.satPerVByte, tc.satPerKWeight,
				tc.legacySatPerKWeight,
			)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tc.wantSatPerKWeight, got)
		})
	}
}
