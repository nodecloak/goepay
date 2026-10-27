package jobs

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestScheduler171UnconfiguredLegacyClaimStillCompletesOriginalJob(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()
	if _, e := q.Pool.Exec(ctx, `INSERT INTO jobs(event_key,kind,payload)VALUES('scheduler171-legacy','notify','{}')`); e != nil {
		t.Fatal(e)
	}
	j, e := q.Claim(ctx, []string{"notify"}, time.Second)
	if e != nil || j.Attempts != 1 || j.EventKey != "scheduler171-legacy" {
		t.Fatal(j.ID, j.Attempts, e)
	}
	if e = q.Complete(ctx, j); e != nil {
		t.Fatal(e)
	}
	if _, e = q.Claim(ctx, []string{"notify"}, time.Second); !IsEmpty(e) {
		t.Fatal("legacy terminal job was rearmed", e)
	}
}

func TestScheduler171LegacyClaimLastWritePastLeaseCannotConsumeAttempt(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()
	if _, e := q.Pool.Exec(ctx, `INSERT INTO jobs(event_key,kind,payload)VALUES('scheduler171-legacy-delayed','notify','{}');CREATE FUNCTION scheduler171_legacy_claim_delay()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.state='running' THEN PERFORM pg_sleep(1.2);END IF;RETURN NEW;END$$;CREATE TRIGGER scheduler171_legacy_claim_delay BEFORE UPDATE ON jobs FOR EACH ROW EXECUTE FUNCTION scheduler171_legacy_claim_delay()`); e != nil {
		t.Fatal(e)
	}
	j, e := q.Claim(ctx, []string{"notify"}, time.Second)
	if !errors.Is(e, ErrLeaseLost) || j.ID != 0 {
		t.Errorf("legacy expired claim committed: returned_id=%d err=%v", j.ID, e)
	}
	var state string
	var attempts int
	var free bool
	if e = q.Pool.QueryRow(ctx, `SELECT state,attempts,lease_token IS NULL AND lease_until IS NULL FROM jobs WHERE event_key='scheduler171-legacy-delayed'`).Scan(&state, &attempts, &free); e != nil || state != "pending" || attempts != 0 || !free {
		t.Fatal("expired legacy claim burned original attempt", state, attempts, free, e)
	}
}
