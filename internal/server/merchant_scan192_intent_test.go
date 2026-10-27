package server

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

func TestMerchantScan192UnknownOutcomeQueriesOriginalAccountAfterGateAndGrantRemoval(t *testing.T) {
	setup, f := merchantScan192Ready(t)
	setup.loseScanResponse.Store(true)
	const key = "fcbf026f-4937-4244-a18f-6bd8de0391f2"
	merchantScan192Prepare(t, f, key)
	apply := `{"auth_code":"134567890123456789"}`
	unknown := channel168Data(t, merchantScan192Request(f, "POST", "/intents/"+key+"/apply", apply))
	if setup.loseScanResponse.Load() || unknown["state"] == "paid" || unknown["can_query"] != true || unknown["can_apply"] != false || setup.creates.Load() != 1 {
		t.Fatal("actual accepted response loss did not retain unknown original", unknown, setup.creates.Load())
	}
	gate := channel168Data(t, merchantScanGate192Request(setup.f, "GET", ""))
	body, _ := json.Marshal(map[string]any{"scanpay_open": "0", "expected_fingerprint": gate["fingerprint"]})
	channel168Data(t, merchantScanGate192Request(setup.f, "PATCH", string(body)))
	parent180RotateOutgoing(t, setup)
	setup.f.s.MerchantChannels[1] = nil
	if w := merchantScan192Request(f, "POST", "/intents/"+key+"/apply", apply); w.Code != 200 {
		t.Fatal("unknown replay not readable after stop", w.Code)
	}
	w := merchantScan192Request(f, "POST", "/intents/"+key+"/query", "{}")
	if w.Code != 200 {
		t.Fatal("original-account query failed after current stop", w.Code, w.Body.String())
	}
	paid := channel168Data(t, w)
	if paid["state"] != "paid" || paid["trade_no"] != unknown["trade_no"] || setup.creates.Load() != 1 || setup.queries.Load() != 1 || setup.bad.Load() != 0 {
		t.Fatal("recovery changed original account/charge", paid, setup.creates.Load(), setup.queries.Load(), setup.bad.Load())
	}
}

func TestMerchantScan192FinalSessionClockRejectsExpiryDuringAttemptWrites(t *testing.T) {
	setup, f := merchantScan192Ready(t)
	const key = "f6be02e1-6cde-46de-9d9e-13fa96bb71c3"
	merchantScan192Prepare(t, f, key)
	if _, e := f.p.Exec(t.Context(), `CREATE SEQUENCE scan192_final_probe;CREATE FUNCTION scan192_delay_attempt()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN PERFORM nextval('scan192_final_probe');PERFORM pg_sleep(3);RETURN NEW;END$$;CREATE TRIGGER scan192_attempt_delay BEFORE INSERT ON jobs FOR EACH ROW WHEN(NEW.kind='payment.reconcile')EXECUTE FUNCTION scan192_delay_attempt();UPDATE console_sessions SET expires_at=clock_timestamp()+interval '2 seconds' WHERE role='merchant'`); e != nil {
		t.Fatal(e)
	}
	w := merchantScan192Request(f, "POST", "/intents/"+key+"/apply", `{"auth_code":"134567890123456789"}`)
	if w.Code != 403 {
		t.Fatal("expired final session admitted", w.Code)
	}
	var entered bool
	var effects int
	if e := f.p.QueryRow(t.Context(), `SELECT is_called FROM scan192_final_probe`).Scan(&entered); e != nil || !entered {
		t.Fatal("did not enter delayed final write", entered, e)
	}
	if e := f.p.QueryRow(t.Context(), `SELECT(SELECT count(*)FROM provider_attempts)+(SELECT count(*)FROM jobs)+(SELECT count(*)FROM journals)`).Scan(&effects); e != nil || effects != 0 || setup.creates.Load() != 0 {
		t.Fatal("expired attempt committed or reached provider", effects, setup.creates.Load(), e)
	}
}

func merchantScan192Ready(t *testing.T) (parent180Runtime, buyer104MerchantFixture) {
	t.Helper()
	setup := parent180RuntimeReady(t)
	config, _ := json.Marshal(map[string]any{"expected_fingerprint": setup.state["fingerprint"], "account": map[string]any{"config": map[string]any{"enable_scan_pay": map[string]string{"operation": "replace", "value": "true"}}}})
	if w := parent180HTTP(setup.f, "PATCH", "/"+setup.id, string(config)); w.Code != 200 {
		t.Fatal("enable local scan", w.Code)
	} else {
		setup.state = channel168Data(t, w)
	}
	return setup, buyer190MerchantOn(t, setup.f)
}
func merchantScan192Prepare(t *testing.T, f buyer104MerchantFixture, key string) string {
	t.Helper()
	body := `{"request_id":"` + key + `","money":"100.00","remark":"合成收款","auth_code":"134567890123456789"}`
	if w := merchantScan192Request(f, "POST", "/prepare", body); w.Code != 200 {
		t.Fatal("prepare", w.Code, w.Body.String())
	}
	return body
}

