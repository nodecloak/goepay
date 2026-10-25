package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"goepay/internal/identity"
	"goepay/internal/payment"
)

func referralPolicyFundsTarget(p []string) (fundsTargetDefinition, bool) {
	if len(p) != 3 || p[0] != "referral-policies" {
		return fundsTargetDefinition{}, false
	}
	if p[1] == "global" {
		if p[2] != "0" {
			return fundsTargetDefinition{}, false
		}
	} else if p[1] == "group" {
		if _, ok := positiveFundsID(p[2]); !ok {
			return fundsTargetDefinition{}, false
		}
	} else {
		return fundsTargetDefinition{}, false
	}
	return fundsTargetDefinition{guard: "referral-policy", category: "referral-policy", id: p[2], verb: p[1], keys: []string{"expected", "enabled", "rate_bps", "mode", "basis", "refund_policy"}}, true
}

func normalizeReferralPolicyFundsBody(raw []byte, d fundsTargetDefinition) (map[string]json.RawMessage, []byte, error) {
	var fields map[string]json.RawMessage
	if strictFundsJSON(raw, &fields) != nil || len(fields) != 6 {
		return nil, nil, payment.ErrInvalid
	}
	for _, key := range d.keys {
		if _, ok := fields[key]; !ok {
			return nil, nil, payment.ErrInvalid
		}
	}
	var expected string
	if json.Unmarshal(fields["expected"], &expected) != nil {
		return nil, nil, payment.ErrInvalid
	}
	n, err := strconv.ParseInt(expected, 10, 64)
	if err != nil || n < 0 || strconv.FormatInt(n, 10) != expected {
		return nil, nil, payment.ErrInvalid
	}
	var input payment.ReferralPolicyInput
	if json.Unmarshal(raw, &input) != nil || input.Mode != "immediate" && input.Mode != "daily" || input.Basis != "fee" && input.Basis != "gross" && input.Basis != "profit" || input.RefundPolicy != "any_successful_refund_voids_reward" || input.RateBPS != nil && (*input.RateBPS < 0 || *input.RateBPS > 10000) || d.verb == "global" && (input.Enabled == nil || input.RateBPS == nil) {
		return nil, nil, payment.ErrInvalid
	}
	canonical, err := json.Marshal(fields)
	return fields, canonical, err
}

func (s *Server) fundsReferralPolicy(ctx context.Context, q identity.FundsQuerier, d fundsTargetDefinition, fields map[string]json.RawMessage, summary map[string]string) (any, error) {
	id, err := strconv.ParseInt(d.id, 10, 64)
	if err != nil {
		return nil, payment.ErrInvalid
	}
	current, err := s.Store.FundsReferralPolicySnapshot(ctx, q, d.verb, id)
	if err != nil {
		return nil, err
	}
	expected, err := strconv.ParseInt(fundsText(fields, "expected"), 10, 64)
	if err != nil || current.Revision != expected {
		return nil, payment.ErrConflict
	}
	var group json.RawMessage
	if d.verb == "group" {
		group, err = fundsJSONRow(ctx, q, "SELECT body FROM backoffice_records WHERE resource='groups' AND id=$1", id)
		if err != nil {
			return nil, err
		}
	}
	summary["scope"], summary["scope_id"], summary["expected_revision"] = d.verb, d.id, fmt.Sprint(expected)
	for _, key := range []string{"enabled", "rate_bps", "mode", "basis", "refund_policy"} {
		summary[key] = string(fields[key])
	}
	summary["reward_budget"] = "仅原订单平台手续费；不使用其它订单资金"
	summary["reward_basis"] = "原订单平台手续费比例，按分四舍五入"
	if fundsText(fields, "basis") == "gross" {
		summary["reward_basis"] = "原订单金额比例，按分四舍五入，奖励不超过原订单平台手续费"
	}
	if fundsText(fields, "basis") == "profit" {
		summary["reward_basis"] = "原订单手续费减原通道配置估计成本的差额最终按分四舍五入，负利润按零计；非厂商实际账单"
		summary["cost_source"] = "仅新订单出生时服务器可信配置；缺成本不可创建，关闭奖励不要求成本；不使用商户请求成本"
	}
	summary["refund_recovery"] = "任意成功退款撤销整笔原奖励；未知结果预留，失败恢复"
	if fundsText(fields, "mode") == "daily" {
		summary["reward_timing"] = "每笔真实付款按原政策固定奖励，次个北京时间业务日起由专用调度器发放；不按迟到汇总或当前用户组重算"
		summary["refund_recovery"] = "发放前退款暂存原奖励；未知保持暂存，失败原路恢复，任意成功退款撤销整笔原奖励；发放后沿原奖励回收"
	}
	return struct {
		Current payment.ReferralPolicyView
		Group   json.RawMessage
	}{current, group}, nil
}
