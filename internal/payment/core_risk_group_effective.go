package payment

import (
	"context"
	"crypto/sha256"
	"github.com/jackc/pgx/v5"
	"goepay/internal/merchantpolicy"
	"goepay/internal/riskpolicy"
	"time"
)

type coreRiskGroupSource struct {
	GroupID, Revision          int64
	Config                     map[string]string
	PolicyProof, GroupBodyHash []byte
	ValidUntil                 time.Time
}

func (s *Store) coreRiskGroupEffectiveTx(ctx context.Context, tx pgx.Tx, mid int64, global riskpolicy.Settings, globalConfigured bool) (riskpolicy.Settings, *coreRiskGroupSource, error) {
	var exists bool
	if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core_payment_risk_groups)OR EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='core-payment-risk-groups'AND operation='configure')`).Scan(&exists); e != nil {
		return nil, nil, e
	}
	if !globalConfigured && !exists {
		return global, nil, nil
	}
	g, e := merchantpolicy.ReadEffectiveGroup(ctx, tx, mid, true)
	if e != nil {
		return nil, nil, e
	}
	p, _, e := s.coreRiskGroupPolicyTx(ctx, tx, g.ID, false)
	if e != nil {
		return nil, nil, e
	}
	effective := riskpolicy.Settings{}
	for k, v := range global {
		effective[k] = v
	}
	if value := p.Config["pay_block_oversea"]; value != "" {
		effective["pay_block_oversea"] = value
	}
	if value := p.Config["check_pay_regoin"]; value != "" {
		effective["check_pay_regoin"] = value
	}
	hash := sha256.Sum256(g.Body)
	source := &coreRiskGroupSource{GroupID: g.ID, Revision: p.Revision, Config: p.Config, PolicyProof: p.Proof, GroupBodyHash: hash[:], ValidUntil: g.ValidUntil}
	return effective, source, nil
}
