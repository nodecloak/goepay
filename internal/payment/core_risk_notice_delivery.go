package payment

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"goepay/internal/merchantnotice"
	"time"
)

type CoreRiskNoticeResolver interface {
	ResolveCoreRiskNotice(context.Context, string) (merchantnotice.Sender, string, error)
}
type coreRiskNoticeClaim struct {
	ActionID, JobID                                 int64
	Attempt                                         int
	NoticeHash, RecipientHash, RouteHash, LeaseHash []byte
	At, Until                                       time.Time
}
type coreRiskNoticeResult struct {
	ActionID  int64
	ClaimHash []byte
	State     string
	At        time.Time
}

func (s *Store) readCoreRiskNoticeOutcome(ctx context.Context, tx pgx.Tx, id int64, noticeHash []byte) (bool, bool, error) {
	var raw, proof []byte
	e := tx.QueryRow(ctx, `SELECT facts,proof_hash FROM core_risk_notice_claims WHERE action_id=$1`, id).Scan(&raw, &proof)
	if e == pgx.ErrNoRows {
		var exists bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='core-risk-notices' AND resource_id=$1 AND operation IN('attempt','succeeded','unknown'))`, id).Scan(&exists); e != nil {
			return false, false, e
		}
		if exists {
			return false, false, ErrConflict
		}
		return false, false, nil
	}
	if e != nil {
		return false, false, e
	}
	var claim coreRiskNoticeClaim
	if decodeCoreRiskFacts(raw, &claim) != nil || claim.ActionID != id || claim.JobID <= 0 || claim.Attempt < 1 || !hmac.Equal(claim.NoticeHash, noticeHash) || len(claim.LeaseHash) != 32 || len(claim.RouteHash) != 32 || len(claim.RecipientHash) != 32 {
		return false, false, ErrConflict
	}
	expected, e := s.referralDigest("risk102/notice-claim", claim)
	if e != nil || !hmac.Equal(expected, proof) {
		return false, false, ErrConflict
	}
	if e = coreRiskNoticeOrigin(ctx, tx, id, "attempt", proof); e != nil {
		return false, false, e
	}
	claimHash := proof
	e = tx.QueryRow(ctx, `SELECT facts,proof_hash FROM core_risk_notice_results WHERE action_id=$1`, id).Scan(&raw, &proof)
	if e == pgx.ErrNoRows {
		return true, false, nil
	}
	if e != nil {
		return false, false, e
	}
	var result coreRiskNoticeResult
	if decodeCoreRiskFacts(raw, &result) != nil || result.ActionID != id || !hmac.Equal(result.ClaimHash, claimHash) || result.State != "succeeded" && result.State != "unknown" || result.At.Before(claim.At) {
		return false, false, ErrConflict
	}
	expected, e = s.referralDigest("risk102/notice-result", result)
	if e != nil || !hmac.Equal(expected, proof) {
		return false, false, ErrConflict
	}
	if e = coreRiskNoticeOrigin(ctx, tx, id, result.State, proof); e != nil {
		return false, false, e
	}
	return true, result.State == "succeeded", nil
}
func (s *Store) DeliverCoreRiskNotice(ctx context.Context, job int64, lease string, resolver CoreRiskNoticeResolver) error {
	if job < 1 || lease == "" || resolver == nil {
		return ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var id int64
	var payload []byte
	var attempt int
	var until time.Time
	var key string
	if e = tx.QueryRow(ctx, `SELECT payload,event_key,attempts,lease_until FROM jobs WHERE id=$1 AND kind='core-risk.notice' AND state='running' AND lease_token=$2 AND lease_until>clock_timestamp() FOR UPDATE`, job, lease).Scan(&payload, &key, &attempt, &until); e != nil {
		return ErrState
	}
	var p struct {
		ActionID string `json:"action_id"`
	}
	if json.Unmarshal(payload, &p) != nil {
		return ErrConflict
	}
	if e = tx.QueryRow(ctx, `SELECT action_id FROM core_risk_notices WHERE job_id=$1`, job).Scan(&id); e != nil {
		return ErrConflict
	}
	canonical, _ := json.Marshal(map[string]string{"action_id": fmt.Sprint(id)})
	var exact bool
	if e = tx.QueryRow(ctx, `SELECT $1::jsonb=$2::jsonb`, string(payload), string(canonical)).Scan(&exact); e != nil || !exact || p.ActionID != fmt.Sprint(id) || key != fmt.Sprint("core-risk-notice:", id) {
		return ErrConflict
	}
	n, body, noticeHash, e := s.readCoreRiskNotice(ctx, tx, id)
	if e != nil {
		return e
	}
	if n.JobID != job {
		return ErrConflict
	}
	claimed, success, e := s.readCoreRiskNoticeOutcome(ctx, tx, id, noticeHash)
	if e != nil {
		return e
	}
	if claimed {
		if success {
			return tx.Commit(ctx)
		}
		return ErrState
	}
	var sender merchantnotice.Sender
	var recipient string
	if resolved, ok := resolver.(interface {
		ResolveCoreRiskNoticeTx231(context.Context, pgx.Tx, string) (merchantnotice.Sender, string, error)
	}); ok {
		sender, recipient, e = resolved.ResolveCoreRiskNoticeTx231(ctx, tx, n.Kind)
	} else {
		sender, recipient, e = resolver.ResolveCoreRiskNotice(ctx, n.Kind)
	}
	if e != nil || sender == nil {
		return ErrState
	}
	if n.Kind == "merchant" {
		recipient = body.Recipient
		if sender.NoticeTransport() != merchantnotice.SMTP {
			return ErrInvalid
		}
	}
	if sender.NoticeScope() == "" || sender.ValidateNoticeRecipient(recipient) != nil {
		return ErrInvalid
	}
	if guarded, ok := sender.(interface {
		ValidateNoticeConfig231(context.Context, pgx.Tx, time.Time) error
	}); ok {
		if e = guarded.ValidateNoticeConfig231(ctx, tx, n.CreatedAt); e != nil {
			return e
		}
	}
	message, e := merchantnotice.Render(body.Input)
	if e != nil {
		return e
	}
	now, e := coreRiskDBNow(ctx, tx)
	if e != nil {
		return e
	}
	claim := coreRiskNoticeClaim{ActionID: id, JobID: job, Attempt: attempt, NoticeHash: noticeHash, At: now, Until: until.UTC()}
	claim.LeaseHash, e = s.referralDigest("risk102/notice-lease", lease)
	if e != nil {
		return e
	}
	claim.RecipientHash, e = s.referralDigest("risk102/notice-recipient", recipient)
	if e != nil {
		return e
	}
	claim.RouteHash, e = s.referralDigest("risk102/notice-route", []string{string(sender.NoticeTransport()), sender.NoticeScope()})
	if e != nil {
		return e
	}
	hash, e := s.referralDigest("risk102/notice-claim", claim)
	if e != nil {
		return e
	}
	raw, _ := json.Marshal(claim)
	if _, e = tx.Exec(ctx, `INSERT INTO core_risk_notice_claims(action_id,facts,proof_hash)VALUES($1,$2::jsonb,$3)`, id, string(raw), hash); e != nil {
		return e
	}
	if e = s.coreRiskNoticeAudit(ctx, tx, id, "attempt", hash); e != nil {
		return e
	}
	var live bool
	if e = tx.QueryRow(ctx, `SELECT state='running' AND lease_token=$2 AND attempts=$3 AND lease_until=$4 AND lease_until>clock_timestamp() FROM jobs WHERE id=$1`, job, lease, attempt, until).Scan(&live); e != nil {
		return e
	}
	if !live {
		return ErrState
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	sendError := sender.SendMerchantNotice(ctx, recipient, message)
	work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	resultTx, e := s.pool.Begin(work)
	if e != nil {
		return ErrState
	}
	defer resultTx.Rollback(work)
	state := "succeeded"
	if sendError != nil {
		state = "unknown"
	}
	at, e := coreRiskDBNow(work, resultTx)
	if e != nil {
		return e
	}
	result := coreRiskNoticeResult{ActionID: id, ClaimHash: hash, State: state, At: at}
	resultProof, e := s.referralDigest("risk102/notice-result", result)
	if e != nil {
		return e
	}
	raw, _ = json.Marshal(result)
	if _, e = resultTx.Exec(work, `INSERT INTO core_risk_notice_results(action_id,facts,proof_hash)VALUES($1,$2::jsonb,$3)`, id, string(raw), resultProof); e != nil {
		return e
	}
	if e = s.coreRiskNoticeAudit(work, resultTx, id, state, resultProof); e != nil {
		return e
	}
	if e = resultTx.Commit(work); e != nil {
		return e
	}
	if sendError != nil {
		return ErrState
	}
	return nil
}
