package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/identity"
	"goepay/internal/ledger"
	"goepay/internal/payment"
)

type fundsTargetDefinition struct {
	guard, category, id, verb string
	keys                      []string
}

func positiveFundsID(s string) (int64, bool) {
	n, e := strconv.ParseInt(s, 10, 64)
	return n, e == nil && n > 0 && strconv.FormatInt(n, 10) == s
}
func fundsTarget(target string) (fundsTargetDefinition, bool) {
	var d fundsTargetDefinition
	if !strings.HasPrefix(target, "/console/v1/admin/") || len(target) > 512 || strings.ContainsAny(target, "?%#\\\x00\r\n") || path.Clean(target) != target {
		return d, false
	}
	p := strings.Split(strings.TrimPrefix(target, "/console/v1/admin/"), "/")
	if len(p) == 4 && p[0] == "orders" && p[1] == "business-batches" && p[3] == "execute" {
		if _, ok := positiveFundsID(p[2]); !ok {
			return d, false
		}
		return fundsTargetDefinition{guard: "order-business-batch", category: "order-business-batch", id: p[2], verb: "execute", keys: []string{"limit"}}, true
	}
	if len(p) == 3 && p[0] == "channel-stops" && p[2] == "resume" {
		if _, ok := positiveFundsID(p[1]); !ok {
			return d, false
		}
		return fundsTargetDefinition{guard: "channel-resume", category: "channel-resume", id: p[1], verb: p[2], keys: []string{"expected_event_id", "expected_revision"}}, true
	}
	if d, ok := settlementPlanFundsTarget(p); ok {
		return d, true
	}
	if d, ok := settlementAutoFundsTarget(p); ok {
		return d, true
	}
	if d, ok := referralPolicyFundsTarget(p); ok {
		return d, true
	}
	if d, ok := membershipReferralPolicyFundsTarget(p); ok {
		return d, true
	}
	if d, ok := channelSharingFundsTarget(p); ok {
		return d, true
	}
	if len(p) == 3 && p[0] == "settlement-payouts" {
		if _, ok := positiveFundsID(p[1]); !ok {
			return d, false
		}
		switch p[2] {
		case "execute":
			return fundsTargetDefinition{guard: "settlement-payout", category: "settlement-payout", id: p[1], verb: p[2]}, true
		case "cancel-unsent":
			return fundsTargetDefinition{guard: "settlement-payout-cancel", category: "settlement-payout", id: p[1], verb: p[2]}, true
		}
	}
	if len(p) == 1 && p[0] == "transfers" {
		return fundsTargetDefinition{guard: "transfer", category: "transfer", keys: []string{"merchant_id", "channel_id", "type", "account", "name", "amount", "remark", "external_no", "bank_code"}}, true
	}
	if len(p) == 1 && p[0] == "transfer-batches" {
		return fundsTargetDefinition{guard: "batch-prepare", category: "batch-prepare", keys: []string{"merchant_id", "external_no", "items"}}, true
	}
	if len(p) == 3 && p[0] == "transfer-batches" {
		if _, ok := positiveFundsID(p[1]); !ok {
			return d, false
		}
		if p[2] == "execute" {
			return fundsTargetDefinition{guard: "batch-execute", category: "batch", id: p[1], verb: p[2], keys: []string{"limit"}}, true
		}
		if p[2] == "cancel-unsent" {
			return fundsTargetDefinition{guard: "batch-cancel", category: "batch", id: p[1], verb: p[2]}, true
		}
	}
	if len(p) == 3 && p[0] == "orders" && len(p[1]) > 0 && len(p[1]) <= 64 {
		d = fundsTargetDefinition{category: "order", id: p[1], verb: p[2]}
		switch p[2] {
		case "refund":
			d.guard = "refund"
			d.keys = []string{"amount", "external_no"}
		case "manual-refund":
			d.guard = "refund"
			d.keys = []string{"amount", "external_no", "reason", "offline_confirmed"}
		case "close":
			d.guard = "order-close"
		case "capture-authorization":
			d.guard = "authorization-capture"
		case "release-authorization":
			d.guard = "authorization-release"
		case "release-remaining-authorization":
			d.guard = "authorization-release-remaining"
			d.keys = []string{"expected_observation_id", "expected_remaining_cents", "expected_source_revision"}
		case "freeze-income":
			d.guard = "income-freeze"
			d.keys = []string{"external_no"}
		case "unfreeze-income":
			d.guard = "income-unfreeze"
			d.keys = []string{"hold_id"}
		case "adopt-income-hold":
			d.guard = "income-hold-adopt"
			d.keys = []string{"hold_id", "external_no"}
		default:
			return d, false
		}
		return d, true
	}
	if len(p) >= 2 && p[0] == "workflows" {
		d = fundsTargetDefinition{category: p[1]}
		if len(p) == 2 {
			switch p[1] {
			case "settlements", "internal-transfers", "profitsharing":
				d.guard = "funds-request"
				d.keys = []string{"merchant_id", "recipient_id", "amount", "external_no", "expected_fee_cents", "expected_net_cents", "recipient_config_id"}
			case "memberships":
				d.guard = "membership"
				d.keys = []string{"merchant_id", "group_id", "external_no", "quantity"}
			case "deposits":
				d.guard = "deposit"
				d.keys = []string{"merchant_id", "action", "amount", "external_no"}
			case "balance-adjustments":
				d.guard = "admin-adjust"
				d.keys = []string{"merchant_id", "direction", "amount", "reason", "external_no"}
			default:
				return d, false
			}
			return d, true
		}
		if len(p) == 4 {
			if _, ok := positiveFundsID(p[2]); !ok {
				return d, false
			}
			d.id = p[2]
			d.verb = p[3]
			if p[1] == "profitsharing-rules" && p[3] == "execute" {
				d.guard = "funds-request"
				d.keys = []string{"trade_no"}
				return d, true
			}
			if p[3] == "approve" || p[3] == "reject" {
				switch p[1] {
				case "settlements", "internal-transfers", "profitsharing":
					d.guard = "funds-review"
					return d, true
				case "deposits":
					d.guard = "deposit-review"
					return d, true
				}
			}
		}
	}
	return d, false
}

