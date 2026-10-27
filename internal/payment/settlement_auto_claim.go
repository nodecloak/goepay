package payment

import (
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"time"
)

type settlementAutoClaimKey struct{}
type settlementAutoClaim struct {
	PlanID                                                            int64
	Sequence                                                          int64
	RequestID, AuthorizationID, RunID, ItemID, TransferID, MerchantID int64
	Gross, Net, Fee                                                   int64
	BusinessDate                                                      string
	ClaimedAt                                                         time.Time
	TransactionID                                                     string
	Proof                                                             []byte
}

func (s *Store) settlementAutoClaimProof(c settlementAutoClaim, a settlementAutoAuthorization, item settlementPlanItemRecord, run settlementPlanRunRecord) ([]byte, error) {
	return s.referralDigest("settlement-auto73/claim", []any{c.Sequence, c.RequestID, c.PlanID, c.AuthorizationID, c.RunID, c.ItemID, c.TransferID, c.MerchantID, c.Gross, c.Net, c.Fee, c.BusinessDate, c.ClaimedAt.UTC().Format(time.RFC3339Nano), c.TransactionID, a.Proof, item.Proof, run.Proof})
}

func (g ProviderFlow) validateSettlementAutoCurrent(ctx context.Context, q referralReader, a settlementAutoAuthorization) error {
	current, e := g.Store.currentSettlementAuto(ctx, q, a.View.PlanID)
	if e != nil {
		return e
	}
	if current.View.ID != a.View.ID || current.View.Revoked || !hmac.Equal(current.Proof, a.Proof) {
		return ErrState
	}
	var at time.Time
	if e = q.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&at); e != nil {
		return e
	}
	if at.Before(a.View.ValidFrom) || !at.Before(a.View.ExpiresAt) {
		return ErrState
	}
	p, e := g.Store.readSettlementPlan(ctx, q, a.View.PlanID)
	if e != nil {
		return e
	}
	if p.View.Revision != a.View.PlanRevision {
		return ErrConflict
	}
	scope, e := g.settlementAutoScope(ctx, q, p, a.View.SettlementAutoInput)
	if e != nil {
		return e
	}
	want, _ := json.Marshal(a.Scope)
	actual, _ := json.Marshal(scope)
	if !bytes.Equal(want, actual) {
		return ErrConflict
	}
	// This is the last database statement of the final pre-send check. Earlier
	// reads may themselves wait; do not use their earlier clock/head snapshot.
	return g.settlementAutoCurrentWindow(ctx, q, a, "")
}

func (g ProviderFlow) settlementAutoCurrentWindow(ctx context.Context, q referralReader, a settlementAutoAuthorization, day string) error {
	var ready bool
	e := q.QueryRow(ctx, `SELECT clock_timestamp()>=$3 AND clock_timestamp()<$4
 AND EXISTS(SELECT 1 FROM settlement_auto_heads WHERE plan_id=$1 AND authorization_id=$2)
 AND NOT EXISTS(SELECT 1 FROM settlement_auto_revocations WHERE authorization_id=$2)
 AND NOT EXISTS(SELECT 1 FROM settlement_plan_revocations WHERE authorization_id=$5)
 AND ($6::text='' OR (clock_timestamp() AT TIME ZONE 'Asia/Shanghai')::date::text=$6)`, a.View.PlanID, a.View.ID, a.View.ValidFrom, a.View.ExpiresAt, a.View.GenerationAuthorizationID, day).Scan(&ready)
	if e == nil && !ready {
		return ErrState
	}
	return e
}

