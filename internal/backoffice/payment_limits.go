package backoffice

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"goepay/internal/ledger"
)

func init() {
	d := definitions["groups"]
	for _, pair := range [][2]string{{"pay_maxmoney", "最大支付金额（元）"}, {"pay_minmoney", "最小支付金额（元）"}} {
		f := text(pair[0], pair[1], false, 32)
		d.fields = append(d.fields, f)
	}
	d.description += " 最大/最小支付金额单位元，留空继承系统设置、0不限制；仅约束新订单，不改写历史同号订单。"
	definitions["groups"] = d
	d = definitions["settings"]
	for i := range d.fields {
		if d.fields[i].Name == "key" {
			d.fields[i].Options = append(d.fields[i].Options, Option{Label: "最大支付金额（元）", Value: "pay_maxmoney"}, Option{Label: "最小支付金额（元）", Value: "pay_minmoney"})
		}
	}
	definitions["settings"] = d
}

func lockPaymentLimitSettings(ctx context.Context, tx pgx.Tx, resource string) error {
	if resource != "settings" {
		return nil
	}
	// All settings writes take this lock before locking an individual record,
	// so two concurrent edits cannot each validate against an obsolete peer.
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7170140001)`)
	return err
}

func validatePaymentLimitSetting(ctx context.Context, tx pgx.Tx, resource string, id int64, v map[string]any) error {
	key := fmt.Sprint(v["key"])
	if resource != "settings" || key != "pay_minmoney" && key != "pay_maxmoney" {
		return nil
	}
	values := map[string]any{key: v["value"]}
	rows, err := tx.Query(ctx, `SELECT body->>'key',COALESCE(body->>'value','') FROM backoffice_records WHERE resource='settings' AND body->>'key' IN ('pay_minmoney','pay_maxmoney') AND body->>'key'<>$1 AND id<>$2`, key, id)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var k, raw string
		if err = rows.Scan(&k, &raw); err != nil {
			return err
		}
		if _, found := values[k]; found {
			return ErrInvalid
		}
		values[k] = raw
	}
	if err = rows.Err(); err != nil {
		return err
	}
	return validateOriginalPaymentLimits(values)
}

func validateOriginalPaymentLimits(v map[string]any) error {
	bounds := map[string]int64{}
	for _, key := range []string{"pay_minmoney", "pay_maxmoney"} {
		if raw, ok := v[key]; ok && raw != nil && fmt.Sprint(raw) != "" {
			n, e := ledger.Parse(fmt.Sprint(raw))
			if e != nil {
				return ErrInvalid
			}
			bounds[key] = n
		}
	}
	if bounds["pay_maxmoney"] > 0 && bounds["pay_minmoney"] > bounds["pay_maxmoney"] {
		return ErrInvalid
	}
	return nil
}
