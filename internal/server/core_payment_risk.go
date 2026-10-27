package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"goepay/internal/identity"
	"goepay/internal/payment"
	"goepay/internal/riskpolicy"
)

func coreRiskObject(d *json.Decoder, names []string) (map[string]json.RawMessage, error) {
	v, e := d.Token()
	if e != nil || v != json.Delim('{') {
		return nil, payment.ErrInvalid
	}
	out := map[string]json.RawMessage{}
	for d.More() {
		v, e = d.Token()
		if e != nil {
			return nil, e
		}
		name, ok := v.(string)
		if !ok || out[name] != nil {
			return nil, payment.ErrInvalid
		}
		allowed := false
		for _, n := range names {
			allowed = allowed || n == name
		}
		if !allowed {
			return nil, payment.ErrInvalid
		}
		var raw json.RawMessage
		if e = d.Decode(&raw); e != nil || strings.TrimSpace(string(raw)) == "null" {
			return nil, payment.ErrInvalid
		}
		out[name] = raw
	}
	if v, e = d.Token(); e != nil || v != json.Delim('}') || len(out) != len(names) {
		return nil, payment.ErrInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, payment.ErrInvalid
	}
	return out, nil
}
func coreRiskInput(w http.ResponseWriter, r *http.Request) (payment.CoreRiskSettings, string, bool) {
	fail := func() (payment.CoreRiskSettings, string, bool) {
		response(w, 400, -1, "invalid core risk policy JSON", nil)
		return nil, "", false
	}
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		return fail()
	}
	outer, e := coreRiskObject(json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768)), []string{"settings", "expected_fingerprint"})
	if e != nil {
		return fail()
	}
	var expected string
	if json.Unmarshal(outer["expected_fingerprint"], &expected) != nil || !identityHex(expected) {
		return fail()
	}
	names := []string{}
	for key := range riskpolicy.Defaults() {
		names = append(names, key)
	}
	fields, e := coreRiskObject(json.NewDecoder(strings.NewReader(string(outer["settings"]))), names)
	if e != nil {
		return fail()
	}
	out := payment.CoreRiskSettings{}
	for key, raw := range fields {
		var value string
		if json.Unmarshal(raw, &value) != nil {
			return fail()
		}
		out[key] = value
	}
	return out, expected, true
}

