package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOrderBatch185NotificationSuccessfulAfterPreparationIsNotResent(t *testing.T) {
	f, _, _ := subchannel168BProductLocal(t)
	ctx := context.Background()
	if out := subchannel168BProductPay(t, f, "notify185-done", "native", map[string]string{"notify_url": "https://synthetic.example.invalid/notify"}); out["code"] != float64(1) {
		t.Fatal(out)
	}
	o, e := f.s.Store.Order(ctx, 1, "notify185-done")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProviderFlow.Query(ctx, 1, o.TradeNo); e != nil {
		t.Fatal(e)
	}
	read := func(want bool) {
		t.Helper()
		r := httptest.NewRequest("GET", "/console/v1/admin/orders", nil)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		var v struct {
			Data struct{ Items []map[string]any }
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil || len(v.Data.Items) != 1 {
			t.Fatal(w.Code, w.Body.String())
		}
		if got, ok := v.Data.Items[0]["can_batch_notify"].(bool); !ok || got != want {
			t.Fatal("actual list notification capability", v.Data.Items[0])
		}
	}
	read(true)
	body := strings.Replace(batch185Input(o.TradeNo, "notify185-completed-later"), `"action":"query"`, `"action":"notify"`, 1)
	w := batch185Request(f, "POST", "", body)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var prepared struct{ Data struct{ ID string } }
	if json.Unmarshal(w.Body.Bytes(), &prepared) != nil {
		t.Fatal(w.Body.String())
	}
	if _, e = f.p.Exec(ctx, `UPDATE jobs SET state='succeeded' WHERE kind='payment.notify' AND payload->>'trade_no'=$1`, o.TradeNo); e != nil {
		t.Fatal(e)
	}
	read(false)
	w = batch185Request(f, "POST", "/"+prepared.Data.ID+"/execute", `{"limit":1}`)
	var result struct {
		Data struct {
			State string
			Items []struct{ State, Code string }
		}
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Data.State != "completed" || len(result.Data.Items) != 1 || result.Data.Items[0].State != "succeeded" || result.Data.Items[0].Code != "original_notification_delivered" {
		t.Fatal("already delivered item was queued again", w.Code, w.Body.String())
	}
	var jobs, requests int
	if e = f.p.QueryRow(ctx, `SELECT (SELECT count(*) FROM jobs WHERE kind='payment.notify' AND payload->>'trade_no'=$1),(SELECT count(*) FROM notification_resend_requests WHERE order_id=$2)`, o.TradeNo, o.ID).Scan(&jobs, &requests); e != nil || jobs != 1 || requests != 0 {
		t.Fatal(jobs, requests, e)
	}
	w = batch185Request(f, "POST", "", strings.Replace(body, "notify185-completed-later", "notify185-already-done", 1))
	if w.Code != 409 {
		t.Fatal("successful notification accepted as original non-success selection", w.Code, w.Body.String())
	}
}

func TestOrderBatch185NotificationsCoalesceOriginalJobAndNeverSendDuringRequest(t *testing.T) {
	f, factory, _ := subchannel168BProductLocal(t)
	if out := subchannel168BProductPay(t, f, "notify185", "native", map[string]string{"notify_url": "https://synthetic.example.invalid/notify"}); out["code"] != float64(1) {
		t.Fatal(out)
	}
	o, e := f.s.Store.Order(context.Background(), 1, "notify185")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProviderFlow.Query(context.Background(), 1, o.TradeNo); e != nil {
		t.Fatal(e)
	}
	body := strings.Replace(batch185Input(o.TradeNo, "notify-local185"), `"action":"query"`, `"action":"notify"`, 1)
	w := batch185Request(f, "POST", "", body)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var v struct{ Data map[string]any }
	if json.Unmarshal(w.Body.Bytes(), &v) != nil {
		t.Fatal(w.Body.String())
	}
	id := v.Data["id"].(string)
	for i := 0; i < 2; i++ {
		w = batch185Request(f, "POST", "/"+id+"/execute", `{"limit":1}`)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var out struct {
			Data struct {
				State string `json:"state"`
				Items []struct {
					State string `json:"state"`
				} `json:"items"`
			} `json:"data"`
		}
		if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Data.State != "completed" || out.Data.Items[0].State != "queued" {
			t.Fatal("queued does not mean delivered", w.Body.String())
		}
	}
	var jobs, requests int
	if e = f.p.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM jobs WHERE kind='payment.notify' AND payload->>'trade_no'=$1),(SELECT count(*) FROM notification_resend_requests WHERE order_id=$2)`, o.TradeNo, o.ID).Scan(&jobs, &requests); e != nil || jobs != 1 || requests != 1 {
		t.Fatal("batch created duplicate notification intent", jobs, requests, e)
	}
	if factory.providers["products-v1"].queries.Load() != 1 || factory.providers["products-v1"].refunds.Load() != 0 {
		t.Fatal("notification used unrelated upstream business")
	}
}