func (s *Store) verifySettlementAutoPayout(ctx context.Context, q fundsReader, id int64) error {
	var aid *int64
	var transfer int64
	var state string
	e := q.QueryRow(ctx, "SELECT auto_authorization_id,transfer_id,send_state FROM settlement_payouts WHERE request_id=$1", id).Scan(&aid, &transfer, &state)
	if e != nil {
		return e
	}
	if aid == nil {
		var system bool
		if e = q.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM settlement_payout_events WHERE request_id=$1 AND action='claim' AND actor_role='system')", id).Scan(&system); e != nil {
			return e
		}
		if system {
			return ErrConflict
		}
		return nil
	}
	var c settlementAutoClaim
	e = q.QueryRow(ctx, `SELECT claim_seq,request_id,plan_id,authorization_id,run_id,item_id,transfer_id,merchant_id,gross,net,fee,business_date::text,claimed_at,transaction_id::text,proof_hash FROM settlement_auto_claims WHERE request_id=$1`, id).Scan(&c.Sequence, &c.RequestID, &c.PlanID, &c.AuthorizationID, &c.RunID, &c.ItemID, &c.TransferID, &c.MerchantID, &c.Gross, &c.Net, &c.Fee, &c.BusinessDate, &c.ClaimedAt, &c.TransactionID, &c.Proof)
	if errors.Is(e, pgx.ErrNoRows) {
		return ErrConflict
	}
	if e != nil {
		return e
	}
	a, e := s.readSettlementAuto(ctx, q, c.AuthorizationID)
	if e != nil {
		return e
	}
	item, e := s.readSettlementPlanItem(ctx, q, c.ItemID)
	if e != nil {
		return e
	}
	run, e := s.readSettlementPlanRun(ctx, q, c.RunID)
	if e != nil {
		return e
	}
	if state != "claimed" || c.AuthorizationID != *aid || c.TransferID != transfer || item.RunID != c.RunID || item.View.RequestID != id || item.View.MerchantID != c.MerchantID || item.View.Gross != c.Gross || item.View.Net != c.Net || item.View.Fee != c.Fee || run.View.AuthorizationID != a.View.GenerationAuthorizationID || run.View.PlanID != a.View.PlanID || run.View.PlanRevision != a.View.PlanRevision || c.ClaimedAt.Before(a.View.ValidFrom) || !c.ClaimedAt.Before(a.View.ExpiresAt) || c.BusinessDate != c.ClaimedAt.In(settlementScheduleZone).Format("2006-01-02") || c.BusinessDate != run.View.BusinessDate {
		return ErrConflict
	}
	proof, e := s.settlementAutoClaimProof(c, a, item, run)
	if e != nil || !hmac.Equal(proof, c.Proof) {
		return ErrConflict
	}
	if c.Sequence <= 0 || c.PlanID != a.View.PlanID || c.Gross > a.View.MaxItemGross || c.ClaimedAt.Before(a.View.CreatedAt) {
		return ErrConflict
	}
	var budget, revoked bool
	e = q.QueryRow(ctx, `SELECT COALESCE(sum(c.gross) FILTER(WHERE c.run_id=$2),0)<=$5::numeric AND COALESCE(sum(c.gross) FILTER(WHERE c.business_date=$3::date),0)<=$6::numeric AND count(*) FILTER(WHERE c.business_date=$3::date)<=$7::bigint FROM settlement_auto_claims c WHERE c.plan_id=$1 AND c.claim_seq<=$4`, a.View.PlanID, c.RunID, c.BusinessDate, c.Sequence, a.View.MaxRunGross, a.View.MaxDayGross, a.View.MaxDayItems).Scan(&budget)
	if e != nil {
		return e
	}
	if !budget {
		return ErrConflict
	}
	e = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM settlement_auto_revocations WHERE authorization_id=$1 AND created_at<=$3) OR EXISTS(SELECT 1 FROM settlement_plan_revocations WHERE authorization_id=$2 AND created_at<=$3)`, c.AuthorizationID, a.View.GenerationAuthorizationID, c.ClaimedAt).Scan(&revoked)
	if e != nil {
		return e
	}
	if revoked {
		return ErrConflict
	}
	var valid bool
	e = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM settlement_payouts p JOIN transfers t ON t.id=p.transfer_id WHERE p.request_id=$1 AND (p.gross_amount,p.net_amount,p.fee_amount,t.amount) = ($2::bigint,$3::bigint,$4::bigint,$3::bigint) AND EXISTS(SELECT 1 FROM settlement_payout_events e WHERE e.request_id=p.request_id AND e.action='claim' AND (e.actor_role,e.actor_id,e.actor_name)=('system',0::bigint,'settlement-auto')))`, id, c.Gross, c.Net, c.Fee).Scan(&valid)
	if e == nil && !valid {
		return ErrConflict
	}
	return e
}

