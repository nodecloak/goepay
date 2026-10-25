package server

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"goepay/internal/identity"
	"goepay/internal/payment"
)

func (s *Server) settlementPlanConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, p []string) bool {
	prefs := len(p) >= 3 && p[1] == "account" && p[2] == "settlement-schedule"
	if len(p) < 2 || !prefs && p[1] != "settlement-plans" && p[1] != "settlement-plan-runs" && p[1] != "settlement-plan-authorizations" {
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	if session.User.ID <= 0 || session.User.Username == "" || session.User.Role != role || session.Issuer != nil || prefs && role != "merchant" || !prefs && (role != "admin" || session.User.AdminClass != "super") {
		response(w, 403, -1, "own merchant or original super administrator session required", nil)
		return true
	}
	if r.Method != "GET" && (session.CSRF == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(session.CSRF)) != 1) {
		response(w, 403, -1, "invalid CSRF token", nil)
		return true
	}
	if s.Store == nil || s.Pool == nil {
		response(w, 503, -1, "settlement scheduling unavailable", nil)
		return true
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	actor := payment.SettlementPlanActor{Role: role, Class: session.User.AdminClass, ID: session.User.ID, Name: session.User.Username}
	if prefs {
		s.settlementSchedulePreferenceHTTP(ctx, w, r, p, actor)
		return true
	}
	var out any
	var err error
	status := http.StatusOK
	if len(p) == 2 && p[1] == "settlement-plans" {
		switch r.Method {
		case "GET":
			after, limit, ok := settlementPlanPaging(w, r)
			if !ok {
				return true
			}
			items, e := s.Store.SettlementPlans(ctx, after, limit, actor)
			err = e
			if items == nil {
				items = []payment.SettlementPlanView{}
			}
			next := ""
			if len(items) == limit {
				next = fmt.Sprint(items[len(items)-1].ID)
			}
			out = map[string]any{"items": items, "next_cursor": next}
		case "POST":
			if !settlementPlanNoQuery(w, r) {
				return true
			}
			in, _, ok := decodeSettlementPlanDefinition(w, r, false)
			if !ok {
				return true
			}
			out, err = s.Store.CreateSettlementPlan(ctx, in, actor)
			status = http.StatusCreated
		default:
			response(w, 405, -1, "GET or POST required", nil)
			return true
		}
	} else {
		if len(p) < 3 {
			response(w, 404, -1, "settlement plan operation unavailable", nil)
			return true
		}
		id, ok := positiveFundsID(p[2])
		if !ok {
			response(w, 400, -1, "canonical positive ID required", nil)
			return true
		}
		switch {
		case p[1] == "settlement-plans" && len(p) == 3:
			if !settlementPlanNoQuery(w, r) {
				return true
			}
			switch r.Method {
			case "GET":
				out, err = s.Store.SettlementPlan(ctx, id, actor)
			case "PATCH":
				in, expected, ok := decodeSettlementPlanDefinition(w, r, true)
				if !ok {
					return true
				}
				out, err = s.Store.UpdateSettlementPlan(ctx, id, expected, in, actor)
			default:
				response(w, 405, -1, "GET or PATCH required", nil)
				return true
			}
		case p[1] == "settlement-plans" && len(p) == 4 && p[3] == "authorizations":
			if r.Method != "POST" {
				response(w, 405, -1, "POST required", nil)
				return true
			}
			if !settlementPlanNoQuery(w, r) {
				return true
			}
			var raw map[string]json.RawMessage
			if !decodeFundsJSON(w, r, &raw) {
				return true
			}
			body, _ := json.Marshal(raw)
			fields, _, e := normalizeSettlementPlanFundsBody(body, fundsTargetDefinition{})
			if e != nil {
				response(w, 422, -1, "explicit bounded plan authorization required", nil)
				return true
			}
			in, expected, e := settlementPlanAuthorizationInput(fields)
			if e != nil {
				s.fail(w, e)
				return true
			}
			out, err = s.Store.AuthorizeSettlementPlan(ctx, id, expected, in, actor)
			status = http.StatusCreated
		case p[1] == "settlement-plan-authorizations" && len(p) == 4 && p[3] == "revoke":
			if r.Method != "POST" {
				response(w, 405, -1, "POST required", nil)
				return true
			}
			if !settlementPlanNoQuery(w, r) || !decodeSettlementPlanEmptyBody(w, r) {
				return true
			}
			err = s.Store.RevokeSettlementPlanAuthorization(ctx, id, actor)
			out = map[string]string{"authorization_id": fmt.Sprint(id), "state": "revoked"}
		case p[1] == "settlement-plans" && len(p) == 4 && p[3] == "runs":
			switch r.Method {
			case "GET":
				after, limit, ok := settlementPlanPaging(w, r)
				if !ok {
					return true
				}
				items, e := s.Store.SettlementPlanRuns(ctx, id, after, limit, actor)
				err = e
				if items == nil {
					items = []payment.SettlementPlanRunView{}
				}
				next := ""
				if len(items) == limit {
					next = fmt.Sprint(items[len(items)-1].ID)
				}
				out = map[string]any{"items": items, "next_cursor": next}
			case "POST":
				if !settlementPlanNoQuery(w, r) {
					return true
				}
				var in struct {
					Expected string `json:"expected_revision"`
				}
				if !decodeFundsJSON(w, r, &in) {
					return true
				}
				expected, ok := positiveFundsID(in.Expected)
				if !ok {
					response(w, 422, -1, "canonical expected revision required", nil)
					return true
				}
				out, err = s.Store.CreateSettlementPlanRun(ctx, id, expected, actor)
				status = http.StatusAccepted
			default:
				response(w, 405, -1, "GET or POST required", nil)
				return true
			}
		case p[1] == "settlement-plan-runs" && len(p) == 3:
			if r.Method != "GET" {
				response(w, 405, -1, "GET required", nil)
				return true
			}
			if !settlementPlanNoQuery(w, r) {
				return true
			}
			out, err = s.Store.SettlementPlanRun(ctx, id, actor)
		default:
			response(w, 404, -1, "settlement plan operation unavailable", nil)
			return true
		}
	}
	if err != nil {
		s.fail(w, err)
	} else {
		response(w, status, 0, "generation and reservation only; approval and payout require their original authorization", out)
	}
	return true
}

func settlementPlanNoQuery(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.RawQuery != "" {
		response(w, 400, -1, "query parameters not accepted", nil)
		return false
	}
	return true
}

func decodeSettlementPlanEmptyBody(w http.ResponseWriter, r *http.Request) bool {
	var fields map[string]json.RawMessage
	if !decodeFundsJSON(w, r, &fields) {
		return false
	}
	if len(fields) != 0 {
		response(w, 422, -1, "empty object required", nil)
		return false
	}
	return true
}

func settlementPlanPaging(w http.ResponseWriter, r *http.Request) (int64, int, bool) {
	q, parseErr := url.ParseQuery(r.URL.RawQuery)
	if parseErr != nil {
		response(w, 400, -1, "invalid plan list query", nil)
		return 0, 0, false
	}
	for key, values := range q {
		if (key != "after_id" && key != "limit") || len(values) != 1 {
			response(w, 400, -1, "invalid plan list query", nil)
			return 0, 0, false
		}
	}
	a, l := q.Get("after_id"), q.Get("limit")
	if a == "" {
		a = "0"
	}
	if l == "" {
		l = "20"
	}
	after, err := strconv.ParseInt(a, 10, 64)
	limit, le := strconv.Atoi(l)
	if err != nil || le != nil || after < 0 || fmt.Sprint(after) != a || limit < 1 || limit > 100 || fmt.Sprint(limit) != l {
		response(w, 400, -1, "canonical cursor and limit 1..100 required", nil)
		return 0, 0, false
	}
	return after, limit, true
}

func decodeSettlementPlanDefinition(w http.ResponseWriter, r *http.Request, update bool) (payment.SettlementPlanInput, int64, bool) {
	var out payment.SettlementPlanInput
	var raw map[string]json.RawMessage
	if !decodeFundsJSON(w, r, &raw) {
		return out, 0, false
	}
	keys := []string{"name", "mode", "schedule", "merchant_ids"}
	if update {
		keys = append(keys, "expected_revision")
	}
	if len(raw) != len(keys) {
		response(w, 422, -1, "explicit plan definition required", nil)
		return out, 0, false
	}
	for _, key := range keys {
		if _, ok := raw[key]; !ok {
			response(w, 422, -1, "missing plan definition field", nil)
			return out, 0, false
		}
	}
	var in struct {
		Name        string                         `json:"name"`
		Mode        string                         `json:"mode"`
		Schedule    payment.SettlementScheduleSpec `json:"schedule"`
		MerchantIDs []string                       `json:"merchant_ids"`
		Expected    string                         `json:"expected_revision"`
	}
	body, _ := json.Marshal(raw)
	var schedule map[string]json.RawMessage
	if strictFundsJSON(body, &in) != nil || json.Unmarshal(raw["schedule"], &schedule) != nil || len(schedule) != 6 || len(in.MerchantIDs) < 1 || len(in.MerchantIDs) > 100 || (in.Mode != "generate_only" && in.Mode != "balance_threshold") {
		response(w, 422, -1, "explicit bounded settlement business mode required", nil)
		return out, 0, false
	}
	for _, key := range []string{"frequency", "interval", "anchor_at", "execute_hour", "execute_minute", "execute_second"} {
		if value, ok := schedule[key]; !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			response(w, 422, -1, "explicit schedule fields required", nil)
			return out, 0, false
		}
	}
	var interval string
	if json.Unmarshal(schedule["interval"], &interval) != nil || fmt.Sprint(in.Schedule.Interval) != interval || in.Schedule.Interval < 0 {
		response(w, 422, -1, "canonical schedule interval required", nil)
		return out, 0, false
	}
	out = payment.SettlementPlanInput{Name: in.Name, Mode: in.Mode, Schedule: in.Schedule}
	seen := map[int64]bool{}
	for _, text := range in.MerchantIDs {
		id, ok := positiveFundsID(text)
		if !ok || seen[id] {
			response(w, 422, -1, "unique canonical merchant IDs required", nil)
			return out, 0, false
		}
		seen[id] = true
		out.MerchantIDs = append(out.MerchantIDs, id)
	}
	var expected int64
	if update {
		var ok bool
		expected, ok = positiveFundsID(in.Expected)
		if !ok {
			response(w, 422, -1, "canonical expected revision required", nil)
			return out, 0, false
		}
	}
	return out, expected, true
}

