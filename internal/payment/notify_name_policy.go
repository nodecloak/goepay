package payment

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// NotifyHideName reports the original notifyordername setting: when "1", the
// product name sent in payment callbacks is replaced with the fixed literal
// "product" (notification.URLWithPolicy applies this before signing, so the
// signature stays valid). A missing or malformed row means off, matching the
// original where an unset $conf['notifyordername'] leaves the real name.
func (s *Store) NotifyHideName(ctx context.Context) (bool, error) {
	return notifyHideName(ctx, s.pool)
}

// NotifyHideNameTx keeps console return signing on its existing transaction
// connection, including when that transaction holds merchant/session locks.
func (s *Store) NotifyHideNameTx(ctx context.Context, tx pgx.Tx) (bool, error) {
	return notifyHideName(ctx, tx)
}

func notifyHideName(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) (bool, error) {
	var value string
	err := q.QueryRow(ctx, `SELECT body->>'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'='notifyordername' ORDER BY id LIMIT 1`).Scan(&value)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return value == "1", nil
}
