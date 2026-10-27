package server

import (
	"encoding/json"
	"testing"
)

func TestParentChannel180ActualTLSScanPaymentAndOriginalQueryAfterRotation(t *testing.T) {
	setup := parent180RuntimeReady(t)
	f := setup.f
	ctx := t.Context()
	body, _ := json.Marshal(map[string]any{"expected_fingerprint": setup.state["fingerprint"], "account": map[string]any{"config": map[string]any{"enable_scan_pay": map[string]string{"operation": "replace", "value": "true"}}}})
	w := parent180HTTP(f, "PATCH", "/"+setup.id, string(body))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var current struct{ Data map[string]any }
	json.Unmarshal(w.Body.Bytes(), &current)
	setup.state = current.Data
	out := parent180WxPayment(t, f, setup.id, "parent180-scan-A", "scan", "120000000000000001")
	if out["code"] != float64(1) || setup.creates.Load() != 1 || setup.bad.Load() != 0 {
		t.Fatal("scan never reached its source account", out, setup.creates.Load(), setup.bad.Load())
	}
	order, e := f.s.Store.Order(ctx, 1, "parent180-scan-A")
	if e != nil {
		t.Fatal(e)
	}
	parent180RotateOutgoing(t, setup)
	if _, e = f.s.ProviderFlow.Query(ctx, 1, order.TradeNo); e != nil || setup.queries.Load() != 1 || setup.bad.Load() != 0 {
		t.Fatal("scan query changed original account", setup.queries.Load(), setup.bad.Load(), e)
	}
}
