package businessnotice

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/jackc/pgx/v5"
	"goepay/internal/ledger"
	"goepay/internal/merchantnotice"
)

func (s *Store) readEvent(ctx context.Context, tx pgx.Tx, id int64) (eventRecord, eventData, error) {
	var r eventRecord
	var data eventData
	err := tx.QueryRow(ctx, `SELECT id,merchant_id,scene,mode,COALESCE(audit_id,0),COALESCE(request_event_id,0),COALESCE(job_id,0),source_transaction::text,created_at,payload_cipher,body_hash,proof_hash FROM admin_business_notice_events WHERE id=$1`, id).Scan(&r.ID, &r.MerchantID, &r.Scene, &r.Mode, &r.AuditID, &r.RequestEventID, &r.JobID, &r.Transaction, &r.At, &r.Cipher, &r.BodyHash, &r.Proof)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, data, ErrNotFound
	}
	if err != nil {
		return r, data, err
	}
	r.At = r.At.UTC()
	if len(r.Proof) != 32 || len(r.BodyHash) != 32 || !hmac.Equal(r.Proof, s.eventProof(r)) || len(r.Cipher) < s.aead.NonceSize()+s.aead.Overhead() {
		return r, data, ErrConflict
	}
	n := s.aead.NonceSize()
	raw, err := s.aead.Open(nil, r.Cipher[:n], r.Cipher[n:], eventAAD(r))
	if err != nil || !hmac.Equal(r.BodyHash, s.digest("event-body", raw)) || json.Unmarshal(raw, &data) != nil || data.Version != 1 || data.Input.Scene != r.Scene || !data.Input.At.Equal(r.At) {
		return r, data, ErrConflict
	}
	kind, err := ModeFor(r.Scene, r.Mode)
	if err != nil || ((kind == "") != (r.JobID == 0)) {
		return r, data, ErrConflict
	}
	expected := map[string]string{"uid": fmt.Sprint(r.MerchantID)}
	if r.AuditID > 0 {
		if r.RequestEventID != 0 || data.Audit == nil || data.Request != nil {
			return r, data, ErrConflict
		}
		var a auditSource
		switch r.Scene {
		case "domain":
			a, err = readDomainSource(ctx, tx, r.AuditID)
		case "regaudit":
			a, err = readRegistrationSource(ctx, tx, r.AuditID)
		default:
			return r, data, ErrConflict
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return r, data, ErrConflict
		}
		if err != nil {
			return r, data, err
		}
		if a.MerchantID != r.MerchantID || a.Scene != r.Scene || a.Transaction != r.Transaction || !hmac.Equal(s.digest("original-audit", a), s.digest("original-audit", *data.Audit)) {
			return r, data, ErrConflict
		}
		if r.Scene == "domain" {
			var body struct {
				Domain string `json:"domain"`
			}
			if json.Unmarshal(a.Changes, &body) != nil || body.Domain == "" {
				return r, data, ErrConflict
			}
			expected["domain"] = body.Domain
		} else {
			expected["account"] = a.Account
		}
	} else {
		if r.RequestEventID <= 0 || data.Request == nil || data.Audit != nil {
			return r, data, ErrConflict
		}
		a, e := s.readRequestSource(ctx, tx, r.RequestEventID)
		if errors.Is(e, pgx.ErrNoRows) {
			return r, data, ErrConflict
		}
		if e != nil {
			return r, data, e
		}
		if a.Facts.MerchantID != r.MerchantID || a.Transaction != r.Transaction || !hmac.Equal(s.digest("original-request", a), s.digest("original-request", *data.Request)) {
			return r, data, ErrConflict
		}
		switch {
		case r.Scene == "internal_transfer" && a.Facts.Kind == "internal-transfers":
			expected["touid"] = fmt.Sprint(a.Facts.RecipientID)
			expected["money"] = ledger.Format(a.Facts.Gross)
		case r.Scene == "apply" && a.Facts.Kind == "settlements":
			expected["type"] = a.Facts.DisplayType
			expected["realmoney"] = ledger.Format(a.Facts.Net)
		default:
			return r, data, ErrConflict
		}
	}
	if !reflect.DeepEqual(expected, data.Input.Params) {
		return r, data, ErrConflict
	}
	if _, err = merchantnotice.Render(data.Input); err != nil {
		return r, data, ErrConflict
	}
	var valid bool
	if err = tx.QueryRow(ctx, `SELECT count(*)=1 FROM backoffice_audit WHERE resource='admin-business-notice-events' AND resource_id=$1 AND operation='create' AND (actor_role,actor_id,actor_name)=('system',0,'business-notice') AND changes=$2::jsonb`, r.ID, eventAudit(r)).Scan(&valid); err != nil {
		return r, data, err
	}
	if !valid {
		return r, data, ErrConflict
	}
	if r.JobID > 0 {
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE id=$1 AND kind='admin.business-notice' AND event_key='admin-business-notice:'||$2::bigint::text AND payload=jsonb_build_object('event_id',$2::bigint::text))`, r.JobID, r.ID).Scan(&valid); err != nil {
			return r, data, err
		}
		if !valid {
			return r, data, ErrConflict
		}
	}
	return r, data, nil
}
