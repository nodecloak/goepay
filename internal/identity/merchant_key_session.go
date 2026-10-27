package identity

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Only a persistent original merchant session may be issued by key login.
// The caller holds merchant -> durable user locks and commits its audit with
// this insertion. It must perform its final original-session/clock check.
func (s *Sessions) CreateMerchantSessionTx(ctx context.Context, tx pgx.Tx, u User) (string, string, error) {
	if s == nil || s.pool == nil || tx == nil || u.Role != "merchant" || u.ID <= 0 || u.Username == "" || u.AuthVersion <= 0 {
		return "", "", ErrOriginalMerchantSession
	}
	token, e := randomToken()
	if e != nil {
		return "", "", e
	}
	csrf, e := randomToken()
	if e != nil {
		return "", "", e
	}
	tag, e := tx.Exec(ctx, `INSERT INTO console_sessions(token_hash,username,role,subject_id,csrf,expires_at,auth_version) SELECT $1,username,role,subject_id,$2,clock_timestamp()+interval '12 hours',auth_version FROM console_users WHERE username=$3 AND role='merchant' AND subject_id=$4 AND auth_version=$5 AND enabled`, tokenDigest(token), csrf, u.Username, u.ID, u.AuthVersion)
	if e != nil {
		return "", "", e
	}
	if tag.RowsAffected() != 1 {
		return "", "", ErrOriginalMerchantSession
	}
	return token, csrf, nil
}
