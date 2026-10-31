package backoffice

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

// 添加商户 (the Pro add-merchant form) also asks for the 手续费扣除模式 and the
// first 结算信息, and does not ask for a key. These fields are accepted on
// create only, are never stored in the merchant profile and are never returned:
// the fee policy and the encrypted recipient are written in the merchant's own
// creation transaction, and later changes use their existing editors.
var merchantCreateSetupFields = map[string]bool{
	"fee_mode":           true,
	"settlement_type":    true,
	"settlement_account": true,
	"settlement_name":    true,
	"settlement_app_id":  true,
}

func init() {
	d := definitions["merchants"]
	for i := range d.fields {
		// A merchant created without choosing an algorithm signs with MD5.
		if d.fields[i].Name == "signature_algorithm" {
			d.fields[i].Default = "MD5"
		}
	}
	fee := selectField("fee_mode", "手续费扣除模式", "0", "1")
	fee.Required = false
	fee.Default = "0"
	fee.Options = []Option{{Label: "余额扣费", Value: "0"}, {Label: "订单加费", Value: "1"}}
	settle := selectField("settlement_type", "结算方式", "alipay", "wxpay", "bank")
	settle.Required = false
	settle.Default = "alipay"
	settle.Options = []Option{{Label: "支付宝", Value: "alipay"}, {Label: "微信", Value: "wxpay"}, {Label: "银行卡", Value: "bank"}}
	account := text("settlement_account", "结算账号", false, 128)
	account.Description = "不使用平台代收可留空"
	name := text("settlement_name", "结算账号姓名", false, 128)
	name.Description = "不使用平台代收可留空"
	app := text("settlement_app_id", "收款所属 AppID", false, 128)
	app.Description = "微信结算必填：收款 OpenID 所属应用的 AppID"
	for _, f := range []Field{fee, settle, account, name, app} {
		f.CreateOnly, f.WriteOnly, f.AdminOnly = true, true, true
		d.fields = append(d.fields, f)
	}
	definitions["merchants"] = d
}

// generatedMerchantAPIKey has the format of the administrator 重置MD5密钥:
// 32 random lowercase hexadecimal characters.
func generatedMerchantAPIKey() (string, error) {
	raw := make([]byte, 16)
	if _, e := rand.Read(raw); e != nil {
		return "", e
	}
	return hex.EncodeToString(raw), nil
}

type merchantCreateSetup struct {
	feeMode   int
	recipient *payment.SettlementRecipientInput
}

// readMerchantCreateSetup checks the validated create values before the
// transaction starts. A blank account and name mean the merchant does not use
// platform settlement, so no recipient is recorded.
func (s *Service) readMerchantCreateSetup(ctx context.Context, p Principal, v map[string]any) (merchantCreateSetup, error) {
	var out merchantCreateSetup
	switch fmt.Sprint(v["fee_mode"]) {
	case "0":
	case "1":
		out.feeMode = 1
	default:
		return out, ErrInvalid
	}
	account, _ := v["settlement_account"].(string)
	name, _ := v["settlement_name"].(string)
	if account == "" && name == "" {
		return out, nil
	}
	if p.Role != "admin" || p.AdminClass != "super" && p.AdminClass != "operator" {
		return out, ErrForbidden
	}
	in := payment.SettlementRecipientInput{Account: account, Name: name}
	in.Type, _ = v["settlement_type"].(string)
	if in.Type == "wxpay" {
		in.AppID, _ = v["settlement_app_id"].(string)
	}
	values, e := s.SettingValues(ctx, "settle_alipay", "settle_wxpay", "settle_qqpay", "settle_bank")
	if e != nil {
		return out, e
	}
	if !SettlementAccountTypeAllowed(values, in.Type) {
		return out, PaymentRefusedError{Message: "该收款方式的结算已关闭"}
	}
	out.recipient = &in
	return out, nil
}

func (c merchantCreateSetup) apply(ctx context.Context, tx pgx.Tx, store *payment.Store, p Principal, id int64) error {
	if e := store.SetInitialMerchantBuyerFeeModeTx(ctx, tx, id, c.feeMode, identity.User{ID: p.SubjectID, Role: p.Role, Username: p.Username}); e != nil {
		return e
	}
	if c.recipient == nil {
		return nil
	}
	_, e := store.SetInitialSettlementRecipientTx(ctx, tx, id, *c.recipient, payment.SettlementRecipientActor{Role: p.Role, Class: p.AdminClass, ID: p.SubjectID, Name: p.Username})
	return e
}
