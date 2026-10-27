package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"goepay/internal/identity"
	"goepay/internal/payment"
)

func runtimeChannelLimitsError(w http.ResponseWriter, e error) {
	if errors.Is(e, payment.ErrInvalid) {
		response(w, 422, -1, "invalid global parent channel limits", nil)
		return
	}
	runtimeChannelPolicyError(w, e)
}

func runtimeChannelLimitsInput(w http.ResponseWriter, r *http.Request) (payment.RuntimeChannelLimitsInput, string, bool) {
	var out payment.RuntimeChannelLimitsInput
	fail := func() (payment.RuntimeChannelLimitsInput, string, bool) {
		response(w, 400, -1, "invalid global channel limits JSON", nil)
		return out, "", false
	}
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		return fail()
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	token, e := d.Token()
	if e != nil || token != json.Delim('{') {
		return fail()
	}
	var expected string
	fields := map[string]*string{"daytop": &out.DayTop, "daymaxorder": &out.DayMaxOrder, "paymin": &out.PayMin, "paymax": &out.PayMax, "timestart": &out.TimeStart, "timestop": &out.TimeStop, "rate_limit_seconds": &out.RateLimitSeconds, "rate_limit_count": &out.RateLimitCount, "expected_fingerprint": &expected}
	seen := map[string]bool{}
	for d.More() {
		token, e = d.Token()
		if e != nil {
			return fail()
		}
		key, ok := token.(string)
		if !ok || seen[key] || fields[key] == nil {
			return fail()
		}
		seen[key] = true
		var raw json.RawMessage
		if d.Decode(&raw) != nil || string(raw) == "null" || json.Unmarshal(raw, fields[key]) != nil {
			return fail()
		}
	}
	if token, e = d.Token(); e != nil || token != json.Delim('}') || len(seen) != len(fields) {
		return fail()
	}
	if _, e = d.Token(); e != io.EOF || !identityHex(expected) {
		return fail()
	}
	return out, expected, true
}
func (s *Server) runtimeChannelLimits(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) < 4 || parts[1] != "channel-policies" || parts[3] != "limits" {
		return false
	}
	identityHeaders(w)
	if role != "admin" || session.User.AdminClass != "super" || session.Issuer != nil {
		response(w, 403, -1, "original super administrator session required", nil)
		return true
	}
	if len(parts) != 4 || r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "invalid global channel limits target", nil)
		return true
	}
	id, ok := positiveFundsID(parts[2])
	if !ok {
		response(w, 400, -1, "invalid runtime channel target", nil)
		return true
	}
	if s.Store == nil || s.ProviderFlow == nil {
		response(w, 503, -1, "runtime channel registry unavailable", nil)
		return true
	}
	binding, e := s.ProviderFlow.AdminParentRuntimeChannel(r.Context(), id)
	if e != nil {
		response(w, 404, -1, "registered runtime channel unavailable", nil)
		return true
	}
	actor := payment.MerchantCredentialActor{User: session.User, SessionToken: token, Delegated: session.Issuer != nil}
	switch r.Method {
	case "GET":
		out, e := s.Store.RuntimeChannelLimits(r.Context(), binding, actor)
		if e != nil {
			runtimeChannelLimitsError(w, e)
		} else {
			response(w, 200, 0, "", out)
		}
	case "PATCH":
		in, expected, ok := runtimeChannelLimitsInput(w, r)
		if !ok {
			return true
		}
		out, e := s.Store.UpdateRuntimeChannelLimits(r.Context(), binding, actor, in, expected)
		if e != nil {
			runtimeChannelLimitsError(w, e)
		} else {
			response(w, 200, 0, "global channel limits updated", out)
		}
	default:
		response(w, 405, -1, "method not allowed", nil)
	}
	return true
}
