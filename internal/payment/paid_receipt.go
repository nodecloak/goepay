package payment

import (
	"context"
	"github.com/jackc/pgx/v5"
	"net"
)

// PaidReceipt may only be supplied after the provider identity, signature,
// platform order, currency and exact amount have all been authenticated.
type PaidReceipt struct {
	RealMoney                int64 `json:"-"`
	ChannelID                int64
	ProviderTradeNo, BuyerID string
	// CombinePaid is the verified per-sub-order payment of a combined order.
	CombinePaid []CombinePaid207 `json:"-"`
}

func recordPaidReceipt(ctx context.Context, tx pgx.Tx, orderID int64, r PaidReceipt) error {
	if r.ChannelID <= 0 || r.ProviderTradeNo == "" || len(r.ProviderTradeNo) > 256 || len(r.BuyerID) > 256 {
		return ErrInvalid
	}
	var expected string
	if e := tx.QueryRow(ctx, "SELECT provider_trade_no FROM provider_attempts WHERE order_id=$1 AND channel_id=$2", orderID, r.ChannelID).Scan(&expected); e != nil {
		return e
	}
	if expected != "" && expected != r.ProviderTradeNo {
		return ErrConflict
	}
	if _, e := tx.Exec(ctx, "INSERT INTO verified_payment_receipts(order_id,channel_id,provider_trade_no) VALUES($1,$2,$3) ON CONFLICT(order_id) DO NOTHING", orderID, r.ChannelID, r.ProviderTradeNo); e != nil {
		return e
	}
	var stored PaidReceipt
	if e := tx.QueryRow(ctx, "SELECT channel_id,provider_trade_no FROM verified_payment_receipts WHERE order_id=$1", orderID).Scan(&stored.ChannelID, &stored.ProviderTradeNo); e != nil {
		return e
	}
	if stored.ChannelID != r.ChannelID || stored.ProviderTradeNo != r.ProviderTradeNo {
		return ErrConflict
	}
	if r.BuyerID != "" {
		tag, e := tx.Exec(ctx, "INSERT INTO order_reporting(order_id,buyer) VALUES($1,$2) ON CONFLICT(order_id) DO UPDATE SET buyer=EXCLUDED.buyer WHERE order_reporting.buyer='' OR order_reporting.buyer=EXCLUDED.buyer", orderID, r.BuyerID)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return ErrConflict
		}
	}
	return nil
}

// SetOrderClientIP retains the original validated client address. A retry from a
// different device must not rewrite the original payment metadata.
func (s *Store) SetOrderClientIP(ctx context.Context, orderID int64, raw string) error {
	ip := net.ParseIP(raw)
	if orderID <= 0 || ip == nil {
		return ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var locked int64
	// Match paid-confirmation lock order: platform order before reporting row.
	if e = tx.QueryRow(ctx, "SELECT id FROM orders WHERE id=$1 FOR NO KEY UPDATE", orderID).Scan(&locked); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO order_reporting(order_id,client_ip) VALUES($1,$2) ON CONFLICT(order_id) DO UPDATE SET client_ip=EXCLUDED.client_ip WHERE order_reporting.client_ip=''", orderID, ip.String()); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
