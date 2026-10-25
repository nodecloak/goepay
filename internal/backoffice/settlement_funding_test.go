package backoffice

import (
	"context"
	"encoding/json"
	"goepay/internal/payment"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSettlementRecipientWorkflowAndScopedRequestResponse(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	merchant := Principal{Role: "merchant", SubjectID: 101, Username: "merchant"}
	other := Principal{Role: "merchant", SubjectID: 102, Username: "other"}
	admin := Principal{Role: "admin", SubjectID: 1, Username: "admin", AdminClass: "operator"}
	o, e := s.store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 101, OutTradeNo: "fund-scope-http", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.store.MarkPaid(ctx, o.TradeNo, "fund-scope-http"); e != nil {
		t.Fatal(e)
	}
	invoke := func(p Principal, method, path, body string, parts []string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		w := httptest.NewRecorder()
		s.ServeWorkflows(w, r, p, parts)
		return w
	}
	w := invoke(merchant, "POST", "/settlement-recipient", `{"type":"alipay","account":"secret-account-1234","name":"Secret Person","expected_config_id":"0"}`, []string{"settlement-recipient"})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var out struct {
		Data struct {
			ConfigID string `json:"config_id"`
		}
	}
	if e = json.Unmarshal(w.Body.Bytes(), &out); e != nil || out.Data.ConfigID == "" {
		t.Fatal(w.Body.String(), e)
	}
	for _, p := range []Principal{merchant, admin} {
		path := "/settlement-recipient"
		if p.Role == "admin" {
			path += "?merchant_id=101"
		}
		w = invoke(p, "GET", path, "", []string{"settlement-recipient"})
		if w.Code != 200 || strings.Contains(w.Body.String(), "secret-account") || strings.Contains(w.Body.String(), "Secret Person") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w = invoke(other, "GET", "/settlement-recipient?merchant_id=101", "", []string{"settlement-recipient"}); w.Code != 403 {
		t.Fatal("foreign config", w.Code)
	}
	if w = invoke(admin, "GET", "/settlement-recipient", "", []string{"settlement-recipient"}); w.Code != 422 {
		t.Fatal("admin owner omitted", w.Code)
	}
	w = invoke(merchant, "POST", "/settlements", `{"amount":"2.00","external_no":"scope-http","recipient_config_id":"`+out.Data.ConfigID+`"}`, []string{"settlements"})
	if w.Code != 201 || !strings.Contains(w.Body.String(), `"settlement_funding":{"state":"reserved"`) || !strings.Contains(w.Body.String(), `"state":"recorded"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	var request struct {
		Data struct {
			ID string `json:"id"`
		}
	}
	json.Unmarshal(w.Body.Bytes(), &request)
	w = invoke(merchant, "GET", "/settlements", "", []string{"settlements"})
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"reserved_amount":"200"`) || !strings.Contains(w.Body.String(), `"external_payout":false`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = invoke(admin, "POST", "/settlements/"+request.Data.ID+"/reject", `{}`, []string{"settlements", request.Data.ID, "reject"})
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"released"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	balance, _ := s.store.Balance(ctx, 101)
	if balance.Available != 1000 || balance.Reserved != 0 {
		t.Fatal(balance)
	}
	var audits string
	if e = s.pool.QueryRow(ctx, "SELECT COALESCE(string_agg(changes::text,''),'') FROM backoffice_audit").Scan(&audits); e != nil || strings.Contains(audits, "secret-account") || strings.Contains(audits, "Secret Person") {
		t.Fatal("PII audit", audits, e)
	}
}
