package payment

import (
	"math"
)

func calculateBuyerChargeRelease104(original, feeBPS int64, mode int, buyerBPS *int64, minimum buyerMinimum104) (buyerAmounts104, error) {
	var out buyerAmounts104
	if original <= 0 || feeBPS < 0 || feeBPS > 10000 || (mode != 0 && mode != 1) || minimum.Threshold < 0 || minimum.Cost < 0 {
		return out, ErrInvalid
	}
	effectiveMode := mode
	if buyerBPS != nil {
		if mode != 1 || *buyerBPS < 0 || *buyerBPS > 10000 {
			return out, ErrInvalid
		}
		switch *buyerBPS {
		case 0:
			effectiveMode = 0
		case feeBPS:
			effectiveMode = 1
		default:
			return out, ErrInvalid
		}
	}
	// Original Release compares independently rounded fee against the threshold,
	// even when retained-net rounding makes the ordinary deducted fee differ.
	addedFee := original/10000*feeBPS + (original%10000*feeBPS+5000)/10000
	replace := minimum.Threshold > 0 && minimum.Cost > 0 && addedFee < minimum.Threshold
	out.Original, out.Charge = original, original
	if effectiveMode == 0 {
		if replace {
			if minimum.Cost < original {
				out.Net = original - minimum.Cost
			}
		} else {
			var e error
			out.Net, _, e = calculateFee(original, feeBPS)
			if e != nil {
				return buyerAmounts104{}, e
			}
		}
	} else {
		if replace {
			addedFee = minimum.Cost
		}
		if addedFee > math.MaxInt64-original {
			return buyerAmounts104{}, ErrInvalid
		}
		out.Charge = original + addedFee
		out.Net = original
	}
	out.Fee = out.Charge - out.Net
	return out, nil
}
