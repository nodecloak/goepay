package server

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"path"
	"strings"
	"unicode/utf8"

	"goepay/internal/identity"
)

// strictFundsJSON preserves the exact body passed to the authoritative action
// resolver, rejecting duplicate keys at every depth before business decoding.
func strictFundsJSON(raw []byte, out any) error {
	if len(raw) == 0 || len(raw) > 131072 || !utf8.Valid(raw) {
		return identity.ErrFundsInvalid
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return identity.ErrFundsInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	tokens := 0
	var value func(int) error
	value = func(depth int) error {
		tokens++
		if depth > 24 || tokens > 10000 {
			return identity.ErrFundsInvalid
		}
		t, e := d.Token()
		if e != nil {
			return identity.ErrFundsInvalid
		}
		if delim, ok := t.(json.Delim); ok {
			switch delim {
			case '{':
				seen := map[string]bool{}
				for d.More() {
					k, e := d.Token()
					key, ok := k.(string)
					if e != nil || !ok || seen[key] {
						return identity.ErrFundsInvalid
					}
					seen[key] = true
					if e = value(depth + 1); e != nil {
						return e
					}
				}
				end, e := d.Token()
				if e != nil || end != json.Delim('}') {
					return identity.ErrFundsInvalid
				}
			case '[':
				for d.More() {
					if e = value(depth + 1); e != nil {
						return e
					}
				}
				end, e := d.Token()
				if e != nil || end != json.Delim(']') {
					return identity.ErrFundsInvalid
				}
			default:
				return identity.ErrFundsInvalid
			}
		}
		return nil
	}
	if e := value(0); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return identity.ErrFundsInvalid
	}
	if e := validateFundsJSONFields(raw, out); e != nil {
		return e
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(out); e != nil {
		return identity.ErrFundsInvalid
	}
	return nil
}
func decodeFundsJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	media, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || media != "application/json" {
		response(w, 415, -1, "application/json required", nil)
		return false
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, 131073))
	if e != nil || strictFundsJSON(raw, out) != nil {
		response(w, 400, -1, "invalid or ambiguous JSON", nil)
		return false
	}
	return true
}
func fundsHTTPError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, identity.ErrFundsInvalid):
		response(w, 422, -1, "invalid funds authorization input", nil)
	case errors.Is(e, identity.ErrFundsRateLimited):
		w.Header().Set("Retry-After", "900")
		response(w, 429, -1, "too many funds verification attempts", nil)
	case errors.Is(e, identity.ErrFundsBusy):
		response(w, 503, -1, "funds verification capacity unavailable", nil)
	case errors.Is(e, identity.ErrFundsNotConfigured):
		response(w, 428, -1, "configure your funds password first", map[string]string{"error": "FUNDS_PASSWORD_NOT_CONFIGURED"})
	case errors.Is(e, identity.ErrFundsAuthentication):
		response(w, 403, -1, "funds authentication failed", map[string]string{"error": "FUNDS_AUTHENTICATION_FAILED"})
	case errors.Is(e, identity.ErrFundsAuthorization):
		response(w, 428, -1, "fresh action authorization required", map[string]string{"error": "PAYMENT_AUTH_REQUIRED"})
	default:
		response(w, 503, -1, "funds authorization unavailable", nil)
	}
}
func isFundsAuthorizationError(e error) bool {
	return errors.Is(e, identity.ErrFundsAuthorization) || errors.Is(e, identity.ErrFundsAuthentication) || errors.Is(e, identity.ErrFundsNotConfigured) || errors.Is(e, identity.ErrFundsRateLimited) || errors.Is(e, identity.ErrFundsBusy) || errors.Is(e, identity.ErrFundsInvalid)
}

