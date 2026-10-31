package backoffice

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"goepay/internal/payment"
)

var createSetupSuper = Principal{Role: "admin", SubjectID: 1, Username: "admin", AdminClass: "super"}

func merchantCreateSetupStored(t *testing.T, s *Service, id int64) (profile string, policies int) {
	t.Helper()
	if e := s.pool.QueryRow(context.Background(), `SELECT profile::text,(SELECT count(*) FROM customer_charge_policies104 WHERE kind='merchant' AND owner_id=$1) FROM merchants WHERE id=$1`, id).Scan(&profile, &policies); e != nil {
		t.Fatal(e)
	}
	return profile, policies
}

func TestMerchantCreateWithoutKeyGeneratesMD5Key(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	schema, e := s.Schema(createSetupSuper, "merchants")
	if e != nil {
		t.Fatal(e)
	}
	byName := map[string]Field{}
	for _, f := range schema.Fields {
		byName[f.Name] = f
	}
	if byName["signature_algorithm"].Default != "MD5" {
		t.Fatal("signature algorithm has no MD5 default", byName["signature_algorithm"])
	}
	for name := range merchantCreateSetupFields {
		if f := byName[name]; !f.CreateOnly || !f.WriteOnly || f.Required {
			t.Fatalf("%s must be an optional create-only write-only field: %+v", name, f)
		}
	}
	if byName["fee_mode"].Default != "0" || byName["settlement_type"].Default != "alipay" {
		t.Fatal("Pro add-merchant defaults", byName["fee_mode"], byName["settlement_type"])
	}
	item, e := s.Create(ctx, createSetupSuper, "merchants", input(`{"id":"11921","name":"本地新增商户"}`))
	if e != nil {
		t.Fatal(e)
	}
	for name := range merchantCreateSetupFields {
		if _, ok := item.Fields[name]; ok {
			t.Fatalf("create-only field %s returned", name)
		}
	}
	if _, ok := item.Fields["api_key"]; ok {
		t.Fatal("generated key returned")
	}
	m, e := s.store.Merchant(ctx, 11921)
	if raw, _ := hex.DecodeString(m.APIKey); e != nil || m.SignatureAlgorithm != "MD5" || len(raw) != 16 || hex.EncodeToString(raw) != m.APIKey {
		t.Fatalf("generated credentials: algorithm=%q key length=%d %v", m.SignatureAlgorithm, len(m.APIKey), e)
	}
	if _, e = s.Create(ctx, createSetupSuper, "merchants", input(`{"id":"11929","name":"另一个商户"}`)); e != nil {
		t.Fatal(e)
	}
	if other, e := s.store.Merchant(ctx, 11929); e != nil || other.APIKey == m.APIKey {
		t.Fatal("generated keys repeat", e)
	}
	recipient, e := s.store.SettlementRecipient(ctx, 11921)
	if e != nil || recipient.State != "missing" {
		t.Fatal("blank 结算信息 recorded a recipient", recipient, e)
	}
	profile, policies := merchantCreateSetupStored(t, s, 11921)
	if policies != 0 || strings.Contains(profile, "fee_mode") || strings.Contains(profile, "settlement_") {
		t.Fatal("create setup leaked into storage", profile, policies)
	}
	if _, e = s.Create(ctx, createSetupSuper, "merchants", input(`{"id":"11928","name":"短密钥","api_key":"short-key"}`)); !errors.Is(e, ErrInvalid) {
		t.Fatal("explicit short key accepted", e)
	}
}

