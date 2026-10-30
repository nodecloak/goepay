package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	"goepay/internal/identity"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The 商户相关/转账付款 parity switches default ON (a missing settings row leaves
// the feature available) and, when an administrator sets them to "0", hide the
// merchant menu entry (via the session payload) and make the matching merchant
// endpoint refuse with 403. Administrator management paths are not gated.
func TestMerchantParitySwitchesGateMenuAndEndpoints(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	var err error
	s.MFA, err = identity.NewMFA(pool, bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	s.MerchantChannels = map[int64][]int64{1: {7}}

	merchant := func() (string, string) {
		token, csrf, e := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1})
		if e != nil {
			t.Fatal(e)
		}
		return token, csrf
	}
	call := func(token, csrf, method, path, body string) (int, map[string]any) {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if csrf != "" {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	setSwitch := func(key, value string) {
		if _, e := pool.Exec(ctx, `DELETE FROM backoffice_records WHERE resource='settings' AND body->>'key'=$1`, key); e != nil {
			t.Fatal(e)
		}
		if value != "" {
			if _, e := pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key',$1::text,'value',$2::text))`, key, value); e != nil {
				t.Fatal(e)
			}
		}
	}
	features := func() map[string]any {
		token, _ := merchant()
		code, out := call(token, "", "GET", "/console/v1/merchant/session", "")
		if code != 200 {
			t.Fatalf("merchant session %d %v", code, out)
		}
		data, _ := out["data"].(map[string]any)
		f, _ := data["merchant_features"].(map[string]any)
		return f
	}

	// Default (no row): every switch available, so its endpoint does NOT refuse
	// with the feature-closed 403 and the session reports it true.
	f := features()
	for _, key := range []string{"user_totp_open", "subchannel_open", "user_profitsharing", "user_transfer_batch"} {
		if f[key] != true {
			t.Fatalf("default feature %s not ON: %v", key, f[key])
		}
	}
	token, csrf := merchant()
	// transfer-batches list reaches the handler (not a 403 feature refusal).
	if code, out := call(token, csrf, "GET", "/console/v1/merchant/transfer-batches", ""); code == 403 && msg(out) == "未开启批量代付功能" {
		t.Fatal("transfer-batches feature-refused while user_transfer_batch default ON")
	}
	// subchannel list reaches the handler (other 403s, e.g. actor checks, are
	// unrelated to the feature gate; only the feature message must be absent).
	if code, out := call(token, csrf, "GET", "/console/v1/merchant/payment-subchannels", ""); code == 403 && msg(out) == "未开启子商户支付通道功能" {
		t.Fatal("payment-subchannels feature-refused while subchannel_open default ON")
	}
	// TOTP prepare reaches the password check (401), not the feature 403.
	if code, out := call(token, csrf, "POST", "/console/v1/merchant/security/totp/prepare", `{"current_password":"x"}`); code == 403 && msg(out) == "未开启登录二次验证功能" {
		t.Fatal("totp prepare feature-refused while user_totp_open default ON")
	}

	// Turn each off: the session drops the feature and the endpoint refuses 403.
	setSwitch("user_transfer_batch", "0")
	setSwitch("subchannel_open", "0")
	setSwitch("user_profitsharing", "0")
	setSwitch("user_totp_open", "0")
	f = features()
	for _, key := range []string{"user_totp_open", "subchannel_open", "user_profitsharing", "user_transfer_batch"} {
		if f[key] != false {
			t.Fatalf("disabled feature %s still ON: %v", key, f[key])
		}
	}
	token, csrf = merchant()
	if code, out := call(token, csrf, "GET", "/console/v1/merchant/transfer-batches", ""); code != 403 || msg(out) != "未开启批量代付功能" {
		t.Fatalf("transfer-batches not feature-refused when off: %d %v", code, out)
	}
	if code, out := call(token, csrf, "GET", "/console/v1/merchant/payment-subchannels", ""); code != 403 || msg(out) != "未开启子商户支付通道功能" {
		t.Fatalf("payment-subchannels not feature-refused when off: %d %v", code, out)
	}
	if code, out := call(token, csrf, "POST", "/console/v1/merchant/security/totp/prepare", `{"current_password":"x"}`); code != 403 || msg(out) != "未开启登录二次验证功能" {
		t.Fatalf("totp prepare not feature-refused when off: %d %v", code, out)
	}
	// Disabling TOTP enrollment must still leave DISABLE reachable (not a feature
	// 403) so an already-enrolled merchant is never locked out.
	if code, out := call(token, csrf, "POST", "/console/v1/merchant/security/totp/disable", `{"current_password":"x","code":"000000"}`); code == 403 && msg(out) == "未开启登录二次验证功能" {
		t.Fatal("totp disable wrongly feature-refused; an enrolled merchant could be locked out")
	}
}

func msg(out map[string]any) string {
	m, _ := out["message"].(string)
	return m
}
