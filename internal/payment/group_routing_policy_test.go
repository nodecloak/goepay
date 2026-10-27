package payment

import (
	"context"
	"goepay/internal/grouppolicy"
	"testing"
	"time"
)

func TestGroupRoutingPolicyUsesEffectiveGroupAndRejectsDisabledOrUnresolved(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 15201, Name: "routing", APIKey: "local-test-only"}); e != nil {
		t.Fatal(e)
	}
	var base, vip int64
	if e := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"enabled":true,"info":{"alipay":{"channel":"7","type":"channel","rate":"99"}}}') RETURNING id`).Scan(&base); e != nil {
		t.Fatal(e)
	}
	if e := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"enabled":true,"info":{"alipay":{"channel":"8","type":"channel"}}}') RETURNING id`).Scan(&vip); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `UPDATE merchants SET base_group_id=$1 WHERE id=15201`, base); e != nil {
		t.Fatal(e)
	}
	check := func(wantGroup, wantChannel int64, wantErr bool) {
		t.Helper()
		tx, e := s.pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback(ctx)
		got, e := s.GroupPaymentRoutingTx(ctx, tx, 15201, "alipay")
		if wantErr {
			if e == nil {
				t.Fatal("invalid group accepted")
			}
			return
		}
		if e != nil || got.GroupID != wantGroup || got.Choice.Kind() != grouppolicy.Channel || got.Choice.ID() != wantChannel {
			t.Fatal(got, e)
		}
	}
	check(base, 7, false)
	if _, e := s.pool.Exec(ctx, `INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES(15201,$1,clock_timestamp()+interval '1 day')`, vip); e != nil {
		t.Fatal(e)
	}
	check(vip, 8, false)
	if _, e := s.pool.Exec(ctx, `UPDATE memberships SET expires_at=clock_timestamp()-interval '1 second' WHERE merchant_id=15201`); e != nil {
		t.Fatal(e)
	}
	check(base, 7, false)
	if _, e := s.pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{enabled}','false') WHERE id=$1`, base); e != nil {
		t.Fatal(e)
	}
	check(0, 0, true)
	if _, e := s.pool.Exec(ctx, `UPDATE merchants SET group_baseline_unresolved=true WHERE id=15201`); e != nil {
		t.Fatal(e)
	}
	check(0, 0, true)
}

func TestGroupRoutingPolicyRechecksMembershipExpiryAfterActualGroupRowWait(t *testing.T) {
	s := testStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if e := s.CreateMerchant(ctx, Merchant{ID: 15202, Name: "routing expiry", APIKey: "local-only"}); e != nil {
		t.Fatal(e)
	}
	var base, vip int64
	for i, id := range []*int64{&base, &vip} {
		channel := "7"
		if i == 1 {
			channel = "8"
		}
		if e := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups',jsonb_build_object('enabled',true,'info',jsonb_build_object('alipay',jsonb_build_object('channel',$1::text,'type','channel')))) RETURNING id`, channel).Scan(id); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := s.pool.Exec(ctx, `UPDATE merchants SET base_group_id=$1 WHERE id=15202`, base); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES(15202,$1,clock_timestamp()+interval '2 seconds')`, vip); e != nil {
		t.Fatal(e)
	}
	block, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer block.Rollback(context.Background())
	var locked int64
	if e = block.QueryRow(ctx, `SELECT id FROM backoffice_records WHERE id=$1 FOR UPDATE`, vip).Scan(&locked); e != nil {
		t.Fatal(e)
	}
	type result struct {
		policy GroupPaymentRouting
		err    error
	}
	done := make(chan result, 1)
	pids := make(chan int, 1)
	go func() {
		tx, e := s.pool.Begin(ctx)
		if e != nil {
			done <- result{err: e}
			return
		}
		defer tx.Rollback(context.Background())
		var pid int
		if e = tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
			done <- result{err: e}
			return
		}
		pids <- pid
		policy, e := s.GroupPaymentRoutingTx(ctx, tx, 15202, "alipay")
		done <- result{policy, e}
	}()
	var pid int
	select {
	case pid = <-pids:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	for {
		var waiting bool
		if e = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event_type='Lock')`, pid).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		select {
		case early := <-done:
			t.Fatal("routing did not wait for the locked group", early)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	if _, e = s.pool.Exec(ctx, `SELECT pg_sleep(GREATEST(0,extract(epoch FROM expires_at-clock_timestamp()))::double precision+0.05) FROM memberships WHERE merchant_id=15202`); e != nil {
		t.Fatal(e)
	}
	if e = block.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case got := <-done:
		if got.err != nil || got.policy.GroupID != base || got.policy.Choice.ID() != 7 {
			t.Fatal("expired membership retained after group lock wait", got)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
