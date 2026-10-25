package contract_test

import (
	"context"
	"encoding/json"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSettlementFundingHTTPRequiresSessionCSRFAndImmutableRecipientVersion(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "funding-contract", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.MarkPaid(ctx, o.TradeNo, "funding-contract"); e != nil {
		t.Fatal(e)
	}
	token, csrf, e := s.Sessions.Create(identity.User{Role: "merchant", ID: 1, Username: "merchant"})
	if e != nil {
		t.Fatal(e)
	}
	invoke := func(method, path, body, csrfValue string) (int, map[string]any) {
		t.Helper()
		r := httptest.NewRequest(method, "/console/v1/merchant/workflows/"+path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		r.Header.Set("X-CSRF-Token", csrfValue)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		var out map[string]any
		if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
			t.Fatal(w.Body.String())
		}
		return w.Code, out
	}
	config := `{"type":"alipay","account":"contract-private-account","name":"Contract Private Name","expected_config_id":"0"}`
	if code, _ := invoke("POST", "settlement-recipient", config, ""); code != 403 {
		t.Fatal("CSRF config", code)
	}
	code, out := invoke("POST", "settlement-recipient", config, csrf)
	if code != 200 {
		t.Fatal(code, out)
	}
	id := out["data"].(map[string]any)["config_id"].(string)
	if code, _ = invoke("GET", "settlement-recipient?merchant_id=2", "", csrf); code != 403 {
		t.Fatal("foreign config", code)
	}
	body := `{"amount":"2.00","external_no":"funding-original","recipient_config_id":"` + id + `"}`
	if code, _ = invoke("POST", "settlements", body, ""); code != 403 {
		t.Fatal("CSRF application", code)
	}
	code, out = invoke("POST", "settlements", body, csrf)
	if code != 201 {
		t.Fatal(code, out)
	}
	row := out["data"].(map[string]any)
	if row["external_payout"] != false || row["settlement_recipient"].(map[string]any)["config_id"] != id || row["settlement_funding"].(map[string]any)["reserved_amount"] != "200" {
		t.Fatal(row)
	}
	config = `{"type":"alipay","account":"replacement-account","name":"Replacement Name","expected_config_id":"` + id + `"}`
	code, out = invoke("POST", "settlement-recipient", config, csrf)
	if code != 200 {
		t.Fatal(code, out)
	}
	changed := out["data"].(map[string]any)["config_id"].(string)
	if code, _ = invoke("POST", "settlements", strings.Replace(body, id, changed, 1), csrf); code != 409 {
		t.Fatal("changed same-number recipient accepted", code)
	}
	if code, out = invoke("POST", "settlements", body, csrf); code != 201 || out["data"].(map[string]any)["id"] != row["id"] {
		t.Fatal("old replay lost identity", code, out)
	}
	code, out = invoke("GET", "settlements", "", csrf)
	raw, _ := json.Marshal(out)
	if code != 200 || strings.Contains(string(raw), "contract-private-account") || strings.Contains(string(raw), "Contract Private Name") {
		t.Fatal(code, string(raw))
	}
	var transfers, jobs int
	if e = pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM transfers),(SELECT count(*) FROM jobs WHERE kind LIKE 'transfer.%')").Scan(&transfers, &jobs); e != nil || transfers+jobs != 0 {
		t.Fatal("40 created external transfer work", transfers, jobs, e)
	}
	balance, _ := s.Store.Balance(ctx, 1)
	if balance.Available != 800 || balance.Reserved != 200 {
		t.Fatal(balance)
	}
}
