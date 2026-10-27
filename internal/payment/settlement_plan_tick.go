package payment

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ScheduleSettlementPlans creates only durable runs and jobs for existing,
// signed, finite authorities. It has no provider or administrator credentials.
func (s *Store) ScheduleSettlementPlans(ctx context.Context) error {
	var now time.Time
	if err := s.pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		return err
	}
	for after := int64(0); ; {
		ids, err := settlementPlanIDs(ctx, s.pool, `SELECT p.id FROM settlement_plans p JOIN settlement_plan_authorizations a ON a.id=p.last_authorization_id WHERE p.id>$1 AND p.enabled AND a.valid_from<=$2 AND a.expires_at>$2 AND NOT EXISTS(SELECT 1 FROM settlement_plan_revocations r WHERE r.authorization_id=a.id) ORDER BY p.id LIMIT 100`, after, now)
		if err != nil {
			return err
		}
		for _, id := range ids {
			after = id
			p, e := s.readSettlementPlan(ctx, s.pool, id)
			if e == nil {
				key := "settle"
				if p.Input.Mode == settlementThresholdMode {
					key = "transfer"
				}
				allowed, err := s.LegacyScheduledTaskAllowed(ctx, key)
				if err != nil {
					return err
				}
				if !allowed {
					continue
				}
				slot, _, due := dueSettlementSlot(p.Input.Schedule, now)
				if due != nil {
					continue
				}
				var queued bool
				e = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM settlement_plan_runs r JOIN jobs j ON j.event_key='settlement-plan-run:'||r.id::text AND j.kind='settlement-plan.generate' AND j.payload=jsonb_build_object('run_id',r.id::text) WHERE r.plan_id=$1 AND r.slot_no=$2)`, id, slot).Scan(&queued)
				if e == nil && queued {
					continue
				}
				if e == nil {
					_, e = s.createSettlementPlanRun(ctx, id, p.View.Revision, nil)
				}
			}
			// An ineligible or concurrently changed definition cannot stop other
			// independent plans. Corruption remains visible to CheckLedger.
			if e != nil && !errors.Is(e, ErrState) && !errors.Is(e, ErrConflict) && !errors.Is(e, ErrInvalid) && !errors.Is(e, ErrNotFound) {
				return e
			}
		}
		if len(ids) < 100 {
			return nil
		}
		if err = ctx.Err(); err != nil {
			return fmt.Errorf("settlement scheduling interrupted: %w", err)
		}
	}
}
