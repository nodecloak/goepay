package server

import (
	"errors"
	"net"
	"net/http"

	"goepay/internal/enrollment"
	"goepay/internal/identity"
)

func email197Error(w http.ResponseWriter, e error) {
	status, message := 503, "email verification unavailable; query the original flow before retrying"
	switch {
	case errors.Is(e, enrollment.ErrEmailActor):
		status, message = 403, "original merchant session required"
	case errors.Is(e, enrollment.ErrEmailPassword):
		status, message = 422, "当前登录密码不正确，请重试"
	case errors.Is(e, enrollment.ErrLimited):
		status, message = 429, "verification rate limit exceeded"
	case errors.Is(e, enrollment.ErrCode):
		status, message = 422, "invalid or expired verification"
	case errors.Is(e, enrollment.ErrInvalid):
		status, message = 422, "invalid email binding input"
	case errors.Is(e, enrollment.ErrConflict):
		status, message = 409, "email binding changed or flow cannot perform this action"
	}
	response(w, status, -1, message, nil)
}
func (s *Server) accountEmail197(w http.ResponseWriter, r *http.Request, session identity.Session, parts []string) bool {
	identityHeaders(w)
	original, ok := merchantRSAActor(r, session)
	if !ok {
		email197Error(w, enrollment.ErrEmailActor)
		return true
	}
	configured, ok := s.Enrollment.(interface{ AccountVerification197() *enrollment.Service })
	if !ok || configured.AccountVerification197() == nil {
		email197Error(w, enrollment.ErrUnavailable)
		return true
	}
	service := configured.AccountVerification197()
	source, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		source = r.RemoteAddr
	}
	a := enrollment.EmailActor197{User: original.User, Token: original.Token, CSRF: original.CSRF, Source: source}
	write := func(v any, e error) {
		if e != nil {
			email197Error(w, e)
		} else {
			response(w, 200, 0, "", v)
		}
	}
	if len(parts) == 3 {
		switch r.Method {
		case "GET":
			v, e := service.EmailMetadata197(r.Context(), a)
			write(v, e)
		case "POST":
			in, ok := identityJSON(w, r, "flow_id", "expected_fingerprint", "current_password")
			if !ok {
				return true
			}
			if len(in) != 3 {
				email197Error(w, enrollment.ErrInvalid)
				return true
			}
			if !claimLoginVerification() {
				email197Error(w, enrollment.ErrLimited)
				return true
			}
			defer releaseLoginVerification()
			v, e := service.BeginEmail197(r.Context(), a, in["flow_id"], in["expected_fingerprint"], in["current_password"])
			write(v, e)
		default:
			response(w, 405, -1, "method not allowed", nil)
		}
		return true
	}
	if len(parts) == 4 && (r.Method == "GET" || r.Method == "DELETE") {
		v, e := service.GetEmail197(r.Context(), a, parts[3], r.Method == "DELETE")
		write(v, e)
		return true
	}
	if len(parts) != 5 {
		response(w, 404, -1, "not found", nil)
		return true
	}
	if r.Method != "POST" {
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	switch parts[4] {
	case "old-code":
		in, ok := identityJSON(w, r, "send_id")
		if !ok {
			return true
		}
		if len(in) > 1 {
			email197Error(w, enrollment.ErrInvalid)
			return true
		}
		v, e := service.SendEmail197(r.Context(), a, parts[3], "", in["send_id"], true)
		write(v, e)
	case "new-code":
		in, ok := identityJSON(w, r, "email", "send_id")
		if !ok {
			return true
		}
		if len(in) < 1 || in["email"] == "" {
			email197Error(w, enrollment.ErrInvalid)
			return true
		}
		v, e := service.SendEmail197(r.Context(), a, parts[3], in["email"], in["send_id"], false)
		write(v, e)
	case "old-verify", "complete":
		in, ok := identityJSON(w, r, "code")
		if !ok {
			return true
		}
		if len(in) != 1 {
			email197Error(w, enrollment.ErrInvalid)
			return true
		}
		v, e := service.VerifyEmail197(r.Context(), a, parts[3], in["code"], parts[4] == "complete")
		write(v, e)
	default:
		response(w, 404, -1, "not found", nil)
	}
	return true
}
