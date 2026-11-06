package kyc

import (
	"context"
	"crypto/hmac"
	"time"
)

func validCode(v string) bool {
	if v == "" || len(v) > 2048 {
		return false
	}
	for _, c := range v {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}

// CacheReturn stores a short-lived OAuth code against an unpredictable,
// merchant-bound attempt/state. It neither verifies identity, calls a provider,
// discloses a token nor charges a fee. A later original merchant session must
// explicitly consume the code through Query.
func (s *Service) CacheReturn(ctx context.Context, id, state, code string) error {
	if !tokenID.MatchString(id) || !tokenID.MatchString(state) || !validCode(code) {
		return ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var merchant int64
	if e = tx.QueryRow(ctx, "SELECT merchant_id FROM kyc_attempts233 WHERE id=$1", id).Scan(&merchant); e != nil {
		return ErrInvalid
	}
	var locked int64
	if e = tx.QueryRow(ctx, "SELECT id FROM merchants WHERE id=$1 FOR NO KEY UPDATE", merchant).Scan(&locked); e != nil {
		return e
	}
	r, e := s.readAttempt(ctx, tx, id, merchant, true)
	if e != nil {
		return e
	}
	if r.Data.Snapshot.Mode != 3 || (r.Status != "pending" && r.Status != "unknown") || r.Data.AuthCodeUsed || !hmac.Equal([]byte(state), []byte(r.Data.Provider.State)) || !time.Now().Before(r.Expires) {
		return ErrConflict
	}
	if r.Data.Provider.AuthCode != "" {
		if hmac.Equal([]byte(code), []byte(r.Data.Provider.AuthCode)) && time.Now().Before(r.Data.ReturnExpiresAt) {
			return nil
		}
		return ErrConflict
	}
	r.Data.Provider.AuthCode = code
	r.Data.ReturnExpiresAt = time.Now().UTC().Truncate(time.Microsecond).Add(5 * time.Minute)
	if r.Data.ReturnExpiresAt.After(r.Expires) {
		r.Data.ReturnExpiresAt = r.Expires
	}
	if e = s.writeAttempt(ctx, tx, &r, false); e != nil {
		return e
	}
	if !time.Now().Before(r.Expires) || !time.Now().Before(r.Data.ReturnExpiresAt) {
		return ErrConflict
	}
	return tx.Commit(ctx)
}
