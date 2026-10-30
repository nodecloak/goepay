package server

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/smsgateway134"
	"goepay/internal/verifycode134"
)

// publicSMSCode134 is the SMS half of the original register / find-password /
// login verification-code flow (VerifyCode::send_code, type=1 phone): send a
// phone code. It is INERT unless an admin enabled the SMS gateway with an active
// provider — with the gateway off (the default) it answers 503 and issues
// nothing, so today's behaviour and every existing test are unchanged.
//
// Order matches the original: check caps + generate, send via the active
// provider, persist the code only on a successful send (IssueWithSender). An
// unconfirmed provider reply is never treated as sent.
func (s *Server) publicSMSCode134(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/console/v1/public/sms-code" {
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "POST" {
		w.Header().Set("Allow", "POST")
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	if s.Pool == nil || len(s.CheckoutKey) != 32 {
		response(w, 503, -1, "sms verification unavailable", nil)
		return true
	}
	var in struct {
		Purpose string `json:"purpose"`
		Phone   string `json:"phone"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil {
		response(w, 400, -1, "invalid JSON input", nil)
		return true
	}
	scene := ""
	switch in.Purpose {
	case "register":
		scene = "reg"
	case "recover":
		scene = "find"
	case "login":
		// 验证码登录: epay_pro's auth/sendcode type=login. Same SMS gateway, same
		// per-recipient/IP caps; inert while the gateway is off, exactly like the
		// register/recover scenes above. The code is consumed by the login handler
		// (verifycode134 scene "login"), never by this send path.
		scene = "login"
	default:
		response(w, 400, -1, "unknown purpose", nil)
		return true
	}
	if !validTestPhone(in.Phone) {
		response(w, 400, -1, "invalid phone", nil)
		return true
	}
	// verifytype (商户相关 → 用户验证方式) restricts the registration channel:
	// "0" (邮箱验证) forbids phone registration, so the register SMS send must
	// refuse before any code is sent. "1" and no row allow it. Recovery and
	// login sends are never restricted. Treated as a closed flow, mirroring the
	// gateway-off 503 style the handler already uses.
	if scene == "reg" {
		var value string
		e := s.Pool.QueryRow(r.Context(), `SELECT COALESCE(body->>'value','') FROM backoffice_records
	 WHERE resource='settings' AND body->>'key'='verifytype' ORDER BY id LIMIT 1`).Scan(&value)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			response(w, 503, -1, "sms verification unavailable", nil)
			return true
		}
		if value == "0" {
			response(w, 503, -1, "sms verification is not enabled", nil)
			return true
		}
	}
	source, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		source = r.RemoteAddr
	}
	if net.ParseIP(source) == nil {
		source = ""
	}
	store := smsgateway134.New(s.Pool, s.CheckoutKey)
	sender, e := s.smsActiveSender(r.Context(), store)
	if e != nil {
		// Gateway off / unmanaged / unconfigured: inert, nothing issued.
		response(w, 503, -1, "sms verification is not enabled", nil)
		return true
	}
	codes := verifycode134.New(s.Pool)
	e = codes.IssueWithSender(r.Context(), verifycode134.Issue{Scene: scene, Type: 1, Recipient: in.Phone, IP: source},
		func(code string) error {
			sendCtx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
			defer cancel()
			return sender.Send(sendCtx, in.Phone, code)
		})
	switch {
	case e == nil:
		response(w, 202, 0, "verification requested", nil)
	case errors.Is(e, verifycode134.ErrTooSoon):
		response(w, 429, -1, "两次发送短信之间需要相隔60秒", nil)
	case errors.Is(e, verifycode134.ErrRecipLim):
		response(w, 429, -1, "该手机号码发送次数过多", nil)
	case errors.Is(e, verifycode134.ErrIPLim):
		response(w, 429, -1, "你今天发送次数过多，请稍后再试", nil)
	default:
		// A send failure / unconfirmed reply: nothing was persisted.
		response(w, 503, -1, "短信发送未确认，请稍后重试", nil)
	}
	return true
}
