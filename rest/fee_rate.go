package rest

import (
	"fmt"

	tapsdk "github.com/lightninglabs/tap-sdk"
)

func feeRateSatPerKWeight(satPerVByte, satPerKWeight,
	legacySatPerKWeight uint32) (uint32, error) {

	unitsSet := 0
	for _, feeRate := range []uint32{
		satPerVByte, satPerKWeight, legacySatPerKWeight,
	} {
		if feeRate != 0 {
			unitsSet++
		}
	}
	if unitsSet > 1 {
		return 0, fmt.Errorf("fee rate must use at most one unit")
	}

	switch {
	case satPerKWeight != 0:
		return satPerKWeight, nil

	case satPerVByte != 0:
		return tapsdk.SatPerVByteToSatPerKWeight(satPerVByte)

	default:
		return legacySatPerKWeight, nil
	}
}
