package payment

import (
	"math"
	"math/big"
	"strconv"
	"strings"
)

type buyerMinimum104 struct{ Threshold, Cost int64 }
type buyerAmounts104 struct{ Original, Charge, Net, Fee int64 }

// Pro rounds the nominal fee first. The optional buyer rate is capped by that
// nominal fee after minimum replacement; the final fee is always C minus net.
// See the pinned offline original-calculator oracle test for provenance/bounds.
func calculateBuyerCharge104(original, feeBPS int64, mode int, buyerBPS *int64, minimum buyerMinimum104) (buyerAmounts104, error) {
	var rate *string
	if buyerBPS != nil {
		if *buyerBPS < 0 || *buyerBPS > 10000 {
			return buyerAmounts104{}, ErrInvalid
		}
		v := strconv.FormatInt(*buyerBPS/100, 10) + "." + strconv.FormatInt(100+*buyerBPS%100, 10)[1:]
		rate = &v
	}
	return calculateBuyerChargeRate104(original, feeBPS, mode, rate, minimum)
}

func calculateBuyerChargeRate104(original, feeBPS int64, mode int, buyerRate *string, minimum buyerMinimum104) (buyerAmounts104, error) {
	var out buyerAmounts104
	if original <= 0 || feeBPS < 0 || feeBPS > 10000 || (mode != 0 && mode != 1) || minimum.Threshold < 0 || minimum.Cost < 0 {
		return out, ErrInvalid
	}
	var coefficient, denominator int64
	if buyerRate != nil {
		_, c, d, e := parseBuyerRate104(*buyerRate)
		if e != nil {
			return out, e
		}
		coefficient, denominator = c, d
	}
	round := func(rate int64) int64 { return original/10000*rate + (original%10000*rate+5000)/10000 }
	nominal := round(feeBPS)
	if minimum.Threshold > 0 && minimum.Cost > 0 && nominal < minimum.Threshold {
		nominal = minimum.Cost
	}
	buyer := int64(0)
	if mode == 1 {
		buyer = nominal
		if buyerRate != nil {
			var n big.Int
			n.Mul(big.NewInt(original), big.NewInt(coefficient))
			d := big.NewInt(denominator * 100)
			n.Add(&n, new(big.Int).Quo(new(big.Int).Set(d), big.NewInt(2)))
			n.Quo(&n, d)
			if !n.IsInt64() {
				return out, ErrInvalid
			}
			buyer = n.Int64()
			if buyer > nominal {
				buyer = nominal
			}
		}
	}
	if buyer > math.MaxInt64-original {
		return out, ErrInvalid
	}
	out.Original, out.Charge = original, original+buyer
	if nominal < out.Charge {
		out.Net = out.Charge - nominal
	}
	out.Fee = out.Charge - out.Net
	return out, nil
}

// A parent overrides the global pair only when both parent values are positive.
// A blank, zero, or only one populated parent field retains both global values.
func selectBuyerMinimum104(global buyerMinimum104, threshold, cost *int64) buyerMinimum104 {
	if threshold != nil && cost != nil && *threshold > 0 && *cost > 0 {
		return buyerMinimum104{*threshold, *cost}
	}
	return global
}

// A percentage remains decimal text through the API, source MAC and quote.
// Twelve fractional digits are evidenced by the original offline Pro oracle.
func parseBuyerRate104(text string) (string, int64, int64, error) {
	if text == "" || len(text) > 16 || strings.TrimSpace(text) != text {
		return "", 0, 0, ErrInvalid
	}
	parts := strings.Split(text, ".")
	if len(parts) > 2 || len(parts[0]) == 0 || len(parts[0]) > 3 || len(parts[0]) > 1 && parts[0][0] == '0' {
		return "", 0, 0, ErrInvalid
	}
	for _, c := range parts[0] {
		if c < '0' || c > '9' {
			return "", 0, 0, ErrInvalid
		}
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
		if len(fraction) == 0 || len(fraction) > 12 {
			return "", 0, 0, ErrInvalid
		}
		for _, c := range fraction {
			if c < '0' || c > '9' {
				return "", 0, 0, ErrInvalid
			}
		}
	}
	denominator := int64(1)
	for range fraction {
		denominator *= 10
	}
	whole, e := strconv.ParseInt(parts[0], 10, 64)
	if e != nil {
		return "", 0, 0, ErrInvalid
	}
	f := int64(0)
	if fraction != "" {
		f, e = strconv.ParseInt(fraction, 10, 64)
		if e != nil {
			return "", 0, 0, ErrInvalid
		}
	}
	coefficient := whole*denominator + f
	if coefficient > 100*denominator {
		return "", 0, 0, ErrInvalid
	}
	canonical := parts[0]
	fraction = strings.TrimRight(fraction, "0")
	if fraction != "" {
		canonical += "." + fraction
	}
	return canonical, coefficient, denominator, nil
}
func validBuyerRate104(rate *string) bool {
	if rate == nil {
		return true
	}
	canonical, _, _, e := parseBuyerRate104(*rate)
	return e == nil && canonical == *rate
}
