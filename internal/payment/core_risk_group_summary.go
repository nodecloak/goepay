package payment

import (
	"context"
	"github.com/jackc/pgx/v5"
)

type CoreRiskGroupSummary struct {
	Configured          bool              `json:"configured"`
	Config              map[string]string `json:"config"`
	EffectiveScanRegion string            `json:"effective_scan_region"`
	EffectiveOversea    string            `json:"effective_oversea"`
	Revision            int64             `json:"revision,string"`
}

// Only decorates group IDs already selected by an authorized resource query.
func (s *Store) CoreRiskGroupSummaries(ctx context.Context, ids []int64) (map[int64]CoreRiskGroupSummary, error) {
	if len(ids) > 1000 {
		return nil, ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	ready, e := s.coreRiskSchema(ctx, tx)
	if e != nil {
		return nil, e
	}
	out := map[int64]CoreRiskGroupSummary{}
	var global coreRiskPolicy
	if ready {
		global, _, e = s.coreRiskPolicyTx(ctx, tx, false)
		if e != nil {
			return nil, e
		}
	}
	for _, id := range ids {
		if id < 0 {
			return nil, ErrInvalid
		}
		v := CoreRiskGroupSummary{Config: map[string]string{"pay_block_oversea": "", "check_pay_regoin": ""}, EffectiveOversea: "0", EffectiveScanRegion: "0"}
		if ready {
			p, configured, e := s.coreRiskGroupPolicyTx(ctx, tx, id, false)
			if e != nil {
				return nil, e
			}
			v.Configured = configured
			v.Config = p.Config
			v.Revision = p.Revision
			v.EffectiveOversea = global.Settings["pay_block_oversea"]
			v.EffectiveScanRegion = global.Settings["check_pay_regoin"]
			if value := p.Config["check_pay_regoin"]; value != "" {
				v.EffectiveScanRegion = value
			}
			if value := p.Config["pay_block_oversea"]; value != "" {
				v.EffectiveOversea = value
			}
		}
		out[id] = v
	}
	return out, tx.Commit(ctx)
}
