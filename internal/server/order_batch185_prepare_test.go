package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOrderBatch185ActualHTTPPreparationFreezesOriginalSelectionWithoutUpstreamIO(t *testing.T) {
	f, factory, _ := subchannel168BProductLocal(t)
	if out := f.pay(t, "batch185-original", "1.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	o, err := f.s.Store.Order(context.Background(), 1, "batch185-original")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"external_no": "batch185-local-preparation", "action": "query", "reason": "本地原批量补单验证", "items": []map[string]string{{"trade_no": o.TradeNo}}})
	r := httptest.NewRequest("POST", "/console/v1/admin/orders/business-batches", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("original business batch preparation HTTP=%d want201 body=%s", w.Code, w.Body.String())
	}
	var result struct {
		Code int `json:"code"`
		Data struct {
			ID     string `json:"id"`
			State  string `json:"state"`
			Action string `json:"action"`
			Items  []struct {
				TradeNo string `json:"trade_no"`
				State   string `json:"state"`
			} `json:"items"`
		} `json:"data"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Code != 0 || result.Data.ID == "" || result.Data.State != "prepared" || result.Data.Action != "query" || len(result.Data.Items) != 1 || result.Data.Items[0].TradeNo != o.TradeNo || result.Data.Items[0].State != "unsent" {
		t.Fatal("batch did not retain original selected intent", w.Body.String(), err)
	}
	p := factory.providers["products-v1"]
	if p.creates.Load() != 1 || p.queries.Load() != 0 || p.refunds.Load() != 0 {
		t.Fatal("preparation performed upstream business", p.creates.Load(), p.queries.Load(), p.refunds.Load())
	}
	var rows int
	if err = f.p.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM refunds)+(SELECT count(*) FROM provider_operations)`).Scan(&rows); err != nil || rows != 0 {
		t.Fatal("read/preparation reserved or claimed funds", rows, err)
	}
}
