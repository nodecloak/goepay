package payment

import (
	"context"
	"crypto/hmac"
	"errors"

	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
)

func (s *Store) lockOrderBusinessBatch185(ctx context.Context, tx pgx.Tx, a OrderBusinessBatchActor, id int64) (orderBusinessBatchRecord, error) {
	var locked int64
	e := tx.QueryRow(ctx, `SELECT id FROM order_business_batches WHERE id=$1 AND actor_id=$2 AND actor_name=$3 FOR UPDATE`, id, a.User.ID, a.User.Username).Scan(&locked)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return orderBusinessBatchRecord{}, e
	}
	return s.readOrderBusinessBatchTx(ctx, tx, a, id)
}

func (s *Store) claimOrderBusinessBatchQuery185(ctx context.Context, a OrderBusinessBatchActor, id int64) (orderBusinessBatchSource, bool, error) {
	var source orderBusinessBatchSource
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return source, false, e
	}
	defer tx.Rollback(ctx)
	r, e := s.lockOrderBusinessBatch185(ctx, tx, a, id)
	if e != nil {
		return source, false, e
	}
	if r.Action != "query" && !isOrderBatchFinancial185(r.Action) {
		return source, false, ErrInvalid
	}
	var item *OrderBusinessBatchItem
	for i := range r.Items {
		if !batchItem185Terminal(r.Items[i].State) {
			item = &r.Items[i]
			source = r.Sources[i]
			break
		}
	}
	if e = s.checkOrderBusinessBatchActor(ctx, tx, a); e != nil {
		return source, false, e
	}
	if item == nil {
		return source, false, tx.Commit(ctx)
	}
	o, e := scanOrder(tx.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE id=$1 FOR SHARE", source.OrderID))
	if e != nil {
		return source, false, e
	}
	if o.MerchantID != source.MerchantID || o.TradeNo != source.TradeNo || o.Amount != source.Amount || o.MerchantAmount != source.MerchantAmount || o.PlatformFee != source.PlatformFee {
		return source, false, ErrConflict
	}
	quote, e := s.validateOrderRouteFeeTerms(ctx, tx, o)
	if e != nil {
		return source, false, e
	}
	if e = s.authenticateFrozenCharge104(ctx, tx, o, source.CustomerCharge104); e != nil {
		return source, false, e
	}
	var proof []byte
	if quote != nil {
		proof = quote.proof
	}
	if !hmac.Equal(proof, source.RouteFeeProof) {
		return source, false, ErrConflict
	}
	if item.State == "unsent" {
		if e = s.appendOrderBusinessBatchEvent(ctx, tx, r, item.Position, "claim", orderBusinessBatchEventFacts{State: "running"}); e != nil {
			return source, false, e
		}
	}
	if e = s.checkOrderBusinessBatchActor(ctx, tx, a); e != nil {
		return source, false, e
	}
	return source, true, tx.Commit(ctx)
}

func (s *Store) finishOrderBusinessBatchQuery185(ctx context.Context, a OrderBusinessBatchActor, id int64, position int, facts orderBusinessBatchEventFacts) (OrderBusinessBatch, error) {
	var out OrderBusinessBatch
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	r, e := s.lockOrderBusinessBatch185(ctx, tx, a, id)
	if e != nil {
		return out, e
	}
	if (r.Action != "query" && !isOrderBatchFinancial185(r.Action)) || position < 1 || position > len(r.Items) {
		return out, ErrConflict
	}
	if e = s.checkOrderBusinessBatchActor(ctx, tx, a); e != nil {
		return out, e
	}
	item := r.Items[position-1]
	if !batchItem185Terminal(item.State) {
		if item.State != "running" && item.State != "unknown" {
			return out, ErrState
		}
		// Repeated unconfirmed reads do not append unbounded identical events.
		if item.State != facts.State || item.Code != facts.Code || item.OrderStatus != facts.OrderStatus {
			if e = s.appendOrderBusinessBatchEvent(ctx, tx, r, position, "result", facts); e != nil {
				return out, e
			}
		}
	}
	r, e = s.readOrderBusinessBatchTx(ctx, tx, a, id)
	if e != nil {
		return out, e
	}
	if e = s.checkOrderBusinessBatchActor(ctx, tx, a); e != nil {
		return out, e
	}
	return r.OrderBusinessBatch, tx.Commit(ctx)
}

// ExecuteOrderBusinessBatchQuery resumes the fixed original selection. Financial
// actions require its durable batch authorization and keep original request IDs.
func (g ProviderFlow) ExecuteOrderBusinessBatchQuery(ctx context.Context, a OrderBusinessBatchActor, id int64, limit int) (OrderBusinessBatch, error) {
	var out OrderBusinessBatch
	if g.Store == nil || id <= 0 || limit < 1 || limit > 100 {
		return out, ErrInvalid
	}
	batch, e := g.Store.OrderBusinessBatch(ctx, a, id)
	if e != nil {
		return out, e
	}
	if isOrderBatchFinancial185(batch.Action) {
		for _, item := range batch.Items {
			if batchItem185Terminal(item.State) {
				continue
			}
			if item.State == "unsent" {
				if e = g.Store.authorizeOrderBusinessBatch185(ctx, a, id); e != nil {
					return out, e
				}
			}
			break
		}
	}
	// The session lock spans the bounded execution, including provider reads.
	// A crashed process releases it; another executor never shares the same
	// in-flight item. Durable claims/results remain the recovery authority.
	release, e := g.Store.acquireOrderBusinessBatch185Execution(ctx, id)
	if e != nil {
		return out, e
	}
	defer release()
	for i := 0; i < limit; i++ {
		source, run, e := g.Store.claimOrderBusinessBatchQuery185(ctx, a, id)
		if e != nil {
			return out, e
		}
		if !run {
			break
		}
		facts := orderBusinessBatchEventFacts{}
		if batch.Action == "freeze-income" || batch.Action == "unfreeze-income" {
			facts = g.Store.executeOrderBusinessBatchHold185(ctx, a, id, source, batch.Action)
		} else if batch.Action == "refund" {
			facts = g.executeOrderBusinessBatchRefund185(ctx, a, id, source)
		} else {
			result, e := g.Query(ctx, source.MerchantID, source.TradeNo)
			facts = orderBusinessBatchEventFacts{State: "unpaid", Code: "verified_unpaid", OrderStatus: "pending"}
			if errors.Is(e, adapter.ErrUnsupported) {
				facts = orderBusinessBatchEventFacts{State: "unsupported", Code: "original_query_unsupported"}
			} else if e != nil {
				facts = orderBusinessBatchEventFacts{State: "unknown", Code: "original_query_unconfirmed"}
			} else {
				switch result.State {
				case "SUCCESS", "TRADE_SUCCESS", "TRADE_FINISHED":
					facts = orderBusinessBatchEventFacts{State: "succeeded", Code: "verified_paid", OrderStatus: "paid"}
				}
			}
		}
		out, e = g.Store.finishOrderBusinessBatchQuery185(ctx, a, id, source.Position, facts)
		if e != nil {
			return out, e
		}
		if facts.State == "unknown" || (batch.Action == "refund" && facts.State == "failed") {
			return out, nil
		}
	}
	return g.Store.OrderBusinessBatch(ctx, a, id)
}
