package payment

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"sync/atomic"
	"testing"
)

type buyerQueryCounter104 struct{ selects atomic.Int32 }

func (c *buyerQueryCounter104) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(strings.TrimSpace(d.SQL), "SELECT") {
		c.selects.Add(1)
	}
	return ctx
}
func (c *buyerQueryCounter104) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestBuyerCharge104BoundedPageAuthenticatesSourcesWithoutPerOrderQueries(t *testing.T) {
	s := testStore(t)
	feeMerchant(t, s, 10415, 600, "platform")
	buyer104PolicyFixture(t, s, "merchant", 10415, 1, nil, nil)
	ids := []int64{}
	for i := 0; i < 12; i++ {
		o, e := s.CreateOrder(t.Context(), CreateOrderInput{MerchantID: 10415, OutTradeNo: fmt.Sprint("page104-", i), Amount: 100})
		if e != nil {
			t.Fatal(e)
		}
		ids = append(ids, o.ID)
	}
	counter := &buyerQueryCounter104{}
	cfg := s.pool.Config()
	cfg.ConnConfig.Tracer = counter
	p, e := pgxpool.NewWithConfig(t.Context(), cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	reader, e := NewWithKey(p, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	values, e := reader.OriginalOrderCharges104(t.Context(), 10415, ids)
	if e != nil || len(values) != 12 {
		t.Fatal("actual page unavailable", values, e)
	}
	if n := counter.selects.Load(); n != 2 {
		t.Fatal("charge page performed per-order SELECTs", n)
	}
	for _, v := range values {
		if v != 106 {
			t.Fatal("page substituted original M", v)
		}
	}
	if _, e = s.pool.Exec(t.Context(), `ALTER TABLE order_customer_charges104 DISABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(t.Context(), `DELETE FROM order_customer_charges104 WHERE order_id=$1`, ids[0]); e != nil {
		t.Fatal(e)
	}
	values, e = reader.OriginalOrderCharges104(t.Context(), 10415, ids)
	if e != nil || len(values) != 11 {
		t.Fatal("one missing source contaminated page or silently fell back", len(values), e)
	}
	if _, ok := values[ids[0]]; ok {
		t.Fatal("lost source substituted original M")
	}
	values, e = reader.OriginalOrderCharges104(t.Context(), 10416, ids)
	if e != nil || len(values) != 0 {
		t.Fatal("charge projection crossed merchant owner", values, e)
	}
}
