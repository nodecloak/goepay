package payment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
)

const scanPolicyResource106 = "merchant-scan-policy106"

type MerchantScanPlatformSettings106 struct {
	Open        string `json:"scanpay_open"`
	Configured  bool   `json:"configured"`
	Revision    string `json:"revision"`
	Fingerprint string `json:"fingerprint"`
	CanUpdate   bool   `json:"can_update"`
}
type merchantScanPolicyFacts106 struct {
	Version         int
	Revision        int64
	Open            string
	LegacyID        int64
	LegacyUpdatedAt time.Time
	ChangedAt       time.Time
	ActorID         int64
	ActorName       string
}
type merchantScanPolicyState106 struct {
	Facts      merchantScanPolicyFacts106
	Proof      []byte
	Configured bool
}

func (s *Store) merchantScanSchema106(ctx context.Context, q fundsReader) error {
	var tables int
	var registry bool
	if err := q.QueryRow(ctx, `SELECT num_nonnulls(to_regclass(current_schema()||'.merchant_scan_registry106'),to_regclass(current_schema()||'.merchant_scan_policies106'),to_regclass(current_schema()||'.merchant_scan_policy_witnesses106'),to_regclass(current_schema()||'.merchant_scan_policy_head106'),to_regclass(current_schema()||'.merchant_scan_intents106'),to_regclass(current_schema()||'.merchant_scan_order_sources106'),to_regclass(current_schema()||'.merchant_scan_closures106')),to_regclass(current_schema()||'.schema_migrations')IS NOT NULL`).Scan(&tables, &registry); err != nil {
		return err
	}
	if tables != 7 {
		return ErrConflict
	}
	if registry {
		var registered bool
		if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name='000106_merchant_scan_cashier.up.sql')`).Scan(&registered); err != nil {
			return err
		}
		if !registered {
			return ErrConflict
		}
	}
	return nil
}

func scanCommitment106(proof []byte) string {
	sum := sha256.Sum256(proof)
	return hex.EncodeToString(sum[:])
}

func (s *Store) merchantScanPolicyTx106(ctx context.Context, tx pgx.Tx, write bool) (merchantScanPolicyState106, error) {
	var out merchantScanPolicyState106
	lock := "pg_advisory_xact_lock_shared"
	if write {
		lock = "pg_advisory_xact_lock"
	}
	if _, err := tx.Exec(ctx, "SELECT "+lock+"($1)", platformSettingsLock); err != nil {
		return out, err
	}
	if err := s.merchantScanSchema106(ctx, tx); err != nil {
		return out, err
	}
	var managed bool
	var rows, witnesses, maximum, witnessMax, audits, auditMax, heads int64
	if err := tx.QueryRow(ctx, `SELECT policy_managed,(SELECT count(*)FROM merchant_scan_policies106),(SELECT count(*)FROM merchant_scan_policy_witnesses106),(SELECT coalesce(max(revision),0)FROM merchant_scan_policies106),(SELECT coalesce(max(revision),0)FROM merchant_scan_policy_witnesses106),(SELECT count(*)FROM backoffice_audit WHERE resource=$1),(SELECT coalesce(max(resource_id),0)FROM backoffice_audit WHERE resource=$1),(SELECT count(*)FROM merchant_scan_policy_head106)FROM merchant_scan_registry106 WHERE singleton`, scanPolicyResource106).Scan(&managed, &rows, &witnesses, &maximum, &witnessMax, &audits, &auditMax, &heads); err != nil {
		return out, ErrConflict
	}
	// A legacy setting is preserved until the explicit protected takeover. It
	// never overrides a managed policy, and malformed/duplicate values deny.
	legacyOpen := "1"
	var legacyID int64
	var legacyTime time.Time
	var valid bool
	err := tx.QueryRow(ctx, `SELECT id,body->>'value',updated_at,body=jsonb_build_object('key','scanpay_open','value',body->>'value') AND jsonb_typeof(body->'value')='string' FROM backoffice_records WHERE resource='settings' AND body->>'key'='scanpay_open'`).Scan(&legacyID, &legacyOpen, &legacyTime, &valid)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return out, ErrConflict
	}
	if err == nil {
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*)FROM backoffice_records WHERE resource='settings' AND body->>'key'='scanpay_open'`).Scan(&count); err != nil || count != 1 || !valid || (legacyOpen != "0" && legacyOpen != "1") {
			return out, ErrConflict
		}
	}
	if !managed {
		if rows != 0 || witnesses != 0 || maximum != 0 || witnessMax != 0 || audits != 0 || auditMax != 0 || heads != 0 {
			return out, ErrConflict
		}
		out.Facts = merchantScanPolicyFacts106{Version: 1, Open: legacyOpen, LegacyID: legacyID, LegacyUpdatedAt: legacyTime.UTC()}
		return out, nil
	}
	if rows <= 0 || rows != maximum || witnesses != rows || witnessMax != maximum || audits != rows || auditMax != maximum || heads != 1 || legacyID == 0 {
		return out, ErrConflict
	}
	var raw, proof, witnessProof []byte
	var revision int64
	if err = tx.QueryRow(ctx, `SELECT p.revision,p.facts,p.proof_hash,w.witness_proof FROM merchant_scan_policy_head106 h JOIN merchant_scan_policies106 p ON p.revision=h.revision JOIN merchant_scan_policy_witnesses106 w ON w.revision=p.revision AND w.policy_proof=p.proof_hash WHERE h.singleton`).Scan(&revision, &raw, &proof, &witnessProof); err != nil || revision != maximum {
		return out, ErrConflict
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 8 {
		return out, ErrConflict
	}
	for _, key := range []string{"Version", "Revision", "Open", "LegacyID", "LegacyUpdatedAt", "ChangedAt", "ActorID", "ActorName"} {
		if _, ok := fields[key]; !ok {
			return out, ErrConflict
		}
	}
	var facts merchantScanPolicyFacts106
	if json.Unmarshal(raw, &facts) != nil || facts.Version != 1 || facts.Revision != revision || (facts.Open != "0" && facts.Open != "1") || facts.LegacyID != legacyID || facts.Open != legacyOpen || !facts.LegacyUpdatedAt.Equal(legacyTime) || facts.ChangedAt.IsZero() || facts.ActorID <= 0 || facts.ActorName == "" {
		return out, ErrConflict
	}
	facts.LegacyUpdatedAt = facts.LegacyUpdatedAt.UTC()
	facts.ChangedAt = facts.ChangedAt.UTC()
	want, err := s.referralDigest("merchant-scan106/policy", facts)
	if err != nil || len(proof) != 32 || !hmac.Equal(want, proof) {
		return out, ErrConflict
	}
	wantWitness, err := s.referralDigest("merchant-scan106/policy-witness", []any{revision, proof})
	if err != nil || !hmac.Equal(wantWitness, witnessProof) {
		return out, ErrConflict
	}
	var matching int
	if err = tx.QueryRow(ctx, `SELECT count(*)FROM backoffice_audit WHERE resource=$1 AND resource_id=$2 AND operation='scanpay-policy-update' AND actor_role='admin' AND actor_id=$3 AND actor_name=$4 AND changes=jsonb_build_object('revision',$2::bigint::text,'scanpay_open',$5::text,'commitment',$6::text)`, scanPolicyResource106, revision, facts.ActorID, facts.ActorName, facts.Open, scanCommitment106(proof)).Scan(&matching); err != nil || matching != 1 {
		return out, ErrConflict
	}
	out = merchantScanPolicyState106{Facts: facts, Proof: proof, Configured: true}
	return out, nil
}

