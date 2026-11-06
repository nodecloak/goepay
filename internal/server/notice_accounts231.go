package server

import (
	"encoding/json"
	"errors"
	"goepay/internal/identity"
	"goepay/internal/noticeconfig"
	"io"
	"net/http"
	"time"
)

func (s *Server) noticeAccounts231(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) < 4 || parts[1] != "settings" || parts[2] != "notice-accounts" {
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
	store, e := noticeconfig.New(s.Pool, s.CheckoutKey)
	if e != nil {
		response(w, 503, -1, "notification accounts unavailable", nil)
		return true
	}
	a := noticeconfig.Actor{User: session.User, Token: token, CSRF: session.CSRF}
	var out any
	if len(parts) == 4 {
		switch r.Method {
		case "GET":
			out, e = store.Read(r.Context(), a, parts[3])
		case "PUT":
			var fields map[string]json.RawMessage
			if !decodeFundsJSON(w, r, &fields) {
				return true
			}
			keys := []string{"expected_revision", "enabled", "credential_action", "app_id", "secret", "phone", "token_source"}
			if len(fields) != len(keys) {
				response(w, 400, -1, "complete explicit notification account required", nil)
				return true
			}
			for _, k := range keys {
				v, ok := fields[k]
				if !ok || string(v) == "null" {
					response(w, 400, -1, "complete explicit notification account required", nil)
					return true
				}
			}
			raw, _ := json.Marshal(fields)
			var in noticeconfig.Input
			if json.Unmarshal(raw, &in) != nil {
				response(w, 400, -1, "invalid notification account types", nil)
				return true
			}
			out, e = store.Update(r.Context(), a, parts[3], in)
		default:
			w.Header().Set("Allow", "GET, PUT")
			response(w, 405, -1, "method not allowed", nil)
			return true
		}
	} else if parts[3] == "qqbot" && len(parts) == 5 && parts[4] == "binding" && (r.Method == "POST" || r.Method == "DELETE") {
		var fields map[string]json.RawMessage
		if !decodeFundsJSON(w, r, &fields) {
			return true
		}
		var revision struct {
			Revision int64 `json:"expected_revision,string"`
		}
		raw, _ := json.Marshal(fields)
		if len(fields) != 1 || string(fields["expected_revision"]) == "null" || json.Unmarshal(raw, &revision) != nil || revision.Revision < 1 {
			response(w, 400, -1, "explicit current revision required", nil)
			return true
		}
		if r.Method == "POST" {
			out, e = store.StartBinding(r.Context(), a, revision.Revision)
		} else {
			out, e = store.Unbind(r.Context(), a, revision.Revision)
		}
	} else if parts[3] == "qqbot" && len(parts) == 6 && parts[4] == "binding" && r.Method == "DELETE" {
		e = store.CancelBinding(r.Context(), a, parts[5])
		out = map[string]bool{"cancelled": e == nil}
	} else {
		response(w, 404, -1, "unknown notification operation", nil)
		return true
	}
	if e != nil {
		status := 503
		if errors.Is(e, noticeconfig.ErrInvalid) {
			status = 422
		} else if errors.Is(e, noticeconfig.ErrConflict) {
			status = 409
		} else if errors.Is(e, identity.ErrAdminSession) {
			status = 403
		}
		response(w, status, -1, "配置或绑定结果未确认，请重新读取状态；不要自动重试", nil)
	} else {
		response(w, 200, 0, "", out)
	}
	return true
}
func (s *Server) qqNoticeCallback231(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/payment/qqbot/webhook" {
		return false
	}
	identityHeaders(w)
	if r.Method != "POST" {
		w.Header().Set("Allow", "POST")
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	// Administrator scope is fixed by the path; a body/query cannot select an
	// arbitrary merchant or convert an unsolicited message into a binding.
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "unsupported binding scope", nil)
		return true
	}
	store, e := noticeconfig.New(s.Pool, s.CheckoutKey)
	if e != nil {
		response(w, 503, -1, "binding unavailable", nil)
		return true
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, 65537))
	if e != nil || len(raw) > 65536 {
		response(w, 400, -1, "invalid callback", nil)
		return true
	}
	out, e := store.QQCallback(r.Context(), r.Header.Get("X-Bot-Appid"), r.Header.Get("X-Signature-Timestamp"), r.Header.Get("X-Signature-Ed25519"), raw, time.Now())
	if e != nil {
		response(w, 400, -1, "callback could not be verified", nil)
	} else {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(out)
	}
	return true
}
