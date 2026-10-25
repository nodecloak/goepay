package server

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/mail"
	"strings"
)

var errCurrentPassword = errors.New("current password rejected")

func (s *Server) account(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) < 2 || parts[1] != "account" {
		return false
	}
	if role != "merchant" && !(role == "admin" && len(parts) == 3 && parts[2] == "password") {
		response(w, 403, -1, "merchant account required", nil)
		return true
	}
	if len(parts) < 3 || len(parts) > 4 {
		response(w, 404, -1, "not found", nil)
		return true
	}
	mid := session.User.ID
	switch parts[2] {
	case "rsa-key":
		return s.accountRSA(w, r, session, parts)
	case "profile":
		if r.Method == "GET" {
			var name, email, pay string
			e := s.Pool.QueryRow(r.Context(), "SELECT name,COALESCE(profile->>'email',''),COALESCE(to_jsonb(merchants)->>'pay','1') FROM merchants WHERE id=$1", mid).Scan(&name, &email, &pay)
			if e != nil {
				s.fail(w, e)
				return true
			}
			response(w, 200, 0, "", map[string]any{"name": name, "email": email, "pay": pay})
			return true
		}
		if r.Method == "PATCH" {
			var in struct {
				Name  string `json:"name"`
				Email string `json:"email"`
			}
			if !decode(w, r, &in) {
				return true
			}
			in.Name = strings.TrimSpace(in.Name)
			in.Email = strings.TrimSpace(in.Email)
			if in.Name == "" || len(in.Name) > 128 || len(in.Email) > 254 {
				response(w, 422, -1, "invalid profile", nil)
				return true
			}
			if in.Email != "" {
				address, e := mail.ParseAddress(in.Email)
				if e != nil || address.Address != in.Email {
					response(w, 422, -1, "invalid email", nil)
					return true
				}
			}
			tx, e := s.Pool.Begin(r.Context())
			if e != nil {
				s.fail(w, e)
				return true
			}
			defer tx.Rollback(r.Context())
			_, e = tx.Exec(r.Context(), "UPDATE merchants SET name=$2,profile=profile||jsonb_build_object('email',$3::text) WHERE id=$1 AND enabled", mid, in.Name, in.Email)
			if e == nil {
				_, e = tx.Exec(r.Context(), "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('merchant',$1,$2,'profile',$1,'update','{\"fields\":[\"name\",\"email\"]}')", mid, session.User.Username)
			}
			if e == nil {
				e = tx.Commit(r.Context())
			}
			if e != nil {
				s.fail(w, e)
				return true
			}
			response(w, 200, 0, "", map[string]any{"name": in.Name, "email": in.Email})
			return true
		}
	case "api-info":
		if r.Method == "GET" {
			m, e := s.Store.Merchant(r.Context(), mid)
			if e != nil {
				s.fail(w, e)
				return true
			}
			masked := "********"
			if len(m.APIKey) >= 4 {
				masked += m.APIKey[len(m.APIKey)-4:]
			}
			platform := ""
			if s.PlatformKey != nil {
				der, e := x509.MarshalPKIXPublicKey(&s.PlatformKey.PublicKey)
				if e != nil {
					s.fail(w, e)
					return true
				}
				platform = string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
			}
			fingerprint := payment.MerchantRSAFingerprint(m.SignatureAlgorithm, m.RSAPublicKey)
			canRotate := false
			if actor, ok := merchantRSAActor(r, session); ok {
				if state, err := s.Store.MerchantRSAState(r.Context(), actor); err == nil {
					fingerprint = state.Fingerprint
					m.RSAPublicKey = state.PublicKey
					m.SignatureAlgorithm = state.Algorithm
					canRotate = true
				}
			}
			identityHeaders(w)
			response(w, 200, 0, "", map[string]any{"pid": m.ID, "algorithm": m.SignatureAlgorithm, "merchant_public_key": m.RSAPublicKey, "platform_public_key": platform, "api_key_masked": masked, "rsa_fingerprint": fingerprint, "can_rotate_rsa": canRotate})
			return true
		}
	case "password":
		if r.Method == "POST" {
			var in struct {
				CurrentPassword string `json:"current_password"`
				NewPassword     string `json:"new_password"`
			}
			if !decode(w, r, &in) {
				return true
			}
			if len(in.NewPassword) < 12 || len(in.NewPassword) > 1024 {
				response(w, 422, -1, "password must contain 12..1024 bytes", nil)
				return true
			}
			tx, e := s.Pool.Begin(r.Context())
			if e != nil {
				s.fail(w, e)
				return true
			}
			defer tx.Rollback(r.Context())
			var hash string
			e = tx.QueryRow(r.Context(), "SELECT password_hash FROM console_users WHERE username=$1 AND role=$2 AND subject_id=$3 AND enabled FOR UPDATE", session.User.Username, role, mid).Scan(&hash)
			if e != nil || !identity.CheckPassword(hash, in.CurrentPassword) {
				response(w, 401, -1, "invalid current password", nil)
				return true
			}
			replacement, e := identity.HashPassword(in.NewPassword)
			if e == nil {
				_, e = tx.Exec(r.Context(), "UPDATE console_users SET password_hash=$1 WHERE username=$2 AND role=$3", replacement, session.User.Username, role)
			}
			if e == nil {
				_, e = tx.Exec(r.Context(), "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES($1,$2,$3,'account',$2,'password-change','{}')", role, mid, session.User.Username)
			}
			if e == nil {
				e = tx.Commit(r.Context())
			}
			if e != nil {
				s.fail(w, e)
				return true
			}
			s.Sessions.RevokeUser(session.User.Username, role)
			response(w, 200, 0, "password changed; sign in again", nil)
			return true
		}
	case "api-key":
		if len(parts) == 4 && parts[3] == "rotate" && r.Method == "POST" {
			var in struct {
				CurrentPassword string `json:"current_password"`
			}
			if !decode(w, r, &in) {
				return true
			}
			b := make([]byte, 24)
			if _, e := rand.Read(b); e != nil {
				s.fail(w, e)
				return true
			}
			newKey := base64.RawURLEncoding.EncodeToString(b)
			e := s.Store.RotateMerchantAPIKeyAtomic(r.Context(), mid, newKey, func(ctx context.Context, tx pgx.Tx) error {
				var hash string
				if e := tx.QueryRow(ctx, "SELECT password_hash FROM console_users WHERE username=$1 AND role=$2 AND subject_id=$3 AND enabled FOR UPDATE", session.User.Username, role, mid).Scan(&hash); e != nil {
					return e
				}
				if !identity.CheckPassword(hash, in.CurrentPassword) {
					return errCurrentPassword
				}
				_, e := tx.Exec(ctx, "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES($1,$2,$3,'account',$2,'api-key-rotate','{}')", role, mid, session.User.Username)
				return e
			})
			if errors.Is(e, errCurrentPassword) {
				response(w, 401, -1, "invalid current password", nil)
				return true
			}
			if e != nil {
				s.fail(w, e)
				return true
			}
			response(w, 200, 0, "API key rotated; save this value now", map[string]any{"api_key": newKey})
			return true
		}
	}
	response(w, 405, -1, "method not allowed", nil)
	return true
}
