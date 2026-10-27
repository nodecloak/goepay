package payment

import "encoding/json"

// One policy merge serves immutable route quotes and the read-only current fee
// column. It does not read costs, estimate profit, or rewrite historical terms.
func effectiveRuntimeFee(f feeSnapshot, configured bool, parentRetained int64, groupBody []byte, kind string, amount int64) (feeSnapshot, string, error) {
	source := "legacy"
	if f.Policy == "none" {
		return f, "none", nil
	}
	if configured {
		var fields struct{ Info json.RawMessage }
		if json.Unmarshal(groupBody, &fields) != nil {
			return f, "", ErrState
		}
		if len(fields.Info) == 0 {
			fields.Info = json.RawMessage(`{}`)
		}
		rates, e := ParseGroupRetainedRates(fields.Info)
		if e != nil {
			return f, "", e
		}
		retained := parentRetained
		source = "parent"
		if positive, ok := rates[kind]; ok {
			retained = positive
			source = "group"
		}
		f.Rate = 10000 - retained
		f.Net, f.Fee, e = calculateFee(amount, f.Rate)
		if e != nil {
			return f, "", e
		}
	}
	return f, source, nil
}
