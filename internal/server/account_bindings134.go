package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
	"time"

	"goepay/internal/identity"
	"goepay/internal/smsgateway"
	"goepay/internal/verifycode"
)

// Third-party login providers whose binding state the merchant can see and
// unbind. Bind (linking a new identity) happens only through the authenticated
// OAuth bind step (item c); unbind here clears the stored uid.
var merchantConnectProviders = []string{"qq", "wx", "alipay", "telegram"}

func connectKnown(p string) bool {
	for _, v := range merchantConnectProviders {
		if v == p {
			return true
		}
	}
	return false
}
func connectUIDKey(p string) string { return p + "_uid" }

func maskBindMobile(phone string) string {
	if len(phone) == 11 {
		return phone[:3] + "****" + phone[7:]
	}
	if len(phone) > 4 {
		return phone[:2] + "****" + phone[len(phone)-2:]
	}
	if phone == "" {
		return ""
	}
	return "****"
}

// accountBindings serves the original editinfo.php bind surface:
//
//	GET  account/bindings                    -> checkBind (mobile + 3rd-party)
//	POST account/bindings/mobile             -> editBind by SMS code
//	DELETE account/bindings/connect/{prov}   -> unbind_connect
//
// Merchant session + CSRF required; every change is audited.
func (s *Server) accountBindings(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) < 2 || parts[1] != "account" || len(parts) < 3 || parts[2] != "bindings" {
		return false
	}
	identityHeaders(w)
	if role != "merchant" {
		response(w, 403, -1, "merchant account required", nil)
		return true
	}
	if s.Pool == nil {
		response(w, 503, -1, "account bindings unavailable", nil)
		return true
	}
	mid := session.User.ID
	csrfOK := func() bool {
		return session.Issuer == nil && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(session.CSRF)) == 1
	}

	// GET account/bindings -> checkBind
	if len(parts) == 3 {
		if r.Method != "GET" {
			response(w, 405, -1, "method not allowed", nil)
			return true
		}
		var profile map[string]any
		var enabled bool
		if e := s.Pool.QueryRow(r.Context(), "SELECT enabled,COALESCE(profile,'{}'::jsonb) FROM merchants WHERE id=$1", mid).Scan(&enabled, &profile); e != nil || !enabled {
			response(w, 403, -1, "merchant unavailable", nil)
			return true
		}
		mobile, _ := profile["bind_mobile"].(string)
		// Whether the SMS gateway can deliver a bind code right now (affects
		// whether the UI should offer "修改绑定" for mobile).
		smsReady := false
		if len(s.CheckoutKey) == 32 {
			if _, e := s.smsActiveSender(r.Context(), smsgateway.New(s.Pool, s.CheckoutKey)); e == nil {
				smsReady = true
			}
		}
		connects := make([]map[string]any, 0, len(merchantConnectProviders))
		for _, p := range merchantConnectProviders {
			uid, _ := profile[connectUIDKey(p)].(string)
			connects = append(connects, map[string]any{"provider": p, "bound": uid != ""})
		}
		response(w, 200, 0, "", map[string]any{
			"mobile":       maskBindMobile(mobile),
			"mobile_bound": mobile != "",
			"sms_ready":    smsReady,
			"third_party":  connects,
		})
		return true
	}

	// POST account/bindings/mobile/send-code -> send a bind-scene SMS code to the
	// phone the merchant is binding (authenticated; gateway-gated; CSRF).
	if len(parts) == 5 && parts[3] == "mobile" && parts[4] == "send-code" {
		if r.Method != "POST" {
			response(w, 405, -1, "method not allowed", nil)
			return true
		}
		if !csrfOK() {
			response(w, 403, -1, "original merchant session required", nil)
			return true
		}
		in, ok := identityJSON(w, r, "phone")
		if !ok {
			return true
		}
		phone := strings.TrimSpace(in["phone"])
		if !validTestPhone(phone) {
			response(w, 400, -1, "手机号码无效", nil)
			return true
		}
		if len(s.CheckoutKey) != 32 {
			response(w, 503, -1, "短信网关不可用", nil)
			return true
		}
		sender, e := s.smsActiveSender(r.Context(), smsgateway.New(s.Pool, s.CheckoutKey))
		if e != nil {
			response(w, 503, -1, "短信网关未开启", nil)
			return true
		}
		codes := verifycode.New(s.Pool)
		e = codes.IssueWithSender(r.Context(), verifycode.Issue{UID: mid, Scene: "bind", Type: 1, Recipient: phone}, func(code string) error {
			sendCtx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
			defer cancel()
			return sender.Send(sendCtx, phone, code)
		})
		switch {
		case e == nil:
			response(w, 202, 0, "验证码已发送", nil)
		case errors.Is(e, verifycode.ErrTooSoon):
			response(w, 429, -1, "两次发送短信之间需要相隔60秒", nil)
		case errors.Is(e, verifycode.ErrRecipLim):
			response(w, 429, -1, "该手机号码发送次数过多", nil)
		case errors.Is(e, verifycode.ErrIPLim):
			response(w, 429, -1, "你今天发送次数过多，请稍后再试", nil)
		default:
			response(w, 503, -1, "短信发送未确认，请稍后重试", nil)
		}
		return true
	}

	// POST account/bindings/mobile -> editBind by SMS code
	if len(parts) == 4 && parts[3] == "mobile" {
		if r.Method != "POST" {
			response(w, 405, -1, "method not allowed", nil)
			return true
		}
		if !csrfOK() {
			response(w, 403, -1, "original merchant session required", nil)
			return true
		}
		in, ok := identityJSON(w, r, "phone", "code")
		if !ok {
			return true
		}
		phone, code := strings.TrimSpace(in["phone"]), strings.TrimSpace(in["code"])
		if !validTestPhone(phone) || code == "" {
			response(w, 400, -1, "手机号码或验证码无效", nil)
			return true
		}
		// Verify the bind-scene SMS code, consume once.
		codes := verifycode.New(s.Pool)
		if e := codes.Verify(r.Context(), "bind", 1, phone, code, mid); e != nil {
			switch {
			case errors.Is(e, verifycode.ErrWrong):
				response(w, 422, -1, "验证码不正确", nil)
			case errors.Is(e, verifycode.ErrNoCode):
				response(w, 422, -1, "请重新获取验证码", nil)
			case errors.Is(e, verifycode.ErrExpired):
				response(w, 422, -1, "验证码已失效，请重新获取", nil)
			default:
				response(w, 503, -1, "验证码校验未确认", nil)
			}
			return true
		}
		tx, e := s.Pool.Begin(r.Context())
		if e != nil {
			s.fail(w, e)
			return true
		}
		defer tx.Rollback(r.Context())
		var enabled bool
		if e = tx.QueryRow(r.Context(), "SELECT enabled FROM merchants WHERE id=$1 FOR UPDATE", mid).Scan(&enabled); e != nil || !enabled {
			response(w, 403, -1, "merchant unavailable", nil)
			return true
		}
		if _, e = tx.Exec(r.Context(), "UPDATE merchants SET profile=COALESCE(profile,'{}'::jsonb)||jsonb_build_object('bind_mobile',$2::text) WHERE id=$1 AND enabled", mid, phone); e != nil {
			s.fail(w, e)
			return true
		}
		if _, e = tx.Exec(r.Context(), "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('merchant',$1,$2,'account-bindings',$1,'bind-mobile','{\"field\":\"bind_mobile\"}')", mid, session.User.Username); e != nil {
			s.fail(w, e)
			return true
		}
		if e = tx.Commit(r.Context()); e != nil {
			s.fail(w, e)
			return true
		}
		response(w, 200, 0, "", map[string]any{"mobile": maskBindMobile(phone), "mobile_bound": true})
		return true
	}

	// POST account/bindings/connect/{provider}/start -> 3rd-party BIND start.
	if len(parts) == 6 && parts[3] == "connect" && parts[5] == "start" {
		if !connectKnown(parts[4]) {
			response(w, 404, -1, "unknown provider", nil)
			return true
		}
		return s.accountBindingsConnectStart(w, r, session, parts[4])
	}

	// DELETE account/bindings/connect/{provider} -> unbind_connect
	if len(parts) == 5 && parts[3] == "connect" {
		if r.Method != "DELETE" {
			response(w, 405, -1, "method not allowed", nil)
			return true
		}
		if !csrfOK() {
			response(w, 403, -1, "original merchant session required", nil)
			return true
		}
		provider := parts[4]
		if !connectKnown(provider) {
			response(w, 404, -1, "unknown provider", nil)
			return true
		}
		tx, e := s.Pool.Begin(r.Context())
		if e != nil {
			s.fail(w, e)
			return true
		}
		defer tx.Rollback(r.Context())
		var enabled bool
		var profile map[string]any
		if e = tx.QueryRow(r.Context(), "SELECT enabled,COALESCE(profile,'{}'::jsonb) FROM merchants WHERE id=$1 FOR UPDATE", mid).Scan(&enabled, &profile); e != nil || !enabled {
			response(w, 403, -1, "merchant unavailable", nil)
			return true
		}
		if uid, _ := profile[connectUIDKey(provider)].(string); uid == "" {
			response(w, 409, -1, "该第三方账号未绑定", nil)
			return true
		}
		if _, e = tx.Exec(r.Context(), "UPDATE merchants SET profile=COALESCE(profile,'{}'::jsonb)-$2 WHERE id=$1 AND enabled", mid, connectUIDKey(provider)); e != nil {
			s.fail(w, e)
			return true
		}
		if _, e = tx.Exec(r.Context(), "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('merchant',$1,$2,'account-bindings',$1,'unbind-connect',jsonb_build_object('provider',$3::text))", mid, session.User.Username, provider); e != nil {
			s.fail(w, e)
			return true
		}
		if e = tx.Commit(r.Context()); e != nil {
			s.fail(w, e)
			return true
		}
		response(w, 200, 0, "", map[string]any{"provider": provider, "bound": false})
		return true
	}

	response(w, 404, -1, "not found", nil)
	return true
}
