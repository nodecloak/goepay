package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"goepay/internal/identity"
	"goepay/internal/payment"
)

func membershipReferralPolicyFundsTarget(p []string) (fundsTargetDefinition, bool) {
	if len(p) != 3 || p[0] != "membership-referral-policies" {
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
	return fundsTargetDefinition{guard: "membership-referral-policy", category: "membership-referral-policy", id: p[2], verb: p[1], keys: []string{"expected", "enabled", "rate_bps"}}, true
}

func normalizeMembershipReferralPolicyFundsBody(raw []byte, d fundsTargetDefinition) (map[string]json.RawMessage, []byte, error) {
	var fields map[string]json.RawMessage
	if strictFundsJSON(raw, &fields) != nil || len(fields) != 3 {
		return nil, nil, payment.ErrInvalid
	}
	for _, key := range []string{"expected", "enabled", "rate_bps"} {
		if _, ok := fields[key]; !ok {
			return nil, nil, payment.ErrInvalid
		}
	}
	var expected string
	if json.Unmarshal(fields["expected"], &expected) != nil {
		return nil, nil, payment.ErrInvalid
	}
	n, e := strconv.ParseInt(expected, 10, 64)
	if e != nil || n < 0 || strconv.FormatInt(n, 10) != expected {
		return nil, nil, payment.ErrInvalid
	}
	var input payment.MembershipReferralPolicyInput
	if json.Unmarshal(raw, &input) != nil || input.RateBPS != nil && (*input.RateBPS < 0 || *input.RateBPS > 10000) || d.verb == "global" && (input.Enabled == nil || input.RateBPS == nil) {
		return nil, nil, payment.ErrInvalid
	}
	canonical, e := json.Marshal(fields)
	return fields, canonical, e
}

func (s *Server) fundsMembershipReferralPolicy(ctx context.Context, q identity.FundsQuerier, d fundsTargetDefinition, fields map[string]json.RawMessage, summary map[string]string) (any, error) {
	id, e := strconv.ParseInt(d.id, 10, 64)
	if e != nil {
		return nil, payment.ErrInvalid
	}
	current, e := s.Store.FundsMembershipReferralPolicySnapshot(ctx, q, d.verb, id)
	if e != nil {
		return nil, e
	}
	expected, e := strconv.ParseInt(fundsText(fields, "expected"), 10, 64)
	if e != nil || current.Revision != expected {
		return nil, payment.ErrConflict
	}
	var group json.RawMessage
	if d.verb == "group" {
		group, e = fundsJSONRow(ctx, q, "SELECT body FROM backoffice_records WHERE resource='groups' AND id=$1", id)
		if e != nil {
			return nil, e
		}
	}
	summary["scope"], summary["scope_id"], summary["expected_revision"] = d.verb, d.id, fmt.Sprint(expected)
	summary["enabled"], summary["rate_bps"] = string(fields["enabled"]), string(fields["rate_bps"])
	summary["reward_budget"] = "仅该次真实余额会员购买收入；不使用普通订单手续费或其它购买资金"
	summary["reward_basis"] = "原购买价格乘固定比例，按分四舍五入，比例0..10000BPS"
	summary["reward_timing"] = "余额扣款、会员变更与一次奖励同一事务提交；旧购买不追发"
	return struct {
		Current payment.MembershipReferralPolicyView
		Group   json.RawMessage
	}{current, group}, nil
}

func (s *Server) membershipReferralConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	// A delegated merchant session must not bypass the administrator's own
	// funds confirmation by buying membership through the merchant workflow.
	if role == "merchant" && r.Method == "POST" && len(parts) == 3 && parts[1] == "workflows" && parts[2] == "memberships" && session.Issuer != nil {
		response(w, 403, -1, "own merchant session required for membership purchase", nil)
		return true
	}
	if len(parts) < 2 || parts[1] != "membership-referral-policies" {
		return false
	}
	if session.User.ID <= 0 || session.User.Username == "" || session.User.Role != role || session.Issuer != nil || role != "admin" || session.User.AdminClass != "super" {
		response(w, 403, -1, "own super administrator session required", nil)
		return true
	}
	if r.Method != "GET" && (session.CSRF == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(session.CSRF)) != 1) {
		response(w, 403, -1, "invalid CSRF token", nil)
		return true
	}
	if s.Store == nil || s.Pool == nil {
		response(w, 503, -1, "membership referral service unavailable", nil)
		return true
	}
	if len(parts) != 4 {
		response(w, 404, -1, "membership referral policy unavailable", nil)
		return true
	}
	d, ok := membershipReferralPolicyFundsTarget(parts[1:])
	if !ok || r.URL.RawQuery != "" {
		response(w, 400, -1, "canonical policy scope required", nil)
		return true
	}
	id, _ := strconv.ParseInt(d.id, 10, 64)
	actor := payment.TransferBatchActor{Role: role, Class: session.User.AdminClass, ID: session.User.ID, Name: session.User.Username}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	var out payment.MembershipReferralPolicyView
	var e error
	switch r.Method {
	case "GET":
		out, e = s.Store.MembershipReferralPolicy(ctx, d.verb, id, actor)
	case "POST":
		var fields map[string]json.RawMessage
		if !decodeFundsJSON(w, r, &fields) {
			return true
		}
		raw, _ := json.Marshal(fields)
		fields, _, e = normalizeMembershipReferralPolicyFundsBody(raw, d)
		if e != nil {
			response(w, 422, -1, "explicit supported membership policy fields required", nil)
			return true
		}
		var expected string
		json.Unmarshal(fields["expected"], &expected)
		revision, _ := strconv.ParseInt(expected, 10, 64)
		var input payment.MembershipReferralPolicyInput
		json.Unmarshal(raw, &input)
		out, e = s.Store.SetMembershipReferralPolicy(ctx, d.verb, id, revision, input, actor)
	default:
		response(w, 405, -1, "GET or POST required", nil)
		return true
	}
	if e != nil {
		s.fail(w, e)
	} else {
		response(w, 200, 0, "policy affects new balance membership purchases only", struct {
			payment.MembershipReferralPolicyView
			Available bool `json:"available"`
		}{out, true})
	}
	return true
}
