package contract_test

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRefundColumnsUXCompletionUsesImmutableTerminalEvidence(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "refund-column-original", Name: "原退款列", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.MarkPaid(ctx, o.TradeNo, "refund-column-paid"); e != nil {
		t.Fatal(e)
	}
	done, e := s.Store.ReserveRefund(ctx, 1, o.OutTradeNo, "refund-column-done", 123)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.ResolveRefund(ctx, done.ID, "succeeded"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Store.ReserveRefund(ctx, 1, o.OutTradeNo, "refund-column-pending", 45); e != nil {
		t.Fatal(e)
	}
	var expected time.Time
	if e = pool.QueryRow(ctx, "SELECT created_at FROM provider_refund_result_evidence WHERE refund_id=$1", done.ID).Scan(&expected); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, "UPDATE refunds SET updated_at=updated_at+interval '2 days' WHERE id=$1", done.ID); e != nil {
		t.Fatal(e)
	}
	token, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant"})
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("GET", "/console/v1/merchant/refunds", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	var out struct {
		Data struct {
			Items []struct {
				OutRefundNo    string     `json:"out_refund_no"`
				CompletedAt    *time.Time `json:"completed_at"`
				MerchantAmount string     `json:"merchant_amount"`
			}
		}
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out.Data.Items) != 2 {
		t.Fatalf("refund columns %d %s", w.Code, w.Body.String())
	}
	for _, item := range out.Data.Items {
		if item.OutRefundNo == "refund-column-done" {
			if item.CompletedAt == nil || !item.CompletedAt.Equal(expected) || item.MerchantAmount != "123" {
				t.Fatalf("original terminal time or debit missing/moved: %+v expected %s", item, expected)
			}
		} else if item.CompletedAt != nil || item.MerchantAmount != "45" {
			t.Fatalf("pending completion invented: %+v", item)
		}
	}
	r = httptest.NewRequest("GET", "/console/v1/merchant/refunds?format=csv", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	rows, e := csv.NewReader(strings.NewReader(strings.TrimPrefix(w.Body.String(), "\ufeff"))).ReadAll()
	wantColumns := []string{"id", "merchant_id", "order_id", "trade_no", "refund_no", "amount", "merchant_amount", "fee_amount", "status", "created_at", "platform_refund_no", "out_refund_no", "completed_at"}
	if w.Code != 200 || e != nil || len(rows) != 3 || !reflect.DeepEqual(rows[0], wantColumns) {
		t.Fatalf("refund CSV changed existing column order or omitted completion: %d %v %v", w.Code, e, rows)
	}
	for _, row := range rows[1:] {
		if row[11] == "refund-column-done" {
			at, err := time.Parse(time.RFC3339Nano, row[12])
			if err != nil || !at.Equal(expected) || row[6] != "123" {
				t.Fatalf("CSV changed original completion/debit: %v", row)
			}
		} else if row[12] != "" || row[6] != "45" {
			t.Fatalf("CSV invented pending completion: %v", row)
		}
	}
}
