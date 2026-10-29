package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"goepay/internal/connectconfig"
	"goepay/internal/identity"
)

// oauthCredentialsConsole: /console/v1/admin/settings/oauth-credentials, the
// write-only 快捷登录 provider-secret editor (QQ/微信/支付宝 appkey and the
// Telegram bot token). It never returns any secret, only whether each is set.
func (s *Server) oauthCredentialsConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) != 3 || parts[1] != "settings" || parts[2] != "oauth-credentials" {
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
	store := s.connectConfigStore()
	if store == nil {
		response(w, 503, -1, "oauth credential settings unavailable", nil)
		return true
	}
	actor := connectconfig.Actor{User: session.User, Token: token, CSRF: session.CSRF, Delegated: session.Issuer != nil}
	var out connectconfig.View
	var err error
	switch r.Method {
	case "GET":
		out, err = store.Read(r.Context(), actor)
	case "PATCH":
		var fields map[string]json.RawMessage
		if !decodeFundsJSON(w, r, &fields) {
			return true
		}
		var in connectconfig.Update
		if json.Unmarshal(fields["field"], &in.Field) != nil || json.Unmarshal(fields["expected_fingerprint"], &in.Expected) != nil {
			response(w, 400, -1, "field and expected_fingerprint required", nil)
			return true
		}
		for field, value := range fields {
			switch field {
			case "field", "expected_fingerprint":
			case "value":
				if json.Unmarshal(value, &in.Value) != nil || in.Value == nil {
					response(w, 400, -1, "string oauth secret required", nil)
					return true
				}
			case "clear":
				if json.Unmarshal(value, &in.Clear) != nil {
					response(w, 400, -1, "boolean clear required", nil)
					return true
				}
			default:
				response(w, 400, -1, "unsupported oauth credential field", nil)
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
		if errors.Is(err, identity.ErrAdminSession) {
			status = 403
		} else if errors.Is(err, connectconfig.ErrConflict) {
			status = 409
		} else if errors.Is(err, connectconfig.ErrInvalid) {
			status = 422
		}
		response(w, status, -1, "oauth credential operation could not be confirmed; reload before retrying", nil)
	} else {
		response(w, 200, 0, "", out)
	}
	return true
}
