package payment

import (
	"context"
	"errors"
	"testing"
)

func TestReferralOrderPurposeIsAtomicAndCannotBeChangedByReplay(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	feeMerchant(t, s, 1801, 1000, "platform")
	in := CreateOrderInput{MerchantID: 1801, OutTradeNo: "ordinary", Amount: 100, Name: "item"}
	o, err := s.CreateOrder(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	var purpose, reason string
	var enabled bool
	if err = s.pool.QueryRow(ctx, "SELECT purpose,reason,enabled FROM referral_order_terms WHERE order_id=$1", o.ID).Scan(&purpose, &reason, &enabled); err != nil || purpose != "payment" || reason != "no_relation" || enabled {
		t.Fatal("new order lost negative birth snapshot", purpose, reason, enabled, err)
	}
	if _, err = s.CreateRechargeOrder(ctx, in, "local-purpose"); !errors.Is(err, ErrConflict) {
		t.Fatal("ordinary intent reclassified as recharge", err)
	}
	in.OutTradeNo = "recharge-original"
	r, err := s.CreateRechargeOrder(ctx, in, "local-purpose")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.pool.QueryRow(ctx, "SELECT t.purpose,t.reason,t.enabled FROM referral_order_terms t JOIN recharge_intents i ON i.order_id=t.order_id WHERE t.order_id=$1", r.ID).Scan(&purpose, &reason, &enabled); err != nil || purpose != "recharge" || reason != "recharge" || enabled {
		t.Fatal(purpose, reason, enabled, err)
	}
	if _, err = s.CreateOrder(ctx, in); !errors.Is(err, ErrConflict) {
		t.Fatal("public payment replay adopted private recharge", err)
	}
	same, err := s.CreateRechargeOrder(ctx, in, "local-purpose")
	if err != nil || same.ID != r.ID {
		t.Fatal(same, err)
	}
	if _, err = s.pool.Exec(ctx, "INSERT INTO recharge_intents(order_id,created_by) VALUES($1,'late-purpose')", o.ID); err == nil {
		t.Fatal("late recharge changed sealed original purpose")
	}
	if _, err = s.pool.Exec(ctx, "UPDATE referral_order_terms SET reason='disabled' WHERE order_id=$1", o.ID); err == nil {
		t.Fatal("immutable negative terms changed")
	}
	if _, err = s.pool.Exec(ctx, `CREATE FUNCTION reject_referral_birth_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'forced birth failure'; END $$; CREATE TRIGGER reject_referral_birth_test BEFORE INSERT ON referral_order_terms FOR EACH ROW EXECUTE FUNCTION reject_referral_birth_test()`); err != nil {
		t.Fatal(err)
	}
	in.OutTradeNo = "atomic-recharge-failure"
	if _, err = s.CreateRechargeOrder(ctx, in, "local-purpose"); err == nil {
		t.Fatal("forced birth fault accepted")
	}
	var count int
	if err = s.pool.QueryRow(ctx, "SELECT count(*) FROM orders WHERE merchant_id=$1 AND out_trade_no=$2", in.MerchantID, in.OutTradeNo).Scan(&count); err != nil || count != 0 {
		t.Fatal("order survived recharge snapshot rollback", count, err)
	}
}
