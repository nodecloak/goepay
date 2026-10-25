package payment

import (
	"context"

	"crypto/rand"

	"encoding/hex"
	"errors"
	"github.com/jackc/pgx/v5"
	"strings"
)

type MiniLaunchBinding struct {
	TradeNo                                string
	MerchantID, ChannelID                  int64
	Provider, AppID, Method, Purpose, Path string
}

func validMiniBinding(b MiniLaunchBinding) bool {
	if b.TradeNo == "" || b.MerchantID <= 0 || b.ChannelID <= 0 || b.AppID == "" || len(b.AppID) > 128 || b.Purpose != "mini-payment" || len(b.Path) == 0 || len(b.Path) > 256 {
		return false
	}
	for _, part := range strings.Split(b.Path, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
		for _, c := range part {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '.' || c == '-') {
				return false
			}
		}
	}
	return b.Provider == "wxpay" && b.Method == "jsapi" || b.Provider == "alipay" && b.Method == "mini"
}

const miniBindingColumns = "o.trade_no,o.merchant_id,i.channel_id,i.provider,i.app_id,i.method,i.purpose,i.path"

func scanMiniBinding(row pgx.Row) (MiniLaunchBinding, error) {
	var b MiniLaunchBinding
	e := row.Scan(&b.TradeNo, &b.MerchantID, &b.ChannelID, &b.Provider, &b.AppID, &b.Method, &b.Purpose, &b.Path)
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrNotFound
	}
	return b, e
}
func (s *Store) MiniPaymentIntent(ctx context.Context, tradeNo string) (MiniLaunchBinding, error) {
	return scanMiniBinding(s.pool.QueryRow(ctx, "SELECT "+miniBindingColumns+" FROM mini_payment_intents i JOIN orders o ON o.id=i.order_id WHERE o.trade_no=$1", tradeNo))
}
func (s *Store) PrepareMiniPayment(ctx context.Context, b MiniLaunchBinding) error {
	if !validMiniBinding(b) {
		return ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var id int64
	var status, provider string
	var enabled, attempted bool
	e = tx.QueryRow(ctx, "SELECT o.id,o.status,o.type,m.enabled,EXISTS(SELECT 1 FROM provider_attempts WHERE order_id=o.id) FROM orders o JOIN merchants m ON m.id=o.merchant_id WHERE o.trade_no=$1 AND o.merchant_id=$2 FOR UPDATE OF o", b.TradeNo, b.MerchantID).Scan(&id, &status, &provider, &enabled, &attempted)
	if e != nil {
		return e
	}
	if !enabled || provider != b.Provider {
		return ErrState
	}
	old, e := scanMiniBinding(tx.QueryRow(ctx, "SELECT "+miniBindingColumns+" FROM mini_payment_intents i JOIN orders o ON o.id=i.order_id WHERE i.order_id=$1", id))
	if e == nil {
		if old != b {
			return ErrConflict
		}
		return tx.Commit(ctx)
	}
	if !errors.Is(e, ErrNotFound) {
		return e
	}
	if status != "created" || attempted {
		return ErrState
	}
	if _, e = tx.Exec(ctx, "INSERT INTO mini_payment_intents(order_id,channel_id,provider,app_id,method,purpose,path) VALUES($1,$2,$3,$4,$5,$6,$7)", id, b.ChannelID, b.Provider, b.AppID, b.Method, b.Purpose, b.Path); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Store) IssueMiniState(ctx context.Context, b MiniLaunchBinding) (string, error) {
	if !validMiniBinding(b) {
		return "", ErrOAuthState
	}
	nonce := make([]byte, 32)
	if _, e := rand.Read(nonce); e != nil {
		return "", e
	}
	state := hex.EncodeToString(nonce)
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return "", e
	}
	defer tx.Rollback(ctx)
	var id int64
	var status string
	var enabled, attempted bool
	if e = tx.QueryRow(ctx, "SELECT o.id,o.status,m.enabled,EXISTS(SELECT 1 FROM provider_attempts WHERE order_id=o.id) FROM orders o JOIN merchants m ON m.id=o.merchant_id WHERE o.trade_no=$1 AND o.merchant_id=$2 FOR UPDATE OF o", b.TradeNo, b.MerchantID).Scan(&id, &status, &enabled, &attempted); e != nil {
		return "", ErrOAuthState
	}
	if status != "created" || !enabled || attempted {
		return "", ErrOAuthState
	}
	stored, e := scanMiniBinding(tx.QueryRow(ctx, "SELECT "+miniBindingColumns+" FROM mini_payment_intents i JOIN orders o ON o.id=i.order_id WHERE i.order_id=$1", id))
	if e != nil || stored != b {
		return "", ErrOAuthState
	}
	var active int
	if e = tx.QueryRow(ctx, "SELECT count(*) FROM mini_oauth_states WHERE order_id=$1 AND consumed_at IS NULL AND expires_at>clock_timestamp()", id).Scan(&active); e != nil {
		return "", e
	}
	if active >= 8 {
		return "", ErrState
	}
	_, e = tx.Exec(ctx, "INSERT INTO mini_oauth_states(state_hash,order_id,merchant_id,channel_id,provider,app_id,method,purpose,path) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)", s.oauthHash("mini-state", state), id, b.MerchantID, b.ChannelID, b.Provider, b.AppID, b.Method, b.Purpose, b.Path)
	if e != nil {
		return "", e
	}
	if e = tx.Commit(ctx); e != nil {
		return "", e
	}
	return state, nil
}
func (s *Store) ConsumeMiniState(ctx context.Context, state, code string, b MiniLaunchBinding) error {
	if !validMiniBinding(b) || !validOAuthBrowser(state) || code == "" || len(code) > 4096 || strings.TrimSpace(code) != code || strings.HasPrefix(code, "@") || strings.ContainsAny(code, "\r\n\x00") {
		return ErrOAuthState
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var id int64
	var status string
	var enabled, attempted bool
	if e = tx.QueryRow(ctx, "SELECT o.id,o.status,m.enabled,EXISTS(SELECT 1 FROM provider_attempts WHERE order_id=o.id) FROM orders o JOIN merchants m ON m.id=o.merchant_id WHERE o.trade_no=$1 AND o.merchant_id=$2 FOR UPDATE OF o", b.TradeNo, b.MerchantID).Scan(&id, &status, &enabled, &attempted); e != nil {
		return ErrOAuthState
	}
	tag, e := tx.Exec(ctx, "UPDATE mini_oauth_states SET consumed_at=clock_timestamp() WHERE state_hash=$1 AND order_id=$2 AND merchant_id=$3 AND channel_id=$4 AND provider=$5 AND app_id=$6 AND method=$7 AND purpose=$8 AND path=$9 AND consumed_at IS NULL AND expires_at>clock_timestamp()", s.oauthHash("mini-state", state), id, b.MerchantID, b.ChannelID, b.Provider, b.AppID, b.Method, b.Purpose, b.Path)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrOAuthState
	}
	tag, e = tx.Exec(ctx, "INSERT INTO oauth_code_claims(provider,app_id,code_hash,mini_state_hash) VALUES($1,$2,$3,$4) ON CONFLICT(provider,app_id,code_hash) DO NOTHING", b.Provider, b.AppID, s.oauthHash("code", code), s.oauthHash("mini-state", state))
	if e != nil {
		return e
	}
	legacyClaimed := tag.RowsAffected() == 1
	globalClaimed, e := s.claimOAuthAuthorizationCode(ctx, tx, b.Provider, b.AppID, "mini", code, s.oauthHash("mini-state", state))
	if e != nil {
		return e
	}
	accepted := legacyClaimed && globalClaimed && status == "created" && enabled && !attempted
	var current bool
	if e = tx.QueryRow(ctx, "SELECT expires_at>clock_timestamp() FROM mini_oauth_states WHERE state_hash=$1", s.oauthHash("mini-state", state)).Scan(&current); e != nil {
		return e
	}
	accepted = accepted && current
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	if !accepted {
		return ErrOAuthState
	}
	return nil
}
