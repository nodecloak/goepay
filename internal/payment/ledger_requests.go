package payment

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"time"
)

type LedgerRequest struct {
	ID, MerchantID           int64
	RecipientID              *int64
	Kind, ExternalNo, Status string
	Amount                   int64
	CreatedAt                time.Time
	Settlement               *SettlementTerms         `json:"settlement,omitempty"`
	Funding                  *SettlementFunding       `json:"settlement_funding,omitempty"`
	SettlementRecipient      *SettlementRecipientView `json:"settlement_recipient,omitempty"`
}

func scanLedgerRequest(row pgx.Row) (LedgerRequest, error) {
	var v LedgerRequest
	e := row.Scan(&v.ID, &v.Kind, &v.MerchantID, &v.RecipientID, &v.ExternalNo, &v.Amount, &v.Status, &v.CreatedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrNotFound
	}
	return v, e
}

const requestColumns = "id,kind,merchant_id,recipient_id,external_no,amount,status,created_at"

// RequestFunds reserves immediately. Approval of settlements only records review:
// funds remain reserved until an authenticated payout result is implemented.
func (s *Store) RequestFunds(ctx context.Context, kind string, merchantID, recipientID int64, externalNo string, amount int64, actor string) (LedgerRequest, error) {
	return s.RequestFundsExpected(ctx, kind, merchantID, recipientID, externalNo, amount, actor, nil)
}

type SettlementExpectation struct{ FeeAmount, NetAmount int64 }

func (s *Store) RequestFundsExpected(ctx context.Context, kind string, merchantID, recipientID int64, externalNo string, amount int64, actor string, expected *SettlementExpectation) (LedgerRequest, error) {
	return s.RequestFundsWithSettlementRecipient(ctx, kind, merchantID, recipientID, externalNo, amount, actor, expected, nil)
}

func (s *Store) RequestFundsWithSettlementRecipient(ctx context.Context, kind string, merchantID, recipientID int64, externalNo string, amount int64, actor string, expected *SettlementExpectation, configID *int64) (LedgerRequest, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return LedgerRequest{}, e
	}
	defer tx.Rollback(ctx)
	v, e := s.requestFundsTxSettlement(ctx, tx, kind, merchantID, recipientID, externalNo, amount, actor, expected, configID)
	if e != nil {
		return v, e
	}
	return v, tx.Commit(ctx)
}
func (s *Store) requestFundsTx(ctx context.Context, tx pgx.Tx, kind string, merchantID, recipientID int64, externalNo string, amount int64, actor string) (LedgerRequest, error) {
	return s.requestFundsTxExpected(ctx, tx, kind, merchantID, recipientID, externalNo, amount, actor, nil)
}
func (s *Store) requestFundsTxExpected(ctx context.Context, tx pgx.Tx, kind string, merchantID, recipientID int64, externalNo string, amount int64, actor string, expected *SettlementExpectation) (LedgerRequest, error) {
	return s.requestFundsTxSettlement(ctx, tx, kind, merchantID, recipientID, externalNo, amount, actor, expected, nil)
}

