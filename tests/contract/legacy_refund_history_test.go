package contract_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goepay/internal/identity"
	"goepay/internal/migration"
)

func TestLegacyRefundHistoryHTTPIsScopedReadOnlyAndNeverExecutable(t *testing.T) {
	s, p, _ := fixture(t)
	ctx := context.Background()
	const snapshot = `{"schema_version":1,"export_id":"history-http-local","source":"epay_pro","exported_at":"2026-10-24T00:00:00+08:00","merchants":[{"id":9001,"name":"Imported local","available_money":"12.34","reserved_money":"0.50"}],"orders":[{"trade_no":"history-t1","merchant_id":9001,"out_trade_no":"history-o1","name":"Historical","type":"wxpay","money":"5.00","status":"paid","created_at":"2026-10-20T12:00:00+08:00","paid_at":"2026-10-20T12:01:00+08:00"}],"refunds":[{"merchant_id":9001,"trade_no":"history-t1","refund_no":"history-r1","money":"0.50","status":"unknown"}],"transfers":[]}`
	if _, e := migration.Apply(ctx, p, make([]byte, 32), []byte(snapshot)); e != nil {
		t.Fatal(e)
	}
	reader, e := migration.NewRefundHistoryReader(p, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	s.LegacyRefundHistory = reader
	admin, csrf, e := s.Sessions.Create(identity.User{Username: "admin", Role: "admin", ID: 1, AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	merchant, _, e := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1})
	if e != nil {
		t.Fatal(e)
	}
	request := func(role, token, method, path string) (int, string) {
		r := httptest.NewRequest(method, "/console/v1/"+role+"/legacy-refund-history"+path, strings.NewReader("{}"))
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("cacheable history")
		}
		return w.Code, w.Body.String()
	}
	status, body := request("admin", admin, "GET", "?merchant_id=9001&limit=1")
	if status != 200 {
		t.Fatal(status, body)
	}
	var result struct {
		Data struct {
			Items []migration.RefundHistory `json:"items"`
			Next  string                    `json:"next_cursor"`
		}
	}
	if e = json.Unmarshal([]byte(body), &result); e != nil || len(result.Data.Items) != 1 || result.Data.Items[0].VerifiedPaymentSource || result.Data.Items[0].Amount != 50 || result.Data.Items[0].Status != "unknown" || result.Data.Next == "" {
		t.Fatal(body, e)
	}
	for _, private := range []string{"proof_hash", "source_hash", "digest", "master"} {
		if strings.Contains(body, private) {
			t.Fatal("private import proof leaked", private)
		}
	}
	if status, body = request("merchant", merchant, "GET", ""); status != 200 || strings.Contains(body, "history-r1") {
		t.Fatal("cross-merchant archive read", status, body)
	}
	if status, _ = request("merchant", merchant, "GET", "?merchant_id=9001"); status != 403 {
		t.Fatal("merchant supplied another owner", status)
	}
	for _, query := range []string{"", "?merchant_id=9001&merchant_id=1", "?merchant_id=09001", "?merchant_id=9001&limit=0", "?merchant_id=9001&after_id=-1", "?merchant_id=9001&status=unknown"} {
		if status, _ = request("admin", admin, "GET", query); status != 400 {
			t.Fatal("ambiguous history request accepted", query, status)
		}
	}
	if status, _ = request("admin", admin, "POST", "?merchant_id=9001"); status != 405 {
		t.Fatal("archive mutation accepted", status)
	}
	if status, _ = request("admin", admin, "POST", "/resolve"); status != 404 {
		t.Fatal("historical assertion exposed a resolution action", status)
	}
	var refunds, jobs int
	if e = p.QueryRow(ctx, "SELECT (SELECT count(*) FROM refunds),(SELECT count(*) FROM jobs)").Scan(&refunds, &jobs); e != nil || refunds != 0 || jobs != 0 {
		t.Fatal("history read created executable work", refunds, jobs, e)
	}
	check, e := migration.CheckRefundHistory(ctx, p, make([]byte, 32))
	if e != nil || check.UnknownLegacyRefunds != 1 {
		t.Fatal("history lost unknown obligation", check, e)
	}
	if _, e = p.Exec(ctx, `ALTER TABLE legacy_refund_history DISABLE TRIGGER USER;DELETE FROM legacy_refund_history;ALTER TABLE legacy_refund_history ENABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	if status, body = request("admin", admin, "GET", "?merchant_id=9001"); status != 409 {
		t.Fatal("missing final owner child was presented as authentic empty history", status, body)
	}
}
