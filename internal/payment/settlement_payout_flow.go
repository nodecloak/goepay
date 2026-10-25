package payment

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
	"time"
)

func (g ProviderFlow) ExecuteSettlementPayout(ctx context.Context, id int64, actor TransferBatchActor, check TransferBatchCheck) (SettlementPayout, error) {
	if g.Store == nil {
		return SettlementPayout{}, ErrInvalid
	}
	if !g.AllowLive {
		return SettlementPayout{}, ErrLiveDisabled
	}
	tx, e := g.Store.pool.Begin(ctx)
	if e != nil {
		return SettlementPayout{}, e
	}
	defer tx.Rollback(ctx)
	p, owner, e := g.claimSettlementPayoutTx(ctx, tx, id, actor, check, false)
	if e != nil {
		return p, e
	}
	if e = tx.Commit(ctx); e != nil {
		return p, e
	}
	if !owner {
		return p, nil
	}
	return g.sendClaimedSettlement(ctx, p, actor, check, nil)
}

func (g ProviderFlow) claimSettlementPayoutTx(ctx context.Context, tx pgx.Tx, id int64, actor TransferBatchActor, check TransferBatchCheck, automatic bool) (SettlementPayout, bool, error) {
	v, profile, e := g.Store.lockSettlementRequest(ctx, tx, id)
	if e != nil {
		return SettlementPayout{}, false, e
	}
	if !payoutActor(actor, v.MerchantID) && !(automatic && settlementAutoActorFor(ctx, actor, id)) {
		return SettlementPayout{}, false, ErrInvalid
	}
	p, e := g.Store.settlementPayout(ctx, tx, id)
	if e != nil {
		return p, false, e
	}
	if e = tx.QueryRow(ctx, "SELECT id FROM transfers WHERE id=$1 FOR UPDATE", p.TransferID).Scan(&p.TransferID); e != nil {
		return p, false, e
	}
	if p.SendState != "unsent" {
		return p, false, nil
	}
	history, e := g.Store.readChannelStops(ctx, tx, p.ChannelID)
	if e != nil {
		return p, false, e
	}
	if len(history) > 0 && history[len(history)-1].Action == "pause" {
		return p, false, ErrChannelStopped
	}
	if v.Status != "approved" || p.Funding == nil || p.Funding.State != "reserved" || p.Funding.ReservedAmount != v.Amount {
		return p, false, ErrState
	}
	if e = g.Store.settlementWritePolicy(ctx, tx, v.MerchantID, profile); e != nil {
		return p, false, e
	}
	if e = g.payoutProviderMatches(ctx, tx, p); e != nil {
		return p, false, e
	}
	if check != nil {
		if e = check(ctx, p.ChannelID); e != nil {
			return p, false, e
		}
	}
	if e = requireFundsAction(ctx, tx, "settlement-payout", fmt.Sprint(id)); e != nil {
		return p, false, e
	}
	claimSQL := "UPDATE settlement_payouts SET send_state='claimed',claimed_at=clock_timestamp() WHERE request_id=$1 AND send_state='unsent'"
	args := []any{id}
	if automatic {
		c, ok := ctx.Value(settlementAutoClaimKey{}).(*settlementAutoClaim)
		if !ok || c == nil || c.RequestID != id || c.TransferID != p.TransferID || c.AuthorizationID <= 0 {
			return p, false, ErrInvalid
		}
		claimSQL = "UPDATE settlement_payouts SET send_state='claimed',claimed_at=clock_timestamp(),auto_authorization_id=$2 WHERE request_id=$1 AND send_state='unsent'"
		args = append(args, c.AuthorizationID)
	}
	if _, e = tx.Exec(ctx, claimSQL, args...); e != nil {
		return p, false, e
	}
	if e = payoutEvent(ctx, tx, id, "claim", "processing", actor); e != nil {
		return p, false, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO jobs(event_key,kind,payload,run_at) VALUES($1,'transfer.reconcile',jsonb_build_object('transfer_id',$2::bigint),clock_timestamp()+interval '30 seconds') ON CONFLICT DO NOTHING", fmt.Sprintf("settlement-payout:%d:reconcile", id), p.TransferID); e != nil {
		return p, false, e
	}
	return p, true, nil
}

