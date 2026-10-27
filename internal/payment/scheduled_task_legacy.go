package payment

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// LegacyScheduledTaskAllowed leaves an unconfigured deployment untouched. Once
// configured, the new durable scheduler owns both enabled and disabled policy;
// an old periodic loop must not ignore the new frequency by doing the same scan.
func (s *Store) LegacyScheduledTaskAllowed(ctx context.Context, key string) (bool, error) {
	if _, ok := scheduledDefinition(key); !ok {
		return false, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return false, e
	}
	defer tx.Rollback(ctx)
	allowed, e := s.legacyScheduledTaskAllowedTx(ctx, tx, key)
	if e != nil {
		return false, e
	}
	if allowed && key == "referral_daily" {
		allowed, e = s.legacyScheduledTaskAllowedTx(ctx, tx, "order")
		if e != nil {
			return false, e
		}
	}
	return allowed, tx.Commit(ctx)
}
func (s *Store) legacyScheduledTaskAllowedTx(ctx context.Context, tx pgx.Tx, key string) (bool, error) {
	ready, e := scheduledAdmissionSchema(ctx, tx)
	if e != nil {
		return false, e
	}
	if !ready {
		return true, nil
	}
	_, configured, e := s.scheduledPolicy(ctx, tx, key, false)
	return !configured, e
}
func (s *Store) ScheduledNoticeSceneLegacyAllowedTx(ctx context.Context, tx pgx.Tx, scene string) (bool, error) {
	key := ""
	switch scene {
	case "daily_report":
		key = "daily_report"
	case "balance":
		key = "balance_notice"
	case "group":
		key = "order"
	default:
		return false, ErrInvalid
	}
	return s.legacyScheduledTaskAllowedTx(ctx, tx, key)
}
