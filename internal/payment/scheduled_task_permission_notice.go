package payment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	notice "goepay/internal/merchantnotice"
	"goepay/internal/platform/jobs"
)

type scheduledPermissionMailClaim struct {
	StopID, JobID                                   int64
	StopHash, RecipientHash, AccountHash, LeaseHash []byte
	At                                              time.Time
}

func (s *Store) scheduledPermissionStop(ctx context.Context, tx pgx.Tx, id int64) (scheduledNotificationStop, []byte, error) {
	var out scheduledNotificationStop
	var mid, rev, failure, risk int64
	var raw, proof []byte
	var at time.Time
	if e := tx.QueryRow(ctx, `SELECT merchant_id,original_revision,trigger_failure_id,risk_record_id,facts,created_at,proof_hash FROM scheduled_notification_stops WHERE id=$1`, id).Scan(&mid, &rev, &failure, &risk, &raw, &at, &proof); e != nil {
		return out, nil, ErrConflict
	}
	if decodeScheduledJSON(raw, &out) != nil || out.ID != id || out.MerchantID != mid || out.Revision != rev || out.TriggerFailureID != failure || out.RiskRecordID != risk || !out.At.Equal(at) || out.MailState != "pending" || len(out.FailureIDs) < 1 || len(out.FailureIDs) > 1000 {
		return out, nil, ErrConflict
	}
	expected, e := s.referralDigest("scheduler171/notification-stop", out)
	if e != nil || !hmac.Equal(expected, proof) {
		return out, nil, ErrConflict
	}
	var origin bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='scheduled-notification-stops' AND resource_id=$1 AND operation='close-pay' AND actor_role='system' AND actor_id=0 AND changes->>'merchant_id'=$2 AND changes->>'risk_record_id'=$3 AND changes->>'notice_state'='pending' AND changes->>'failure_count'=$4)`, id, fmt.Sprint(mid), fmt.Sprint(risk), fmt.Sprint(len(out.FailureIDs))).Scan(&origin); e != nil || !origin {
		return out, nil, ErrConflict
	}
	seen := map[int64]bool{}
	trigger := false
	for _, fid := range out.FailureIDs {
		f, e := s.readScheduledNotificationFailure(ctx, tx, fid)
		if e != nil || seen[fid] || f.MerchantID != mid || f.At.After(at) {
			return out, nil, ErrConflict
		}
		seen[fid] = true
		trigger = trigger || fid == failure
	}
	if !trigger {
		return out, nil, ErrConflict
	}
	return out, proof, nil
}

// DeliverScheduledPaymentDisabledNotice consumes only an original immutable
// permission-stop source. The scheduler itself never invokes a sender.
func (s *Store) DeliverScheduledPaymentDisabledNotice(ctx context.Context, jobID int64, lease string, sender notice.Sender) error {
	if sender == nil || sender.NoticeTransport() != notice.SMTP || sender.NoticeScope() == "" || lease == "" {
		return ErrState
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var kind, event string
	var payload []byte
	var until time.Time
	var head int64
	e = tx.QueryRow(ctx, `SELECT kind,event_key,payload,lease_until,COALESCE(scheduled_notification_mail_head,0) FROM jobs WHERE id=$1 AND state='running' AND lease_token=$2 AND lease_until>clock_timestamp() FOR UPDATE`, jobID, lease).Scan(&kind, &event, &payload, &until, &head)
	if e == pgx.ErrNoRows {
		return jobs.ErrLeaseLost
	}
	if e != nil {
		return e
	}
	var input struct {
		StopID string `json:"stop_id"`
	}
	if decodeScheduledJSON(payload, &input) != nil {
		return ErrConflict
	}
	id, e := strconv.ParseInt(input.StopID, 10, 64)
	if e != nil || id < 1 || fmt.Sprint(id) != input.StopID || kind != "merchant.payment-disabled-notice" || event != "scheduler-notify-stop:"+input.StopID {
		return ErrConflict
	}
	stop, stopHash, e := s.scheduledPermissionStop(ctx, tx, id)
	if e != nil {
		return e
	}
	var old scheduledPermissionMailClaim
	var oldProof []byte
	e = tx.QueryRow(ctx, `SELECT stop_id,job_id,stop_hash,recipient_hash,account_hash,lease_hash,started_at,proof_hash FROM scheduled_notification_mail_claims WHERE stop_id=$1`, id).Scan(&old.StopID, &old.JobID, &old.StopHash, &old.RecipientHash, &old.AccountHash, &old.LeaseHash, &old.At, &oldProof)
	var started bool
	if ae := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='scheduled-notification-mail' AND resource_id=$1 AND operation='start' AND actor_role='system' AND actor_id=0 AND changes->>'job_id'=$2)`, id, fmt.Sprint(jobID)).Scan(&started); ae != nil {
		return ae
	}
	if e == nil {
		old.At = old.At.UTC()
		p, pe := s.referralDigest("scheduler171/permission-mail-claim", old)
		if pe != nil || !hmac.Equal(p, oldProof) || old.JobID != jobID || head != id || !started || !hmac.Equal(old.StopHash, stopHash) {
			return ErrConflict
		}
		var state string
		var at time.Time
		var result []byte
		if e = tx.QueryRow(ctx, `SELECT state,completed_at,proof_hash FROM scheduled_notification_mail_results WHERE stop_id=$1`, id).Scan(&state, &at, &result); e != nil {
			return ErrState
		}
		p, pe = s.referralDigest("scheduler171/permission-mail-result", []any{id, oldProof, state, at.UTC().Format(time.RFC3339Nano)})
		if pe != nil || !hmac.Equal(p, result) {
			return ErrConflict
		}
		if state != "accepted" {
			return ErrState
		}
		return nil
	}
	if e != pgx.ErrNoRows {
		return e
	}
	if head != 0 || started {
		return ErrConflict
	}
	if len(stop.MailCipher) < s.aead.NonceSize() {
		return ErrConflict
	}
	nonce := stop.MailCipher[:s.aead.NonceSize()]
	plain, e := s.aead.Open(nil, nonce, stop.MailCipher[s.aead.NonceSize():], []byte(fmt.Sprintf("scheduler171/notify-stop/%d", id)))
	if e != nil {
		return ErrConflict
	}
	to := string(plain)
	if sender.ValidateNoticeRecipient(to) != nil {
		return ErrState
	}
	message, e := notice.Render(notice.Input{Scene: "payment_disabled", SiteName: stop.SiteName, At: stop.At, Params: map[string]string{"uid": fmt.Sprint(stop.MerchantID), "count": fmt.Sprint(len(stop.FailureIDs))}})
	if e != nil {
		return ErrState
	}
	rh, e := s.referralDigest("scheduler171/permission-mail-recipient", to)
	if e != nil {
		return e
	}
	ah, e := s.referralDigest("scheduler171/permission-mail-account", sender.NoticeScope())
	if e != nil {
		return e
	}
	lh := sha256.Sum256([]byte(lease))
	at, e := scheduledDBNow(ctx, tx)
	if e != nil {
		return e
	}
	claim := scheduledPermissionMailClaim{StopID: id, JobID: jobID, StopHash: stopHash, RecipientHash: rh, AccountHash: ah, LeaseHash: lh[:], At: at}
	proof, e := s.referralDigest("scheduler171/permission-mail-claim", claim)
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO scheduled_notification_mail_claims(stop_id,job_id,stop_hash,recipient_hash,account_hash,lease_hash,started_at,proof_hash)VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, id, jobID, stopHash, rh, ah, lh[:], at, proof); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE jobs SET scheduled_notification_mail_head=$2 WHERE id=$1`, jobID, id); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('system',0,'scheduled-notify','scheduled-notification-mail',$1,'start',jsonb_build_object('job_id',$2::text))`, id, fmt.Sprint(jobID)); e != nil {
		return e
	}
	now, e := scheduledDBNow(ctx, tx)
	if e != nil {
		return e
	}
	if !now.Before(until) {
		return jobs.ErrLeaseLost
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	// A durable claim precedes I/O. A missing result is deliberately unknown:
	// neither lease recovery nor a replacement SMTP account may resend it.
	sendErr := sender.SendMerchantNotice(ctx, to, message)
	state := "accepted"
	if sendErr != nil {
		state = "unknown"
	}
	finishCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	finish, e := s.pool.Begin(finishCtx)
	if e != nil {
		return ErrState
	}
	defer finish.Rollback(finishCtx)
	completed, e := scheduledDBNow(finishCtx, finish)
	if e != nil {
		return ErrState
	}
	result, e := s.referralDigest("scheduler171/permission-mail-result", []any{id, proof, state, completed.UTC().Format(time.RFC3339Nano)})
	if e != nil {
		return ErrState
	}
	if _, e = finish.Exec(finishCtx, `INSERT INTO scheduled_notification_mail_results(stop_id,state,completed_at,proof_hash)VALUES($1,$2,$3,$4)`, id, state, completed, result); e != nil {
		return ErrState
	}
	if e = finish.Commit(finishCtx); e != nil {
		return ErrState
	}
	if sendErr != nil {
		return ErrState
	}
	return nil
}
