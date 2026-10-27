package payment

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestScheduler171LeaseExpiryDuringLastResultWriteRollsBackCompletion(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := scheduler171Admin(t, s)
	scheduler171Configure(t, s, a, "referral_daily")
	scheduler171Pending(t, s, a, "referral_daily")
	conn, e := s.pool.Acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Release()
	r, claimed, e := s.claimScheduledRun(ctx, conn, "referral_daily", 300*time.Millisecond)
	if e != nil || !claimed {
		t.Fatal(e)
	}
	counts, e := s.executeScheduledTask(ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `CREATE SEQUENCE scheduler171_last_write;CREATE FUNCTION scheduler171_last_write_delay()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN PERFORM nextval('scheduler171_last_write');PERFORM pg_sleep(0.5);RETURN NEW;END$$;CREATE TRIGGER scheduler171_last_write_delay BEFORE INSERT ON scheduled_task_results FOR EACH ROW EXECUTE FUNCTION scheduler171_last_write_delay()`); e != nil {
		t.Fatal(e)
	}
	if e = s.completeScheduledLease(ctx, r, counts, nil); !errors.Is(e, ErrConflict) {
		t.Errorf("expired owner committed after final waiting result write: %v", e)
	}
	var results int
	var state string
	var entered, expired bool
	if e = s.pool.QueryRow(ctx, `SELECT(SELECT count(*) FROM scheduled_task_results),(SELECT state FROM scheduled_task_runs WHERE id=$1),(SELECT is_called FROM scheduler171_last_write),clock_timestamp()>$2::timestamptz`, r.id, r.until).Scan(&results, &state, &entered, &expired); e != nil {
		t.Fatal(e)
	}
	if !entered || !expired || results != 0 || state != "running" {
		t.Fatal("last-write gate was not reached or did not roll back", entered, expired, results, state)
	}
}