func (g ProviderFlow) ExecuteSettlementAuto(ctx context.Context, id int64, check TransferBatchCheck) (SettlementPayout, error) {
	return g.executeSettlementAuto(ctx, id, 0, check)
}

// A queued first-send job may consume only the exact authority that created
// it. Replacing a head must not silently give an old job fresh payment powers.
func (g ProviderFlow) ExecuteSettlementAutoAuthorized(ctx context.Context, id, authorizationID int64, check TransferBatchCheck) (SettlementPayout, error) {
	if authorizationID <= 0 {
		return SettlementPayout{}, ErrInvalid
	}
	return g.executeSettlementAuto(ctx, id, authorizationID, check)
}

func (g ProviderFlow) executeSettlementAuto(ctx context.Context, id, expectedAuthorizationID int64, check TransferBatchCheck) (SettlementPayout, error) {
	if g.Store == nil || id <= 0 {
		return SettlementPayout{}, ErrInvalid
	}
	if !g.paymentWritesEnabled() {
		return SettlementPayout{}, ErrLiveDisabled
	}
	resolved, resolveErr := g.parentSettlementRuntime(ctx, id, 0)
	if resolveErr != nil {
		return SettlementPayout{}, resolveErr
	}
	g = resolved
	tx, e := g.Store.pool.Begin(ctx)
	if e != nil {
		return SettlementPayout{}, e
	}
	defer tx.Rollback(ctx)
	var plan, itemID int64
	e = tx.QueryRow(ctx, `SELECT r.plan_id,i.id FROM settlement_plan_items i JOIN settlement_plan_runs r ON r.id=i.run_id WHERE i.request_id=$1 AND i.state='created'`, id).Scan(&plan, &itemID)
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrState
	}
	if e != nil {
		return SettlementPayout{}, e
	}
	if e = tx.QueryRow(ctx, "SELECT id FROM settlement_plans WHERE id=$1 FOR UPDATE", plan).Scan(&plan); e != nil {
		return SettlementPayout{}, e
	}
	// A completed first claim is never rearmed, even if its authority expired
	// or was revoked. Outcome recovery uses the existing original TF query job.
	old, e := g.Store.settlementPayout(ctx, tx, id)
	if e == nil && old.SendState != "unsent" {
		var aid *int64
		if e = tx.QueryRow(ctx, "SELECT auto_authorization_id FROM settlement_payouts WHERE request_id=$1", id).Scan(&aid); e != nil {
			return old, e
		}
		if aid == nil {
			return old, ErrState
		}
		if expectedAuthorizationID > 0 && *aid != expectedAuthorizationID {
			return old, ErrConflict
		}
		return old, nil
	}
	if e != nil && !errors.Is(e, ErrNotFound) {
		return old, e
	}
	a, e := g.Store.currentSettlementAuto(ctx, tx, plan)
	if e != nil {
		return SettlementPayout{}, e
	}
	if expectedAuthorizationID > 0 && a.View.ID != expectedAuthorizationID {
		return SettlementPayout{}, ErrConflict
	}
	rows, e := tx.Query(ctx, "SELECT id FROM merchants WHERE id=ANY($1::bigint[]) ORDER BY id FOR NO KEY UPDATE", a.Scope.StaticMerchantIDs())
	if e != nil {
		return SettlementPayout{}, e
	}
	for rows.Next() {
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return SettlementPayout{}, e
	}
	item, e := g.Store.readSettlementPlanItem(ctx, tx, itemID)
	if e != nil {
		return SettlementPayout{}, e
	}
	run, e := g.Store.readSettlementPlanRun(ctx, tx, item.RunID)
	if e != nil {
		return SettlementPayout{}, e
	}
	if run.View.AuthorizationID != a.View.GenerationAuthorizationID || run.View.PlanID != plan || run.View.PlanRevision != a.View.PlanRevision {
		return SettlementPayout{}, ErrConflict
	}
	channel := int64(0)
	for _, ch := range a.Scope.Channels {
		if ch.MerchantID == item.View.MerchantID {
			channel = ch.ChannelID
		}
	}
	if channel <= 0 {
		return SettlementPayout{}, ErrConflict
	}
	if e = g.validateSettlementAutoCurrent(ctx, tx, a); e != nil {
		return SettlementPayout{}, e
	}
	claim := settlementAutoClaim{PlanID: plan, RequestID: id, AuthorizationID: a.View.ID, RunID: item.RunID, ItemID: itemID, MerchantID: item.View.MerchantID, Gross: item.View.Gross, Net: item.View.Net, Fee: item.View.Fee}
	guarded := WithFundsActionGuard(ctx, func(c context.Context, work pgx.Tx, kind, key string) error {
		if work != tx || !(kind == "funds-review" && key == fmt.Sprintf("%d:true", id) || kind == "settlement-payout" && key == fmt.Sprint(id)) {
			return ErrInvalid
		}
		if kind == "settlement-payout" {
			var now time.Time
			if e := work.QueryRow(c, "SELECT clock_timestamp()").Scan(&now); e != nil {
				return e
			}
			if claim.ClaimedAt.IsZero() || now.In(settlementScheduleZone).Format("2006-01-02") != claim.BusinessDate {
				return ErrState
			}
		}
		return g.validateSettlementAutoCurrent(c, work, a)
	})
	guarded = context.WithValue(guarded, settlementAutoClaimKey{}, &claim)
	if _, e = g.Store.reviewFundsRequestTx(guarded, tx, id, true, "system:settlement-auto"); e != nil {
		return SettlementPayout{}, e
	}
	actor := TransferBatchActor{Role: "system", ID: 0, Name: "settlement-auto"}
	p, e := g.prepareSettlementPayoutTx(guarded, tx, id, channel, actor, check, true)
	if e != nil {
		return p, e
	}
	claim.TransferID = p.TransferID
	if e = tx.QueryRow(ctx, "SELECT clock_timestamp(),pg_current_xact_id()::text").Scan(&claim.ClaimedAt, &claim.TransactionID); e != nil {
		return p, e
	}
	claim.BusinessDate = claim.ClaimedAt.In(settlementScheduleZone).Format("2006-01-02")
	if claim.BusinessDate != run.View.BusinessDate {
		return p, ErrState
	}
	if e = g.Store.verifySettlementAutoBudgetHistory(ctx, tx, plan, claim.RunID, claim.BusinessDate); e != nil {
		return p, e
	}
	if e = tx.QueryRow(ctx, "SELECT nextval(pg_get_serial_sequence('settlement_auto_claims','claim_seq'))").Scan(&claim.Sequence); e != nil {
		return p, e
	}
	var runUsed, dayUsed, dayItems int64
	if e = tx.QueryRow(ctx, `SELECT COALESCE(sum(c.gross) FILTER(WHERE c.run_id=$2),0),COALESCE(sum(c.gross) FILTER(WHERE c.business_date=$3::date),0),count(*) FILTER(WHERE c.business_date=$3::date) FROM settlement_auto_claims c WHERE c.plan_id=$1`, a.View.PlanID, claim.RunID, claim.BusinessDate).Scan(&runUsed, &dayUsed, &dayItems); e != nil {
		return p, e
	}
	if claim.Gross <= 0 || claim.Gross > a.View.MaxItemGross || runUsed > a.View.MaxRunGross-claim.Gross || dayUsed > a.View.MaxDayGross-claim.Gross || dayItems >= a.View.MaxDayItems {
		return p, ErrState
	}
	if e = g.validateSettlementAutoCurrent(ctx, tx, a); e != nil {
		return p, e
	}
	claim.Proof, e = g.Store.settlementAutoClaimProof(claim, a, item, run)
	if e != nil {
		return p, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO settlement_auto_claims(claim_seq,request_id,plan_id,authorization_id,run_id,item_id,transfer_id,merchant_id,gross,net,fee,business_date,claimed_at,transaction_id,proof_hash)VALUES($14,$1,$15,$2,$3,$4,$5,$6,$7,$8,$9,$10::date,$11,$12::xid8,$13)`, id, claim.AuthorizationID, claim.RunID, claim.ItemID, claim.TransferID, claim.MerchantID, claim.Gross, claim.Net, claim.Fee, claim.BusinessDate, claim.ClaimedAt, claim.TransactionID, claim.Proof, claim.Sequence, claim.PlanID); e != nil {
		return p, e
	}
	p, owner, e := g.claimSettlementPayoutTx(guarded, tx, id, actor, check, true)
	if e != nil {
		return p, e
	}
	if !owner {
		return p, ErrConflict
	}
	if e = tx.Commit(ctx); e != nil {
		return p, e
	}
	lastCheck := func(c context.Context, ch int64) error {
		if check != nil {
			if e := check(c, ch); e != nil {
				return e
			}
		}
		return g.validateSettlementAutoCurrent(c, g.Store.pool, a)
	}
	result, sendErr := g.sendClaimedSettlement(ctx, p, actor, lastCheck, func(c context.Context, _ int64) error {
		if e := g.validateSettlementAutoCurrent(c, g.Store.pool, a); e != nil {
			return e
		}
		return g.settlementAutoCurrentWindow(c, g.Store.pool, a, run.View.BusinessDate)
	})
	if g.ChannelAutoStopEnabled && sendErr != nil {
		sendErr = errors.Join(sendErr, g.Store.recordAutomaticChannelFailure(ctx, id, sendErr))
	}
	return result, sendErr
}

func (s settlementAutoScope) StaticMerchantIDs() []int64 {
	out := make([]int64, 0, len(s.Static.Merchants))
	for _, m := range s.Static.Merchants {
		out = append(out, m.MerchantID)
	}
	return out
}

func (s *Store) verifySettlementAutoBudgetHistory(ctx context.Context, q referralReader, plan, run int64, day string) error {
	// Inspect both immutable claims and surviving payout parents. Deleting a
	// child must not erase spend and turn it into fresh authority capacity.
	rows, e := q.Query(ctx, `SELECT request_id FROM (
 SELECT c.request_id FROM settlement_auto_claims c WHERE c.plan_id=$1 AND (c.run_id=$2 OR c.business_date=$3::date)
 UNION
 SELECT p.request_id FROM settlement_payouts p JOIN ledger_requests l ON l.id=p.request_id JOIN settlement_plan_items i ON i.id=l.settlement_plan_item_id JOIN settlement_plan_runs r ON r.id=i.run_id
 WHERE r.plan_id=$1 AND (i.run_id=$2 OR (p.claimed_at AT TIME ZONE 'Asia/Shanghai')::date=$3::date) AND (p.auto_authorization_id IS NOT NULL OR EXISTS(SELECT 1 FROM settlement_payout_events e WHERE e.request_id=p.request_id AND e.action='claim' AND e.actor_role='system'))
 ) original ORDER BY request_id LIMIT 201`, plan, run, day)
	if e != nil {
		return e
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if len(ids) > 200 {
		return ErrConflict
	}
	for _, id := range ids {
		if e = s.verifySettlementAutoPayout(ctx, q, id); e != nil {
			return e
		}
	}
	return nil
}
