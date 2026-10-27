package contract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"goepay/internal/compat"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestT1BalanceAndConsoleCapabilitiesAgree(t *testing.T) {
	s, pool, key := fixture(t)
	ctx := context.Background()
	if _, e := pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"settle_type","value":"1"}'); UPDATE merchants SET profile='{"settle_type":"alipay","transfer_api_enabled":true}' WHERE id=2`); e != nil {
		t.Fatal(e)
	}
	o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 2, OutTradeNo: "today", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.MarkPaid(ctx, o.TradeNo, "t1-today"); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, "INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) SELECT 'rsa','merchant',2,'RSA',password_hash,'operator' FROM console_users WHERE role='merchant' AND subject_id=1"); e != nil {
		t.Fatal(e)
	}
	p := compat.Params{"pid": "2", "sign_type": "RSA", "timestamp": fmt.Sprint(time.Now().Unix())}
	p["sign"], _ = compat.SignRSA(p, key)
	v := url.Values{}
	for k, x := range p {
		v.Set(k, x)
	}
	_, out := call(t, s.Handler(), "POST", "/api/transfer/balance", v)
	if out["available_money"] != "0.00" || out["ledger_available_money"] != "10.00" || out["today_blocked_money"] != "10.00" || out["settle_type"] != float64(1) {
		t.Fatalf("signed balance %+v", out)
	}
	token, _, e := s.Sessions.Create(identity.User{Username: "rsa", Role: "merchant", ID: 2})
	if e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{"/console/v1/merchant/transfers/capabilities", "/console/v1/merchant/workflows/funds-availability"} {
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		var envelope struct {
			Data map[string]any `json:"data"`
		}
		if e = json.Unmarshal(w.Body.Bytes(), &envelope); e != nil {
			t.Fatal(e)
		}
		if w.Code != 200 || envelope.Data["available_money"] != out["available_money"] || envelope.Data["ledger_available_money"] != out["ledger_available_money"] || envelope.Data["today_blocked_money"] != out["today_blocked_money"] || envelope.Data["settle_type"] != out["settle_type"] {
			t.Fatalf("%s status=%d body=%s", path, w.Code, w.Body.String())
		}
	}
}
