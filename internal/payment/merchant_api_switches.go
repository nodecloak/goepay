package payment

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"goepay/internal/identity"
)

var ErrMerchantAPIDisabled = errors.New("merchant API disabled")
var ErrMerchantAPIAdmission = errors.New("merchant API admission could not be confirmed")

type MerchantAPISwitches struct {
	MerchantID      int64  `json:"merchant_id,string"`
	KeyLogin        int    `json:"keylogin"`
	Refund          int    `json:"refund"`
	Transfer        int    `json:"transfer"`
	Configured      bool   `json:"configured"`
	Revision        int64  `json:"revision,string"`
	Fingerprint     string `json:"fingerprint"`
	CanUpdate       bool   `json:"can_update"`
	TransferGranted bool   `json:"transfer_granted"`
}
type merchantAPIAccount struct {
	ID, Revision             int64
	Enabled, TransferGranted bool
	Profile, KeyCipher       []byte
	Algorithm, PublicKey     string
	Keytype                  *int
}
type merchantAPIPolicy struct {
	MerchantID                 int64
	KeyLogin, Refund, Transfer int
	Revision                   int64
	Proof                      []byte
}

func apiSwitch(v int) bool { return v == 0 || v == 1 }
func (s *Store) merchantAPIAccountTx(ctx context.Context, tx pgx.Tx, mid int64, write bool) (merchantAPIAccount, error) {
	var m merchantAPIAccount
	lock := " FOR SHARE"
	if write {
		lock = " FOR NO KEY UPDATE"
	}
	e := tx.QueryRow(ctx, `SELECT id,config_revision,enabled,profile,api_key,signature_algorithm,rsa_public_key,keytype FROM merchants WHERE id=$1`+lock, mid).Scan(&m.ID, &m.Revision, &m.Enabled, &m.Profile, &m.KeyCipher, &m.Algorithm, &m.PublicKey, &m.Keytype)
	if errors.Is(e, pgx.ErrNoRows) {
		return m, ErrNotFound
	}
	if e != nil {
		return m, e
	}
	var profile map[string]json.RawMessage
	if json.Unmarshal(m.Profile, &profile) != nil || profile == nil || m.Revision <= 0 {
		return m, ErrState
	}
	if raw, ok := profile["transfer_api_enabled"]; ok {
		if string(raw) == "null" || json.Unmarshal(raw, &m.TransferGranted) != nil {
			return m, ErrState
		}
	}
	return m, nil
}
func (s *Store) merchantAPISchema(ctx context.Context, q referralReader) (bool, error) {
	var table, migrations, audit bool
	if e := q.QueryRow(ctx, `SELECT to_regclass(current_schema()||'.merchant_api_switches') IS NOT NULL,to_regclass(current_schema()||'.schema_migrations') IS NOT NULL,to_regclass(current_schema()||'.backoffice_audit') IS NOT NULL`).Scan(&table, &migrations, &audit); e != nil {
		return false, e
	}
	if table {
		return true, nil
	}
	var origin bool
	if migrations {
		if e := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name='000101_merchant_api_switches.up.sql')`).Scan(&origin); e != nil {
			return false, e
		}
		if origin {
			return false, ErrConflict
		}
	}
	if audit {
		if e := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='merchant-api-switches' AND operation='merchant-api-switches-update')`).Scan(&origin); e != nil {
			return false, e
		}
		if origin {
			return false, ErrConflict
		}
	}
	return false, nil
}
func (s *Store) merchantAPIPolicyProof(p merchantAPIPolicy) ([]byte, error) {
	return s.referralDigest("merchant-api174/policy", []any{p.MerchantID, p.KeyLogin, p.Refund, p.Transfer, p.Revision})
}
func (s *Store) merchantAPIPolicyTx(ctx context.Context, tx pgx.Tx, m merchantAPIAccount, write bool) (merchantAPIPolicy, bool, error) {
	p := merchantAPIPolicy{MerchantID: m.ID, KeyLogin: 1, Refund: 1}
	if m.TransferGranted {
		p.Transfer = 1
	}
	ready, e := s.merchantAPISchema(ctx, tx)
	if e != nil {
		return p, false, e
	}
	if !ready {
		return p, false, nil
	}
	lock := " FOR SHARE"
	if write {
		lock = " FOR UPDATE"
	}
	e = tx.QueryRow(ctx, `SELECT keylogin,refund,transfer,revision,proof_hash FROM merchant_api_switches WHERE merchant_id=$1`+lock, m.ID).Scan(&p.KeyLogin, &p.Refund, &p.Transfer, &p.Revision, &p.Proof)
	if errors.Is(e, pgx.ErrNoRows) {
		var origin bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='merchant-api-switches' AND resource_id=$1 AND operation='merchant-api-switches-update')`, m.ID).Scan(&origin); e != nil {
			return p, false, e
		}
		if origin {
			return p, false, ErrConflict
		}
		return p, false, nil
	}
	if e != nil {
		return p, false, e
	}
	proof, e := s.merchantAPIPolicyProof(p)
	if e != nil {
		return p, false, e
	}
	if !apiSwitch(p.KeyLogin) || !apiSwitch(p.Refund) || !apiSwitch(p.Transfer) || p.Revision <= 0 || !hmac.Equal(p.Proof, proof) {
		return p, false, ErrConflict
	}
	// A valid old MAC authenticates old authority too. The independently
	// append-only audit advances exactly once with every committed policy save,
	// under the same merchant lock; unrelated config revisions do not advance it.
	// Its existing (resource,resource_id,id) index scopes this count to one owner.
	var committed int64
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE resource='merchant-api-switches' AND resource_id=$1 AND operation='merchant-api-switches-update'`, m.ID).Scan(&committed); e != nil {
		return p, false, e
	}
	if committed != p.Revision {
		return p, false, ErrConflict
	}
	return p, true, nil
}
func (s *Store) merchantAPIDTO(m merchantAPIAccount, p merchantAPIPolicy, configured bool) (MerchantAPISwitches, error) {
	out := MerchantAPISwitches{MerchantID: m.ID, KeyLogin: p.KeyLogin, Refund: p.Refund, Transfer: p.Transfer, Configured: configured, Revision: p.Revision, CanUpdate: true, TransferGranted: m.TransferGranted}
	proof, e := s.referralDigest("merchant-api174/state", []any{m.ID, m.Revision, m.Enabled, json.RawMessage(m.Profile), m.KeyCipher, m.Algorithm, m.PublicKey, m.Keytype, configured, p.KeyLogin, p.Refund, p.Transfer, p.Revision, p.Proof})
	if e != nil {
		return out, e
	}
	out.Fingerprint = hex.EncodeToString(proof)
	return out, nil
}
func (s *Store) MerchantAPISwitches(ctx context.Context, a MerchantRSAActor) (MerchantAPISwitches, error) {
	var out MerchantAPISwitches
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	m, e := s.merchantAPIAccountTx(ctx, tx, a.User.ID, false)
	if e != nil {
		return out, e
	}
	if !m.Enabled {
		return out, identity.ErrOriginalMerchantSession
	}
	p, configured, e := s.merchantAPIPolicyTx(ctx, tx, m, false)
	if e != nil {
		return out, e
	}
	if e = identity.CheckOriginalMerchantSessionTx(ctx, tx, a.User, a.Token, a.CSRF); e != nil {
		return out, e
	}
	out, e = s.merchantAPIDTO(m, p, configured)
	if e != nil {
		return out, e
	}
	if e = identity.CheckOriginalMerchantSessionTx(ctx, tx, a.User, a.Token, a.CSRF); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func (s *Store) UpdateMerchantAPISwitches(ctx context.Context, a MerchantRSAActor, keylogin, refund, transfer int, expected string) (MerchantAPISwitches, error) {
	var out MerchantAPISwitches
	if !apiSwitch(keylogin) || !apiSwitch(refund) || !apiSwitch(transfer) {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	m, e := s.merchantAPIAccountTx(ctx, tx, a.User.ID, true)
	if e != nil {
		return out, e
	}
	if !m.Enabled {
		return out, identity.ErrOriginalMerchantSession
	}
	p, configured, e := s.merchantAPIPolicyTx(ctx, tx, m, true)
	if e != nil {
		return out, e
	}
	if e = identity.CheckOriginalMerchantSessionTx(ctx, tx, a.User, a.Token, a.CSRF); e != nil {
		return out, e
	}
	before, e := s.merchantAPIDTO(m, p, configured)
	if e != nil {
		return out, e
	}
	if len(expected) != 64 || !hmac.Equal([]byte(expected), []byte(before.Fingerprint)) {
		return out, ErrConflict
	}
	p.KeyLogin, p.Refund, p.Transfer = keylogin, refund, transfer
	p.Revision++
	p.Proof, e = s.merchantAPIPolicyProof(p)
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO merchant_api_switches(merchant_id,keylogin,refund,transfer,revision,proof_hash) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(merchant_id) DO UPDATE SET keylogin=EXCLUDED.keylogin,refund=EXCLUDED.refund,transfer=EXCLUDED.transfer,revision=EXCLUDED.revision,proof_hash=EXCLUDED.proof_hash`, m.ID, p.KeyLogin, p.Refund, p.Transfer, p.Revision, p.Proof); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `UPDATE merchants SET config_revision=config_revision+1 WHERE id=$1`, m.ID); e != nil {
		return out, e
	}
	m.Revision++
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('merchant',$1,$2,'merchant-api-switches',$1,'merchant-api-switches-update',jsonb_build_object('keylogin',$3::int,'refund',$4::int,'transfer',$5::int))`, m.ID, a.User.Username, keylogin, refund, transfer); e != nil {
		return out, e
	}
	out, e = s.merchantAPIDTO(m, p, true)
	if e != nil {
		return out, e
	}
	if e = identity.CheckOriginalMerchantSessionTx(ctx, tx, a.User, a.Token, a.CSRF); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}

// These checks are consumed only by trusted API new-intent callsites. Console,
// worker, notification and original-result recovery do not become API writes.
func (s *Store) checkMerchantAPIAdmissionTx(ctx context.Context, tx pgx.Tx, m merchantAPIAccount, kind string) error {
	if !m.Enabled {
		return errors.Join(ErrMerchantAPIAdmission, ErrMerchantAPIDisabled)
	}
	if kind == "refund" {
		platform, e := s.platformAccessTx(ctx, tx, false)
		if e != nil {
			return errors.Join(ErrMerchantAPIAdmission, e)
		}
		if platform.UserRefund != "1" {
			return errors.Join(ErrMerchantAPIAdmission, ErrMerchantAPIDisabled)
		}
	}
	p, _, e := s.merchantAPIPolicyTx(ctx, tx, m, false)
	if e != nil {
		return errors.Join(ErrMerchantAPIAdmission, e)
	}
	allowed := false
	switch kind {
	case "refund":
		allowed = p.Refund == 1
	case "transfer":
		allowed = p.Transfer == 1 && m.TransferGranted
	default:
		return ErrInvalid
	}
	if !allowed {
		return errors.Join(ErrMerchantAPIAdmission, ErrMerchantAPIDisabled)
	}
	return nil
}
func (s *Store) CheckMerchantAPI(ctx context.Context, mid int64, kind string) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	m, e := s.merchantAPIAccountTx(ctx, tx, mid, false)
	if e != nil {
		return e
	}
	if e = s.checkMerchantAPIAdmissionTx(ctx, tx, m, kind); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
