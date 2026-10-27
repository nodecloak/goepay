package payment

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"math"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"goepay/internal/identity"
	"goepay/internal/riskpolicy"
)

type CoreRiskSettings = riskpolicy.Settings
type CoreRiskView struct {
	GeoConfigured bool             `json:"geo_configured"`
	Configured    bool             `json:"configured"`
	Revision      int64            `json:"revision,string"`
	Fingerprint   string           `json:"fingerprint"`
	Settings      CoreRiskSettings `json:"settings"`
}
type coreRiskPolicy struct {
	Revision int64
	Settings CoreRiskSettings
	SavedAt  time.Time
	Proof    []byte
}

func (s *Store) coreRiskPolicyProof(p coreRiskPolicy) ([]byte, error) {
	return s.referralDigest("risk102/policy", []any{p.Revision, p.Settings, p.SavedAt.UTC().Format(time.RFC3339Nano)})
}
func (s *Store) coreRiskPolicyTx(ctx context.Context, tx pgx.Tx, write bool) (coreRiskPolicy, bool, error) {
	p := coreRiskPolicy{Settings: riskpolicy.Defaults()}
	var raw []byte
	suffix := " FOR SHARE"
	if write {
		suffix = " FOR UPDATE"
	}
	e := tx.QueryRow(ctx, `SELECT revision,settings,saved_at,proof_hash FROM core_payment_risk_policy WHERE id=1`+suffix).Scan(&p.Revision, &raw, &p.SavedAt, &p.Proof)
	if e == pgx.ErrNoRows {
		var origin bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='core-payment-risk' AND resource_id=1 AND operation='configure')`).Scan(&origin); e != nil {
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
	if json.Unmarshal(raw, &p.Settings) != nil {
		return p, false, ErrConflict
	}
	canonical, e := riskpolicy.Normalize(p.Settings)
	if e != nil {
		return p, false, ErrConflict
	}
	a, _ := json.Marshal(canonical)
	b, _ := json.Marshal(p.Settings)
	if !hmac.Equal(a, b) {
		return p, false, ErrConflict
	}
	p.SavedAt = p.SavedAt.UTC()
	proof, e := s.coreRiskPolicyProof(p)
	if e != nil || !hmac.Equal(proof, p.Proof) {
		return p, false, ErrConflict
	}
	var commits int64
	var ordered bool
	if e = tx.QueryRow(ctx, `SELECT count(*),COALESCE(bool_and(changes->>'revision'=ordinal::text),true)FROM(SELECT changes,row_number()OVER(ORDER BY id)ordinal FROM backoffice_audit WHERE resource='core-payment-risk' AND resource_id=1 AND operation='configure' AND actor_role='admin' AND actor_id>0)a`).Scan(&commits, &ordered); e != nil {
		return p, false, e
	}
	if !ordered || commits != p.Revision {
		return p, false, ErrConflict
	}
	return p, true, nil
}
func (s *Store) coreRiskView(p coreRiskPolicy, configured bool) (CoreRiskView, error) {
	v := CoreRiskView{Configured: configured, Revision: p.Revision, Settings: p.Settings, GeoConfigured: s.coreRiskGeoAvailable()}
	fp, e := s.referralDigest("risk102/policy-fingerprint", []any{configured, p.Revision, p.Settings, p.SavedAt.UTC().Format(time.RFC3339Nano)})
	if e != nil {
		return v, e
	}
	v.Fingerprint = hex.EncodeToString(fp)
	return v, nil
}
func (s *Store) CoreRiskPolicy(ctx context.Context, a MerchantCredentialActor) (CoreRiskView, error) {
	var out CoreRiskView
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.SessionToken, a.Delegated); e != nil {
		return out, e
	}
	p, configured, e := s.coreRiskPolicyTx(ctx, tx, false)
	if e != nil {
		return out, e
	}
	out, e = s.coreRiskView(p, configured)
	if e != nil {
		return out, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.SessionToken, a.Delegated); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}

func (s *Store) UpdateCoreRiskPolicy(ctx context.Context, a MerchantCredentialActor, settings CoreRiskSettings, expected string) (CoreRiskView, error) {
	var out CoreRiskView
	normalized, e := riskpolicy.Normalize(settings)
	if e != nil {
		return out, ErrInvalid
	}
	if coreRiskNeedsGeo(normalized) && !s.coreRiskGeoAvailable() {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('goepay/core-payment-risk-policy',0))`); e != nil {
		return out, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.SessionToken, a.Delegated); e != nil {
		return out, e
	}
	p, configured, e := s.coreRiskPolicyTx(ctx, tx, true)
	if e != nil {
		return out, e
	}
	old, e := s.coreRiskView(p, configured)
	if e != nil {
		return out, e
	}
	if len(expected) != 64 || !hmac.Equal([]byte(old.Fingerprint), []byte(expected)) {
		return out, ErrConflict
	}
	if p.Revision == math.MaxInt64 {
		return out, ErrState
	}
	p.Revision++
	p.Settings = normalized
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&p.SavedAt); e != nil {
		return out, e
	}
	p.SavedAt = p.SavedAt.UTC()
	p.Proof, e = s.coreRiskPolicyProof(p)
	if e != nil {
		return out, e
	}
	raw, e := json.Marshal(p.Settings)
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO core_payment_risk_policy(id,revision,settings,saved_at,proof_hash)VALUES(1,$1,$2::jsonb,$3,$4)ON CONFLICT(id)DO UPDATE SET revision=EXCLUDED.revision,settings=EXCLUDED.settings,saved_at=EXCLUDED.saved_at,proof_hash=EXCLUDED.proof_hash`, p.Revision, string(raw), p.SavedAt, p.Proof); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('admin',$1,$2,'core-payment-risk',1,'configure',jsonb_build_object('revision',$3::text))`, a.User.ID, a.User.Username, strconv.FormatInt(p.Revision, 10)); e != nil {
		return out, e
	}
	// This must follow the last policy/audit write, including any trigger wait.
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.SessionToken, a.Delegated); e != nil {
		return out, e
	}
	out, e = s.coreRiskView(p, true)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
