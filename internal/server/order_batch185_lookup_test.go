package server

import (
	"context"
	"encoding/json"
	"testing"
)

func TestOrderBatch185LostPrepareResponseCanLookupSameRequestWithoutAnotherPOST(t *testing.T) {
	f, factory, _ := subchannel168BProductLocal(t)
	if out := f.pay(t, "lookup185", "1.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	o, e := f.s.Store.Order(context.Background(), 1, "lookup185")
	if e != nil {
		t.Fatal(e)
	}
	v := batch185Prepared(t, f, o.TradeNo, "lookup-stable185")
	w := batch185Request(f, "GET", "?external_no=lookup-stable185", "")
	if w.Code != 200 {
		t.Fatal("lost POST response has no original request lookup", w.Code, w.Body.String())
	}
	var out struct{ Data map[string]any }
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Data["id"] != v["id"] {
		t.Fatal(w.Body.String())
	}
	if factory.providers["products-v1"].queries.Load() != 0 {
		t.Fatal("lookup sent business")
	}
}