func (g ProviderFlow) sendClaimedSettlement(ctx context.Context, p SettlementPayout, actor TransferBatchActor, check, finalCheck TransferBatchCheck) (SettlementPayout, error) {
	id := p.RequestID
	var e error
	if check != nil {
		if e = check(ctx, p.ChannelID); e != nil {
			return g.payoutAfterAction(ctx, id, e)
		}
	}
	if e = g.payoutProviderMatches(ctx, g.Store.pool, p); e != nil {
		return g.payoutAfterAction(ctx, id, e)
	}
	if e = g.recheckSettlementWrite(ctx, p); e != nil {
		return g.payoutAfterAction(ctx, id, e)
	}
	provider, ok := g.Providers[p.ChannelID].(adapter.TransferProvider)
	if !ok {
		return g.payoutAfterAction(ctx, id, adapter.ErrUnsupported)
	}
	// Policy locks above may have waited beyond revocation or expiry. An
	// automatic caller must revalidate after those waits, immediately before
	// the first provider write. A stopped durable claim is never rearmed.
	if finalCheck != nil {
		if e = finalCheck(ctx, p.ChannelID); e != nil {
			return g.payoutAfterAction(ctx, id, e)
		}
	}
	_, sendErr := g.sendClaimedTransfer(ctx, p.Transfer, provider)
	current, readErr := g.payoutAfterAction(ctx, id, nil)
	state := current.State
	if state == "" {
		state = "unknown"
	}
	auditErr := g.Store.settlementPayoutAudit(ctx, id, "result", state, actor)
	return current, errors.Join(sendErr, readErr, auditErr)
}
func (g ProviderFlow) QuerySettlementPayout(ctx context.Context, id int64, actor TransferBatchActor) (SettlementPayout, error) {
	if g.Store == nil {
		return SettlementPayout{}, ErrInvalid
	}
	p, e := g.Store.SettlementPayout(ctx, id)
	if e != nil {
		return p, e
	}
	if !actor.valid(p.MerchantID) {
		return p, ErrInvalid
	}
	if p.SendState != "claimed" {
		return p, nil
	}
	if ctx.Err() != nil {
		return p, ctx.Err()
	}
	if e = g.Store.settlementPayoutAudit(ctx, id, "query", "checking", actor); e != nil {
		return p, e
	}
	t, queryErr := g.QueryTransfer(ctx, p.MerchantID, p.Transfer.TransferNo)
	auditErr := g.Store.settlementPayoutAudit(ctx, id, "query", t.Status, actor)
	return g.payoutAfterAction(ctx, id, errors.Join(queryErr, auditErr))
}
func (g ProviderFlow) CancelUnsentSettlementPayout(ctx context.Context, id int64, actor TransferBatchActor) (SettlementPayout, error) {
	if g.Store == nil {
		return SettlementPayout{}, ErrInvalid
	}
	tx, e := g.Store.pool.Begin(ctx)
	if e != nil {
		return SettlementPayout{}, e
	}
	defer tx.Rollback(ctx)
	v, _, e := g.Store.lockSettlementRequest(ctx, tx, id)
	if e != nil {
		return SettlementPayout{}, e
	}
	if !payoutActor(actor, v.MerchantID) {
		return SettlementPayout{}, ErrInvalid
	}
	p, e := g.Store.settlementPayout(ctx, tx, id)
	if e != nil {
		return p, e
	}
	if e = tx.QueryRow(ctx, "SELECT id FROM transfers WHERE id=$1 FOR UPDATE", p.TransferID).Scan(&p.TransferID); e != nil {
		return p, e
	}
	if p.SendState == "cancelled" {
		return p, tx.Commit(ctx)
	}
	if p.SendState != "unsent" || p.Transfer.LedgerStatus != "pending" || p.Transfer.AttemptState != "processing" || p.Transfer.ProviderTransferNo != "" {
		return p, ErrState
	}
	if e = requireFundsAction(ctx, tx, "settlement-payout-cancel", fmt.Sprint(id)); e != nil {
		return p, e
	}
	if _, e = tx.Exec(ctx, "UPDATE settlement_payouts SET send_state='cancelled',cancelled_at=clock_timestamp() WHERE request_id=$1 AND send_state='unsent'", id); e != nil {
		return p, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO settlement_payout_cancellations(request_id,transfer_id) VALUES($1,$2)", id, p.TransferID); e != nil {
		return p, e
	}
	if e = payoutEvent(ctx, tx, id, "cancel", "cancelled", actor); e != nil {
		return p, e
	}
	if e = g.Store.resolveTransferTx(ctx, tx, p.TransferID, "failed"); e != nil {
		return p, e
	}
	if _, e = tx.Exec(ctx, "UPDATE provider_transfer_attempts SET state='failed',updated_at=clock_timestamp() WHERE transfer_id=$1", p.TransferID); e != nil {
		return p, e
	}
	if p.Transfer.Type == "bank" {
		if _, e = tx.Exec(ctx, "UPDATE bank_transfer_lifecycle SET state='cancelled',review_reason='platform_proven_unsent',updated_at=clock_timestamp() WHERE transfer_id=$1 AND NOT settled AND NOT return_observed AND state='processing'", p.TransferID); e != nil {
			return p, e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return p, e
	}
	return g.Store.SettlementPayout(ctx, id)
}

func (g ProviderFlow) payoutProviderMatches(ctx context.Context, q fundsReader, p SettlementPayout) error {
	if e := g.Store.checkTransferAccount(ctx, q, p.TransferID, g.Providers[p.ChannelID]); e != nil {
		return e
	}
	in, _, e := g.Store.originalSettlementRecipient(ctx, q, p.RequestID)
	if e != nil {
		return e
	}
	kind, app, e := g.settlementProvider(p.ChannelID, in, transferRequest(p.Transfer))
	if e != nil {
		return e
	}
	if kind != p.ProviderKind || app != p.AppID {
		return ErrConflict
	}
	return nil
}

// A committed claim is never rearmed when policy disappears in the gap before
// I/O. Recheck after the caller's final hook, using fresh database statements.
func (g ProviderFlow) recheckSettlementWrite(ctx context.Context, p SettlementPayout) error {
	tx, e := g.Store.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	v, profile, e := g.Store.lockSettlementRequest(ctx, tx, p.RequestID)
	if e != nil {
		return e
	}
	var status string
	if e = tx.QueryRow(ctx, "SELECT status FROM transfers WHERE id=$1 FOR UPDATE", p.TransferID).Scan(&status); e != nil {
		return e
	}
	if v.Status != "approved" || status != "pending" {
		return ErrState
	}
	if e = g.Store.settlementWritePolicy(ctx, tx, v.MerchantID, profile); e != nil {
		return e
	}
	if e = g.payoutProviderMatches(ctx, tx, p); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Store) settlementPayoutAudit(ctx context.Context, id int64, action, state string, actor TransferBatchActor) error {
	work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	tx, e := s.pool.Begin(work)
	if e != nil {
		return e
	}
	defer tx.Rollback(work)
	if e = payoutEvent(work, tx, id, action, state, actor); e != nil {
		return e
	}
	return tx.Commit(work)
}
func (g ProviderFlow) payoutAfterAction(ctx context.Context, id int64, cause error) (SettlementPayout, error) {
	work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	p, e := g.Store.SettlementPayout(work, id)
	return p, errors.Join(cause, e)
}

func (g ProviderFlow) settlementTransferLocal(ctx context.Context, t Transfer) (Transfer, bool, error) {
	var id int64
	var state, kind, app string
	e := g.Store.pool.QueryRow(ctx, "SELECT request_id,send_state,provider_kind,app_id FROM settlement_payouts WHERE transfer_id=$1", t.ID).Scan(&id, &state, &kind, &app)
	if errors.Is(e, pgx.ErrNoRows) {
		return t, false, nil
	}
	if e != nil {
		return t, true, e
	}
	if e = g.Store.verifySettlementAutoPayout(ctx, g.Store.pool, id); e != nil {
		return t, true, e
	}
	if state != "claimed" {
		t.Status = state
		return t, true, nil
	}
	p := g.Providers[t.ChannelID]
	if p == nil || p.ID() != kind {
		return t, true, ErrConflict
	}
	identity, ok := p.(adapter.ApplicationIdentity)
	if !ok || identity.ApplicationID() != app {
		return t, true, ErrConflict
	}
	return t, false, nil
}
