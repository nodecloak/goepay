package payment

import (
	"errors"
	"testing"
)

func TestBuyerCharge104T1BlocksActualGrossAndRejectsLostPrivateSource(t *testing.T) {
	s := testStore(t)
	const mid = 10432
	feeMerchant(t, s, mid, 600, "platform")
	buyer104PolicyFixture(t, s, "merchant", mid, 1, nil, nil)
	tx, e := s.pool.Begin(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	if e = post(t.Context(), tx, "synthetic104-opening", "synthetic opening balance", 0, "clearing", mid, "available", 1000); e != nil {
		tx.Rollback(t.Context())
		t.Fatal(e)
	}
	if e = tx.Commit(t.Context()); e != nil {
		t.Fatal(e)
	}
	o, e := s.CreateOrder(t.Context(), CreateOrderInput{MerchantID: mid, OutTradeNo: "t1-C104", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(t.Context(), o.TradeNo, "synthetic-t1-C104"); e != nil {
		t.Fatal(e)
	}
	transferSetting(t, s, "settle_type", "1")
	v, e := s.FundsAvailability(t.Context(), mid)
	if e != nil || v.LedgerAvailable != 1100 || v.TodayGross != 106 || v.Blocked != 106 || v.Available != 994 {
		t.Fatal("T1 did not block authenticated actual C", v, e)
	}
	if _, e = s.ReserveTransfer(t.Context(), mid, "above-C104-available", 995); !errors.Is(e, ErrInsufficient) {
		t.Fatal("outgoing reservation used original M", e)
	}
	if _, e = s.pool.Exec(t.Context(), `ALTER TABLE order_customer_charges104 DISABLE TRIGGER USER;DELETE FROM order_customer_charges104;ALTER TABLE order_customer_charges104 ENABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.FundsAvailability(t.Context(), mid); !errors.Is(e, ErrConflict) {
		t.Fatal("missing source returned spendable funds", e)
	}
	if _, e = s.ReserveTransfer(t.Context(), mid, "lost-C104-source", 1); !errors.Is(e, ErrConflict) {
		t.Fatal("missing source allowed outgoing reserve", e)
	}
}
