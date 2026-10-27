package payment

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	notice "goepay/internal/merchantnotice"
)

type scheduledTaskSettlementSlotKey struct{}
type scheduledTaskSettlementExpected struct{ PlanID, Slot int64 }

func (s *Store) scheduledSettlementSnapshot(ctx context.Context, tx pgx.Tx, limit int, now time.Time) ([]scheduledSettlementSlot, error) {
	return s.scheduledSettlementSnapshotFor(ctx, tx, "settlement_generate", limit, now)
}
func (s *Store) scheduledSettlementSnapshotFor(ctx context.Context, tx pgx.Tx, task string, limit int, now time.Time) ([]scheduledSettlementSlot, error) {
	out := []scheduledSettlementSlot{}
	mode := "generate_only"
	if task == "transfer" {
		mode = settlementThresholdMode
	}
	for after := int64(0); len(out) < limit; {
		ids, e := settlementPlanIDs(ctx, tx, `SELECT p.id FROM settlement_plans p JOIN settlement_plan_versions v ON(v.plan_id,v.revision)=(p.id,p.revision) JOIN settlement_plan_authorizations a ON a.id=p.last_authorization_id WHERE p.id>$1 AND p.enabled AND v.mode=$3 AND a.valid_from<=$2 AND a.expires_at>$2 AND NOT EXISTS(SELECT 1 FROM settlement_plan_revocations r WHERE r.authorization_id=a.id) ORDER BY p.id LIMIT 100`, after, now, mode)
		if e != nil {
			return nil, e
		}
		for _, id := range ids {
			after = id
			p, e := s.readSettlementPlan(ctx, tx, id)
			if e != nil {
				return nil, e
			}
			if p.Input.Mode != mode {
				return nil, ErrConflict
			}
			slot, _, e := dueSettlementSlot(p.Input.Schedule, now)
			if errors.Is(e, ErrState) {
				continue
			}
			if e != nil {
				return nil, e
			}
			var runID int64
			e = tx.QueryRow(ctx, `SELECT id FROM settlement_plan_runs WHERE plan_id=$1 AND slot_no=$2`, id, slot).Scan(&runID)
			if e == nil {
				v, e := s.settlementPlanRunView(ctx, tx, runID)
				if e != nil {
					return nil, e
				}
				if v.PlanRevision != p.View.Revision || task == "settlement_generate" && (v.State == "completed" || v.State == "blocked" || v.State == "partial") {
					continue
				}
			} else if !errors.Is(e, pgx.ErrNoRows) {
				return nil, e
			}
			autoID := int64(0)
			if task == "transfer" || task == "settle" {
				current, err := s.currentSettlementAuto(ctx, tx, id)
				if err == nil {
					autoID = current.View.ID
				} else if !errors.Is(err, ErrNotFound) {
					return nil, err
				}
			}
			out = append(out, scheduledSettlementSlot{id, p.View.Revision, slot, autoID})
			if len(out) == limit {
				break
			}
		}
		if len(ids) < 100 {
			return out, nil
		}
		if e := ctx.Err(); e != nil {
			return nil, e
		}
	}
	return out, nil
}
func (s *Store) executeScheduledTask(ctx context.Context, r scheduledTaskRun) (ScheduledTaskCounts, error) {
	var counts ScheduledTaskCounts
	switch r.facts.Policy.Task {
	case "check":
		return s.executeScheduledCoreRiskCheck(ctx, r)
	case "transfer", "settle":
		return s.executeScheduledAutomaticSettlement(ctx, r)
	case "notify", "order_query", "profitsharing":
		return s.admitScheduledJobs(ctx, r)
	case "daily_report", "balance_notice", "order":
		counts = r.progressCounts
		scene, hour := "balance", 0
		if r.facts.Policy.Task == "daily_report" {
			scene = "daily_report"
			hour = r.facts.Policy.Params["send_hour"]
		}
		if r.facts.Policy.Task == "order" {
			scene = "group"
		}
		for index := r.progress; index < int64(len(r.facts.Merchants)); index++ {
			if e := ctx.Err(); e != nil {
				return counts, e
			}
			recorded, e := s.notice.GenerateScheduledSceneMerchant(ctx, r.facts.Merchants[index], hour, s, scene, r.facts.BusinessDate)
			if e != nil {
				if errors.Is(e, notice.ErrState) {
					return counts, ErrState
				}
				return counts, e
			}
			counts.Examined++
			if recorded {
				counts.Enqueued++
			}
			if e = s.checkpointScheduledRun(ctx, r, index+1, counts); e != nil {
				return counts, e
			}
			r.progress = index + 1
			r.progressCounts = counts
		}
		if r.facts.Policy.Task == "order" {
			// The existing funded engine owns eligibility, refunds, journals and
			// per-order idempotence. A separately configured referral policy wins.
			for batch := 0; batch < 100; batch++ {
				tx, e := s.pool.Begin(ctx)
				if e != nil {
					return counts, e
				}
				allowed, e := s.legacyScheduledTaskAllowedTx(ctx, tx, "referral_daily")
				tx.Rollback(ctx)
				if e != nil {
					return counts, e
				}
				if !allowed {
					break
				}
				value, e := s.TickReferralDaily(ctx, r.facts.BusinessDate, 100)
				counts.Examined += value.Examined
				counts.Awarded += value.Awarded
				counts.Skipped += value.Skipped
				if e != nil {
					return counts, e
				}
				if value.Examined < 100 || value.Awarded == 0 {
					break
				}
				if batch == 99 {
					return counts, ErrState
				}
			}
		}
		return counts, nil
	case "referral_daily":
		value, e := s.TickReferralDaily(ctx, r.facts.BusinessDate, r.facts.Policy.Params["limit"])
		counts.Examined, counts.Awarded, counts.Skipped = value.Examined, value.Awarded, value.Skipped
		return counts, e
	case "settlement_generate":
		for _, slot := range r.facts.Plans {
			if e := ctx.Err(); e != nil {
				return counts, e
			}
			var runID int64
			e := s.pool.QueryRow(ctx, `SELECT id FROM settlement_plan_runs WHERE plan_id=$1 AND slot_no=$2`, slot.PlanID, slot.Slot).Scan(&runID)
			if errors.Is(e, pgx.ErrNoRows) {
				guarded := context.WithValue(ctx, scheduledTaskSettlementSlotKey{}, scheduledTaskSettlementExpected{slot.PlanID, slot.Slot})
				created, e := s.createSettlementPlanRun(guarded, slot.PlanID, slot.Revision, nil)
				if e != nil {
					return counts, e
				}
				runID = created.ID
			} else if e != nil {
				return counts, e
			}
			original, e := s.readSettlementPlanRun(ctx, s.pool, runID)
			if e != nil {
				return counts, e
			}
			if original.View.PlanID != slot.PlanID || original.View.PlanRevision != slot.Revision || original.View.SlotNo != slot.Slot || original.View.BusinessDate != r.facts.BusinessDate {
				return counts, ErrConflict
			}
			authority, e := s.readSettlementPlanAuthorization(ctx, s.pool, original.View.AuthorizationID)
			if e != nil {
				return counts, e
			}
			definition, e := s.readSettlementPlanDefinition(ctx, s.pool, authority.Scope.DefinitionID)
			if e != nil {
				return counts, e
			}
			if definition.Input.Mode != "generate_only" {
				return counts, ErrConflict
			}
			if e = s.GenerateSettlementPlanRun(ctx, runID); e != nil {
				return counts, e
			}
			view, e := s.settlementPlanRunView(ctx, s.pool, runID)
			if e != nil {
				return counts, e
			}
			counts.Examined++
			counts.Generated += int(view.CreatedCount)
			counts.Skipped += int(view.SkippedCount + view.BlockedCount)
		}
		return counts, nil
	default:
		return counts, ErrInvalid
	}
}
