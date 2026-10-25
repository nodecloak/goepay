package payment

import (
	"context"
	"encoding/json"
	"testing"

	notice "goepay/internal/merchantnotice"
)

func TestMerchantNotice80PaidBirthFreezesOriginalSettingsAndNeverBackfills(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if err := s.CreateMerchant(ctx, Merchant{ID: 80001, Name: "notice-source-owner", APIKey: "local-only80"}); err != nil {
		t.Fatal(err)
	}
	notifications, err := notice.NewStore(s.pool, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	actor := notice.Actor{Role: "merchant", ID: 80001, Name: "notice-source-owner"}
	pay := func(no string) Order {
		t.Helper()
		o, err := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 80001, OutTradeNo: no, Name: "<safe>商品80", Type: "wxpay", Amount: 1200})
		if err != nil {
			t.Fatal(err)
		}
		if _, owner, err := s.BeginProviderAttempt(ctx, o.TradeNo, 7, "native"); err != nil || !owner {
			t.Fatal(owner, err)
		}
		if err := s.markPaid(ctx, o.TradeNo, "event-"+no, false, PaidReceipt{ChannelID: 7, ProviderTradeNo: "provider-" + no}); err != nil {
			t.Fatal(err)
		}
		return o
	}
	old := pay("before-profile")
	if _, err = s.pool.Exec(ctx, `UPDATE merchants SET profile=profile||'{"email":"owner80@example.test"}'::jsonb WHERE id=80001`); err != nil {
		t.Fatal(err)
	}
	prefs, err := notifications.SaveSettings(ctx, 80001, 0, notice.SettingsWrite{Preferences: notice.Preferences{Order: 2}}, actor)
	if err != nil {
		t.Fatal(err)
	}
	fresh := pay("after-profile")
	for _, o := range []Order{old, fresh} {
		if err = s.markPaid(ctx, o.TradeNo, "replay", false, PaidReceipt{ChannelID: 7, ProviderTradeNo: "provider-" + o.OutTradeNo}); err != nil {
			t.Fatal(err)
		}
	}
	var events, queued int
	if err = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM merchant_notice_events),(SELECT count(*) FROM jobs WHERE kind='merchant.notice')`).Scan(&events, &queued); err != nil || events != 2 || queued != 1 {
		t.Fatalf("source missing, duplicate or historical backfill: events%d queued%d %v", events, queued, err)
	}
	var originalProfile, freshProfile int64
	if err = s.pool.QueryRow(ctx, `SELECT COALESCE(profile_id,0) FROM merchant_notice_events WHERE order_id=$1`, old.ID).Scan(&originalProfile); err != nil || originalProfile != 0 {
		t.Fatal("historical notification adopted current preference", originalProfile, err)
	}
	if err = s.pool.QueryRow(ctx, `SELECT COALESCE(profile_id,0) FROM merchant_notice_events WHERE order_id=$1`, fresh.ID).Scan(&freshProfile); err != nil || freshProfile != prefs.ID {
		t.Fatal("new notification lost original preference", freshProfile, err)
	}
	var cipher []byte
	if err = s.pool.QueryRow(ctx, `SELECT payload_cipher FROM merchant_notice_events WHERE order_id=$1`, fresh.ID).Scan(&cipher); err != nil || json.Valid(cipher) {
		t.Fatal("notice content not encrypted", err)
	}
}

func TestMerchantNotice80OptionalProjectionFailureCannotUndoVerifiedPayment(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if err := s.CreateMerchant(ctx, Merchant{ID: 80002, Name: "notice-local", APIKey: "local-only80"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `CREATE FUNCTION reject_notice80_projection() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic notice store unavailable'; END $$;CREATE TRIGGER notice80_projection_failure BEFORE INSERT ON merchant_notice_events FOR EACH ROW EXECUTE FUNCTION reject_notice80_projection()`); err != nil {
		t.Fatal(err)
	}
	o, err := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 80002, OutTradeNo: "independent", Name: "notice80", Type: "alipay", Amount: 800})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.BeginProviderAttempt(ctx, o.TradeNo, 7, "native"); err != nil {
		t.Fatal(err)
	}
	if err = s.markPaid(ctx, o.TradeNo, "verified-independent", false, PaidReceipt{ChannelID: 7, ProviderTradeNo: "local-verified80"}); err != nil {
		t.Fatal(err)
	}
	balance, err := s.Balance(ctx, 80002)
	if err != nil || balance.Available != 800 {
		t.Fatal("optional notice rolled back money", balance, err)
	}
	var receipts int
	if err = s.pool.QueryRow(ctx, `SELECT count(*) FROM verified_payment_receipts WHERE order_id=$1`, o.ID).Scan(&receipts); err != nil || receipts != 1 {
		t.Fatal("verified receipt rolled back", receipts, err)
	}
}
