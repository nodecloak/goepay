package contract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"goepay/internal/printing"
)

func TestPrintingConsoleOwnerCASPaidReceiptAndOriginalIntent(t *testing.T) {
	s, p, _ := fixture(t)
	ctx := context.Background()
	var err error
	s.Printing, err = printing.New(p, s.Store)
	if err != nil {
		t.Fatal(err)
	}
	const wxKey = "synthetic-print-http-wx-key-12345"
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "print-http-app", "mch_id": "print-http-mch", "code_url": "weixin://local-printing-test"}
		response["sign"], _ = adapter.SignWechatV2(response, wxKey, "MD5")
		w.Write(providerXML(response))
	}))
	defer upstream.Close()
	wx, err := adapter.NewWechatV2(adapter.WechatV2Config{AppID: "print-http-app", MerchantID: "print-http-mch", APIKey: wxKey, BaseURL: upstream.URL, HTTPClient: upstream.Client()})
	if err != nil {
		t.Fatal(err)
	}
	flow := payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: wx}, AllowLive: true, CallbackBaseURL: "https://local.example.invalid"}
	markVerified := func(o payment.Order) {
		t.Helper()
		if _, e := flow.Submit(ctx, o.TradeNo, 7, "native", "127.0.0.1", ""); e != nil {
			t.Fatal(e)
		}
		callback := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "print-http-app", "mch_id": "print-http-mch", "out_trade_no": o.TradeNo, "transaction_id": "local-print-" + o.TradeNo, "total_fee": strconv.FormatInt(o.Amount, 10), "openid": "synthetic-private-buyer"}
		callback["sign"], _ = adapter.SignWechatV2(callback, wxKey, "MD5")
		if e := flow.HandleNotification(ctx, 7, o.TradeNo, nil, providerXML(callback)); e != nil {
			t.Fatal(e)
		}
	}
	admin, adminCSRF, err := s.Sessions.Create(identity.User{Role: "admin", AdminClass: "super", ID: 1, Username: "admin", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	merchant, merchantCSRF, err := s.Sessions.Create(identity.User{Role: "merchant", ID: 1, Username: "merchant", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	callPrint := func(role, method, path, body string) (int, map[string]any, string) {
		t.Helper()
		cookie, csrf := merchant, merchantCSRF
		if role == "admin" {
			cookie, csrf = admin, adminCSRF
		}
		r := httptest.NewRequest(method, "/console/v1/"+role+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: cookie})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("printing response cacheable")
		}
		var out map[string]any
		if json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatal("printing response not JSON", w.Code)
		}
		return w.Code, out, w.Body.String()
	}
	platform := `{"expected_revision":"0","enabled":true,"provider":"local_fake","credential_action":"replace","app_id":"local-http-app","secret":"local-http-secret"}`
	if code, _, body := callPrint("admin", "PUT", "/printing/settings", platform); code != 200 || strings.Contains(body, "local-http-secret") || strings.Contains(body, "local-http-app") {
		t.Fatal("platform config or response leak", code)
	}
	if code, _, _ := callPrint("admin", "PUT", "/printing/settings", platform); code != 409 {
		t.Fatal("stale platform CAS", code)
	}
	if code, _, _ := callPrint("merchant", "GET", "/printing/settings", ""); code != 403 {
		t.Fatal("merchant reached platform config", code)
	}
	if code, _, _ := callPrint("merchant", "PUT", "/account/printing", `{"expected_revision":"0","mode":2,"copies":2,"voice":false,"device_action":"replace","device_id":"local-http-device"}`); code != 200 {
		t.Fatal("owner config", code)
	}
	if code, _, _ := callPrint("merchant", "GET", "/account/printing?merchant_id=2", ""); code != 400 {
		t.Fatal("tenant override", code)
	}
	order, err := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "print-http-owner", Name: "<cut/> safe item", Amount: 12345, Type: "wxpay"})
	if err != nil {
		t.Fatal(err)
	}
	if code, _, _ := callPrint("merchant", "GET", "/orders/"+order.TradeNo+"/receipt", ""); code != 409 {
		t.Fatal("unpaid receipt accepted", code)
	}
	markVerified(order)
	code, receipt, _ := callPrint("merchant", "GET", "/orders/"+order.TradeNo+"/receipt", "")
	if code != 200 || receipt["data"].(map[string]any)["amount"] != "12345" {
		t.Fatal("original paid receipt", code)
	}
	other, err := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 2, OutTradeNo: "print-http-other", Name: "other", Amount: 100, Type: "wxpay"})
	if err != nil {
		t.Fatal(err)
	}
	markVerified(other)
	if code, _, _ := callPrint("merchant", "GET", "/orders/"+other.TradeNo+"/receipt", ""); code != 404 {
		t.Fatal("other owner receipt exposed", code)
	}
	body := `{"external_no":"local-http-manual","previous_job_id":"0","acknowledge_duplicate":false}`
	code, created, _ := callPrint("merchant", "POST", "/orders/"+order.TradeNo+"/print-jobs", body)
	if code != 202 {
		t.Fatal("manual intent", code, created["message"])
	}
	first := created["data"].(map[string]any)
	if first["state"] != "queued" || first["provider"] != "local_fake" {
		t.Fatal("queued intent falsely reported printed", first["state"])
	}
	code, replay, _ := callPrint("merchant", "POST", "/orders/"+order.TradeNo+"/print-jobs", body)
	if code != 202 || replay["data"].(map[string]any)["id"] != first["id"] {
		t.Fatal("manual replay changed original intent", code)
	}
	if code, listed, _ := callPrint("merchant", "GET", "/orders/"+order.TradeNo+"/print-jobs?after_id=0&limit=20", ""); code != 200 || len(listed["data"].(map[string]any)["items"].([]any)) != 1 {
		t.Fatal("order-specific history", code)
	}
	if code, _, _ := callPrint("merchant", "GET", fmt.Sprint("/print-jobs/", first["id"]), ""); code != 200 {
		t.Fatal("owner original job", code)
	}
	if code, _, _ := callPrint("merchant", "GET", "/print-jobs?limit=1&limit=2", ""); code != 400 {
		t.Fatal("ambiguous history query", code)
	}
	oldTask := fmt.Sprint(first["task_id"])
	if oldTask == "" || oldTask == "0" {
		t.Fatal("original intent missing processing task")
	}
	if _, err = p.Exec(ctx, "UPDATE jobs SET state='dead',attempts=max_attempts WHERE id=$1", oldTask); err != nil {
		t.Fatal(err)
	}
	recovery := fmt.Sprintf(`{"expected_task_id":"%s"}`, oldTask)
	recoverPath := fmt.Sprint("/print-jobs/", first["id"], "/recover")
	code, recovered, _ := callPrint("merchant", "POST", recoverPath, recovery)
	if code != 200 {
		t.Fatal("original dead task recovery", code, recovered["message"])
	}
	replacement := recovered["data"].(map[string]any)
	if replacement["id"] != first["id"] || replacement["request_no"] != first["request_no"] || fmt.Sprint(replacement["task_id"]) == oldTask {
		t.Fatal("recovery replaced the print intent or failed to create original replacement task")
	}
	code, recoveredAgain, _ := callPrint("merchant", "POST", recoverPath, recovery)
	if code != 200 || recoveredAgain["data"].(map[string]any)["task_id"] != replacement["task_id"] {
		t.Fatal("lost recovery response consumed another attempt", code)
	}
	var recoveryCount int
	if err = p.QueryRow(ctx, "SELECT count(*) FROM printing_recoveries").Scan(&recoveryCount); err != nil || recoveryCount != 1 {
		t.Fatal("duplicate task recovery", recoveryCount, err)
	}
	var intents, leaks int
	if err = p.QueryRow(ctx, `SELECT count(*) FROM printing_intents`).Scan(&intents); err != nil || intents != 1 {
		t.Fatal("duplicate printing intent", intents, err)
	}
	if err = p.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE changes::text LIKE '%local-http-secret%' OR changes::text LIKE '%local-http-device%'`).Scan(&leaks); err != nil || leaks != 0 {
		t.Fatal("plaintext credential audit", leaks, err)
	}
	if invalid, checkErr := s.Printing.Check(ctx); checkErr != nil || invalid != 0 {
		t.Fatal("healthy HTTP config/paid/original recovery proof failed checker", invalid, checkErr)
	}
}