func (s *Store) scanPolicyView106(state merchantScanPolicyState106) (MerchantScanPlatformSettings106, error) {
	proof, err := s.referralDigest("merchant-scan106/policy-cas", []any{state.Configured, state.Facts, state.Proof})
	if err != nil {
		return MerchantScanPlatformSettings106{}, err
	}
	return MerchantScanPlatformSettings106{Open: state.Facts.Open, Configured: state.Configured, Revision: strconv.FormatInt(state.Facts.Revision, 10), Fingerprint: hex.EncodeToString(proof), CanUpdate: true}, nil
}

func (s *Store) MerchantScanPlatformSettings106(ctx context.Context, a MerchantCredentialActor) (MerchantScanPlatformSettings106, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return MerchantScanPlatformSettings106{}, err
	}
	defer tx.Rollback(ctx)
	if err = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.SessionToken, a.Delegated); err != nil {
		return MerchantScanPlatformSettings106{}, err
	}
	state, err := s.merchantScanPolicyTx106(ctx, tx, false)
	if err != nil {
		return MerchantScanPlatformSettings106{}, err
	}
	out, err := s.scanPolicyView106(state)
	if err != nil {
		return out, err
	}
	if err = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.SessionToken, a.Delegated); err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

func (s *Store) UpdateMerchantScanPlatformSettings106(ctx context.Context, a MerchantCredentialActor, open, expected string) (MerchantScanPlatformSettings106, error) {
	var out MerchantScanPlatformSettings106
	if (open != "0" && open != "1") || len(expected) != 64 {
		return out, ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, platformSettingsLock); err != nil {
		return out, err
	}
	if err = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.SessionToken, a.Delegated); err != nil {
		return out, err
	}
	before, err := s.merchantScanPolicyTx106(ctx, tx, true)
	if err != nil {
		return out, err
	}
	view, err := s.scanPolicyView106(before)
	if err != nil || !hmac.Equal([]byte(expected), []byte(view.Fingerprint)) {
		return out, ErrConflict
	}
	var id int64
	var updated, changed time.Time
	if before.Facts.LegacyID == 0 {
		err = tx.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body)VALUES('settings',jsonb_build_object('key','scanpay_open','value',$1::text))RETURNING id,updated_at`, open).Scan(&id, &updated)
	} else {
		err = tx.QueryRow(ctx, `UPDATE backoffice_records SET body=jsonb_build_object('key','scanpay_open','value',$2::text),updated_at=clock_timestamp() WHERE resource='settings' AND id=$1 RETURNING id,updated_at`, before.Facts.LegacyID, open).Scan(&id, &updated)
	}
	if err != nil {
		return out, err
	}
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&changed); err != nil {
		return out, err
	}
	facts := merchantScanPolicyFacts106{Version: 1, Revision: before.Facts.Revision + 1, Open: open, LegacyID: id, LegacyUpdatedAt: updated.UTC(), ChangedAt: changed.UTC(), ActorID: a.User.ID, ActorName: a.User.Username}
	proof, err := s.referralDigest("merchant-scan106/policy", facts)
	if err != nil {
		return out, err
	}
	witness, err := s.referralDigest("merchant-scan106/policy-witness", []any{facts.Revision, proof})
	if err != nil {
		return out, err
	}
	raw, err := json.Marshal(facts)
	if err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO merchant_scan_policies106(revision,facts,proof_hash)VALUES($1,$2::jsonb,$3)`, facts.Revision, string(raw), proof); err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO merchant_scan_policy_witnesses106(revision,policy_proof,witness_proof)VALUES($1,$2,$3)`, facts.Revision, proof, witness); err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO merchant_scan_policy_head106(singleton,revision)VALUES(true,$1)ON CONFLICT(singleton)DO UPDATE SET revision=excluded.revision`, facts.Revision); err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `UPDATE merchant_scan_registry106 SET policy_managed=true WHERE singleton`); err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('admin',$1,$2,$3,$4,'scanpay-policy-update',jsonb_build_object('revision',$4::bigint::text,'scanpay_open',$5::text,'commitment',$6::text))`, a.User.ID, a.User.Username, scanPolicyResource106, facts.Revision, open, scanCommitment106(proof)); err != nil {
		return out, err
	}
	after, err := s.merchantScanPolicyTx106(ctx, tx, true)
	if err != nil {
		return out, err
	}
	out, err = s.scanPolicyView106(after)
	if err != nil {
		return out, err
	}
	if err = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.SessionToken, a.Delegated); err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

// An authenticated capability read is not an admission to send. The future
// intent writer must repeat these checks under its own final-write guard.
func (s *Store) MerchantScanAvailability106(ctx context.Context, a MerchantRSAActor) (bool, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, false, err
	}
	defer tx.Rollback(ctx)
	var enabled bool
	if err = tx.QueryRow(ctx, `SELECT enabled AND pay=1 FROM merchants WHERE id=$1 FOR SHARE`, a.User.ID).Scan(&enabled); err != nil {
		return false, false, ErrState
	}
	if err = identity.CheckOriginalMerchantSessionTx(ctx, tx, a.User, a.Token, a.CSRF); err != nil {
		return false, false, err
	}
	policy, err := s.merchantScanPolicyTx106(ctx, tx, false)
	if err != nil {
		return false, false, err
	}
	if err = identity.CheckOriginalMerchantSessionTx(ctx, tx, a.User, a.Token, a.CSRF); err != nil {
		return false, false, err
	}
	return policy.Facts.Open == "1", enabled, tx.Commit(ctx)
}
