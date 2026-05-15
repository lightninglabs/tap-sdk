package tapsdk

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSatPerVByteToSatPerKWeight(t *testing.T) {
	tests := []struct {
		name          string
		satPerVByte   uint32
		wantSatPerKwu uint32
		wantErr       bool
	}{
		{
			name:          "zero",
			satPerVByte:   0,
			wantSatPerKwu: 0,
		},
		{
			name:          "one sat per vbyte",
			satPerVByte:   1,
			wantSatPerKwu: 250,
		},
		{
			name:          "round number",
			satPerVByte:   20,
			wantSatPerKwu: 5000,
		},
		{
			name: "overflow",
			satPerVByte: ^uint32(0)/SatPerKWeightPerSatPerVByte +
				1,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SatPerVByteToSatPerKWeight(tc.satPerVByte)
			if tc.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tc.wantSatPerKwu, got)
		})
	}
}
