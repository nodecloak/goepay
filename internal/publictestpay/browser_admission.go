package publictestpay

import (
	"context"
	"crypto/hmac"
	"encoding/hex"

	"github.com/jackc/pgx/v5"
)

func (s *Service) lockBrowserAdmission203(ctx context.Context, tx pgx.Tx, browser string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(current_schema()||':public-testpay203:'||$1::text,203))`, hex.EncodeToString(s.sum("browser", []byte(browser))))
	return err
}

func (s *Service) outstandingIntent203(ctx context.Context, tx pgx.Tx, browser string) (Intent, error) {
	var in Intent
	var proof []byte
	err := tx.QueryRow(ctx, `SELECT i.id,i.browser_hash,i.csrf_hash,i.merchant_id,i.out_trade_no,i.config_hash,i.amount,i.type_id,i.pay_type,i.state,i.order_trade_no,i.expires_at,i.proof_hash FROM public_testpay203_intents i LEFT JOIN orders o ON o.merchant_id=i.merchant_id AND o.out_trade_no=i.out_trade_no WHERE i.browser_hash=$1 AND i.state IN ('admitted','submitted','unknown') AND COALESCE(o.status,'pending') NOT IN ('paid','closed','failed') ORDER BY i.created_at DESC,i.id DESC LIMIT 1 FOR SHARE OF i`, s.sum("browser", []byte(browser))).Scan(&in.ID, &in.Browser, &in.CSRF, &in.MerchantID, &in.OutTradeNo, &in.ConfigHash, &in.Amount, &in.TypeID, &in.PayType, &in.State, &in.OrderTradeNo, &in.ExpiresAt, &proof)
	if err == nil && !hmac.Equal(proof, s.intentProof(in)) {
		return Intent{}, ErrConflict
	}
	return in, err
}
