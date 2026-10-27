package server

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	adapter "goepay/internal/adapters/payment"
)

func TestOrderBatch185ActualQueryExecutionUsesOriginalAccountAndReturnsDurableResult(t *testing.T) {
	f, factory, row := subchannel168BProductLocal(t)
	if out := f.pay(t, "query-batch185", "1.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	o, e := f.s.Store.Order(context.Background(), 1, "query-batch185")
	if e != nil {
		t.Fatal(e)
	}
	v := batch185Prepared(t, f, o.TradeNo, "execute-query185")
	id := v["id"].(string)
	subchannel168BRotate(t, f, row)
	for i := 0; i < 2; i++ {
		w := batch185Request(f, "POST", "/"+id+"/execute", `{"limit":1}`)
		if w.Code != 200 {
			t.Fatal("original pending batch query HTTP", i, w.Code, w.Body.String())
		}
		var out struct {
			Data struct {
				State string `json:"state"`
				Items []struct {
					State string `json:"state"`
				} `json:"items"`
			} `json:"data"`
		}
		if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Data.State != "completed" || len(out.Data.Items) != 1 || out.Data.Items[0].State != "succeeded" {
			t.Fatal("batch result is not actual completed query", w.Body.String())
		}
	}
	after, e := f.s.Store.Order(context.Background(), 1, o.OutTradeNo)
	if e != nil || after.Status != "paid" {
		t.Fatal(after.Status, e)
	}
	if factory.providers["products-v1"].queries.Load() != 1 || factory.providers["products-v2"].queries.Load() != 0 {
		t.Fatal("batch retry queried current child or duplicated finished original", factory.providers["products-v1"].queries.Load(), factory.providers["products-v2"].queries.Load())
	}
	if w := batch185Request(f, "GET", "/"+id, ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, e = f.s.Store.CheckLedger(context.Background()); e != nil {
		t.Fatal(e)
	}
}

type batch185BlockingQuery struct {
	*subchannel168BProducts
	entered chan struct{}
	resume  chan struct{}
	once    sync.Once
	reads   atomic.Int64
}

func (p *batch185BlockingQuery) PaymentProductIdentity() string {
	product, _ := adapter.PaymentProductScope(p.subchannel168BProducts)
	return product
}

func (p *batch185BlockingQuery) Query(ctx context.Context, r adapter.QueryRequest) (adapter.QueryResult, error) {
	p.reads.Add(1)
	p.once.Do(func() { close(p.entered) })
	select {
	case <-p.resume:
	case <-ctx.Done():
		return adapter.QueryResult{}, ctx.Err()
	}
	return p.subchannel168BProducts.Query(ctx, r)
}
func TestOrderBatch185ConcurrentExecuteDoesNotDispatchSameOriginalItemTwice(t *testing.T) {
	f, factory, _ := subchannel168BProductLocal(t)
	if out := f.pay(t, "query-concurrent185", "1.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	o, e := f.s.Store.Order(context.Background(), 1, "query-concurrent185")
	if e != nil {
		t.Fatal(e)
	}
	v := batch185Prepared(t, f, o.TradeNo, "concurrent-query185")
	p := &batch185BlockingQuery{subchannel168BProducts: factory.providers["products-v1"], entered: make(chan struct{}), resume: make(chan struct{})}
	// The actual child factory must restore the same original execution object;
	// wrap only that synthetic object's query hook through the factory below.
	f.s.ProviderFlow.SubchannelFactories[7] = &batch185QueryFactory{subchannel168BProductFactory: factory, wrapped: p}
	done := make(chan int, 1)
	go func() { done <- batch185Request(f, "POST", "/"+v["id"].(string)+"/execute", `{"limit":1}`).Code }()
	select {
	case <-p.entered:
	case code := <-done:
		t.Fatalf("original query did not reach controlled provider: HTTP %d", code)
	case <-time.After(5 * time.Second):
		t.Fatal("original query did not reach controlled provider in deadline")
	}
	second := make(chan int, 1)
	go func() { second <- batch185Request(f, "POST", "/"+v["id"].(string)+"/execute", `{"limit":1}`).Code }()
	code := 0
	select {
	case code = <-second:
	case <-time.After(300 * time.Millisecond):
	}
	close(p.resume)
	if code == 0 {
		code = <-second
	}
	if code != 409 {
		t.Fatal("concurrent executor did not see original in-flight item", code)
	}
	if code := <-done; code != 200 {
		t.Fatal(code)
	}
	if p.reads.Load() != 1 {
		t.Fatal("duplicate original query dispatch", p.reads.Load())
	}
}

type batch185QueryFactory struct {
	*subchannel168BProductFactory
	wrapped *batch185BlockingQuery
}

func TestOrderBatch185CancelOnlyUnsentSelectionAndNeverQueryProvider(t *testing.T) {
	f, factory, _ := subchannel168BProductLocal(t)
	if out := f.pay(t, "cancel185", "1.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	o, e := f.s.Store.Order(context.Background(), 1, "cancel185")
	if e != nil {
		t.Fatal(e)
	}
	v := batch185Prepared(t, f, o.TradeNo, "cancel-local185")
	body, _ := json.Marshal(map[string]string{"expected_fingerprint": v["fingerprint"].(string)})
	w := batch185Request(f, "POST", "/"+v["id"].(string)+"/cancel", string(body))
	if w.Code != 200 {
		t.Fatal("cancel unsent batch", w.Code, w.Body.String())
	}
	var out struct {
		Data struct {
			State string `json:"state"`
			Items []struct {
				State string `json:"state"`
			} `json:"items"`
		} `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Data.State != "cancelled" || out.Data.Items[0].State != "cancelled" {
		t.Fatal(w.Body.String())
	}
	w = batch185Request(f, "POST", "/"+v["id"].(string)+"/execute", `{"limit":1}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if factory.providers["products-v1"].queries.Load() != 0 {
		t.Fatal("cancelled unsent item reached provider")
	}
}

func (f *batch185QueryFactory) Restore(raw []byte) (adapter.SubchannelExecution, error) {
	v, e := f.subchannel168BProductFactory.Restore(raw)
	if app, ok := v.Payment.(adapter.ApplicationIdentity); e == nil && ok && app.ApplicationID() == "products-v1" {
		v.Payment = f.wrapped
	}
	return v, e
}