func (s *Server) settlementSchedulePreferenceHTTP(ctx context.Context, w http.ResponseWriter, r *http.Request, p []string, actor payment.SettlementPlanActor) {
	if len(p) != 3 {
		response(w, 404, -1, "settlement preference unavailable", nil)
		return
	}
	if !settlementPlanNoQuery(w, r) {
		return
	}
	var out payment.SettlementSchedulePreferenceView
	var err error
	switch r.Method {
	case "GET":
		out, err = s.Store.SettlementSchedulePreference(ctx, actor.ID, actor)
	case "PUT":
		var raw map[string]json.RawMessage
		if !decodeFundsJSON(w, r, &raw) {
			return
		}
		var in struct {
			Expected  string `json:"expected_revision"`
			Enabled   *bool  `json:"enabled"`
			Recipient string `json:"recipient_config_id"`
		}
		body, _ := json.Marshal(raw)
		if len(raw) != 3 || strictFundsJSON(body, &in) != nil || in.Enabled == nil {
			response(w, 422, -1, "explicit preference fields required", nil)
			return
		}
		expected, e := strconv.ParseInt(in.Expected, 10, 64)
		config, ce := strconv.ParseInt(in.Recipient, 10, 64)
		if e != nil || ce != nil || expected < 0 || config < 0 || fmt.Sprint(expected) != in.Expected || fmt.Sprint(config) != in.Recipient || *in.Enabled && config == 0 {
			response(w, 422, -1, "canonical preference revision and original recipient required", nil)
			return
		}
		out, err = s.Store.SetSettlementSchedulePreference(ctx, actor.ID, expected, *in.Enabled, config, actor)
	default:
		response(w, 405, -1, "GET or PUT required", nil)
		return
	}
	if err != nil {
		s.fail(w, err)
	} else {
		response(w, 200, 0, "explicit opt-in to pending request generation only", out)
	}
}