func TestMerchantCreateRecordsFeeModeAndFirstRecipient(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	item, e := s.Create(ctx, createSetupSuper, "merchants", input(`{"id":"11922","name":"本地订单加费","fee_mode":"1","settlement_type":"alipay","settlement_account":"local11922@example.invalid","settlement_name":"本地收款人"}`))
	if e != nil {
		t.Fatal(e)
	}
	if r, ok := item.Fields["settlement_recipient"].(payment.SettlementRecipientView); !ok || r.State != "configured" || r.Type != "alipay" || r.AccountMasked != "***alid" {
		t.Fatal("created merchant does not show its recipient", item.Fields["settlement_recipient"])
	}
	profile, policies := merchantCreateSetupStored(t, s, 11922)
	if policies != 1 || strings.Contains(profile, "local11922") || strings.Contains(profile, "本地收款人") || strings.Contains(profile, "fee_mode") {
		t.Fatal("fee policy or plaintext recipient", profile, policies)
	}
	var mode string
	if e = s.pool.QueryRow(ctx, `SELECT facts->>'MerchantMode' FROM customer_charge_policies104 WHERE kind='merchant' AND owner_id=11922`).Scan(&mode); e != nil || mode != "1" {
		t.Fatal("订单加费 not recorded", mode, e)
	}
	if _, e = s.Create(ctx, createSetupSuper, "merchants", input(`{"id":"11923","name":"本地微信结算","settlement_type":"wxpay","settlement_account":"openid-local-11923","settlement_name":"本地微信","settlement_app_id":"wx-local-11923"}`)); e != nil {
		t.Fatal(e)
	}
	if v, e := s.store.SettlementRecipient(ctx, 11923); e != nil || v.Type != "wxpay" || v.AppID != "wx-local-11923" {
		t.Fatal("wxpay recipient", v, e)
	}
	// An AppID typed before switching to 银行卡 is not part of a bank recipient.
	if _, e = s.Create(ctx, createSetupSuper, "merchants", input(`{"id":"11924","name":"本地银行卡","settlement_type":"bank","settlement_account":"6222000011112222","settlement_name":"本地银行","settlement_app_id":"wx-left-over"}`)); e != nil {
		t.Fatal(e)
	}
	if v, e := s.store.SettlementRecipient(ctx, 11924); e != nil || v.Type != "bank" || v.AppID != "" {
		t.Fatal("bank recipient", v, e)
	}
	if _, e = s.Update(ctx, createSetupSuper, "merchants", 11922, input(`{"fee_mode":"0"}`)); !errors.Is(e, ErrForbidden) {
		t.Fatal("create-only fee mode changed by edit", e)
	}
	if _, e = s.Update(ctx, createSetupSuper, "merchants", 11922, input(`{"settlement_account":"other@example.invalid"}`)); !errors.Is(e, ErrForbidden) {
		t.Fatal("create-only recipient changed by edit", e)
	}
	if _, e = s.Update(ctx, createSetupSuper, "merchants", 11922, input(`{"name":"本地订单加费改名"}`)); e != nil {
		t.Fatal("ordinary edit after create setup", e)
	}
}

func TestMerchantCreateSetupRefusalsCreateNothing(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	if _, e := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key','settle_bank','value','0'))`); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name string
		p    Principal
		body string
		want func(error) bool
	}{
		{"微信 without AppID", createSetupSuper, `{"id":"11931","name":"x","settlement_type":"wxpay","settlement_account":"openid","settlement_name":"本地"}`, func(e error) bool { return errors.Is(e, payment.ErrInvalid) }},
		{"account without name", createSetupSuper, `{"id":"11931","name":"x","settlement_account":"a@example.invalid"}`, func(e error) bool { return errors.Is(e, payment.ErrInvalid) }},
		{"银行卡 switched off by settle_bank", createSetupSuper, `{"id":"11931","name":"x","settlement_type":"bank","settlement_account":"6222000011112222","settlement_name":"本地"}`, func(e error) bool {
			var refused PaymentRefusedError
			return errors.As(e, &refused) && refused.Message == "该收款方式的结算已关闭"
		}},
		{"unknown fee mode", createSetupSuper, `{"id":"11931","name":"x","fee_mode":"2"}`, func(e error) bool { return errors.Is(e, ErrInvalid) }},
		{"unknown settlement type", createSetupSuper, `{"id":"11931","name":"x","settlement_type":"qqpay","settlement_account":"10001","settlement_name":"本地"}`, func(e error) bool { return errors.Is(e, ErrInvalid) }},
		{"administrator without class", admin, `{"id":"11931","name":"x","settlement_account":"a@example.invalid","settlement_name":"本地"}`, func(e error) bool { return errors.Is(e, ErrForbidden) }},
	} {
		if _, e := s.Create(ctx, tc.p, "merchants", input(tc.body)); !tc.want(e) {
			t.Errorf("%s: %v", tc.name, e)
		}
		if _, e := s.store.Merchant(ctx, 11931); !errors.Is(e, payment.ErrNotFound) {
			t.Fatalf("%s left a merchant behind: %v", tc.name, e)
		}
	}
}
