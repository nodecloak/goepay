package payment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	adapter "goepay/internal/adapters/payment"
)

func (g ProviderFlow) claimChannelSharingOrder(ctx context.Context, mid int64, trade, no string, actor TransferBatchActor) (sharingOrderRecord, bool, error) {
	var r sharingOrderRecord
	if g.Store == nil || !actor.valid(mid) || !channelSharingExternalNo.MatchString(no) || trade == "" {
		return r, false, ErrInvalid
	}
	s := g.Store
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return r, false, e
	}
	defer tx.Rollback(ctx)
	var enabled bool
	if e = tx.QueryRow(ctx, "SELECT enabled FROM merchants WHERE id=$1 FOR SHARE", mid).Scan(&enabled); e != nil || !enabled {
		return r, false, ErrState
	}
	o, e := scanOrder(tx.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE merchant_id=$1 AND trade_no=$2 FOR UPDATE", mid, trade))
	if e != nil {
		return r, false, e
	}
	original, e := s.scanSharingOrder(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingOrderColumns+" FROM channel_sharing_orders WHERE order_id=$1", o.ID))
	if e == nil {
		if original.ExternalNo != no {
			return r, false, ErrConflict
		}
		return original, false, nil
	}
	if !errors.Is(e, ErrNotFound) {
		return r, false, e
	}
	if o.Status != "paid" {
		return r, false, ErrState
	}
	r.Payment, e = scanSharingPayment(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingPaymentColumns+" FROM provider_attempt_sharing WHERE order_id=$1", o.ID))
	if e != nil {
		return r, false, e
	}
	var recharge bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM recharge_intents WHERE order_id=$1)", o.ID).Scan(&recharge); e != nil {
		return r, false, e
	}
	if !r.Payment.Declared || r.Payment.Purpose != "payment" || recharge {
		return r, false, ErrState
	}
	if e = s.verifySharingPayment(ctx, tx, r.Payment); e != nil {
		return r, false, e
	}
	var valid, conflicting bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM channel_sharing_funding_invariants WHERE order_id=$1 AND valid IS TRUE),EXISTS(SELECT 1 FROM refunds WHERE order_id=$1 AND status<>'failed') OR EXISTS(SELECT 1 FROM order_income_holds WHERE order_id=$1 AND state='held') OR EXISTS(SELECT 1 FROM order_profit_allocations WHERE order_id=$1)", o.ID).Scan(&valid, &conflicting); e != nil {
		return r, false, e
	}
	if !valid || conflicting {
		return r, false, ErrState
	}
	r.OrderID, r.MerchantID, r.ExternalNo, r.AutoFinish = o.ID, mid, no, true
	r.Actor = actor
	if e = tx.QueryRow(ctx, "SELECT account_id FROM channel_sharing_fundings WHERE order_id=$1", o.ID).Scan(&r.FundingAccount); e != nil {
		return r, false, e
	}
	if e = tx.QueryRow(ctx, "SELECT provider_trade_no FROM verified_payment_receipts WHERE order_id=$1 AND channel_id=$2", o.ID, r.Payment.ChannelID).Scan(&r.ProviderTradeNo); e != nil {
		return r, false, e
	}
	if len(r.ProviderTradeNo) > 64 {
		return r, false, ErrInvalid
	}
	nonce := make([]byte, 15)
	if _, e = rand.Read(nonce); e != nil {
		return r, false, e
	}
	r.OutNo = "PS" + hex.EncodeToString(nonce)
	if e = g.checkParentOrderWriteTx(ctx, tx, mid, r.OrderID, r.Payment.ChannelID); e != nil {
		return r, false, e
	}
	p, e := g.sharingOrderProvider(ctx, tx, r)
	if e != nil {
		return r, false, e
	}
	request, e := s.sharingOrderRequest(ctx, tx, r, true)
	if e != nil {
		return r, false, e
	}
	if e = p.ValidateChannelSharing(request); e != nil {
		return r, false, e
	}
	if e = requireFundsAction(ctx, tx, "channel-sharing-submit", fmt.Sprintf("%d:%s", mid, no)); e != nil {
		return r, false, e
	}
	if e = tx.QueryRow(ctx, "SELECT nextval(pg_get_serial_sequence('channel_sharing_orders','id'))").Scan(&r.ID); e != nil {
		return r, false, e
	}
	r.RequestHash, e = s.sharingOrderFingerprint(r)
	if e != nil {
		return r, false, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO channel_sharing_orders(id,order_id,merchant_id,external_no,out_no,provider_trade_no,request_hash,auto_finish,receiver_count,actor_role,actor_id,actor_name,actor_class) VALUES($1,$2,$3,$4,$5,$6,$7,true,$8,$9,$10,$11,$12)`, r.ID, o.ID, mid, no, r.OutNo, r.ProviderTradeNo, r.RequestHash, len(r.Payment.Items), actor.Role, actor.ID, actor.Name, actor.Class); e != nil {
		return r, false, e
	}
	for i, item := range r.Payment.Items {
		if _, e = tx.Exec(ctx, "INSERT INTO channel_sharing_order_items(sharing_id,position,receiver_id,amount) VALUES($1,$2,$3,$4)", r.ID, i+1, item.ReceiverID, item.Amount); e != nil {
			return r, false, e
		}
	}
	if _, e = tx.Exec(ctx, "UPDATE channel_sharing_orders SET state='claimed',claimed_at=clock_timestamp() WHERE id=$1", r.ID); e != nil {
		return r, false, e
	}
	if e = sharingConfigurationAudit(ctx, tx, actor, "channel-sharing-orders", r.ID, "submit", "claimed"); e != nil {
		return r, false, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO jobs(event_key,kind,payload,run_at) VALUES($1,'channel-sharing.reconcile',jsonb_build_object('sharing_id',$2::bigint),statement_timestamp()+interval '30 seconds')", fmt.Sprintf("channel-sharing:%d:reconcile", r.ID), r.ID); e != nil {
		return r, false, e
	}
	if e = g.checkParentOrderWriteTx(ctx, tx, mid, r.OrderID, r.Payment.ChannelID); e != nil {
		return r, false, e
	}
	if e = tx.Commit(ctx); e != nil {
		return r, false, e
	}
	r.State = "claimed"
	r.Items = make([]ChannelSharingOrderItem, 0, len(r.Payment.Items))
	for _, item := range r.Payment.Items {
		r.Items = append(r.Items, ChannelSharingOrderItem{ReceiverID: item.ReceiverID, Amount: item.Amount, State: "pending"})
	}
	return r, true, nil
}
func (g ProviderFlow) SubmitChannelSharing(ctx context.Context, mid int64, trade, no string, actor TransferBatchActor) (ChannelSharingOrderView, error) {
	if !g.paymentWritesEnabled() {
		return ChannelSharingOrderView{}, ErrLiveDisabled
	}
	r, owner, e := g.claimChannelSharingOrder(ctx, mid, trade, no, actor)
	if e != nil {
		return r.ChannelSharingOrderView, e
	}
	if !owner {
		return r.ChannelSharingOrderView, nil
	}
	if e = g.checkParentOrderWrite(ctx, mid, r.OrderID, r.Payment.ChannelID); e != nil {
		return r.ChannelSharingOrderView, e
	}
	p, e := g.sharingOrderProvider(ctx, g.Store.pool, r)
	if e == nil {
		var enabled bool
		if check := g.Store.pool.QueryRow(ctx, "SELECT enabled FROM merchants WHERE id=$1", mid).Scan(&enabled); check != nil {
			e = check
		} else if !enabled {
			e = ErrState
		}
	}
	if e != nil {
		save := g.Store.sharingOrderFailure(ctx, r, "failed", actor)
		return r.ChannelSharingOrderView, errors.Join(e, save)
	}
	request, e := g.Store.readSharingOrderRequest(ctx, r)
	if e != nil {
		save := g.Store.sharingOrderFailure(ctx, r, "failed", actor)
		return r.ChannelSharingOrderView, errors.Join(e, save)
	}
	result, e := p.SubmitChannelSharing(ctx, request)
	if e != nil {
		state := "unknown"
		var rejected *adapter.ProviderError
		if errors.As(e, &rejected) && definiteProviderRejection(rejected) {
			state = "failed"
		}
		if save := g.Store.sharingOrderFailure(ctx, r, state, actor); save != nil {
			e = errors.Join(e, save)
		}
		view, _ := g.Store.ChannelSharingOrder(ctx, mid, no, actor)
		if state == "unknown" {
			return view, &adapter.UnknownOutcome{Operation: "channel-sharing-submit", Cause: e}
		}
		return view, e
	}
	if e = g.Store.applySharingObservation(ctx, r, request, result, "submit", actor); e != nil {
		return r.ChannelSharingOrderView, &adapter.UnknownOutcome{Operation: "persist-channel-sharing-submit", Cause: e}
	}
	return g.finishObservedSharing(ctx, mid, no, actor)
}
func (s *Store) sharingOrderFailure(ctx context.Context, r sharingOrderRecord, state string, actor TransferBatchActor) error {
	if state != "unknown" && state != "failed" {
		return ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT id FROM orders WHERE id=$1 FOR UPDATE", r.OrderID); e != nil {
		return e
	}
	current, e := s.scanSharingOrder(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingOrderColumns+" FROM channel_sharing_orders WHERE id=$1 FOR UPDATE", r.ID))
	if e != nil {
		return e
	}
	if current.State != "claimed" || current.LastObservationID != 0 {
		return tx.Commit(ctx)
	}
	var proof []byte
	if state == "failed" {
		proof, e = s.sharingOrderFailureFingerprint(current)
		if e != nil {
			return e
		}
	}
	if _, e = tx.Exec(ctx, "UPDATE channel_sharing_orders SET state=$2,failure_proof=$3 WHERE id=$1", r.ID, state, proof); e != nil {
		return e
	}
	if e = sharingConfigurationAudit(ctx, tx, actor, "channel-sharing-orders", r.ID, "submit-result", state); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
