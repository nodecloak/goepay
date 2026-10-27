package identity

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
)

var ErrAdminSession = errors.New("original administrator session required")

// CheckOriginalAdminSessionTx permits the original operator or super account;
// stronger configuration and financial actions keep their separate checks.
func CheckOriginalAdminSessionTx(ctx context.Context, tx pgx.Tx, user User, token string, delegated bool) error {
	if tx == nil || (user.AdminClass != "super" && user.AdminClass != "operator") {
		return ErrAdminSession
	}
	if _, e := fundsActorTx(ctx, tx, FundsActor{User: user, SessionToken: token, Delegated: delegated}); e != nil {
		if errors.Is(e, ErrFundsAuthentication) {
			return ErrAdminSession
		}
		return e
	}
	var actual string
	if e := tx.QueryRow(ctx, "SELECT admin_class FROM console_users WHERE username=$1 AND role='admin' FOR SHARE", user.Username).Scan(&actual); e != nil {
		return e
	}
	if actual != user.AdminClass {
		return ErrAdminSession
	}
	return nil
}

// Reuse the durable actor/session locks used by funds confirmations. Callers
// recheck after their last potentially waiting write and before commit.
func CheckOriginalSuperAdminSessionTx(ctx context.Context, tx pgx.Tx, user User, token string, delegated bool) error {
	if tx == nil || user.AdminClass != "super" {
		return ErrAdminSession
	}
	if _, e := fundsActorTx(ctx, tx, FundsActor{User: user, SessionToken: token, Delegated: delegated}); e != nil {
		if errors.Is(e, ErrFundsAuthentication) {
			return ErrAdminSession
		}
		return e
	}
	var actual string
	if e := tx.QueryRow(ctx, "SELECT admin_class FROM console_users WHERE username=$1 AND role='admin' FOR SHARE", user.Username).Scan(&actual); e != nil {
		return e
	}
	if actual != "super" || actual != user.AdminClass {
		return ErrAdminSession
	}
	return nil
}
