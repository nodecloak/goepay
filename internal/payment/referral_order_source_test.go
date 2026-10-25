package payment

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestReferral71OrderSourceBirthImmutableAndCopiedHashRejected(t *testing.T) {
	s, o, _ := profitOrder71(t, "immediate")
	ctx := context.Background()
	for _, sql := range []string{`UPDATE orders SET referral_source_version=0,referral_source_hash=NULL WHERE id=$1`, `UPDATE orders SET referral_source_hash=decode(repeat('00',32),'hex') WHERE id=$1`} {
		if _, e := s.pool.Exec(ctx, sql, o.ID); e == nil {
			t.Fatal("original birth anchor mutation accepted", sql)
		}
	}
	next, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: o.MerchantID, OutTradeNo: "anchor-copy-target", Name: "synthetic", Type: o.Type, Amount: o.Amount, PaymentCost: profitQuote71(t, "5")})
	if e != nil {
		t.Fatal(e)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "SET LOCAL session_replication_role=replica"); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `UPDATE orders SET referral_source_hash=(SELECT referral_source_hash FROM orders WHERE id=$1) WHERE id=$2`, o.ID, next.ID); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.BeginProviderAttemptWithAccount(ctx, next.TradeNo, 7, "native", "profit71-app", "", profitQuote71(t, "5").estimate.AccountScope); !errors.Is(e, ErrConflict) {
		t.Fatal("copied parent hash accepted", e)
	}
	report, e := s.CheckLedger(ctx)
	if e != nil || report.InvalidReferralTerms == 0 {
		t.Fatal("copied hash escaped checker", report, e)
	}
}

func TestReferral71UpgradeDoesNotBackfillHistoricalOrdersOrPermitLateAnchor(t *testing.T) {
	s := membership72HistoricalStore(t, 70)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 7191, Name: "synthetic original", APIKey: "original-local-71"}); e != nil {
		t.Fatal(e)
	}
	o, e := scanOrder(s.pool.QueryRow(ctx, `INSERT INTO orders(trade_no,merchant_id,out_trade_no,name,type,amount,merchant_amount) VALUES(nextval('trade_no_seq')::text,7191,'before71','historical','wxpay',100,100) RETURNING `+orderColumns))
	if e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile("../../db/migrations/000071_referral_profit_costs.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, string(raw)); e != nil {
		t.Fatal(e)
	}
	var version int
	var hash []byte
	if e = s.pool.QueryRow(ctx, "SELECT referral_source_version,referral_source_hash FROM orders WHERE id=$1", o.ID).Scan(&version, &hash); e != nil || version != 0 || hash != nil {
		t.Fatal("historical order backfilled", version, hash != nil, e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE orders SET referral_source_version=3,referral_source_hash=decode(repeat('00',32),'hex') WHERE id=$1`, o.ID); e == nil {
		t.Fatal("late historical anchor allowed")
	}
	referralPayTestOrder(t, s, o)
	if _, e = s.ReferralRefundPreview(ctx, s.pool, o.ID, "before71-refund", 1); e != nil {
		t.Fatal("historical refund preview", e)
	}
	r, e := s.ReserveRefund(ctx, o.MerchantID, o.OutTradeNo, "before71-refund", 1)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ResolveRefund(ctx, r.ID, "failed"); e != nil {
		t.Fatal(e)
	}
	var n int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM referral_order_terms WHERE order_id=$1", o.ID).Scan(&n); e != nil || n != 0 {
		t.Fatal("historical acquired current terms", n, e)
	}
}
