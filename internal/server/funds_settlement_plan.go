package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"goepay/internal/identity"
	"goepay/internal/ledger"
	"goepay/internal/payment"
)

func settlementPlanAuthorizationInput(fields map[string]json.RawMessage) (payment.SettlementPlanAuthorizationInput, int64, error) {
	var in payment.SettlementPlanAuthorizationInput
	raw, err := json.Marshal(fields)
	if err != nil {
		return in, 0, err
	}
	if err = json.Unmarshal(raw, &in); err != nil {
		return in, 0, payment.ErrInvalid
	}
	expected, ok := positiveFundsID(fundsText(fields, "expected_revision"))
	if !ok {
		return in, 0, payment.ErrInvalid
	}
	return in, expected, nil
}

func (s *Server) fundsSettlementPlan(ctx context.Context, q identity.FundsQuerier, d fundsTargetDefinition, fields map[string]json.RawMessage, summary map[string]string) (json.RawMessage, error) {
	id, ok := positiveFundsID(d.id)
	if !ok {
		return nil, payment.ErrInvalid
	}
	in, expected, err := settlementPlanAuthorizationInput(fields)
	if err != nil {
		return nil, err
	}
	snapshot, err := s.Store.SettlementPlanAuthorizationSnapshot(ctx, q, id, expected, in)
	if err != nil {
		return nil, err
	}
	// Display only the authenticated snapshot that is also bound to the grant.
	// Never rebuild the recipient or policy from a separate, potentially newer read.
	var approved struct {
		Plan  payment.SettlementPlanView `json:"plan"`
		Scope struct {
			Threshold *struct {
				Enabled   bool                              `json:"enabled"`
				ChannelID string                            `json:"channel_id"`
				Policy    payment.SettlementThresholdPolicy `json:"policy"`
			} `json:"threshold"`
			Policy     payment.SettlementPolicy `json:"policy"`
			SettleType string                   `json:"settle_type"`
			Merchants  []struct {
				Threshold *struct {
					GroupID       string                            `json:"group_id"`
					GroupDisabled bool                              `json:"group_disabled"`
					Policy        payment.SettlementThresholdPolicy `json:"policy"`
				} `json:"threshold"`
				SettlementPolicy  *payment.SettlementPolicy       `json:"settlement_policy"`
				SettleType        *int                            `json:"settle_type"`
				MerchantID        string                          `json:"merchant_id"`
				RecipientConfigID string                          `json:"recipient_config_id"`
				Recipient         payment.SettlementRecipientView `json:"recipient"`
			} `json:"merchants"`
		} `json:"scope"`
	}
	if err = json.Unmarshal(snapshot, &approved); err != nil || len(approved.Scope.Merchants) == 0 || len(approved.Scope.Merchants) > 100 {
		return nil, payment.ErrConflict
	}
	merchants := make([]string, 0, len(approved.Scope.Merchants))
	recipients := make([]string, 0, len(approved.Scope.Merchants))
	maturityRules := make([]string, 0, len(approved.Scope.Merchants))
	commonMaturity, mixedMaturity := "", false
	for _, merchant := range approved.Scope.Merchants {
		merchants = append(merchants, merchant.MerchantID)
		recipients = append(recipients, fmt.Sprintf("%s: %s %s (config %s)", merchant.MerchantID, merchant.Recipient.Type, merchant.Recipient.AccountMasked, merchant.RecipientConfigID))
		mode := approved.Scope.SettleType
		if merchant.SettleType != nil {
			mode = strconv.Itoa(*merchant.SettleType)
		}
		if mode != "0" && mode != "1" {
			return nil, payment.ErrConflict
		}
		maturityRules = append(maturityRules, fmt.Sprintf("商户%s：D+%s", merchant.MerchantID, mode))
		if commonMaturity == "" {
			commonMaturity = mode
		} else if commonMaturity != mode {
			mixedMaturity = true
		}
	}
	summary["plan_name"] = approved.Plan.Name
	summary["merchant_ids"], summary["recipients"] = strings.Join(merchants, ","), strings.Join(recipients, "; ")
	summary["group_maturity_rules"] = strings.Join(maturityRules, "; ")
	summary["maturity"] = "D+0"
	if mixedMaturity {
		summary["maturity"] = "逐商户，见结算周期规则"
	} else if commonMaturity == "1" {
		summary["maturity"] = "D+1（UTC+8 00:00）"
	}
	policy := approved.Scope.Policy
	summary["retained_money"] = ledger.Format(policy.RetainedCents)
	summary["minimum_money"], summary["maximum_money"] = ledger.Format(policy.MinimumAmount), ledger.Format(policy.MaximumAmount)
	summary["fee_rate_bps"] = strconv.FormatInt(policy.FeeRateBPS, 10)
	summary["fee_minimum_money"], summary["fee_maximum_money"] = ledger.Format(policy.FeeMinimumCents), ledger.Format(policy.FeeMaximumCents)
	summary["merchant_daily_limit"] = strconv.FormatInt(policy.DailyLimit, 10)
	summary["plan_id"], summary["expected_revision"] = d.id, fmt.Sprint(expected)
	summary["mode"] = "仅自动生成待审核申请及资金预留；不审批、不付款"
	if approved.Plan.Mode == "generate_only" {
		rules := make([]string, 0, len(approved.Scope.Merchants))
		minimumRules := make([]string, 0, len(approved.Scope.Merchants))
		var common *int64
		var commonMinimum *int64
		mixed, mixedMinimum := false, false
		for _, merchant := range approved.Scope.Merchants {
			current := approved.Scope.Policy
			if merchant.SettlementPolicy != nil {
				current = *merchant.SettlementPolicy
			}
			rules = append(rules, fmt.Sprintf("商户%s：%s%%", merchant.MerchantID, ledger.Format(current.FeeRateBPS)))
			minimumRules = append(minimumRules, fmt.Sprintf("商户%s：%s元", merchant.MerchantID, ledger.Format(current.MinimumAmount)))
			if common == nil {
				rate := current.FeeRateBPS
				common = &rate
			} else if *common != current.FeeRateBPS {
				mixed = true
			}
			if commonMinimum == nil {
				minimum := current.MinimumAmount
				commonMinimum = &minimum
			} else if *commonMinimum != current.MinimumAmount {
				mixedMinimum = true
			}
		}
		summary["group_fee_rules"] = strings.Join(rules, "; ")
		summary["group_minimum_rules"] = strings.Join(minimumRules, "; ")
		summary["fee_scope"] = "逐商户使用已授权有效组结算比例及最低毛额；保留原全局费用上下限、留存及每日限制"
		if mixed {
			summary["fee_rate_bps"] = "逐商户，见费率规则"
		} else if common != nil {
			summary["fee_rate_bps"] = strconv.FormatInt(*common, 10)
		}
		if mixedMinimum {
			summary["minimum_money"] = "逐商户，见最低金额规则"
		} else if commonMinimum != nil {
			summary["minimum_money"] = ledger.Format(*commonMinimum)
		}
	}
	if approved.Plan.Mode == "balance_threshold" {
		threshold := approved.Scope.Threshold
		if threshold == nil || !threshold.Enabled {
			return nil, payment.ErrConflict
		}
		summary["business_mode"] = "balance_threshold"
		summary["minimum_money"] = "不适用（按余额阈值）"
		summary["balance_threshold_money"] = ledger.Format(threshold.Policy.ThresholdCents)
		summary["transfer_channel_id"] = threshold.ChannelID
		summary["selection_rule"] = "UID倒序最多5个；关闭组不补位"
		summary["payout_amount"] = "原整余额，超过授权剩余额度跳过，不截小"
		summary["fee_scope"] = "组结算比例覆盖全局，仅全局fee_max；不使用fee_min、留存或普通每日次数"
		groups := []string{}
		for _, m := range approved.Scope.Merchants {
			if m.Threshold == nil {
				return nil, payment.ErrConflict
			}
			groups = append(groups, fmt.Sprintf("%s: group %s, fee_bps %d, closed %t", m.MerchantID, m.Threshold.GroupID, m.Threshold.Policy.FeeRateBPS, m.Threshold.GroupDisabled))
		}
		summary["group_fee_rules"] = strings.Join(groups, "; ")
	}
	summary["valid_from"], summary["expires_at"] = in.ValidFrom.UTC().Format(time.RFC3339Nano), in.ExpiresAt.UTC().Format(time.RFC3339Nano)
	summary["max_item_gross"], summary["max_run_gross"], summary["max_day_gross"] = ledger.Format(in.MaxItemGross), ledger.Format(in.MaxRunGross), ledger.Format(in.MaxDayGross)
	summary["max_day_items"], summary["previous_authorization_id"] = fmt.Sprint(in.MaxDayItems), fmt.Sprint(in.ExpectedAuthorizationID)
	summary["revocation"] = "撤销或到期阻止新增申请；已冻结款项按原申请审核处理"
	return snapshot, nil
}

