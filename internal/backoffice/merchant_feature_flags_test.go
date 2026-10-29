package backoffice

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func seedSetting(t *testing.T, s *Service, key, value string) {
	t.Helper()
	if _, e := s.pool.Exec(context.Background(), `INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key',$1::text,'value',$2::text))`, key, value); e != nil {
		t.Fatal(e)
	}
}

// internal_transfer / recharge / onecode default ON: a missing settings row
// reads as enabled, preserving today's contract; only an explicit "0" disables.
func TestMerchantFeatureVisibleDefaultsOnAndRespectsZero(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	for _, key := range MerchantFeatureVisibleKeys {
		on, e := s.MerchantFeatureVisible(ctx, key)
		if e != nil || !on {
			t.Fatalf("%s default: on=%v err=%v (want on)", key, on, e)
		}
	}
	seedSetting(t, s, "recharge", "0")
	if on, e := s.MerchantFeatureVisible(ctx, "recharge"); e != nil || on {
		t.Fatalf("recharge=0: on=%v err=%v (want off)", on, e)
	}
	seedSetting(t, s, "internal_transfer", "1")
	if on, e := s.MerchantFeatureVisible(ctx, "internal_transfer"); e != nil || !on {
		t.Fatalf("internal_transfer=1: on=%v err=%v (want on)", on, e)
	}
	// A default-OFF menu feature: absent reads off, "1" reads on.
	if on, e := s.MerchantFeatureEnabled(ctx, "complain_open"); e != nil || on {
		t.Fatalf("complain_open default: on=%v err=%v (want off)", on, e)
	}
	seedSetting(t, s, "complain_open", "1")
	if on, e := s.MerchantFeatureEnabled(ctx, "complain_open"); e != nil || !on {
		t.Fatalf("complain_open=1: on=%v err=%v (want on)", on, e)
	}
}

// SettingValues reads several keys at once; missing keys stay absent so the
// settle_*/transfer_* decision helpers treat them as enabled.
func TestSettingValuesReadsPresentKeysOnly(t *testing.T) {
	s := setup(t)
	seedSetting(t, s, "settle_wxpay", "0")
	seedSetting(t, s, "transfer_qqpay", "1")
	v, e := s.SettingValues(context.Background(), "settle_alipay", "settle_wxpay", "transfer_qqpay")
	if e != nil {
		t.Fatal(e)
	}
	if _, present := v["settle_alipay"]; present {
		t.Error("absent settle_alipay should not appear")
	}
	if v["settle_wxpay"] != "0" || v["transfer_qqpay"] != "1" {
		t.Fatalf("values %+v", v)
	}
	if SettlementAccountTypeAllowed(v, "wxpay") {
		t.Error("settle_wxpay=0 should disallow wxpay settlement")
	}
	if !SettlementAccountTypeAllowed(v, "alipay") {
		t.Error("absent settle_alipay should allow alipay settlement")
	}
	if !TransferTypeOffered(v, "qqpay") {
		t.Error("transfer_qqpay=1 should offer qqpay")
	}
}

// The merchant 站内转账 endpoint is refused with the original feature message
// when internal_transfer is turned off, and served when on/absent (default ON).
func TestInternalTransferEndpointRefusedWhenFeatureOff(t *testing.T) {
	s := setup(t)
	merchant := Principal{Role: "merchant", SubjectID: 101, Username: "m1"}
	call := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/workflows/internal-transfers", nil)
		s.ServeWorkflows(w, r, merchant, []string{"internal-transfers"})
		return w
	}
	// Default ON: the list is served (not a 403 feature refusal).
	if w := call(); w.Code == 403 && strings.Contains(w.Body.String(), "未开启站内转账功能") {
		t.Fatalf("default should not refuse: %d %s", w.Code, w.Body)
	}
	seedSetting(t, s, "internal_transfer", "0")
	w := call()
	if w.Code != 403 || !strings.Contains(w.Body.String(), "未开启站内转账功能") {
		t.Fatalf("off should refuse with feature message: %d %s", w.Code, w.Body)
	}
}

