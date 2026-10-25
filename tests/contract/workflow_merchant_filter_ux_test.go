package contract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorkflowMerchantFilterUXExactOwnerPaginationAndReadOnly(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	for _, mid := range []int64{1, 2} {
		o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: mid, OutTradeNo: fmt.Sprintf("workflow148-source-%d", mid), Amount: 10000})
		if e != nil {
			t.Fatal(e)
		}
		if e = s.Store.MarkPaid(ctx, o.TradeNo, "workflow148-paid:"+o.TradeNo); e != nil {
			t.Fatal(e)
		}
		if _, e = s.Store.SetSettlementRecipient(ctx, mid, payment.SettlementRecipientInput{Type: "alipay", Account: fmt.Sprintf("local%d@example.invalid", mid), Name: "Local Payee"}, nil, payment.SettlementRecipientActor{Role: "merchant", ID: mid, Name: "merchant"}); e != nil {
			t.Fatal(e)
		}
		for i := 0; i < 3; i++ {
			if _, e = s.Store.RequestFunds(ctx, "settlements", mid, 0, fmt.Sprintf("workflow148-%d-%d", mid, i), 100, "merchant"); e != nil {
				t.Fatal(e)
			}
		}
	}
	token, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	query := func(q string) map[string]any {
		t.Helper()
		r := httptest.NewRequest("GET", "/console/v1/admin/workflows/settlements?"+q, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		var out struct{ Data map[string]any }
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("workflow list %d %s", w.Code, w.Body.String())
		}
		return out.Data
	}
	fingerprint := func() string {
		t.Helper()
		var v string
		if e := pool.QueryRow(ctx, `SELECT md5((SELECT jsonb_agg(to_jsonb(a) ORDER BY id)::text FROM accounts a)||(SELECT jsonb_agg(to_jsonb(r) ORDER BY id)::text FROM ledger_requests r)||(SELECT jsonb_agg(to_jsonb(j) ORDER BY id)::text FROM journals j))`).Scan(&v); e != nil {
			t.Fatal(e)
		}
		return v
	}
	before := fingerprint()
	for _, mid := range []string{"1", "2"} {
		for page := 1; page <= 2; page++ {
			data := query(fmt.Sprintf("merchant_id=%s&page=%d&limit=2", mid, page))
			if data["total"] != float64(3) {
				t.Fatalf("owner count not filtered %+v", data)
			}
			items := data["items"].([]any)
			expected := 2
			if page == 2 {
				expected = 1
			}
			if len(items) != expected {
				t.Fatalf("pagination mismatch %+v", data)
			}
			for _, item := range items {
				if item.(map[string]any)["merchant_id"] != mid {
					t.Fatalf("wrong owner %+v", item)
				}
			}
		}
	}
	if data := query("merchant_id=9223372036854775807"); data["total"] != float64(0) || len(data["items"].([]any)) != 0 {
		t.Fatalf("absent merchant scope broadened %+v", data)
	}
	if fingerprint() != before {
		t.Fatal("linked record reads mutated ledger")
	}
}

func TestWorkflowMerchantFilterUXInvalidAndMerchantScopeNeverBroaden(t *testing.T) {
	s, _, _ := fixture(t)
	admin, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	merchant, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant"})
	if e != nil {
		t.Fatal(e)
	}
	call := func(role, q string) int {
		parts := strings.SplitN("/console/v1/"+role+"/workflows/settlements?"+q, "?", 2)
		r := httptest.NewRequest("GET", parts[0], nil)
		r.URL.RawQuery = parts[1]
		token := admin
		if role == "merchant" {
			token = merchant
		}
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w.Code
	}
	for _, q := range []string{"merchant_id=0", "merchant_id=01", "merchant_id=-1", "merchant_id=9223372036854775808", "merchant_id=1&merchant_id=2", "merchant_id=1&uid=2", "page=1&page=2", "page=01", "limit=0", "limit=101", "limit=+2", "merchant_id=%zz", "unknown=1"} {
		if status := call("admin", q); status != 422 {
			t.Fatalf("invalid filter %q silently accepted %d", q, status)
		}
	}
	if status := call("merchant", "merchant_id=2"); status != 403 {
		t.Fatalf("cross merchant accepted %d", status)
	}
	if status := call("merchant", "merchant_id=1&page=1&limit=20"); status != 200 {
		t.Fatalf("own merchant refused %d", status)
	}
}
