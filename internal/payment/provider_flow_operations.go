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
	a, e := g.Store.ProviderAttempt(ctx, tradeNo)
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
	var authorizationErr error
	if a.Method == "preauth" {
		authorizationErr = g.RefreshPreauthorization(ctx, tradeNo, a.ChannelID)
	}
	result, e := p.Query(ctx, provider.QueryRequest{TradeNo: tradeNo})
	if e != nil {
		var rejection *provider.ProviderError
		// A freeze authorization legitimately has no payment transaction yet.
		// Only this exact, authenticated provider rejection can use the
		// separately verified authorization evidence; transport or signature
		// errors never turn into a successful query.
		if a.Method == "preauth" && authorizationErr == nil && errors.As(e, &rejection) && rejection.Code == "40004:ACQ.TRADE_NOT_EXIST" {
			return provider.QueryResult{TradeNo: tradeNo, State: "AUTHORIZED", Currency: "CNY", Amount: o.Amount}, nil
		}
		return result, e
	}
	if result.TradeNo != tradeNo || result.Amount != o.Amount || result.Currency != "CNY" {
		return result, provider.ErrAuthentication
	}
	switch result.State {
	case "SUCCESS", "TRADE_SUCCESS", "TRADE_FINISHED":
		if result.ProviderTradeNo == "" {
			return result, provider.ErrAuthentication
		}
		e = g.Store.markPaid(ctx, tradeNo, fmt.Sprintf("provider-query:%s:%s", p.ID(), result.ProviderTradeNo), true, PaidReceipt{ChannelID: a.ChannelID, ProviderTradeNo: result.ProviderTradeNo, BuyerID: result.BuyerID})
	default:
		if authorizationErr != nil {
			return result, authorizationErr
		}
	}
	return result, e
}
func (s *Store) beginProviderOperation(ctx context.Context, kind, key string, o Order, a ProviderAttempt, refundID int64) (int64, string, bool, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return 0, "", false, e
	}
	defer tx.Rollback(ctx)
	var status string
	if e = tx.QueryRow(ctx, "SELECT status FROM orders WHERE id=$1 FOR UPDATE", o.ID).Scan(&status); e != nil {
		return 0, "", false, e
	}
	if kind == "close" {
		if e = s.checkScanCancellationBarrier(ctx, tx, o.ID); e != nil {
			return 0, "", false, e
		}
	}
	if kind == "refund" {
		if refundID <= 0 {
			return 0, "", false, ErrInvalid
		}
		r, err := scanRefund(tx.QueryRow(ctx, "SELECT "+refundColumns+" FROM refunds WHERE id=$1", refundID))
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
	if !g.AllowLive {
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
	e = p.Close(ctx, provider.QueryRequest{TradeNo: tradeNo})
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
	if !g.AllowLive {
		return Refund{}, ErrLiveDisabled
	}
	o, a, p, e := g.owned(ctx, merchantID, tradeNo)
	if e != nil {
		return Refund{}, e
	}
	if !p.Capabilities().Refund {
		return Refund{}, provider.ErrUnsupported
	}
	r, e := g.Store.ReserveRefund(ctx, merchantID, o.OutTradeNo, refundNo, amount)
	if e != nil {
		return r, e
	}
	if r.Status == "succeeded" || r.Status == "failed" {
		return r, nil
	}
	id, state, owner, e := g.Store.beginProviderOperation(ctx, "refund", fmt.Sprint(r.ID), o, a, r.ID)
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
	result, e := p.Refund(ctx, provider.RefundRequest{TradeNo: tradeNo, RefundNo: providerRefundNo, TotalAmount: o.Amount, Amount: amount})
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
	o, _, p, e := g.owned(ctx, merchantID, tradeNo)
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
	result, e := p.QueryRefund(ctx, provider.RefundRequest{TradeNo: tradeNo, RefundNo: providerRefundNo, TotalAmount: o.Amount, Amount: r.Amount})
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
