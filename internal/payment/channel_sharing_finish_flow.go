package payment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
)

func (g ProviderFlow) claimSharingFinish(ctx context.Context, mid int64, trade, no string, actor TransferBatchActor) (sharingFinishRecord, bool, error) {
	var r sharingFinishRecord
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
	var oid int64
	if e = tx.QueryRow(ctx, "SELECT id FROM orders WHERE merchant_id=$1 AND trade_no=$2 FOR UPDATE", mid, trade).Scan(&oid); errors.Is(e, pgx.ErrNoRows) {
		return r, false, ErrNotFound
	} else if e != nil {
		return r, false, e
	}
	old, e := s.scanSharingFinish(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingFinishColumns+" FROM channel_sharing_finishes p JOIN orders o ON o.id=p.order_id WHERE p.merchant_id=$1 AND p.external_no=$2", mid, no))
	if e == nil {
		if old.OrderID != oid {
			return old, false, ErrConflict
		}
		return old, false, nil
	}
	if !errors.Is(e, ErrNotFound) {
		return r, false, e
	}
	r, e = s.previewSharingFinish(ctx, tx, mid, trade, actor)
	if e != nil {
		return r, false, e
	}
	if r.Source == "sharing" {
		return r, false, nil
	}
	if r.ID > 0 {
		return r, false, ErrConflict
	}
	r.ExternalNo = no
	nonce := make([]byte, 15)
	if _, e = rand.Read(nonce); e != nil {
		return r, false, e
	}
	r.OutNo = "PF" + hex.EncodeToString(nonce)
	if e = g.checkParentOrderWriteTx(ctx, tx, mid, r.OrderID, r.Payment.ChannelID); e != nil {
		return r, false, e
	}
	if _, e = g.sharingFinishProvider(ctx, tx, r); e != nil {
		return r, false, e
	}
	if e = requireFundsAction(ctx, tx, "channel-sharing-finish", fmt.Sprintf("%d:%s", mid, no)); e != nil {
		return r, false, e
	}
	if e = tx.QueryRow(ctx, "SELECT nextval(pg_get_serial_sequence('channel_sharing_finishes','id'))").Scan(&r.ID); e != nil {
		return r, false, e
	}
	r.RequestHash, e = s.sharingFinishFingerprint(r)
	if e != nil {
		return r, false, e
	}
	var ps, previous any
	if r.PreviousFailedID > 0 {
		previous = r.PreviousFailedID
	}
	if r.OriginalSharingID > 0 {
		ps = r.OriginalSharingID
	}
	if e = tx.QueryRow(ctx, `INSERT INTO channel_sharing_finishes(id,order_id,merchant_id,external_no,out_no,provider_trade_no,gross_amount,merchant_net,shared_amount,expected_unsplit,release_amount,original_sharing_id,previous_finish_id,request_hash,actor_role,actor_id,actor_name,actor_class) VALUES($1,$2,$3,$4,$5,$6,$7,$8,0,$7,$8,$9,$10,$11,$12,$13,$14,$15) RETURNING created_at`, r.ID, oid, mid, no, r.OutNo, r.ProviderTradeNo, r.GrossAmount, r.MerchantNet, ps, previous, r.RequestHash, actor.Role, actor.ID, actor.Name, actor.Class).Scan(&r.CreatedAt); e != nil {
		return r, false, e
	}
	if _, e = tx.Exec(ctx, "UPDATE channel_sharing_finishes SET state='claimed',claimed_at=clock_timestamp() WHERE id=$1", r.ID); e != nil {
		return r, false, e
	}
	if e = sharingConfigurationAudit(ctx, tx, actor, "channel-sharing-finishes", r.ID, "submit", "claimed"); e != nil {
		return r, false, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO jobs(event_key,kind,payload,run_at) VALUES($1,'channel-sharing-finish.reconcile',jsonb_build_object('finish_id',$2::bigint),statement_timestamp()+interval '30 seconds')", fmt.Sprintf("channel-sharing-finish:%d:reconcile", r.ID), r.ID); e != nil {
		return r, false, e
	}
	if e = g.checkParentOrderWriteTx(ctx, tx, mid, r.OrderID, r.Payment.ChannelID); e != nil {
		return r, false, e
	}
	if e = tx.Commit(ctx); e != nil {
		return r, false, e
	}
	r.State = "claimed"
	return r, true, nil
}
func (g ProviderFlow) SubmitChannelSharingFinish(ctx context.Context, mid int64, trade, no string, actor TransferBatchActor) (ChannelSharingFinishView, error) {
	if !g.paymentWritesEnabled() {
		return ChannelSharingFinishView{}, ErrLiveDisabled
	}
	r, owner, e := g.claimSharingFinish(ctx, mid, trade, no, actor)
	if e != nil || !owner {
		return r.ChannelSharingFinishView, e
	}
	if e = g.checkParentOrderWrite(ctx, mid, r.OrderID, r.Payment.ChannelID); e != nil {
		return r.ChannelSharingFinishView, e
	}
	p, e := g.sharingFinishProvider(ctx, g.Store.pool, r)
	if e == nil {
		var enabled bool
		if check := g.Store.pool.QueryRow(ctx, "SELECT enabled FROM merchants WHERE id=$1", mid).Scan(&enabled); check != nil {
			e = check
		} else if !enabled {
			e = ErrState
		}
	}
	if e != nil {
		return r.ChannelSharingFinishView, errors.Join(e, g.Store.sharingFinishFailure(ctx, r, "failed", actor))
	}
	result, e := p.FinishChannelSharing(ctx, sharingFinishRequest(r))
	if e != nil {
		state := "unknown"
		var rejection *adapter.ProviderError
		if errors.As(e, &rejection) && definiteProviderRejection(rejection) {
			state = "failed"
		}
		save := g.Store.sharingFinishFailure(ctx, r, state, actor)
		view, _ := g.Store.ChannelSharingFinish(ctx, mid, no, actor)
		if state == "unknown" {
			return view, &adapter.UnknownOutcome{Operation: "channel-sharing-finish", Cause: errors.Join(e, save)}
		}
		return view, errors.Join(e, save)
	}
	if e = g.Store.applySharingFinishObservation(ctx, r, result, "submit", actor); e != nil {
		return r.ChannelSharingFinishView, &adapter.UnknownOutcome{Operation: "persist-channel-sharing-finish", Cause: e}
	}
	return g.Store.ChannelSharingFinish(ctx, mid, no, actor)
}
func (s *Store) sharingFinishFailure(ctx context.Context, original sharingFinishRecord, state string, actor TransferBatchActor) error {
	if state != "failed" && state != "unknown" {
		return ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT id FROM orders WHERE id=$1 FOR UPDATE", original.OrderID); e != nil {
		return e
	}
	r, e := s.scanSharingFinish(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingFinishColumns+" FROM channel_sharing_finishes p JOIN orders o ON o.id=p.order_id WHERE p.id=$1 FOR UPDATE OF p", original.ID))
	if e != nil {
		return e
	}
	if r.State != "claimed" || r.LastObservationID != 0 {
		return tx.Commit(ctx)
	}
	var proof any
	if state == "failed" {
		h, e := s.sharingFinishFailureFingerprint(r)
		if e != nil {
			return e
		}
		proof = h
	}
	if _, e = tx.Exec(ctx, "UPDATE channel_sharing_finishes SET state=$2,failure_proof=$3 WHERE id=$1", r.ID, state, proof); e != nil {
		return e
	}
	if e = sharingConfigurationAudit(ctx, tx, actor, "channel-sharing-finishes", r.ID, "submit-result", state); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (g ProviderFlow) QueryChannelSharingFinish(ctx context.Context, mid int64, no string, actor TransferBatchActor) (ChannelSharingFinishView, error) {
	if g.Store == nil || !actor.valid(mid) || !channelSharingExternalNo.MatchString(no) {
		return ChannelSharingFinishView{}, ErrInvalid
	}
	r, e := g.Store.readSharingFinish(ctx, mid, no, 0)
	if e != nil {
		return r.ChannelSharingFinishView, e
	}
	return g.querySharingFinish(ctx, r, actor)
}
func (g ProviderFlow) ReconcileChannelSharingFinish(ctx context.Context, id int64) (ChannelSharingFinishView, error) {
	if g.Store == nil || id <= 0 {
		return ChannelSharingFinishView{}, ErrInvalid
	}
	r, e := g.Store.readSharingFinish(ctx, 0, "", id)
	if e != nil {
		return r.ChannelSharingFinishView, e
	}
	return g.querySharingFinish(ctx, r, TransferBatchActor{Role: "system", Name: "channel-sharing-finish-worker"})
}
func (g ProviderFlow) querySharingFinish(ctx context.Context, r sharingFinishRecord, actor TransferBatchActor) (ChannelSharingFinishView, error) {
	if r.State == "succeeded" || r.State == "failed" {
		return r.ChannelSharingFinishView, nil
	}
	if r.State == "unsent" {
		return r.ChannelSharingFinishView, ErrState
	}
	p, e := g.sharingFinishProvider(ctx, g.Store.pool, r)
	if e != nil {
		return r.ChannelSharingFinishView, e
	}
	result, e := p.QueryChannelSharingFinish(ctx, sharingFinishRequest(r))
	if e != nil {
		return r.ChannelSharingFinishView, e
	}
	if e = g.Store.applySharingFinishObservation(ctx, r, result, "query", actor); e != nil {
		return r.ChannelSharingFinishView, e
	}
	next, e := g.Store.readSharingFinish(ctx, 0, "", r.ID)
	return next.ChannelSharingFinishView, e
}
