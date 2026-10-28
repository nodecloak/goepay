package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"

	provider "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

func runtimeChannelPolicyInput(w http.ResponseWriter, r *http.Request) (rate string, mode int, enabled bool, expected string, ok bool) {
	fail := func() (string, int, bool, string, bool) {
		response(w, 400, -1, "invalid runtime channel policy JSON", nil)
		return "", 0, false, "", false
	}
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		return fail()
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	token, e := d.Token()
	if e != nil || token != json.Delim('{') {
		return fail()
	}
	seen := map[string]bool{}
	for d.More() {
		token, e = d.Token()
		if e != nil {
			return fail()
		}
		key, valid := token.(string)
		if !valid || seen[key] || (key != "rate" && key != "mode" && key != "enabled" && key != "expected_fingerprint") {
			return fail()
		}
		seen[key] = true
		var raw json.RawMessage
		if e = d.Decode(&raw); e != nil || string(raw) == "null" {
			return fail()
		}
		switch key {
		case "rate":
			if json.Unmarshal(raw, &rate) != nil {
				return fail()
			}
		case "mode":
			if json.Unmarshal(raw, &mode) != nil {
				return fail()
			}
		case "enabled":
			if json.Unmarshal(raw, &enabled) != nil {
				return fail()
			}
		case "expected_fingerprint":
			if json.Unmarshal(raw, &expected) != nil || !identityHex(expected) {
				return fail()
			}
		}
	}
	if token, e = d.Token(); e != nil || token != json.Delim('}') || len(seen) != 4 {
		return fail()
	}
	if _, e = d.Token(); e != io.EOF {
		return fail()
	}
	return rate, mode, enabled, expected, true
}
func runtimeChannelPolicyError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, identity.ErrAdminSession):
		response(w, 403, -1, "original super administrator session required", nil)
	case errors.Is(e, payment.ErrConflict):
		response(w, 409, -1, "runtime channel policy or identity changed; refresh before confirming", nil)
	case errors.Is(e, payment.ErrInvalid):
		response(w, 422, -1, "invalid retained rate or unsupported collection mode", nil)
	default:
		response(w, 503, -1, "runtime channel policy could not be confirmed", nil)
	}
}
func (s *Server) runtimeChannelPolicies(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) < 2 || parts[1] != "channel-policies" {
		return false
	}
	identityHeaders(w)
	if role != "admin" || session.User.AdminClass != "super" || session.Issuer != nil {
		response(w, 403, -1, "original super administrator session required", nil)
		return true
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery || len(parts) > 3 {
		response(w, 400, -1, "invalid runtime channel target", nil)
		return true
	}
	if s.ProviderFlow == nil || s.Store == nil {
		response(w, 503, -1, "runtime channel registry unavailable", nil)
		return true
	}
	a := payment.MerchantCredentialActor{User: session.User, SessionToken: token, Delegated: session.Issuer != nil}
	bindings := []payment.RuntimeChannel{}
	if len(parts) == 2 {
		if r.Method != "GET" {
			response(w, 405, -1, "method not allowed", nil)
			return true
		}
		ids := []int64{}
		for id, p := range s.ProviderFlow.Providers {
			if p != nil && provider.CatalogPayType(p.ID()) {
				ids = append(ids, id)
			}
		}
		if len(ids) > 1000 {
			response(w, 503, -1, "runtime channel registry unavailable", nil)
			return true
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, id := range ids {
			b, e := s.ProviderFlow.RuntimeChannel(id)
			if e != nil {
				runtimeChannelPolicyError(w, e)
				return true
			}
			bindings = append(bindings, b)
		}
	} else {
		id, ok := positiveFundsID(parts[2])
		if !ok {
			response(w, 400, -1, "invalid runtime channel target", nil)
			return true
		}
		b, e := s.ProviderFlow.AdminParentRuntimeChannel(r.Context(), id)
		if e != nil {
			response(w, 404, -1, "registered runtime channel unavailable", nil)
			return true
		}
		bindings = append(bindings, b)
	}
	if r.Method == "GET" {
		items, e := s.Store.RuntimeChannelPolicies(r.Context(), bindings, a)
		if e != nil {
			runtimeChannelPolicyError(w, e)
		} else if len(parts) == 2 {
			response(w, 200, 0, "", map[string]any{"items": items, "total": len(items)})
		} else {
			response(w, 200, 0, "", items[0])
		}
		return true
	}
	if r.Method != "PATCH" {
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	rate, mode, enabled, expected, ok := runtimeChannelPolicyInput(w, r)
	if !ok {
		return true
	}
	out, e := s.Store.UpdateRuntimeChannelPolicy(r.Context(), bindings[0], a, rate, mode, enabled, expected)
	if e != nil {
		runtimeChannelPolicyError(w, e)
	} else {
		response(w, 200, 0, "runtime channel policy updated", out)
	}
	return true
}
