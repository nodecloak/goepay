package payment

import (
	"errors"
	"testing"
)

func TestBuyerCharge190EarlySchemaCannotHideModernSourceLoss(t *testing.T) {
	s := testStore(t)
	for _, tc := range []struct {
		name     string
		ddl      string
		order    Order
		legacy   bool
		conflict bool
	}{
		{name: "pre-referral", legacy: true},
		{name: "registered57", ddl: `CREATE TABLE schema_migrations(name text); INSERT INTO schema_migrations VALUES('000057_referral_rewards.up.sql')`, conflict: true},
		{name: "registered71", ddl: `CREATE TABLE schema_migrations(name text); INSERT INTO schema_migrations VALUES('000071_referral_profit_costs.up.sql')`, conflict: true},
		{name: "registered104", ddl: `CREATE TABLE schema_migrations(name text); INSERT INTO schema_migrations VALUES('000104_order_customer_charges.up.sql')`, conflict: true},
		{name: "terms-remain", ddl: `CREATE TABLE referral_order_terms(id bigint)`},
		{name: "costs-remain", ddl: `CREATE TABLE referral_profit_costs(id bigint)`},
		{name: "origin-version-remains", ddl: `ALTER TABLE orders ADD COLUMN referral_source_version integer`},
		{name: "origin-hash-remains", ddl: `ALTER TABLE orders ADD COLUMN referral_source_hash bytea`},
		{name: "charge-table-remains", ddl: `CREATE TABLE order_customer_charges104(id bigint)`},
		{name: "new-birth-marker", order: Order{actualChargeRequired104: true}},
		{name: "route-birth-marker", order: Order{routeFeeRequired: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := s.pool.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(t.Context())
			if _, err = tx.Exec(t.Context(), `CREATE SCHEMA early_charge190; SET LOCAL search_path=early_charge190; CREATE TABLE orders(id bigint)`); err != nil {
				t.Fatal(err)
			}
			if tc.ddl != "" {
				if _, err = tx.Exec(t.Context(), tc.ddl); err != nil {
					t.Fatal(err)
				}
			}
			got, err := s.beforeReferralChargeSchema190(t.Context(), tx, tc.order)
			if got != tc.legacy || errors.Is(err, ErrConflict) != tc.conflict || (err != nil && !tc.conflict) {
				t.Fatalf("legacy=%v err=%v; want legacy=%v conflict=%v", got, err, tc.legacy, tc.conflict)
			}
		})
	}
}

func TestBuyerCharge190BirthTimestampTamperingCannotSettle(t *testing.T) {
	s := testStore(t)
	feeMerchant(t, s, 19001, 600, "platform")
	o, err := s.CreateOrder(t.Context(), CreateOrderInput{MerchantID: 19001, OutTradeNo: "birth-time190", Amount: 100})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(t.Context(), `ALTER TABLE orders DISABLE TRIGGER USER; UPDATE orders SET created_at=created_at-interval '1 day'`); err != nil {
		t.Fatal(err)
	}
	o, err = s.OrderByTradeNo(t.Context(), o.TradeNo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AuthenticatedOrderCharge104(t.Context(), o); !errors.Is(err, ErrConflict) {
		t.Fatal("tampered birth accepted", err)
	}
	if err = s.MarkPaid(t.Context(), o.TradeNo, "synthetic190"); !errors.Is(err, ErrConflict) {
		t.Fatal("tampered birth settled", err)
	}
	var journals int
	if err = s.pool.QueryRow(t.Context(), `SELECT count(*) FROM journals`).Scan(&journals); err != nil || journals != 0 {
		t.Fatal("tampered birth wrote money", journals, err)
	}
}
