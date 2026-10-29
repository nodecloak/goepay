package backoffice

import (
	"fmt"
	"goepay/internal/ledger"
	"strings"
)

func init() {
	d := definitions["merchants"]
	remain := text("remain_money", "结算留存（百分比如 10% 或固定金额如 50.00）", false, 20)
	remain.Description = "每次自动结算前从可结算余额中保留的金额。留空表示不保留。"
	d.fields = append(d.fields, flag("transfer_api_enabled", "允许代付 API", false), text("transfer_rate", "代付费率（百分比，最多两位小数）", false, 6), remain)
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
	// 结算留存 (原 pre_user.remain_money): 空值不保留；"N%" 为百分比留存
	// (0<N<100)，否则按固定金额(元)留存。两种写法都以两位小数解析。
	if raw, ok := v["remain_money"]; ok {
		s := strings.TrimSpace(fmt.Sprint(raw))
		if s != "" {
			numeric := strings.TrimSpace(strings.TrimSuffix(s, "%"))
			n, err := ledger.Parse(numeric)
			if err != nil || n < 0 {
				return ErrInvalid
			}
			// A percentage is a plain fraction; reject an out-of-range percent
			// so a typo is not silently coerced into a huge fixed reserve.
			if strings.HasSuffix(s, "%") && (n <= 0 || n >= 10000) {
				return ErrInvalid
			}
		}
	}
	return nil
}