// The create gate (CheckPayment) applies the pay-guard settings. At defaults
// (no rows) a payment passes; each setting then takes effect with the original
// message, and a blacklist hit shows the configured blackmsg.
func TestCheckPaymentAppliesPayGuardSettings(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	base := PaymentContext{MerchantID: 101, ClientIP: "192.0.2.1", Name: "普通商品", NotifyURL: "https://shop.example.com/hook"}
	// Default: no pay-guard rows → passes.
	if e := s.CheckPayment(ctx, base); e != nil {
		t.Fatalf("default create should pass: %v", e)
	}
	// forceqq on + empty merchant QQ → refused with the original message.
	seedSetting(t, s, "forceqq", "1")
	e := s.CheckPayment(ctx, base)
	if !errors.Is(e, ErrForbidden) {
		t.Fatalf("forceqq should refuse: %v", e)
	}
	var refused PaymentRefusedError
	if !errors.As(e, &refused) || refused.Message != "当前商户未填写联系QQ，无法收款" {
		t.Fatalf("forceqq message: %v", e)
	}
	// Supplying the merchant QQ clears forceqq.
	if e := s.CheckPayment(ctx, PaymentContext{MerchantID: 101, ClientIP: "192.0.2.1", Name: "普通商品", MerchantQQ: "12345", NotifyURL: "https://shop.example.com/hook"}); e != nil {
		t.Fatalf("forceqq with QQ should pass: %v", e)
	}
	// blockname keyword in the order name → refused with blockalert (default).
	seedSetting(t, s, "blockname", "赌博|色情")
	hit := PaymentContext{MerchantID: 101, ClientIP: "192.0.2.1", Name: "在线赌博", MerchantQQ: "12345", NotifyURL: "https://shop.example.com/hook"}
	e = s.CheckPayment(ctx, hit)
	if !errors.As(e, &refused) || refused.Message != "该商品禁止出售" {
		t.Fatalf("blockname default alert: %v", e)
	}
	seedSetting(t, s, "blockalert", "本店禁止该商品")
	if e = s.CheckPayment(ctx, hit); !errors.As(e, &refused) || refused.Message != "本店禁止该商品" {
		t.Fatalf("blockname configured alert: %v", e)
	}
	// A non-matching name still passes.
	if e := s.CheckPayment(ctx, PaymentContext{MerchantID: 101, ClientIP: "192.0.2.1", Name: "正常商品", MerchantQQ: "12345", NotifyURL: "https://shop.example.com/hook"}); e != nil {
		t.Fatalf("non-matching name should pass: %v", e)
	}
}

// A blacklisted payer sees the configured blackmsg; the refusal still unwraps
// to ErrForbidden so existing callers are unaffected.
func TestCheckPaymentBlacklistShowsBlackmsg(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	if _, e := s.Create(ctx, admin, "blacklist", input(`{"kind":"ip","value":"192.0.2.0/24","reason":"test","enabled":true}`)); e != nil {
		t.Fatal(e)
	}
	pc := PaymentContext{MerchantID: 101, ClientIP: "192.0.2.1"}
	e := s.CheckPayment(ctx, pc)
	if !errors.Is(e, ErrForbidden) {
		t.Fatalf("blacklist should refuse: %v", e)
	}
	var refused PaymentRefusedError
	if !errors.As(e, &refused) || refused.Message != "系统异常无法完成付款" {
		t.Fatalf("default blackmsg: %v", e)
	}
	seedSetting(t, s, "blackmsg", "请联系客服处理")
	if e = s.CheckPayment(ctx, pc); !errors.As(e, &refused) || refused.Message != "请联系客服处理" {
		t.Fatalf("configured blackmsg: %v", e)
	}
}

func TestMerchantContactQQReadsProfile(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	if qq, e := s.MerchantContactQQ(ctx, 101); e != nil || qq != "" {
		t.Fatalf("default qq: %q %v", qq, e)
	}
	if _, e := s.pool.Exec(ctx, `UPDATE merchants SET profile=profile||'{"qq":"10001"}'::jsonb WHERE id=101`); e != nil {
		t.Fatal(e)
	}
	if qq, e := s.MerchantContactQQ(ctx, 101); e != nil || qq != "10001" {
		t.Fatalf("set qq: %q %v", qq, e)
	}
}

// 结算设置 save refuses an account type whose settle_* rail is off, and accepts
// one whose rail is on or unset (default).
func TestSettlementRecipientSaveRespectsSettleSwitches(t *testing.T) {
	s := setup(t)
	merchant := Principal{Role: "merchant", SubjectID: 101, Username: "m1"}
	save := func(typ string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/settlement-recipient", strings.NewReader(`{"type":"`+typ+`","account":"account-12345678","name":"收款人","expected_config_id":"0"}`))
		s.ServeWorkflows(w, r, merchant, []string{"settlement-recipient"})
		return w
	}
	// Default (no settle_* rows): alipay accepted.
	if w := save("alipay"); w.Code != 200 {
		t.Fatalf("default alipay should save: %d %s", w.Code, w.Body)
	}
	// Turn alipay settlement off → save refused with the original-style message.
	seedSetting(t, s, "settle_alipay", "0")
	if w := save("alipay"); w.Code != 403 || !strings.Contains(w.Body.String(), "该收款方式的结算已关闭") {
		t.Fatalf("settle_alipay=0 should refuse alipay: %d %s", w.Code, w.Body)
	}
	// wxpay rail still on (unset) → accepted (needs app_id for wxpay). No
	// expected_config_id so the save is unconditional (a prior save advanced it).
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/settlement-recipient", strings.NewReader(`{"type":"wxpay","app_id":"wx-app","account":"openid-1","name":"收款人"}`))
	s.ServeWorkflows(w, r, merchant, []string{"settlement-recipient"})
	if w.Code != 200 {
		t.Fatalf("settle_wxpay unset should save wxpay: %d %s", w.Code, w.Body)
	}
}
