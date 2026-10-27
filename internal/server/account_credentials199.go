package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

var errCredentialActor199 = errors.New("original durable account session required")
var errCredentialUnset199 = errors.New("existing login password required")

type credentialActor199 struct {
	user        identity.User
	token, csrf string
}

type credentialAccount199 struct {
	hash   string
	expiry time.Time
}

func originalCredentialActor199(r *http.Request, role string, session identity.Session) (credentialActor199, error) {
	a := credentialActor199{user: session.User, csrf: session.CSRF}
	var ok bool
	a.token, ok = identityCookie(r, "goepay_"+role)
	if !ok || (role != "merchant" && role != "admin") || a.user.Role != role || a.user.ID <= 0 || a.user.AuthVersion <= 0 || a.user.Username == "" || session.Issuer != nil || len(a.token) != 43 || len(a.csrf) != 43 || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(a.csrf)) != 1 {
		return credentialActor199{}, errCredentialActor199
	}
	return a, nil
}

// Acquire every account write lock before the session locks. The MD5 store
// already owns the merchant lock; acquiring it again in this transaction keeps
// the same merchant -> account -> session order for both credential operations.
func (s *Server) credentialAccountTx199(ctx context.Context, tx pgx.Tx, a credentialActor199) (credentialAccount199, error) {
	var out credentialAccount199
	if a.user.Role == "merchant" {
		var enabled bool
		if e := tx.QueryRow(ctx, `SELECT enabled FROM merchants WHERE id=$1 FOR UPDATE`, a.user.ID).Scan(&enabled); e != nil {
			if errors.Is(e, pgx.ErrNoRows) {
				return out, errCredentialActor199
			}
			return out, e
		} else if !enabled {
			return out, errCredentialActor199
		}
	}
	var id, version int64
	var enabled bool
	e := tx.QueryRow(ctx, `SELECT subject_id,auth_version,enabled,password_hash FROM console_users WHERE username=$1 AND role=$2 FOR UPDATE`, a.user.Username, a.user.Role).Scan(&id, &version, &enabled, &out.hash)
	if errors.Is(e, pgx.ErrNoRows) || e == nil && (!enabled || id != a.user.ID || version != a.user.AuthVersion) {
		return out, errCredentialActor199
	}
	if e != nil {
		return out, e
	}
	if a.user.Role == "merchant" {
		e = identity.CheckOriginalMerchantSessionTx(ctx, tx, a.user, a.token, a.csrf)
	} else {
		e = identity.CheckOriginalAdminSessionTx(ctx, tx, a.user, a.token, false)
	}
	if errors.Is(e, identity.ErrOriginalMerchantSession) || errors.Is(e, identity.ErrAdminSession) {
		return out, errCredentialActor199
	}
	if e != nil {
		return out, e
	}
	// The durable actor helper owns the original session's shared row lock. Read
	// its actual expiry and CSRF after those locks, including for administrators.
	h := sha256.Sum256([]byte(a.token))
	var csrf string
	e = tx.QueryRow(ctx, `SELECT csrf,expires_at FROM console_sessions WHERE token_hash=$1 AND role=$2`, h[:], a.user.Role).Scan(&csrf, &out.expiry)
	if errors.Is(e, pgx.ErrNoRows) || e == nil && subtle.ConstantTimeCompare([]byte(csrf), []byte(a.csrf)) != 1 {
		return out, errCredentialActor199
	}
	if e != nil {
		return out, e
	}
	return out, credentialDeadline199(ctx, tx, out.expiry)
}

func credentialDeadline199(ctx context.Context, tx pgx.Tx, expiry time.Time) error {
	var now time.Time
	if e := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return e
	}
	if !now.Before(expiry) {
		return errCredentialActor199
	}
	return nil
}

func checkCredentialPassword199(hash, password string) error {
	if hash == "" {
		return errCredentialUnset199
	}
	if !identity.CheckPassword(hash, password) {
		return errCurrentPassword
	}
	return nil
}

func credentialError199(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, errCurrentPassword):
		response(w, 422, -1, "当前登录密码不正确，请重试", nil)
	case errors.Is(e, errCredentialUnset199):
		response(w, 422, -1, "尚未设置登录密码，请通过已验证的找回流程或联系管理员处理", nil)
	case errors.Is(e, errCredentialActor199), errors.Is(e, payment.ErrState):
		response(w, 403, -1, "original active account session required", nil)
	default:
		// A failed commit may have an unknown outcome. Do not return a generated
		// key or invite an automatic retry when the result cannot be confirmed.
		response(w, 503, -1, "凭据修改结果无法确认，请重新核对登录或接入信息，勿自动重试", nil)
	}
}

func (s *Server) credentialBudget199(w http.ResponseWriter, r *http.Request, a credentialActor199) bool {
	if s.Pool == nil {
		credentialError199(w, errors.New("persistent account store unavailable"))
		return false
	}
	// This statement commits independently of the business transaction. Wrong
	// passwords, late expiry, and an audit/delete rollback cannot undo attempts.
	allowed, e := s.allowLoginRequest(r, a.user.Role, a.user.Username)
	if e != nil {
		credentialError199(w, e)
		return false
	}
	if !allowed {
		w.Header().Set("Retry-After", "900")
		response(w, 429, -1, "凭据验证次数过多，请稍后重试", nil)
		return false
	}
	if !claimLoginVerification() {
		w.Header().Set("Retry-After", "1")
		response(w, 429, -1, "credential verification busy", nil)
		return false
	}
	return true
}

