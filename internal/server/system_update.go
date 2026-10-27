package server

import (
	"errors"
	"net/http"

	"goepay/internal/identity"
	"goepay/internal/payment"
	"goepay/internal/systemupdate"
)

func systemUpdateError183(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, identity.ErrAdminSession):
		response(w, 403, -1, "original authorized administrator session required", nil)
	case errors.Is(e, payment.ErrInvalid):
		response(w, 400, -1, "exact update intent and fingerprints required", nil)
	case errors.Is(e, payment.ErrConflict), errors.Is(e, systemupdate.ErrChanged):
		response(w, 409, -1, "update state changed; inspect before confirming", nil)
	case errors.Is(e, systemupdate.ErrPackage):
		response(w, 422, -1, "local update package could not be verified", nil)
	default:
		response(w, 503, -1, "update preparation could not be confirmed; read original intent", nil)
	}
}
func (s *Server) systemUpdateConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) < 2 || parts[1] != "system-update" {
		return false
	}
	identityHeaders(w)
	if role != "admin" || session.Issuer != nil || session.User.Username == "" || session.User.AuthVersion <= 0 {
		response(w, 403, -1, "original administrator session required", nil)
		return true
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "query parameters not accepted", nil)
		return true
	}
	if s.Pool == nil || s.Store == nil {
		response(w, 503, -1, "update state unavailable", nil)
		return true
	}
	var schema int64
	if s.Pool != nil {
		if e := s.Pool.QueryRow(r.Context(), `SELECT COALESCE(max(substring(name FROM '^([0-9]+)_')::bigint),0) FROM schema_migrations`).Scan(&schema); e != nil {
			s.recordRuntimeFailure(e)
			response(w, 503, -1, "current build state unavailable", nil)
			return true
		}
	}
	build := systemupdate.RuntimeBuild(schema)
	if s.SystemUpdateBuild != nil {
		build = s.SystemUpdateBuild(schema)
	}
	service := s.SystemUpdate
	if service == nil {
		service, _ = systemupdate.New(systemupdate.Config{})
	}
	actor := payment.MerchantCredentialActor{User: session.User, SessionToken: token, Delegated: session.Issuer != nil}
	switch {
	case len(parts) == 2 && r.Method == http.MethodGet:
		view, e := s.Store.SystemUpdateInspect(r.Context(), service, build, actor)
		if e != nil {
			systemUpdateError183(w, e)
		} else {
			response(w, 200, 0, "", view)
		}
	case len(parts) == 3 && parts[2] == "prepare" && r.Method == http.MethodPost:
		in, ok := identityJSON(w, r, "expected_fingerprint", "intent")
		if !ok {
			return true
		}
		if len(in) != 2 || !identityHex(in["expected_fingerprint"]) || in["expected_fingerprint"] != in["intent"] {
			systemUpdateError183(w, payment.ErrInvalid)
			return true
		}
		status, created, e := s.Store.PrepareSystemUpdate(r.Context(), service, build, actor, in["intent"], in["expected_fingerprint"])
		if e != nil {
			systemUpdateError183(w, e)
		} else {
			code := 200
			if created {
				code = 202
			}
			response(w, code, 0, "", status)
		}
	case len(parts) == 4 && parts[2] == "preparations" && r.Method == http.MethodGet:
		if !identityHex(parts[3]) {
			systemUpdateError183(w, payment.ErrInvalid)
			return true
		}
		status, e := s.Store.SystemUpdateStatus(r.Context(), service, build, actor, parts[3])
		if e != nil {
			systemUpdateError183(w, e)
		} else {
			response(w, 200, 0, "", status)
		}
	case len(parts) == 5 && parts[2] == "preparations" && r.Method == http.MethodPost && (parts[4] == "cancel" || parts[4] == "retry"):
		if !identityHex(parts[3]) {
			systemUpdateError183(w, payment.ErrInvalid)
			return true
		}
		fields := []string{"expected_state"}
		if parts[4] == "retry" {
			fields = append(fields, "expected_fingerprint")
		}
		in, ok := identityJSON(w, r, fields...)
		if !ok {
			return true
		}
		if len(in) != len(fields) || !identityHex(in["expected_state"]) || parts[4] == "retry" && (in["expected_fingerprint"] != parts[3] || !identityHex(in["expected_fingerprint"])) {
			systemUpdateError183(w, payment.ErrInvalid)
			return true
		}
		var status payment.SystemUpdateStatus
		var e error
		code := 200
		if parts[4] == "cancel" {
			status, e = s.Store.CancelSystemUpdate(r.Context(), service, build, actor, parts[3], in["expected_state"])
		} else {
			status, e = s.Store.RetrySystemUpdate(r.Context(), service, build, actor, parts[3], in["expected_fingerprint"], in["expected_state"])
			code = 202
		}
		if e != nil {
			systemUpdateError183(w, e)
		} else {
			response(w, code, 0, "", status)
		}
	default:
		response(w, 404, -1, "unknown system update action", nil)
	}
	return true
}
