package server

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestParentChannel180ActualHTTPCallbackRestoresNewParentOriginalAccountEvenWhenDisabled(t *testing.T) {
	setup := parent180RuntimeReady(t)
	f := setup.f
	ctx := t.Context()
	if out := parent180WxPay(t, f, setup.id, "parent180-http-callback-A"); out["code"] != float64(1) {
		t.Fatal("original create failed")
	}
	order, e := f.s.Store.Order(ctx, 1, "parent180-http-callback-A")
	if e != nil {
		t.Fatal(e)
	}
	parent180RotateOutgoing(t, setup)
	read := parent180HTTP(f, "GET", "/"+setup.id, "")
	var current struct{ Data map[string]any }
	json.Unmarshal(read.Body.Bytes(), &current)
	body, _ := json.Marshal(map[string]any{"expected_fingerprint": current.Data["fingerprint"], "enabled": false})
	if w := parent180HTTP(f, "PATCH", "/"+setup.id, string(body)); w.Code != 200 {
		t.Fatal(w.Code)
	}
	f.s.ProviderFlow.AllowLive = false
	callback := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "synthetic180-fresh", "mch_id": "synthetic180-fresh-mch", "out_trade_no": order.TradeNo, "transaction_id": "WX180_" + order.TradeNo, "total_fee": "10000", "trade_type": "NATIVE", "openid": "synthetic180-buyer"}
	send := func(key string) int {
		r := httptest.NewRequest("POST", "/callbacks/providers/"+setup.id+"/"+order.TradeNo, bytes.NewReader(parent180SignedXML(callback, key)))
		r.Header.Set("Content-Type", "application/xml")
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		return w.Code
	}
	if code := send(setup.keyB); code != 400 {
		t.Fatal("current key authenticated original HTTP callback", code)
	}
	if code := send("abcdef0123456789abcdef0123456789"); code != 200 {
		t.Fatal("new parent original callback unavailable", code)
	}
	paid, e := f.s.Store.Order(ctx, 1, "parent180-http-callback-A")
	if e != nil || paid.Status != "paid" || setup.creates.Load() != 1 || setup.queries.Load() != 0 || setup.bad.Load() != 0 {
		t.Fatal("callback did not settle exact original order", paid.Status, e)
	}
}
