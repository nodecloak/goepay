package jobs

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type NotificationRetryPolicy interface {
	ObserveNotificationFailure(context.Context, pgx.Tx, Job, string, time.Duration) (time.Duration, bool, error)
}

// RetryNotificationFailure is used only after the original notification handler
// proves an actual delivery failure bound to this job and lease. It must not be
// used for an admission, configuration, cancellation or financial proof error.
func (q Queue) RetryNotificationFailure(ctx context.Context, j Job, kind string, delay time.Duration) error {
	if j.Kind != "payment.notify" || (kind != "ack" && kind != "transport" && kind != "read") {
		return errors.New("invalid notification failure")
	}
	hook, ok := q.Policy.(NotificationRetryPolicy)
	if !ok {
		return q.Retry(ctx, j, "notification delivery failed", delay)
	}
	if delay < 0 {
		return errors.New("negative delay")
	}
	tx, e := q.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var actual Job
	var until time.Time
	e = tx.QueryRow(ctx, `SELECT id,kind,event_key,payload,attempts,lease_token,lease_until FROM jobs WHERE id=$1 AND state='running' AND lease_token=$2 AND lease_until>clock_timestamp() FOR UPDATE`, j.ID, j.Token).Scan(&actual.ID, &actual.Kind, &actual.EventKey, &actual.Payload, &actual.Attempts, &actual.Token, &until)
	if errors.Is(e, pgx.ErrNoRows) {
		return ErrLeaseLost
	}
	if e != nil {
		return e
	}
	if actual.Kind != j.Kind || actual.EventKey != j.EventKey || actual.Attempts != j.Attempts || string(actual.Payload) != string(j.Payload) {
		return ErrLeaseLost
	}
	delay, stop, e := hook.ObserveNotificationFailure(ctx, tx, actual, kind, delay)
	if e != nil {
		return e
	}
	if delay < 0 || delay > 24*time.Hour {
		return errors.New("invalid notification retry delay")
	}
	tag, e := tx.Exec(ctx, `UPDATE jobs SET state=CASE WHEN attempts>=max_attempts OR $5::boolean THEN 'dead' ELSE 'pending' END,run_at=clock_timestamp()+$3::bigint*interval '1 millisecond',last_error=$4,lease_until=NULL,lease_token=NULL WHERE id=$1 AND state='running' AND lease_token=$2 AND lease_until>clock_timestamp()`, j.ID, j.Token, delay.Milliseconds(), "notification delivery failed", stop)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	var live bool
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()<$1::timestamptz`, until).Scan(&live); e != nil {
		return e
	}
	if !live {
		return ErrLeaseLost
	}
	return tx.Commit(ctx)
}
