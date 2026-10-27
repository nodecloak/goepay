package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFundsActionDefaultGroupPolicyIsBoundWithoutDisplayNameOrOtherGroups(t *testing.T) {
	s, p, _ := fixture(t)
	ctx := context.Background()
	target := "/console/v1/admin/workflows/settlements"
	body := json.RawMessage(`{"merchant_id":"1","amount":"2.00","external_no":"default-policy154"}`)
	digest := func() []byte {
		t.Helper()
		a, _, e := s.ResolveFundsAction(ctx, p, target, body)
		if e != nil {
			t.Fatal(e)
		}
		return a.Digest()
	}
	first := digest()
	if _, e := p.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{settle_open}','"2"') WHERE resource='groups' AND id=0`); e != nil {
		t.Fatal(e)
	}
	second := digest()
	if bytes.Equal(first, second) {
		t.Fatal("default financial permission changed without invalidating old funds confirmation")
	}
	if _, e := p.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{name}','"display only default name"') WHERE resource='groups' AND id=0; INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"unrelated group154","enabled":false,"user_transfer":"1"}')`); e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(second, digest()) {
		t.Fatal("display name or unrelated group invalidated exact target policy")
	}
}

func TestOriginalDefaultGroupHTTPConfigurationStatisticsDashboardAndCSRF(t *testing.T) {
	s, p, _ := fixture(t)
	// The current fee summary describes actual eligible runtime channels.
	// Register one real local provider instead of asserting a fee without one.
	runtimeProvider := &groupRoutingProvider152{kind: "alipay"}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: runtimeProvider}}
	s.MerchantChannels = map[int64][]int64{1: {7}}
	ctx := context.Background()
	token, csrf, e := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	request := func(role, cookie, proof, method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "/console/v1/"+role+path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: cookie})
		r.Header.Set("X-CSRF-Token", proof)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	body := `{"name":"零号默认组","rate_bps":125,"info":{"alipay":{"channel":"7","type":"channel","rate":"0"}}}`
	if w := request("admin", token, "", "PATCH", "/resources/groups/0", body); w.Code != 403 {
		t.Fatal("default zero bypassed CSRF", w.Code, w.Body.String())
	}
	if w := request("admin", token, csrf, "PATCH", "/resources/groups/0", body); w.Code != 200 || !strings.Contains(w.Body.String(), `"id":"0"`) {
		t.Fatal("real default zero edit failed", w.Code, w.Body.String())
	}
	if w := request("admin", token, csrf, "DELETE", "/resources/groups/0", ""); w.Code != 403 {
		t.Fatal("reserved zero deletion accepted", w.Code, w.Body.String())
	}
	w := request("admin", token, csrf, "GET", "/reports/groups?gid=0", "")
	var stat struct {
		Data struct {
			Name string `json:"group_name"`
		}
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &stat) != nil || stat.Data.Name != "零号默认组" {
		t.Fatal("default statistics name is a virtual placeholder", w.Code, w.Body.String())
	}
	if _, e = p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"payment_fee_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	mt, mc, e := s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant"})
	if e != nil {
		t.Fatal(e)
	}
	w = request("merchant", mt, mc, "GET", "/dashboard", "")
	var dash struct {
		Data struct {
			Fee struct {
				Name string `json:"group_name"`
				Rate string `json:"rate_bps"`
			} `json:"fee_schedule"`
		}
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &dash) != nil || dash.Data.Fee.Name != "零号默认组" || dash.Data.Fee.Rate != "125" {
		t.Fatal("dashboard default policy differs from actual group", w.Code, w.Body.String())
	}
	if w = request("merchant", mt, mc, "PATCH", "/resources/groups/0", body); w.Code != 403 {
		t.Fatal("merchant altered default group", w.Code, w.Body.String())
	}
	if runtimeProvider.creates.Load() != 0 {
		t.Fatal("read-only current fee projection called the provider")
	}
	var writes, journals, orders int
	if e = p.QueryRow(ctx, `SELECT (SELECT count(*) FROM backoffice_audit WHERE resource='groups' AND resource_id=0),(SELECT count(*) FROM journals),(SELECT count(*) FROM orders)`).Scan(&writes, &journals, &orders); e != nil || writes != 1 || journals != 0 || orders != 0 {
		t.Fatal("default configuration/read wrote money or repeated audit", writes, journals, orders, e)
	}
}
