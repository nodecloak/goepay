package identity

import (
	"context"
	"crypto/sha256"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
	"time"
)

func NewPersistentSessions(pool *pgxpool.Pool, secure bool) *Sessions {
	s := NewSessions(secure)
	s.pool = pool
	return s
}
func sessionContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 3*time.Second)
}
func tokenDigest(token string) []byte { h := sha256.Sum256([]byte(token)); return h[:] }
func (s *Sessions) createPersistent(u User) (string, string, error) {
	if u.AuthVersion < 1 {
		return "", "", errors.New("verified account version required")
	}
	token, err := randomToken()
	if err != nil {
		return "", "", err
	}
	csrf, err := randomToken()
	if err != nil {
		return "", "", err
	}
	ctx, cancel := sessionContext()
	defer cancel()
	tag, err := s.pool.Exec(ctx, "INSERT INTO console_sessions(token_hash,username,role,subject_id,csrf,expires_at,auth_version) SELECT $1,username,role,subject_id,$2,$3,auth_version FROM console_users WHERE username=$4 AND role=$5 AND subject_id=$6 AND auth_version=$7 AND enabled", tokenDigest(token), csrf, s.now().Add(12*time.Hour), u.Username, u.Role, u.ID, u.AuthVersion)
	if err == nil && tag.RowsAffected() != 1 {
		err = errors.New("account unavailable")
	}
	if err != nil {
		return "", "", err
	}
	return token, csrf, nil
}
func (s *Sessions) getPersistent(token, role string) (Session, bool) {
	var v Session
	if len(token) != 43 {
		return v, false
	}
	ctx, cancel := sessionContext()
	defer cancel()
	err := s.pool.QueryRow(ctx, `SELECT u.username,u.role,u.subject_id,u.display_name,s.csrf,s.expires_at,u.auth_version,u.admin_class FROM console_sessions s JOIN console_users u ON u.username=s.username AND u.role=s.role AND u.subject_id=s.subject_id AND u.auth_version=s.auth_version AND u.enabled WHERE s.token_hash=$1 AND s.role=$2 AND s.expires_at>$3`, tokenDigest(token), role, s.now()).Scan(&v.User.Username, &v.User.Role, &v.User.ID, &v.User.Name, &v.CSRF, &v.Expires, &v.User.AuthVersion, &v.User.AdminClass)
	if err != nil {
		return v, false
	}
	issuer, err := s.delegatedIssuer(ctx, token)
	if err != nil {
		return Session{}, false
	}
	v.Issuer = issuer
	return v, true
}
func (s *Sessions) deletePersistent(token string) {
	ctx, cancel := sessionContext()
	defer cancel()
	if _, err := s.pool.Exec(ctx, "DELETE FROM console_sessions WHERE token_hash=$1", tokenDigest(token)); err != nil {
		slog.Error("session revocation failed")
	}
}
func (s *Sessions) revokePersistent(username, role string) {
	ctx, cancel := sessionContext()
	defer cancel()
	if _, err := s.pool.Exec(ctx, "DELETE FROM console_sessions WHERE username=$1 AND role=$2", username, role); err != nil {
		slog.Error("account session revocation failed")
	}
}
func (s *Sessions) cleanupPersistent(cutoff time.Time, remove bool) int {
	ctx, cancel := sessionContext()
	defer cancel()
	if cutoff.After(s.now()) {
		return -1
	}
	if remove {
		tag, err := s.pool.Exec(ctx, "DELETE FROM console_sessions WHERE expires_at<=$1", cutoff)
		if err != nil {
			return -1
		}
		return int(tag.RowsAffected())
	}
	var n int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM console_sessions WHERE expires_at<=$1", cutoff).Scan(&n); err != nil {
		return -1
	}
	return n
}
