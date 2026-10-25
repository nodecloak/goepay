package payment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

var errSettlementPlanDayChanged = errors.New("settlement generation crossed original business date")

type settlementPlanWindowError string

func (e settlementPlanWindowError) Error() string { return string(e) }

type settlementPlanRequestWindowKey struct{}
type settlementPlanRequestWindow struct {
	Day       string
	Authority SettlementPlanAuthorizationView
}

func (s *Store) GenerateSettlementPlanRun(ctx context.Context, id int64) error {
	if id <= 0 {
		return ErrInvalid
	}
	r, err := s.readSettlementPlanRun(ctx, s.pool, id)
	if err != nil {
		return err
	}
	a, err := s.readSettlementPlanAuthorization(ctx, s.pool, r.View.AuthorizationID)
	if err != nil {
		return err
	}
	if a.Scope.Threshold != nil {
		return s.generateThresholdSettlementRun(ctx, id)
	}
	for _, m := range a.Scope.Merchants {
		if err = s.generateSettlementPlanItem(ctx, r.View, m.MerchantID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) generateSettlementPlanItem(ctx context.Context, run SettlementPlanRunView, mid int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var pid int64
	if err = tx.QueryRow(ctx, "SELECT id FROM settlement_plans WHERE id=$1 FOR UPDATE", run.PlanID).Scan(&pid); err != nil {
		return err
	}
	r, err := s.readSettlementPlanRun(ctx, tx, run.ID)
	if err != nil {
		return err
	}
	run = r.View
	var old int64
	err = tx.QueryRow(ctx, "SELECT id FROM settlement_plan_items WHERE run_id=$1 AND merchant_id=$2", run.ID, mid).Scan(&old)
	if err == nil {
		if _, err = s.readSettlementPlanItem(ctx, tx, old); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	a, err := s.readSettlementPlanAuthorization(ctx, tx, run.AuthorizationID)
	if err != nil {
		return err
	}
	definition, err := s.readSettlementPlanDefinition(ctx, tx, a.Scope.DefinitionID)
	if err != nil {
		return err
	}
	rows, err := tx.Query(ctx, "SELECT id FROM merchants WHERE id=ANY($1::bigint[]) ORDER BY id FOR NO KEY UPDATE", definition.Input.MerchantIDs)
	if err != nil {
		return err
	}
	for rows.Next() {
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	p, err := s.readSettlementPlan(ctx, tx, run.PlanID)
	if err != nil {
		return err
	}
	item := settlementPlanItemRecord{RunID: run.ID, View: SettlementPlanItemView{MerchantID: mid, State: "blocked"}}
	for _, m := range a.Scope.Merchants {
		if m.MerchantID == mid {
			item.View.RecipientConfigID = m.RecipientConfigID
		}
	}
	if item.View.RecipientConfigID <= 0 {
		return ErrConflict
	}
	var now time.Time
	if err = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		return err
	}
	item.View.Reason = settlementPlanAuthorityAt(a.View, now)
	if item.View.Reason == "" && (!p.View.Enabled || p.View.Revision != run.PlanRevision || p.View.LastAuthorizationID != run.AuthorizationID) {
		item.View.Reason = "authorization_inactive"
	}
	if item.View.Reason == "" {
		if _, err = readSettlementPolicy(ctx, tx, true); err != nil {
			return err
		}
		// Maturity is a separate financial key. A waiting account lock must
		// not let a later D1-to-D0 change expand this original authority.
		rows, e := tx.Query(ctx, "SELECT id FROM backoffice_records WHERE resource='settings' AND body->>'key'='settle_type' ORDER BY id FOR SHARE")
		if e != nil {
			return e
		}
		for rows.Next() {
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		current, e := s.settlementPlanStaticScope(ctx, tx, p)
		left, _ := json.Marshal(current)
		right, _ := json.Marshal(a.Scope)
		if e != nil || !bytes.Equal(left, right) {
			item.View.Reason = "scope_changed"
		}
	}
	if item.View.Reason == "" {
		day, e := settlementBusinessDate(now, 0)
		if e != nil {
			return e
		}
		if day != run.BusinessDate {
			item.View.Reason = "day_changed"
		}
	}
	if item.View.Reason != "" {
		return s.commitSettlementPlanItem(ctx, tx, item, run, a.View)
	}
	// The immutable original item also proves ordinary daily consumption. A
	// missing intent child cannot make that parent become a fresh daily slot.
	prior, err := settlementPlanIDs(ctx, tx, `SELECT item_id FROM settlement_scheduled_intents WHERE merchant_id=$1 AND business_date=$2::date
 UNION SELECT i.id FROM settlement_plan_items i JOIN settlement_plan_runs r ON r.id=i.run_id JOIN settlement_plan_versions v ON (v.plan_id,v.revision)=(r.plan_id,r.plan_revision)
 WHERE i.merchant_id=$1 AND r.business_date=$2::date AND v.mode='generate_only' AND i.state='created' LIMIT 2`, mid, run.BusinessDate)
	if err != nil {
		return err
	}
	if len(prior) > 1 {
		return ErrConflict
	}
	if len(prior) == 1 {
		original, e := s.readSettlementPlanItem(ctx, tx, prior[0])
		if e != nil {
			if invalidReferralEvidence(e) {
				return ErrConflict
			}
			return e
		}
		if original.View.MerchantID != mid || original.View.State != "created" {
			return ErrConflict
		}
		item.View.State, item.View.Reason = "skipped", "already_scheduled"
		return s.commitSettlementPlanItem(ctx, tx, item, run, a.View)
	}
	var runUsed, dayUsed, dayItems int64
	if err = tx.QueryRow(ctx, `SELECT (SELECT COALESCE(sum(gross),0)::bigint FROM settlement_plan_items WHERE run_id=$1),(SELECT COALESCE(sum(gross),0)::bigint FROM settlement_plan_budget_uses WHERE plan_id=$2 AND business_date=$3::date),(SELECT count(*) FROM settlement_plan_budget_uses WHERE plan_id=$2 AND business_date=$3::date)`, run.ID, run.PlanID, run.BusinessDate).Scan(&runUsed, &dayUsed, &dayItems); err != nil {
		return err
	}
	if runUsed >= a.View.MaxRunGross || dayUsed >= a.View.MaxDayGross || dayItems >= a.View.MaxDayItems {
		item.View.State, item.View.Reason = "skipped", "budget_exhausted"
		return s.commitSettlementPlanItem(ctx, tx, item, run, a.View)
	}
	maximum := a.View.MaxItemGross
	if n := a.View.MaxRunGross - runUsed; n < maximum {
		maximum = n
	}
	if n := a.View.MaxDayGross - dayUsed; n < maximum {
		maximum = n
	}
	if err = requireOutgoingFunds(ctx, tx, mid, 0); err != nil {
		return err
	}
	if err = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		return err
	}
	if err = verifySettlementPlanGlobalPolicy(ctx, tx, a.Scope); err != nil {
		if errors.Is(err, ErrConflict) {
			item.View.Reason = "scope_changed"
			return s.commitSettlementPlanItem(ctx, tx, item, run, a.View)
		}
		return err
	}
	if day, e := settlementBusinessDate(now, 0); e != nil {
		return e
	} else if day != run.BusinessDate {
		item.View.Reason = "day_changed"
		return s.commitSettlementPlanItem(ctx, tx, item, run, a.View)
	}
	funds, err := readFundsAvailability(ctx, tx, mid, &now)
	if err != nil {
		return err
	}
	quote, err := quoteScheduledSettlement(a.Scope.Policy, funds.LedgerAvailable, funds.Available, a.Scope.Policy.RetainedCents, maximum, now)
	item.View.LedgerAvailable, item.View.Eligible, item.View.Retained = funds.LedgerAvailable, funds.Available, a.Scope.Policy.RetainedCents
	if err != nil || quote.Gross == 0 {
		item.View.State, item.View.Reason = "skipped", "insufficient_funds"
		if err != nil {
			item.View.Reason = "amount_policy"
		}
		return s.commitSettlementPlanItem(ctx, tx, item, run, a.View)
	}
	if err = tx.QueryRow(ctx, "SELECT nextval(pg_get_serial_sequence('settlement_plan_items','id'))").Scan(&item.View.ID); err != nil {
		return err
	}
	external := fmt.Sprintf("SP%d", item.View.ID)
	// A shared request helper can fail after posting its reservation. Roll back
	// that entire attempt before committing a durable blocked/skipped item.
	attempt, err := tx.Begin(ctx)
	if err != nil {
		return err
	}
	defer attempt.Rollback(ctx)
	consumed := false
	guarded := WithFundsActionGuard(ctx, func(c context.Context, got pgx.Tx, kind, key string) error {
		if got != attempt || consumed || kind != "funds-request" || key != fmt.Sprintf("settlements:%d:%s", mid, external) {
			return ErrInvalid
		}
		if e := verifySettlementPlanGlobalPolicy(c, got, a.Scope); e != nil {
			return e
		}
		var at time.Time
		if e := got.QueryRow(c, "SELECT clock_timestamp()").Scan(&at); e != nil {
			return e
		}
		day, e := settlementBusinessDate(at, 0)
		if e != nil {
			return e
		}
		if day != run.BusinessDate {
			return errSettlementPlanDayChanged
		}
		if reason := settlementPlanAuthorityAt(a.View, at); reason != "" {
			return settlementPlanWindowError(reason)
		}
		consumed = true
		return nil
	})
	guarded = context.WithValue(guarded, settlementPlanRequestWindowKey{}, settlementPlanRequestWindow{Day: run.BusinessDate, Authority: a.View})
	request, err := s.requestFundsTxSettlement(guarded, attempt, "settlements", mid, 0, external, quote.Gross, "system:settlement-scheduler", &SettlementExpectation{FeeAmount: quote.Fee, NetAmount: quote.Net}, &item.View.RecipientConfigID)
	if err != nil {
		if rollbackErr := attempt.Rollback(ctx); rollbackErr != nil {
			return rollbackErr
		}
	}
	if errors.Is(err, errSettlementPlanDayChanged) {
		item.View.State, item.View.Reason = "blocked", "day_changed"
		return s.commitSettlementPlanItem(ctx, tx, item, run, a.View)
	}
	var window settlementPlanWindowError
	if errors.As(err, &window) {
		item.View.State, item.View.Reason = "blocked", string(window)
		return s.commitSettlementPlanItem(ctx, tx, item, run, a.View)
	}
	if errors.Is(err, ErrSettlementMode) {
		item.View.State, item.View.Reason = "skipped", "settlement_mode"
		return s.commitSettlementPlanItem(ctx, tx, item, run, a.View)
	}
	if errors.Is(err, ErrState) && consumed {
		item.View.State, item.View.Reason = "skipped", "daily_limit"
		return s.commitSettlementPlanItem(ctx, tx, item, run, a.View)
	}
	if errors.Is(err, ErrInsufficient) {
		item.View.State, item.View.Reason = "skipped", "funds_changed"
		return s.commitSettlementPlanItem(ctx, tx, item, run, a.View)
	}
	if errors.Is(err, ErrConflict) {
		item.View.State, item.View.Reason = "blocked", "scope_changed"
		return s.commitSettlementPlanItem(ctx, tx, item, run, a.View)
	}
	if err != nil {
		return err
	}
	if !consumed {
		return ErrConflict
	}
	if err = attempt.Commit(ctx); err != nil {
		return err
	}
	item.View.State, item.View.Reason, item.View.RequestID = "created", "pending_review", request.ID
	item.View.Gross, item.View.Fee, item.View.Net = quote.Gross, quote.Fee, quote.Net
	return s.commitSettlementPlanItem(ctx, tx, item, run, a.View)
}

func verifySettlementPlanGlobalPolicy(ctx context.Context, q referralReader, scope settlementPlanScope) error {
	policy, err := readSettlementPolicy(ctx, q, false)
	if err != nil {
		return err
	}
	var mode string
	if err = q.QueryRow(ctx, `SELECT COALESCE((SELECT body->>'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'='settle_type'),'0')`).Scan(&mode); err != nil {
		return err
	}
	if policy != scope.Policy || mode != scope.SettleType {
		return ErrConflict
	}
	return nil
}

func (s *Store) commitSettlementPlanItem(ctx context.Context, tx pgx.Tx, item settlementPlanItemRecord, run SettlementPlanRunView, a SettlementPlanAuthorizationView) error {
	if err := s.persistSettlementPlanItemTx(ctx, tx, item, run, a); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// The caller owns the transaction. Threshold selection must be able to persist
// all of its original items and roll them back together before any first send.
func (s *Store) persistSettlementPlanItemTx(ctx context.Context, tx pgx.Tx, item settlementPlanItemRecord, run SettlementPlanRunView, a SettlementPlanAuthorizationView) error {
	return s.persistSettlementPlanItemRowsTx(ctx, tx, item, run, a, true)
}

// Original normal generation validates its one item immediately. Threshold
// generation validates the complete original selection after writing all rows.
func (s *Store) persistSettlementPlanItemRowsTx(ctx context.Context, tx pgx.Tx, item settlementPlanItemRecord, run SettlementPlanRunView, a SettlementPlanAuthorizationView, validate bool) error {
	if item.View.ID == 0 {
		if err := tx.QueryRow(ctx, "SELECT nextval(pg_get_serial_sequence('settlement_plan_items','id'))").Scan(&item.View.ID); err != nil {
			return err
		}
	}
	if err := tx.QueryRow(ctx, "SELECT transaction_timestamp()").Scan(&item.CreatedAt); err != nil {
		return err
	}
	var err error
	item.Proof, err = s.settlementPlanItemProof(item)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO settlement_plan_items(id,run_id,merchant_id,state,reason,request_id,recipient_config_id,ledger_available,eligible,retained,gross,fee,net,proof_hash,created_at) VALUES($1,$2,$3,$4,$5,NULLIF($6::bigint,0),NULLIF($7::bigint,0),$8,$9,$10,$11,$12,$13,$14,$15)`, item.View.ID, item.RunID, item.View.MerchantID, item.View.State, item.View.Reason, item.View.RequestID, item.View.RecipientConfigID, item.View.LedgerAvailable, item.View.Eligible, item.View.Retained, item.View.Gross, item.View.Fee, item.View.Net, item.Proof, item.CreatedAt); err != nil {
		return err
	}
	if item.View.State == "created" {
		var mode string
		if err = tx.QueryRow(ctx, "SELECT mode FROM settlement_plan_versions WHERE plan_id=$1 AND revision=$2", run.PlanID, run.PlanRevision).Scan(&mode); err != nil {
			return err
		}
		if mode == settlementThresholdMode {
			_, err = tx.Exec(ctx, "INSERT INTO settlement_threshold_intents(run_id,merchant_id,item_id,request_id)VALUES($1,$2,$3,$4)", run.ID, item.View.MerchantID, item.View.ID, item.View.RequestID)
		} else if mode == "generate_only" {
			_, err = tx.Exec(ctx, "INSERT INTO settlement_scheduled_intents(merchant_id,business_date,item_id,request_id) VALUES($1,$2::date,$3,$4)", item.View.MerchantID, run.BusinessDate, item.View.ID, item.View.RequestID)
		} else {
			return ErrConflict
		}
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "INSERT INTO settlement_plan_budget_uses(item_id,authorization_id,plan_id,business_date,gross,fee,net) VALUES($1,$2,$3,$4::date,$5,$6,$7)", item.View.ID, a.ID, run.PlanID, run.BusinessDate, item.View.Gross, item.View.Fee, item.View.Net); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "UPDATE ledger_requests SET settlement_plan_item_id=$2 WHERE id=$1", item.View.RequestID, item.View.ID); err != nil {
			return err
		}
	}
	if err = settlementPlanAudit(ctx, tx, "system", 0, "settlement-scheduler", "settlement-plan-items", item.View.ID, "generate", map[string]string{"run_id": fmt.Sprint(run.ID), "state": item.View.State, "reason": item.View.Reason}); err != nil {
		return err
	}
	if validate {
		if _, err = s.readSettlementPlanItem(ctx, tx, item.View.ID); err != nil {
			return err
		}
	}
	return nil
}
