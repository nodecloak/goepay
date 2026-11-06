package server

import (
	"errors"
	"goepay/internal/identity"
	"goepay/internal/mailaccount"
	"net/http"
)

func (s *Server) mailAccounts195(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) < 3 || parts[1] != "settings" || parts[2] != "mail-channels" {
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
	if len(parts) >= 5 && parts[3] == "smtp" && (parts[4] == "test" || parts[4] == "tests") {
		return s.mailTest195(w, r, session, parts, token)
	}
	if len(parts) > 4 || (len(parts) == 4 && parts[3] != "smtp") {
		response(w, 404, -1, "not found", nil)
		return true
	}
	if s.Pool == nil || len(s.CheckoutKey) != 32 {
		response(w, 503, -1, "mail account unavailable", nil)
		return true
	}
	store := mailaccount.New(s.Pool, s.CheckoutKey)
	actor := mailaccount.Actor{User: session.User, Token: token, Delegated: session.Issuer != nil}
	fail := func(e error) {
		switch {
		case errors.Is(e, identity.ErrAdminSession):
			response(w, 403, -1, "original super administrator session required", nil)
		case errors.Is(e, mailaccount.ErrConflict):
			response(w, 409, -1, "mail account changed; reload before confirming", nil)
		case errors.Is(e, mailaccount.ErrInvalid):
			response(w, 422, -1, "invalid SMTP account fields", nil)
		default:
			response(w, 503, -1, "mail account source could not be confirmed", nil)
		}
	}
	switch r.Method {
	case "GET":
		v, e := store.Read(r.Context(), actor)
		v.TestAvailable = s.Mail195TestEnabled && v.Configured && v.Status == 1
		if e != nil {
			fail(e)
		} else if len(parts) == 3 {
			response(w, 200, 0, "", map[string]any{"items": []map[string]any{{"name": v.Name, "title": v.Title, "status": v.Status, "configured": v.Configured}}, "total": 1})
		} else {
			response(w, 200, 0, "", v)
		}
	case "PATCH":
		if len(parts) != 4 {
			response(w, 405, -1, "method not allowed", nil)
			return true
		}
		var input struct {
			Name   *string `json:"name"`
			Status *int    `json:"status"`
			Config *struct {
				Host     *string `json:"host"`
				Port     *string `json:"port"`
				Username *string `json:"username"`
				Password *string `json:"password"`
			} `json:"config"`
			KeepPassword *bool   `json:"keep_password"`
			Expected     *string `json:"expected_fingerprint"`
		}
		if !decodeFundsJSON(w, r, &input) {
			return true
		}
		if input.Name == nil || input.Status == nil || input.Config == nil || input.Config.Host == nil || input.Config.Port == nil || input.Config.Username == nil || input.Expected == nil || !identityHex(*input.Expected) {
			response(w, 400, -1, "exact original SMTP fields and current confirmation required", nil)
			return true
		}
		keep := input.KeepPassword != nil && *input.KeepPassword
		v, e := store.Save(r.Context(), actor, mailaccount.Input{Name: *input.Name, Status: *input.Status, Config: mailaccount.Config{Host: *input.Config.Host, Port: *input.Config.Port, Username: *input.Config.Username}, Password: input.Config.Password, KeepPassword: keep, Expected: *input.Expected})
		v.TestAvailable = s.Mail195TestEnabled && v.Configured && v.Status == 1
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