func normalizeFundsBody(raw []byte, d fundsTargetDefinition) (map[string]json.RawMessage, []byte, error) {
	if d.category == "settlement-auto" {
		return normalizeSettlementAutoBody(raw)
	}
	if d.category == "settlement-plan" {
		return normalizeSettlementPlanFundsBody(raw, d)
	}
	if d.category == "referral-policy" {
		return normalizeReferralPolicyFundsBody(raw, d)
	}
	if d.category == "membership-referral-policy" {
		return normalizeMembershipReferralPolicyFundsBody(raw, d)
	}
	if strings.HasPrefix(d.category, "channel-sharing") {
		return normalizeSharingFundsBody(raw, d)
	}
	var fields map[string]json.RawMessage
	if e := strictFundsJSON(raw, &fields); e != nil {
		return nil, nil, payment.ErrInvalid
	}
	allowed := map[string]bool{}
	for _, k := range d.keys {
		allowed[k] = true
	}
	for k, v := range fields {
		if !allowed[k] {
			return nil, nil, payment.ErrInvalid
		}
		if k == "items" {
			var items []batchConsoleItem
			if validateFundsJSONFields(v, &items) != nil {
				return nil, nil, payment.ErrInvalid
			}
			decoder := json.NewDecoder(bytes.NewReader(v))
			decoder.DisallowUnknownFields()
			if e := decoder.Decode(&items); e != nil || len(items) < 1 || len(items) > 100 {
				return nil, nil, payment.ErrInvalid
			}
			for _, item := range items {
				if _, e := ledger.Parse(item.Amount); e != nil {
					return nil, nil, payment.ErrInvalid
				}
			}
			v, _ = json.Marshal(items)
			fields[k] = v
			continue
		}
		if k == "quantity" {
			var n int
			if json.Unmarshal(v, &n) != nil || n < 1 || n > 300 {
				return nil, nil, payment.ErrInvalid
			}
			continue
		}
		if k == "limit" {
			var n int
			if json.Unmarshal(v, &n) != nil || n < 1 || n > 100 {
				return nil, nil, payment.ErrInvalid
			}
			continue
		}
		if k == "offline_confirmed" {
			var confirmed bool
			if json.Unmarshal(v, &confirmed) != nil || !confirmed {
				return nil, nil, payment.ErrInvalid
			}
			continue
		}
		var text string
		if json.Unmarshal(v, &text) != nil {
			return nil, nil, payment.ErrInvalid
		}
		switch k {
		case "merchant_id", "recipient_id", "group_id", "channel_id", "hold_id", "recipient_config_id":
			if text != "" {
				if _, ok := positiveFundsID(text); !ok {
					return nil, nil, payment.ErrInvalid
				}
			}
		case "expected_fee_cents", "expected_net_cents":
			n, e := strconv.ParseInt(text, 10, 64)
			if e != nil || n < 0 || strconv.FormatInt(n, 10) != text {
				return nil, nil, payment.ErrInvalid
			}
		case "amount":
			n, e := ledger.Parse(text)
			if e != nil || n <= 0 {
				return nil, nil, payment.ErrInvalid
			}
			text = ledger.Format(n)
		case "external_no":
			if text == "" || len(text) > 128 {
				return nil, nil, payment.ErrInvalid
			}
		case "reason":
			if strings.TrimSpace(text) == "" || len(text) > 240 || strings.ContainsAny(text, "\x00\r\n") {
				return nil, nil, payment.ErrInvalid
			}
		case "direction":
			if text != "credit" && text != "debit" {
				return nil, nil, payment.ErrInvalid
			}
		}
		fields[k], _ = json.Marshal(text)
	}
	if d.verb == "manual-refund" && (len(fields) != 4 || fields["offline_confirmed"] == nil || fundsText(fields, "reason") == "") {
		return nil, nil, payment.ErrInvalid
	}
	if d.guard == "income-hold-adopt" {
		if len(fields) != 2 || fundsText(fields, "external_no") == "" {
			return nil, nil, payment.ErrInvalid
		}
		if _, ok := positiveFundsID(fundsText(fields, "hold_id")); !ok {
			return nil, nil, payment.ErrInvalid
		}
	}
	if d.guard == "authorization-release-remaining" {
		if _, e := remainingReleaseInput(fundsText(fields, "expected_observation_id"), fundsText(fields, "expected_remaining_cents"), fundsText(fields, "expected_source_revision")); e != nil || len(fields) != 3 {
			return nil, nil, payment.ErrInvalid
		}
	}
	if d.guard == "channel-resume" {
		if len(fields) != 2 {
			return nil, nil, payment.ErrInvalid
		}
		if _, ok := positiveFundsID(fundsText(fields, "expected_event_id")); !ok {
			return nil, nil, payment.ErrInvalid
		}
		revision, ok := positiveFundsID(fundsText(fields, "expected_revision"))
		if !ok || revision >= 4096 {
			return nil, nil, payment.ErrInvalid
		}
	}
	canonical, e := json.Marshal(fields)
	return fields, canonical, e
}
func fundsText(fields map[string]json.RawMessage, key string) string {
	var s string
	json.Unmarshal(fields[key], &s)
	return s
}

