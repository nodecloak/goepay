package payment

import (
	"context"
	"crypto/hmac"
	"fmt"
	"github.com/jackc/pgx/v5"
)

// Check's original run, lease and current configuration are validated inside the
// same transaction that writes risk actions, notice intents and the final audit.
// Holding the original rows also serializes an administrator's disable/change.
func (s *Store) executeScheduledCoreRiskCheck(ctx context.Context, r scheduledTaskRun) (ScheduledTaskCounts, error) {
	guard := func(ctx context.Context, tx pgx.Tx) error {
		var one int
		if e := tx.QueryRow(ctx, `SELECT 1 FROM scheduled_task_runs WHERE id=$1 FOR UPDATE`, r.id).Scan(&one); e != nil {
			return ErrConflict
		}
		actual, e := s.readScheduledRun(ctx, tx, r.id)
		if e != nil {
			return e
		}
		if actual.facts.Policy.Task != "check" || actual.state != "running" || actual.epoch != r.epoch || !hmac.Equal(actual.token, r.token) || !hmac.Equal(actual.proof, r.proof) || actual.until == nil {
			return ErrConflict
		}
		policy, configured, e := s.scheduledPolicy(ctx, tx, "check", false)
		if e != nil {
			return e
		}
		if !configured || !policy.Enabled || policy.Generation != r.facts.Policy.Generation {
			return ErrConflict
		}
		now, e := scheduledDBNow(ctx, tx)
		if e != nil {
			return e
		}
		if !now.Before(*actual.until) {
			return ErrConflict
		}
		return nil
	}
	result, e := s.RunCoreRiskCheck(WithCoreRiskExecutionGuard(ctx, guard), fmt.Sprintf("scheduled-check:%d", r.id))
	if e != nil {
		return ScheduledTaskCounts{}, e
	}
	return ScheduledTaskCounts{Examined: result.Scanned, Generated: result.Actions}, nil
}
