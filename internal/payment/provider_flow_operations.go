package payment

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	provider "goepay/internal/adapters/payment"
	"strings"
)

func (g ProviderFlow) owned(ctx context.Context, merchantID int64, tradeNo string) (Order, ProviderAttempt, provider.Provider, error) {
	if g.Store == nil {
		return Order{}, ProviderAttempt{}, nil, ErrInvalid
	}
	o, e := g.Store.OrderByTradeNo(ctx, tradeNo)
	if e != nil || o.MerchantID != merchantID {
		return o, ProviderAttempt{}, nil, ErrNotFound
	}
	resolved, resolveErr := g.ParentPaymentIntent(ctx, o.MerchantID, o.OutTradeNo)
	if resolveErr != nil {
		return o, ProviderAttempt{}, nil, resolveErr
	}
	g = resolved
	a, e := g.Store.ProviderAttempt(ctx, tradeNo)
	if e != nil {
		return o, a, nil, e
	}
	g, _, e = g.ForPaymentIntent(ctx, o.MerchantID, o.OutTradeNo, a.ChannelID)
	if e != nil {
		return o, a, nil, e
	}
	p := g.Providers[a.ChannelID]
	if p == nil || p.ID() != o.Type {
		return o, a, nil, provider.ErrUnsupported
	}
	if e = g.Store.checkProviderAccount(ctx, g.Store.pool, a.ID, p); e != nil {
		return o, a, nil, e
	}
	return o, a, p, nil
}
func (g ProviderFlow) Query(ctx context.Context, merchantID int64, tradeNo string) (provider.QueryResult, error) {
	o, a, p, e := g.owned(ctx, merchantID, tradeNo)
	if e != nil {
		return provider.QueryResult{}, e
	}
	if !p.Capabilities().Query {
		return provider.QueryResult{}, provider.ErrUnsupported
	}
	charge104, e := g.Store.AuthenticatedOrderCharge104(ctx, o)
	if e != nil {
		return provider.QueryResult{}, e
	}
	var authorizationErr error
	if a.Method == "preauth" {
		authorizationErr = g.RefreshPreauthorization(ctx, tradeNo, a.ChannelID)
	}
	query, e := g.Store.combineQueryRequest207(ctx, tradeNo)
	if e != nil {
		return provider.QueryResult{}, e
	}
	result, e := p.Query(ctx, query)
	if e != nil {
		var rejection *provider.ProviderError
		// A freeze authorization legitimately has no payment transaction yet.
		// Only this exact, authenticated provider rejection can use the
		// separately verified authorization evidence; transport or signature
		// errors never turn into a successful query.
		if a.Method == "preauth" && authorizationErr == nil && errors.As(e, &rejection) && rejection.Code == "40004:ACQ.TRADE_NOT_EXIST" {
			return provider.QueryResult{TradeNo: tradeNo, State: "AUTHORIZED", Currency: "CNY", Amount: charge104}, nil
		}
		return result, e
	}
	if result.TradeNo != tradeNo || result.Amount != charge104 || result.Currency != "CNY" {
		return result, provider.ErrAuthentication
	}
	switch result.State {
	case "SUCCESS", "TRADE_SUCCESS", "TRADE_FINISHED":
		if result.ProviderTradeNo == "" {
			return result, provider.ErrAuthentication
		}
		if len(result.CombinePaid) != len(query.CombineSubs) {
			return result, provider.ErrAuthentication
		}
		receipt := PaidReceipt{RealMoney: result.Amount, ChannelID: a.ChannelID, ProviderTradeNo: result.ProviderTradeNo, BuyerID: result.BuyerID}
		for _, paid := range result.CombinePaid {
			receipt.CombinePaid = append(receipt.CombinePaid, CombinePaid207{SubTradeNo: paid.TradeNo, TransactionID: paid.TransactionID, Amount: paid.Amount})
		}
		e = g.Store.markPaid(ctx, tradeNo, fmt.Sprintf("provider-query:%s:%s", p.ID(), result.ProviderTradeNo), true, receipt)
	default:
		if authorizationErr != nil {
			return result, authorizationErr
		}
	}
	return result, e
}
func (s *Store) beginProviderOperation(ctx context.Context, kind, key string, o Order, a ProviderAttempt, refundID int64, access ...refundAccess179) (int64, string, bool, error) {
	if len(access) > 1 || len(access) == 1 && (kind != "refund" || access[0] > refundMerchant179) {
		return 0, "", false, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return 0, "", false, e
	}
	defer tx.Rollback(ctx)
	var admission refundAdmission179
	if len(access) == 1 && access[0] != refundInternal179 {
		admission.account, e = s.merchantAPIAccountTx(ctx, tx, o.MerchantID, false)
		if e != nil {
			return 0, "", false, e
		}
		admission.access = access[0]
	}
	var status string
	if e = tx.QueryRow(ctx, "SELECT status FROM orders WHERE id=$1 FOR UPDATE", o.ID).Scan(&status); e != nil {
		return 0, "", false, e
	}
	if kind == "close" {
		if e = s.checkScanCancellationBarrier(ctx, tx, o.ID); e != nil {
			return 0, "", false, e
		}
	}
	var refundRecord Refund
	if kind == "refund" {
		if refundID <= 0 {
			return 0, "", false, ErrInvalid
		}
		r, err := scanRefund(tx.QueryRow(ctx, "SELECT "+refundColumns+" FROM refunds WHERE id=$1 FOR UPDATE", refundID))
		if err != nil {
			return 0, "", false, err
		}
		if r.OrderID != o.ID || r.MerchantID != o.MerchantID {
			return 0, "", false, ErrConflict
		}
		// The reserve and send claim are separate durable transactions. Validate
		// the original award/escrow again at the final no-resend boundary.
		if err = s.verifyReferralRefundEvidence(ctx, tx, r); err != nil {
			return 0, "", false, err
		}
		refundRecord = r
	}
	var id int64
	var state string
	e = tx.QueryRow(ctx, "SELECT id,state FROM provider_operations WHERE kind=$1 AND business_key=$2", kind, key).Scan(&id, &state)
	if e == nil {
		return id, state, false, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return 0, "", false, e
	}
	if kind == "refund" && refundRecord.Status != "pending" {
		// A stale caller must not claim a locally rejected or otherwise resolved
		// reservation after another transaction released its funding.
		return 0, "", false, ErrState
	}
	if kind == "refund" {
		if e = s.checkOrderBusinessBatch185FirstSend(ctx, tx, refundRecord); e != nil {
			return 0, "", false, e
		}
	}
	if admission.access != refundInternal179 {
		if e = s.checkRefundAdmission179(ctx, tx, admission); e != nil {
			if errors.Is(e, ErrMerchantAPIDisabled) {
				// The order/refund locks are shared with every send claim. Only a
				// reservation with no claimed operation can be rejected locally.
				var claimed bool
				if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM provider_operations WHERE kind='refund' AND refund_id=$1)`, refundID).Scan(&claimed); err != nil {
					return 0, "", false, err
				}
				if claimed {
					return 0, "", false, ErrConflict
				}
				if err := s.resolveRefundTx(ctx, tx, refundID, "failed"); err != nil {
					return 0, "", false, err
				}
				proof := providerRefundResultEvidence{RefundID: refundRecord.ID, OrderID: refundRecord.OrderID, Amount: refundRecord.Amount, RefundNo: refundRecord.RefundNo, PriorRefundState: refundRecord.Status, State: "failed", Source: "internal"}
				if err := s.insertRefundResultEvidence(ctx, tx, refundRecord, proof); err != nil {
					return 0, "", false, err
				}
				if err := tx.Commit(ctx); err != nil {
					return 0, "", false, err
				}
			}
			return 0, "", false, e
		}
	}
	if kind == "close" && (status == "paid" || status == "closed" || a.State == "processing") {
		return 0, "", false, ErrState
	}
	if kind == "close" {
		if e = requireFundsAction(ctx, tx, "order-close", o.TradeNo); e != nil {
			return 0, "", false, e
		}
	}
	var refund any
	if refundID > 0 {
		refund = refundID
	}
	e = tx.QueryRow(ctx, "INSERT INTO provider_operations(kind,business_key,order_id,refund_id,channel_id) VALUES($1,$2,$3,$4,$5) RETURNING id,state", kind, key, o.ID, refund, a.ChannelID).Scan(&id, &state)
	if e != nil {
		return 0, "", false, e
	}
	e = tx.Commit(ctx)
	return id, state, e == nil, e
}
func (s *Store) providerOperationState(ctx context.Context, id int64, state string) error {
	_, e := s.pool.Exec(ctx, "UPDATE provider_operations SET state=$2,updated_at=now() WHERE id=$1", id, state)
	return e
}
func (g ProviderFlow) Close(ctx context.Context, merchantID int64, tradeNo string) error {
	if !g.paymentWritesEnabled() {
		return ErrLiveDisabled
	}
	o, a, p, e := g.owned(ctx, merchantID, tradeNo)
	if e != nil {
		return e
	}
	if a.Method == "preauth" {
		return provider.ErrUnsupported
	}
	if !p.Capabilities().Close {
		return provider.ErrUnsupported
	}
	id, state, owner, e := g.Store.beginProviderOperation(ctx, "close", tradeNo, o, a, 0)
	if e != nil {
		return e
	}
	if !owner {
		if state == "succeeded" {
			return nil
		}
		return &provider.UnknownOutcome{Operation: "close", Cause: ErrState}
	}
	closeRequest, e := g.Store.combineQueryRequest207(ctx, tradeNo)
	if e != nil {
		return e
	}
	e = p.Close(ctx, closeRequest)
	if e != nil {
		state = "unknown"
		var rejection *provider.ProviderError
		if errors.As(e, &rejection) && definiteProviderRejection(rejection) {
			state = "failed"
		}
		g.Store.providerOperationState(ctx, id, state)
		return e
	}
	tx, e := g.Store.pool.Begin(ctx)
	if e != nil {
		return &provider.UnknownOutcome{Operation: "persist close", Cause: e}
	}
	defer tx.Rollback(ctx)
	var current string
	if e = tx.QueryRow(ctx, "SELECT status FROM orders WHERE id=$1 FOR UPDATE", o.ID).Scan(&current); e != nil {
		return e
	}
	if current == "paid" {
		if _, e := tx.Exec(ctx, "UPDATE provider_operations SET state='unknown',updated_at=now() WHERE id=$1", id); e != nil {
			return e
		}
		if e := tx.Commit(ctx); e != nil {
			return e
		}
		return ErrState
	}
	if _, e = tx.Exec(ctx, "UPDATE orders SET status='closed' WHERE id=$1", o.ID); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "UPDATE provider_operations SET state='succeeded',updated_at=now() WHERE id=$1", id); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (g ProviderFlow) Refund(ctx context.Context, merchantID int64, tradeNo, refundNo string, amount int64) (Refund, error) {
	return g.refund(ctx, merchantID, tradeNo, refundNo, amount, refundInternal179)
}
func (g ProviderFlow) RefundFromAPI(ctx context.Context, merchantID int64, tradeNo, refundNo string, amount int64) (Refund, error) {
	return g.refund(ctx, merchantID, tradeNo, refundNo, amount, refundAPI179)
}
func (g ProviderFlow) RefundFromMerchant(ctx context.Context, merchantID int64, tradeNo, refundNo string, amount int64) (Refund, error) {
	return g.refund(ctx, merchantID, tradeNo, refundNo, amount, refundMerchant179)
}
func (g ProviderFlow) refund(ctx context.Context, merchantID int64, tradeNo, refundNo string, amount int64, access refundAccess179) (Refund, error) {
	if !g.paymentWritesEnabled() {
		return Refund{}, ErrLiveDisabled
	}
	o, a, p, e := g.owned(ctx, merchantID, tradeNo)
	if e != nil {
		return Refund{}, e
	}
	if !p.Capabilities().Refund {
		return Refund{}, provider.ErrUnsupported
	}
	var r Refund
	if access == refundAPI179 {
		r, e = g.Store.ReserveAPIRefund(ctx, merchantID, o.OutTradeNo, refundNo, amount)
	} else if access == refundMerchant179 {
		r, e = g.Store.ReserveMerchantRefund(ctx, merchantID, o.OutTradeNo, refundNo, amount)
	} else {
		r, e = g.Store.ReserveRefund(ctx, merchantID, o.OutTradeNo, refundNo, amount)
	}
	if e != nil {
		return r, e
	}
	if r.Status == "succeeded" || r.Status == "failed" {
		return r, nil
	}
	id, state, owner, e := g.Store.beginProviderOperation(ctx, "refund", fmt.Sprint(r.ID), o, a, r.ID, access)
	if e != nil {
		return r, e
	}
	if !owner {
		// A send-operation projection is not a newly authenticated provider
		// response. Only QueryRefund may recover a nonterminal refund; replay
		// never promotes an unsigned state into a new keyed financial result.
		return r, &provider.UnknownOutcome{Operation: "refund", Cause: ErrState}
	}
	providerRefundNo := fmt.Sprintf("RF%d", r.ID)
	charge104, e := g.Store.AuthenticatedOrderCharge104(ctx, o)
	if e != nil {
		return Refund{}, e
	}
	if subs, e := g.Store.CombineSubOrders(ctx, o.ID); e != nil {
		return Refund{}, e
	} else if len(subs) > 0 {
		plan, e := g.Store.planCombineRefund207(ctx, r.ID, o.ID, amount, providerRefundNo)
		if e != nil {
			g.Store.resolveProviderRefund(ctx, id, r.ID, "failed")
			return r, e
		}
		state, sendErr := g.combineRefund207(ctx, p, r.ID, plan, true)
		if save := g.Store.resolveProviderRefund(ctx, id, r.ID, state); save != nil {
			return r, &provider.UnknownOutcome{Operation: "persist refund result", Cause: errors.Join(sendErr, save)}
		}
		if sendErr != nil {
			return r, sendErr
		}
		return g.Store.Refund(ctx, merchantID, refundNo)
	}
	result, e := p.Refund(ctx, provider.RefundRequest{TradeNo: tradeNo, RefundNo: providerRefundNo, TotalAmount: charge104, Amount: amount})
	if e != nil {
		state = "unknown"
		var rejection *provider.ProviderError
		if errors.As(e, &rejection) && definiteProviderRejection(rejection) {
			state = "failed"
		}
		if save := g.Store.resolveProviderRefund(ctx, id, r.ID, state); save != nil {
			return r, &provider.UnknownOutcome{Operation: "persist refund result", Cause: errors.Join(e, save)}
		}
		if state == "unknown" {
			return r, &provider.UnknownOutcome{Operation: "refund", Cause: e}
		}
		return r, e
	}
	if result.RefundNo != providerRefundNo || result.Amount != amount {
		g.Store.resolveProviderRefund(ctx, id, r.ID, "unknown")
		return r, &provider.UnknownOutcome{Operation: "refund response", Cause: provider.ErrAuthentication}
	}
	state = refundProviderState(result.State)
	if e = g.Store.resolveProviderRefund(ctx, id, r.ID, state); e != nil {
		return r, &provider.UnknownOutcome{Operation: "persist refund result", Cause: e}
	}
	return g.Store.Refund(ctx, merchantID, refundNo)
}
func refundProviderState(state string) string {
	switch state {
	case "SUCCESS", "REFUND_SUCCESS":
		return "succeeded"
	case "FAIL", "FAILED", "CLOSED":
		return "failed"
	case "PROCESSING":
		return "pending"
	default:
		return "unknown"
	}
}
func (g ProviderFlow) QueryRefund(ctx context.Context, merchantID int64, refundNo string) (Refund, error) {
	return g.queryRefund(ctx, merchantID, refundNo, false)
}

// ResumeRefund continues an already authorized combined refund. It is for
// the durable reconciliation worker, never the public refund-query endpoint.
func (g ProviderFlow) ResumeRefund(ctx context.Context, merchantID int64, refundNo string) (Refund, error) {
	return g.queryRefund(ctx, merchantID, refundNo, g.paymentWritesEnabled())
}

func (g ProviderFlow) queryRefund(ctx context.Context, merchantID int64, refundNo string, resume bool) (Refund, error) {
	if g.Store == nil {
		return Refund{}, ErrInvalid
	}
	r, e := g.Store.Refund(ctx, merchantID, refundNo)
	if e != nil {
		return r, e
	}
	var tradeNo string
	if e = g.Store.pool.QueryRow(ctx, "SELECT trade_no FROM orders WHERE id=$1", r.OrderID).Scan(&tradeNo); e != nil {
		return r, e
	}
	o, a, p, e := g.owned(ctx, merchantID, tradeNo)
	if e != nil {
		return r, e
	}
	if !p.Capabilities().RefundQuery {
		return r, provider.ErrUnsupported
	}
	if r.Status == "succeeded" || r.Status == "failed" {
		return r, nil
	}
	var id int64
	if e = g.Store.pool.QueryRow(ctx, "SELECT id FROM provider_operations WHERE kind='refund' AND refund_id=$1", r.ID).Scan(&id); e != nil {
		return r, e
	}
	providerRefundNo := fmt.Sprintf("RF%d", r.ID)
	charge104, e := g.Store.AuthenticatedOrderCharge104(ctx, o)
	if e != nil {
		return r, e
	}
	if subs, e := g.Store.CombineSubOrders(ctx, o.ID); e != nil {
		return r, e
	} else if len(subs) > 0 {
		if resume {
			if e = g.Store.checkChannelWrite(ctx, a.ChannelID); e != nil {
				return r, e
			}
		}
		plan, e := g.Store.planCombineRefund207(ctx, r.ID, o.ID, r.Amount, providerRefundNo)
		if e != nil {
			return r, e
		}
		state, queryErr := g.combineRefund207(ctx, p, r.ID, plan, resume)
		if queryErr != nil || state == "pending" || state == "unknown" {
			return r, queryErr
		}
		if e = g.Store.resolveProviderRefund(ctx, id, r.ID, state); e != nil {
			return r, e
		}
		return g.Store.Refund(ctx, merchantID, refundNo)
	}
	result, e := p.QueryRefund(ctx, provider.RefundRequest{TradeNo: tradeNo, RefundNo: providerRefundNo, TotalAmount: charge104, Amount: r.Amount})
	if e != nil {
		return r, e
	}
	if result.RefundNo != providerRefundNo || result.Amount != r.Amount {
		return r, provider.ErrAuthentication
	}
	state := refundProviderState(result.State)
	if state == "pending" {
		return r, nil
	}
	if e = g.Store.resolveProviderRefund(ctx, id, r.ID, state); e != nil {
		return r, e
	}
	return g.Store.Refund(ctx, merchantID, refundNo)
}

// System errors and bank errors can mean the upstream accepted a request but
// lost its result. Release a reservation only for a known definitive rejection.
func definiteProviderRejection(e *provider.ProviderError) bool {
	switch e.Code {
	case "SIGNERROR", "PARAM_ERROR", "INVALID_REQUEST", "NOAUTH", "APPID_NOT_EXIST", "MCHID_NOT_EXIST", "NOTENOUGH", "REFUND_FEE_MISMATCH", "ORDERNOTEXIST":
		return true
	}
	return strings.HasPrefix(e.Code, "40002:")
}
func (s *Store) resolveProviderRefund(ctx context.Context, operationID, refundID int64, state string) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var orderID int64
	if e = tx.QueryRow(ctx, "SELECT order_id FROM refunds WHERE id=$1", refundID).Scan(&orderID); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "SELECT id FROM orders WHERE id=$1 FOR UPDATE", orderID); e != nil {
		return e
	}
	r, e := scanRefund(tx.QueryRow(ctx, "SELECT "+refundColumns+" FROM refunds WHERE id=$1", refundID))
	if e != nil {
		return e
	}
	if e = s.verifyReferralRefundEvidence(ctx, tx, r); e != nil {
		return e
	}
	var current string
	if e = tx.QueryRow(ctx, "SELECT state FROM provider_operations WHERE id=$1 AND refund_id=$2 AND kind='refund' FOR UPDATE", operationID, refundID).Scan(&current); e != nil {
		return e
	}
	// The immutable journal/keyed refund evidence, rather than the operation
	// cache, determines whether a terminal decision already exists. A trusted
	// query can repair an inconsistent cache while the refund is still pending.
	if r.Status == "succeeded" || r.Status == "failed" {
		if state == "pending" || state == "unknown" {
			return nil
		}
		if state != r.Status {
			return ErrState
		}
	}
	if r.Status == "unknown" && state == "pending" {
		return nil
	}
	if state != "pending" {
		if e = s.resolveRefundTx(ctx, tx, refundID, state); e != nil {
			return e
		}
	}
	if e = s.recordProviderRefundResult(ctx, tx, operationID, r, current, state); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "UPDATE provider_operations SET state=$2,updated_at=now() WHERE id=$1", operationID, state); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
