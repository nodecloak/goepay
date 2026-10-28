package server

import (
	"encoding/json"
	"net/http"

	"goepay/internal/identity"
	"goepay/internal/proxyconfig"
)

func (s *Server) proxyCredentialStore() (*proxyconfig.Store, error) {
	return proxyconfig.NewStore(s.Pool, s.CheckoutKey)
}

// proxyCredentialsConsole: /console/v1/admin/settings/proxy-credentials, the
// write-only 代理密码 / API接口密钥 editor. It never returns either secret.
func (s *Server) proxyCredentialsConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) != 3 || parts[1] != "settings" || parts[2] != "proxy-credentials" {
		return false
	}
	identityHeaders(w)
	if role != "admin" || session.Issuer != nil || session.User.AdminClass != "super" {
		response(w, 403, -1, "original super administrator session required", nil)
		return true
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "query parameters not accepted", nil)
		return true
	}
	store, err := s.proxyCredentialStore()
	if err != nil {
		response(w, 503, -1, "proxy credential settings unavailable", nil)
		return true
	}
	actor := proxyconfig.Actor{User: session.User, Token: token, CSRF: session.CSRF, Delegated: session.Issuer != nil}
	var out proxyconfig.View
	switch r.Method {
	case "GET":
		out, err = store.Read(r.Context(), actor)
	case "PATCH":
		var fields map[string]json.RawMessage
		if !decodeFundsJSON(w, r, &fields) {
			return true
		}
		var in proxyconfig.Update
		if json.Unmarshal(fields["expected_fingerprint"], &in.Expected) != nil {
			response(w, 400, -1, "expected_fingerprint required", nil)
			return true
		}
		for field, value := range fields {
			switch field {
			case "expected_fingerprint":
			case "password":
				if err := json.Unmarshal(value, &in.Password); err != nil || in.Password == nil {
					response(w, 400, -1, "string proxy password required", nil)
					return true
				}
			case "apikey":
				if err := json.Unmarshal(value, &in.APIKey); err != nil || in.APIKey == nil {
					response(w, 400, -1, "string proxy API key required", nil)
					return true
				}
			case "clear_password":
				if json.Unmarshal(value, &in.ClearPwd) != nil {
					response(w, 400, -1, "boolean clear_password required", nil)
					return true
				}
			case "clear_apikey":
				if json.Unmarshal(value, &in.ClearAPIKey) != nil {
					response(w, 400, -1, "boolean clear_apikey required", nil)
					return true
				}
			default:
				response(w, 400, -1, "unsupported proxy credential field", nil)
				return true
			}
		}
		out, err = store.Update(r.Context(), actor, in)
	default:
		w.Header().Set("Allow", "GET, PATCH")
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	if err != nil {
		status := 503
		if err == identity.ErrAdminSession {
			status = 403
		} else if err == proxyconfig.ErrConflict {
			status = 409
		} else if err == proxyconfig.ErrInvalid {
			status = 422
		}
		response(w, status, -1, "proxy credential operation could not be confirmed; reload before retrying", nil)
	} else {
		response(w, 200, 0, "", out)
	}
	return true
}
