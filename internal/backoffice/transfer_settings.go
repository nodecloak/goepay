package backoffice

import (
	"fmt"
	"goepay/internal/ledger"
)

func init() {
	d := definitions["merchants"]
	d.fields = append(d.fields, flag("transfer_api_enabled", "允许代付 API", false), text("transfer_rate", "代付费率（百分比，最多两位小数）", false, 6))
	definitions["merchants"] = d
	d = definitions["settings"]
	for i := range d.fields {
		if d.fields[i].Name == "key" {
			for _, key := range []string{"transfer_enabled", "transfer_minmoney", "transfer_maxmoney", "transfer_maxlimit", "transfer_desc", "transfer_name", "settle_type"} {
				d.fields[i].Options = append(d.fields[i].Options, Option{Label: key, Value: key})
			}
		}
	}
	definitions["settings"] = d
}
func validateMerchantOptions(v map[string]any) error {
	if raw, ok := v["transfer_rate"]; ok && fmt.Sprint(raw) != "" {
		rate, err := ledger.Parse(fmt.Sprint(raw))
		if err != nil || rate < 0 || rate > 10000 {
			return ErrInvalid
		}
	}
	return nil
}
