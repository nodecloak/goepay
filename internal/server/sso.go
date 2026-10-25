package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"goepay/internal/identity"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

func ssoDigest(token string) []byte { h := sha256.Sum256([]byte(token)); return h[:] }

func (s *Server) ssoConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, issuerToken string) bool {
	if len(parts) < 2 || parts[1] != "sso" {
		return false
	}
	if role != "admin" || session.User.AdminClass != "super" {
		response(w, 403, -1, "super administrator required", nil)
		return true
	}
	if len(parts) != 3 || r.Method != "POST" {
		response(w, 405, -1, "POST required", nil)
		return true
	}
	if !s.Sessions.SupportsDelegation() {
		response(w, 503, -1, "persistent sessions required", nil)
		return true
	}
	if parts[2] != "grants" && parts[2] != "consume" {
		response(w, 404, -1, "SSO operation not found", nil)
		return true
	}
	tx, e := s.Pool.Begin(r.Context())
	if e != nil {
		s.fail(w, e)
		return true
	}
	defer tx.Rollback(r.Context())
	audit := func(mid int64, operation string) error {
		_, e := tx.Exec(r.Context(), "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'merchants',$3,$4,jsonb_build_object('target_merchant_id',$5::text))", session.User.ID, session.User.Username, mid, operation, fmt.Sprint(mid))
		return e
	}
	if parts[2] == "grants" {
		var in struct {
			MerchantID string `json:"merchant_id"`
		}
		if !decode(w, r, &in) {
			return true
		}
		mid, e := strconv.ParseInt(in.MerchantID, 10, 64)
		if e != nil || mid <= 0 {
			response(w, 422, -1, "valid merchant_id required", nil)
			return true
		}
		// Serializes bounded grant issuance and checks the current issuer version.
		var active bool
		e = tx.QueryRow(r.Context(), "SELECT enabled AND admin_class='super' AND auth_version=$3 FROM console_users WHERE username=$1 AND role='admin' AND subject_id=$2 FOR NO KEY UPDATE", session.User.Username, session.User.ID, session.User.AuthVersion).Scan(&active)
		if e != nil || !active {
			response(w, 401, -1, "administrator authority unavailable", nil)
			return true
		}
		var username string
		var version int64
		e = tx.QueryRow(r.Context(), "SELECT u.username,u.auth_version FROM console_users u JOIN merchants m ON m.id=u.subject_id AND m.enabled WHERE u.role='merchant' AND u.subject_id=$1 AND u.enabled ORDER BY u.username LIMIT 1", mid).Scan(&username, &version)
		if errors.Is(e, pgx.ErrNoRows) {
			response(w, 409, -1, "enabled merchant login account required", nil)
			return true
		}
		if e != nil {
			s.fail(w, e)
			return true
		}
		var pending int
		e = tx.QueryRow(r.Context(), "SELECT count(*) FROM console_sso_grants WHERE issuer_session_hash=$1 AND target_id=$2 AND consumed_at IS NULL AND expires_at>clock_timestamp()", ssoDigest(issuerToken), mid).Scan(&pending)
		if e != nil {
			s.fail(w, e)
			return true
		}
		if pending >= 5 {
			w.Header().Set("Retry-After", "60")
			response(w, 429, -1, "too many outstanding delegation grants", nil)
			return true
		}
		raw := make([]byte, 32)
		if _, e = rand.Read(raw); e != nil {
			s.fail(w, e)
			return true
		}
		grant := base64.RawURLEncoding.EncodeToString(raw)
		var expires time.Time
		e = tx.QueryRow(r.Context(), `INSERT INTO console_sso_grants(grant_hash,issuer_session_hash,issuer_username,issuer_id,issuer_auth_version,target_username,target_id,target_auth_version)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING expires_at`, ssoDigest(grant), ssoDigest(issuerToken), session.User.Username, session.User.ID, session.User.AuthVersion, username, mid, version).Scan(&expires)
		if e == nil {
			e = audit(mid, "sso-grant")
		}
		if e == nil {
			e = tx.Commit(r.Context())
		}
		if e != nil {
			s.fail(w, e)
			return true
		}
		response(w, 201, 0, "single-use grant; merchant cookie will be replaced on consumption", map[string]any{"grant": grant, "expires_at": expires, "merchant_id": fmt.Sprint(mid)})
		return true
	}
	var in struct {
		Grant string `json:"grant"`
	}
	if !decode(w, r, &in) {
		return true
	}
	if len(in.Grant) != 43 {
		response(w, 409, -1, "delegation grant unavailable", nil)
		return true
	}
	var target identity.User
	target.Role = "merchant"
	// Establish ownership of the row first. A clock predicate evaluated before a
	// row-lock wait is not a valid expiry check after that wait has completed.
	var locked bool
	e = tx.QueryRow(r.Context(), `SELECT true FROM console_sso_grants WHERE grant_hash=$1 AND issuer_session_hash=$2 AND issuer_username=$3 AND issuer_id=$4 AND issuer_auth_version=$5 FOR UPDATE`, ssoDigest(in.Grant), ssoDigest(issuerToken), session.User.Username, session.User.ID, session.User.AuthVersion).Scan(&locked)
	if errors.Is(e, pgx.ErrNoRows) {
		response(w, 409, -1, "delegation grant unavailable", nil)
		return true
	}
	if e != nil {
		s.fail(w, e)
		return true
	}
	e = tx.QueryRow(r.Context(), `UPDATE console_sso_grants SET consumed_at=clock_timestamp()
 WHERE grant_hash=$1 AND issuer_session_hash=$2 AND issuer_username=$3 AND issuer_id=$4 AND issuer_auth_version=$5
 AND consumed_at IS NULL AND expires_at>clock_timestamp()
 RETURNING target_username,target_id,target_auth_version`, ssoDigest(in.Grant), ssoDigest(issuerToken), session.User.Username, session.User.ID, session.User.AuthVersion).Scan(&target.Username, &target.ID, &target.AuthVersion)
	if errors.Is(e, pgx.ErrNoRows) {
		response(w, 409, -1, "delegation grant unavailable", nil)
		return true
	}
	if e != nil {
		s.fail(w, e)
		return true
	}
	token, csrf, expires, e := s.Sessions.CreateDelegated(r.Context(), tx, target, session.User, issuerToken)
	if errors.Is(e, identity.ErrDelegationUnavailable) {
		response(w, 409, -1, "delegation target is no longer available", nil)
		return true
	}
	if e == nil {
		e = audit(target.ID, "sso-consume")
	}
	if e == nil {
		e = tx.Commit(r.Context())
	}
	if e != nil {
		s.fail(w, e)
		return true
	}
	http.SetCookie(w, &http.Cookie{Name: "goepay_merchant", Value: token, Path: "/console/v1/merchant", MaxAge: 900, Expires: expires, HttpOnly: true, Secure: s.Sessions.Secure, SameSite: http.SameSiteStrictMode})
	response(w, 200, 0, "delegated merchant session created", map[string]any{"csrf_token": csrf, "merchant_id": fmt.Sprint(target.ID), "expires_at": expires})
	return true
}

// This records an attempted delegated action before any business I/O. Existing
// per-operation audits retain the effective merchant and actual outcome.
func (s *Server) auditDelegatedRequest(w http.ResponseWriter, r *http.Request, session identity.Session) bool {
	if session.Issuer == nil || r.Method == "GET" {
		return true
	}
	_, e := s.Pool.Exec(r.Context(), "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'delegated-sessions',$3,'delegated-request',jsonb_build_object('effective_merchant_id',$4::text,'path',$5::text))", session.Issuer.ID, session.Issuer.Username, session.User.ID, fmt.Sprint(session.User.ID), r.URL.Path)
	if e != nil {
		response(w, 503, -1, "delegated action audit unavailable", nil)
		return false
	}
	return true
}
