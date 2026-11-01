package payment

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"goepay/internal/merchantpolicy"
)

var ErrDepositDisabled = fmt.Errorf("%w: 未开启商户保证金功能", ErrState)

func readMerchantDepositPolicy(ctx context.Context, tx pgx.Tx, mid int64) (merchantpolicy.DepositPolicy, error) {
	policy, err := merchantpolicy.ReadDepositPolicy(ctx, tx, mid, true)
	if errors.Is(err, merchantpolicy.ErrDepositPolicy) || errors.Is(err, merchantpolicy.ErrGroupPolicy) {
		return policy, ErrState
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return policy, ErrNotFound
	}
	return policy, err
}
func checkDepositPolicy(ctx context.Context, tx pgx.Tx, mid int64, policy merchantpolicy.DepositPolicy) error {
	err := policy.Recheck(ctx, tx, mid)
	if errors.Is(err, merchantpolicy.ErrDepositPolicyChanged) {
		return ErrConflict
	}
	return err
}
func checkDepositReleaseWindow(ctx context.Context, tx pgx.Tx, mid int64, action string, days int) error {
	if action != "release" || days <= 0 {
		return nil
	}
	var blocked bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM orders WHERE merchant_id=$1 AND created_at>clock_timestamp()-make_interval(days=>$2)) OR EXISTS(SELECT 1 FROM complaints133 WHERE owner_id=$1 AND first_seen_at>clock_timestamp()-make_interval(days=>$2))`, mid, days).Scan(&blocked)
	if err != nil {
		return err
	}
	if blocked {
		return ErrState
	}
	return nil
}
