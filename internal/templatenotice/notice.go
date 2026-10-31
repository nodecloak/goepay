// Package templatenotice delivers the administrator notice enqueued for every
// 首页自定义 HTML version. It commits a permanent, recipient-bound claim before
// any transport I/O, so a crash or an uncertain transport answer never sends
// the same version's notice twice. Acceptance never proves inbox delivery.
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
)

// ErrNotice marks an administrator template notice that requires the original
// claim to be reviewed before any further delivery attempt.
var ErrNotice = errors.New("administrator template notice requires original claim review")

// ChangeNoticeSender submits one administrator notice for a 首页自定义 HTML
// version through an explicitly configured transport. A nil error means the
// provider accepted the submission; it never proves final delivery.
type ChangeNoticeSender interface {
	SendPageTemplateChanged(context.Context, string, int64, int64, string, time.Time) error
}

// Notices delivers page template administrator notices against a pool.
type Notices struct {
	pool *pgxpool.Pool
}

// New returns a Notices bound to pool.
func New(pool *pgxpool.Pool) *Notices {
	return &Notices{pool: pool}
}

// Deliver claims the version behind the running job (identified by jobID and
// its 32-hex lease token), commits the claim, then submits the notice to
// recipient through sender and records the result plus an audit row. The claim
// is one-shot: once it exists for the version, Deliver returns nil without
// resending when it was made by this same job for this same recipient, and a
// conflict otherwise. A send failure records 'unknown' and still completes the
// job (no retry after a claim); only a failure to record the result returns an
// error.
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

	var versionID, version, restoredFrom int64
	var actor string
	var createdAt time.Time
	e = tx.QueryRow(ctx, `SELECT v.id,v.version,COALESCE(v.restored_from,0),v.actor_name,v.created_at FROM jobs j JOIN page_template_custom_versions v ON j.event_key='page-template-custom:'||v.id::text AND j.payload=jsonb_build_object('version_id',v.id::text) WHERE j.id=$1 AND j.kind='admin.page-template-changed' AND j.state='running' AND j.lease_token=$2 AND j.lease_until>clock_timestamp() FOR UPDATE OF j`, jobID, leaseToken).Scan(&versionID, &version, &restoredFrom, &actor, &createdAt)
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
	e = tx.QueryRow(ctx, `SELECT job_id,recipient_hash FROM page_template_custom_notices WHERE version_id=$1`, versionID).Scan(&existingJob, &existingRecipient)
	if e == nil {
		// The version already has a claim. Only the exact same job and recipient
		// is a safe no-op; anything else is a conflict that must not resend.
		if existingJob != jobID || !hmac.Equal(existingRecipient, recipientHash[:]) {
			return ErrNotice
		}
		return nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return e
	}

	if _, e = tx.Exec(ctx, `INSERT INTO page_template_custom_notices(version_id,job_id,recipient_hash,lease_hash)VALUES($1,$2,$3,$4)`, versionID, jobID, recipientHash[:], leaseHash[:]); e != nil {
		return e
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
	if sender.SendPageTemplateChanged(work, recipient, version, restoredFrom, actor, createdAt) != nil {
		state = "unknown"
	}

	record, cancelRecord := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancelRecord()
	return n.recordResult(record, versionID, version, restoredFrom, state)
}

func (n *Notices) recordResult(ctx context.Context, versionID, version, restoredFrom int64, state string) error {
	tx, e := n.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `INSERT INTO page_template_custom_notice_results(version_id,state)VALUES($1,$2)`, versionID, state); e != nil {
		return e
	}
	changes, _ := json.Marshal(map[string]any{"version": version, "restored_from": restoredFrom, "state": state})
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('system',0,'page-template-notice','page-template-notices',$1,$2,$3)`, versionID, state, changes); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
