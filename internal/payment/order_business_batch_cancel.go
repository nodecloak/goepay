package payment

import (
	"context"
	"crypto/hmac"
)

func (s *Store) CancelOrderBusinessBatch(ctx context.Context, a OrderBusinessBatchActor, id int64, expected string) (OrderBusinessBatch, error) {
	var out OrderBusinessBatch
	if id <= 0 || len(expected) != 64 {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	r, e := s.lockOrderBusinessBatch185(ctx, tx, a, id)
	if e != nil {
		return out, e
	}
	if !hmac.Equal([]byte(r.Fingerprint), []byte(expected)) {
		return out, ErrConflict
	}
	if e = s.checkOrderBusinessBatchActor(ctx, tx, a); e != nil {
		return out, e
	}
	for _, item := range r.Items {
		if item.State != "unsent" {
			continue
		}
		if e = s.appendOrderBusinessBatchEvent(ctx, tx, r, item.Position, "cancel", orderBusinessBatchEventFacts{State: "cancelled", Code: "unsent_cancelled"}); e != nil {
			return out, e
		}
		r, e = s.readOrderBusinessBatchTx(ctx, tx, a, id)
		if e != nil {
			return out, e
		}
	}
	if e = s.checkOrderBusinessBatchActor(ctx, tx, a); e != nil {
		return out, e
	}
	return r.OrderBusinessBatch, tx.Commit(ctx)
}
