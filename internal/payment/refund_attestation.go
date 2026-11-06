package payment

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"goepay/internal/identity"
	"goepay/internal/ledger"
)

// Reasons an unfinished refund cannot be closed by an operator.
const (
	RefundAttestationFinal          = "final"           // the refund already has a final result
	RefundAttestationInFlight       = "in_flight"       // the provider request is still being sent
	RefundAttestationAutomaticQuery = "automatic_query" // the channel queries refund outcomes itself
	RefundAttestationCombined       = "combined_order"  // combined-order refunds are settled per sub-order
)

// RefundAttestationInput is what a super administrator records after checking
// a refund's outcome in the provider's merchant portal.
type RefundAttestationInput struct {
	State             string
	ProviderReference string
	Note              string
}

// RefundAttestationRecord is an immutable operator attestation.
type RefundAttestationRecord struct {
	State             string    `json:"state"`
	ProviderReference string    `json:"provider_reference"`
	Note              string    `json:"note"`
	AdminUsername     string    `json:"admin_username"`
	CreatedAt         time.Time `json:"created_at"`
}

// RefundAttestationView says whether an unfinished refund may be closed by an
// operator, and shows the attestation already recorded for it.
type RefundAttestationView struct {
	RefundID    string                   `json:"refund_id"`
	RefundNo    string                   `json:"refund_no"`
	TradeNo     string                   `json:"trade_no"`
	MerchantID  string                   `json:"merchant_id"`
	Amount      string                   `json:"amount"`
	Status      string                   `json:"status"`
	Sent        bool                     `json:"sent"`
	ChannelID   string                   `json:"channel_id"`
	Eligible    bool                     `json:"eligible"`
	Reason      string                   `json:"reason"`
	States      []string                 `json:"states"`
	Attestation *RefundAttestationRecord `json:"attestation"`
}

type refundAttestationTarget struct {
	refund      Refund
	tradeNo     string
	operationID int64
	channelID   int64
	opState     string
}

func validRefundAttestationText(s string, max int) bool {
	return strings.TrimSpace(s) != "" && utf8.ValidString(s) && utf8.RuneCountInString(s) <= max && !strings.ContainsAny(s, "\x00\r\n")
}

// loadRefundAttestationTarget locks the order and refund (in the same order as
// every refund writer) and the refund's provider operation, if one was sent.
func (s *Store) loadRefundAttestationTarget(ctx context.Context, tx pgx.Tx, refundID int64, lock bool) (refundAttestationTarget, error) {
	var t refundAttestationTarget
	var orderID int64
	if e := tx.QueryRow(ctx, "SELECT order_id FROM refunds WHERE id=$1", refundID).Scan(&orderID); e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return t, ErrNotFound
		}
		return t, e
	}
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	if e := tx.QueryRow(ctx, "SELECT trade_no FROM orders WHERE id=$1"+suffix, orderID).Scan(&t.tradeNo); e != nil {
		return t, e
	}
	r, e := scanRefund(tx.QueryRow(ctx, "SELECT "+refundColumns+" FROM refunds WHERE id=$1"+suffix, refundID))
	if e != nil {
		return t, e
	}
	t.refund = r
	e = tx.QueryRow(ctx, "SELECT id,channel_id,state FROM provider_operations WHERE refund_id=$1 AND kind='refund'"+suffix, refundID).Scan(&t.operationID, &t.channelID, &t.opState)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return t, e
	}
	return t, nil
}

// refundAttestationEligibility explains whether an operator may close the
// refund. Only refunds that no automatic path can finish qualify: an unknown or
// pending send on a channel without refund-result queries, or a reservation
// that was never sent (which can only have failed).
func (g ProviderFlow) refundAttestationEligibility(ctx context.Context, tx pgx.Tx, t refundAttestationTarget) (string, []string, error) {
	r := t.refund
	if r.Status == "succeeded" || r.Status == "failed" {
		return RefundAttestationFinal, nil, nil
	}
	subs, e := g.Store.combineSubOrdersTx(ctx, tx, r.OrderID, false)
	if e != nil {
		return "", nil, e
	}
	if len(subs) > 0 {
		return RefundAttestationCombined, nil, nil
	}
	if t.operationID == 0 {
		return "", []string{"failed"}, nil
	}
	if t.opState != "pending" && t.opState != "unknown" {
		if t.opState == "succeeded" || t.opState == "failed" {
			return RefundAttestationFinal, nil, nil
		}
		return RefundAttestationInFlight, nil, nil
	}
	_, _, p, err := g.ownedTx(ctx, tx, r.MerchantID, t.tradeNo)
	if err != nil {
		return "", nil, err
	}
	if p.Capabilities().RefundQuery {
		return RefundAttestationAutomaticQuery, nil, nil
	}
	return "", []string{"succeeded", "failed"}, nil
}

func (g ProviderFlow) refundAttestationView(ctx context.Context, tx pgx.Tx, t refundAttestationTarget) (RefundAttestationView, error) {
	r := t.refund
	view := RefundAttestationView{RefundID: strconv.FormatInt(r.ID, 10), RefundNo: r.RefundNo, TradeNo: t.tradeNo, MerchantID: strconv.FormatInt(r.MerchantID, 10), Amount: ledger.Format(r.Amount), Status: r.Status, Sent: t.operationID > 0, States: []string{}}
	if t.channelID > 0 {
		view.ChannelID = strconv.FormatInt(t.channelID, 10)
	}
	reason, states, e := g.refundAttestationEligibility(ctx, tx, t)
	if e != nil {
		return view, e
	}
	view.Reason, view.Eligible = reason, reason == ""
	if view.Eligible {
		view.States = states
	}
	var record RefundAttestationRecord
	e = tx.QueryRow(ctx, "SELECT state,provider_reference,note,admin_username,created_at FROM refund_operator_attestations WHERE refund_id=$1", r.ID).Scan(&record.State, &record.ProviderReference, &record.Note, &record.AdminUsername, &record.CreatedAt)
	if e == nil {
		view.Attestation = &record
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return view, e
	}
	return view, nil
}

