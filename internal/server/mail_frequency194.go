package server

import (
	"errors"
	"net/http"

	"goepay/internal/identity"
	"goepay/internal/mailpolicy194"
)

func (s *Server) mailFrequency194(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) != 3 || parts[1] != "settings" || parts[2] != "mail-frequency" {
		return false
	}
	identityHeaders(w)
	if role != "admin" || session.User.AdminClass != "super" || session.Issuer != nil {
		response(w, 403, -1, "original super administrator session required", nil)
		return true
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "query parameters not accepted", nil)
		return true
	}
	if s.Pool == nil || len(s.CheckoutKey) != 32 {
		response(w, 503, -1, "mail frequency unavailable", nil)
		return true
	}
	store := mailpolicy194.New(s.Pool, s.CheckoutKey)
	actor := mailpolicy194.Actor{User: session.User, Token: token, Delegated: session.Issuer != nil}
	fail := func(e error) {
		switch {
		case errors.Is(e, identity.ErrAdminSession):
			response(w, 403, -1, "original super administrator session required", nil)
		case errors.Is(e, mailpolicy194.ErrConflict):
			response(w, 409, -1, "mail frequency changed; reload before confirming", nil)
		case errors.Is(e, mailpolicy194.ErrInvalid):
			response(w, 422, -1, "invalid mail frequency", nil)
		default:
			response(w, 503, -1, "mail frequency source could not be confirmed", nil)
		}
	}
	switch r.Method {
	case "GET":
		v, e := store.Read(r.Context(), actor)
		if e != nil {
			fail(e)
		} else {
			response(w, 200, 0, "", v)
		}
	case "PATCH":
		var input struct {
			mailpolicy194.Values
			Expected string `json:"expected_fingerprint"`
		}
		if !decodeBuyerFeeInput104(w, r, &input, "mail_interval_email", "mail_interval_ip", "mail_daily_email", "mail_daily_ip", "expected_fingerprint") {
			return true
		}
		if !identityHex(input.Expected) {
			response(w, 400, -1, "exact current frequency confirmation required", nil)
			return true
		}
		v, e := store.Save(r.Context(), actor, input.Values, input.Expected)
		if e != nil {
			fail(e)
		} else {
			response(w, 200, 0, "", v)
		}
	default:
		w.Header().Set("Allow", "GET, PATCH")
		response(w, 405, -1, "method not allowed", nil)
	}
	return true
}
