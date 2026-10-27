package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func batch185Request(f channel168Fixture, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/console/v1/admin/orders/business-batches"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}
func batch185Input(trade, no string) string {
	raw, _ := json.Marshal(map[string]any{"external_no": no, "action": "query", "reason": "原批次安全边界本地验证", "items": []map[string]string{{"trade_no": trade}}})
	return string(raw)
}
func batch185Prepared(t *testing.T, f channel168Fixture, trade, no string) map[string]any {
	t.Helper()
	w := batch185Request(f, "POST", "", batch185Input(trade, no))
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var v struct{ Data map[string]any }
	if json.Unmarshal(w.Body.Bytes(), &v) != nil {
		t.Fatal(w.Body.String())
	}
	return v.Data
}

func TestOrderBatch185ActualReadReplayExactLargeIDAndAlteredRequestConflict(t *testing.T) {
	f, factory, _ := subchannel168BProductLocal(t)
	if out := f.pay(t, "replay185", "1.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	o, e := f.s.Store.Order(context.Background(), 1, "replay185")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.p.Exec(context.Background(), `SELECT setval(pg_get_serial_sequence('order_business_batches','id'),9007199254740993,false)`); e != nil {
		t.Fatal(e)
	}
	v := batch185Prepared(t, f, o.TradeNo, "exact-replay185")
	if v["id"] != "9007199254740993" {
		t.Fatal("batch ID rounded", v)
	}
	for _, step := range []struct{ method, path, body string }{{"GET", "/9007199254740993", ""}, {"POST", "", batch185Input(o.TradeNo, "exact-replay185")}} {
		w := batch185Request(f, step.method, step.path, step.body)
		if w.Code != 200 {
			t.Fatal(step.method, w.Code, w.Body.String())
		}
	}
	changed := strings.Replace(batch185Input(o.TradeNo, "exact-replay185"), "query", "notify", 1)
	if w := batch185Request(f, "POST", "", changed); w.Code != 409 {
		t.Fatal("same number adopted different request", w.Code, w.Body.String())
	}
	var rows int
	if e = f.p.QueryRow(context.Background(), `SELECT count(*) FROM order_business_batches`).Scan(&rows); e != nil || rows != 1 {
		t.Fatal(rows, e)
	}
	if factory.providers["products-v1"].queries.Load() != 0 {
		t.Fatal("GET/replay queried provider")
	}
}

func TestOrderBatch185LostPreparedRowRetainsIndependentAnchorAndCannotRecreate(t *testing.T) {
	f, _, _ := subchannel168BProductLocal(t)
	if out := f.pay(t, "lost185", "1.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	o, e := f.s.Store.Order(context.Background(), 1, "lost185")
	if e != nil {
		t.Fatal(e)
	}
	v := batch185Prepared(t, f, o.TradeNo, "lost-batch185")
	id := v["id"].(string)
	if _, e = f.p.Exec(context.Background(), `ALTER TABLE order_business_batch_items DISABLE TRIGGER USER;ALTER TABLE order_business_batches DISABLE TRIGGER USER;DELETE FROM order_business_batch_items;DELETE FROM order_business_batches;ALTER TABLE order_business_batches ENABLE TRIGGER USER;ALTER TABLE order_business_batch_items ENABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	if w := batch185Request(f, "GET", "/"+id, ""); w.Code != 409 {
		t.Fatal("known preparation source loss looked like a new unknown ID", w.Code, w.Body.String())
	}
	if w := batch185Request(f, "POST", "", batch185Input(o.TradeNo, "lost-batch185")); w.Code != 409 {
		t.Fatal("lost preparation silently recreated", w.Code, w.Body.String())
	}
}

func TestOrderBatch185LatestAuditWriteRechecksOriginalSessionClock(t *testing.T) {
	f, factory, _ := subchannel168BProductLocal(t)
	if out := f.pay(t, "clock185", "1.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	ctx := context.Background()
	o, e := f.s.Store.Order(ctx, 1, "clock185")
	if e != nil {
		t.Fatal(e)
	}
	base := time.Now().UTC().Truncate(time.Microsecond)
	f = channel168CClock(t, f, base)
	if _, e = f.p.Exec(ctx, `UPDATE console_sessions SET expires_at=$1 WHERE role='admin'`, base.Add(24*time.Hour)); e != nil {
		t.Fatal(e)
	}
	if _, e = f.p.Exec(ctx, `CREATE FUNCTION expire_batch185_actor() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.resource='order-business-batches' AND NEW.operation='prepare' THEN UPDATE channel168c_test_clock SET instant=instant+interval '48 hours';END IF;RETURN NEW;END$$;CREATE TRIGGER expire_batch185_actor AFTER INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION expire_batch185_actor()`); e != nil {
		t.Fatal(e)
	}
	w := batch185Request(f, "POST", "", batch185Input(o.TradeNo, "clock-batch185"))
	if w.Code != 403 {
		t.Fatal("last write crossed original administrator expiry", w.Code, w.Body.String())
	}
	var batches, items, audits int
	if e = f.p.QueryRow(ctx, `SELECT (SELECT count(*) FROM order_business_batches),(SELECT count(*) FROM order_business_batch_items),(SELECT count(*) FROM backoffice_audit WHERE resource='order-business-batches')`).Scan(&batches, &items, &audits); e != nil || batches+items+audits != 0 {
		t.Fatal("expired prepare partially committed", batches, items, audits, e)
	}
	if factory.providers["products-v1"].queries.Load() != 0 {
		t.Fatal("expired preparation queried upstream")
	}
}

func TestOrderBatch185StrictSelectionRejectsAmbiguityWithoutNewRows(t *testing.T) {
	f, _, _ := subchannel168BProductLocal(t)
	if out := f.pay(t, "strict185", "1.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	o, e := f.s.Store.Order(context.Background(), 1, "strict185")
	if e != nil {
		t.Fatal(e)
	}
	valid := batch185Input(o.TradeNo, "strict-batch185")
	if w := batch185Request(f, "POST", "", strings.Replace(valid, `"action":"query"`, `"action":"confirm-settlement"`, 1)); w.Code != 422 {
		t.Fatal("unsupported settlement capability produced an unusable batch", w.Code, w.Body.String())
	}
	for i, body := range []string{strings.Replace(valid, `"action":"query"`, `"action":"query","action":"notify"`, 1), strings.Replace(valid, `"action":"query"`, `"action":"query","ignored":true`, 1), fmt.Sprintf(`{"external_no":"duplicate185","action":"query","reason":"valid","items":[{"trade_no":%q},{"trade_no":%q}]}`, o.TradeNo, o.TradeNo), `{"external_no":"empty185","action":"query","reason":"valid","items":[]}`} {
		w := batch185Request(f, "POST", "", body)
		if w.Code != 400 && w.Code != 422 {
			t.Fatal(i, w.Code, w.Body.String())
		}
	}
	var rows int
	if e = f.p.QueryRow(context.Background(), `SELECT count(*) FROM order_business_batches`).Scan(&rows); e != nil || rows != 0 {
		t.Fatal(rows, e)
	}
}