// Called after ordinary console auth/permission checks. These endpoints also
// enforce CSRF locally and require the original durable administrator session.
func (s *Server) fundsSecurity(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, sessionToken string) bool {
	if len(parts) < 3 || parts[1] != "security" || (parts[2] != "payment-password" && parts[2] != "funds-authorizations") {
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	if role != "admin" || session.User.Role != "admin" || session.Issuer != nil {
		response(w, 403, -1, "original administrator session required", nil)
		return true
	}
	if s.Pool == nil {
		response(w, 503, -1, "funds authorization unavailable", nil)
		return true
	}
	if r.Method != "GET" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(session.CSRF)) != 1 {
		response(w, 403, -1, "invalid CSRF token", nil)
		return true
	}
	ip, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		ip = r.RemoteAddr
	}
	actor := identity.FundsActor{User: session.User, SessionToken: sessionToken, ClientIP: ip, Delegated: session.Issuer != nil}
	f := identity.NewFundsAuthorization(s.Pool, s.MFA)
	if parts[2] == "payment-password" && len(parts) == 3 {
		switch r.Method {
		case "GET":
			out, e := f.Status(r.Context(), actor)
			if e != nil {
				fundsHTTPError(w, e)
			} else {
				response(w, 200, 0, "", out)
			}
		case "PUT":
			var in struct {
				CurrentPassword        string `json:"current_password"`
				CurrentPaymentPassword string `json:"current_payment_password"`
				NewPaymentPassword     string `json:"new_payment_password"`
				Code                   string `json:"code"`
			}
			if !decodeFundsJSON(w, r, &in) {
				return true
			}
			if e = f.SetPassword(r.Context(), actor, in.CurrentPassword, in.CurrentPaymentPassword, in.NewPaymentPassword, in.Code); e != nil {
				fundsHTTPError(w, e)
			} else {
				response(w, 200, 0, "funds password updated; previous funds authorizations revoked", nil)
			}
		default:
			response(w, 405, -1, "GET or PUT required", nil)
		}
		return true
	}
	if parts[2] != "funds-authorizations" || len(parts) != 4 {
		response(w, 404, -1, "funds security operation not found", nil)
		return true
	}
	if r.Method != "POST" {
		response(w, 405, -1, "POST required", nil)
		return true
	}
	switch parts[3] {
	case "challenges":
		var in struct {
			Target string          `json:"target"`
			Body   json.RawMessage `json:"body"`
		}
		if !decodeFundsJSON(w, r, &in) {
			return true
		}
		// The actual action allowlist and all DB-derived snapshots belong to the
		// shared business resolver. The browser never supplies a trusted action hash.
		target := in.Target
		if !strings.HasPrefix(target, "/console/v1/admin/") || strings.ContainsAny(target, "?%#\\\x00\r\n") || path.Clean(target) != target || len(target) > 512 || len(in.Body) == 0 || bytes.TrimSpace(in.Body)[0] != '{' {
			response(w, 422, -1, "specific administrator POST target and object body required", nil)
			return true
		}
		if s.FundsActionResolver == nil {
			response(w, 503, -1, "funds action resolver unavailable", nil)
			return true
		}
		definition, protected := fundsTarget(target)
		if (strings.HasPrefix(target, "/console/v1/admin/referral-policies/") || strings.HasPrefix(target, "/console/v1/admin/settlement-plans/") || protected && definition.guard == "income-hold-adopt") && actor.User.AdminClass != "super" {
			response(w, 403, -1, "super administrator required for financial policy", nil)
			return true
		}
		action, summary, e := s.FundsActionResolver(r.Context(), s.Pool, target, in.Body)
		if e != nil {
			s.fail(w, e)
			return true
		}
		out, e := f.Prepare(r.Context(), actor, action)
		if e != nil {
			fundsHTTPError(w, e)
		} else {
			response(w, 201, 0, "review this exact action before authorization", map[string]any{"challenge_id": out.ChallengeID, "expires_at": out.ExpiresAt, "summary": summary})
		}
	case "confirm":
		var in struct {
			ChallengeID     string `json:"challenge_id"`
			PaymentPassword string `json:"payment_password"`
		}
		if !decodeFundsJSON(w, r, &in) {
			return true
		}
		out, e := f.Authorize(r.Context(), actor, in.ChallengeID, in.PaymentPassword)
		if e != nil {
			fundsHTTPError(w, e)
		} else {
			response(w, 200, 0, "authorization applies only to the previewed action", out)
		}
	default:
		response(w, 404, -1, "funds security operation not found", nil)
	}
	return true
}