func (s *Store) requestFundsTxSettlement(ctx context.Context, tx pgx.Tx, kind string, merchantID, recipientID int64, externalNo string, amount int64, actor string, expected *SettlementExpectation, configID *int64) (LedgerRequest, error) {
	if configID != nil && (kind != "settlements" || *configID <= 0) {
		return LedgerRequest{}, ErrInvalid
	}
	if expected != nil && (kind != "settlements" || expected.FeeAmount < 0 || expected.NetAmount <= 0) {
		return LedgerRequest{}, ErrInvalid
	}
	if (kind != "settlements" && kind != "internal-transfers" && kind != "profitsharing") || merchantID <= 0 || amount <= 0 || externalNo == "" || len(externalNo) > 128 || actor == "" {
		return LedgerRequest{}, ErrInvalid
	}
	if kind == "settlements" && recipientID != 0 || kind != "settlements" && (recipientID <= 0 || recipientID == merchantID) {
		return LedgerRequest{}, ErrInvalid
	}
	var e error
	var enabled bool
	if e = tx.QueryRow(ctx, "SELECT enabled FROM merchants WHERE id=$1 FOR NO KEY UPDATE", merchantID).Scan(&enabled); e != nil {
		return LedgerRequest{}, e
	}
	if !enabled {
		return LedgerRequest{}, ErrState
	}
	old, e := scanLedgerRequest(tx.QueryRow(ctx, "SELECT "+requestColumns+" FROM ledger_requests WHERE kind=$1 AND merchant_id=$2 AND external_no=$3", kind, merchantID, externalNo))
	if e == nil {
		if e = s.verifyScheduledRequestSource(ctx, tx, old.ID); e != nil {
			return LedgerRequest{}, e
		}
		oldRecipient := int64(0)
		if old.RecipientID != nil {
			oldRecipient = *old.RecipientID
		}
		if old.Amount != amount || oldRecipient != recipientID {
			return LedgerRequest{}, ErrConflict
		}
		old, e = attachSettlementTerms(ctx, tx, old)
		if e != nil {
			return old, e
		}
		if expected != nil && (old.Settlement == nil || old.Settlement.FeeAmount != expected.FeeAmount || old.Settlement.NetAmount != expected.NetAmount) {
			return old, ErrConflict
		}
		old, e = s.attachSettlementRecipient(ctx, tx, old)
		if e != nil {
			return old, e
		}
		if configID != nil && (old.SettlementRecipient == nil || old.SettlementRecipient.ConfigID != *configID) {
			return old, ErrConflict
		}
		return old, nil
	}
	if !errors.Is(e, ErrNotFound) {
		return LedgerRequest{}, e
	}
	var terms *SettlementTerms
	var selectedSettlementDeadline time.Time
	var railGate *settlementTypeGate
	required := amount
	if kind == "settlements" {
		_, recipient, recipientErr := s.readSettlementConfig(ctx, tx, merchantID)
		if recipientErr != nil && !errors.Is(recipientErr, ErrNotFound) {
			return LedgerRequest{}, recipientErr
		}
		gate, railErr := requireSettlementType(ctx, tx, merchantID, recipient.Type, false)
		if railErr != nil {
			return LedgerRequest{}, railErr
		}
		railGate = &gate
		var policy SettlementPolicy
		var err error
		if threshold, ok := ctx.Value(settlementThresholdRequestKey{}).(*settlementThresholdRequest); ok {
			policy, err = s.thresholdRequestPolicy(ctx, tx, threshold, merchantID, externalNo, amount, configID)
		} else {
			var selected merchantSettlementSelection
			selected, err = readMerchantSettlementSelection(ctx, tx, merchantID, true)
			policy, selectedSettlementDeadline = selected.Policy, selected.ValidUntil
		}
		if err != nil {
			return LedgerRequest{}, err
		}
		if err = requireNewSettlementAccess(ctx, tx, merchantID); err != nil {
			return LedgerRequest{}, err
		}
		quote, err := policy.Quote(amount)
		if err != nil {
			return LedgerRequest{}, err
		}
		if expected != nil && (expected.FeeAmount != quote.FeeAmount || expected.NetAmount != quote.NetAmount) {
			return LedgerRequest{}, ErrConflict
		}
		terms = &quote
		required += policy.RetainedCents
	}
	var recipient any
	if recipientID > 0 {
		recipient = recipientID
		if e = tx.QueryRow(ctx, "SELECT enabled FROM merchants WHERE id=$1", recipientID).Scan(&enabled); e != nil {
			return LedgerRequest{}, e
		}
		if !enabled {
			return LedgerRequest{}, ErrState
		}
	}
	selectedCycle, e := s.requireOutgoingFundsCycle(ctx, tx, merchantID, required)
	if e != nil {
		return LedgerRequest{}, e
	}
	if e = requireFundsAction(ctx, tx, "funds-request", fmt.Sprintf("%s:%d:%s", kind, merchantID, externalNo)); e != nil {
		return LedgerRequest{}, e
	}
	// A membership expiring during confirmation may revoke this request's
	// settlement mode. Preserve that business refusal before generic stale
	// cycle/price checks; the later access and captured-term checks remain.
	if kind == "settlements" {
		if e = requireNewSettlementAccess(ctx, tx, merchantID); e != nil {
			return LedgerRequest{}, e
		}
	}
	if e = selectedCycle.checkDeadline(ctx, tx); e != nil {
		return LedgerRequest{}, e
	}
	if railGate != nil {
		if e = railGate.recheck(ctx, tx); e != nil {
			return LedgerRequest{}, e
		}
	}
	var createdAt any
	if terms != nil {
		// Account/policy lock waits may cross midnight. Fix one operation time
		// only after outgoing account locks, and use its day for both the quota
		// check and the recorded request. Transaction now() may be yesterday.
		var operationAt time.Time
		if e = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&operationAt); e != nil {
			return LedgerRequest{}, e
		}
		if window, ok := ctx.Value(settlementPlanRequestWindowKey{}).(settlementPlanRequestWindow); ok {
			day, err := settlementBusinessDate(operationAt, 0)
			if err != nil {
				return LedgerRequest{}, err
			}
			if day != window.Day {
				return LedgerRequest{}, errSettlementPlanDayChanged
			}
			if reason := settlementPlanAuthorityAt(window.Authority, operationAt); reason != "" {
				return LedgerRequest{}, settlementPlanWindowError(reason)
			}
		}
		if e = requireSettlementDaily(ctx, tx, merchantID, terms.DailyLimit, operationAt); e != nil {
			return LedgerRequest{}, e
		}
		// A membership can expire while account/confirmation locks are waiting.
		// Recheck the effective mode before creating this new financial intent.
		if e = requireNewSettlementAccess(ctx, tx, merchantID); e != nil {
			return LedgerRequest{}, e
		}
		// All possible group rows, membership and fee settings remain locked.
		// Only the captured membership deadline can invalidate this price; reuse
		// it instead of repeating the same locked configuration reads.
		if !selectedSettlementDeadline.IsZero() && !operationAt.Before(selectedSettlementDeadline) {
			return LedgerRequest{}, ErrConflict
		}
		createdAt = operationAt
	}
	v, e := scanLedgerRequest(tx.QueryRow(ctx, "INSERT INTO ledger_requests(kind,merchant_id,recipient_id,external_no,amount,created_at) VALUES($1,$2,$3,$4,$5,COALESCE($6::timestamptz,now())) RETURNING "+requestColumns, kind, merchantID, recipient, externalNo, amount, createdAt))
	if e != nil {
		return v, e
	}
	if kind == "settlements" {
		e = reserveSettlementFunding(ctx, tx, v)
	} else {
		e = post(ctx, tx, fmt.Sprintf("request:%d:reserve", v.ID), kind+" reserve", merchantID, "available", merchantID, "reserved", amount)
	}
	if e != nil {
		return v, e
	}
	if terms != nil {
		if e = persistSettlementTerms(ctx, tx, v.ID, *terms); e != nil {
			return v, e
		}
		v.Settlement = terms
		if e = s.snapshotSettlementRecipient(ctx, tx, v, configID); e != nil {
			return v, e
		}
	}
	var businessEventID int64
	if (kind == "settlements" || kind == "internal-transfers") && actor == fmt.Sprintf("merchant:%d", merchantID) && s.AdminBusinessSourceReady(ctx, tx) {
		e = tx.QueryRow(ctx, `INSERT INTO ledger_request_events(request_id,actor,status,business_notice_transaction) VALUES($1,$2,'pending',pg_current_xact_id()) RETURNING id`, v.ID, actor).Scan(&businessEventID)
	} else {
		_, e = tx.Exec(ctx, "INSERT INTO ledger_request_events(request_id,actor,status) VALUES($1,$2,'pending')", v.ID, actor)
	}
	if e != nil {
		return v, e
	}
	v, e = attachSettlementFunding(ctx, tx, v)
	if e != nil {
		return v, e
	}
	v, e = s.attachSettlementRecipient(ctx, tx, v)
	if e != nil {
		return v, e
	}
	if businessEventID > 0 {
		s.recordAdminBusinessRequest(ctx, tx, businessEventID)
	}
	if railGate != nil {
		if e = railGate.recheck(ctx, tx); e != nil {
			return LedgerRequest{}, e
		}
	}
	return v, nil
}
func (s *Store) ReviewFundsRequest(ctx context.Context, id int64, approve bool, actor string) (LedgerRequest, error) {
	return s.ReviewFundsRequestWithReason(ctx, id, approve, actor, "")
}

