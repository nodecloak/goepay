package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestOrderBatchControlsAtomicRevisionReplayAndFinancialFacts(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	orders := []payment.Order{}
	for _, no := range []string{"batch-control-one", "batch-control-two"} {
		o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: no, Name: "本地测试", Type: "alipay", Amount: 100})
		if e != nil {
			t.Fatal(e)
		}
		orders = append(orders, o)
	}
	token, csrf, e := s.Sessions.Create(identity.User{Username: "admin", Role: "admin", ID: 1, AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	call := func(body any, anti string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", "/console/v1/admin/orders/batch-control", bytes.NewReader(raw))
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", anti)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	items := []map[string]string{{"trade_no": orders[0].TradeNo, "expected_revision": "0"}, {"trade_no": orders[1].TradeNo, "expected_revision": "0"}}
	body := map[string]any{"external_no": "batch-control-request", "action": "mark-paid", "reason": "本地批量后台标记", "items": items}
	if w := call(body, ""); w.Code != 403 {
		t.Fatalf("CSRF %d", w.Code)
	}
	w := call(body, csrf)
	if w.Code != 200 {
		t.Fatalf("batch controls unavailable %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Data struct {
			Items []struct {
				TradeNo string `json:"trade_no"`
				Control struct {
					Revision string `json:"revision"`
				} `json:"control"`
			} `json:"items"`
			Replayed bool `json:"replayed"`
		}
	}
	if e = json.Unmarshal(w.Body.Bytes(), &out); e != nil || len(out.Data.Items) != 2 {
		t.Fatalf("batch result %s %v", w.Body.String(), e)
	}
	if w = call(body, csrf); w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"replayed":true`)) {
		t.Fatalf("replay %d %s", w.Code, w.Body.String())
	}
	var wg sync.WaitGroup
	statuses := make(chan int, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); statuses <- call(body, csrf).Code }()
	}
	wg.Wait()
	close(statuses)
	for code := range statuses {
		if code != 200 {
			t.Fatalf("concurrent replay %d", code)
		}
	}
	var batches int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE resource='order-batches' AND operation='control'`).Scan(&batches); e != nil || batches != 1 {
		t.Fatalf("duplicate batch audit %d %v", batches, e)
	}
	body["reason"] = "changed"
	if w = call(body, csrf); w.Code != 409 {
		t.Fatalf("changed request accepted %d", w.Code)
	}
	body["external_no"] = "batch-control-stale"
	body["action"] = "archive"
	items[0]["expected_revision"] = out.Data.Items[0].Control.Revision
	if w = call(body, csrf); w.Code != 409 {
		t.Fatalf("stale second revision accepted %d %s", w.Code, w.Body.String())
	}
	var audits int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE resource='orders' AND operation='console-order-control'`).Scan(&audits); e != nil || audits != 2 {
		t.Fatalf("partially applied stale batch %d %v", audits, e)
	}
	for _, o := range orders {
		current, e := s.Store.OrderByTradeNo(ctx, o.TradeNo)
		if e != nil || current.Status != o.Status {
			t.Fatalf("marker changed payment fact %+v %v", current, e)
		}
	}
	b, e := s.Store.Balance(ctx, 1)
	if e != nil || b.Available != 0 || b.Reserved != 0 {
		t.Fatalf("batch created money %+v %v", b, e)
	}
	body["external_no"] = "batch-control-selection-change"
	body["items"] = []map[string]string{items[0], items[0]}
	if w = call(body, csrf); w.Code != 422 {
		t.Fatalf("duplicate selected order %d", w.Code)
	}
	for i := range items {
		items[i]["expected_revision"] = out.Data.Items[i].Control.Revision
	}
	body["items"] = items
	body["external_no"] = "batch-audit-rollback"
	if _, e = pool.Exec(ctx, `CREATE FUNCTION reject_order_batch_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='order-batches' THEN RAISE EXCEPTION 'synthetic batch audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER test_order_batch_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION reject_order_batch_audit()`); e != nil {
		t.Fatal(e)
	}
	if w = call(body, csrf); w.Code != 500 {
		t.Fatalf("batch audit failure %d %s", w.Code, w.Body.String())
	}
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE resource='orders' AND operation='console-order-control'`).Scan(&audits); e != nil || audits != 2 {
		t.Fatalf("partial batch after final audit failure %d %v", audits, e)
	}
}