func (s *Server) corePaymentRisk(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) < 2 || parts[1] != "payment-risk" {
		return false
	}
	identityHeaders(w)
	if role != "admin" || session.User.AdminClass != "super" || session.Issuer != nil {
		response(w, 403, -1, "original super administrator session required", nil)
		return true
	}
	if len(parts) != 2 || r.URL.RawQuery != "" {
		if len(parts) == 4 && parts[2] == "groups" && r.URL.RawQuery == "" {
			s.coreRiskGroupHTTP(w, r, session, token, parts[3])
			return true
		}
		if len(parts) == 4 && parts[2] == "check-runs" && r.URL.RawQuery == "" {
			if r.Method != "GET" {
				response(w, 405, -1, "risk check read method unavailable", nil)
				return true
			}
			out, e := s.Store.CoreRiskCheckResult(r.Context(), payment.MerchantCredentialActor{User: session.User, SessionToken: token, Delegated: session.Issuer != nil}, parts[3])
			if e == nil {
				response(w, 200, 0, "", out)
			} else if errors.Is(e, payment.ErrNotFound) {
				response(w, 404, -1, "original risk check result is not confirmed", nil)
			} else if errors.Is(e, payment.ErrConflict) {
				response(w, 409, -1, "original risk check source could not be confirmed", nil)
			} else if errors.Is(e, identity.ErrAdminSession) {
				response(w, 403, -1, "original super administrator session required", nil)
			} else {
				response(w, 503, -1, "original risk check result could not be confirmed", nil)
			}
			return true
		}
		if len(parts) >= 3 && parts[2] == "actions" && r.URL.RawQuery == "" {
			s.coreRiskActionsHTTP(w, r, session, token, parts)
			return true
		}
		if len(parts) == 3 && parts[2] == "check" && r.URL.RawQuery == "" {
			s.coreRiskCheckHTTP(w, r, session, token)
			return true
		}
		response(w, 400, -1, "invalid core payment risk target", nil)
		return true
	}
	if r.Method != "GET" && r.Method != "PATCH" {
		response(w, 405, -1, "core payment risk method unavailable", nil)
		return true
	}
	a := payment.MerchantCredentialActor{User: session.User, SessionToken: token, Delegated: session.Issuer != nil}
	var out payment.CoreRiskView
	var e error
	if r.Method == "GET" {
		out, e = s.Store.CoreRiskPolicy(r.Context(), a)
	} else {
		settings, expected, ok := coreRiskInput(w, r)
		if !ok {
			return true
		}
		out, e = s.Store.UpdateCoreRiskPolicy(r.Context(), a, settings, expected)
	}
	if e != nil {
		switch {
		case errors.Is(e, identity.ErrAdminSession):
			response(w, 403, -1, "original super administrator session required", nil)
		case errors.Is(e, payment.ErrConflict):
			response(w, 409, -1, "core risk policy changed; refresh before confirming", nil)
		case errors.Is(e, payment.ErrInvalid):
			response(w, 422, -1, "invalid core risk policy values", nil)
		default:
			s.recordRuntimeFailure(e)
			response(w, 503, -1, "core risk policy could not be confirmed", nil)
		}
	} else {
		response(w, 200, 0, "", out)
	}
	return true
}
func (s *Server) coreRiskGroupHTTP(w http.ResponseWriter, r *http.Request, session identity.Session, token, rawID string) {
	id, e := strconv.ParseInt(rawID, 10, 64)
	if e != nil || id < 0 || strconv.FormatInt(id, 10) != rawID {
		response(w, 400, -1, "invalid group risk target", nil)
		return
	}
	actor := payment.MerchantCredentialActor{User: session.User, SessionToken: token, Delegated: session.Issuer != nil}
	var out payment.CoreRiskGroupView
	if r.Method == "GET" {
		out, e = s.Store.CoreRiskGroupPolicy(r.Context(), actor, id)
	} else if r.Method == "PATCH" {
		if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
			response(w, 400, -1, "JSON content type required", nil)
			return
		}
		fields, err := coreRiskObject(json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)), []string{"config", "expected_fingerprint"})
		var expected string
		if err != nil || json.Unmarshal(fields["expected_fingerprint"], &expected) != nil || !identityHex(expected) {
			response(w, 400, -1, "invalid group risk JSON", nil)
			return
		}
		config, err := coreRiskGroupConfigObject(json.NewDecoder(strings.NewReader(string(fields["config"]))))
		if err != nil {
			response(w, 400, -1, "invalid group risk config", nil)
			return
		}
		out, e = s.Store.UpdateCoreRiskGroupPolicy(r.Context(), actor, id, config, expected)
	} else {
		response(w, 405, -1, "group risk method unavailable", nil)
		return
	}
	if e == nil {
		response(w, 200, 0, "", out)
	} else if errors.Is(e, payment.ErrConflict) {
		response(w, 409, -1, "group or inherited risk settings changed; refresh before confirming", nil)
	} else if errors.Is(e, identity.ErrAdminSession) {
		response(w, 403, -1, "original super administrator session required", nil)
	} else if errors.Is(e, payment.ErrNotFound) {
		response(w, 404, -1, "group unavailable", nil)
	} else if errors.Is(e, payment.ErrInvalid) {
		response(w, 422, -1, "invalid group risk config or local lookup unavailable", nil)
	} else {
		s.recordRuntimeFailure(e)
		response(w, 503, -1, "group risk settings could not be confirmed", nil)
	}
}
func (s *Server) coreRiskCheckHTTP(w http.ResponseWriter, r *http.Request, session identity.Session, token string) {
	if r.Method != "POST" {
		response(w, 405, -1, "core risk check method unavailable", nil)
		return
	}
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		response(w, 400, -1, "JSON content type required", nil)
		return
	}
	fields, e := coreRiskObject(json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)), []string{"expected_fingerprint", "external_no"})
	var expected, external string
	if e != nil || json.Unmarshal(fields["expected_fingerprint"], &expected) != nil || !identityHex(expected) || json.Unmarshal(fields["external_no"], &external) != nil {
		response(w, 400, -1, "invalid core risk check JSON", nil)
		return
	}
	out, e := s.Store.RunCoreRiskCheckAsAdmin(r.Context(), payment.MerchantCredentialActor{User: session.User, SessionToken: token, Delegated: session.Issuer != nil}, external, expected)
	if e == nil {
		response(w, 200, 0, "", out)
	} else if errors.Is(e, payment.ErrConflict) {
		response(w, 409, -1, "core risk facts changed; refresh before confirming", nil)
	} else if errors.Is(e, identity.ErrAdminSession) {
		response(w, 403, -1, "original super administrator session required", nil)
	} else if errors.Is(e, payment.ErrInvalid) {
		response(w, 422, -1, "invalid core risk check values", nil)
	} else {
		s.recordRuntimeFailure(e)
		response(w, 503, -1, "core risk check could not be confirmed", nil)
	}
}
func (s *Server) coreRiskActionsHTTP(w http.ResponseWriter, r *http.Request, session identity.Session, token string, parts []string) {
	actor := payment.MerchantCredentialActor{User: session.User, SessionToken: token, Delegated: session.Issuer != nil}
	var out any
	var e error
	if len(parts) == 3 && r.Method == "GET" {
		var items []payment.CoreRiskActionView
		items, e = s.Store.CoreRiskActions(r.Context(), actor)
		out = map[string]any{"items": items, "total": len(items)}
	} else if len(parts) == 5 && parts[4] == "release" && r.Method == "POST" {
		id, err := strconv.ParseInt(parts[3], 10, 64)
		if err != nil || id < 1 || strconv.FormatInt(id, 10) != parts[3] || strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
			response(w, 400, -1, "invalid risk action target", nil)
			return
		}
		fields, err := coreRiskObject(json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)), []string{"expected_fingerprint", "expected_policy_fingerprint"})
		var expected, policy string
		if err != nil || json.Unmarshal(fields["expected_fingerprint"], &expected) != nil || json.Unmarshal(fields["expected_policy_fingerprint"], &policy) != nil || !identityHex(expected) || !identityHex(policy) {
			response(w, 400, -1, "invalid risk action JSON", nil)
			return
		}
		out, e = s.Store.ReleaseCoreRiskAction(r.Context(), actor, id, expected, policy)
	} else {
		response(w, 405, -1, "risk action method unavailable", nil)
		return
	}
	if e == nil {
		response(w, 200, 0, "", out)
	} else if errors.Is(e, identity.ErrAdminSession) {
		response(w, 403, -1, "original super administrator session required", nil)
	} else if errors.Is(e, payment.ErrConflict) {
		response(w, 409, -1, "risk action changed; refresh before confirming", nil)
	} else if errors.Is(e, payment.ErrNotFound) {
		response(w, 404, -1, "risk action unavailable", nil)
	} else if errors.Is(e, payment.ErrInvalid) {
		response(w, 422, -1, "invalid risk action values", nil)
	} else {
		s.recordRuntimeFailure(e)
		response(w, 503, -1, "risk action could not be confirmed", nil)
	}
}

func coreRiskGroupConfigObject(d *json.Decoder) (map[string]string, error) {
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return nil, payment.ErrInvalid
	}
	out := map[string]string{}
	for d.More() {
		t, e = d.Token()
		if e != nil {
			return nil, e
		}
		key, ok := t.(string)
		if !ok || (key != "pay_block_oversea" && key != "check_pay_regoin") {
			return nil, payment.ErrInvalid
		}
		if _, ok = out[key]; ok {
			return nil, payment.ErrInvalid
		}
		var raw json.RawMessage
		if e = d.Decode(&raw); e != nil {
			return nil, e
		}
		if strings.TrimSpace(string(raw)) == "null" {
			return nil, payment.ErrInvalid
		}
		var v string
		if e = json.Unmarshal(raw, &v); e != nil {
			return nil, e
		}
		out[key] = v
	}
	if t, e = d.Token(); e != nil || t != json.Delim('}') {
		return nil, payment.ErrInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, payment.ErrInvalid
	}
	return riskpolicy.NormalizeGroupConfig(out)
}
