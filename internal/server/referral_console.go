package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type referralAliasKey struct{}

// Exact old entry points reuse the new safe DTO and console identity. PHP
// sessions, encrypted controller envelopes and legacy offset pages are not
// guessed. Cookie scope is deliberately not widened for these aliases.
func (s *Server) referralLegacyAlias(w http.ResponseWriter, r *http.Request) bool {
	path, method := "", ""
	switch r.URL.Path {
	case "/user/invite/inviteInfo":
		path, method = "overview", "GET"
	case "/user/invite/inviteList":
		path, method = "invitees", "POST"
	default:
		return false
	}
	if r.Method != method {
		response(w, 405, -1, "legacy referral alias method unavailable", nil)
		return true
	}
	clone := r.Clone(context.WithValue(r.Context(), referralAliasKey{}, true))
	clone.URL.Path = "/console/v1/merchant/referral/" + path
	w.Header().Set("X-Goepay-Contract", "safe-referral-v1; not-php-json-parity")
	s.console(w, clone)
	return true
}

func (s *Server) referralConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) < 2 || (parts[1] != "referral" && parts[1] != "referral-policies") {
		return false
	}
	if session.User.ID <= 0 || session.User.Username == "" || session.User.Role != role || session.Issuer != nil {
		response(w, 403, -1, "own authenticated session required", nil)
		return true
	}
	policy := parts[1] == "referral-policies"
	if policy && (role != "admin" || session.User.AdminClass != "super") || !policy && role != "merchant" {
		response(w, 403, -1, "referral owner or super administrator required", nil)
		return true
	}
	if r.Method != "GET" && (session.CSRF == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(session.CSRF)) != 1) {
		response(w, 403, -1, "invalid CSRF token", nil)
		return true
	}
	if s.Store == nil || s.Pool == nil {
		response(w, 503, -1, "referral service unavailable", nil)
		return true
	}
	actor := payment.TransferBatchActor{Role: role, Class: session.User.AdminClass, ID: session.User.ID, Name: session.User.Username}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	if policy {
		if len(parts) != 4 {
			response(w, 404, -1, "referral policy unavailable", nil)
			return true
		}
		d, ok := referralPolicyFundsTarget(parts[1:])
		if !ok || r.URL.RawQuery != "" {
			response(w, 400, -1, "canonical policy scope required", nil)
			return true
		}
		id, _ := strconv.ParseInt(d.id, 10, 64)
		var out payment.ReferralPolicyView
		var e error
		switch r.Method {
		case "GET":
			out, e = s.Store.ReferralPolicy(ctx, d.verb, id, actor)
		case "POST":
			var fields map[string]json.RawMessage
			if !decodeFundsJSON(w, r, &fields) {
				return true
			}
			raw, _ := json.Marshal(fields)
			normalized, _, err := normalizeReferralPolicyFundsBody(raw, d)
			if err != nil {
				response(w, 422, -1, "explicit supported reward policy fields required", nil)
				return true
			}
			var expected string
			json.Unmarshal(normalized["expected"], &expected)
			version, _ := strconv.ParseInt(expected, 10, 64)
			var input payment.ReferralPolicyInput
			json.Unmarshal(raw, &input)
			out, e = s.Store.SetReferralPolicy(ctx, d.verb, id, version, input, actor)
		default:
			response(w, 405, -1, "GET or POST required", nil)
			return true
		}
		if e != nil {
			s.fail(w, e)
		} else {
			response(w, 200, 0, "policy affects new original payment snapshots only", struct {
				payment.ReferralPolicyView
				DailyModeAvailable                bool `json:"daily_mode_available"`
				ProfitBasisAvailable              bool `json:"profit_basis_available"`
				MembershipReferralPolicyAvailable bool `json:"membership_referral_policy_available"`
			}{out, true, true, true})
		}
		return true
	}
	if len(parts) < 3 || len(parts) > 4 {
		response(w, 404, -1, "referral operation unavailable", nil)
		return true
	}
	if len(parts) == 4 && parts[2] == "token" && (parts[3] == "rotate" || parts[3] == "revoke") {
		if r.Method != "POST" {
			response(w, 405, -1, "POST required", nil)
			return true
		}
		if r.URL.RawQuery != "" {
			response(w, 400, -1, "query parameters not accepted", nil)
			return true
		}
		var in struct {
			Expected string `json:"expected"`
		}
		if !decodeFundsJSON(w, r, &in) {
			return true
		}
		version, e := strconv.ParseInt(in.Expected, 10, 64)
		if e != nil || version < 0 || fmt.Sprint(version) != in.Expected {
			response(w, 422, -1, "canonical expected version required", nil)
			return true
		}
		if !s.allowLogin(r.RemoteAddr, "referral-token", fmt.Sprint(session.User.ID), time.Now()) {
			w.Header().Set("Retry-After", "900")
			response(w, 429, -1, "token change rate limited", nil)
			return true
		}
		if parts[3] == "rotate" {
			out, e := s.Store.RotateReferralToken(ctx, session.User.ID, version, actor)
			if e != nil {
				s.fail(w, e)
			} else {
				response(w, 200, 0, "new bearer token shown once; previous token invalidated; no automatic retry", out)
			}
		} else {
			out, e := s.Store.RevokeReferralToken(ctx, session.User.ID, version, actor)
			if e != nil {
				s.fail(w, e)
			} else {
				response(w, 200, 0, "token revoked; existing registered relations preserved", out)
			}
		}
		return true
	}
	if len(parts) != 3 {
		response(w, 404, -1, "referral operation unavailable", nil)
		return true
	}
	alias, _ := r.Context().Value(referralAliasKey{}).(bool)
	if r.Method != "GET" && !(alias && r.Method == "POST" && parts[2] == "invitees") {
		response(w, 405, -1, "GET required", nil)
		return true
	}
	switch parts[2] {
	case "overview":
		if r.URL.RawQuery != "" {
			response(w, 400, -1, "query parameters not accepted", nil)
			return true
		}
		out, e := s.Store.ReferralOverview(ctx, session.User.ID, actor)
		if e != nil {
			s.fail(w, e)
		} else {
			response(w, 200, 0, "historical issued, voided and refund-pending flows; not a spendable balance", struct {
				payment.ReferralOverview
				DailyQueueAvailable bool `json:"daily_queue_available"`
			}{out, true})
		}
	case "daily-queue":
		after, limit, e := referralPaging(w, r, false)
		if e != nil {
			return true
		}
		out, e := s.Store.ListReferralDailyQueue(ctx, session.User.ID, after, limit, actor)
		if e != nil {
			s.fail(w, e)
		} else {
			response(w, 200, 0, "own verified original daily entitlements; pending amounts are not spendable", out)
		}
	case "token":
		if r.URL.RawQuery != "" {
			response(w, 400, -1, "query parameters not accepted", nil)
			return true
		}
		out, e := s.Store.ReferralToken(ctx, session.User.ID, actor)
		if errors.Is(e, payment.ErrNotFound) {
			out = payment.ReferralTokenView{MerchantID: session.User.ID}
			e = nil
		}
		if e != nil {
			s.fail(w, e)
		} else {
			response(w, 200, 0, "masked current version only; no existing bearer reveal", out)
		}
	case "invitees", "rewards":
		after, limit, e := referralPaging(w, r, alias)
		if e != nil {
			return true
		}
		if parts[2] == "invitees" {
			out, e := s.Store.ReferralInvitees(ctx, session.User.ID, after, limit, actor)
			if e != nil {
				s.fail(w, e)
			} else {
				next := ""
				if len(out) == limit {
					next = fmt.Sprint(out[len(out)-1].ID)
				}
				response(w, 200, 0, "cursor uses relation ID; final full page may be followed by an empty page", map[string]any{"items": out, "next_cursor": next})
			}
		} else {
			out, e := s.Store.ListReferralRewards(ctx, session.User.ID, after, limit, actor)
			if e != nil {
				s.fail(w, e)
			} else {
				response(w, 200, 0, "original immutable reward and derived refund state", out)
			}
		}
	default:
		response(w, 404, -1, "referral operation unavailable", nil)
	}
	return true
}

