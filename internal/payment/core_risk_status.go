package payment

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"time"
)

type CoreRiskStatus struct {
	Blocked  bool       `json:"blocked"`
	Reason   string     `json:"reason"`
	Until    *time.Time `json:"until"`
	ActionID int64      `json:"action_id,string"`
	Revision int64      `json:"revision,string"`
}

func coreRiskReason(rule string) string {
	switch rule {
	case "auto_check_payip":
		return "IP 连续订单未支付"
	case "auto_check_payspeed":
		return "买家支付频率超出规则"
	case "auto_check_sucrate":
		return "商户支付成功率低于规则"
	case "auto_check_channel":
		return "通道连续订单未支付"
	default:
		return "核心付款风控限制"
	}
}
func (s *Store) coreRiskSubjectStatusTx(ctx context.Context, tx pgx.Tx, kind string, id int64) (CoreRiskStatus, error) {
	var out CoreRiskStatus
	ready, e := s.coreRiskSchema(ctx, tx)
	if e != nil || !ready {
		return out, e
	}
	if _, _, e = s.coreRiskPolicyTx(ctx, tx, false); e != nil {
		return out, e
	}
	a, _, e := s.readCoreRiskAction(ctx, tx, kind, fmt.Sprint(id))
	if e != nil {
		return out, e
	}
	now, e := coreRiskDBNow(ctx, tx)
	if e != nil {
		return out, e
	}
	if a == nil {
		return out, nil
	}
	out.Revision = a.Revision
	if e = tx.QueryRow(ctx, `SELECT id FROM core_risk_actions WHERE kind=$1 AND subject_key=$2 AND revision=$3`, kind, fmt.Sprint(id), a.Revision).Scan(&out.ActionID); e != nil {
		return out, e
	}
	out.Blocked = coreRiskActive(a, now)
	if out.Blocked {
		out.Reason = coreRiskReason(a.Rule)
		out.Until = a.Until
	}
	return out, nil
}

// The caller must authorize the already selected, bounded resource rows. This
// returns only effective blocking information, never private subjects or claims.
func (s *Store) CoreRiskSubjectStatuses(ctx context.Context, kind string, ids []int64) (map[int64]CoreRiskStatus, error) {
	if kind != "merchant" && kind != "channel" || len(ids) > 1000 {
		return nil, ErrInvalid
	}
	for _, id := range ids {
		if id < 1 {
			return nil, ErrInvalid
		}
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	out, e := s.coreRiskSubjectStatusesTx(ctx, tx, kind, ids)
	if e != nil {
		return nil, e
	}
	return out, tx.Commit(ctx)
}
