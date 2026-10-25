package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	"goepay/internal/backoffice"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestOrderBlacklistUXUsesOriginalSourceExpiryAndStableRequest(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "blacklist-source", Name: "本地测试", Type: "alipay", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.SetOrderClientIP(ctx, o.ID, "192.0.2.55"); e != nil {
		t.Fatal(e)
	}
	token, csrf, e := s.Sessions.Create(identity.User{Username: "admin", Role: "admin", ID: 1, AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	call := func(body any, anti string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", "/console/v1/admin/orders/"+o.TradeNo+"/blacklist", bytes.NewReader(raw))
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", anti)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	body := map[string]any{"kind": "ip", "expected_value": "192.0.2.55", "days": 1, "reason": "本地风险核对", "external_no": "blacklist-once"}
	if w := call(body, ""); w.Code != 403 {
		t.Fatalf("CSRF %d", w.Code)
	}
	w := call(body, csrf)
	if w.Code != 201 {
		t.Fatalf("blacklist order action unavailable %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Data struct {
			ID        string `json:"id"`
			ExpiresAt string `json:"expires_at"`
			Replayed  bool   `json:"replayed"`
		}
	}
	if e = json.Unmarshal(w.Body.Bytes(), &out); e != nil || out.Data.ID == "" {
		t.Fatalf("blacklist result %s %v", w.Body.String(), e)
	}
	expiry, e := time.Parse(time.RFC3339, out.Data.ExpiresAt)
	if e != nil || time.Until(expiry) < 23*time.Hour {
		t.Fatalf("missing original days expiry %s %v", out.Data.ExpiresAt, e)
	}
	svc := backoffice.New(pool, s.Store)
	if e = svc.CheckPayment(ctx, backoffice.PaymentContext{MerchantID: 1, ClientIP: "192.0.2.55"}); e == nil {
		t.Fatal("new blacklist did not block payment policy")
	}
	if w = call(body, csrf); w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"replayed":true`)) {
		t.Fatalf("replay %d %s", w.Code, w.Body.String())
	}
	var wg sync.WaitGroup
	codes := make(chan int, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); codes <- call(body, csrf).Code }()
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != 200 {
			t.Fatalf("concurrent original blacklist replay %d", code)
		}
	}
	for _, p := range []backoffice.Principal{{Role: "merchant", SubjectID: 1}, {Role: "admin", SubjectID: 1, AdminClass: "unknown"}} {
		if _, e = svc.AddOrderBlacklist(ctx, p, o.ID, "ip", "192.0.2.55", 0, "", "forbidden-original"); e != backoffice.ErrForbidden {
			t.Fatalf("non administrator blacklist %v", e)
		}
	}
	body["days"] = 2
	if w = call(body, csrf); w.Code != 409 {
		t.Fatalf("same request changed expiry %d", w.Code)
	}
	body["days"] = 1
	body["external_no"] = "foreign-blacklist-source"
	body["expected_value"] = "192.0.2.66"
	if w = call(body, csrf); w.Code != 409 {
		t.Fatalf("changed order source %d", w.Code)
	}
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	if _, e = pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{expires_at}',to_jsonb($2::text)) WHERE id=$1::bigint`, out.Data.ID, past); e != nil {
		t.Fatal(e)
	}
	if e = svc.CheckPayment(ctx, backoffice.PaymentContext{MerchantID: 1, ClientIP: "192.0.2.55"}); e != nil {
		t.Fatalf("expired blacklist remains effective %v", e)
	}
	body["external_no"] = "blacklist-once"
	body["expected_value"] = "192.0.2.55"
	if w = call(body, csrf); w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(past)) {
		t.Fatalf("replay extended original expiry %d %s", w.Code, w.Body.String())
	}
	if _, e = pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{expires_at}','"malformed"'::jsonb) WHERE id=$1::bigint`, out.Data.ID); e != nil {
		t.Fatal(e)
	}
	if e = svc.CheckPayment(ctx, backoffice.PaymentContext{MerchantID: 1, ClientIP: "192.0.2.55"}); e == nil {
		t.Fatal("malformed expiry weakened enabled risk rule")
	}
	if _, e = pool.Exec(ctx, `CREATE FUNCTION reject_order_blacklist_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='order-blacklist' THEN RAISE EXCEPTION 'synthetic blacklist audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER test_order_blacklist_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION reject_order_blacklist_audit()`); e != nil {
		t.Fatal(e)
	}
	body["external_no"] = "blacklist-audit-rollback"
	if w = call(body, csrf); w.Code != 500 {
		t.Fatalf("blacklist audit failure %d %s", w.Code, w.Body.String())
	}
	var records, audits int
	if e = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM backoffice_records WHERE resource='blacklist'),(SELECT count(*) FROM backoffice_audit WHERE resource='order-blacklist' AND operation='create')`).Scan(&records, &audits); e != nil || records != 1 || audits != 1 {
		t.Fatalf("duplicate blacklist %d %d %v", records, audits, e)
	}
}