var settlementPlanAuthorizationKeys = []string{"expected_revision", "expected_authorization_id", "valid_from", "expires_at", "max_item_gross", "max_run_gross", "max_day_gross", "max_day_items"}

func settlementPlanFundsTarget(p []string) (fundsTargetDefinition, bool) {
	if len(p) != 3 || p[0] != "settlement-plans" || p[2] != "authorizations" {
		return fundsTargetDefinition{}, false
	}
	if _, ok := positiveFundsID(p[1]); !ok {
		return fundsTargetDefinition{}, false
	}
	return fundsTargetDefinition{guard: "settlement-plan-authorize", category: "settlement-plan", id: p[1], verb: p[2], keys: settlementPlanAuthorizationKeys}, true
}

func normalizeSettlementPlanFundsBody(raw []byte, _ fundsTargetDefinition) (map[string]json.RawMessage, []byte, error) {
	var fields map[string]json.RawMessage
	if strictFundsJSON(raw, &fields) != nil || len(fields) != len(settlementPlanAuthorizationKeys) {
		return nil, nil, payment.ErrInvalid
	}
	values := map[string]int64{}
	for _, key := range settlementPlanAuthorizationKeys {
		v, ok := fields[key]
		if !ok {
			return nil, nil, payment.ErrInvalid
		}
		var value string
		if json.Unmarshal(v, &value) != nil {
			return nil, nil, payment.ErrInvalid
		}
		if key == "valid_from" || key == "expires_at" {
			continue
		}
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || strconv.FormatInt(n, 10) != value || n < 0 || n == 0 && key != "expected_authorization_id" {
			return nil, nil, payment.ErrInvalid
		}
		values[key] = n
	}
	from, err := time.Parse(time.RFC3339Nano, fundsText(fields, "valid_from"))
	if err != nil {
		return nil, nil, payment.ErrInvalid
	}
	until, err := time.Parse(time.RFC3339Nano, fundsText(fields, "expires_at"))
	if err != nil || !until.After(from) || until.Sub(from) > 31*24*time.Hour {
		return nil, nil, payment.ErrInvalid
	}
	if values["max_item_gross"] > values["max_run_gross"] || values["max_run_gross"] > values["max_day_gross"] {
		return nil, nil, payment.ErrInvalid
	}
	fields["valid_from"], _ = json.Marshal(from.UTC().Format(time.RFC3339Nano))
	fields["expires_at"], _ = json.Marshal(until.UTC().Format(time.RFC3339Nano))
	canonical, err := json.Marshal(fields)
	return fields, canonical, err
}
