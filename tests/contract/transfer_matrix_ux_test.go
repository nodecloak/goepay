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

func TestDashboardUXTransferMatrixUsesImmutableResolutionTime(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	order, err := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "transfer-report-funds", Amount: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Store.MarkPaid(ctx, order.TradeNo, "transfer-report-funds-paid"); err != nil {
		t.Fatal(err)
	}
	transfer, err := s.Store.ReserveTransfer(ctx, 1, "transfer-report-manual", 100)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Store.ResolveTransfer(ctx, transfer.ID, "succeeded"); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE transfers SET updated_at=now()+interval '2 days' WHERE id=$1`, transfer.ID); err != nil {
		t.Fatal(err)
	}
	token, _, err := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super"})
	if err != nil {
		t.Fatal(err)
	}
	get := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/console/v1/admin/"+path, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	w := get("dashboard")
	var dashboard struct {
		Data struct{ Summary map[string]string }
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &dashboard) != nil || dashboard.Data.Summary["today_transfer_money"] != "1.00" {
		t.Fatalf("mutable timestamp moved a completed payout from its original day: %d %s", w.Code, w.Body.String())
	}
	for _, method := range []string{"type", "channel"} {
		w = get("reports/transfer-merchants?view=matrix&method=" + method)
		var out struct {
			Data struct {
				Summary map[string]any
				Items   []map[string]any
				Source  string
			}
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Data.Summary["total"] != "1.00" || len(out.Data.Items) != 1 || out.Data.Items[0]["uid"] != "1" || out.Data.Source != "local_resolved_transfer_facts" {
			t.Fatalf("transfer matrix %s %d %s", method, w.Code, w.Body.String())
		}
		key := "type_unknown"
		if method == "channel" {
			key = "channel_unassigned"
		}
		if out.Data.Items[0][key] != "1.00" {
			t.Fatalf("invented legacy transfer type/channel: %s", w.Body.String())
		}
		for _, private := range []string{"recipient_cipher", "account", "api_key"} {
			if strings.Contains(w.Body.String(), `"`+private+`"`) {
				t.Fatal("private transfer material exported")
			}
		}
	}
	if w = get("reports/transfer-merchants?view=matrix&type=1"); w.Code != 400 {
		t.Fatalf("unsupported transfer metric %d", w.Code)
	}
}
