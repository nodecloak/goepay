package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGroupDepositPolicyDrivesMerchantDashboardTips(t *testing.T) {
	f := channel168Local(t)
	ctx := t.Context()
	if _, e := f.p.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"user_deposit":"1","user_deposit_min":"10.00"}'::jsonb WHERE resource='groups' AND id=0`); e != nil {
		t.Fatal(e)
	}
	tips, e := f.s.collectMerchantDashboardTips(httptest.NewRequest("GET", "/tips", nil), 1)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, tip := range tips {
		if strings.Contains(tip.Title, "最低缴纳保证金 10.00") {
			found = true
		}
	}
	if !found {
		t.Fatal("effective group deposit shortfall omitted from dashboard", tips)
	}
	if _, e = f.p.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"user_deposit":"0"}'::jsonb WHERE resource='groups' AND id=0`); e != nil {
		t.Fatal(e)
	}
	tips, e = f.s.collectMerchantDashboardTips(httptest.NewRequest("GET", "/tips", nil), 1)
	if e != nil {
		t.Fatal(e)
	}
	for _, tip := range tips {
		if strings.Contains(tip.Title, "保证金不足") {
			t.Fatal("closed group deposit still showed shortfall", tip)
		}
	}
}

func TestGroupDepositSessionKeepsMalformedPolicyIsolated(t *testing.T) {
	f, token, csrf := groupFeatureHTTPFixture(t)
	ctx := t.Context()
	if _, e := f.p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"user_deposit","value":"0"}'); UPDATE backoffice_records SET body=body||'{"onecode":"1"}'::jsonb WHERE resource='groups' AND id=0`); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		body    string
		enabled bool
	}{{`{"user_deposit":"1","user_deposit_min":"0"}`, true}, {`{"user_deposit":"0","user_deposit_min":"0"}`, false}, {`{"user_deposit":"1","user_deposit_min":true}`, false}} {
		if _, e := f.p.Exec(ctx, `UPDATE backoffice_records SET body=body||$1::jsonb WHERE resource='groups' AND id=0`, tc.body); e != nil {
			t.Fatal(e)
		}
		out := groupFeatureHTTPRequest(f, token, csrf, "GET", "session", "")
		var envelope struct {
			Code int `json:"code"`
			Data struct {
				Features map[string]bool `json:"merchant_features"`
			} `json:"data"`
		}
		if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &envelope) != nil || envelope.Code != 0 || envelope.Data.Features["user_deposit"] != tc.enabled || !envelope.Data.Features["onecode"] {
			t.Fatalf("deposit policy changed unrelated session: HTTP=%d data=%s", out.Code, out.Body)
		}
	}
}
