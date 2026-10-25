package merchantnotice

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"time"
)

type loginEventSource struct {
	ID, MerchantID, AuthVersion int64
	Username, IP, Transaction   string
	At                          time.Time
}

func loginSource(ctx context.Context, tx pgx.Tx, id int64) (loginEventSource, error) {
	var p loginEventSource
	err := tx.QueryRow(ctx, `SELECT id,subject_id,auth_version,username,ip,source_transaction::text,created_at FROM console_login_audit WHERE id=$1 AND role='merchant' AND success AND subject_id>0 AND auth_version>0 AND source_transaction IS NOT NULL`, id).Scan(&p.ID, &p.MerchantID, &p.AuthVersion, &p.Username, &p.IP, &p.Transaction, &p.At)
	p.At = p.At.UTC()
	return p, err
}

// Only the authenticated server path stamps this fresh audit after password,
// MFA and session creation succeed. Failed and historical audits have no source.
func (s *Store) CaptureLoginAudit(ctx context.Context, tx pgx.Tx, id int64) error {
	if !s.available() || tx == nil || id <= 0 {
		return ErrInvalid
	}
	p, err := loginSource(ctx, tx, id)
	if err != nil {
		return ErrState
	}
	var current string
	var active bool
	if err = tx.QueryRow(ctx, `SELECT pg_current_xact_id()::text,EXISTS(SELECT 1 FROM console_users u JOIN merchants m ON m.id=u.subject_id WHERE u.username=$1 AND u.role='merchant' AND u.subject_id=$2 AND u.auth_version=$3 AND u.enabled AND m.enabled)`, p.Username, p.MerchantID, p.AuthVersion).Scan(&current, &active); err != nil {
		return err
	}
	if current != p.Transaction {
		return ErrConflict
	}
	in := Input{Scene: "login", At: p.At, Params: map[string]string{"user": p.Username, "time": p.At.In(time.FixedZone("UTC+8", 28800)).Format("2006-01-02 15:04:05"), "clientip": p.IP, "ipinfo": "未知"}}
	return s.appendEvent(ctx, tx, eventRecord{MerchantID: p.MerchantID, LoginAuditID: p.ID, Scene: "login"}, eventData{Version: 1, Login: &p, Input: in}, active)
}
func (s *Store) verifyEventSource(ctx context.Context, tx pgx.Tx, r eventRecord, data eventData) error {
	if r.Scene != data.Input.Scene {
		return ErrConflict
	}
	if r.Scene == "domain_audit" {
		return s.verifyDomainSource(ctx, tx, r, data)
	}
	if r.DomainAuditID != 0 || data.Domain != nil {
		return ErrConflict
	}
	if r.Scene == "balance" || r.Scene == "group" || r.Scene == "daily_report" {
		return s.verifyTimerSource(ctx, tx, r, data)
	}
	if r.TimerID != 0 || data.Timer != nil {
		return ErrConflict
	}
	if r.Scene == "settle" {
		return s.verifySettlementSource(ctx, tx, r, data)
	}
	if r.SettlementID != 0 || data.Settlement != nil {
		return ErrConflict
	}
	if r.Scene == "login" {
		if r.OrderID != 0 || r.LoginAuditID <= 0 || data.Login == nil {
			return ErrConflict
		}
		p, err := loginSource(ctx, tx, r.LoginAuditID)
		if err != nil || p.MerchantID != r.MerchantID || p.Transaction != r.Transaction {
			return ErrConflict
		}
		a, _ := json.Marshal(p)
		b, _ := json.Marshal(data.Login)
		if !hmac.Equal(a, b) {
			return ErrConflict
		}
		return nil
	}
	if r.Scene != "order" || r.OrderID <= 0 || r.LoginAuditID != 0 || data.Login != nil {
		return ErrConflict
	}
	p, err := paidSource(ctx, tx, r.OrderID)
	if err != nil || p.MerchantID != r.MerchantID {
		return ErrConflict
	}
	a, _ := json.Marshal(p)
	b, _ := json.Marshal(data.Source)
	if !hmac.Equal(a, b) {
		return ErrConflict
	}
	var valid bool
	if err = tx.QueryRow(ctx, `SELECT ($2::bigint=0 OR EXISTS(SELECT 1 FROM journals WHERE event_key='payment:'||$1 AND transaction_id=$4::xid8)) AND ($3::bigint=0 OR EXISTS(SELECT 1 FROM journals WHERE event_key='payment-fee:'||$1 AND transaction_id=$4::xid8))`, p.TradeNo, p.MerchantAmount, p.PlatformFee, r.Transaction).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return ErrConflict
	}
	return nil
}