// Existing business handlers accept strconv.ParseInt aliases such as 01/+1.
// Preview rejects those noncanonical paths, but execution still needs a guard;
// otherwise an alias could evade the protected-route allowlist entirely.
func fundsExecutionTarget(target string) (fundsTargetDefinition, bool) {
	if d, ok := fundsTarget(target); ok {
		return d, true
	}
	if !strings.HasPrefix(target, "/console/v1/admin/") {
		return fundsTargetDefinition{}, false
	}
	p := strings.Split(strings.TrimPrefix(target, "/console/v1/admin/"), "/")
	index := -1
	if len(p) == 3 && (p[0] == "transfer-batches" || p[0] == "settlement-payouts") {
		index = 1
	}
	if len(p) == 4 && p[0] == "workflows" {
		index = 2
	}
	if index < 0 {
		return fundsTargetDefinition{}, false
	}
	n, e := strconv.ParseInt(p[index], 10, 64)
	if e != nil || n <= 0 {
		return fundsTargetDefinition{}, false
	}
	p[index] = strconv.FormatInt(n, 10)
	return fundsTarget("/console/v1/admin/" + strings.Join(p, "/"))
}
func fundsJSONRow(ctx context.Context, q identity.FundsQuerier, sql string, args ...any) (json.RawMessage, error) {
	var raw []byte
	e := q.QueryRow(ctx, sql, args...).Scan(&raw)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, payment.ErrNotFound
	}
	return json.RawMessage(raw), e
}

