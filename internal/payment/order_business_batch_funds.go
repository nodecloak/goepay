package payment

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
)

type OrderBusinessBatchFundsItem struct {
	TradeNo    string
	Amount     int64
	ExternalNo string
	HoldID     int64
}
type OrderBusinessBatchFundsSelection struct {
	ID             int64
	Action, Reason string
	SourceHash     []byte
	Items          []OrderBusinessBatchFundsItem
}

// Only trusted funds fingerprint construction consumes this private selection.
// It does not grant execution or expose account material through a console DTO.
func (s *Store) OrderBusinessBatchFundsSelection(ctx context.Context, q referralReader, id int64) (OrderBusinessBatchFundsSelection, error) {
	var out OrderBusinessBatchFundsSelection
	var a OrderBusinessBatchActor
	if e := q.QueryRow(ctx, `SELECT actor_id,actor_name FROM order_business_batches WHERE id=$1`, id).Scan(&a.User.ID, &a.User.Username); e != nil {
		return out, e
	}
	r, e := s.readOrderBusinessBatchTx(ctx, q, a, id)
	if e != nil {
		return out, e
	}
	if !isOrderBatchFinancial185(r.Action) {
		return out, ErrInvalid
	}
	out = OrderBusinessBatchFundsSelection{ID: id, Action: r.Action, Reason: r.Reason, SourceHash: r.SourceHash}
	for _, source := range r.Sources {
		if source.OperationAmount <= 0 {
			return out, ErrConflict
		}
		out.Items = append(out.Items, OrderBusinessBatchFundsItem{source.TradeNo, source.OperationAmount, fmt.Sprintf("batch185-%s:%d:%d", r.Action, id, source.Position), source.HoldID})
	}
	return out, nil
}

func (s *Store) authorizeOrderBusinessBatch185(ctx context.Context, a OrderBusinessBatchActor, id int64) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	r, e := s.lockOrderBusinessBatch185(ctx, tx, a, id)
	if e != nil {
		return e
	}
	if !isOrderBatchFinancial185(r.Action) {
		return ErrInvalid
	}
	if e = s.checkOrderBusinessBatchActor(ctx, tx, a); e != nil {
		return e
	}
	if !r.authorized {
		guard, _ := ctx.Value(fundsActionGuardKey{}).(FundsActionGuard)
		if guard == nil {
			return identity.ErrFundsAuthorization
		}
		if e = requireFundsAction(ctx, tx, "order-business-batch", fmt.Sprint(id)); e != nil {
			return e
		}
		var epoch int64
		if e = tx.QueryRow(ctx, `SELECT credential_epoch FROM admin_funds_credentials WHERE username=$1 AND enabled FOR SHARE`, a.User.Username).Scan(&epoch); e != nil {
			return e
		}
		if e = s.appendOrderBusinessBatchEvent(ctx, tx, r, 0, "authorize", orderBusinessBatchEventFacts{State: "authorized", AuthorizationEpoch: epoch, AuthVersion: a.User.AuthVersion}); e != nil {
			return e
		}
	} else if e = s.checkOrderBusinessBatchAuthority185(ctx, tx, a, r); e != nil {
		return e
	}
	if e = s.checkOrderBusinessBatchActor(ctx, tx, a); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func isOrderBatchFinancial185(action string) bool {
	return action == "refund" || action == "freeze-income" || action == "unfreeze-income"
}

func (s *Store) checkOrderBusinessBatchAuthority185(ctx context.Context, tx pgx.Tx, a OrderBusinessBatchActor, r orderBusinessBatchRecord) error {
	if !r.authorized || r.authorizationVersion != a.User.AuthVersion {
		return identity.ErrFundsAuthorization
	}
	var epoch int64
	if e := tx.QueryRow(ctx, `SELECT credential_epoch FROM admin_funds_credentials WHERE username=$1 AND enabled FOR SHARE`, a.User.Username).Scan(&epoch); e != nil {
		return identity.ErrFundsAuthorization
	}
	if epoch != r.authorizationEpoch {
		return identity.ErrFundsAuthorization
	}
	return s.checkOrderBusinessBatchActor(ctx, tx, a)
}
