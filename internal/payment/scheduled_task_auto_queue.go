package payment

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"goepay/internal/platform/jobs"
)

type scheduledTaskAutoQueueKey struct{}
type scheduledAutoQueueScopeKey struct{}
type scheduledAutoQueueScope struct{ RunIDs, AuthorityIDs []int64 }

// RunScheduledTasksOnceWithAutoQueue supplies the same controlled provider
// identity used by the original automatic worker. QueueSettlementAuto validates
// the independent authority but performs no provider I/O. Enabling a task never
// creates or substitutes a financial authority.
func (s *Store) RunScheduledTasksOnceWithAutoQueue(ctx context.Context, limit int, flow *ProviderFlow) (ScheduledTaskTick, error) {
	if flow == nil || flow.Store != s || !flow.AllowLive {
		return ScheduledTaskTick{}, ErrInvalid
	}
	return s.RunScheduledTasksOnce(context.WithValue(ctx, scheduledTaskAutoQueueKey{}, flow), limit)
}

func (g *ProviderFlow) VerifyScheduledJobClaim(ctx context.Context, j jobs.Job) error {
	if g == nil || g.Store == nil {
		return ErrInvalid
	}
	return g.Store.VerifyScheduledJobClaim(ctx, j)
}

func (s *Store) executeScheduledAutomaticSettlement(ctx context.Context, r scheduledTaskRun) (ScheduledTaskCounts, error) {
	var counts ScheduledTaskCounts
	flow, _ := ctx.Value(scheduledTaskAutoQueueKey{}).(*ProviderFlow)
	validFlow := flow != nil && flow.Store == s && flow.AllowLive
	if r.facts.Policy.Task == "transfer" && !validFlow {
		return counts, ErrState
	}
	scope := scheduledAutoQueueScope{RunIDs: []int64{}, AuthorityIDs: []int64{}}
	for _, slot := range r.facts.Plans {
		if slot.AutoAuthorizationID <= 0 && r.facts.Policy.Task == "transfer" {
			return counts, ErrState
		}
		var authority settlementAutoAuthorization
		if slot.AutoAuthorizationID > 0 {
			if !validFlow {
				return counts, ErrState
			}
			var e error
			authority, e = s.readSettlementAuto(ctx, s.pool, slot.AutoAuthorizationID)
			if e != nil {
				return counts, e
			}
			if authority.View.PlanID != slot.PlanID || authority.View.PlanRevision != slot.Revision {
				return counts, ErrConflict
			}
			if e = flow.validateSettlementAutoCurrent(ctx, s.pool, authority); e != nil {
				return counts, e
			}
		} else {
			counts.AwaitingAuthorization++
		}
		var runID int64
		e := s.pool.QueryRow(ctx, `SELECT id FROM settlement_plan_runs WHERE plan_id=$1 AND slot_no=$2`, slot.PlanID, slot.Slot).Scan(&runID)
		if errors.Is(e, pgx.ErrNoRows) {
			guarded := context.WithValue(ctx, scheduledTaskSettlementSlotKey{}, scheduledTaskSettlementExpected{slot.PlanID, slot.Slot})
			created, err := s.createSettlementPlanRun(guarded, slot.PlanID, slot.Revision, nil)
			if err != nil {
				return counts, err
			}
			runID = created.ID
		} else if e != nil {
			return counts, e
		}
		original, e := s.readSettlementPlanRun(ctx, s.pool, runID)
		if e != nil {
			return counts, e
		}
		if original.View.PlanID != slot.PlanID || original.View.PlanRevision != slot.Revision || original.View.SlotNo != slot.Slot || original.View.BusinessDate != r.facts.BusinessDate || slot.AutoAuthorizationID > 0 && original.View.AuthorizationID != authority.View.GenerationAuthorizationID {
			return counts, ErrConflict
		}
		generation, e := s.readSettlementPlanAuthorization(ctx, s.pool, original.View.AuthorizationID)
		if e != nil {
			return counts, e
		}
		definition, e := s.readSettlementPlanDefinition(ctx, s.pool, generation.Scope.DefinitionID)
		if e != nil {
			return counts, e
		}
		mode := "generate_only"
		if r.facts.Policy.Task == "transfer" {
			mode = settlementThresholdMode
		}
		if definition.Input.Mode != mode {
			return counts, ErrConflict
		}
		// Original threshold generation freezes balance/payment/recipient/group/fee
		// quotes and cumulative finite budgets in its own atomic source transaction.
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
		scope.RunIDs = append(scope.RunIDs, runID)
		if slot.AutoAuthorizationID > 0 {
			scope.AuthorityIDs = append(scope.AuthorityIDs, slot.AutoAuthorizationID)
		}
	}
	scoped := context.WithValue(ctx, scheduledAutoQueueScopeKey{}, scope)
	if len(scope.AuthorityIDs) > 0 {
		if _, e := flow.QueueSettlementAuto(scoped, 100); e != nil {
			return counts, e
		}
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return counts, e
	}
	defer tx.Rollback(ctx)
	now, e := scheduledDBNow(ctx, tx)
	if e != nil {
		return counts, e
	}
	r.facts.Jobs, e = s.scheduledJobSnapshot(scoped, tx, r.facts.Policy, now)
	if e != nil {
		return counts, e
	}
	if e = tx.Commit(ctx); e != nil {
		return counts, e
	}
	admission, e := s.admitScheduledJobs(ctx, r)
	counts.Enqueued = admission.Enqueued
	return counts, e
}