func referralPaging(w http.ResponseWriter, r *http.Request, alias bool) (int64, int, error) {
	var afterRaw, limitRaw string
	if alias && r.Method == "POST" {
		if r.URL.RawQuery != "" {
			response(w, 400, -1, "query parameters not accepted", nil)
			return 0, 0, payment.ErrInvalid
		}
		var in struct {
			After string `json:"after_id"`
			Limit int    `json:"limit"`
		}
		if !decodeFundsJSON(w, r, &in) {
			return 0, 0, payment.ErrInvalid
		}
		afterRaw, limitRaw = in.After, fmt.Sprint(in.Limit)
	} else {
		q, parseError := url.ParseQuery(r.URL.RawQuery)
		if parseError != nil {
			response(w, 400, -1, "invalid referral list query encoding", nil)
			return 0, 0, payment.ErrInvalid
		}
		for key, values := range q {
			if (key != "after_id" && key != "limit") || len(values) != 1 {
				response(w, 400, -1, "invalid referral list query", nil)
				return 0, 0, payment.ErrInvalid
			}
		}
		afterRaw, limitRaw = q.Get("after_id"), q.Get("limit")
		if afterRaw == "" {
			afterRaw = "0"
		}
		if limitRaw == "" {
			limitRaw = "20"
		}
	}
	after, e := strconv.ParseInt(afterRaw, 10, 64)
	limit, le := strconv.Atoi(limitRaw)
	if e != nil || le != nil || after < 0 || fmt.Sprint(after) != afterRaw || limit < 1 || limit > 100 || fmt.Sprint(limit) != limitRaw {
		response(w, 400, -1, "canonical cursor and limit 1..100 required", nil)
		return 0, 0, payment.ErrInvalid
	}
	return after, limit, nil
}
