package payment

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"goepay/internal/merchantnotice"
	"time"
)

const CoreRiskNoticeJob = "core-risk.notice"

type coreRiskNotice struct {
	ActionID, JobID, TargetID int64
	Kind, Transaction         string
	ActionHash, CipherHash    []byte
	CreatedAt                 time.Time
}
type coreRiskNoticeBody struct {
	Input     merchantnotice.Input
	Recipient string
}

func (s *Store) captureCoreRiskNotice(ctx context.Context, tx pgx.Tx, kind, key, reason string) error {
	a, hash, e := s.readCoreRiskAction(ctx, tx, kind, key)
	if e != nil {
		return e
	}
	if a == nil || a.Action != "block" {
		return ErrConflict
	}
	var n coreRiskNotice
	n.Kind = kind
	n.TargetID = a.TargetID
	n.ActionHash = hash
	if e = tx.QueryRow(ctx, `SELECT id FROM core_risk_actions WHERE kind=$1 AND subject_key=$2 AND revision=$3`, kind, key, a.Revision).Scan(&n.ActionID); e != nil {
		return e
	}
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp(),pg_current_xact_id()::text`).Scan(&n.CreatedAt, &n.Transaction); e != nil {
		return e
	}
	n.CreatedAt = n.CreatedAt.UTC()
	body := coreRiskNoticeBody{Input: merchantnotice.Input{Scene: "core_risk", SiteName: "支付平台", At: n.CreatedAt, Params: map[string]string{"kind": "支付通道", "id": fmt.Sprint(a.TargetID), "reason": reason}}}
	if kind == "subchannel" {
		body.Input.Params["kind"] = "支付子通道"
	}
	if kind == "merchant" {
		body.Input.Params["kind"] = "商户"
		if e = tx.QueryRow(ctx, `SELECT COALESCE(profile->>'email','')FROM merchants WHERE id=$1 FOR SHARE`, a.TargetID).Scan(&body.Recipient); e != nil {
			return e
		}
	}
	if _, e = merchantnotice.Render(body.Input); e != nil {
		return e
	}
	if kind == "channel" || kind == "subchannel" || body.Recipient != "" {
		if e = tx.QueryRow(ctx, `INSERT INTO jobs(event_key,kind,payload,run_at)VALUES($1,'core-risk.notice',jsonb_build_object('action_id',$2::bigint::text),clock_timestamp())RETURNING id`, fmt.Sprint("core-risk-notice:", n.ActionID), n.ActionID).Scan(&n.JobID); e != nil {
			return e
		}
	}
	raw, e := json.Marshal(body)
	if e != nil {
		return e
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return e
	}
	aad := []byte(fmt.Sprint("risk102/notice/", n.ActionID))
	cipher := s.aead.Seal(nonce, nonce, raw, aad)
	digest := sha256.Sum256(cipher)
	n.CipherHash = digest[:]
	proof, e := s.referralDigest("risk102/notice", n)
	if e != nil {
		return e
	}
	header, _ := json.Marshal(n)
	var job any
	if n.JobID > 0 {
		job = n.JobID
	}
	if _, e = tx.Exec(ctx, `INSERT INTO core_risk_notices(action_id,job_id,facts,payload_cipher,proof_hash)VALUES($1,$2,$3::jsonb,$4,$5)`, n.ActionID, job, string(header), cipher, proof); e != nil {
		return e
	}
	return s.coreRiskNoticeAudit(ctx, tx, n.ActionID, "capture", proof)
}
func (s *Store) coreRiskNoticeAudit(ctx context.Context, tx pgx.Tx, id int64, op string, proof []byte) error {
	_, e := tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('system',0,'core-risk-notice','core-risk-notices',$1,$2,jsonb_build_object('source_hash',$3::text))`, id, op, hex.EncodeToString(proof))
	return e
}
func coreRiskNoticeOrigin(ctx context.Context, q referralReader, id int64, op string, proof []byte) error {
	var valid bool
	if e := q.QueryRow(ctx, `SELECT count(*)=1 FROM backoffice_audit WHERE resource='core-risk-notices' AND resource_id=$1 AND operation=$2 AND (actor_role,actor_id,actor_name)=('system',0,'core-risk-notice') AND changes=jsonb_build_object('source_hash',$3::text)`, id, op, hex.EncodeToString(proof)).Scan(&valid); e != nil {
		return e
	}
	if !valid {
		return ErrConflict
	}
	return nil
}
func (s *Store) readCoreRiskNotice(ctx context.Context, tx pgx.Tx, id int64) (coreRiskNotice, coreRiskNoticeBody, []byte, error) {
	var n coreRiskNotice
	var body coreRiskNoticeBody
	var raw, cipher, proof []byte
	var job int64
	if e := tx.QueryRow(ctx, `SELECT COALESCE(job_id,0),facts,payload_cipher,proof_hash FROM core_risk_notices WHERE action_id=$1`, id).Scan(&job, &raw, &cipher, &proof); e != nil {
		return n, body, nil, ErrConflict
	}
	digest := sha256.Sum256(cipher)
	if decodeCoreRiskFacts(raw, &n) != nil || n.ActionID != id || n.JobID != job || !hmac.Equal(digest[:], n.CipherHash) {
		return n, body, nil, ErrConflict
	}
	expected, e := s.referralDigest("risk102/notice", n)
	if e != nil || !hmac.Equal(expected, proof) {
		return n, body, nil, ErrConflict
	}
	if e = coreRiskNoticeOrigin(ctx, tx, id, "capture", proof); e != nil {
		return n, body, nil, e
	}
	var kind, key string
	var actionHash []byte
	if e = tx.QueryRow(ctx, `SELECT kind,subject_key,proof_hash FROM core_risk_actions WHERE id=$1`, id).Scan(&kind, &key, &actionHash); e != nil || kind != n.Kind || !hmac.Equal(actionHash, n.ActionHash) {
		return n, body, nil, ErrConflict
	}
	if _, _, e = s.readCoreRiskAction(ctx, tx, kind, key); e != nil {
		return n, body, nil, e
	}
	size := s.aead.NonceSize()
	if len(cipher) < size+s.aead.Overhead() {
		return n, body, nil, ErrConflict
	}
	plain, e := s.aead.Open(nil, cipher[:size], cipher[size:], []byte(fmt.Sprint("risk102/notice/", id)))
	if e != nil || decodeCoreRiskFacts(plain, &body) != nil || body.Input.Scene != "core_risk" || body.Input.Params["id"] != fmt.Sprint(n.TargetID) || !body.Input.At.Equal(n.CreatedAt) {
		return n, body, nil, ErrConflict
	}
	if _, e = merchantnotice.Render(body.Input); e != nil {
		return n, body, nil, ErrConflict
	}
	return n, body, proof, nil
}
