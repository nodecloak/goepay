package server

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"goepay/internal/compat"
)

func TestDefaultPayment236SimulationReplayAndCashierKeepOriginalOperation(t *testing.T) {
	f := channel168Local(t)
	delete(f.s.MerchantChannels, 1)
	f.s.DefaultPaymentChannels = []int64{7}
	const no = "default236-cashier-replay"
	if out := f.pay(t, no, "1.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	if _, e := f.p.Exec(t.Context(), `UPDATE backoffice_records SET body=body||'{"enabled":false}'::jsonb WHERE resource='groups' AND id=0`); e != nil {
		t.Fatal(e)
	}
	f.s.DefaultPaymentChannels = nil
	if out := f.pay(t, no, "1.00", 7); out["code"] != float64(1) {
		t.Fatal("original simulation request lost its source after defaults closed", out)
	}
	in := cashierIntent{MerchantID: 1, OutTradeNo: no, Name: "synthetic item168", Money: "1.00", NotifyURL: "https://merchant.example.invalid/notify", ReturnURL: "https://merchant.example.invalid/return", ClientIP: "192.0.2.1", Version: "v1"}
	r := httptest.NewRequest("POST", "/cashier", strings.NewReader(url.Values{"type": {"alipay"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	types, e := f.s.cashierIntentTypes236(r, in)
	if e != nil || len(types) != 1 || types[0].Name != "alipay" {
		t.Fatal("cashier original type was gated by current defaults", types, e)
	}
	w := httptest.NewRecorder()
	f.s.cashierChoose(w, r, in)
	if w.Code != 302 && w.Code != 303 {
		t.Fatal("cashier original operation did not resume", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Header().Get("Location"), "/checkout/") {
		t.Fatal("cashier did not resume original checkout", w.Header())
	}
	if f.s.ProviderFlow.Providers[7].(*channel168Provider).sends.Load() != 1 {
		t.Fatal("same cashier/API operation sent payment twice")
	}
	in.OutTradeNo = "default236-new-after-close"
	r = httptest.NewRequest("POST", "/cashier", strings.NewReader("type=alipay"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	f.s.cashierChoose(w, r, in)
	if w.Code != 400 {
		t.Fatal("closed group admitted a new cashier operation", w.Code)
	}
}

func TestDefaultPayment236NewMerchantReceivesAndShowsChannels(t *testing.T) {
	f := channel168Local(t)
	delete(f.s.MerchantChannels, 1)
	f.s.DefaultPaymentChannels = []int64{7}
	if out := f.pay(t, "default236-new-merchant", "1.00", 7); out["code"] != float64(1) {
		t.Fatal("new merchant could not use the configured collection default", out)
	}
	w := httptest.NewRecorder()
	f.s.dashboard(w, httptest.NewRequest("GET", "/console/v1/merchant/dashboard", nil), 1)
	var result struct {
		Code int
		Data struct {
			Channels []struct {
				ID string `json:"channel_id"`
			} `json:"payment_channels"`
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != 200 || result.Code != 0 || len(result.Data.Channels) != 1 || result.Data.Channels[0].ID != "7" {
		t.Fatal("merchant dashboard did not project the actual default channel", w.Code, w.Body.String(), err)
	}
	types := f.s.cashierAllowedTypes(httptest.NewRequest("GET", "/cashier", nil), 1)
	if len(types) != 1 || types[0].Name != "alipay" {
		t.Fatal("cashier omitted default payment type", types)
	}
	if _, exists := f.s.MerchantChannels[1]; exists || len(f.s.ProviderFlow.MerchantChannels[1]) != 0 {
		t.Fatal("default collection mutated generic operator authority")
	}
	report, err := f.s.InspectPaymentAdmission236(t.Context(), 1)
	if err != nil || !report.Exists || report.Source != "default" || len(report.PaymentChannels) != 1 || report.PaymentChannels[0].ID != "7" || len(report.OperatorChannels) != 0 {
		t.Fatal("read-only admission report disagrees with actual payment", report, err)
	}
}

func TestDefaultPayment236ExplicitAdmissionsRemainAuthoritative(t *testing.T) {
	f := channel168Local(t)
	f.s.DefaultPaymentChannels = []int64{7}
	f.s.MerchantChannels[1] = []int64{}
	if out := f.pay(t, "default236-explicit-disabled", "1.00", 7); out["code"] == float64(1) {
		t.Fatal("explicit empty admission inherited defaults")
	}
	f.s.MerchantChannels[1] = []int64{8}
	if out := f.pay(t, "default236-explicit-private-deny", "1.00", 7); out["code"] == float64(1) {
		t.Fatal("explicit admission gained an unlisted default")
	}
	if out := f.pay(t, "default236-explicit-positive", "1.00", 8); out["code"] != float64(1) {
		t.Fatal("explicit positive admission stopped working", out)
	}
}

func TestDefaultPayment236CurrentMerchantAndGroupRestrictions(t *testing.T) {
	f := channel168Local(t)
	for _, tc := range []struct {
		name, query string
		defaults    []int64
	}{
		{"no-defaults", "", nil},
		{"merchant-disabled", "UPDATE merchants SET enabled=false WHERE id=1", []int64{7}},
		{"merchant-payment-closed", "UPDATE merchants SET pay=0 WHERE id=1", []int64{7}},
		{"merchant-review", "UPDATE merchants SET pay=2 WHERE id=1", []int64{7}},
		{"group-disabled", `UPDATE backoffice_records SET body=body||'{"enabled":false}'::jsonb WHERE resource='groups' AND id=0`, []int64{7}},
		{"type-closed", `UPDATE backoffice_records SET body=body||'{"info":{"alipay":{"channel":"0","type":""}}}'::jsonb WHERE resource='groups' AND id=0`, []int64{7}},
		{"dedicated-unavailable", `UPDATE backoffice_records SET body=body||'{"info":{"alipay":{"channel":"8","type":"channel"}}}'::jsonb WHERE resource='groups' AND id=0`, []int64{7}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := f.p.Exec(t.Context(), `UPDATE merchants SET enabled=true,pay=1 WHERE id=1; UPDATE backoffice_records SET body=body||'{"enabled":true,"info":{}}'::jsonb WHERE resource='groups' AND id=0`); err != nil {
				t.Fatal(err)
			}
			if tc.query != "" {
				if _, err := f.p.Exec(t.Context(), tc.query); err != nil {
					t.Fatal(err)
				}
			}
			delete(f.s.MerchantChannels, 1)
			f.s.DefaultPaymentChannels = tc.defaults
			if out := f.pay(t, "default236-"+tc.name, "1.00", 7); out["code"] == float64(1) {
				t.Fatal("restricted merchant received through a default channel", out)
			}
			if _, exists := f.s.MerchantChannels[1]; exists {
				t.Fatal("request leaked a root admission")
			}
		})
	}
}

func TestDefaultPayment236InheritedSourcesDoNotGrantOutgoingFunds(t *testing.T) {
	f := channel168Local(t)
	delete(f.s.MerchantChannels, 1)
	f.s.DefaultPaymentChannels = []int64{7}
	view, err := f.s.withParentRuntime(t.Context(), 1, "", "alipay")
	if err != nil {
		t.Fatal(err)
	}
	if id, _, err := view.providerRoute(1, compat.Params{"type": "alipay", "channel_id": "7", "method": "native"}); err != nil || id != 7 {
		t.Fatal("collection positive control unavailable", id, err)
	}
	tx, err := f.p.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	outgoing, err := f.s.transferRuntimeSnapshot228(t.Context(), tx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(outgoing.MerchantChannels[1]) != 0 || len(f.s.MerchantChannels[1]) != 0 {
		t.Fatal("collection default expanded outgoing authority")
	}
}

func TestDefaultPayment236MembershipExpiryKeepsBaseGroupRestrictions(t *testing.T) {
	f := channel168Local(t)
	delete(f.s.MerchantChannels, 1)
	f.s.DefaultPaymentChannels = []int64{7}
	var base, vip int64
	if e := f.p.QueryRow(t.Context(), `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"enabled":true,"info":{"alipay":{"channel":"8","type":"channel"}}}') RETURNING id`).Scan(&base); e != nil {
		t.Fatal(e)
	}
	if e := f.p.QueryRow(t.Context(), `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"enabled":true,"info":{"alipay":{"channel":"7","type":"channel"}}}') RETURNING id`).Scan(&vip); e != nil {
		t.Fatal(e)
	}
	if _, e := f.p.Exec(t.Context(), `UPDATE merchants SET base_group_id=$1 WHERE id=1`, base); e != nil {
		t.Fatal(e)
	}
	if _, e := f.p.Exec(t.Context(), `INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES(1,$1,clock_timestamp()+interval '1 hour')`, vip); e != nil {
		t.Fatal(e)
	}
	if out := f.pay(t, "default236-member-current", "1.00", 7); out["code"] != float64(1) {
		t.Fatal("active membership positive control unavailable", out)
	}
	if _, e := f.p.Exec(t.Context(), `UPDATE memberships SET expires_at=clock_timestamp()-interval '1 second' WHERE merchant_id=1`); e != nil {
		t.Fatal(e)
	}
	if out := f.pay(t, "default236-member-expired", "1.00", 7); out["code"] == float64(1) {
		t.Fatal("expired membership retained default authority")
	}
	if _, e := f.p.Exec(t.Context(), `UPDATE merchants SET group_baseline_unresolved=true WHERE id=1; UPDATE memberships SET expires_at=clock_timestamp()+interval '1 hour' WHERE merchant_id=1`); e != nil {
		t.Fatal(e)
	}
	if out := f.pay(t, "default236-member-unresolved-active", "1.00", 7); out["code"] != float64(1) {
		t.Fatal("active independent membership lost authority", out)
	}
	if _, e := f.p.Exec(t.Context(), `UPDATE memberships SET expires_at=clock_timestamp()-interval '1 second' WHERE merchant_id=1`); e != nil {
		t.Fatal(e)
	}
	if out := f.pay(t, "default236-member-unresolved-expired", "1.00", 7); out["code"] == float64(1) {
		t.Fatal("unresolved baseline invented a default group")
	}
}