// RefundAttestation reads whether a refund may be closed by an operator.
func (g ProviderFlow) RefundAttestation(ctx context.Context, actor MerchantCredentialActor, refundID int64) (RefundAttestationView, error) {
	if g.Store == nil || refundID <= 0 {
		return RefundAttestationView{}, ErrInvalid
	}
	// Not read-only: the session check takes a FOR SHARE lock on the account.
	tx, e := g.Store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if e != nil {
		return RefundAttestationView{}, e
	}
	defer tx.Rollback(ctx)
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, actor.User, actor.SessionToken, actor.Delegated); e != nil {
		return RefundAttestationView{}, e
	}
	t, e := g.Store.loadRefundAttestationTarget(ctx, tx, refundID, false)
	if e != nil {
		return RefundAttestationView{}, e
	}
	view, e := g.refundAttestationView(ctx, tx, t)
	if e != nil {
		return view, e
	}
	return view, tx.Commit(ctx)
}

// AttestRefund records a super administrator's verified outcome for a refund
// that no automatic path can finish, and settles it exactly like a provider
// result would: ledger resolution, immutable evidence (source "operator", or
// "internal" for a reservation that was never sent), attestation and audit,
// all in one transaction after a fresh funds authorization. A final result
// cannot be changed afterwards.
func (g ProviderFlow) AttestRefund(ctx context.Context, actor MerchantCredentialActor, refundID int64, in RefundAttestationInput) (RefundAttestationView, error) {
	if g.Store == nil || refundID <= 0 || (in.State != "succeeded" && in.State != "failed") || !validRefundAttestationText(in.ProviderReference, 128) || !validRefundAttestationText(in.Note, 500) {
		return RefundAttestationView{}, ErrInvalid
	}
	s := g.Store
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return RefundAttestationView{}, e
	}
	defer tx.Rollback(ctx)
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, actor.User, actor.SessionToken, actor.Delegated); e != nil {
		return RefundAttestationView{}, e
	}
	t, e := s.loadRefundAttestationTarget(ctx, tx, refundID, true)
	if e != nil {
		return RefundAttestationView{}, e
	}
	reason, states, e := g.refundAttestationEligibility(ctx, tx, t)
	if e != nil {
		return RefundAttestationView{}, e
	}
	allowed := false
	for _, state := range states {
		allowed = allowed || state == in.State
	}
	if reason != "" || !allowed {
		return RefundAttestationView{}, ErrState
	}
	r := t.refund
	if e = s.verifyReferralRefundEvidence(ctx, tx, r); e != nil {
		return RefundAttestationView{}, e
	}
	if e = requireFundsAction(ctx, tx, "refund-attestation", fmt.Sprintf("%d:%s", refundID, in.State)); e != nil {
		return RefundAttestationView{}, e
	}
	if e = s.resolveRefundTx(ctx, tx, refundID, in.State); e != nil {
		return RefundAttestationView{}, e
	}
	var operation any
	if t.operationID > 0 {
		operation = t.operationID
	}
	if _, e = tx.Exec(ctx, `INSERT INTO refund_operator_attestations(refund_id,operation_id,state,provider_reference,note,admin_username,admin_id,admin_auth_version) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, refundID, operation, in.State, in.ProviderReference, in.Note, actor.User.Username, actor.User.ID, actor.User.AuthVersion); e != nil {
		return RefundAttestationView{}, e
	}
	evidence := providerRefundResultEvidence{RefundID: r.ID, OrderID: r.OrderID, Amount: r.Amount, RefundNo: r.RefundNo, PriorRefundState: r.Status, State: in.State, Source: "internal"}
	if t.operationID > 0 {
		evidence.OperationID, evidence.ChannelID, evidence.PriorOperationState, evidence.Source = t.operationID, t.channelID, t.opState, "operator"
	}
	if e = s.insertRefundResultEvidence(ctx, tx, r, evidence); e != nil {
		return RefundAttestationView{}, e
	}
	if t.operationID > 0 {
		if _, e = tx.Exec(ctx, "UPDATE provider_operations SET state=$2,updated_at=now() WHERE id=$1", t.operationID, in.State); e != nil {
			return RefundAttestationView{}, e
		}
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'refunds',$3,'operator-attestation',jsonb_build_object('state',$4::text,'provider_reference',$5::text,'note',$6::text,'operation_id',$7::bigint,'prior_status',$8::text))`, actor.User.ID, actor.User.Username, refundID, in.State, in.ProviderReference, in.Note, t.operationID, r.Status); e != nil {
		return RefundAttestationView{}, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, actor.User, actor.SessionToken, actor.Delegated); e != nil {
		return RefundAttestationView{}, e
	}
	t, e = s.loadRefundAttestationTarget(ctx, tx, refundID, false)
	if e != nil {
		return RefundAttestationView{}, e
	}
	view, e := g.refundAttestationView(ctx, tx, t)
	if e != nil {
		return view, e
	}
	return view, tx.Commit(ctx)
}
