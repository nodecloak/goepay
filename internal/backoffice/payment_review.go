package backoffice

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
)

func init() {
	d := definitions["merchants"]
	for i := range d.fields {
		if d.fields[i].Name == "enabled" {
			d.fields[i].Label = "账户状态"
		}
	}
	f := selectField("pay", "收款状态", "0", "1", "2")
	f.Required = false
	f.Default = "1"
	f.Options = []Option{{Label: "禁止收款", Value: "0"}, {Label: "正常", Value: "1"}, {Label: "待审核", Value: "2"}}
	d.fields = append(d.fields, f)
	definitions["merchants"] = d
	d = definitions["settings"]
	for i := range d.fields {
		if d.fields[i].Name == "key" {
			d.fields[i].Options = append(d.fields[i].Options, Option{Label: "新注册商户审核（0关闭/1开启） user_review", Value: "user_review"})
		}
	}
	definitions["settings"] = d
}

// Called only within original CreateMerchantAtomic/ReplaceMerchantAtomic after
// their original configuration CAS. The original audit is committed together.
func updateMerchantPay(ctx context.Context, tx pgx.Tx, p Principal, id int64, in map[string]json.RawMessage, fields map[string]any) error {
	if _, explicit := in["pay"]; !explicit {
		return nil
	}
	if p.Role != "admin" || p.SubjectID <= 0 || p.AdminClass != "" && p.AdminClass != "super" && p.AdminClass != "operator" {
		return ErrForbidden
	}
	pay, ok := fields["pay"].(string)
	if !ok || pay != "0" && pay != "1" && pay != "2" {
		return ErrInvalid
	}
	_, err := tx.Exec(ctx, "UPDATE merchants SET pay=$2::smallint WHERE id=$1", id, pay)
	return err
}
