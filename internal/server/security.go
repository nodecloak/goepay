package server

import (
	"context"
	"goepay/internal/backoffice"
	"goepay/internal/identity"
	"net/http"
	"time"
)

func (s *Server) checkMFA(ctx context.Context, username, role, code string) bool {
	// Even an instance missing its decryption key must not bypass enrolled MFA.
	var enabled bool
	if err := s.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM console_mfa WHERE username=$1 AND role=$2 AND secret IS NOT NULL)", username, role).Scan(&enabled); err != nil {
		return false
	}
	if !enabled {
		return true
	}
	return s.MFA != nil && s.MFA.Verify(ctx, username, role, code) == nil
}
func (s *Server) security(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) < 2 || parts[1] != "security" {
		return false
	}
	if len(parts) < 3 || parts[2] != "totp" || len(parts) > 4 {
		response(w, 404, -1, "security operation not found", nil)
		return true
	}
	if s.MFA == nil {
		response(w, 503, -1, "authenticator encryption unavailable", nil)
		return true
	}
	username := session.User.Username
	if r.Method == "GET" && len(parts) == 3 {
		enabled, err := s.MFA.Enabled(r.Context(), username, role)
		if err != nil {
			s.fail(w, err)
			return true
		}
		response(w, 200, 0, "", map[string]any{"enabled": enabled})
		return true
	}
	if r.Method != "POST" || len(parts) != 4 {
		response(w, 405, -1, "POST required", nil)
		return true
	}
	if !s.allowLogin(r.RemoteAddr, role+":mfa", username, time.Now()) {
		response(w, 429, -1, "too many authenticator attempts", nil)
		return true
	}
	var in struct {
		CurrentPassword string `json:"current_password"`
		Code            string `json:"code"`
	}
	if !decode(w, r, &in) {
		return true
	}
	action := parts[3]
	if action != "prepare" && action != "confirm" && action != "disable" {
		response(w, 404, -1, "security operation not found", nil)
		return true
	}
	// user_totp_open (default ON) gates merchant TOTP enrollment: when the
	// administrator turns it off, a merchant may no longer enroll, but may still
	// disable an existing authenticator so turning the feature off never locks
	// an enrolled merchant out. Administrator TOTP is not gated.
	if role == "merchant" && (action == "prepare" || action == "confirm") && s.Pool != nil {
		on, e := backoffice.New(s.Pool, s.Store).MerchantFeatureVisible(r.Context(), "user_totp_open")
		if e != nil {
			response(w, 503, -1, "merchant feature unavailable", nil)
			return true
		}
		if !on {
			response(w, 403, -1, "未开启登录二次验证功能", nil)
			return true
		}
	}
	if action != "confirm" {
		var hash string
		err := s.Pool.QueryRow(r.Context(), "SELECT password_hash FROM console_users WHERE username=$1 AND role=$2 AND enabled", username, role).Scan(&hash)
		if err != nil || !identity.CheckPassword(hash, in.CurrentPassword) {
			response(w, 401, -1, "invalid current password", nil)
			return true
		}
	}
	if action == "prepare" {
		enrollment, err := s.MFA.Prepare(r.Context(), username, role)
		if err != nil {
			response(w, 409, -1, "authenticator is already enabled or unavailable", nil)
			return true
		}
		response(w, 200, 0, "", enrollment)
		return true
	}
	var err error
	if action == "confirm" {
		err = s.MFA.Confirm(r.Context(), username, role, in.Code)
	} else {
		err = s.MFA.Disable(r.Context(), username, role, in.Code)
	}
	if err != nil {
		response(w, 401, -1, "invalid or reused authenticator code", nil)
		return true
	}
	s.Sessions.RevokeUser(username, role)
	response(w, 200, 0, "authenticator settings changed; sign in again", map[string]any{"enabled": action == "confirm"})
	return true
}