// ReviewFundsRequestWithReason records an optional 拒绝原因 alongside the review
// decision. The reason is stored on the immutable ledger_request_events row the
// decision writes; it is ignored on approval (the original console only prompted
// a reason when rejecting). An empty reason reproduces the prior behaviour.
func (s *Store) ReviewFundsRequestWithReason(ctx context.Context, id int64, approve bool, actor, reason string) (LedgerRequest, error) {
	if id <= 0 || actor == "" {
		return LedgerRequest{}, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return LedgerRequest{}, e
	}
	defer tx.Rollback(ctx)
	v, e := s.reviewFundsRequestTx(ctx, tx, id, approve, actor, reason)
	if e != nil {
		return v, e
	}
	return v, tx.Commit(ctx)
}

func (s *Store) reviewFundsRequestTx(ctx context.Context, tx pgx.Tx, id int64, approve bool, actor, reason string) (LedgerRequest, error) {
	v, e := scanLedgerRequest(tx.QueryRow(ctx, "SELECT "+requestColumns+" FROM ledger_requests WHERE id=$1 FOR UPDATE", id))
	if e != nil {
		return v, e
	}
	if e = s.verifyScheduledRequestSource(ctx, tx, v.ID); e != nil {
		return v, e
	}
	v, e = attachSettlementTerms(ctx, tx, v)
	if e != nil {
		return v, e
	}
	v, e = s.attachSettlementRecipient(ctx, tx, v)
	if e != nil {
		return v, e
	}
	target := "rejected"
	if approve {
		target = "approved"
	}
	if v.Status == target {
		return v, nil
	}
	if v.Status != "pending" {
		return v, ErrState
	}
	if e = requireFundsAction(ctx, tx, "funds-review", fmt.Sprintf("%d:%t", id, approve)); e != nil {
		return v, e
	}
	if !approve {
		var scoped bool
		if v.Kind == "settlements" {
			scoped, e = rejectSettlementFunding(ctx, tx, v)
		}
		if e == nil && !scoped {
			e = post(ctx, tx, fmt.Sprintf("request:%d:review", id), v.Kind+" rejected", v.MerchantID, "reserved", v.MerchantID, "available", v.Amount)
		}
	} else if v.Kind != "settlements" {
		var enabled bool
		if e = tx.QueryRow(ctx, "SELECT enabled FROM merchants WHERE id=$1", *v.RecipientID).Scan(&enabled); e != nil {
			return v, e
		}
		if !enabled {
			return v, ErrState
		}
		e = post(ctx, tx, fmt.Sprintf("request:%d:review", id), v.Kind+" approved", v.MerchantID, "reserved", *v.RecipientID, "available", v.Amount)
	}
	if e != nil {
		return v, e
	}
	if _, e = tx.Exec(ctx, "UPDATE ledger_requests SET status=$2,updated_at=now() WHERE id=$1", id, target); e != nil {
		return v, e
	}
	// The reason is a 拒绝原因 only; approvals never carry one. Empty stays NULL so
	// historical and auto-generated events are indistinguishable from "no note".
	var eventReason any
	if !approve && reason != "" {
		eventReason = reason
	}
	if _, e = tx.Exec(ctx, "INSERT INTO ledger_request_events(request_id,actor,status,reason) VALUES($1,$2,$3,$4)", id, actor, target, eventReason); e != nil {
		return v, e
	}
	v.Status = target
	v, e = attachSettlementFunding(ctx, tx, v)
	if e != nil {
		return v, e
	}
	return v, nil
}
