package tapsdk

import "fmt"

// SatPerKWeightPerSatPerVByte is the exact conversion factor from sat/vB to
// sat/kWU.
const SatPerKWeightPerSatPerVByte uint32 = 250

// SatPerVByteToSatPerKWeight converts a fee rate from sat/vB to sat/kWU.
func SatPerVByteToSatPerKWeight(satPerVByte uint32) (uint32, error) {
	if satPerVByte > ^uint32(0)/SatPerKWeightPerSatPerVByte {
		return 0, fmt.Errorf("fee rate %d sat/vB overflows sat/kWU",
			satPerVByte)
	}

	return satPerVByte * SatPerKWeightPerSatPerVByte, nil
}
