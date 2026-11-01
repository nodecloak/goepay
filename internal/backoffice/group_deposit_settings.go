package backoffice

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"goepay/internal/merchantpolicy"
)

func init() {
	d := definitions["groups"]
	flag := selectField("user_deposit", "商户保证金", "", "_", "0", "1")
	flag.Required = false
	flag.Options = []Option{{Label: "缺省（与系统设置一致）", Value: ""}, {Label: "缺省（保留原值）", Value: "_"}, {Label: "关闭", Value: "0"}, {Label: "开启", Value: "1"}}
	minimum := text("user_deposit_min", "保证金最低金额（元）", false, 32)
	minimum.Pattern = `^(?:_|[0-9]+(?:\.[0-9]{1,2})?)?$`
	minimum.Description = "留空或_继承系统，0不限制；仅约束新支付订单。"
	days := text("user_deposit_day", "保证金冻结天数", false, 4)
	days.Pattern = `^(?:_|0|[1-9][0-9]{0,3})?$`
	days.Description = "留空或_继承系统，0不限制；提取时要求最近指定天数无订单和投诉，最多3660天。"
	d.fields = append(d.fields, flag, minimum, days)
	definitions["groups"] = d
}
func validateGroupDepositSettings(values map[string]any) error {
	raw, err := json.Marshal(values)
	if err != nil {
		return ErrInvalid
	}
	if _, err = merchantpolicy.ResolveDepositPolicy(nil, raw); err != nil {
		return ErrInvalid
	}
	return nil
}
func (s *Service) DepositSettingsForMerchant(ctx context.Context, mid int64) (MerchantDepositSettings, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return MerchantDepositSettings{}, err
	}
	defer tx.Rollback(ctx)
	policy, err := merchantpolicy.ReadDepositPolicyForSummary(ctx, tx, mid)
	if err != nil {
		return MerchantDepositSettings{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return MerchantDepositSettings{}, err
	}
	return MerchantDepositSettings{Enabled: policy.Enabled, MinCents: policy.MinCents, Days: policy.Days}, nil
}
