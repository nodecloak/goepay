package payment

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
)

func (s *Store) LookupOrderBusinessBatch(ctx context.Context, a OrderBusinessBatchActor, no string) (OrderBusinessBatch, error) {
	var out OrderBusinessBatch
	if len(no) < 8 || !sharingSafeField(no, 96, true) {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if e = s.checkOrderBusinessBatchActor(ctx, tx, a); e != nil {
		return out, e
	}
	var id int64
	e = tx.QueryRow(ctx, `SELECT id FROM order_business_batches WHERE actor_id=$1 AND actor_name=$2 AND external_no=$3`, a.User.ID, a.User.Username, no).Scan(&id)
	if errors.Is(e, pgx.ErrNoRows) {
		var anchored bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE actor_role='admin' AND actor_id=$1 AND actor_name=$2 AND resource='order-business-batches' AND operation='prepare' AND changes->>'external_no'=$3)`, a.User.ID, a.User.Username, no).Scan(&anchored); err != nil {
			return out, err
		}
		if anchored {
			return out, ErrConflict
		}
		return out, ErrNotFound
	}
	if e != nil {
		return out, e
	}
	r, e := s.readOrderBusinessBatchTx(ctx, tx, a, id)
	if e != nil {
		return out, e
	}
	if e = s.checkOrderBusinessBatchActor(ctx, tx, a); e != nil {
		return out, e
	}
	return r.OrderBusinessBatch, tx.Commit(ctx)
}
