package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"time"

	smsadapter "goepay/internal/adapters/sms"
	"goepay/internal/identity"
	"goepay/internal/smsgateway134"
)

// newTestRequestID generates a RFC4122 v4 UUID matching the request_id CHECK on
// sms_test_requests134.
func newTestRequestID() (string, error) {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", e
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// smsAccounts134 serves the original SMS gateway settings (sms_api + provider
// keys) as a super-administrator console, modelled on the mail-channels page.
// Secrets are sealed at rest and never returned. A test send is committed as a
// request row before any upstream I/O, so an interrupted or duplicate test can
// never re-fire against the provider.
func (s *Server) smsAccounts134(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) < 3 || parts[1] != "settings" || parts[2] != "sms-channels" {
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
		response(w, 503, -1, "sms gateway unavailable", nil)
		return true
	}
	store := smsgateway134.New(s.Pool, s.CheckoutKey)
	actor := smsgateway134.Actor{User: session.User, Token: token, Delegated: session.Issuer != nil}
	fail := func(e error) {
		switch {
		case errors.Is(e, identity.ErrAdminSession):
			response(w, 403, -1, "original super administrator session required", nil)
		case errors.Is(e, smsgateway134.ErrConflict):
			response(w, 409, -1, "sms gateway changed; reload before confirming", nil)
		case errors.Is(e, smsgateway134.ErrInvalid):
			response(w, 422, -1, "invalid sms gateway fields", nil)
		default:
			response(w, 503, -1, "sms gateway source could not be confirmed", nil)
		}
	}
	// /settings/sms-channels/test (POST) — test send.
	if len(parts) == 4 && parts[3] == "test" {
		return s.smsTest134(w, r, store, actor, session)
	}
	if len(parts) != 3 {
		response(w, 404, -1, "not found", nil)
		return true
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
			Provider    *string `json:"provider"`
			Credentials *struct {
				AccessKeyID     *string `json:"access_key_id"`
				AccessKeySecret *string `json:"access_key_secret"`
				AppID           *string `json:"appid"`
				AppKey          *string `json:"appkey"`
				User            *string `json:"user"`
				Pass            *string `json:"pass"`
				Template        *string `json:"template"`
				Sign            *string `json:"sign"`
			} `json:"credentials"`
			Active   *string `json:"active"`
			Status   *int    `json:"status"`
			Expected *string `json:"expected_fingerprint"`
		}
		if !decodeFundsJSON(w, r, &input) {
			return true
		}
		if input.Status == nil || input.Expected == nil || !identityHex(*input.Expected) {
			response(w, 400, -1, "exact sms gateway fields and current confirmation required", nil)
			return true
		}
		in := smsgateway134.Input{Status: *input.Status, Expected: *input.Expected}
		if input.Active != nil {
			in.Active = *input.Active
		}
		if input.Provider != nil {
			in.Provider = *input.Provider
			if input.Credentials == nil {
				response(w, 400, -1, "provider credentials required", nil)
				return true
			}
			c := smsgateway134.Credentials{}
			str := func(p *string) string {
				if p == nil {
					return ""
				}
				return *p
			}
			c.AccessKeyID = str(input.Credentials.AccessKeyID)
			c.AccessKeySecret = str(input.Credentials.AccessKeySecret)
			c.AppID = str(input.Credentials.AppID)
			c.AppKey = str(input.Credentials.AppKey)
			c.User = str(input.Credentials.User)
			c.Pass = str(input.Credentials.Pass)
			c.Template = str(input.Credentials.Template)
			c.Sign = str(input.Credentials.Sign)
			in.Credentials = &c
		}
		v, e := store.Save(r.Context(), actor, in)
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

func (s *Server) smsTest134(w http.ResponseWriter, r *http.Request, store *smsgateway134.Store, actor smsgateway134.Actor, session identity.Session) bool {
	if r.Method != "POST" {
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	var input struct {
		Phone    string `json:"phone"`
		Expected string `json:"expected_fingerprint"`
	}
	if !decodeFundsJSON(w, r, &input) {
		return true
	}
	if !identityHex(input.Expected) || !validTestPhone(input.Phone) {
		response(w, 400, -1, "a valid test phone and current confirmation required", nil)
		return true
	}
	// Confirm the live fingerprint matches what the operator saw (CAS) and the
	// super-admin session, reusing the store Read (which re-checks both).
	v, e := store.Read(r.Context(), actor)
	if e != nil {
		if errors.Is(e, identity.ErrAdminSession) {
			response(w, 403, -1, "original super administrator session required", nil)
		} else {
			response(w, 503, -1, "sms gateway source could not be confirmed", nil)
		}
		return true
	}
	if v.Fingerprint != input.Expected {
		response(w, 409, -1, "sms gateway changed; reload before confirming", nil)
		return true
	}
	if v.Status != 1 || v.Active == "" {
		response(w, 422, -1, "enable the gateway with an active provider before a test send", nil)
		return true
	}
	requestID, e := newTestRequestID()
	if e != nil {
		response(w, 503, -1, "test request could not be prepared", nil)
		return true
	}
	recipientHash := sha256.Sum256([]byte("goepay/sms-test134-recipient:" + input.Phone))
	// Commit the test-request row before any upstream I/O.
	if _, e = s.Pool.Exec(r.Context(), `INSERT INTO sms_test_requests134(request_id,provider,actor_id,recipient_hash)VALUES($1,$2,$3,$4)`,
		requestID, v.Active, session.User.ID, recipientHash[:]); e != nil {
		response(w, 503, -1, "test request could not be recorded", nil)
		return true
	}
	// Resolve sealed active credentials and send via the matching adapter.
	status, reason := "failed", ""
	sender, e := s.smsActiveSender(r.Context(), store)
	if e == nil {
		send, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		err := sender.Send(send, input.Phone, "123456")
		cancel()
		switch {
		case err == nil:
			status = "accepted"
		case errors.Is(err, smsadapter.ErrRejected):
			status, reason = "failed", "provider rejected"
		default:
			status, reason = "failed", "unconfirmed"
		}
	} else {
		reason = "gateway unavailable"
	}
	if _, e = s.Pool.Exec(r.Context(), `UPDATE sms_test_requests134 SET status=$1,reason=$2,settled_at=clock_timestamp()WHERE request_id=$3 AND status='pending'`,
		status, reason, requestID); e != nil {
		// The row stays pending; the outcome above is still reported.
		_ = e
	}
	response(w, 200, 0, "", map[string]any{"request_id": requestID, "status": status, "reason": reason})
	return true
}

// smsActiveSender builds the adapter for the active provider from sealed
// credentials, inside a short read transaction.
func (s *Server) smsActiveSender(ctx context.Context, store *smsgateway134.Store) (smsadapter.Sender, error) {
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	provider, creds, e := store.ActiveCredentials(ctx, tx)
	if e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	switch provider {
	case "aliyun":
		return smsadapter.Aliyun{AccessKeyID: creds.AccessKeyID, AccessKeySecret: creds.AccessKeySecret, SignName: creds.Sign, TemplateCode: creds.Template}, nil
	case "qcloud":
		return smsadapter.Qcloud{AppID: creds.AppID, AppKey: creds.AppKey, TplID: creds.Template, Sign: creds.Sign}, nil
	case "smsbao":
		return smsadapter.SmsBao{User: creds.User, Pass: creds.Pass, Template: creds.Template, Sign: creds.Sign}, nil
	default:
		return nil, smsgateway134.ErrSource
	}
}

func validTestPhone(p string) bool {
	if len(p) < 5 || len(p) > 20 {
		return false
	}
	for _, r := range p {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
