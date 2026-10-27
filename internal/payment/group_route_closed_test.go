package payment

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestOriginalGroupClosedPaymentTypeIsEnforcedInNewOrderTransactionWithoutFeeSwitch(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 15301, Name: "original closed", APIKey: "local-only"}); e != nil {
		t.Fatal(e)
	}
	original, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 15301, OutTradeNo: "before-closure", Name: "original", Type: "alipay", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	var gid int64
	if e = s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"enabled":true,"info":{"alipay":{"channel":"0","type":"channel","rate":"99"},"wxpay":{"channel":"0","type":"","rate":"0"}}}') RETURNING id`).Scan(&gid); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE merchants SET base_group_id=$1 WHERE id=15301`, gid); e != nil {
		t.Fatal(e)
	}
	for _, kind := range []string{"alipay", "wxpay"} {
		_, e = s.CreateOrder(ctx, CreateOrderInput{MerchantID: 15301, OutTradeNo: "closed-" + kind, Name: "closed", Type: kind, Amount: 100})
		if !errors.Is(e, ErrState) {
			t.Fatal("closed group created a new domain order with fees disabled", kind, e)
		}
	}
	replay, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 15301, OutTradeNo: "before-closure", Name: "original", Type: "alipay", Amount: 100})
	if e != nil || replay.ID != original.ID || replay.Amount != original.Amount || replay.PlatformFee != original.PlatformFee {
		t.Fatal("closing a channel repriced original order", replay, e)
	}
	var orders, journals int
	if e = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM orders),(SELECT count(*) FROM journals)`).Scan(&orders, &journals); e != nil || orders != 1 || journals != 0 {
		t.Fatal(fmt.Sprintf("closed intent left money or extra orders: %d %d", orders, journals), e)
	}
}

func TestOriginalGroupNewOrderCannotRetainExpiredMembershipAfterFeeLockWait(t *testing.T) {
	s := testStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if e := s.CreateMerchant(ctx, Merchant{ID: 15302, Name: "expiry", APIKey: "local-only"}); e != nil {
		t.Fatal(e)
	}
	var gid, setting int64
	if e := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"enabled":true,"rate_bps":100,"info":{"alipay":{"channel":"-5","type":"channel","rate":"99"}}}') RETURNING id`).Scan(&gid); e != nil {
		t.Fatal(e)
	}
	if e := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"payment_fee_enabled","value":"true"}') RETURNING id`).Scan(&setting); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES(15302,$1,clock_timestamp()+interval '2 seconds')`, gid); e != nil {
		t.Fatal(e)
	}
	block, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer block.Rollback(context.Background())
	if _, e = block.Exec(ctx, `SELECT id FROM backoffice_records WHERE id=$1 FOR UPDATE`, setting); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() {
		_, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 15302, OutTradeNo: "expired-fee-wait", Name: "expiry", Type: "alipay", Amount: 10000})
		done <- e
	}()
	for {
		var waiting bool
		if e = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'SELECT body->>%payment_fee_enabled%')`).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		select {
		case early := <-done:
			t.Fatal("fee read did not wait", early)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	if _, e = s.pool.Exec(ctx, `SELECT pg_sleep(GREATEST(0,extract(epoch FROM expires_at-clock_timestamp()))::double precision+0.05) FROM memberships WHERE merchant_id=15302`); e != nil {
		t.Fatal(e)
	}
	if e = block.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		if !errors.Is(e, ErrState) {
			t.Fatal("expired membership committed after fee lock wait", e)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var count int
	if e = s.pool.QueryRow(ctx, `SELECT count(*) FROM orders WHERE merchant_id=15302`).Scan(&count); e != nil || count != 0 {
		t.Fatal("expired fee snapshot persisted", count, e)
	}
}