func (s *Server) accountPassword199(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	identityHeaders(w)
	if len(parts) != 3 {
		response(w, 404, -1, "not found", nil)
		return true
	}
	if r.Method != "POST" {
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	a, e := originalCredentialActor199(r, role, session)
	if e != nil {
		credentialError199(w, e)
		return true
	}
	in, ok := identityJSON(w, r, "current_password", "new_password")
	if !ok {
		return true
	}
	if len(in) != 2 || len(in["current_password"]) > 1024 || len(in["new_password"]) < 12 || len(in["new_password"]) > 1024 {
		response(w, 422, -1, "新密码须为 12 至 1024 字节，当前密码不得超过 1024 字节", nil)
		return true
	}
	if !s.credentialBudget199(w, r, a) {
		return true
	}
	defer releaseLoginVerification()
	if e = s.replaceOwnPassword199(r.Context(), a, in["current_password"], in["new_password"]); e != nil {
		credentialError199(w, e)
		return true
	}
	response(w, 200, 0, "密码已修改，请使用新密码重新登录", nil)
	return true
}

func (s *Server) replaceOwnPassword199(ctx context.Context, a credentialActor199, current, next string) error {
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	account, e := s.credentialAccountTx199(ctx, tx, a)
	if e != nil {
		return e
	}
	if e = checkCredentialPassword199(account.hash, current); e != nil {
		return e
	}
	if e = credentialDeadline199(ctx, tx, account.expiry); e != nil {
		return e
	}
	replacement, e := identity.HashPassword(next)
	if e != nil {
		return e
	}
	var version int64
	// The existing console_auth_version trigger increments a changed hash once.
	// Preserve its +1 result rather than adding a second version-changing UPDATE.
	e = tx.QueryRow(ctx, `UPDATE console_users SET password_hash=$4 WHERE username=$1 AND role=$2 AND subject_id=$3 AND auth_version=$5 AND enabled RETURNING auth_version`, a.user.Username, a.user.Role, a.user.ID, replacement, a.user.AuthVersion).Scan(&version)
	if e != nil {
		return e
	}
	if version != a.user.AuthVersion+1 {
		return errCredentialActor199
	}
	if _, e = tx.Exec(ctx, `DELETE FROM console_sessions WHERE username=$1 AND role=$2`, a.user.Username, a.user.Role); e != nil {
		return e
	}
	var remaining bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM console_sessions WHERE username=$1 AND role=$2)`, a.user.Username, a.user.Role).Scan(&remaining); e != nil {
		return e
	}
	if remaining {
		return errors.New("mandatory account session revocation incomplete")
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES($1,$2,$3,'account',$2,'password-change','{}')`, a.user.Role, a.user.ID, a.user.Username); e != nil {
		return e
	}
	// This check follows the last potentially blocking business write. Successful
	// self-revocation deletes the session, so use its locked original expiry.
	if e = credentialDeadline199(ctx, tx, account.expiry); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func (s *Server) accountMD5Rotation199(w http.ResponseWriter, r *http.Request, session identity.Session, parts []string) bool {
	identityHeaders(w)
	if len(parts) != 4 || parts[3] != "rotate" {
		response(w, 404, -1, "not found", nil)
		return true
	}
	if r.Method != "POST" {
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	a, e := originalCredentialActor199(r, "merchant", session)
	if e != nil {
		credentialError199(w, e)
		return true
	}
	in, ok := identityJSON(w, r, "current_password")
	if !ok {
		return true
	}
	if len(in) != 1 || len(in["current_password"]) > 1024 {
		response(w, 422, -1, "当前密码不得超过 1024 字节", nil)
		return true
	}
	if !s.credentialBudget199(w, r, a) {
		return true
	}
	defer releaseLoginVerification()
	if s.Store == nil {
		credentialError199(w, errors.New("payment credential store unavailable"))
		return true
	}
	var expiry time.Time
	key, e := s.Store.RotateMerchantAPIKeyAtomicFinal(r.Context(), a.user.ID, func(ctx context.Context, tx pgx.Tx) error {
		account, e := s.credentialAccountTx199(ctx, tx, a)
		if e != nil {
			return e
		}
		if e = checkCredentialPassword199(account.hash, in["current_password"]); e != nil {
			return e
		}
		expiry = account.expiry
		if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('merchant',$1,$2,'account',$1,'api-key-rotate','{}')`, a.user.ID, a.user.Username); e != nil {
			return e
		}
		return credentialDeadline199(ctx, tx, expiry)
	}, func(ctx context.Context, tx pgx.Tx) error {
		// This separate final hook follows the store's actual api_key UPDATE;
		// checking only the earlier authorization callback misses its lock waits.
		return credentialDeadline199(ctx, tx, expiry)
	})
	if e != nil {
		credentialError199(w, e)
		return true
	}
	response(w, 200, 0, "API key rotated; save this value now", map[string]any{"api_key": key})
	return true
}
