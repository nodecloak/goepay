package backoffice

import (
	"context"
	"encoding/json"
	"goepay/internal/payment"
	"net/http/httptest"
	"testing"
)

func TestT1SettingAndTenantFundsAvailability(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	for _, value := range []string{"true", "2", "-1", "01", ""} {
		if _, e := s.Create(ctx, admin, "settings", input(`{"key":"settle_type","value":"`+value+`"}`)); e == nil {
			t.Fatalf("invalid settlement mode accepted %q", value)
		}
	}
	setting, e := s.Create(ctx, admin, "settings", input(`{"key":"settle_type","value":"1"}`))
	if e != nil {
		t.Fatal(e)
	}
	o, e := s.store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 101, OutTradeNo: "today", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.store.MarkPaid(ctx, o.TradeNo, "t1-today"); e != nil {
		t.Fatal(e)
	}
	merchant := Principal{Role: "merchant", SubjectID: 101, Username: "merchant"}
	for _, tc := range []struct {
		p     Principal
		query string
		code  int
	}{
		{merchant, "", 200}, {merchant, "?merchant_id=102", 403}, {admin, "?merchant_id=101", 200}, {admin, "", 422}, {admin, "?merchant_id=invalid", 422},
	} {
		w := httptest.NewRecorder()
		s.ServeWorkflows(w, httptest.NewRequest("GET", "/workflows/funds-availability"+tc.query, nil), tc.p, []string{"funds-availability"})
		if w.Code != tc.code {
			t.Fatalf("availability %s status=%d body=%s", tc.query, w.Code, w.Body.String())
		}
		if w.Code == 200 {
			var out struct {
				Data map[string]any `json:"data"`
			}
			if e = json.Unmarshal(w.Body.Bytes(), &out); e != nil {
				t.Fatal(e)
			}
			if out.Data["available_money"] != "0.00" || out.Data["ledger_available_money"] != "1.00" || out.Data["today_blocked_money"] != "1.00" || out.Data["settle_type"] != float64(1) {
				t.Fatal(out.Data)
			}
		}
	}
	if _, e = s.Update(ctx, admin, "settings", setting.ID, input(`{"value":"0"}`)); e != nil {
		t.Fatal(e)
	}
	if v, e := s.store.FundsAvailability(ctx, 101); e != nil || v.Available != 100 || v.SettleType != 0 {
		t.Fatalf("disable T+1 %+v %v", v, e)
	}
}
