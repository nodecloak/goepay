package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"strings"
	"sync/atomic"
	"testing"
)

type buyerBatchSourceCounter104 struct{ n atomic.Int32 }

func (c *buyerBatchSourceCounter104) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if strings.Contains(d.SQL, "FROM order_customer_charges104") || strings.Contains(d.SQL, "LEFT JOIN order_customer_charges104") {
		c.n.Add(1)
	}
	return ctx
}
func (*buyerBatchSourceCounter104) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}
func TestBuyerCharge104BatchPrepareAuthenticatesOneJoinedSourceRead(t *testing.T) {
	f := buyer104MerchantLocal(t)
	state := channel168Data(t, f.buyer("GET", "", ""))
	body, _ := json.Marshal(map[string]any{"mode": 1, "mode_feerate": "", "expected_fingerprint": state["fingerprint"]})
	channel168Data(t, f.buyer("PATCH", "", string(body)))
	global := channel168Data(t, f.buyerMinimum104("GET", "global", ""))
	body, _ = json.Marshal(map[string]any{"payfee_lessthan": "0.01", "payfee_mincost": "0.06", "expected_fingerprint": global["fingerprint"]})
	channel168Data(t, f.buyerMinimum104("PATCH", "global", string(body)))
	items := []payment.OrderBusinessBatchSelection{}
	for i := 0; i < 4; i++ {
		o, e := f.s.Store.CreateOrder(t.Context(), payment.CreateOrderInput{MerchantID: 1, OutTradeNo: fmt.Sprint("batch-C104-", i), Amount: 100})
		if e != nil {
			t.Fatal(e)
		}
		if e = f.s.Store.MarkPaid(t.Context(), o.TradeNo, fmt.Sprint("synthetic-batch-C104-", i)); e != nil {
			t.Fatal(e)
		}
		items = append(items, payment.OrderBusinessBatchSelection{TradeNo: o.TradeNo})
	}
	counter := &buyerBatchSourceCounter104{}
	cfg := f.p.Config()
	cfg.ConnConfig.Tracer = counter
	p, e := pgxpool.NewWithConfig(t.Context(), cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	s, e := payment.NewWithKey(p, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	actor := payment.OrderBusinessBatchActor{User: identity.User{ID: 168, Role: "admin", Username: "admin168", AdminClass: "super", AuthVersion: 1}, Token: f.token}
	for _, n := range []int{1, 4} {
		counter.n.Store(0)
		batch, e := s.PrepareOrderBusinessBatch(t.Context(), actor, payment.OrderBusinessBatchInput{ExternalNo: fmt.Sprint("batch-C104-read-", n), Action: "refund", Reason: "synthetic joined source read", Items: items[:n]})
		if e != nil {
			t.Fatal(e)
		}
		if count := counter.n.Load(); count != 1 {
			t.Fatalf("%d selected orders used %d customer charge source reads; need one joined read", n, count)
		}
		selection, e := s.OrderBusinessBatchFundsSelection(t.Context(), p, batch.ID)
		if e != nil || len(selection.Items) != n {
			t.Fatal(selection, e)
		}
		for _, item := range selection.Items {
			if item.Amount != 106 {
				t.Fatal("batch source lost authenticated actual C", item.Amount)
			}
		}
	}
}
