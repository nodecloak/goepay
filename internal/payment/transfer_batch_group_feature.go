package payment

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/merchantpolicy"
)

type batchGroupFeatureGate struct {
	mid    int64
	active bool
	access merchantpolicy.GroupFeatureAccess
}

func requireBatchGroupFeature(ctx context.Context, tx pgx.Tx, mid int64, actor TransferBatchActor) (batchGroupFeatureGate, error) {
	gate := batchGroupFeatureGate{mid: mid, active: actor.Role == "merchant"}
	if !gate.active {
		return gate, nil // Independent administrator management keeps its own checks.
	}
	access, err := merchantpolicy.ReadGroupFeatureAccess(ctx, tx, mid, "user_transfer_batch", true)
	if err != nil {
		return gate, fmt.Errorf("%w: batch feature unavailable: %v", ErrState, err)
	}
	if !access.Enabled {
		return gate, ErrState
	}
	gate.access = access
	return gate, nil
}

func (gate *batchGroupFeatureGate) recheck(ctx context.Context, tx pgx.Tx) error {
	if !gate.active {
		return nil
	}
	if !gate.access.ValidUntil.IsZero() {
		var now time.Time
		if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return err
		}
		if !now.Before(gate.access.ValidUntil) {
			return ErrConflict
		}
	}
	if !gate.access.GlobalConfigured {
		// A missing default-ON global row cannot be row-locked. Re-read after
		// authorization/account waits to catch its first explicit configuration.
		access, err := merchantpolicy.ReadGroupFeatureAccess(ctx, tx, gate.mid, "user_transfer_batch", true)
		if err != nil {
			return fmt.Errorf("%w: batch feature unavailable: %v", ErrState, err)
		}
		if !access.Enabled {
			return ErrState
		}
		gate.access.GlobalConfigured = access.GlobalConfigured
	}
	return nil
}
