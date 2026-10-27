package payment

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
)

// The private context key is installed only by the typed batch refund flow.
// Public merchant requests cannot choose or manufacture this admission.
type orderBusinessBatch185SendGuardKey struct{}

func (s *Store) checkOrderBusinessBatch185FirstSend(ctx context.Context, tx pgx.Tx, r Refund) error {
	guard, _ := ctx.Value(orderBusinessBatch185SendGuardKey{}).(FundsActionGuard)
	if guard == nil {
		return nil
	}
	e := guard(ctx, tx, "refund", fmt.Sprintf("%d:%s", r.MerchantID, r.RefundNo))
	if e == nil {
		return nil
	}
	if !errors.Is(e, identity.ErrFundsAuthorization) && !errors.Is(e, identity.ErrAdminSession) {
		return e
	}
	var claimed bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM provider_operations WHERE kind='refund' AND refund_id=$1)`, r.ID).Scan(&claimed); err != nil {
		return err
	}
	if claimed {
		return ErrConflict
	}
	if err := s.resolveRefundTx(ctx, tx, r.ID, "failed"); err != nil {
		return err
	}
	proof := providerRefundResultEvidence{RefundID: r.ID, OrderID: r.OrderID, Amount: r.Amount, RefundNo: r.RefundNo, PriorRefundState: r.Status, State: "failed", Source: "internal"}
	if err := s.insertRefundResultEvidence(ctx, tx, r, proof); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return e
}
