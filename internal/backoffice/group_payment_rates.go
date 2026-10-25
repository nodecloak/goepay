package backoffice

import (
	"encoding/json"
	"goepay/internal/payment"
)

func init() {
	d := definitions["groups"]
	d.fields = append(d.fields, Field{Name: "info", Label: "支付方式分成比例", Type: "json", MaxLength: 4096})
	definitions["groups"] = d
}
func validateGroupPaymentRates(v map[string]any) error {
	if info, ok := v["info"]; ok {
		raw, e := json.Marshal(info)
		if e != nil {
			return ErrInvalid
		}
		if _, e = payment.ParseGroupRetainedRates(raw); e != nil {
			return ErrInvalid
		}
	}
	return nil
}

func validateGroupPaymentRatesRaw(in map[string]json.RawMessage) error {
	if raw, ok := in["info"]; ok {
		var encoded string
		if json.Unmarshal(raw, &encoded) == nil {
			raw = []byte(encoded)
		}
		if _, err := payment.ParseGroupRetainedRates(raw); err != nil {
			return ErrInvalid
		}
	}
	return nil
}
