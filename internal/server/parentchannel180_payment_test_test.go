package server

import (
	"encoding/json"
	"goepay/internal/identity"
	"strconv"
	"testing"
)

func TestParentChannel180ActualHTTPTestPaymentUsesOrdinaryOrchestrationAndReadsOriginalIntent(t *testing.T) {
	setup := parent180RuntimeReady(t)
	f := setup.f
	ctx := t.Context()
	if _, e := f.p.Exec(ctx, `INSERT INTO backoffice_records(resource,body)VALUES('settings','{"key":"test_pay_uid","value":"1"}'),('settings','{"key":"payment_fee_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	requestID := "18001800180018001800180018001800"
	body, _ := json.Marshal(map[string]any{"request_id": requestID, "name": "支付测试", "money": "100.00", "expected_fingerprint": setup.state["fingerprint"]})
	w := parent180HTTP(f, "POST", "/"+setup.id+"/tests", string(body))
	if w.Code != 200 {
		t.Fatal("original protected test payment unavailable", w.Code)
	}
	var out struct{ Data map[string]any }
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Data["request_id"] != requestID || out.Data["state"] != "pending" || setup.creates.Load() != 1 {
		t.Fatal("test never used ordinary payment adapter", setup.creates.Load(), w.Body.String())
	}
	trade, _ := out.Data["trade_no"].(string)
	if trade == "" {
		t.Fatal("test has no original order")
	}
	order, e := f.s.Store.OrderByTradeNo(ctx, trade)
	if e != nil || order.MerchantAmount != 9950 || order.Amount != 10000 {
		t.Fatal("test bypassed original fee/order", e)
	}
	if w = parent180HTTP(f, "POST", "/"+setup.id+"/tests", string(body)); w.Code != 200 || setup.creates.Load() != 1 {
		t.Fatal("same test intent sent again", w.Code, setup.creates.Load())
	}
	if w = parent180HTTP(f, "GET", "/"+setup.id+"/tests/"+requestID, ""); w.Code != 200 || setup.creates.Load() != 1 || setup.queries.Load() != 0 {
		t.Fatal("original read caused provider mutation", w.Code)
	}
	if w = parent180HTTP(f, "POST", "/"+setup.id+"/tests/"+requestID+"/query", "{}"); w.Code != 200 || setup.creates.Load() != 1 || setup.queries.Load() != 1 || setup.bad.Load() != 0 {
		t.Fatal("test query lost original intent", w.Code, setup.creates.Load(), setup.queries.Load())
	}
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Data["state"] != "paid" {
		t.Fatal("test query did not show verified original receipt")
	}
}

func TestParentChannel180ActualHTTPTestPaymentRejectsCallerOverridesAndMissingFixedMerchant(t *testing.T) {
	setup := parent180RuntimeReady(t)
	f := setup.f
	ctx := t.Context()
	body := map[string]any{"request_id": "18011801180118011801180118011801", "name": "支付测试", "money": "100.00", "expected_fingerprint": setup.state["fingerprint"]}
	raw, _ := json.Marshal(body)
	if w := parent180HTTP(f, "POST", "/"+setup.id+"/tests", string(raw)); w.Code != 422 {
		t.Fatal("missing fixed merchant was inferred", w.Code)
	}
	for _, field := range []string{"merchant_id", "notify_url", "return_url", "method", "provider_url"} {
		body[field] = "untrusted"
		raw, _ = json.Marshal(body)
		if w := parent180HTTP(f, "POST", "/"+setup.id+"/tests", string(raw)); w.Code != 400 {
			t.Fatal("caller override accepted", field, w.Code)
		}
		delete(body, field)
	}
	var rows int
	if e := f.p.QueryRow(ctx, `SELECT (SELECT count(*) FROM runtime_parent_payment_tests)+(SELECT count(*) FROM orders)+(SELECT count(*) FROM provider_attempts)`).Scan(&rows); e != nil || rows != 0 || setup.creates.Load() != 0 {
		t.Fatal("rejected test left financial work", rows, setup.creates.Load(), e)
	}
}

func TestParentChannel180ActualHTTPTestPaymentFinalSessionExpiryRollsBackFirstSend(t *testing.T) {
	setup := parent180RuntimeReady(t)
	f := setup.f
	ctx := t.Context()
	key := "18021802180218021802180218021802"
	if _, e := f.p.Exec(ctx, `INSERT INTO backoffice_records(resource,body)VALUES('settings','{"key":"test_pay_uid","value":"1"}');CREATE FUNCTION parent180_test_last_wait()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.kind='payment.reconcile' THEN PERFORM pg_sleep(1.3);END IF;RETURN NEW;END$$;CREATE TRIGGER parent180_test_last_wait BEFORE INSERT ON jobs FOR EACH ROW EXECUTE FUNCTION parent180_test_last_wait();UPDATE console_sessions SET expires_at=clock_timestamp()+interval '1 second' WHERE username='admin168'`); e != nil {
		t.Fatal(e)
	}
	body, _ := json.Marshal(map[string]any{"request_id": key, "name": "支付测试", "money": "100.00", "expected_fingerprint": setup.state["fingerprint"]})
	w := parent180HTTP(f, "POST", "/"+setup.id+"/tests", string(body))
	if w.Code != 403 || setup.creates.Load() != 0 {
		t.Fatal("expired session crossed test first send", w.Code, setup.creates.Load())
	}
	var attempts, jobs int
	if e := f.p.QueryRow(ctx, `SELECT (SELECT count(*) FROM provider_attempts),(SELECT count(*) FROM jobs WHERE kind='payment.reconcile')`).Scan(&attempts, &jobs); e != nil || attempts != 0 || jobs != 0 {
		t.Fatal("expired test left send barrier or reconciliation", attempts, jobs, e)
	}
	var e error
	f.token, f.csrf, e = f.s.Sessions.Create(identity.User{ID: 168, Role: "admin", Username: "admin168", AdminClass: "super", AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	if w = parent180HTTP(f, "GET", "/"+setup.id+"/tests/"+key, ""); w.Code != 200 || setup.creates.Load() != 0 {
		t.Fatal("cannot recover original expired test", w.Code)
	}
	if w = parent180HTTP(f, "POST", "/"+setup.id+"/tests", string(body)); w.Code != 200 || setup.creates.Load() != 0 {
		t.Fatal("replay rearmed expired test", w.Code, setup.creates.Load())
	}
	var trade string
	if e = f.p.QueryRow(ctx, `SELECT trade_no FROM orders WHERE param LIKE $1`, "parent-test:168:"+key+":%").Scan(&trade); e != nil {
		t.Fatal(e)
	}
	id, _ := strconv.ParseInt(setup.id, 10, 64)
	if _, e = f.s.ProviderFlow.Submit(ctx, trade, id, "native", "192.0.2.1", ""); e == nil || setup.creates.Load() != 0 {
		t.Fatal("ordinary payment call bypassed protected test admission", setup.creates.Load(), e)
	}
}

func TestParentChannel180ActualHTTPTestPaymentLostRegistryNeverCreatesAnotherOrder(t *testing.T) {
	setup := parent180RuntimeReady(t)
	f := setup.f
	ctx := t.Context()
	key := "18031803180318031803180318031803"
	if _, e := f.p.Exec(ctx, `INSERT INTO backoffice_records(resource,body)VALUES('settings','{"key":"test_pay_uid","value":"1"}')`); e != nil {
		t.Fatal(e)
	}
	body, _ := json.Marshal(map[string]any{"request_id": key, "name": "支付测试", "money": "100.00", "expected_fingerprint": setup.state["fingerprint"]})
	if w := parent180HTTP(f, "POST", "/"+setup.id+"/tests", string(body)); w.Code != 200 || setup.creates.Load() != 1 {
		t.Fatal("first test did not send", w.Code)
	}
	if _, e := f.p.Exec(ctx, `ALTER TABLE runtime_parent_payment_tests DISABLE TRIGGER runtime_parent_payment_test_immutable;DELETE FROM runtime_parent_payment_tests;ALTER TABLE runtime_parent_payment_tests ENABLE TRIGGER runtime_parent_payment_test_immutable`); e != nil {
		t.Fatal(e)
	}
	if w := parent180HTTP(f, "POST", "/"+setup.id+"/tests", string(body)); w.Code != 409 || setup.creates.Load() != 1 {
		t.Fatal("lost original test created another order", w.Code, setup.creates.Load())
	}
	if w := parent180HTTP(f, "GET", "/"+setup.id+"/tests/"+key, ""); w.Code != 409 || setup.queries.Load() != 0 {
		t.Fatal("lost source used a guessed order", w.Code, setup.queries.Load())
	}
}
