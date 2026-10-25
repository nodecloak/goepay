package merchantnotice

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

type domainEventSource struct {
	AuditID, DomainID, MerchantID, ActorID int64
	ActorName, Domain, Transaction         string
	Approved                               bool
	AuditHash                              []byte
	At                                     time.Time
}

func (s *Store) domainSource(ctx context.Context, tx pgx.Tx, id int64) (domainEventSource, error) {
	var p domainEventSource
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT id,resource_id,actor_id,actor_name,changes,notice_source_transaction::text,created_at FROM backoffice_audit WHERE id=$1 AND resource='domains' AND operation='update' AND actor_role='admin' AND actor_id>0 AND notice_source_transaction IS NOT NULL`, id).Scan(&p.AuditID, &p.DomainID, &p.ActorID, &p.ActorName, &raw, &p.Transaction, &p.At)
	if err != nil {
		return p, err
	}
	var fields map[string]json.RawMessage
	var mid string
	if json.Unmarshal(raw, &fields) != nil || json.Unmarshal(fields["merchant_id"], &mid) != nil || json.Unmarshal(fields["domain"], &p.Domain) != nil || string(fields["approved"]) == "null" || json.Unmarshal(fields["approved"], &p.Approved) != nil {
		return p, ErrConflict
	}
	p.MerchantID, err = strconv.ParseInt(mid, 10, 64)
	if err != nil || p.MerchantID <= 0 || strconv.FormatInt(p.MerchantID, 10) != mid || p.DomainID <= 0 || !safeValue(p.Domain, 253, false) || !safeValue(p.ActorName, 128, false) {
		return p, ErrConflict
	}
	p.At = p.At.UTC()
	p.AuditHash = s.digest("domain-audit", raw)
	return p, nil
}

// Only an explicit administrator decision stamps a new immutable audit with
// the original transaction. Ordinary edits and historical audits cannot send.
func (s *Store) CaptureDomainAudit(ctx context.Context, tx pgx.Tx, id int64) error {
	if !s.available() || ctx == nil || tx == nil || id <= 0 {
		return ErrInvalid
	}
	p, err := s.domainSource(ctx, tx, id)
	if err != nil {
		return err
	}
	var current string
	if err = tx.QueryRow(ctx, `SELECT pg_current_xact_id()::text`).Scan(&current); err != nil {
		return err
	}
	if p.Transaction != current {
		return ErrConflict
	}
	var matches bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_records WHERE id=$1 AND resource='domains' AND owner_id=$2 AND body->>'domain'=$3 AND body->'approved'=to_jsonb($4::boolean))`, p.DomainID, p.MerchantID, p.Domain, p.Approved).Scan(&matches); err != nil {
		return err
	}
	if !matches {
		return ErrConflict
	}
	enabled, _, err := noticeMerchant(ctx, tx, p.MerchantID, false)
	if err != nil {
		return err
	}
	result := "0"
	if p.Approved {
		result = "1"
	}
	in := Input{Scene: "domain_audit", At: p.At, Params: map[string]string{"domain": p.Domain, "result": result}}
	return s.appendEvent(ctx, tx, eventRecord{MerchantID: p.MerchantID, DomainAuditID: p.AuditID, Scene: "domain_audit"}, eventData{Version: 1, Domain: &p, Input: in}, enabled)
}
func (s *Store) verifyDomainSource(ctx context.Context, tx pgx.Tx, r eventRecord, data eventData) error {
	if r.DomainAuditID <= 0 || r.OrderID != 0 || r.LoginAuditID != 0 || r.SettlementID != 0 || r.TimerID != 0 || data.Domain == nil || data.Login != nil || data.Settlement != nil || data.Timer != nil || data.Source != (paidEventSource{}) {
		return ErrConflict
	}
	p, err := s.domainSource(ctx, tx, r.DomainAuditID)
	if err != nil {
		return ErrConflict
	}
	a, _ := json.Marshal(p)
	b, _ := json.Marshal(data.Domain)
	result := "0"
	if p.Approved {
		result = "1"
	}
	if p.MerchantID != r.MerchantID || p.Transaction != r.Transaction || !hmac.Equal(a, b) || !p.At.Equal(data.Input.At) || p.Domain != data.Input.Params["domain"] || result != data.Input.Params["result"] {
		return ErrConflict
	}
	return nil
}
