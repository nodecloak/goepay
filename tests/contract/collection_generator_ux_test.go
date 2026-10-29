package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"goepay/internal/simulation"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

func TestCollectionGeneratorUXDirectMetadataIdempotencyAndFixedAmount(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	var styleID string
	if e := pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('collection-styles','{"name":"generator_standard","title":"标准二维码","enabled":true,"background_color":"#ffffff","text_color":"#000000"}') RETURNING id::text`).Scan(&styleID); e != nil {
		t.Fatal(e)
	}
	env, e := simulation.New(bytes.Repeat([]byte{9}, 32), "http://127.0.0.1:8180")
	if e != nil {
		t.Fatal(e)
	}
	s.CheckoutKey = bytes.Repeat([]byte{7}, 32)
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: env.Providers(), AllowLive: true, LocalSimulation: true, CallbackBaseURL: "http://127.0.0.1:8180"}
	s.MerchantChannels = map[int64][]int64{1: {1, 2}}
	token, csrf, e := s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant"})
	if e != nil {
		t.Fatal(e)
	}
	call := func(method, path string, body any, anti string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, "/console/v1/merchant"+path, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", anti)
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	w := call("GET", "/collection-generator", nil, csrf)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"styles":`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"domains":`)) {
		t.Fatalf("direct generator context %d %s", w.Code, w.Body.String())
	}
	var before int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM backoffice_records WHERE resource='collection-codes'`).Scan(&before); e != nil || before != 0 {
		t.Fatalf("GET wrote a code %d %v", before, e)
	}
	body := map[string]any{"external_no": "generator-once", "recipient_name": "本地收款", "style_id": styleID, "origin": "http://127.0.0.1:8180", "amount": "1.23"}
	if w = call("POST", "/collection-generator", body, "wrong"); w.Code != 403 {
		t.Fatalf("missing CSRF %d", w.Code)
	}
	w = call("POST", "/collection-generator", body, csrf)
	if w.Code != 201 {
		t.Fatalf("generate %d %s", w.Code, w.Body.String())
	}
	var generated struct {
		Data struct {
			CodeID      string `json:"code_id"`
			URL         string `json:"url"`
			ConfigHash  string `json:"config_hash"`
			FixedAmount string `json:"fixed_amount_cents"`
			Recipient   string `json:"recipient_name"`
			Replayed    bool   `json:"replayed"`
		}
	}
	if e = json.Unmarshal(w.Body.Bytes(), &generated); e != nil || generated.Data.CodeID == "" || len(generated.Data.ConfigHash) != 64 || generated.Data.FixedAmount != "123" || generated.Data.Recipient != "本地收款" {
		t.Fatalf("generated response %s %v", w.Body.String(), e)
	}
	if bytes.Contains(w.Body.Bytes(), []byte("proof_hash")) {
		t.Fatal("private generation proof exposed")
	}
	w = call("POST", "/collection-generator", body, csrf)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"replayed":true`)) {
		t.Fatalf("replay %d %s", w.Code, w.Body.String())
	}
	body["amount"] = "2.00"
	if w = call("POST", "/collection-generator", body, csrf); w.Code != 409 {
		t.Fatalf("same request changed amount %d", w.Code)
	}
	body["amount"] = "1.23"
	body["external_no"] = "arbitrary-origin"
	body["origin"] = "https://not-authorized.example.test"
	if w = call("POST", "/collection-generator", body, csrf); w.Code != 422 {
		t.Fatalf("arbitrary code origin %d", w.Code)
	}
	address, e := url.Parse(generated.Data.URL)
	if e != nil || address.Query().Get("mode") != "generated" || address.Query().Get("config") != generated.Data.ConfigHash {
		t.Fatalf("generated mode not bound to signed URL %s %v", generated.Data.URL, e)
	}
	public := func(method, amount string) *httptest.ResponseRecorder {
		var content *strings.Reader
		if method == "POST" {
			content = strings.NewReader(url.Values{"amount": {amount}, "type": {"wxpay"}, "external_no": {"payer-once-intent"}}.Encode())
		} else {
			content = strings.NewReader("")
		}
		r := httptest.NewRequest(method, address.String(), content)
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	w = public("GET", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `value="1.23"`) || !strings.Contains(w.Body.String(), "readonly") {
		t.Fatalf("fixed amount public form %d %s", w.Code, w.Body.String())
	}
	if w = public("POST", "1.24"); w.Code != 422 {
		t.Fatalf("tampered fixed amount %d %s", w.Code, w.Body.String())
	}
	var orders int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM orders").Scan(&orders); e != nil || orders != 0 {
		t.Fatalf("rejected amount created order %d %v", orders, e)
	}
	if w = public("POST", "1.23"); w.Code != 302 {
		t.Fatalf("authorized fixed collection %d %s", w.Code, w.Body.String())
	}
	if w = public("POST", "1.23"); w.Code != 302 {
		t.Fatalf("same payer retry %d", w.Code)
	}
	for i := 0; i < 12; i++ {
		if w = public("POST", "1.23"); w.Code != 302 {
			t.Fatalf("collection retries inherited login budget at %d: %d", i, w.Code)
		}
	}
	var generations, audits, attempts int
	if e = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM collection_code_generations),(SELECT count(*) FROM backoffice_audit WHERE resource='collection-generator' AND operation='generate'),(SELECT count(*) FROM provider_attempts)`).Scan(&generations, &audits, &attempts); e != nil || generations != 1 || audits != 1 || attempts != 1 {
		t.Fatalf("duplicate generation/payment %d %d %d %v", generations, audits, attempts, e)
	}
	balance, e := s.Store.Balance(ctx, 1)
	if e != nil || balance.Available != 0 {
		t.Fatalf("unpaid generation credited merchant %+v %v", balance, e)
	}
	if w = call("PATCH", "/resources/collection-codes/"+generated.Data.CodeID, map[string]any{"title": "changed fixed source"}, csrf); w.Code != 403 {
		t.Fatalf("generic update changed generated config %d", w.Code)
	}
	var cid int64
	if e = pool.QueryRow(ctx, `SELECT code_id FROM collection_code_generations`).Scan(&cid); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Store.CreateCollectionOrder(ctx, cid, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "direct-missing-hash", Name: "本地收款", Type: "wxpay", Amount: 123}); e == nil {
		t.Fatal("store bypassed generation source")
	}
	if _, e = s.Store.CreateCollectionOrder(ctx, cid, payment.CreateOrderInput{CollectionConfigHash: generated.Data.ConfigHash, MerchantID: 1, OutTradeNo: "direct-wrong-amount", Name: "本地收款", Type: "wxpay", Amount: 124}); e == nil {
		t.Fatal("store bypassed fixed amount")
	}
	if _, e = pool.Exec(ctx, `UPDATE backoffice_records SET body=body-'generated' WHERE id=$1`, cid); e != nil {
		t.Fatal(e)
	}
	if w = public("GET", ""); w.Code != 404 {
		t.Fatalf("cleared marker downgraded generated source %d", w.Code)
	}
	report, e := s.Store.CheckLedger(ctx)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(report)
	var checks map[string]int64
	_ = json.Unmarshal(raw, &checks)
	if checks["invalid_collection_generators"] != 1 {
		t.Fatalf("missing collection corruption check %s", raw)
	}
	if _, e = pool.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"generated":true}' WHERE id=$1`, cid); e != nil {
		t.Fatal(e)
	}
	if w = call("PATCH", "/resources/collection-codes/"+generated.Data.CodeID, map[string]any{"enabled": false}, csrf); w.Code != 200 {
		t.Fatalf("disable generated code %d %s", w.Code, w.Body.String())
	}
	body["external_no"] = "generator-once"
	body["origin"] = "http://127.0.0.1:8180"
	if w = call("POST", "/collection-generator", body, csrf); w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"enabled":false`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"url":""`)) {
		t.Fatalf("replay rearmed disabled code %d %s", w.Code, w.Body.String())
	}
	if w = public("GET", ""); w.Code != 404 {
		t.Fatalf("disabled generated code still public %d", w.Code)
	}
	if w = call("DELETE", "/resources/collection-codes/"+generated.Data.CodeID, nil, csrf); w.Code != 200 {
		t.Fatalf("archive generated code %d %s", w.Code, w.Body.String())
	}
	if _, e = s.Store.CollectionGeneration(ctx, cid); e != nil {
		t.Fatalf("archive removed source %v", e)
	}
	report, e = s.Store.CheckLedger(ctx)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ = json.Marshal(report)
	_ = json.Unmarshal(raw, &checks)
	if checks["invalid_collection_generators"] != 0 {
		t.Fatalf("legitimate revocation marked corrupted %s", raw)
	}
}

