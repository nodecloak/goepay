package contract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"goepay/internal/identity"
	"goepay/internal/payment"
)

func TestReferral170CurrentPolicyReadOnlyMissingPolicyAndRoleBoundary(t *testing.T) {
	s, p, _ := fixture(t)
	ctx := context.Background()
	token, csrf, e := s.Sessions.Create(identity.User{ID: 1, Role: "merchant", Username: "merchant"})
	if e != nil {
		t.Fatal(e)
	}
	invoke := func(method, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/console/v1/merchant/referral/"+path, strings.NewReader(`{}`))
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	w := invoke("GET", "current-policy")
	if w.Code != 200 {
		t.Fatal("missing safe current effective policy projection", w.Code, w.Body.String())
	}
	var out struct{ Data map[string]any }
	if e = json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	if out.Data["merchant_id"] != "1" || out.Data["status"] != "budget_disabled" || out.Data["payment_fee_enabled"] != false || out.Data["enabled"] != false || out.Data["rate_bps"] != float64(0) {
		t.Fatal("default disabled policy or owner fabricated", out.Data)
	}
	group := out.Data["current_group"].(map[string]any)
	if group["id"] != "0" || group["source"] != "base" {
		t.Fatal(group)
	}
	for _, path := range []string{"current-policy?merchant_id=2", "current-policy?scope=group", "current-policy?merchant_id=1&merchant_id=2"} {
		if w = invoke("GET", path); w.Code != 400 {
			t.Fatal("selector accepted", path, w.Code)
		}
	}
	if w = invoke("POST", "current-policy"); w.Code != 405 {
		t.Fatal("policy read became writable", w.Code)
	}
	admin, _, e := s.Sessions.Create(identity.User{ID: 1, Role: "admin", Username: "admin", AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("GET", "/console/v1/admin/referral/current-policy", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: admin})
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("admin obtained merchant projection", w.Code)
	}
	r = httptest.NewRequest("GET", "/console/v1/admin/referral-policies/global/0", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: admin})
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("original administrator policy read changed", w.Code, w.Body.String())
	}
	if _, e = p.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash) SELECT 'referral170-owner2','merchant',2,'Synthetic170',password_hash FROM console_users WHERE username='merchant' AND role='merchant'`); e != nil {
		t.Fatal(e)
	}
	other, _, e := s.Sessions.Create(identity.User{ID: 2, Role: "merchant", Username: "referral170-owner2"})
	if e != nil {
		t.Fatal(e)
	}
	r = httptest.NewRequest("GET", "/console/v1/merchant/referral/current-policy", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: other})
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("second owner own read", w.Code, w.Body.String())
	}
	if e = json.Unmarshal(w.Body.Bytes(), &out); e != nil || out.Data["merchant_id"] != "2" {
		t.Fatal("second owner was projected as first owner", e, out.Data)
	}
	var n int
	if e = p.QueryRow(ctx, `SELECT (SELECT count(*) FROM referral_tokens)+(SELECT count(*) FROM referral_rewards)+(SELECT count(*) FROM referral_daily_awards)+(SELECT count(*) FROM jobs)`).Scan(&n); e != nil || n != 0 {
		t.Fatal("GET wrote token/reward/worker state", n, e)
	}
}

func TestReferral170CurrentPolicyGroupInheritanceExpiryAndUnknownDoesNotRewriteHistory(t *testing.T) {
	s, p, _ := fixture(t)
	ctx := context.Background()
	var snapshots []map[string]any
	t.Cleanup(func() {
		if target := os.Getenv("REFERRAL170_SNAPSHOT_OUT"); target != "" {
			raw, e := json.MarshalIndent(snapshots, "", "  ")
			if e != nil {
				t.Error(e)
			} else if e = os.WriteFile(target, raw, 0600); e != nil {
				t.Error(e)
			}
		}
	})
	actor := payment.TransferBatchActor{Role: "admin", ID: 1, Class: "super", Name: "synthetic170"}
	old, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "referral170-history", Amount: 10000, Type: "wxpay"})
	if e != nil {
		t.Fatal(e)
	}
	var historical string
	if e = p.QueryRow(ctx, `SELECT md5(to_jsonb(t)::text) FROM referral_order_terms t WHERE order_id=$1`, old.ID).Scan(&historical); e != nil {
		t.Fatal(e)
	}
	var base, vip, payer int64
	for i, target := range []*int64{&base, &vip, &payer} {
		if e = p.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups',jsonb_build_object('name',$1::text,'enabled',true,'rate_bps','100')) RETURNING id`, fmt.Sprintf("synthetic170-%d", i)).Scan(target); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = p.Exec(ctx, `UPDATE merchants SET base_group_id=$1 WHERE id=1`, base); e != nil {
		t.Fatal(e)
	}
	enabled, rate := true, int64(3333)
	if _, e = s.Store.SetReferralPolicy(ctx, "global", 0, 0, payment.ReferralPolicyInput{Enabled: &enabled, RateBPS: &rate, Mode: "immediate", Basis: "fee", RefundPolicy: "any_successful_refund_voids_reward"}, actor); e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"payment_fee_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	token, _, _ := s.Sessions.Create(identity.User{ID: 1, Role: "merchant", Username: "merchant"})
	read := func() map[string]any {
		t.Helper()
		r := httptest.NewRequest("GET", "/console/v1/merchant/referral/current-policy", nil)
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		for _, secret := range []string{"proof_hash", "token_cipher", "index_key", "actor_name", "config_revision", "account_hash", "appsecret"} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatal("private source exposed", secret)
			}
		}
		var v struct{ Data map[string]any }
		if e = json.Unmarshal(w.Body.Bytes(), &v); e != nil {
			t.Fatal(e)
		}
		snapshots = append(snapshots, v.Data)
		return v.Data
	}
	current := read()
	if current["status"] != "active" || current["basis"] != "fee" || current["rate_bps"] != float64(3333) || current["payer_group_dependent"] != false {
		t.Fatal(current)
	}
	sample := current["example"].(map[string]any)
	if sample["basis_amount"] != "10000" || sample["percentage_amount"] != "3333" || sample["order_estimate_status"] != "unknown" || sample["order_estimate"] != nil {
		t.Fatal("example fabricated real order income", sample)
	}
	disabled, payerRate := false, int64(1000)
	if _, e = s.Store.SetReferralPolicy(ctx, "group", payer, 0, payment.ReferralPolicyInput{Enabled: &disabled, RateBPS: &payerRate, Mode: "daily", Basis: "gross", RefundPolicy: "any_successful_refund_voids_reward"}, actor); e != nil {
		t.Fatal(e)
	}
	current = read()
	if current["status"] != "conditional" || current["payer_group_dependent"] != true {
		t.Fatal("future payer condition hidden", current)
	}
	if _, e = s.Store.SetReferralPolicy(ctx, "group", vip, 0, payment.ReferralPolicyInput{Enabled: &enabled, RateBPS: &rate, Mode: "daily", Basis: "profit", RefundPolicy: "any_successful_refund_voids_reward"}, actor); e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(ctx, `INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES(1,$1,statement_timestamp()+interval '1 day')`, vip); e != nil {
		t.Fatal(e)
	}
	current = read()
	group := current["current_group"].(map[string]any)
	if group["id"] != fmt.Sprint(vip) || group["source"] != "membership" || group["expires_at"] == nil || current["basis"] != "profit" || current["mode"] != "daily" || current["payer_group_dependent"] != false {
		t.Fatal("live VIP inheritance", current)
	}
	if _, e = p.Exec(ctx, `UPDATE memberships SET expires_at=statement_timestamp()-interval '1 second' WHERE merchant_id=1`); e != nil {
		t.Fatal(e)
	}
	current = read()
	group = current["current_group"].(map[string]any)
	if group["id"] != fmt.Sprint(base) || group["source"] != "base" || current["basis"] != "fee" {
		t.Fatal("expired VIP didn't restore base", current)
	}
	if _, e = p.Exec(ctx, `UPDATE merchants SET group_baseline_unresolved=true WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	current = read()
	if current["status"] != "unknown" || current["example"] != nil || current["rate_bps"] != nil {
		t.Fatal("unresolved baseline fabricated current policy", current)
	}
	if _, e = p.Exec(ctx, `UPDATE merchants SET group_baseline_unresolved=false WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Store.SetReferralPolicy(ctx, "group", base, 0, payment.ReferralPolicyInput{Enabled: &disabled, Mode: "immediate", Basis: "gross", RefundPolicy: "any_successful_refund_voids_reward"}, actor); e != nil {
		t.Fatal(e)
	}
	current = read()
	if current["status"] != "disabled" || current["enabled"] != false {
		t.Fatal("explicit own disabled not authoritative", current)
	}
	if _, e = p.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{value}','"false"'::jsonb) WHERE resource='settings' AND body->>'key'='payment_fee_enabled'`); e != nil {
		t.Fatal(e)
	}
	current = read()
	if current["status"] != "budget_disabled" || current["payment_fee_enabled"] != false {
		t.Fatal("disabled budget promised reward", current)
	}
	var after string
	if e = p.QueryRow(ctx, `SELECT md5(to_jsonb(t)::text) FROM referral_order_terms t WHERE order_id=$1`, old.ID).Scan(&after); e != nil || after != historical {
		t.Fatal("current read rewrote historical terms", e)
	}
}
