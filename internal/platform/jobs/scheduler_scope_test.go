package jobs

import (
	"context"
	"testing"
	"time"
)

func TestClaimExhaustionCleanupIsLimitedToRequestedKinds(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()
	if _, err := q.Pool.Exec(ctx, `INSERT INTO jobs(event_key,kind,payload,state,attempts,max_attempts,lease_until,lease_token) VALUES('scheduler-exhausted','settlement-plan.generate','{}','running',12,12,now()-interval '1 minute','old-scheduler'),('notification-exhausted','payment.notify','{}','running',12,12,now()-interval '1 minute','old-notification')`); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Claim(ctx, []string{"settlement-plan.generate"}, time.Minute); !IsEmpty(err) {
		t.Fatal("exhausted job unexpectedly claimable", err)
	}
	var own, other string
	if err := q.Pool.QueryRow(ctx, `SELECT (SELECT state FROM jobs WHERE event_key='scheduler-exhausted'),(SELECT state FROM jobs WHERE event_key='notification-exhausted')`).Scan(&own, &other); err != nil || own != "dead" || other != "running" {
		t.Fatal("claim cleaned another worker kind", own, other, err)
	}
}
