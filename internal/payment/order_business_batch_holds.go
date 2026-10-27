package payment

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
)

type orderBusinessBatch185LocalGuardKey struct{}

func checkOrderBusinessBatch185LocalFinal(ctx context.Context, tx pgx.Tx) error {
	guard, _ := ctx.Value(orderBusinessBatch185LocalGuardKey{}).(func(context.Context, pgx.Tx) error)
	if guard == nil {
		return nil
	}
	return guard(ctx, tx)
}

func (s *Store) executeOrderBusinessBatchHold185(ctx context.Context, a OrderBusinessBatchActor, id int64, source orderBusinessBatchSource, action string) orderBusinessBatchEventFacts {
	no := fmt.Sprintf("batch185-%s:%d:%d", action, id, source.Position)
	authority := func(ctx context.Context, tx pgx.Tx) error {
		r, e := s.readOrderBusinessBatchTx(ctx, tx, a, id)
		if e != nil {
			return e
		}
		if r.Action != action || !r.authorized || (r.Items[source.Position-1].State != "running" && r.Items[source.Position-1].State != "unknown") {
			return identity.ErrFundsAuthorization
		}
		return s.checkOrderBusinessBatchAuthority185(ctx, tx, a, r)
	}
	guard := func(ctx context.Context, tx pgx.Tx, kind, key string) error {
		if action == "freeze-income" && (kind != "income-freeze" || key != fmt.Sprintf("%d:%s", source.MerchantID, no)) {
			return identity.ErrFundsAuthorization
		}
		if action == "unfreeze-income" && (kind != "income-unfreeze" || key != fmt.Sprint(source.HoldID)) {
			return identity.ErrFundsAuthorization
		}
		if action == "freeze-income" {
			o, e := scanOrder(tx.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE id=$1", source.OrderID))
			if e != nil {
				return e
			}
			budget, e := s.orderIncomeSource(ctx, tx, o)
			if e != nil {
				return e
			}
			if budget.Remaining != source.OperationAmount {
				return ErrConflict
			}
		} else {
			h, e := scanIncomeHold(tx.QueryRow(ctx, "SELECT "+incomeHoldColumns+" FROM order_income_holds h JOIN orders o ON o.id=h.order_id WHERE h.id=$1 AND h.order_id=$2", source.HoldID, source.OrderID))
			if e != nil {
				return e
			}
			h, _, _, e = s.readIncomeHold(ctx, tx, h)
			if e != nil {
				return e
			}
			if h.RemainingAmount != source.OperationAmount || h.RefundPendingCount != 0 {
				return ErrConflict
			}
		}
		return authority(ctx, tx)
	}
	local := context.WithValue(WithFundsActionGuard(ctx, guard), orderBusinessBatch185LocalGuardKey{}, authority)
	actor := IncomeHoldActor{ID: a.User.ID, Name: a.User.Username, Class: a.User.AdminClass}
	var h OrderIncomeHold
	var e error
	if action == "freeze-income" {
		h, e = s.FreezeOrderIncome(local, source.MerchantID, source.TradeNo, no, actor)
	} else {
		h, e = s.UnfreezeOrderIncome(local, source.MerchantID, source.TradeNo, source.HoldID, actor)
	}
	if e != nil {
		if facts, definite := s.failUnstartedOrderBusinessBatch185(ctx, a, id, source, action, e); definite {
			return facts
		}
		return orderBusinessBatchEventFacts{State: "unknown", Code: "original_income_action_unconfirmed"}
	}
	if (action == "freeze-income" && h.State != "held") || (action == "unfreeze-income" && h.State != "released") {
		return orderBusinessBatchEventFacts{State: "unknown", Code: "original_income_state_unconfirmed"}
	}
	return orderBusinessBatchEventFacts{State: "succeeded", Code: "original_income_" + h.State, OrderStatus: "paid"}
}
