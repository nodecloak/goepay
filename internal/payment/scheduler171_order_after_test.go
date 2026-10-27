package payment

import (
	"context"
	"testing"
)

func TestScheduler171KnownOrderAfterCoreCommitsWithOriginalPaidLedgerAndOutbox(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := scheduler171Admin(t, s)
	if e := s.CreateMerchant(ctx, Merchant{ID: 901, Name: "Synthetic order-after", APIKey: "synthetic171-only"}); e != nil {
		t.Fatal(e)
	}
	scheduler171ConfigureManaged(t, s, a, "notify", false)
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 901, OutTradeNo: "scheduler171-atomic-after", Type: "alipay", Amount: 1000, NotifyURL: "http://127.0.0.1/synthetic171"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `CREATE FUNCTION scheduler171_after_fault()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.kind='payment.notify' THEN RAISE EXCEPTION 'synthetic original outbox fault';END IF;RETURN NEW;END$$;CREATE TRIGGER scheduler171_after_fault BEFORE INSERT ON jobs FOR EACH ROW EXECUTE FUNCTION scheduler171_after_fault()`); e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, o.TradeNo, "scheduler171-original-receipt"); e == nil {
		t.Fatal("partial paid state committed despite missing original local after-action")
	}
	var state string
	var journal, outbox, inbox int
	if e = s.pool.QueryRow(ctx, `SELECT status,(SELECT count(*)FROM journals),(SELECT count(*)FROM outbox),(SELECT count(*)FROM inbox)FROM orders WHERE id=$1`, o.ID).Scan(&state, &journal, &outbox, &inbox); e != nil || state != "created" || journal != 0 || outbox != 0 || inbox != 0 {
		t.Fatal("atomic core rollback", state, journal, outbox, inbox, e)
	}
	if _, e = s.pool.Exec(ctx, `DROP TRIGGER scheduler171_after_fault ON jobs`); e != nil {
		t.Fatal(e)
	}
	for range 2 {
		if e = s.MarkPaid(ctx, o.TradeNo, "scheduler171-original-receipt"); e != nil {
			t.Fatal(e)
		}
	}
	b, e := s.Balance(ctx, 901)
	if e != nil || b.Available != 1000 {
		t.Fatal("original receipt replay changed money", b, e)
	}
	var notifications int
	if e = s.pool.QueryRow(ctx, `SELECT(SELECT count(*)FROM outbox WHERE event_key=$1),(SELECT count(*)FROM jobs WHERE event_key=$2)`, "payment:"+o.TradeNo, "notify:"+o.TradeNo).Scan(&outbox, &notifications); e != nil || outbox != 1 || notifications != 1 {
		t.Fatal("disabled cron removed independent first notification or duplicated atomic source", outbox, notifications, e)
	}
}
