package payment

import "goepay/internal/grouppolicy"

// ParseGroupRetainedRates preserves the original rate and inheritance contract
// while sharing the strict group configuration model. Fee calculation does
// not interpret or apply the routing choice.
func ParseGroupRetainedRates(raw []byte) (map[string]int64, error) {
	info, err := grouppolicy.ParseInfo(raw)
	if err != nil {
		return nil, ErrInvalid
	}
	rates := map[string]int64{}
	for kind, policy := range info {
		if rate, explicit := policy.RetainedBPS(); explicit {
			rates[kind] = rate
		}
	}
	return rates, nil
}