// F10: the generator prefills with the merchant's default aggregate-code name
// (account/onecodeInfo), falls back to the display name, and PATCH
// (account/editCodename) persists or clears that default.
func TestCollectionGeneratorUXDefaultCodenameF10(t *testing.T) {
	s, _, _ := fixture(t)
	s.CheckoutKey = bytes.Repeat([]byte{7}, 32)
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, AllowLive: false, CallbackBaseURL: "http://127.0.0.1:8180"}
	token, csrf, e := s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant"})
	if e != nil {
		t.Fatal(e)
	}
	call := func(method string, body any, anti string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, "/console/v1/merchant/collection-generator", bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", anti)
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	read := func(w *httptest.ResponseRecorder) map[string]any {
		var env struct {
			Data map[string]any `json:"data"`
		}
		if e := json.Unmarshal(w.Body.Bytes(), &env); e != nil {
			t.Fatalf("decode %s", w.Body.String())
		}
		return env.Data
	}
	// No stored default: prefill is the merchant display name, not customized.
	w := call("GET", nil, csrf)
	if w.Code != 200 {
		t.Fatalf("generator GET %d %s", w.Code, w.Body.String())
	}
	d := read(w)
	if d["recipient_name"] != "legacy" || d["default_codename"] != "legacy" || d["codename_customized"] != false {
		t.Fatalf("default prefill not display name: %v", d)
	}
	// PATCH requires CSRF.
	if w = call("PATCH", map[string]any{"recipient_name": "我的收款"}, "wrong"); w.Code != 403 {
		t.Fatalf("PATCH without CSRF %d", w.Code)
	}
	// PATCH persists a custom default.
	if w = call("PATCH", map[string]any{"recipient_name": "我的收款"}, csrf); w.Code != 200 {
		t.Fatalf("PATCH set %d %s", w.Code, w.Body.String())
	}
	if read(w)["default_codename"] != "我的收款" {
		t.Fatalf("PATCH did not return the saved name: %s", w.Body.String())
	}
	// GET now prefills the stored default and marks it customized.
	d = read(call("GET", nil, csrf))
	if d["recipient_name"] != "我的收款" || d["codename_customized"] != true {
		t.Fatalf("stored default not prefilled: %v", d)
	}
	// Empty PATCH clears it, falling back to the display name.
	if w = call("PATCH", map[string]any{"recipient_name": ""}, csrf); w.Code != 200 || read(w)["default_codename"] != "legacy" {
		t.Fatalf("PATCH clear %d %s", w.Code, w.Body.String())
	}
	d = read(call("GET", nil, csrf))
	if d["recipient_name"] != "legacy" || d["codename_customized"] != false {
		t.Fatalf("cleared default not reverted: %v", d)
	}
	// An over-long name is rejected.
	if w = call("PATCH", map[string]any{"recipient_name": strings.Repeat("名", 101)}, csrf); w.Code != 422 {
		t.Fatalf("over-long codename accepted %d", w.Code)
	}
}

