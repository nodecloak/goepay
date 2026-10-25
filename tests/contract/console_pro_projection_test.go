package contract_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	provider "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

func TestConsoleBuyerFilterMatchesPreauthorizationProjection(t *testing.T) {
	s, p, _ := fixture(t)
	ctx := context.Background()
	for _, row := range []struct{ external, reported string }{{"no-report", ""}, {"empty-report", ""}, {"report-priority", "reported-buyer"}} {
		o, err := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: row.external, Name: "Synthetic preauthorization", Type: "alipay", Amount: 1234})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err = s.Store.BeginProviderAttempt(ctx, o.TradeNo, 7, "preauth"); err != nil {
			t.Fatal(err)
		}
		if err = s.Store.RecordPreauthorization(ctx, 7, provider.AuthorizationEvent{TradeNo: o.TradeNo, RequestNo: o.TradeNo, State: "SUCCESS", Amount: 1234, AuthNo: "synthetic-" + row.external, BuyerID: "authorization-buyer"}); err != nil {
			t.Fatal(err)
		}
		if row.external != "no-report" {
			if _, err = p.Exec(ctx, `INSERT INTO order_reporting(order_id,buyer,client_ip) VALUES($1,$2,'198.51.100.10')`, o.ID, row.reported); err != nil {
				t.Fatal(err)
			}
		}
	}
	token, csrf, err := s.Sessions.Create(identity.User{Username: "merchant", ID: 1, Role: "merchant"})
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []struct {
		buyer string
		total float64
	}{{"authorization-buyer", 2}, {"reported-buyer", 1}, {"absent-buyer", 0}} {
		response := lookupHTTP(t, s.Handler(), "GET", "/console/v1/merchant/orders?buyer="+query.buyer, "", token, "merchant", csrf)
		if response.Code != 200 {
			t.Fatal(response.Code, response.Body.String())
		}
		data := lookupData(t, response)
		if data["total"] != query.total {
			t.Errorf("buyer %s: got %v want %v", query.buyer, data["total"], query.total)
		}
		for _, raw := range data["items"].([]any) {
			if raw.(map[string]any)["buyer"] != query.buyer {
				t.Errorf("filter and displayed buyer disagree: %v", raw)
			}
		}
	}
	if _, err = p.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES('merchant-two','merchant',2,'Synthetic other','unused')`); err != nil {
		t.Fatal(err)
	}
	other, proof, err := s.Sessions.Create(identity.User{Username: "merchant-two", ID: 2, Role: "merchant"})
	if err != nil {
		t.Fatal(err)
	}
	response := lookupHTTP(t, s.Handler(), "GET", "/console/v1/merchant/orders?buyer=authorization-buyer", "", other, "merchant", proof)
	if response.Code != 200 || lookupData(t, response)["total"] != float64(0) {
		t.Fatal("buyer filter crossed tenant", response.Body.String())
	}
}

func TestConsoleProOrderProjectionUsesLocalMetadataAndTenantScope(t *testing.T) {
	s, p, _ := fixture(t)
	ctx := context.Background()
	one, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "pro-display-one", Name: "Synthetic item", Type: "wxpay", Amount: 1234})
	if e != nil {
		t.Fatal(e)
	}
	two, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 2, OutTradeNo: "pro-display-two", Name: "Other item", Type: "alipay", Amount: 2345})
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.Store.BeginProviderAttempt(ctx, one.TradeNo, 7, "native"); e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(ctx, `UPDATE provider_attempts SET provider_trade_no='synthetic-upstream',material='private-material-must-not-display' WHERE order_id=$1`, one.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(ctx, `INSERT INTO order_reporting(order_id,buyer,client_ip) VALUES($1,'buyer-one','198.51.100.10'),($2,'other-tenant-private-buyer','198.51.100.20')`, one.ID, two.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(ctx, `INSERT INTO jobs(event_key,kind,payload,state,created_at,last_error) VALUES('pro-notify-old','payment.notify',jsonb_build_object('trade_no',$1::text),'succeeded',now()-interval '1 minute',''),('pro-notify-current','payment.notify',jsonb_build_object('trade_no',$1::text),'pending',now(),'private-error-must-not-display')`, one.TradeNo); e != nil {
		t.Fatal(e)
	}
	token, csrf, e := s.Sessions.Create(identity.User{Username: "merchant", ID: 1, Role: "merchant"})
	if e != nil {
		t.Fatal(e)
	}
	var jobsBefore int
	if e = p.QueryRow(ctx, `SELECT count(*) FROM jobs`).Scan(&jobsBefore); e != nil {
		t.Fatal(e)
	}
	listed := lookupHTTP(t, s.Handler(), "GET", "/console/v1/merchant/orders", "", token, "merchant", csrf)
	if listed.Code != 200 {
		t.Fatal(listed.Code, listed.Body.String())
	}
	data := lookupData(t, listed)
	items := data["items"].([]any)
	if len(items) != 1 || data["total"] != float64(1) {
		t.Fatal("tenant list changed", data)
	}
	expected := map[string]string{"channel_id": "7", "api_trade_no": "synthetic-upstream", "buyer": "buyer-one", "client_ip": "198.51.100.10", "notification_state": "pending", "refunded_amount": "0", "amount": "1234", "merchant_id": "1"}
	row := items[0].(map[string]any)
	for key, want := range expected {
		if row[key] != want {
			t.Errorf("%s: got %#v, want %q", key, row[key], want)
		}
	}
	for _, secret := range []string{"private-material-must-not-display", "private-error-must-not-display", "other-tenant-private-buyer", "source_proof", "api_key"} {
		if strings.Contains(listed.Body.String(), secret) {
			t.Fatal("projection leaked private/cross-tenant data", secret)
		}
	}
	detail := lookupHTTP(t, s.Handler(), "GET", "/console/v1/merchant/orders/"+one.TradeNo, "", token, "merchant", csrf)
	if detail.Code != 200 {
		t.Fatal(detail.Code, detail.Body.String())
	}
	reporting, ok := lookupData(t, detail)["reporting"].(map[string]any)
	if !ok {
		t.Fatal("order detail reporting projection missing")
	}
	for key, want := range expected {
		if key == "amount" || key == "merchant_id" {
			continue
		}
		if reporting[key] != want {
			t.Errorf("detail %s: got %#v want %q", key, reporting[key], want)
		}
	}
	denied := lookupHTTP(t, s.Handler(), "GET", "/console/v1/merchant/orders/"+two.TradeNo, "", token, "merchant", csrf)
	if denied.Code != 404 {
		t.Fatal("other tenant detail exposed", denied.Code)
	}
	var jobsAfter, journals int
	if e = p.QueryRow(ctx, `SELECT count(*) FROM jobs`).Scan(&jobsAfter); e != nil {
		t.Fatal(e)
	}
	if e = p.QueryRow(ctx, `SELECT count(*) FROM journals`).Scan(&journals); e != nil {
		t.Fatal(e)
	}
	if jobsAfter != jobsBefore || journals != 0 {
		t.Fatal("display reads changed financial/worker state", jobsBefore, jobsAfter, journals)
	}
	// Keep large/empty values as strings and do not invent a notification result
	// for an order which never had a local notification task.
	admin, proof, e := s.Sessions.Create(identity.User{Username: "admin", ID: 1, Role: "admin"})
	if e != nil {
		t.Fatal(e)
	}
	other := lookupHTTP(t, s.Handler(), "GET", "/console/v1/admin/orders?merchant_id=2", "", admin, "admin", proof)
	if other.Code != 200 {
		t.Fatal(other.Code, other.Body.String())
	}
	raw, _ := json.Marshal(lookupData(t, other)["items"])
	if !strings.Contains(string(raw), `"channel_id":""`) || !strings.Contains(string(raw), `"notification_state":"none"`) {
		t.Fatal("unavailable metadata was fabricated", string(raw))
	}
}
