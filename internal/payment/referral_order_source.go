package payment

import (
	"context"
	"crypto/hmac"
	"github.com/jackc/pgx/v5"
)

// This is a separate birth family; existing v1/v2 fact encodings stay unchanged.
func (s *Store) referralOrderSourceProof(o Order, terms, cost []byte) ([]byte, error) {
	return s.referralDigest("order-source-v3/birth", []any{o.ID, o.TradeNo, o.MerchantID, terms, cost})
}

func (s *Store) readReferralOrderSource(ctx context.Context, q referralReader, o Order, terms []byte) error {
	var version int
	var hash, cost []byte
	if e := q.QueryRow(ctx, `SELECT referral_source_version,referral_source_hash,(SELECT proof_hash FROM referral_profit_costs WHERE order_id=o.id) FROM orders o WHERE id=$1`, o.ID).Scan(&version, &hash, &cost); e != nil {
		return e
	}
	if version == 0 {
		if hash != nil {
			return ErrConflict
		}
		return nil
	}
	if version != 3 || len(terms) != 32 || len(hash) != 32 {
		return ErrConflict
	}
	expected, e := s.referralOrderSourceProof(o, terms, cost)
	if e != nil {
		return e
	}
	if !hmac.Equal(expected, hash) {
		return ErrConflict
	}
	return nil
}

func (s *Store) insertReferralOrderSource(ctx context.Context, tx pgx.Tx, o Order, t referralOrderTerms, c *referralProfitCost) error {
	var cost []byte
	if c != nil {
		cost = c.Proof
	}
	hash, e := s.referralOrderSourceProof(o, t.ProofHash, cost)
	if e != nil {
		return e
	}
	tag, e := tx.Exec(ctx, `UPDATE orders SET referral_source_version=3,referral_source_hash=$2 WHERE id=$1 AND referral_source_version=0 AND referral_source_hash IS NULL`, o.ID, hash)
	if e == nil && tag.RowsAffected() != 1 {
		return ErrConflict
	}
	return e
}
