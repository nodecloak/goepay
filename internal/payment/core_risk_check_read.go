package payment

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
)

func (s *Store) CoreRiskCheckResult(ctx context.Context, actor MerchantCredentialActor, external string) (CoreRiskCheckResult, error) {
	var out CoreRiskCheckResult
	if len(external) < 8 || len(external) > 96 {
		return out, ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, actor.User, actor.SessionToken, actor.Delegated); e != nil {
		return out, e
	}
	var raw, proof []byte
	e = tx.QueryRow(ctx, `SELECT facts,proof_hash FROM core_risk_check_runs WHERE external_no=$1`, external).Scan(&raw, &proof)
	if e == pgx.ErrNoRows {
		var committed bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='core-risk-check'AND changes->>'external_no'=$1)`, external).Scan(&committed); e != nil {
			return out, e
		}
		if committed {
			return out, ErrConflict
		}
		return out, ErrNotFound
	}
	if e != nil {
		return out, e
	}
	if decodeCoreRiskFacts(raw, &out) != nil || out.ExternalNo != external {
		return out, ErrConflict
	}
	hash, e := s.referralDigest("risk102/check", out)
	if e != nil || !hmac.Equal(hash, proof) {
		return out, ErrConflict
	}
	var valid bool
	if e = tx.QueryRow(ctx, `SELECT count(*)=1 FROM backoffice_audit WHERE resource='core-risk-check' AND operation='complete' AND changes->>'external_no'=$1 AND changes->>'source_hash'=$2 AND (actor_role,actor_id,actor_name)=('system',0,'core-risk')`, external, hex.EncodeToString(hash)).Scan(&valid); e != nil {
		return out, e
	}
	if !valid {
		return out, ErrConflict
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, actor.User, actor.SessionToken, actor.Delegated); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
