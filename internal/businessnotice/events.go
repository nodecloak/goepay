package businessnotice

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"goepay/internal/merchantnotice"
	"time"
)

const JobKind = "admin.business-notice"

type auditSource struct {
	Account                                   string
	ID, ResourceID, MerchantID                int64
	Role, Name, Operation, Scene, Transaction string
	Changes                                   json.RawMessage
	At                                        time.Time
}
type requestSource struct {
	ID, RequestID      int64
	Actor, Transaction string
	At                 time.Time
	Facts              RequestFacts
}
type eventData struct {
	Version int
	Audit   *auditSource
	Request *requestSource
	Input   merchantnotice.Input
}
type eventRecord struct {
	ID, MerchantID, AuditID, RequestEventID, JobID int64
	Scene, Mode, Transaction                       string
	At                                             time.Time
	Cipher, BodyHash, Proof                        []byte
}

func eventAAD(r eventRecord) []byte {
	raw, _ := json.Marshal([]any{"admin-business83/event/v1", r.ID, r.MerchantID, r.Scene, r.Mode, r.AuditID, r.RequestEventID, r.JobID, r.Transaction, r.At.UTC().Format(time.RFC3339Nano), r.BodyHash})
	return raw
}
func (s *Store) eventProof(r eventRecord) []byte {
	cipherHash := sha256.Sum256(r.Cipher)
	return s.digest("event-header", []any{eventAAD(r), cipherHash[:]})
}
func eventAudit(r eventRecord) []byte {
	raw, _ := json.Marshal(map[string]string{"proof": hex.EncodeToString(r.Proof), "job_id": fmt.Sprint(r.JobID)})
	return raw
}

func (s *Store) appendEvent(ctx context.Context, tx pgx.Tx, r eventRecord, data eventData) error {
	values, err := ReadSettings(ctx, tx)
	if err != nil {
		return err
	}
	key, err := SettingKey(r.Scene)
	if err != nil {
		return err
	}
	r.Mode = "0"
	if value, ok := values[key]; ok {
		r.Mode = value
	}
	if _, err = ModeFor(r.Scene, r.Mode); err != nil {
		return err
	}
	var current string
	if err = tx.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('admin_business_notice_events','id')),clock_timestamp(),pg_current_xact_id()::text`).Scan(&r.ID, &r.At, &current); err != nil {
		return err
	}
	r.At = r.At.UTC()
	if r.Transaction != current {
		return ErrConflict
	}
	data.Version = 1
	data.Input.Scene = r.Scene
	data.Input.At = r.At
	data.Input.SiteName = values["sitename"]
	if _, err = merchantnotice.Render(data.Input); err != nil {
		return ErrInvalid
	}
	if r.Mode != "0" {
		if err = tx.QueryRow(ctx, `INSERT INTO jobs(event_key,kind,payload) VALUES('admin-business-notice:'||$1::bigint::text,'admin.business-notice',jsonb_build_object('event_id',$1::bigint::text)) RETURNING id`, r.ID).Scan(&r.JobID); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(data)
	if err != nil || len(raw) > 60000 {
		return ErrInvalid
	}
	r.BodyHash = s.digest("event-body", raw)
	nonce := make([]byte, s.aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	r.Cipher = s.aead.Seal(nonce, nonce, raw, eventAAD(r))
	r.Proof = s.eventProof(r)
	var audit, request, job any
	if r.AuditID > 0 {
		audit = r.AuditID
	}
	if r.RequestEventID > 0 {
		request = r.RequestEventID
	}
	if r.JobID > 0 {
		job = r.JobID
	}
	_, err = tx.Exec(ctx, `INSERT INTO admin_business_notice_events(id,merchant_id,scene,mode,audit_id,request_event_id,job_id,source_transaction,created_at,payload_cipher,body_hash,proof_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8::xid8,$9,$10,$11,$12)`, r.ID, r.MerchantID, r.Scene, r.Mode, audit, request, job, r.Transaction, r.At, r.Cipher, r.BodyHash, r.Proof)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('system',0,'business-notice','admin-business-notice-events',$1,'create',$2::jsonb)`, r.ID, eventAudit(r))
	return err
}
