package payment

import (
	"errors"
	"goepay/internal/identity"
	"testing"
)

func buyer104PolicyFixture(t *testing.T, s *Store, kind string, owner int64, mode int, threshold, cost *int64) {
	t.Helper()
	tx, e := s.pool.Begin(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(t.Context())
	old, e := s.buyerPolicyTx104(t.Context(), tx, kind, owner, true)
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.writeBuyerPolicy104(t.Context(), tx, old, buyerPolicyFacts104{Kind: kind, OwnerID: owner, MerchantMode: mode, Threshold: threshold, Cost: cost}, MerchantRSAActor{User: identity.User{ID: owner, Role: "merchant", Username: "synthetic-source104"}})
	if e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(t.Context()); e != nil {
		t.Fatal(e)
	}
}

func TestBuyerCharge104BirthFreezesAuthenticatedActualAndDefaultPolicy(t *testing.T) {
	s := testStore(t)
	feeMerchant(t, s, 10401, 600, "platform")
	in := CreateOrderInput{MerchantID: 10401, OutTradeNo: "default-policy104", Amount: 100, Name: "Synthetic goods"}
	old, e := s.CreateOrder(t.Context(), in)
	if e != nil {
		t.Fatal(e)
	}
	buyer104PolicyFixture(t, s, "merchant", 10401, 1, nil, nil)
	in.OutTradeNo = "full-buyer104"
	o, e := s.CreateOrder(t.Context(), in)
	if e != nil {
		t.Fatal(e)
	}
	c, e := s.AuthenticatedOrderCharge104(t.Context(), o)
	if e != nil || c != 106 || o.Amount != 100 || o.MerchantAmount != 100 || o.PlatformFee != 6 || o.FeePolicy != "buyer_additive" {
		t.Fatalf("new original/actual/net/fee: %+v C=%d %v", o, c, e)
	}
	buyer104PolicyFixture(t, s, "merchant", 10401, 0, nil, nil)
	again, e := s.CreateOrder(t.Context(), in)
	if e != nil {
		t.Fatal(e)
	}
	if c, e = s.AuthenticatedOrderCharge104(t.Context(), again); e != nil || c != 106 {
		t.Fatal("replay repriced existing order", c, e)
	}
	if c, e = s.AuthenticatedOrderCharge104(t.Context(), old); e != nil || c != 100 {
		t.Fatal("default order repriced", c, e)
	}
	var oldColumn *int64
	var oldSources int
	if e = s.pool.QueryRow(t.Context(), `SELECT actual_charge_amount104,(SELECT count(*) FROM order_customer_charges104 WHERE order_id=o.id) FROM orders o WHERE id=$1`, old.ID).Scan(&oldColumn, &oldSources); e != nil || oldColumn == nil || *oldColumn != 100 || oldSources != 1 {
		t.Fatal("unconfigured new birth omitted authenticated source", oldColumn, oldSources, e)
	}
}

func TestBuyerCharge104ActualSourceLossCannotDowngrade(t *testing.T) {
	s := testStore(t)
	feeMerchant(t, s, 10402, 600, "platform")
	buyer104PolicyFixture(t, s, "merchant", 10402, 1, nil, nil)
	o, e := s.CreateOrder(t.Context(), CreateOrderInput{MerchantID: 10402, OutTradeNo: "source-loss104", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	// Simulate privileged database corruption. The application never disables guards.
	if _, e = s.pool.Exec(t.Context(), `ALTER TABLE order_customer_charges104 DISABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(t.Context(), `DELETE FROM order_customer_charges104 WHERE order_id=$1`, o.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.AuthenticatedOrderCharge104(t.Context(), o); !errors.Is(e, ErrConflict) {
		t.Fatal("missing source accepted", e)
	}
	if _, e = s.CreateOrder(t.Context(), CreateOrderInput{MerchantID: 10402, OutTradeNo: "source-loss104", Amount: 100}); !errors.Is(e, ErrConflict) {
		t.Fatal("idempotent create returned an unconfirmed price", e)
	}
	if _, e = s.pool.Exec(t.Context(), `ALTER TABLE orders DISABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(t.Context(), `UPDATE orders SET actual_charge_amount104=NULL,actual_charge_required104=false,merchant_amount=94,platform_fee=6,fee_policy='merchant_deduct' WHERE id=$1`, o.ID); e != nil {
		t.Fatal(e)
	}
	o, e = s.OrderByTradeNo(t.Context(), o.TradeNo)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.AuthenticatedOrderCharge104(t.Context(), o); !errors.Is(e, ErrConflict) {
		t.Fatal("source and marker loss forged legacy", e)
	}
}

func TestBuyerCharge104LedgerAndRefundUseActualNotOriginal(t *testing.T) {
	s := testStore(t)
	feeMerchant(t, s, 10403, 600, "platform")
	buyer104PolicyFixture(t, s, "merchant", 10403, 1, nil, nil)
	o, e := s.CreateOrder(t.Context(), CreateOrderInput{MerchantID: 10403, OutTradeNo: "refund-actual104", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(t.Context(), o.TradeNo, "synthetic-only104"); e != nil {
		t.Fatal(e)
	}
	var clearing, fees, available int64
	if e = s.pool.QueryRow(t.Context(), `SELECT (SELECT balance FROM accounts WHERE owner_id=0 AND kind='clearing'),(SELECT balance FROM accounts WHERE owner_id=0 AND kind='fees'),(SELECT balance FROM accounts WHERE owner_id=10403 AND kind='available')`).Scan(&clearing, &fees, &available); e != nil || clearing != -106 || fees != 6 || available != 100 {
		t.Fatal("actual double-entry gross", clearing, fees, available, e)
	}
	first, e := s.ReserveRefund(t.Context(), 10403, o.OutTradeNo, "partial-original104", 100)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ResolveRefund(t.Context(), first.ID, "succeeded"); e != nil {
		t.Fatal(e)
	}
	last, e := s.ReserveRefund(t.Context(), 10403, o.OutTradeNo, "remaining-buyerfee104", 6)
	if e != nil || last.MerchantAmount != 0 || last.FeeAmount != 6 {
		t.Fatal("R==M incorrectly treated as fully refunded", last, e)
	}
	if e = s.ResolveRefund(t.Context(), last.ID, "succeeded"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReserveRefund(t.Context(), 10403, o.OutTradeNo, "over-actual104", 1); !errors.Is(e, ErrInvalid) {
		t.Fatal("actual refund cap bypassed", e)
	}
}

func TestBuyerCharge104MissingSourceCannotSettle(t *testing.T) {
	s := testStore(t)
	feeMerchant(t, s, 10404, 600, "platform")
	buyer104PolicyFixture(t, s, "merchant", 10404, 1, nil, nil)
	o, e := s.CreateOrder(t.Context(), CreateOrderInput{MerchantID: 10404, OutTradeNo: "settle-missing-source104", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(t.Context(), `ALTER TABLE order_customer_charges104 DISABLE TRIGGER USER; DELETE FROM order_customer_charges104`); e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(t.Context(), o.TradeNo, "forbidden-missing104"); !errors.Is(e, ErrConflict) {
		t.Fatal("settled unverified actual amount", e)
	}
	var journals int
	if e = s.pool.QueryRow(t.Context(), `SELECT count(*) FROM journals`).Scan(&journals); e != nil || journals != 0 {
		t.Fatal("failed source check wrote money", journals, e)
	}
}
