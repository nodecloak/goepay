package server

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
)

var errCurrentPassword = errors.New("current password rejected")

// merchantProfilePhonePattern mirrors the admin merchant-directory contact rule
// (internal/backoffice/merchant_contacts.go) so a merchant editing their own
// contact phone is held to the same shape the console enforces for admins.
var merchantProfilePhonePattern = regexp.MustCompile(`^\+?[0-9]{5,20}$`)

func (s *Server) account(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) < 2 || parts[1] != "account" {
		return false
	}
	if role != "merchant" && !(role == "admin" && len(parts) == 3 && parts[2] == "password") {
		response(w, 403, -1, "merchant account required", nil)
		return true
	}
	if len(parts) >= 3 && parts[2] == "email" {
		return s.accountEmail197(w, r, session, parts)
	}
	if len(parts) < 3 || len(parts) > 4 {
		response(w, 404, -1, "not found", nil)
		return true
	}
	mid := session.User.ID
	switch parts[2] {
	case "event-subscription":
		return s.accountEvents201(w, r, session, parts)
	case "buyer-fees":
		return s.accountBuyerFees104(w, r, session, parts)
	case "api-switches":
		return s.accountAPISwitches(w, r, session, parts)
	case "personal-settings":
		return s.accountPersonalSettings(w, r, session, parts)
	case "signature-policy":
		return s.accountSignaturePolicy(w, r, session, parts)
	case "login-log":
		return s.accountLoginLog(w, r, session, parts)
	case "rsa-key":
		return s.accountRSA(w, r, session, parts)
	case "profile":
		if r.Method == "GET" {
			profile, e := s.readMerchantProfile(r.Context(), mid, session.User.Username)
			if e != nil {
				s.fail(w, e)
				return true
			}
			response(w, 200, 0, "", profile)
			return true
		}
		if r.Method == "PATCH" {
			var in struct {
				Name  string `json:"name"`
				Email string `json:"email"`
				Phone string `json:"phone"`
			}
			if !decode(w, r, &in) {
				return true
			}
			in.Name = strings.TrimSpace(in.Name)
			in.Email = strings.TrimSpace(in.Email)
			in.Phone = strings.TrimSpace(in.Phone)
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
			if in.Phone != "" && !merchantProfilePhonePattern.MatchString(in.Phone) {
				response(w, 422, -1, "invalid phone", nil)
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
			var verified string
			e = tx.QueryRow(r.Context(), "SELECT email FROM enrollment_emails WHERE merchant_id=$1", mid).Scan(&verified)
			if e != nil && !errors.Is(e, pgx.ErrNoRows) {
				s.fail(w, e)
				return true
			}
			if verified != "" {
				if strings.ToLower(in.Email) != verified {
					response(w, 409, -1, "verify both email addresses to change the binding", nil)
					return true
				}
				in.Email = verified
			}
			_, e = tx.Exec(r.Context(), "UPDATE merchants SET name=$2,profile=profile||jsonb_build_object('email',$3::text,'phone',$4::text) WHERE id=$1 AND enabled", mid, in.Name, in.Email, in.Phone)
			if e == nil {
				_, e = tx.Exec(r.Context(), "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('merchant',$1,$2,'profile',$1,'update','{\"fields\":[\"name\",\"email\",\"phone\"]}')", mid, session.User.Username)
			}
			if e == nil {
				e = tx.Commit(r.Context())
			}
			if e != nil {
				s.fail(w, e)
				return true
			}
			response(w, 200, 0, "", map[string]any{"name": in.Name, "email": in.Email, "phone": in.Phone})
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
		return s.accountPassword199(w, r, role, session, parts)
	case "api-key":
		return s.accountMD5Rotation199(w, r, session, parts)
	}
	response(w, 405, -1, "method not allowed", nil)
	return true
}
