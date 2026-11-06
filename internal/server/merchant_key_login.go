package server

import (
	"errors"
	"net/http"

	"goepay/internal/identity"
	"goepay/internal/payment"
)

func (s *Server) merchantKeyLogin(w http.ResponseWriter, r *http.Request) {
	identityHeaders(w)
	if r.Method != "POST" {
		response(w, 405, -1, "POST JSON required", nil)
		return
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "query parameters not accepted", nil)
		return
	}
	if !s.loginFromThisSite(r) {
		response(w, 403, -1, "cross-site login refused", nil)
		return
	}
	in, ok := identityJSON(w, r, "merchant_id", "api_key", "code")
	if !ok {
		return
	}
	mid, ok := positiveFundsID(in["merchant_id"])
	if !ok || len(in) != 3 || in["api_key"] == "" || len(in["api_key"]) > 512 || len(in["code"]) > 16 {
		response(w, 400, -1, "exact merchant key login required", nil)
		return
	}
	if s.Pool == nil || s.Store == nil || s.Sessions == nil || !s.Sessions.SupportsDelegation() {
		response(w, 503, -1, "persistent merchant authentication unavailable", nil)
		return
	}
	success := false
	var verified identity.User
	username := "merchant-key:" + in["merchant_id"]
	defer func() { s.recordLogin(r, "merchant", username, &success, verified) }()
	// A nonlocking name projection shares the existing password account budget.
	// Exhausted requests must be rejected before waiting on credential locks.
	var candidate string
	var accounts int
	if e := s.Pool.QueryRow(r.Context(), `SELECT COALESCE(min(username),''),count(*) FROM console_users WHERE role='merchant' AND subject_id=$1 AND enabled`, mid).Scan(&candidate, &accounts); e != nil {
		response(w, 503, -1, "login verification unavailable", nil)
		return
	}
	if accounts == 1 {
		username = candidate
	}
	allowed, e := s.allowLoginRequest(r, "merchant", username)
	if e != nil {
		response(w, 503, -1, "login verification unavailable", nil)
		return
	}
	if !allowed {
		w.Header().Set("Retry-After", "900")
		response(w, 429, -1, "too many attempts", nil)
		return
	}
	if !claimLoginVerification() {
		w.Header().Set("Retry-After", "1")
		response(w, 429, -1, "login verification busy", nil)
		return
	}
	defer releaseLoginVerification()
	grant, prepareError := s.Store.PrepareMerchantKeyLogin(r.Context(), mid, in["api_key"])
	if grant.User().Username != "" {
		username = grant.User().Username
	}
	if prepareError != nil {
		if errors.Is(prepareError, payment.ErrMerchantKeyLogin) {
			response(w, 401, -1, "invalid credentials", nil)
		} else {
			response(w, 503, -1, "key login could not be verified", nil)
		}
		return
	}
	if !s.checkMFA(r.Context(), username, "merchant", in["code"]) {
		response(w, 401, -1, "invalid credentials or authenticator code", nil)
		return
	}
	user, token, csrf, e := s.Store.CompleteMerchantKeyLogin(r.Context(), grant, s.Sessions)
	if e != nil {
		if errors.Is(e, payment.ErrMerchantKeyLogin) || errors.Is(e, identity.ErrOriginalMerchantSession) {
			response(w, 401, -1, "invalid credentials", nil)
		} else {
			response(w, 503, -1, "key login could not be completed", nil)
		}
		return
	}
	success, verified = true, user
	http.SetCookie(w, &http.Cookie{Name: "goepay_merchant", Value: token, Path: "/console/v1/merchant", HttpOnly: true, Secure: s.Sessions.Secure, SameSite: http.SameSiteStrictMode, MaxAge: 43200})
	response(w, 200, 0, "", map[string]any{"user": consoleSessionUser(user), "csrf_token": csrf})
}
