package payment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
	"goepay/internal/riskpolicy"
	"math"
	"time"
)

type coreRiskGroupPolicy struct {
	GroupID, Revision int64
	Config            map[string]string
	SavedAt           time.Time
	Proof             []byte
}
type CoreRiskGroupView struct {
	GroupID             int64             `json:"group_id,string"`
	GroupName           string            `json:"group_name"`
	Configured          bool              `json:"configured"`
	Revision            int64             `json:"revision,string"`
	Fingerprint         string            `json:"fingerprint"`
	Config              map[string]string `json:"config"`
	EffectiveScanRegion string            `json:"effective_scan_region"`
	EffectiveOversea    string            `json:"effective_oversea"`
	GeoConfigured       bool              `json:"geo_configured"`
}

func (s *Store) coreRiskGroupProof(p coreRiskGroupPolicy) ([]byte, error) {
	return s.referralDigest("risk102/group-policy", []any{p.GroupID, p.Revision, p.Config, p.SavedAt.UTC().Format(time.RFC3339Nano)})
}
func (s *Store) coreRiskGroupPolicyTx(ctx context.Context, tx pgx.Tx, id int64, write bool) (coreRiskGroupPolicy, bool, error) {
	p := coreRiskGroupPolicy{GroupID: id, Config: map[string]string{"pay_block_oversea": "", "check_pay_regoin": ""}}
	suffix := " FOR SHARE"
	if write {
		suffix = " FOR UPDATE"
	}
	var raw []byte
	e := tx.QueryRow(ctx, `SELECT revision,config,saved_at,proof_hash FROM core_payment_risk_groups WHERE group_id=$1`+suffix, id).Scan(&p.Revision, &raw, &p.SavedAt, &p.Proof)
	if e == pgx.ErrNoRows {
		var origin bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='core-payment-risk-groups' AND resource_id=$1 AND operation='configure')`, id).Scan(&origin); e != nil {
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
	if json.Unmarshal(raw, &p.Config) != nil {
		return p, false, ErrConflict
	}
	normalized, e := riskpolicy.NormalizeGroupConfig(p.Config)
	if e != nil {
		return p, false, ErrConflict
	}
	a, _ := json.Marshal(normalized)
	b, _ := json.Marshal(p.Config)
	if !hmac.Equal(a, b) {
		return p, false, ErrConflict
	}
	p.SavedAt = p.SavedAt.UTC()
	proof, e := s.coreRiskGroupProof(p)
	if e != nil || !hmac.Equal(proof, p.Proof) {
		return p, false, ErrConflict
	}
	var commits int64
	var ordered bool
	if e = tx.QueryRow(ctx, `SELECT count(*),COALESCE(bool_and(changes->>'revision'=ordinal::text),true)FROM(SELECT changes,row_number()OVER(ORDER BY id)ordinal FROM backoffice_audit WHERE resource='core-payment-risk-groups' AND resource_id=$1 AND operation='configure' AND actor_role='admin' AND actor_id>0)a`, id).Scan(&commits, &ordered); e != nil {
		return p, false, e
	}
	if commits != p.Revision || !ordered {
		return p, false, ErrConflict
	}
	return p, true, nil
}
func (s *Store) coreRiskGroupView(ctx context.Context, tx pgx.Tx, p coreRiskGroupPolicy, configured bool, global coreRiskPolicy) (CoreRiskGroupView, error) {
	out := CoreRiskGroupView{GroupID: p.GroupID, Configured: configured, Revision: p.Revision, Config: p.Config, EffectiveOversea: global.Settings["pay_block_oversea"], EffectiveScanRegion: global.Settings["check_pay_regoin"], GeoConfigured: s.coreRiskGeoAvailable()}
	var raw []byte
	if e := tx.QueryRow(ctx, `SELECT body FROM backoffice_records WHERE id=$1 AND resource='groups' FOR SHARE`, p.GroupID).Scan(&raw); e == pgx.ErrNoRows {
		return out, ErrNotFound
	} else if e != nil {
		return out, e
	}
	var body struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &body) != nil || body.Name == "" {
		return out, ErrInvalid
	}
	out.GroupName = body.Name
	if override := p.Config["pay_block_oversea"]; override != "" {
		out.EffectiveOversea = override
	}
	if override := p.Config["check_pay_regoin"]; override != "" {
		out.EffectiveScanRegion = override
	}
	groupHash := sha256.Sum256(raw)
	fp, e := s.referralDigest("risk102/group-view", []any{p.GroupID, configured, p.Revision, p.Config, p.Proof, global.Revision, global.Proof, groupHash[:]})
	if e != nil {
		return out, e
	}
	out.Fingerprint = hex.EncodeToString(fp)
	return out, nil
}
func (s *Store) CoreRiskGroupPolicy(ctx context.Context, actor MerchantCredentialActor, id int64) (CoreRiskGroupView, error) {
	var out CoreRiskGroupView
	if id < 0 {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, actor.User, actor.SessionToken, actor.Delegated); e != nil {
		return out, e
	}
	global, _, e := s.coreRiskPolicyTx(ctx, tx, false)
	if e != nil {
		return out, e
	}
	p, configured, e := s.coreRiskGroupPolicyTx(ctx, tx, id, false)
	if e != nil {
		return out, e
	}
	out, e = s.coreRiskGroupView(ctx, tx, p, configured, global)
	if e != nil {
		return out, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, actor.User, actor.SessionToken, actor.Delegated); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func (s *Store) UpdateCoreRiskGroupPolicy(ctx context.Context, actor MerchantCredentialActor, id int64, config map[string]string, expected string) (CoreRiskGroupView, error) {
	var out CoreRiskGroupView
	normalized, e := riskpolicy.NormalizeGroupConfig(config)
	if e != nil || id < 0 || len(expected) != 64 {
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
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, actor.User, actor.SessionToken, actor.Delegated); e != nil {
		return out, e
	}
	global, _, e := s.coreRiskPolicyTx(ctx, tx, false)
	if e != nil {
		return out, e
	}
	p, configured, e := s.coreRiskGroupPolicyTx(ctx, tx, id, true)
	if e != nil {
		return out, e
	}
	before, e := s.coreRiskGroupView(ctx, tx, p, configured, global)
	if e != nil {
		return out, e
	}
	if !hmac.Equal([]byte(before.Fingerprint), []byte(expected)) {
		return out, ErrConflict
	}
	effective := normalized["pay_block_oversea"]
	if effective == "" {
		effective = global.Settings["pay_block_oversea"]
	}
	scan := normalized["check_pay_regoin"]
	if scan == "" {
		scan = global.Settings["check_pay_regoin"]
	}
	if (effective == "1" || scan != "0") && !s.coreRiskGeoAvailable() {
		return out, ErrInvalid
	}
	if p.Revision == math.MaxInt64 {
		return out, ErrState
	}
	p.Revision++
	p.Config = normalized
	if p.SavedAt, e = coreRiskDBNow(ctx, tx); e != nil {
		return out, e
	}
	p.Proof, e = s.coreRiskGroupProof(p)
	if e != nil {
		return out, e
	}
	raw, _ := json.Marshal(p.Config)
	if _, e = tx.Exec(ctx, `INSERT INTO core_payment_risk_groups(group_id,revision,config,saved_at,proof_hash)VALUES($1,$2,$3::jsonb,$4,$5)ON CONFLICT(group_id)DO UPDATE SET revision=EXCLUDED.revision,config=EXCLUDED.config,saved_at=EXCLUDED.saved_at,proof_hash=EXCLUDED.proof_hash`, id, p.Revision, string(raw), p.SavedAt, p.Proof); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('admin',$1,$2,'core-payment-risk-groups',$3,'configure',jsonb_build_object('revision',$4::text))`, actor.User.ID, actor.User.Username, id, fmt.Sprint(p.Revision)); e != nil {
		return out, e
	}
	out, e = s.coreRiskGroupView(ctx, tx, p, true, global)
	if e != nil {
		return out, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, actor.User, actor.SessionToken, actor.Delegated); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
