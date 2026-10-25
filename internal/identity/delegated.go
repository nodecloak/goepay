package identity

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// SessionIssuer preserves the real operator independently of the effective user.
type SessionIssuer struct {
	ID       int64  `json:"id,string"`
	Name     string `json:"name"`
	Username string `json:"-"`
}

var ErrDelegationUnavailable = errors.New("delegated identity unavailable")

func (s *Sessions) SupportsDelegation() bool { return s.pool != nil }

func (s *Sessions) delegatedIssuer(ctx context.Context, token string) (*SessionIssuer, error) {
	var issuer SessionIssuer
	var valid bool
	e := s.pool.QueryRow(ctx, `SELECT d.issuer_id,COALESCE(u.display_name,''),d.issuer_username,
 EXISTS(SELECT 1 FROM console_sessions a WHERE a.token_hash=d.issuer_session_hash AND a.role='admin' AND a.username=d.issuer_username AND a.subject_id=d.issuer_id AND a.auth_version=d.issuer_auth_version AND a.expires_at>clock_timestamp())
 AND u.username IS NOT NULL
 FROM console_session_issuers d LEFT JOIN console_users u ON u.username=d.issuer_username AND u.role='admin' AND u.subject_id=d.issuer_id AND u.auth_version=d.issuer_auth_version AND u.enabled AND u.admin_class='super'
 WHERE d.token_hash=$1`, tokenDigest(token)).Scan(&issuer.ID, &issuer.Name, &issuer.Username, &valid)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	if !valid {
		return nil, errors.New("delegated authority revoked")
	}
	return &issuer, nil
}

// CreateDelegated joins the caller's grant/audit transaction. Neither a token nor
// a partially attributed session may escape if any statement fails.
func (s *Sessions) CreateDelegated(ctx context.Context, tx pgx.Tx, target User, issuer User, issuerToken string) (token, csrf string, expires time.Time, err error) {
	if s.pool == nil || target.Role != "merchant" || target.AuthVersion < 1 || issuer.Role != "admin" || issuer.AdminClass != "super" || issuer.AuthVersion < 1 {
		return "", "", time.Time{}, errors.New("verified delegated identities required")
	}
	token, err = randomToken()
	if err != nil {
		return
	}
	csrf, err = randomToken()
	if err != nil {
		return
	}
	expires = s.now().Add(15 * time.Minute)
	var count int64
	tag, e := tx.Exec(ctx, `INSERT INTO console_sessions(token_hash,username,role,subject_id,csrf,expires_at,auth_version)
 SELECT $1,u.username,u.role,u.subject_id,$2,$3,u.auth_version FROM console_users u JOIN merchants m ON m.id=u.subject_id AND m.enabled
 WHERE u.username=$4 AND u.role='merchant' AND u.subject_id=$5 AND u.auth_version=$6 AND u.enabled`, tokenDigest(token), csrf, expires, target.Username, target.ID, target.AuthVersion)
	if e != nil {
		err = e
		return
	}
	count = tag.RowsAffected()
	if count != 1 {
		err = ErrDelegationUnavailable
		return
	}
	_, err = tx.Exec(ctx, `INSERT INTO console_session_issuers(token_hash,issuer_session_hash,issuer_username,issuer_id,issuer_auth_version) VALUES($1,$2,$3,$4,$5)`, tokenDigest(token), tokenDigest(issuerToken), issuer.Username, issuer.ID, issuer.AuthVersion)
	return
}
