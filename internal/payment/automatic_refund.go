package payment

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Unnamed API retries share an identity for the current/previous ten-minute
// window. Explicit refund numbers remain the way to request equal partial
// refunds separately. Keep the number format readable across upgrades.
const automaticRefundWindow = 10 * time.Minute

// Identity selection and reservation must share the original order lock and
// transaction. A request crossing a window boundary waits for any earlier
// reservation to commit before inspecting the previous window's identity.
func (s *Store) reserveAutomaticAPIRefund(ctx context.Context, merchantID int64, outTradeNo string, amount int64, now func() time.Time) (Refund, error) {
	if amount <= 0 {
		return Refund{}, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Refund{}, e
	}
	defer tx.Rollback(ctx)
	account, e := s.merchantAPIAccountTx(ctx, tx, merchantID, false)
	if e != nil {
		return Refund{}, e
	}
	var tradeNo string
	e = tx.QueryRow(ctx, "SELECT trade_no FROM orders WHERE merchant_id=$1 AND out_trade_no=$2 FOR UPDATE", merchantID, outTradeNo).Scan(&tradeNo)
	if errors.Is(e, pgx.ErrNoRows) {
		return Refund{}, ErrNotFound
	}
	if e != nil {
		return Refund{}, e
	}
	number, e := automaticRefundNoTx(ctx, tx, merchantID, tradeNo, amount, now())
	if e != nil {
		return Refund{}, e
	}
	r, e := s.reserveRefundTx(ctx, tx, merchantID, outTradeNo, number, amount, refundAdmission179{account, refundAPI179})
	if e != nil {
		return r, e
	}
	return r, tx.Commit(ctx)
}

func automaticRefundNoTx(ctx context.Context, tx pgx.Tx, merchantID int64, tradeNo string, amount int64, now time.Time) (string, error) {
	window := now.Unix() / int64(automaticRefundWindow/time.Second)
	number := func(window int64, attempt int) string {
		sum := sha256.Sum256([]byte(fmt.Sprintf("goepay/unnumbered-refund/v1\x00%d\x00%s\x00%d\x00%d\x00%d", merchantID, tradeNo, amount, window, attempt)))
		return "rf" + base64.RawURLEncoding.EncodeToString(sum[:16])
	}
	status := func(no string) (string, error) {
		var state string
		e := tx.QueryRow(ctx, "SELECT status FROM refunds WHERE merchant_id=$1 AND refund_no=$2", merchantID, no).Scan(&state)
		if errors.Is(e, pgx.ErrNoRows) {
			return "", nil
		}
		return state, e
	}
	const attempts = 20
	for attempt := 0; attempt < attempts; attempt++ {
		candidate := number(window-1, attempt)
		state, e := status(candidate)
		if e != nil {
			return "", e
		}
		if state == "" {
			break
		}
		if state != "failed" {
			return candidate, nil
		}
	}
	for attempt := 0; attempt < attempts; attempt++ {
		candidate := number(window, attempt)
		state, e := status(candidate)
		if e != nil {
			return "", e
		}
		if state != "failed" {
			return candidate, nil
		}
	}
	return "", ErrConflict
}