func TestCollectionGeneratorUXConcurrentReplayAndAuditRollback(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	in := payment.CollectionGenerationInput{ExternalNo: "concurrent-generator", RecipientName: "本地测试", StyleID: 0, Origin: "http://127.0.0.1:8180"}
	var wg sync.WaitGroup
	results := make(chan payment.CollectionGeneration, 12)
	failures := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g, e := s.Store.GenerateCollectionCode(ctx, 1, in, in.Origin, "merchant")
			if e != nil {
				failures <- e
			} else {
				results <- g
			}
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for e := range failures {
		t.Fatal(e)
	}
	var id int64
	fresh := 0
	for g := range results {
		if id == 0 {
			id = g.CodeID
		}
		if id != g.CodeID {
			t.Fatal("concurrent duplicate codes")
		}
		if !g.Replayed {
			fresh++
		}
	}
	if fresh != 1 {
		t.Fatalf("fresh generations %d", fresh)
	}
	if _, e := pool.Exec(ctx, `UPDATE collection_code_generations SET fixed_amount_cents=1 WHERE code_id=$1`, id); e == nil {
		t.Fatal("mutable generated configuration")
	}
	if _, e := pool.Exec(ctx, `CREATE FUNCTION reject_generator_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='collection-generator' THEN RAISE EXCEPTION 'synthetic generator audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER test_generator_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION reject_generator_audit()`); e != nil {
		t.Fatal(e)
	}
	in.ExternalNo = "audit-rollback-generator"
	if _, e := s.Store.GenerateCollectionCode(ctx, 1, in, in.Origin, "merchant"); e == nil {
		t.Fatal("ignored audit failure")
	}
	var records, generations int
	if e := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM backoffice_records WHERE resource='collection-codes'),(SELECT count(*) FROM collection_code_generations)`).Scan(&records, &generations); e != nil || records != 1 || generations != 1 {
		t.Fatalf("partial generation after audit failure %d %d %v", records, generations, e)
	}
}
