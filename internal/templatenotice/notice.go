// Package templatenotice delivers the administrator notice enqueued for every
// uploaded page template event (upload, restore, remove, select). It commits a
// permanent, recipient-bound claim before any transport I/O, so a crash or an
// uncertain transport answer never sends the same event's notice twice.
// Acceptance never proves inbox delivery.
package templatenotice

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/templatechange"
)

// ErrNotice marks an administrator template notice that requires the original
// claim to be reviewed before any further delivery attempt.
var ErrNotice = errors.New("administrator template notice requires original claim review")

// ChangeNoticeSender submits one administrator notice for a page template event
// through an explicitly configured transport. A nil error means the provider
// accepted the submission; it never proves final delivery.
type ChangeNoticeSender interface {
	SendPageTemplateNotice(ctx context.Context, to string, n templatechange.Notice) error
}

// Notices delivers page template administrator notices against a pool.
type Notices struct {
	pool *pgxpool.Pool
}

// New returns a Notices bound to pool.
func New(pool *pgxpool.Pool) *Notices {
	return &Notices{pool: pool}
}

// Deliver claims the event behind the running job (identified by jobID and its
// 32-hex lease token), commits the claim, then submits the notice to recipient
// through sender and records the result plus an audit row. The claim is
// one-shot: once it exists for the event, Deliver returns nil without resending
// when it was made by this same job for this same recipient, and a conflict
// otherwise. A send failure records 'unknown' and still completes the job (no
// retry after a claim); only a failure to record the result returns an error.
func (n *Notices) Deliver(ctx context.Context, jobID int64, leaseToken, recipient string, sender ChangeNoticeSender) error {
	if n == nil || n.pool == nil || ctx == nil || jobID <= 0 || len(leaseToken) != 32 || recipient == "" || sender == nil {
		return ErrNotice
	}
	if _, e := hex.DecodeString(leaseToken); e != nil {
		return ErrNotice
	}
	tx, e := n.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)

	var eventID int64
	var notice templatechange.Notice
	e = tx.QueryRow(ctx, `SELECT e.id,u.kind,u.name,v.title,e.operation,e.version,COALESCE(e.restored_from,0),e.actor_name,e.created_at FROM jobs j JOIN page_template_upload_events e ON j.event_key='page-template-event:'||e.id::text AND j.payload=jsonb_build_object('event_id',e.id::text) JOIN page_template_uploads u ON u.id=e.upload_id JOIN page_template_upload_versions v ON v.upload_id=e.upload_id AND v.version=e.version WHERE j.id=$1 AND j.kind='admin.page-template-notice' AND j.state='running' AND j.lease_token=$2 AND j.lease_until>clock_timestamp() FOR UPDATE OF j`, jobID, leaseToken).Scan(&eventID, &notice.Kind, &notice.Name, &notice.Title, &notice.Operation, &notice.Version, &notice.RestoredFrom, &notice.Actor, &notice.At)
	if errors.Is(e, pgx.ErrNoRows) {
		return ErrNotice
	}
	if e != nil {
		return e
	}

	recipientHash := sha256.Sum256([]byte(recipient))
	leaseHash := sha256.Sum256([]byte(leaseToken))

	var existingJob int64
	var existingRecipient []byte
	e = tx.QueryRow(ctx, `SELECT job_id,recipient_hash FROM page_template_notices WHERE event_id=$1`, eventID).Scan(&existingJob, &existingRecipient)
	if e == nil {
		// The event already has a claim. Only the exact same job and recipient
		// is a safe no-op; anything else is a conflict that must not resend.
		if existingJob != jobID || !hmac.Equal(existingRecipient, recipientHash[:]) {
			return ErrNotice
		}
		return nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return e
	}

	if _, e = tx.Exec(ctx, `INSERT INTO page_template_notices(event_id,job_id,recipient_hash,lease_hash)VALUES($1,$2,$3,$4)`, eventID, jobID, recipientHash[:], leaseHash[:]); e != nil {
		return e
	}
	if guarded, ok := sender.(interface {
		ValidateNoticeConfig231(context.Context, pgx.Tx, time.Time) error
	}); ok {
		if e = guarded.ValidateNoticeConfig231(ctx, tx, notice.At); e != nil {
			return e
		}
	}
	// The job lease must still hold the claim at commit; otherwise another
	// worker could have taken the job between the lock and here.
	var alive bool
	if e = tx.QueryRow(ctx, `SELECT state='running' AND lease_token=$2 AND lease_until>clock_timestamp() FROM jobs WHERE id=$1`, jobID, leaseToken).Scan(&alive); e != nil {
		return e
	}
	if !alive {
		return ErrNotice
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}

	// Transport I/O only after the claim is durable. A bounded 10 s budget
	// detached from the caller's cancellation keeps one send attempt.
	work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	state := "accepted"
	if sender.SendPageTemplateNotice(work, recipient, notice) != nil {
		state = "unknown"
	}

	record, cancelRecord := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancelRecord()
	return n.recordResult(record, eventID, notice, state)
}

func (n *Notices) recordResult(ctx context.Context, eventID int64, notice templatechange.Notice, state string) error {
	tx, e := n.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `INSERT INTO page_template_notice_results(event_id,state)VALUES($1,$2)`, eventID, state); e != nil {
		return e
	}
	changes, _ := json.Marshal(map[string]any{"kind": notice.Kind, "name": notice.Name, "operation": notice.Operation, "version": notice.Version, "restored_from": notice.RestoredFrom, "state": state})
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('system',0,'page-template-notice','page-template-notices',$1,$2,$3)`, eventID, state, changes); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
