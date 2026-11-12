package server

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/backoffice"
	"goepay/internal/identity"
)

type consoleAdminAuthorityKey struct{}
type consoleAdminAuthorityGuard func(context.Context, pgx.Tx) error

// The first check locks the current actor and, for persistent sessions, its
// original session until the business transaction ends. Later checks in that
// same transaction validate the original deadline: an authorized self-edit may
// change its own epoch or revoke its sessions without invalidating that edit.
// Neither another transaction nor a closed transaction can reuse this proof.
func (s *Server) withConsoleAdminAuthority(r *http.Request, session identity.Session, token string, accountVersion int64, parts []string) *http.Request {
	method := r.Method
	parts = append([]string(nil), parts...)
	var mu sync.Mutex
	verified := make(map[pgx.Tx]time.Time)
	guard := consoleAdminAuthorityGuard(func(ctx context.Context, tx pgx.Tx) error {
		if tx == nil {
			return identity.ErrFundsAuthentication
		}
		mu.Lock()
		defer mu.Unlock()
		expires, ok := verified[tx]
		if !ok {
			var err error
			expires, err = s.consoleAdminAuthorityTx(ctx, tx, session, token, accountVersion, method, parts)
			if err != nil {
				return err
			}
			verified[tx] = expires
		}
		if !s.Sessions.SupportsDelegation() {
			if _, ok := s.currentMemoryAdminSession(session, token); !ok {
				return identity.ErrFundsAuthentication
			}
		}
		var now time.Time
		if err := tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
			return err
		}
		if !now.Before(expires) {
			return identity.ErrFundsAuthentication
		}
		return nil
	})
	ctx := context.WithValue(r.Context(), consoleAdminAuthorityKey{}, guard)
	ctx = backoffice.WithAdminAuthorityGuard(ctx, guard)
	return r.WithContext(ctx)
}

func requireConsoleAdminAuthorityTx(ctx context.Context, tx pgx.Tx) error {
	guard, ok := ctx.Value(consoleAdminAuthorityKey{}).(consoleAdminAuthorityGuard)
	if !ok {
		return identity.ErrFundsAuthentication
	}
	return guard(ctx, tx)
}

func (s *Server) currentMemoryAdminSession(original identity.Session, token string) (identity.Session, bool) {
	current, ok := s.Sessions.Get(token, "admin")
	return current, ok && current.Issuer == nil && current.User.Username == original.User.Username &&
		current.User.Role == original.User.Role && current.User.ID == original.User.ID &&
		current.User.AuthVersion == original.User.AuthVersion && current.CSRF == original.CSRF
}

func (s *Server) consoleAdminAuthorityTx(ctx context.Context, tx pgx.Tx, session identity.Session, token string, accountVersion int64, method string, parts []string) (time.Time, error) {
	if session.User.Role != "admin" || session.User.ID <= 0 || session.User.Username == "" || session.Issuer != nil || accountVersion <= 0 || s.Sessions == nil {
		return time.Time{}, identity.ErrFundsAuthentication
	}
	current := session.User
	var enabled bool
	err := tx.QueryRow(ctx, `SELECT subject_id,enabled,admin_class,auth_version,permissions FROM console_users WHERE username=$1 AND role='admin' FOR SHARE`, session.User.Username).Scan(&current.ID, &enabled, &current.AdminClass, &current.AuthVersion, &current.Permissions)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, identity.ErrFundsAuthentication
	}
	if err != nil {
		return time.Time{}, err
	}
	if !enabled || current.ID != session.User.ID || current.AdminClass != session.User.AdminClass || current.AuthVersion != accountVersion || !adminRequestAllowedHTTP(current, method, parts) {
		return time.Time{}, identity.ErrFundsAuthentication
	}
	if s.Sessions.SupportsDelegation() {
		if err = identity.CheckOriginalAdminSessionTx(ctx, tx, session.User, token, false); err != nil {
			if errors.Is(err, identity.ErrAdminSession) {
				err = identity.ErrFundsAuthentication
			}
			return time.Time{}, err
		}
		digest := sha256.Sum256([]byte(token))
		var expires time.Time
		if err = tx.QueryRow(ctx, "SELECT expires_at FROM console_sessions WHERE token_hash=$1 AND role='admin' FOR SHARE", digest[:]).Scan(&expires); err != nil {
			return time.Time{}, err
		}
		return expires, nil
	}
	// The non-persistent repository remains a supported local test seam. It
	// still rechecks the actual token and the locked current database authority;
	// an absent auth_version in an old test session grants no database bypass.
	actual, ok := s.currentMemoryAdminSession(session, token)
	if !ok {
		return time.Time{}, identity.ErrFundsAuthentication
	}
	return actual.Expires, nil
}
