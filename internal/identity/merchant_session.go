package identity

import (
	"context"
	"crypto/subtle"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrOriginalMerchantSession = errors.New("original merchant session required")

// Callers lock the merchant first. Durable account/session locks serialize this
// operation with revocation, disablement, password changes and delegated access.
func CheckOriginalMerchantSessionTx(ctx context.Context, tx pgx.Tx, user User, token, csrf string) error {
	if tx == nil || user.Role != "merchant" || user.ID <= 0 || user.AuthVersion <= 0 || user.Username == "" || len(token) != 43 || len(csrf) != 43 {
		return ErrOriginalMerchantSession
	}
	var id, version int64
	e := tx.QueryRow(ctx, `SELECT subject_id,auth_version FROM console_users WHERE username=$1 AND role='merchant' AND enabled FOR SHARE`, user.Username).Scan(&id, &version)
	if errors.Is(e, pgx.ErrNoRows) {
		return ErrOriginalMerchantSession
	}
	if e != nil {
		return e
	}
	if id != user.ID || version != user.AuthVersion {
		return ErrOriginalMerchantSession
	}
	var sid, sv int64
	var name, actualCSRF string
	var expiry time.Time
	e = tx.QueryRow(ctx, `SELECT username,subject_id,auth_version,csrf,expires_at FROM console_sessions WHERE token_hash=$1 AND role='merchant' FOR SHARE`, tokenDigest(token)).Scan(&name, &sid, &sv, &actualCSRF, &expiry)
	if errors.Is(e, pgx.ErrNoRows) {
		return ErrOriginalMerchantSession
	}
	if e != nil {
		return e
	}
	if name != user.Username || sid != id || sv != version || subtle.ConstantTimeCompare([]byte(actualCSRF), []byte(csrf)) != 1 {
		return ErrOriginalMerchantSession
	}
	var delegated bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM console_session_issuers WHERE token_hash=$1)`, tokenDigest(token)).Scan(&delegated); e != nil {
		return e
	}
	if delegated {
		return ErrOriginalMerchantSession
	}
	// Read the clock after acquiring every row lock; an expression evaluated before
	// a lock wait must not authorize a session that expired while it was waiting.
	var now time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return e
	}
	if !now.Before(expiry) {
		return ErrOriginalMerchantSession
	}
	return nil
}