// ResolveFundsAction is the shared preview and in-transaction execution builder.
// Only keyed fingerprints of the canonical body and DB snapshots are retained.
func (s *Server) ResolveFundsAction(ctx context.Context, q identity.FundsQuerier, target string, raw json.RawMessage) (identity.FundsAction, map[string]string, error) {
	d, ok := fundsTarget(target)
	if !ok || s.Store == nil || q == nil {
		return identity.FundsAction{}, nil, payment.ErrInvalid
	}
	fields, canonical, e := normalizeFundsBody(raw, d)
	if e != nil {
		return identity.FundsAction{}, nil, e
	}
	if d.category == "order-business-batch" {
		return s.resolveOrderBusinessBatchFunds185(ctx, q, d, target, canonical)
	}
	summary := map[string]string{"action": d.guard, "target": target}
	snapshot := map[string]any{}
	mids := map[int64]bool{}
	channels := map[int64]bool{}
	addMerchant := func(mid int64) {
		if mid > 0 {
			mids[mid] = true
		}
	}
	mid, _ := positiveFundsID(fundsText(fields, "merchant_id"))
	addMerchant(mid)
	if recipient, ok := positiveFundsID(fundsText(fields, "recipient_id")); ok {
		addMerchant(recipient)
	}
	for _, key := range []string{"amount", "external_no"} {
		if v := fundsText(fields, key); v != "" {
			summary[key] = v
		}
	}
	switch d.category {
	case "channel-resume":
		channel, _ := positiveFundsID(d.id)
		event, _ := positiveFundsID(fundsText(fields, "expected_event_id"))
		revision, _ := positiveFundsID(fundsText(fields, "expected_revision"))
		snapshot["channel_stop"], e = s.Store.ChannelResumeSnapshot(ctx, q, channel, event, revision)
		if e != nil {
			return identity.FundsAction{}, nil, e
		}
		summary["channel_id"], summary["expected_event_id"], summary["expected_revision"] = d.id, fmt.Sprint(event), fmt.Sprint(revision)
		var stopped struct {
			Code string `json:"code"`
		}
		if json.Unmarshal(snapshot["channel_stop"].(json.RawMessage), &stopped) != nil || stopped.Code == "" {
			return identity.FundsAction{}, nil, payment.ErrInvalid
		}
		summary["channel_stop_code"] = stopped.Code
		if s.ProviderFlow == nil || s.ProviderFlow.Providers[channel] == nil {
			return identity.FundsAction{}, nil, payment.ErrInvalid
		}
		current := s.ProviderFlow.Providers[channel]
		account, e := adapter.TransferAccountScope(current)
		if e != nil {
			return identity.FundsAction{}, nil, e
		}
		product, ok := current.(adapter.TransferProductIdentity)
		if !ok || product.TransferProductIdentity() == "" {
			return identity.FundsAction{}, nil, payment.ErrInvalid
		}
		snapshot["channel_runtime"] = []string{current.ID(), account, product.TransferProductIdentity()}
		summary["provider"], summary["product"] = current.ID(), product.TransferProductIdentity()
	case "settlement-auto":
		var choices []payment.SettlementAutoChannel
		snapshot["settlement_auto"], choices, e = s.fundsSettlementAuto(ctx, q, d, fields, summary)
		if e != nil {
			return identity.FundsAction{}, nil, e
		}
		for _, choice := range choices {
			addMerchant(choice.MerchantID)
			channels[choice.ChannelID] = true
		}
	case "settlement-plan":
		snapshot["settlement_plan"], e = s.fundsSettlementPlan(ctx, q, d, fields, summary)
		if e != nil {
			return identity.FundsAction{}, nil, e
		}
	case "referral-policy":
		snapshot["referral_policy"], e = s.fundsReferralPolicy(ctx, q, d, fields, summary)
		if e != nil {
			return identity.FundsAction{}, nil, e
		}
	case "membership-referral-policy":
		snapshot["membership_referral_policy"], e = s.fundsMembershipReferralPolicy(ctx, q, d, fields, summary)
		if e != nil {
			return identity.FundsAction{}, nil, e
		}
	case "channel-sharing", "channel-sharing-rule", "channel-sharing-disable", "channel-sharing-bind", "channel-sharing-unbind", "channel-sharing-return", "channel-sharing-finish":
		mid, channel, value, err := s.fundsChannelSharing(ctx, q, d, fields, summary)
		if err != nil {
			return identity.FundsAction{}, nil, err
		}
		addMerchant(mid)

		if channel > 0 {
			channels[channel] = true
		}
		snapshot["channel_sharing"] = value
		summary["merchant_id"] = fmt.Sprint(mid)
	case "settlement-payout":
		id, _ := positiveFundsID(d.id)
		var channel int64
		snapshot["wallet_payout"], mid, channel, e = s.fundsSettlementPayout(ctx, q, id, summary)
		if e != nil {
			return identity.FundsAction{}, nil, e
		}
		addMerchant(mid)
		channels[channel] = true
	case "transfer":
		if mid <= 0 || fundsText(fields, "external_no") == "" || fundsText(fields, "account") == "" || fundsText(fields, "amount") == "" {
			return identity.FundsAction{}, nil, payment.ErrInvalid
		}
		// Resolve defaults by the same runtime merchant whitelist as the real route.
		if s.ProviderFlow == nil {
			return identity.FundsAction{}, nil, payment.ErrLiveDisabled
		}
		channel, err := s.transferChannel(mid, compat.Params{"type": fundsText(fields, "type"), "channel_id": fundsText(fields, "channel_id")})
		if err != nil {
			return identity.FundsAction{}, nil, err
		}
		channels[channel] = true
		summary["channel_id"] = fmt.Sprint(channel)
		summary["recipient"] = "***"
	case "batch-prepare":
		if mid <= 0 || fundsText(fields, "external_no") == "" || s.ProviderFlow == nil {
			return identity.FundsAction{}, nil, payment.ErrInvalid
		}
		var items []batchConsoleItem
		if json.Unmarshal(fields["items"], &items) != nil || len(items) == 0 {
			return identity.FundsAction{}, nil, payment.ErrInvalid
		}
		for _, item := range items {
			channel, err := s.transferChannel(mid, compat.Params{"type": item.Type, "channel_id": item.ChannelID})
			if err != nil {
				return identity.FundsAction{}, nil, err
			}
			channels[channel] = true
		}
		summary["items_count"] = fmt.Sprint(len(items))
	case "order":
		var orderID, channel int64
		if e = q.QueryRow(ctx, "SELECT id,merchant_id FROM orders WHERE trade_no=$1", d.id).Scan(&orderID, &mid); errors.Is(e, pgx.ErrNoRows) {
			e = payment.ErrNotFound
		}
		if e != nil {
			return identity.FundsAction{}, nil, e
		}
		addMerchant(mid)
		if d.guard == "authorization-release-remaining" {
			in, err := remainingReleaseInput(fundsText(fields, "expected_observation_id"), fundsText(fields, "expected_remaining_cents"), fundsText(fields, "expected_source_revision"))
			if err != nil {
				return identity.FundsAction{}, nil, err
			}
			snapshot["remaining_release"], e = s.Store.PreauthRemainingConfirmationSnapshot(ctx, q, mid, d.id, in)
			if e != nil {
				return identity.FundsAction{}, nil, e
			}
			summary["release_remaining_amount"] = ledger.Format(in.ExpectedRemainingCents)
			summary["release_source_revision"] = fmt.Sprint(in.ExpectedSourceRevision)
			summary["authorization_observation_id"] = in.ExpectedObservationID
			summary["release_policy"] = "只解除本次核对的买家剩余冻结；不会增加商户余额，发送后仅按原请求号查询"
		}
		snapshot["order"], e = fundsJSONRow(ctx, q, "SELECT to_jsonb(o)-'status'-'paid_at'-'created_at' FROM orders o WHERE id=$1", orderID)
		if e != nil {
			return identity.FundsAction{}, nil, e
		}
		if d.verb == "refund" || d.verb == "manual-refund" {
			amount, err := ledger.Parse(fundsText(fields, "amount"))
			if err != nil || amount <= 0 {
				return identity.FundsAction{}, nil, payment.ErrInvalid
			}
			funding, err := s.Store.RefundFundingPreview(ctx, q, orderID, fundsText(fields, "external_no"), amount)
			if err != nil {
				return identity.FundsAction{}, nil, err
			}
			snapshot["refund_funding"] = funding.AuthorizationEvidence()
			summary["income_hold_id"] = fmt.Sprint(funding.HoldID)
			summary["refund_funding_mode"] = funding.FundingMode
			summary["held_original"] = ledger.Format(funding.HeldOriginal)
			summary["held_remaining"] = ledger.Format(funding.HeldRemaining)
			summary["held_pending"] = ledger.Format(funding.HeldPending)
			summary["held_succeeded"] = ledger.Format(funding.HeldSucceeded)
			summary["refund_held_amount"] = ledger.Format(funding.HeldAmount)
			summary["refund_available_amount"] = ledger.Format(funding.AvailableAmount)
			summary["refund_fee_amount"] = ledger.Format(funding.FeeAmount)
			clawback, err := s.Store.ReferralRefundPreview(ctx, q, orderID, fundsText(fields, "external_no"), amount)
			if err != nil {
				return identity.FundsAction{}, nil, err
			}
			snapshot["referral_refund"] = clawback
			if clawback.PromoterID > 0 {
				addMerchant(clawback.PromoterID)
			}
			if clawback.RewardID > 0 {
				summary["referral_reward_id"] = fmt.Sprint(clawback.RewardID)
				summary["referral_promoter_id"] = fmt.Sprint(clawback.PromoterID)
				summary["referral_recovery_amount"] = ledger.Format(clawback.Amount)
				summary["referral_fee_contribution"] = ledger.Format(clawback.FeeContribution)
				summary["referral_recovery_state"] = clawback.ClawbackState
				summary["referral_refund_policy"] = "任意成功退款将撤销整笔原邀请奖励；未知结果保留回收预留，失败则恢复"
			} else if strings.HasPrefix(clawback.ClawbackState, "daily_") {
				summary["referral_promoter_id"] = fmt.Sprint(clawback.PromoterID)
				summary["referral_daily_cancellation_amount"] = ledger.Format(clawback.Amount)
				summary["referral_fee_contribution"] = ledger.Format(clawback.FeeContribution)
				summary["referral_recovery_state"] = clawback.ClawbackState
				summary["referral_refund_policy"] = "取消原订单尚未发放的邀请奖励，不扣推广商户可用余额；未知退款保留暂存，失败恢复原待发额，成功撤销整笔"
			}
		}
		if d.verb == "adopt-income-hold" {
			id, ok := positiveFundsID(fundsText(fields, "hold_id"))
			if !ok {
				return identity.FundsAction{}, nil, payment.ErrInvalid
			}
			snapshot["income_hold_adoption"], e = s.Store.IncomeHoldAdoptionSnapshot(ctx, q, mid, d.id, id, fundsText(fields, "external_no"))
			if e != nil {
				return identity.FundsAction{}, nil, e
			}
			var adoption struct {
				OriginalHold string `json:"original_hold"`
				SourceMode   string `json:"source_mode"`
			}
			if e = json.Unmarshal(snapshot["income_hold_adoption"].(json.RawMessage), &adoption); e != nil {
				return identity.FundsAction{}, nil, payment.ErrInvalid
			}
			amount, err := strconv.ParseInt(adoption.OriginalHold, 10, 64)
			if err != nil || amount <= 0 {
				return identity.FundsAction{}, nil, payment.ErrInvalid
			}
			summary["income_hold_id"] = fmt.Sprint(id)
			summary["income_hold_source_mode"] = adoption.SourceMode
			summary["held_original"] = ledger.Format(amount)
			summary["income_hold_adoption_policy"] = "核对历史冻结凭证和总额后，仅将此批原订单冻结款迁入专用来源；不增加余额，不发起外部退款"
		}
		e = q.QueryRow(ctx, "SELECT channel_id FROM provider_attempts WHERE order_id=$1", orderID).Scan(&channel)
		if e == nil {
			channels[channel] = true
			snapshot["attempt"], e = fundsJSONRow(ctx, q, "SELECT jsonb_build_object('channel',channel_id,'method',method) FROM provider_attempts WHERE order_id=$1", orderID)
		} else if errors.Is(e, pgx.ErrNoRows) {
			e = nil
		}
		if e != nil {
			return identity.FundsAction{}, nil, e
		}
		snapshot["authorization"], e = fundsJSONRow(ctx, q, "SELECT COALESCE((SELECT to_jsonb(a)-'capture_state'-'updated_at'-'created_at' FROM payment_authorizations a WHERE order_id=$1),'null'::jsonb)", orderID)
		if e != nil {
			return identity.FundsAction{}, nil, e
		}
		if d.verb == "unfreeze-income" {
			id, ok := positiveFundsID(fundsText(fields, "hold_id"))
			if !ok {
				return identity.FundsAction{}, nil, payment.ErrInvalid
			}
			snapshot["income_hold"], e = s.Store.IncomeHoldReleaseSnapshot(ctx, q, mid, d.id, id)
			if e != nil {
				return identity.FundsAction{}, nil, e
			}
			var release struct {
				Hold    payment.OrderIncomeHold `json:"hold"`
				Pending string                  `json:"pending_refunds"`
			}
			if json.Unmarshal(snapshot["income_hold"].(json.RawMessage), &release) != nil || release.Pending != "0" {
				return identity.FundsAction{}, nil, payment.ErrState
			}
			summary["income_hold_id"] = fmt.Sprint(id)
			summary["held_remaining"] = ledger.Format(release.Hold.RemainingAmount)
			summary["held_succeeded"] = ledger.Format(release.Hold.RefundSucceededAmount)
		}
	case "batch":
		id, _ := positiveFundsID(d.id)
		if e = q.QueryRow(ctx, "SELECT merchant_id FROM transfer_batches WHERE id=$1", id).Scan(&mid); errors.Is(e, pgx.ErrNoRows) {
			e = payment.ErrNotFound
		}
		if e != nil {
			return identity.FundsAction{}, nil, e
		}
		addMerchant(mid)
		snapshot["batch"], e = fundsJSONRow(ctx, q, "SELECT to_jsonb(b) FROM transfer_batches b WHERE id=$1", id)
		if e != nil {
			return identity.FundsAction{}, nil, e
		}
		limit := 100
		if d.verb == "execute" {
			if json.Unmarshal(fields["limit"], &limit) != nil || limit < 1 || limit > 100 {
				return identity.FundsAction{}, nil, payment.ErrInvalid
			}
		}
		rows, err := q.Query(ctx, `SELECT i.transfer_id,a.channel_id,jsonb_build_object('position',i.position,'id',i.transfer_id,'provider',i.provider_kind,'app_id',i.app_id,'product',i.bank_product,'amount',t.amount,'cost_amount',t.cost_amount,'recipient_cipher',encode(a.recipient_cipher,'hex')) FROM transfer_batch_items i JOIN transfers t ON t.id=i.transfer_id JOIN provider_transfer_attempts a ON a.transfer_id=t.id WHERE i.batch_id=$1 AND i.state='unsent' ORDER BY i.position LIMIT $2`, id, limit)
		if err != nil {
			return identity.FundsAction{}, nil, err
		}
		selected := []json.RawMessage{}
		ids := []int64{}
		for rows.Next() {
			var transferID, ch int64
			var item []byte
			if e = rows.Scan(&transferID, &ch, &item); e != nil {
				break
			}
			selected = append(selected, json.RawMessage(item))
			ids = append(ids, transferID)
			channels[ch] = true
		}
		rows.Close()
		if e != nil {
			return identity.FundsAction{}, nil, e
		}
		if e = rows.Err(); e != nil {
			return identity.FundsAction{}, nil, e
		}
		snapshot["selected"] = selected
		snapshot["selected_ids"] = ids
		summary["items_count"] = fmt.Sprint(len(ids))
	case "memberships":
		if mid <= 0 || fundsText(fields, "external_no") == "" {
			return identity.FundsAction{}, nil, payment.ErrInvalid
		}
		id, ok := positiveFundsID(fundsText(fields, "group_id"))
		if !ok {
			return identity.FundsAction{}, nil, payment.ErrInvalid
		}
		snapshot["group"], e = fundsJSONRow(ctx, q, "SELECT body FROM backoffice_records WHERE resource='groups' AND id=$1", id)
		if e != nil {
			return identity.FundsAction{}, nil, e
		}
		quantity := 1
		if v, ok := fields["quantity"]; ok {
			if json.Unmarshal(v, &quantity) != nil {
				return identity.FundsAction{}, nil, payment.ErrInvalid
			}
		}
		sale, err := s.Store.FundsMembershipSaleSnapshot(ctx, q, mid, id, quantity)
		if err != nil {
			return identity.FundsAction{}, nil, err
		}
		snapshot["membership_sale"] = sale
		summary["quantity"] = fmt.Sprint(quantity)
		summary["membership_total_cents"] = fmt.Sprint(sale.UnitPrice * int64(quantity))
		rewardSource, err := s.Store.FundsMembershipReferralPurchaseSnapshot(ctx, q, mid, id, fundsText(fields, "external_no"), quantity)
		if err != nil {
			return identity.FundsAction{}, nil, err
		}
		snapshot["membership_referral"] = rewardSource
		addMerchant(rewardSource.PromoterID)
		summary["membership_reward_cents"] = fmt.Sprint(rewardSource.Reward)
		summary["membership_reward_reason"] = rewardSource.Reason
	case "balance-adjustments":
		direction := fundsText(fields, "direction")
		if mid <= 0 || fundsText(fields, "external_no") == "" || fundsText(fields, "amount") == "" || (direction != "credit" && direction != "debit") {
			return identity.FundsAction{}, nil, payment.ErrInvalid
		}
		summary["direction"] = direction
		summary["adjust_type"] = payment.AdminAdjustRecordLabel(direction == "credit")
	case "deposits":
		if d.id == "" {
			if mid <= 0 || fundsText(fields, "external_no") == "" || fundsText(fields, "amount") == "" || (fundsText(fields, "action") != "hold" && fundsText(fields, "action") != "release") {
				return identity.FundsAction{}, nil, payment.ErrInvalid
			}
		} else {
			id, _ := positiveFundsID(d.id)
			snapshot["deposit"], e = fundsJSONRow(ctx, q, "SELECT to_jsonb(a)-'status'-'reviewed_by'-'reviewed_at' FROM deposit_actions a WHERE id=$1", id)
			if e != nil {
				return identity.FundsAction{}, nil, e
			}
			if e = q.QueryRow(ctx, "SELECT merchant_id FROM deposit_actions WHERE id=$1", id).Scan(&mid); e != nil {
				return identity.FundsAction{}, nil, e
			}
			addMerchant(mid)
		}
	case "profitsharing-rules":
		id, _ := positiveFundsID(d.id)
		snapshot["rule"], e = fundsJSONRow(ctx, q, "SELECT body FROM backoffice_records WHERE resource='profitsharing-rules' AND id=$1", id)
		if e != nil {
			return identity.FundsAction{}, nil, e
		}
		trade := fundsText(fields, "trade_no")
		if trade == "" {
			return identity.FundsAction{}, nil, payment.ErrInvalid
		}
		if e = q.QueryRow(ctx, "SELECT merchant_id FROM orders WHERE trade_no=$1", trade).Scan(&mid); e != nil {
			return identity.FundsAction{}, nil, e
		}
		addMerchant(mid)
		snapshot["order"], e = fundsJSONRow(ctx, q, "SELECT to_jsonb(o)-'status'-'paid_at'-'created_at' FROM orders o WHERE trade_no=$1", trade)
		if e != nil {
			return identity.FundsAction{}, nil, e
		}
	case "settlements", "internal-transfers", "profitsharing":
		if d.id == "" {
			if mid <= 0 || fundsText(fields, "external_no") == "" || fundsText(fields, "amount") == "" {
				return identity.FundsAction{}, nil, payment.ErrInvalid
			}
		} else {
			id, _ := positiveFundsID(d.id)
			var recipient *int64
			if e = q.QueryRow(ctx, "SELECT merchant_id,recipient_id FROM ledger_requests WHERE id=$1 AND kind=$2", id, d.category).Scan(&mid, &recipient); errors.Is(e, pgx.ErrNoRows) {
				e = payment.ErrNotFound
			}
			if e != nil {
				return identity.FundsAction{}, nil, e
			}
			addMerchant(mid)
			if recipient != nil {
				addMerchant(*recipient)
			}
			snapshot["funds_request"], e = fundsJSONRow(ctx, q, "SELECT to_jsonb(r)-'status'-'reviewed_by'-'reviewed_at' FROM ledger_requests r WHERE id=$1", id)
			if e != nil {
				return identity.FundsAction{}, nil, e
			}
		}
		if d.category == "settlements" {
			if d.id == "" {
				snapshot["settlement_policy"], e = s.Store.FundsSettlementPolicySnapshot(ctx, q, mid)
				if e != nil {
					return identity.FundsAction{}, nil, e
				}
				snapshot["recipient_config"], e = fundsJSONRow(ctx, q, `SELECT COALESCE((SELECT to_jsonb(c) FROM settlement_recipient_current p JOIN settlement_recipient_configs c ON c.id=p.config_id AND c.merchant_id=p.merchant_id WHERE p.merchant_id=$1),'null'::jsonb)`, mid)
			} else {
				id, _ := positiveFundsID(d.id)
				snapshot["recipient_config"], e = fundsJSONRow(ctx, q, `SELECT COALESCE((SELECT to_jsonb(c) FROM settlement_recipients p JOIN settlement_recipient_configs c ON c.id=p.config_id WHERE p.request_id=$1),'null'::jsonb)`, id)
				if e == nil {
					snapshot["settlement_terms"], e = fundsJSONRow(ctx, q, `SELECT COALESCE((SELECT to_jsonb(t) FROM settlement_request_terms t WHERE t.request_id=$1),'null'::jsonb)`, id)
				}
			}
			if e != nil {
				return identity.FundsAction{}, nil, e
			}
		}
	default:
		return identity.FundsAction{}, nil, payment.ErrInvalid
	}
	if mid > 0 {
		summary["merchant_id"] = fmt.Sprint(mid)
	}
	// Policies contain no balances/day counters: those may legitimately change
	// while the already-confirmed operation is waiting for its account lock.
	policies, err := fundsRelevantPolicies(ctx, q, mids)
	if err != nil {
		return identity.FundsAction{}, nil, err
	}
	snapshot["policies"] = policies
	merchants := map[string]json.RawMessage{}
	runtime := map[string]any{}
	for merchant := range mids {
		m, e := fundsJSONRow(ctx, q, "SELECT jsonb_build_object('id',id,'enabled',enabled,'profile',profile,'config_revision',config_revision,'settlement_permission_revision',settlement_permission_revision) FROM merchants WHERE id=$1", merchant)
		if e != nil {
			return identity.FundsAction{}, nil, e
		}
		merchants[fmt.Sprint(merchant)] = m
		ids := append([]int64(nil), s.MerchantChannels[merchant]...)
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		runtime["merchant:"+fmt.Sprint(merchant)] = ids
	}
	snapshot["merchants"] = merchants
	if e = fundsActionSummary(summary, d, fields, snapshot, merchants, policies); e != nil {
		return identity.FundsAction{}, nil, e
	}
	for ch := range channels {
		if s.ProviderFlow == nil || s.ProviderFlow.Providers[ch] == nil {
			return identity.FundsAction{}, nil, payment.ErrLiveDisabled
		}
		p := s.ProviderFlow.Providers[ch]
		appID := ""
		if identity, ok := p.(adapter.ApplicationIdentity); ok {
			appID = identity.ApplicationID()
		}
		capability := map[string]any{"provider": p.ID(), "app_id": appID, "capabilities": p.Capabilities()}
		accountScope, err := adapter.TransferAccountScope(p)
		if err != nil {
			return identity.FundsAction{}, nil, err
		}
		capability["payer_account"] = accountScope
		if types, ok := p.(adapter.TransferCapabilities); ok {
			capability["transfer_types"] = types.TransferTypes()
		}
		if bank, ok := p.(adapter.BankTransferProduct); ok {
			capability["bank_product"] = bank.BankTransferProduct()
		}
		runtime["channel:"+fmt.Sprint(ch)] = capability
	}
	snapshot["runtime"] = runtime
	serialized, e := json.Marshal(snapshot)
	if e != nil || len(serialized) > 4<<20 {
		return identity.FundsAction{}, nil, payment.ErrInvalid
	}
	bodyHash, e := s.Store.FundsActionHMAC("body/v1", canonical)
	if e != nil {
		return identity.FundsAction{}, nil, e
	}
	snapshotHash, e := s.Store.FundsActionHMAC("snapshot/v1", serialized)
	if e != nil {
		return identity.FundsAction{}, nil, e
	}
	action, e := identity.NewFundsAction("admin."+strings.ReplaceAll(d.guard, "-", "_"), target, map[string]string{"body_hmac": bodyHash, "snapshot_hmac": snapshotHash})
	return action, summary, e
}

