package payment

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type referralPromoterGroupView struct {
	Enabled, Unresolved  bool
	BaseGroupID, GroupID int64
	ExpiresAt            *time.Time
}

// The engine keeps its existing merchant FOR SHARE lock and membership expiry
// predicate. A read-only explanation selects the same facts without row locks.
func (s *Store) referralPromoterGroup(ctx context.Context, q referralReader, mid int64, lock bool) (referralPromoterGroupView, error) {
	var out referralPromoterGroupView
	query := "SELECT enabled,group_baseline_unresolved,base_group_id FROM merchants WHERE id=$1"
	if lock {
		query += " FOR SHARE"
	}
	if e := q.QueryRow(ctx, query, mid).Scan(&out.Enabled, &out.Unresolved, &out.BaseGroupID); e != nil {
		return out, e
	}
	if out.Unresolved {
		return out, nil
	}
	out.GroupID = out.BaseGroupID
	var expiry time.Time
	e := q.QueryRow(ctx, "SELECT group_id,expires_at FROM memberships WHERE merchant_id=$1 AND expires_at>statement_timestamp()", mid).Scan(&out.GroupID, &expiry)
	if e == nil {
		out.ExpiresAt = &expiry
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	return out, nil
}

type ReferralCurrentGroup struct {
	ID          *int64     `json:"id,string"`
	BaseGroupID *int64     `json:"base_group_id,string"`
	Source      string     `json:"source"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}
type ReferralCurrentInheritance struct {
	Enabled string `json:"enabled"`
	RateBPS string `json:"rate_bps"`
	Basis   string `json:"basis"`
	Mode    string `json:"mode"`
}
type ReferralCurrentExample struct {
	BasisAmount         int64  `json:"basis_amount,string"`
	PercentageAmount    int64  `json:"percentage_amount,string"`
	OrderEstimateStatus string `json:"order_estimate_status"`
	OrderEstimate       *int64 `json:"order_estimate,string"`
}

// This DTO contains no bearer token, other merchant, private channel cost,
// policy proof or historical reward. It explains current common conditions;
// an unknown future payer group is never represented as a universal policy.
type ReferralCurrentPolicy struct {
	MerchantID          int64                      `json:"merchant_id,string"`
	Status              string                     `json:"status"`
	Enabled             *bool                      `json:"enabled"`
	RateBPS             *int64                     `json:"rate_bps"`
	Basis               string                     `json:"basis"`
	Mode                string                     `json:"mode"`
	PaymentFeeEnabled   bool                       `json:"payment_fee_enabled"`
	PayerGroupDependent bool                       `json:"payer_group_dependent"`
	CurrentGroup        ReferralCurrentGroup       `json:"current_group"`
	Inheritance         ReferralCurrentInheritance `json:"inheritance"`
	RefundPolicy        string                     `json:"refund_policy"`
	Example             *ReferralCurrentExample    `json:"example"`
}

func (s *Store) ReferralCurrentPolicy(ctx context.Context, mid int64, actor TransferBatchActor) (ReferralCurrentPolicy, error) {
	out := ReferralCurrentPolicy{MerchantID: mid, Status: "unknown", Basis: "unknown", Mode: "unknown", RefundPolicy: referralRefundPolicy, CurrentGroup: ReferralCurrentGroup{Source: "unknown"}, Inheritance: ReferralCurrentInheritance{Enabled: "default", RateBPS: "default", Basis: "default", Mode: "default"}}
	if mid <= 0 || actor.Role != "merchant" || actor.ID != mid || !actor.valid(mid) {
		return out, ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	group, e := s.referralPromoterGroup(ctx, tx, mid, false)
	if e != nil {
		return out, e
	}
	rows, e := tx.Query(ctx, `SELECT body->>'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'='payment_fee_enabled'`)
	if e != nil {
		return out, e
	}
	count := 0
	for rows.Next() {
		var value string
		if e = rows.Scan(&value); e != nil {
			rows.Close()
			return out, e
		}
		count++
		if count > 1 || value != "true" && value != "false" {
			rows.Close()
			return out, ErrConflict
		}
		out.PaymentFeeEnabled = value == "true"
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return out, e
	}
	if group.Unresolved {
		return out, nil
	}
	out.CurrentGroup = ReferralCurrentGroup{ID: &group.GroupID, BaseGroupID: &group.BaseGroupID, Source: "base", ExpiresAt: group.ExpiresAt}
	if group.ExpiresAt != nil {
		out.CurrentGroup.Source = "membership"
	}
	policy := effectiveReferralPolicy{Basis: "fee", Mode: "immediate"}
	var own *ReferralPolicyInput
	for _, scope := range []struct {
		kind   string
		id     int64
		source string
	}{{"global", 0, "global"}, {"group", group.GroupID, "promoter_group"}} {
		if scope.kind == "group" && scope.id == 0 {
			continue
		}
		r, err := s.readReferralPolicyTx(ctx, tx, scope.kind, scope.id)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return out, err
		}
		policy = mergeEffectiveReferralPolicy(policy, r.ReferralPolicyInput)
		out.Inheritance.Basis, out.Inheritance.Mode = scope.source, scope.source
		if r.Enabled != nil {
			out.Inheritance.Enabled = scope.source
		}
		if r.RateBPS != nil {
			out.Inheritance.RateBPS = scope.source
		}
		if scope.kind == "group" {
			input := r.ReferralPolicyInput
			own = &input
		}
	}
	var other bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM referral_policy_heads WHERE scope_kind='group' AND scope_id<>$1)`, group.GroupID).Scan(&other); e != nil {
		return out, e
	}
	out.PayerGroupDependent = other && (own == nil || own.Enabled == nil || own.RateBPS == nil)
	out.Enabled, out.RateBPS = &policy.Enabled, &policy.RateBPS
	out.Basis, out.Mode = policy.Basis, policy.Mode
	switch {
	case !group.Enabled:
		out.Status = "merchant_disabled"
	case !out.PaymentFeeEnabled:
		out.Status = "budget_disabled"
	case own != nil && own.Enabled != nil && !*own.Enabled:
		out.Status = "disabled"
	case own != nil && own.RateBPS != nil && *own.RateBPS == 0:
		out.Status = "zero_rate"
	case out.PayerGroupDependent:
		out.Status = "conditional"
	case !policy.Enabled:
		out.Status = "disabled"
	case policy.RateBPS == 0:
		out.Status = "zero_rate"
	default:
		out.Status = "active"
	}
	sample, e := calculateReferralReward(10000, policy.RateBPS)
	if e != nil {
		return out, e
	}
	out.Example = &ReferralCurrentExample{BasisAmount: 10000, PercentageAmount: sample, OrderEstimateStatus: "unknown"}
	return out, nil
}
