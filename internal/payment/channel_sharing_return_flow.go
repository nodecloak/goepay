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

func (g ProviderFlow) claimChannelSharingReturn(ctx context.Context, mid int64, sharingNo string, receiverID int64, no string, amount int64, actor TransferBatchActor) (sharingReturnRecord, bool, error) {
	var r sharingReturnRecord
	if g.Store == nil || !actor.valid(mid) || !channelSharingExternalNo.MatchString(no) || !channelSharingExternalNo.MatchString(sharingNo) || receiverID <= 0 || amount <= 0 || amount > 999999999999 {
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
	// The original order lock serializes returns with operations that consume
	// original income. Wallet balances cannot substitute for recipient capacity.
	var oid int64
	if e = tx.QueryRow(ctx, "SELECT o.id FROM orders o JOIN channel_sharing_orders ps ON ps.order_id=o.id WHERE ps.merchant_id=$1 AND ps.external_no=$2 FOR UPDATE OF o", mid, sharingNo).Scan(&oid); errors.Is(e, pgx.ErrNoRows) {
		return r, false, ErrNotFound
	} else if e != nil {
		return r, false, e
	}
	original, e := s.scanSharingOrder(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingOrderColumns+" FROM channel_sharing_orders WHERE order_id=$1", oid))
	if e != nil {
		return r, false, e
	}
	old, e := s.scanSharingReturn(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingReturnColumns+" FROM channel_sharing_returns WHERE merchant_id=$1 AND external_no=$2", mid, no))
	if e == nil {
		if old.SharingID != original.ID || old.ReceiverID != receiverID || old.Amount != amount {
			return old, false, ErrConflict
		}
		return old, false, nil
	}
	if !errors.Is(e, ErrNotFound) {
		return r, false, e
	}
	if !original.Completed || !original.FundsReleased {
		return r, false, ErrState
	}
	var itemAmount int64
	found := false
	for _, item := range original.Items {
		if item.ReceiverID == receiverID && item.State == "succeeded" {
			itemAmount = item.Amount
			found = true
			break
		}
	}
	if !found || amount > itemAmount {
		return r, false, ErrState
	}
	var reserved int64
	var active bool
	if e = tx.QueryRow(ctx, "SELECT COALESCE(sum(amount) FILTER(WHERE state<>'failed'),0),COALESCE(bool_or(state IN ('unsent','claimed','unknown','pending')),false) FROM channel_sharing_returns WHERE sharing_id=$1 AND receiver_id=$2", original.ID, receiverID).Scan(&reserved, &active); e != nil {
		return r, false, e
	}
	if active || reserved > itemAmount-amount {
		return r, false, ErrConflict
	}
	// Verify prior keyed observations before a final failure may reopen capacity.
	if e = s.verifyOrderSharingReturnEvidence(ctx, tx, oid); e != nil {
		return r, false, e
	}
	r = sharingReturnRecord{ChannelSharingReturnView: ChannelSharingReturnView{SharingID: original.ID, OrderID: oid, MerchantID: mid, ReceiverID: receiverID, ExternalNo: no, Amount: amount}, Actor: actor, Original: original}
	nonce := make([]byte, 15)
	if _, e = rand.Read(nonce); e != nil {
		return r, false, e
	}
	r.OutNo = "PR" + hex.EncodeToString(nonce)
	if e = g.checkParentOrderWriteTx(ctx, tx, mid, r.OrderID, r.Original.Payment.ChannelID); e != nil {
		return r, false, e
	}
	if _, e = g.sharingReturnProvider(ctx, tx, r); e != nil {
		return r, false, e
	}
	if _, e = s.sharingReturnRequest(ctx, tx, r); e != nil {
		return r, false, e
	}
	if e = requireFundsAction(ctx, tx, "channel-sharing-return", fmt.Sprintf("%d:%s", mid, no)); e != nil {
		return r, false, e
	}
	if e = tx.QueryRow(ctx, "SELECT nextval(pg_get_serial_sequence('channel_sharing_returns','id'))").Scan(&r.ID); e != nil {
		return r, false, e
	}
	r.RequestHash, e = s.sharingReturnFingerprint(r)
	if e != nil {
		return r, false, e
	}
	e = tx.QueryRow(ctx, `INSERT INTO channel_sharing_returns(id,sharing_id,order_id,merchant_id,receiver_id,external_no,out_no,amount,request_hash,actor_role,actor_id,actor_name,actor_class) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING created_at`, r.ID, r.SharingID, r.OrderID, mid, receiverID, no, r.OutNo, amount, r.RequestHash, actor.Role, actor.ID, actor.Name, actor.Class).Scan(&r.CreatedAt)
	if e != nil {
		return r, false, e
	}
	if _, e = tx.Exec(ctx, "UPDATE channel_sharing_returns SET state='claimed',claimed_at=clock_timestamp() WHERE id=$1", r.ID); e != nil {
		return r, false, e
	}
	if e = sharingConfigurationAudit(ctx, tx, actor, "channel-sharing-returns", r.ID, "submit", "claimed"); e != nil {
		return r, false, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO jobs(event_key,kind,payload,run_at) VALUES($1,'channel-sharing-return.reconcile',jsonb_build_object('return_id',$2::bigint),statement_timestamp()+interval '30 seconds')", fmt.Sprintf("channel-sharing-return:%d:reconcile", r.ID), r.ID); e != nil {
		return r, false, e
	}
	if e = g.checkParentOrderWriteTx(ctx, tx, mid, r.OrderID, r.Original.Payment.ChannelID); e != nil {
		return r, false, e
	}
	if e = tx.Commit(ctx); e != nil {
		return r, false, e
	}
	r.State = "claimed"
	return r, true, nil
}
func (g ProviderFlow) SubmitChannelSharingReturn(ctx context.Context, mid int64, sharingNo string, receiverID int64, no string, amount int64, actor TransferBatchActor) (ChannelSharingReturnView, error) {
	if !g.paymentWritesEnabled() {
		return ChannelSharingReturnView{}, ErrLiveDisabled
	}
	r, owner, e := g.claimChannelSharingReturn(ctx, mid, sharingNo, receiverID, no, amount, actor)
	if e != nil || !owner {
		return r.ChannelSharingReturnView, e
	}
	if e = g.checkParentOrderWrite(ctx, mid, r.OrderID, r.Original.Payment.ChannelID); e != nil {
		return r.ChannelSharingReturnView, e
	}
	p, e := g.sharingReturnProvider(ctx, g.Store.pool, r)
	if e == nil {
		var enabled bool
		if check := g.Store.pool.QueryRow(ctx, "SELECT enabled FROM merchants WHERE id=$1", mid).Scan(&enabled); check != nil {
			e = check
		} else if !enabled {
			e = ErrState
		}
	}
	if e != nil {
		return r.ChannelSharingReturnView, errors.Join(e, g.Store.sharingReturnFailure(ctx, r, "failed", actor))
	}
	request, e := g.Store.readSharingReturnRequest(ctx, r)
	if e != nil {
		return r.ChannelSharingReturnView, errors.Join(e, g.Store.sharingReturnFailure(ctx, r, "failed", actor))
	}
	result, e := p.ReturnChannelSharing(ctx, request)
	if e != nil {
		state := "unknown"
		var rejection *adapter.ProviderError
		if errors.As(e, &rejection) && definiteProviderRejection(rejection) {
			state = "failed"
		}
		save := g.Store.sharingReturnFailure(ctx, r, state, actor)
		view, _ := g.Store.ChannelSharingReturn(ctx, mid, no, actor)
		if state == "unknown" {
			return view, &adapter.UnknownOutcome{Operation: "channel-sharing-return", Cause: errors.Join(e, save)}
		}
		return view, errors.Join(e, save)
	}
	if e = g.Store.applySharingReturnObservation(ctx, r, result, "submit", actor); e != nil {
		return r.ChannelSharingReturnView, &adapter.UnknownOutcome{Operation: "persist-channel-sharing-return", Cause: e}
	}
	return g.Store.ChannelSharingReturn(ctx, mid, no, actor)
}
func (s *Store) sharingReturnFailure(ctx context.Context, r sharingReturnRecord, state string, actor TransferBatchActor) error {
	if state != "failed" && state != "unknown" {
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
	current, e := s.scanSharingReturn(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingReturnColumns+" FROM channel_sharing_returns WHERE id=$1 FOR UPDATE", r.ID))
	if e != nil {
		return e
	}
	if current.State != "claimed" || current.LastObservationID != 0 {
		return tx.Commit(ctx)
	}
	var failureProof []byte
	if state == "failed" {
		failureProof, e = s.sharingReturnFailureFingerprint(current)
		if e != nil {
			return e
		}
	}
	if _, e = tx.Exec(ctx, "UPDATE channel_sharing_returns SET state=$2,failure_proof=$3 WHERE id=$1", r.ID, state, failureProof); e != nil {
		return e
	}
	if e = sharingConfigurationAudit(ctx, tx, actor, "channel-sharing-returns", r.ID, "submit-result", state); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func sharingReturnActorValid(actor TransferBatchActor, mid int64) bool {
	return actor.valid(mid) || actor.Role == "system" && actor.ID == 0 && actor.Name == "channel-sharing-return-worker" && actor.Class == ""
}
func (s *Store) applySharingReturnObservation(ctx context.Context, r sharingReturnRecord, result adapter.ChannelSharingReturnResult, source string, actor TransferBatchActor) error {
	if !sharingReturnActorValid(actor, r.MerchantID) || source != "submit" && source != "query" {
		return ErrInvalid
	}
	if e := validateSharingReturnResult(r, result); e != nil {
		return e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT id FROM orders WHERE id=$1 FOR UPDATE", r.OrderID); e != nil {
		return e
	}
	current, e := s.scanSharingReturn(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingReturnColumns+" FROM channel_sharing_returns WHERE id=$1 FOR UPDATE", r.ID))
	if e != nil {
		return e
	}
	if current.State == "succeeded" || current.State == "failed" {
		return tx.Commit(ctx)
	}
	if e = validateSharingReturnResult(current, result); e != nil {
		return e
	}
	proof, e := s.sharingReturnObservationFingerprint(current, result)
	if e != nil {
		return e
	}
	var observation int64
	e = tx.QueryRow(ctx, `INSERT INTO channel_sharing_return_observations(return_id,fingerprint,source,provider_return_no,state,amount,actor_role,actor_id,actor_name) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(return_id,fingerprint) DO NOTHING RETURNING id`, r.ID, proof, source, result.ProviderReturnNo, result.State, result.Amount, actor.Role, actor.ID, actor.Name).Scan(&observation)
	if errors.Is(e, pgx.ErrNoRows) {
		if e = tx.QueryRow(ctx, "SELECT id FROM channel_sharing_return_observations WHERE return_id=$1 AND fingerprint=$2", r.ID, proof).Scan(&observation); e != nil {
			return e
		}
	} else if e != nil {
		return e
	}
	var journal any
	if result.State == "succeeded" {
		var clearing, available, id int64
		if e = tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=0 AND kind='clearing' AND scope_id=0").Scan(&clearing); e != nil {
			return e
		}
		if e = tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=$1 AND kind='available' AND scope_id=0", r.MerchantID).Scan(&available); e != nil {
			return e
		}
		key := fmt.Sprintf("channel-sharing-return:%d:paid", r.ID)
		if e = postByAccountIDs(ctx, tx, key, "verified original channel sharing return", clearing, available, r.Amount); e != nil {
			return e
		}
		if e = tx.QueryRow(ctx, "SELECT id FROM journals WHERE event_key=$1", key).Scan(&id); e != nil {
			return e
		}
		journal = id
	}
	if _, e = tx.Exec(ctx, "UPDATE channel_sharing_returns SET state=$2,provider_return_no=$3,last_observation_id=$4,paid_journal_id=$5 WHERE id=$1", r.ID, result.State, result.ProviderReturnNo, observation, journal); e != nil {
		return e
	}
	if e = sharingConfigurationAudit(ctx, tx, actor, "channel-sharing-returns", r.ID, source+"-result", result.State); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (g ProviderFlow) QueryChannelSharingReturn(ctx context.Context, mid int64, no string, actor TransferBatchActor) (ChannelSharingReturnView, error) {
	if g.Store == nil {
		return ChannelSharingReturnView{}, ErrInvalid
	}
	r, e := g.Store.readSharingReturn(ctx, mid, no, actor)
	if e != nil {
		return r.ChannelSharingReturnView, e
	}
	return g.querySharingReturnRecord(ctx, r, actor)
}
func (g ProviderFlow) ReconcileChannelSharingReturn(ctx context.Context, id int64) (ChannelSharingReturnView, error) {
	if g.Store == nil {
		return ChannelSharingReturnView{}, ErrInvalid
	}
	r, e := g.Store.readSharingReturnID(ctx, id)
	if e != nil {
		return r.ChannelSharingReturnView, e
	}
	return g.querySharingReturnRecord(ctx, r, TransferBatchActor{Role: "system", Name: "channel-sharing-return-worker"})
}
func (g ProviderFlow) querySharingReturnRecord(ctx context.Context, r sharingReturnRecord, actor TransferBatchActor) (ChannelSharingReturnView, error) {
	if r.State == "succeeded" || r.State == "failed" {
		return r.ChannelSharingReturnView, nil
	}
	p, e := g.sharingReturnProvider(ctx, g.Store.pool, r)
	if e != nil {
		return r.ChannelSharingReturnView, e
	}
	request, e := g.Store.readSharingReturnRequest(ctx, r)
	if e != nil {
		return r.ChannelSharingReturnView, e
	}
	result, e := p.QueryChannelSharingReturn(ctx, request)
	if e != nil {
		return r.ChannelSharingReturnView, e
	}
	if e = g.Store.applySharingReturnObservation(ctx, r, result, "query", actor); e != nil {
		return r.ChannelSharingReturnView, e
	}
	final, e := g.Store.readSharingReturnID(ctx, r.ID)
	return final.ChannelSharingReturnView, e
}