// protectAdminFunds installs only a request-local guard. It leaves the original
// body available to the existing handler, and never creates a business intent.
func mandatoryFundsAuthorization(d fundsTargetDefinition) bool {
	if d.category == "order-business-batch" {
		return true
	}
	return d.verb == "manual-refund" || d.category == "settlement-plan" || d.category == "settlement-auto" || d.guard == "income-hold-adopt" || d.guard == "authorization-release-remaining" || d.guard == "channel-resume"
}

func (s *Server) protectAdminFunds(w http.ResponseWriter, r *http.Request, role string, session identity.Session, token string) (*http.Request, bool) {
	if role != "admin" || r.Method != "POST" {
		return r, true
	}
	d, protected := fundsExecutionTarget(r.URL.Path)
	if !protected {
		return r, true
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if e != nil || len(raw) > 1<<20 {
		response(w, 400, -1, "invalid financial request body", nil)
		return r, false
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	actor := identity.FundsActor{User: session.User, SessionToken: token, Delegated: session.Issuer != nil}
	grant := r.Header.Get("X-Payment-Authorization")
	ambiguous := len(r.Header.Values("X-Payment-Authorization")) > 1
	guard := func(ctx context.Context, tx pgx.Tx, kind, key string) error {
		enforced, e := identity.FundsEnforcedTx(ctx, tx)
		if e != nil {
			return e
		}
		if !enforced && !mandatoryFundsAuthorization(d) {
			return nil
		}
		if actor.Delegated || ambiguous || kind != d.guard || s.FundsActionResolver == nil {
			return identity.ErrFundsAuthorization
		}
		if _, canonical := fundsTarget(r.URL.Path); !canonical {
			return identity.ErrFundsAuthorization
		}
		action, _, e := s.FundsActionResolver(ctx, tx, r.URL.Path, raw)
		if e != nil {
			return e
		}
		if mandatoryFundsAuthorization(d) {
			return identity.NewFundsAuthorization(s.Pool, s.MFA).ConsumeRequiredTx(ctx, tx, actor, grant, action, key)
		}
		return identity.NewFundsAuthorization(s.Pool, s.MFA).ConsumeTx(ctx, tx, actor, grant, action, key)
	}
	return r.WithContext(payment.WithFundsActionGuard(r.Context(), guard)), true
}
