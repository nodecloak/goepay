package payment

import (
	"errors"
	"testing"
)

func TestBuyerCharge104LostMigrationRegistrationCannotBecomeLegacy(t *testing.T) {
	s := testStore(t)
	feeMerchant(t, s, 10435, 50, "platform")
	o, e := s.CreateOrder(t.Context(), CreateOrderInput{MerchantID: 10435, OutTradeNo: "registration104", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(t.Context(), `CREATE TABLE schema_migrations(name text PRIMARY KEY,sha256 text);INSERT INTO schema_migrations VALUES('000104_order_customer_charges.up.sql','synthetic-test-only')`); e != nil {
		t.Fatal(e)
	}
	if c, e := s.AuthenticatedOrderCharge104(t.Context(), o); e != nil || c != 100 {
		t.Fatal(c, e)
	}
	if _, e = s.pool.Exec(t.Context(), `DELETE FROM schema_migrations WHERE name='000104_order_customer_charges.up.sql'`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.AuthenticatedOrderCharge104(t.Context(), o); !errors.Is(e, ErrConflict) {
		t.Fatal("lost migration registration used remaining columns as legacy", e)
	}
	if _, e = s.CreateOrder(t.Context(), CreateOrderInput{MerchantID: 10435, OutTradeNo: "registration104", Amount: 100}); !errors.Is(e, ErrConflict) {
		t.Fatal("old replay ignored lost registration", e)
	}
	if _, e = s.CreateOrder(t.Context(), CreateOrderInput{MerchantID: 10435, OutTradeNo: "new-after-registration-loss104", Amount: 100}); !errors.Is(e, ErrConflict) {
		t.Fatal("new birth downgraded after registration loss", e)
	}
}
