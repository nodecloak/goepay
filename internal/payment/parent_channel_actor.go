package payment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
)

// Configuration writes require the original durable super session and the same
// CSRF stamp that admitted this request, including after the final waiting write.
type ParentChannelActor struct {
	User        identity.User
	Token, CSRF string
	Delegated   bool
}

func (a ParentChannelActor) check(ctx context.Context, tx pgx.Tx) error {
	if len(a.CSRF) != 43 {
		return identity.ErrAdminSession
	}
	if e := identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.Token, a.Delegated); e != nil {
		return e
	}
	hash := sha256.Sum256([]byte(a.Token))
	var csrf string
	if e := tx.QueryRow(ctx, `SELECT csrf FROM console_sessions WHERE token_hash=$1 AND role='admin' FOR SHARE`, hash[:]).Scan(&csrf); e != nil {
		return identity.ErrAdminSession
	}
	if !hmac.Equal([]byte(csrf), []byte(a.CSRF)) {
		return identity.ErrAdminSession
	}
	return nil
}

func (s *Store) CheckParentChannelActorTx(ctx context.Context, tx pgx.Tx, a ParentChannelActor) error {
	return a.check(ctx, tx)
}
