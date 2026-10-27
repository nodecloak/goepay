package server

import (
	"errors"
	"goepay/internal/identity"
	"goepay/internal/mailaccount195"
	"net/http"
)

func (s *Server) mailTest195(w http.ResponseWriter, r *http.Request, session identity.Session, parts []string, token string) bool {
	if s.Pool == nil || len(s.CheckoutKey) != 32 {
		response(w, 503, -1, "mail test unavailable", nil)
		return true
	}
	store := mailaccount195.New(s.Pool, s.CheckoutKey)
	actor := mailaccount195.Actor{User: session.User, Token: token, Delegated: session.Issuer != nil}
	fail := func(e error) {
		switch {
		case errors.Is(e, identity.ErrAdminSession):
			response(w, 403, -1, "original super administrator session required", nil)
		case errors.Is(e, mailaccount195.ErrInvalid):
			response(w, 422, -1, "invalid original test email request", nil)
		case errors.Is(e, mailaccount195.ErrTestMissing):
			response(w, 404, -1, "original test email request not found", nil)
		case errors.Is(e, mailaccount195.ErrConflict):
			response(w, 409, -1, "test request or account changed; inspect original request", nil)
		case errors.Is(e, mailaccount195.ErrTestLimited):
			response(w, 429, -1, "test email submission limit exceeded", nil)
		default:
			response(w, 503, -1, "test email source could not be confirmed", nil)
		}
	}
	if len(parts) == 6 && parts[4] == "tests" {
		if r.Method != "GET" {
			response(w, 405, -1, "method not allowed", nil)
			return true
		}
		v, e := store.ReadTest(r.Context(), actor, parts[5])
		if e != nil {
			fail(e)
		} else {
			response(w, 200, 0, "", v)
		}
		return true
	}
	if len(parts) != 5 || parts[4] != "test" {
		response(w, 404, -1, "not found", nil)
		return true
	}
	if r.Method != "POST" {
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	if !s.Mail195TestEnabled {
		response(w, 503, -1, "test email submission is disabled", nil)
		return true
	}
	var input struct {
		ID       string `json:"request_id"`
		To       string `json:"to"`
		Expected string `json:"expected_fingerprint"`
	}
	if !decodeBuyerFeeInput104(w, r, &input, "request_id", "to", "expected_fingerprint") {
		return true
	}
	if !identityHex(input.Expected) {
		response(w, 400, -1, "exact current account confirmation required", nil)
		return true
	}
	v, e := store.SubmitTest(r.Context(), actor, mailaccount195.TestInput{ID: input.ID, To: input.To, Expected: input.Expected}, s.Mail195SMTPRoots)
	if e != nil {
		fail(e)
	} else {
		response(w, 200, 0, "", v)
	}
	return true
}