func TestMerchantScan192CancelUnsentPreparationPreventsApply(t *testing.T) {
	setup, f := merchantScan192Ready(t)
	const key = "75b5ce21-077b-4cf5-93da-a8885d4bf7e7"
	merchantScan192Prepare(t, f, key)
	w := merchantScan192Request(f, "POST", "/intents/"+key+"/close", "{}")
	if w.Code != 200 {
		t.Fatal("close unsent preparation missing", w.Code)
	}
	if v := channel168Data(t, w); v["state"] != "closed" || v["can_apply"] != false {
		t.Fatal("unsent intent not closed", v)
	}
	w = merchantScan192Request(f, "POST", "/intents/"+key+"/apply", `{"auth_code":"134567890123456789"}`)
	if w.Code != 200 || channel168Data(t, w)["state"] != "closed" {
		t.Fatal("closed intent revived", w.Code)
	}
	if setup.creates.Load() != 0 || setup.queries.Load() != 0 {
		t.Fatal("closing unsent preparation reached provider")
	}
}

func TestMerchantScan192OrdinaryAPIAndLostSourceCannotAdoptPreparation(t *testing.T) {
	setup, f := merchantScan192Ready(t)
	const key = "69ded9a8-8ecb-471a-95d9-55c24ae8bad5"
	body := merchantScan192Prepare(t, f, key)
	state := channel168Data(t, merchantScan192Request(f, "GET", "/intents/"+key, ""))
	trade := state["trade_no"].(string)
	var channel int64
	if e := f.p.QueryRow(t.Context(), `SELECT channel_id FROM provider_route_assignments WHERE merchant_id=1`).Scan(&channel); e != nil {
		t.Fatal(e)
	}
	if _, e := f.s.ProviderFlow.SubmitScan(t.Context(), trade, channel, "192.0.2.1", "134567890123456789"); e == nil {
		t.Fatal("ordinary API adopted cashier intent")
	}
	if _, e := f.p.Exec(t.Context(), `ALTER TABLE merchant_scan_order_sources106 DISABLE TRIGGER USER;DELETE FROM merchant_scan_order_sources106;ALTER TABLE merchant_scan_order_sources106 ENABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	if w := merchantScan192Request(f, "POST", "/intents/"+key+"/apply", `{"auth_code":"134567890123456789"}`); w.Code != 409 {
		t.Fatal("missing source allowed apply", w.Code)
	}
	if w := merchantScan192Request(f, "POST", "/prepare", body); w.Code != 409 {
		t.Fatal("prepare regenerated missing original source", w.Code)
	}
	if setup.creates.Load() != 0 || setup.queries.Load() != 0 {
		t.Fatal("lost source reached provider")
	}
}

func TestMerchantScan192GateCloseReopenDoesNotRevivePreparedAdmission(t *testing.T) {
	setup, f := merchantScan192Ready(t)
	const key = "a0871178-0e74-4ee4-810b-2c05745b33a8"
	merchantScan192Prepare(t, f, key)
	gate := channel168Data(t, merchantScanGate192Request(setup.f, "GET", ""))
	for _, value := range []string{"0", "1"} {
		body, _ := json.Marshal(map[string]any{"scanpay_open": value, "expected_fingerprint": gate["fingerprint"]})
		gate = channel168Data(t, merchantScanGate192Request(setup.f, "PATCH", string(body)))
	}
	w := merchantScan192Request(f, "POST", "/intents/"+key+"/apply", `{"auth_code":"134567890123456789"}`)
	if w.Code != 409 {
		t.Fatal("ABA reopening revived original send", w.Code)
	}
	if setup.creates.Load() != 0 {
		t.Fatal("stale platform generation charged")
	}
}

func TestMerchantScan192ConcurrentApplyCannotChargeTwice(t *testing.T) {
	setup, f := merchantScan192Ready(t)
	const key = "fb0c8c7f-2b24-420c-be31-9f79fa46fe43"
	merchantScan192Prepare(t, f, key)
	var wg sync.WaitGroup
	results := make(chan int, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- merchantScan192Request(f, "POST", "/intents/"+key+"/apply", `{"auth_code":"134567890123456789"}`).Code
		}()
	}
	wg.Wait()
	close(results)
	for status := range results {
		if status != 200 {
			t.Fatal("concurrent original replay", status)
		}
	}
	if setup.creates.Load() != 1 || setup.bad.Load() != 0 {
		t.Fatal("concurrent cashier sent again", setup.creates.Load(), setup.bad.Load())
	}
	if v := channel168Data(t, merchantScan192Request(f, "GET", "/intents/"+key, "")); v["state"] != "paid" {
		t.Fatal("original result lost", v)
	}
}

func TestMerchantScan192UnroutedDeclarationCanBeClosedWithoutLaterRevival(t *testing.T) {
	setup, f := merchantScan192Ready(t)
	const key = "54daf14c-02a0-444f-8f4d-37646b841f31"
	original := append([]int64(nil), setup.f.s.MerchantChannels[1]...)
	setup.f.s.MerchantChannels[1] = nil
	body := `{"request_id":"` + key + `","money":"100.00","remark":"合成收款","auth_code":"134567890123456789"}`
	if w := merchantScan192Request(f, "POST", "/prepare", body); w.Code == 200 {
		t.Fatal("no route accepted")
	}
	state := channel168Data(t, merchantScan192Request(f, "GET", "/intents/"+key, ""))
	if state["state"] != "unprepared" {
		t.Fatal("declaration lost after route rejection", state)
	}
	w := merchantScan192Request(f, "POST", "/intents/"+key+"/close", "{}")
	if w.Code != 200 {
		t.Fatal("cannot abandon durable unsent declaration", w.Code)
	}
	if state = channel168Data(t, w); state["state"] != "closed" {
		t.Fatal("declaration not closed", state)
	}
	setup.f.s.MerchantChannels[1] = original
	if w = merchantScan192Request(f, "POST", "/prepare", body); w.Code != 409 {
		t.Fatal("closed declaration revived", w.Code)
	}
	if setup.creates.Load() != 0 || setup.queries.Load() != 0 {
		t.Fatal("abandon contacted provider")
	}
}

func TestMerchantScan192RejectsAmbiguousBodiesBeforeDeclaringOrRouting(t *testing.T) {
	setup, f := merchantScan192Ready(t)
	body := `{"request_id":"4f032ca4-c99d-44ac-9526-f78e07cf9a81","money":"100.00","remark":"","auth_code":"134567890123456789"}`
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"more than six integer digits", strings.Replace(body, `"100.00"`, `"0000000100.00"`, 1), 422},
		{"duplicate field", strings.Replace(body, `"remark":""`, `"remark":"","remark":"duplicate"`, 1), 400},
		{"null remark", strings.Replace(body, `"remark":""`, `"remark":null`, 1), 400},
		{"numeric amount", strings.Replace(body, `"money":"100.00"`, `"money":100`, 1), 400},
		{"authority override", strings.Replace(body, `"remark":""`, `"remark":"","merchant_id":"2"`, 1), 400},
		{"wrong barcode", strings.Replace(body, `134567890123456789`, `13456789012345678`, 1), 422},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := merchantScan192Request(f, "POST", "/prepare", tc.body)
			if w.Code != tc.status {
				t.Fatal("ambiguous cashier accepted", w.Code, tc.status)
			}
		})
	}
	var effects int
	if e := f.p.QueryRow(t.Context(), `SELECT(SELECT count(*)FROM merchant_scan_intents106)+(SELECT count(*)FROM provider_route_assignments)+(SELECT count(*)FROM orders)+(SELECT count(*)FROM provider_attempts)+(SELECT count(*)FROM journals)`).Scan(&effects); e != nil || effects != 0 || setup.creates.Load() != 0 {
		t.Fatal("invalid cashier wrote an intent/route/order", effects, setup.creates.Load(), e)
	}
}

func TestMerchantScan192CloseBeforeDelayedPrepareReservesRequestWithoutCharge(t *testing.T) {
	setup, f := merchantScan192Ready(t)
	const key = "966f0dda-fc56-485c-897b-ad89f36e487c"
	w := merchantScan192Request(f, "POST", "/intents/"+key+"/close", "{}")
	if w.Code != 200 {
		t.Fatal("lost preparation cannot be safely closed", w.Code)
	}
	if state := channel168Data(t, w); state["state"] != "closed" || state["can_apply"] != false || state["can_query"] != false {
		t.Fatal("unsent request not reserved closed", state)
	}
	// The original prepare might still be in flight when the user closes. It
	// must see the durable closure under the same merchant/request lock.
	body := `{"request_id":"` + key + `","money":"100.00","remark":"","auth_code":"134567890123456789"}`
	if w = merchantScan192Request(f, "POST", "/prepare", body); w.Code != 409 {
		t.Fatal("delayed preparation revived a closed request", w.Code)
	}
	var effects int
	if e := f.p.QueryRow(t.Context(), `SELECT(SELECT count(*)FROM merchant_scan_intents106)+(SELECT count(*)FROM provider_route_assignments)+(SELECT count(*)FROM orders)+(SELECT count(*)FROM provider_attempts)`).Scan(&effects); e != nil || effects != 0 || setup.creates.Load() != 0 {
		t.Fatal("closed request allocated a charge", effects, setup.creates.Load(), e)
	}
}

// A prepare that reaches micropay, or a replay that owns a second send, is a
// real charge duplication bug. The local peer verifies actual signed XML/mTLS.
func TestMerchantScan192PrepareDoesNotChargeAndApplyOwnsExactlyOneSend(t *testing.T) {
	setup := parent180RuntimeReady(t)
	config, _ := json.Marshal(map[string]any{"expected_fingerprint": setup.state["fingerprint"], "account": map[string]any{"config": map[string]any{"enable_scan_pay": map[string]string{"operation": "replace", "value": "true"}}}})
	if w := parent180HTTP(setup.f, "PATCH", "/"+setup.id, string(config)); w.Code != 200 {
		t.Fatal("enable local scan", w.Code)
	}
	f := buyer190MerchantOn(t, setup.f)
	const request = "c601ef3b-5189-4ae1-92d1-71536bdf1524"
	const code = "134567890123456789"
	body := `{"request_id":"` + request + `","money":"100.00","remark":"合成收款","auth_code":"` + code + `"}`
	w := merchantScan192Request(f, "POST", "/prepare", body)
	if w.Code != 200 {
		t.Fatal("prepare missing", w.Code, w.Body.String())
	}
	prepared := channel168Data(t, w)
	if prepared["state"] != "prepared" || prepared["money"] != "100.00" || prepared["charge_money"] != "100.00" || prepared["can_apply"] != true || prepared["request_id"] != request {
		t.Fatal("preparation not fixed", prepared)
	}
	if setup.creates.Load() != 0 || setup.queries.Load() != 0 {
		t.Fatal("prepare reached upstream")
	}
	replay := channel168Data(t, merchantScan192Request(f, "POST", "/prepare", body))
	if replay["trade_no"] != prepared["trade_no"] {
		t.Fatal("prepare allocated another order")
	}
	if w = merchantScan192Request(f, "POST", "/prepare", strings.Replace(body, "100.00", "100.01", 1)); w.Code != 409 {
		t.Fatal("request accepted different amount", w.Code)
	}
	apply := `{"auth_code":"` + code + `"}`
	w = merchantScan192Request(f, "POST", "/intents/"+request+"/apply", apply)
	if w.Code != 200 {
		t.Fatal("apply failed", w.Code, w.Body.String())
	}
	paid := channel168Data(t, w)
	if paid["state"] != "paid" || paid["trade_no"] != prepared["trade_no"] || paid["can_apply"] != false {
		t.Fatal("verified result lost", paid)
	}
	if w = merchantScan192Request(f, "POST", "/intents/"+request+"/apply", apply); w.Code != 200 {
		t.Fatal("lost-response replay", w.Code)
	}
	if w = merchantScan192Request(f, "GET", "/intents/"+request, ""); w.Code != 200 {
		t.Fatal("original result unreadable", w.Code)
	}
	if setup.creates.Load() != 1 || setup.queries.Load() != 0 || setup.bad.Load() != 0 {
		t.Fatal("not exactly one signed local charge", setup.creates.Load(), setup.queries.Load(), setup.bad.Load())
	}
	var orders, attempts, imbalanced int
	if e := f.p.QueryRow(t.Context(), `SELECT(SELECT count(*)FROM orders),(SELECT count(*)FROM provider_attempts),(SELECT count(*)FROM(SELECT journal_id FROM ledger_entries GROUP BY journal_id HAVING sum(amount)<>0)x)`).Scan(&orders, &attempts, &imbalanced); e != nil || orders != 1 || attempts != 1 || imbalanced != 0 {
		t.Fatal("duplicate or unbalanced", orders, attempts, imbalanced, e)
	}
	var leaked bool
	if e := f.p.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM merchant_scan_intents106 WHERE facts::text LIKE $1) OR EXISTS(SELECT 1 FROM merchant_scan_order_sources106 WHERE facts::text LIKE $1) OR EXISTS(SELECT 1 FROM backoffice_audit WHERE changes::text LIKE $1)`, "%"+code+"%").Scan(&leaked); e != nil || leaked {
		t.Fatal("raw bearer persisted", leaked, e)
	}
}
