package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type Job struct {
	ID       int64
	Kind     string
	Payload  json.RawMessage
	Attempts int
	Token    string
}
type Queue struct{ Pool *pgxpool.Pool }

var ErrLeaseLost = errors.New("job lease lost")

func (q Queue) Claim(ctx context.Context, kinds []string, lease time.Duration) (Job, error) {
	return q.claim(ctx, kinds, lease, nil)
}

// ClaimProviderChannels skips reconciliation jobs belonging to other channels.
// Channel routing is read from immutable attempts, never trusted from payloads.
// Notification deliveries remain eligible independently of provider routing.
func (q Queue) ClaimProviderChannels(ctx context.Context, kinds []string, lease time.Duration, channels []int64) (Job, error) {
	if len(channels) == 0 {
		return Job{}, pgx.ErrNoRows
	}
	return q.claim(ctx, kinds, lease, channels)
}
func (q Queue) claim(ctx context.Context, kinds []string, lease time.Duration, channels []int64) (Job, error) {
	var j Job
	if lease < time.Second || lease > time.Hour {
		return j, errors.New("invalid lease duration")
	}
	nonce := make([]byte, 16)
	if _, e := rand.Read(nonce); e != nil {
		return j, e
	}
	token := hex.EncodeToString(nonce)
	_, e := q.Pool.Exec(ctx, `WITH exhausted AS (SELECT id FROM jobs WHERE kind=ANY($1::text[]) AND state='running' AND lease_until<now() AND attempts>=max_attempts ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 500) UPDATE jobs j SET state='dead',lease_token=NULL,lease_until=NULL,last_error='worker lease expired after final attempt' FROM exhausted x WHERE j.id=x.id`, kinds)
	if e != nil {
		return j, e
	}
	filter := ""
	args := []any{kinds, lease.Milliseconds(), token}
	if channels != nil {
		filter = ` AND (
 kind IN('payment.notify','admin.channel-stopped','merchant.notice','admin.business-notice')
 OR (kind='payment.scan-recover' AND EXISTS(SELECT 1 FROM provider_attempts a WHERE jobs.payload=jsonb_build_object('attempt_id',a.id::text) AND jobs.event_key='scan-recover:'||a.id::text AND a.channel_id=ANY($4::bigint[])))
 OR (kind IN ('payment.reconcile','authorization.release.reconcile') AND EXISTS(SELECT 1 FROM orders o JOIN provider_attempts a ON a.order_id=o.id WHERE o.trade_no=jobs.payload->>'trade_no' AND a.channel_id=ANY($4::bigint[])))
 OR (kind='refund.reconcile' AND EXISTS(SELECT 1 FROM refunds r JOIN provider_attempts a ON a.order_id=r.order_id WHERE r.id::text=jobs.payload->>'refund_id' AND a.channel_id=ANY($4::bigint[])))
 OR (kind IN ('transfer.reconcile','transfer.bank_reconcile') AND EXISTS(SELECT 1 FROM provider_transfer_attempts a WHERE a.transfer_id::text=jobs.payload->>'transfer_id' AND a.channel_id=ANY($4::bigint[])))
 OR (kind='channel-sharing.reconcile' AND EXISTS(SELECT 1 FROM channel_sharing_orders s JOIN provider_attempt_sharing d ON d.order_id=s.order_id WHERE s.id::text=jobs.payload->>'sharing_id' AND d.channel_id=ANY($4::bigint[])))
 OR (kind='channel-sharing-return.reconcile' AND EXISTS(SELECT 1 FROM channel_sharing_returns r JOIN provider_attempt_sharing d ON d.order_id=r.order_id WHERE r.id::text=jobs.payload->>'return_id' AND d.channel_id=ANY($4::bigint[])))
 OR (kind='channel-sharing-finish.reconcile' AND EXISTS(SELECT 1 FROM channel_sharing_finishes p JOIN provider_attempt_sharing d ON d.order_id=p.order_id WHERE p.id::text=jobs.payload->>'finish_id' AND d.channel_id=ANY($4::bigint[])))
)`
		args = append(args, channels)
	}
	e = q.Pool.QueryRow(ctx, `WITH candidate AS (SELECT id FROM jobs WHERE kind=ANY($1::text[]) AND attempts<max_attempts AND run_at<=now() AND (state='pending' OR (state='running' AND lease_until<now()))`+filter+` ORDER BY run_at,id FOR UPDATE SKIP LOCKED LIMIT 1) UPDATE jobs j SET state='running', attempts=attempts+1, lease_until=now()+$2::bigint*interval '1 millisecond',lease_token=$3 FROM candidate c WHERE j.id=c.id RETURNING j.id,j.kind,j.payload,j.attempts,j.lease_token`, args...).Scan(&j.ID, &j.Kind, &j.Payload, &j.Attempts, &j.Token)
	return j, e
}
func (q Queue) Complete(ctx context.Context, j Job) error {
	tag, e := q.Pool.Exec(ctx, "UPDATE jobs SET state='succeeded',lease_until=NULL,lease_token=NULL WHERE id=$1 AND state='running' AND lease_token=$2 AND lease_until>now()", j.ID, j.Token)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	return nil
}
func (q Queue) Retry(ctx context.Context, j Job, reason string, delay time.Duration) error {
	if len(reason) > 500 {
		reason = reason[:500]
	}
	if delay < 0 {
		return errors.New("negative delay")
	}
	tag, e := q.Pool.Exec(ctx, "UPDATE jobs SET state=CASE WHEN attempts>=max_attempts THEN 'dead' ELSE 'pending' END,run_at=now()+$3::bigint*interval '1 millisecond',last_error=$4,lease_until=NULL,lease_token=NULL WHERE id=$1 AND state='running' AND lease_token=$2 AND lease_until>now()", j.ID, j.Token, delay.Milliseconds(), reason)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	return nil
}
func (q Queue) Renew(ctx context.Context, j Job, lease time.Duration) error {
	if lease < time.Second || lease > time.Hour {
		return errors.New("invalid lease duration")
	}
	tag, e := q.Pool.Exec(ctx, "UPDATE jobs SET lease_until=now()+$3::bigint*interval '1 millisecond' WHERE id=$1 AND state='running' AND lease_token=$2 AND lease_until>now()", j.ID, j.Token, lease.Milliseconds())
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	return nil
}
func IsEmpty(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
