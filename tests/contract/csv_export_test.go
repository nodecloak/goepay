package contract_test

import (
	"context"
	"encoding/csv"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCSVExportAllFilteredRowsAndTenant(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	for i := 0; i < 27; i++ {
		name := "export,\"item\"\nline"
		if i == 0 {
			name = " \t=HYPERLINK(\"https://invalid\")"
		}
		o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: strings.Repeat("a", i+1), Name: name, Type: "sandbox", Amount: 123})
		if e != nil {
			t.Fatal(e)
		}
		if e = s.Store.MarkPaid(ctx, o.TradeNo, "export:"+o.TradeNo); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 2, OutTradeNo: "foreign", Name: "foreign", Type: "sandbox", Amount: 999}); e != nil {
		t.Fatal(e)
	}
	token, _, e := s.Sessions.Create(identity.User{Username: "merchant", ID: 1, Role: "merchant"})
	if e != nil {
		t.Fatal(e)
	}
	get := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	w := get("/console/v1/merchant/orders?format=csv&status=paid&limit=1&page=2")
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("expected CSV: %d %.160s", w.Code, w.Body.String())
	}
	rows, e := csv.NewReader(strings.NewReader(strings.TrimPrefix(w.Body.String(), "\ufeff"))).ReadAll()
	if e != nil {
		t.Fatal(e)
	}
	if len(rows) != 28 {
		t.Fatalf("export truncated to page: %d", len(rows))
	}
	ix := map[string]int{}
	for i, k := range rows[0] {
		ix[k] = i
	}
	foundFormula := false
	for _, row := range rows[1:] {
		if row[ix["merchant_id"]] != "1" || row[ix["amount"]] != "123" {
			t.Fatal("tenant or exact amount lost")
		}
		if strings.Contains(row[ix["name"]], "HYPERLINK") {
			foundFormula = true
			if !strings.HasPrefix(row[ix["name"]], "'") {
				t.Fatal("spreadsheet formula not neutralized")
			}
		}
	}
	if !foundFormula {
		t.Fatal("missing formula test row")
	}
	if w.Header().Get("Content-Disposition") == "" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing safe download headers")
	}
	for path, want := range map[string]int{"/console/v1/merchant/orders?format=csv&merchant_id=2": 403, "/console/v1/merchant/merchants?format=csv": 403, "/console/v1/merchant/orders?format=csv&format=json": 400, "/console/v1/merchant/orders?format=xml": 400} {
		if got := get(path).Code; got != want {
			t.Fatalf("%s %d want %d", path, got, want)
		}
	}
	w = get("/console/v1/merchant/reports/daily?format=csv&limit=1&page=2")
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("report export: %d", w.Code)
	}
	rows, e = csv.NewReader(strings.NewReader(strings.TrimPrefix(w.Body.String(), "\ufeff"))).ReadAll()
	if e != nil || len(rows) != 2 {
		t.Fatalf("report CSV %d %v", len(rows), e)
	}
	if rows[1][2] != "27" || rows[1][3] != "33.21" {
		t.Fatalf("report scope/money: %v", rows[1])
	}
	var audits int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM backoffice_audit WHERE operation='export-csv' AND actor_role='merchant' AND actor_id=1").Scan(&audits); e != nil || audits != 2 {
		t.Fatalf("download audit %d %v", audits, e)
	}
	if _, e = pool.Exec(ctx, "ALTER TABLE backoffice_audit RENAME TO unavailable_export_audit"); e != nil {
		t.Fatal(e)
	}
	w = get("/console/v1/merchant/orders?format=csv")
	if w.Code < 500 || strings.HasPrefix(w.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("audit failure returned download %d", w.Code)
	}
}
