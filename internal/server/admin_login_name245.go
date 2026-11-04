package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"goepay/internal/identity"
)

var adminLoginName245 = regexp.MustCompile(`^[a-zA-Z0-9_.@-]{3,100}$`)
var errLoginNameConflict245 = errors.New("administrator login name already reserved")
var errLoginNameStale245 = errors.New("administrator login name changed")
var errLoginNameUnchanged245 = errors.New("administrator login name unchanged")

func loginNameError245(w http.ResponseWriter, e error) {
	var pe *pgconn.PgError
	switch {
	case errors.Is(e, errLoginNameConflict245), errors.As(e, &pe) && pe.Code == "23505":
		response(w, 409, -1, "该登录账号已被使用，请更换其他账号", nil)
	case errors.Is(e, errLoginNameStale245):
		response(w, 409, -1, "登录账号已变更，请刷新后重试", nil)
	case errors.Is(e, errLoginNameUnchanged245):
		response(w, 422, -1, "新登录账号与当前账号相同，无需修改", nil)
	default:
		credentialError199(w, e)
	}
}

func (s *Server) accountLoginName245(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	identityHeaders(w)
	if role != "admin" {
		response(w, 403, -1, "administrator account required", nil)
		return true
	}
	if len(parts) != 3 {
		response(w, 404, -1, "not found", nil)
		return true
	}
	if r.Method != "GET" && r.Method != "POST" {
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	var a credentialActor199
	var e error
	if r.Method == "POST" {
		a, e = originalCredentialActor199(r, role, session)
	} else {
		// Metadata GET follows the console's ordinary CSRF-free read contract.
		// The transaction below still proves the original durable session.
		var ok bool
		a = credentialActor199{user: session.User, csrf: session.CSRF}
		a.token, ok = identityCookie(r, "goepay_admin")
		if !ok || a.user.Role != "admin" || a.user.ID <= 0 || a.user.AuthVersion <= 0 || a.user.Username == "" || session.Issuer != nil || len(a.token) != 43 || len(a.csrf) != 43 {
			e = errCredentialActor199
		}
	}
	if e != nil {
		loginNameError245(w, e)
		return true
	}
	if r.Method == "GET" {
		if s.Pool == nil {
			credentialError199(w, errors.New("persistent account store unavailable"))
			return true
		}
		tx, err := s.Pool.Begin(r.Context())
		if err != nil {
			loginNameError245(w, err)
			return true
		}
		defer tx.Rollback(r.Context())
		_, err = s.credentialAccountTx199(r.Context(), tx, a)
		var username string
		if err == nil {
			err = tx.QueryRow(r.Context(), `SELECT COALESCE(login_name,username) FROM console_users WHERE username=$1 AND role='admin'`, a.user.Username).Scan(&username)
		}
		if err == nil {
			err = tx.Commit(r.Context())
		}
		if err != nil {
			loginNameError245(w, err)
			return true
		}
		response(w, 200, 0, "", map[string]string{"username": username, "auth_version": strconv.FormatInt(a.user.AuthVersion, 10)})
		return true
	}
	in, ok := identityJSON(w, r, "username", "current_password", "expected_username")
	if !ok {
		return true
	}
	if len(in) != 3 || !adminLoginName245.MatchString(in["username"]) || len(in["expected_username"]) == 0 || len(in["expected_username"]) > 100 || len(in["current_password"]) == 0 || len(in["current_password"]) > 1024 {
		response(w, 422, -1, "登录账号须为 3 至 100 位字母、数字或 _ . @ -，并填写当前登录密码", nil)
		return true
	}
	if !s.credentialBudget199(w, r, a) {
		return true
	}
	defer releaseLoginVerification()
	if e = s.replaceOwnLoginName245(r.Context(), a, in["current_password"], in["expected_username"], in["username"]); e != nil {
		loginNameError245(w, e)
		return true
	}
	response(w, 200, 0, "登录账号已修改，请使用新账号重新登录", map[string]any{"username": in["username"], "reauthenticate": true})
	return true
}

func (s *Server) replaceOwnLoginName245(ctx context.Context, a credentialActor199, password, expected, next string) error {
	if a.user.Role != "admin" {
		return errCredentialActor199
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	// Match administrator CRUD's write order so simultaneous account edits and
	// session revocation cannot acquire the administrator locks in reverse.
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(74973232)`); e != nil {
		return e
	}
	account, e := s.credentialAccountTx199(ctx, tx, a)
	if e != nil {
		return e
	}
	if e = checkCredentialPassword199(account.hash, password); e != nil {
		return e
	}
	var current string
	if e = tx.QueryRow(ctx, `SELECT COALESCE(login_name,username) FROM console_users WHERE username=$1 AND role='admin'`, a.user.Username).Scan(&current); e != nil {
		return e
	}
	if current != expected {
		return errLoginNameStale245
	}
	if next == current {
		return errLoginNameUnchanged245
	}
	var duplicate bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM console_users WHERE role='admin' AND username<>$1 AND (username=$2 OR COALESCE(login_name,username)=$2))`, a.user.Username, next).Scan(&duplicate); e != nil {
		return e
	}
	if duplicate {
		return errLoginNameConflict245
	}
	if e = credentialDeadline199(ctx, tx, account.expiry); e != nil {
		return e
	}
	var version int64
	e = tx.QueryRow(ctx, `UPDATE console_users SET login_name=NULLIF($4,username) WHERE username=$1 AND role='admin' AND subject_id=$2 AND auth_version=$3 AND enabled AND COALESCE(login_name,username)=$5 RETURNING auth_version`, a.user.Username, a.user.ID, a.user.AuthVersion, next, expected).Scan(&version)
	if errors.Is(e, pgx.ErrNoRows) {
		return errLoginNameStale245
	}
	if e != nil {
		return e
	}
	if version != a.user.AuthVersion+1 {
		return errCredentialActor199
	}
	if _, e = tx.Exec(ctx, `DELETE FROM console_sessions WHERE username=$1 AND role='admin'`, a.user.Username); e != nil {
		return e
	}
	var remaining bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM console_sessions WHERE username=$1 AND role='admin')`, a.user.Username).Scan(&remaining); e != nil {
		return e
	}
	if remaining {
		return errors.New("mandatory account session revocation incomplete")
	}
	changes, _ := json.Marshal(map[string]string{"previous_username": current, "username": next})
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'account',$1,'login-name-change',$3)`, a.user.ID, a.user.Username, changes); e != nil {
		return e
	}
	if e = credentialDeadline199(ctx, tx, account.expiry); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
