package server

import (
	"errors"
	"net/http"

	"goepay/internal/identity"
	"goepay/internal/payment"
)

func platformAccessError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, identity.ErrAdminSession):
		response(w, 403, -1, "original super administrator session required", nil)
	case errors.Is(e, payment.ErrInvalid):
		response(w, 400, -1, "exact platform access settings required", nil)
	case errors.Is(e, payment.ErrConflict):
		response(w, 409, -1, "platform access changed; read before confirming", nil)
	default:
		response(w, 503, -1, "platform access could not be confirmed", nil)
	}
}

func (s *Server) publicMerchantAccess(w http.ResponseWriter, r *http.Request) {
	identityHeaders(w)
	if r.Method != "GET" {
		response(w, 405, -1, "GET required", nil)
		return
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "query parameters not accepted", nil)
		return
	}
	if s.Store == nil {
		response(w, 503, -1, "platform access unavailable", nil)
		return
	}
	out, e := s.Store.PublicMerchantAccess(r.Context())
	if e != nil {
		response(w, 503, -1, "platform access could not be confirmed", nil)
		return
	}
	response(w, 200, 0, "", out)
}

func (s *Server) platformMerchantAccessSettings(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) < 2 || parts[1] != "settings" {
		return false
	}
	identityHeaders(w)
	if len(parts) != 3 || parts[2] != "merchant-access" {
		response(w, 404, -1, "not found", nil)
		return true
	}
	if role != "admin" || session.Issuer != nil || session.User.AdminClass != "super" {
		response(w, 403, -1, "original super administrator session required", nil)
		return true
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "query parameters not accepted", nil)
		return true
	}
	if s.Store == nil {
		response(w, 503, -1, "platform access unavailable", nil)
		return true
	}
	a := payment.MerchantCredentialActor{User: session.User, SessionToken: token, Delegated: session.Issuer != nil}
	if r.Method == "GET" {
		out, e := s.Store.PlatformMerchantAccess(r.Context(), a)
		if e != nil {
			platformAccessError(w, e)
		} else {
			response(w, 200, 0, "", out)
		}
		return true
	}
	if r.Method != "PATCH" {
		response(w, 405, -1, "GET or PATCH required", nil)
		return true
	}
	in, ok := identityJSON(w, r, "close_keylogin", "user_refund", "expected_fingerprint")
	if !ok {
		return true
	}
	if len(in) != 3 || !identityHex(in["expected_fingerprint"]) {
		platformAccessError(w, payment.ErrInvalid)
		return true
	}
	out, e := s.Store.UpdatePlatformMerchantAccess(r.Context(), a, in["close_keylogin"], in["user_refund"], in["expected_fingerprint"])
	if e != nil {
		platformAccessError(w, e)
	} else {
		response(w, 200, 0, "", out)
	}
	return true
}
