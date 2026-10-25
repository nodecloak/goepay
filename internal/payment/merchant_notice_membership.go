package payment

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	notice "goepay/internal/merchantnotice"
)

// This read-only projection authenticates the latest purchase independently
// of mutable group settings. It grants no right to send or change membership.
func (s *Store) NoticeExpiredMembership(ctx context.Context, tx pgx.Tx, mid int64, at time.Time) (*notice.ExpiredMembership, error) {
	if ctx == nil || tx == nil || mid <= 0 || at.IsZero() {
		return nil, ErrInvalid
	}
	p, err := scanMembership(tx.QueryRow(ctx, `SELECT `+strings.ReplaceAll(membershipColumns, "to_jsonb(membership_purchases)", "to_jsonb(p)")+` FROM membership_purchases p WHERE p.merchant_id=$1 AND p.expires_at<=$2
 AND p.id=(SELECT max(q.id) FROM membership_purchases q WHERE q.merchant_id=p.merchant_id)
 AND EXISTS(SELECT 1 FROM memberships m WHERE m.merchant_id=p.merchant_id AND (m.group_id,m.expires_at)=(p.group_id,p.expires_at))`, mid, at))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = s.verifyMembershipPurchase(ctx, tx, p); err != nil {
		return nil, err
	}
	var hash []byte
	if p.SourceVersion == 1 {
		if err = tx.QueryRow(ctx, `SELECT proof_hash FROM membership_referral_sources WHERE purchase_id=$1`, p.ID).Scan(&hash); err != nil {
			return nil, err
		}
	} else {
		if err = tx.QueryRow(ctx, `SELECT row_hash FROM membership_referral_legacy_purchases WHERE purchase_id=$1`, p.ID).Scan(&hash); err != nil {
			return nil, err
		}
	}
	return &notice.ExpiredMembership{PurchaseID: p.ID, MerchantID: p.MerchantID, GroupName: p.GroupName, ExpiresAt: p.ExpiresAt, SourceVersion: p.SourceVersion, ProofHash: hash}, nil
}
