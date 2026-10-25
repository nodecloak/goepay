package payment

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/jackc/pgx/v5"
	"strings"
)

var ErrOAuthState = errors.New("OAuth state or authorization code rejected")

type OAuthBinding struct {
	TradeNo                          string
	MerchantID, ChannelID            int64
	Provider, AppID, Method, Purpose string
}

func validOAuthBinding(b OAuthBinding) bool {
	return b.TradeNo != "" && b.MerchantID > 0 && b.ChannelID > 0 && (b.Provider == "wxpay" || b.Provider == "alipay") && b.AppID != "" && len(b.AppID) <= 128 && b.Method == "jsapi" && b.Purpose == "payment-jsapi"
}
func (s *Store) oauthHash(kind, value string) []byte {
	mac := hmac.New(sha256.New, s.indexKey)
	mac.Write([]byte("goepay/oauth/v1/" + kind + "\x00" + value))
	return mac.Sum(nil)
}

const oauthBindingColumns = "o.trade_no,o.merchant_id,i.channel_id,i.provider,i.app_id,i.method,i.purpose"

func scanOAuthBinding(row pgx.Row) (OAuthBinding, error) {
	var b OAuthBinding
	e := row.Scan(&b.TradeNo, &b.MerchantID, &b.ChannelID, &b.Provider, &b.AppID, &b.Method, &b.Purpose)
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrNotFound
	}
	return b, e
}
func (s *Store) OAuthPaymentIntent(ctx context.Context, tradeNo string) (OAuthBinding, error) {
	return scanOAuthBinding(s.pool.QueryRow(ctx, "SELECT "+oauthBindingColumns+" FROM oauth_payment_intents i JOIN orders o ON o.id=i.order_id WHERE o.trade_no=$1", tradeNo))
}
func (s *Store) PrepareOAuthPayment(ctx context.Context, b OAuthBinding) error {
	if !validOAuthBinding(b) {
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
	old, e := scanOAuthBinding(tx.QueryRow(ctx, "SELECT "+oauthBindingColumns+" FROM oauth_payment_intents i JOIN orders o ON o.id=i.order_id WHERE i.order_id=$1", id))
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
	if _, e = tx.Exec(ctx, "INSERT INTO oauth_payment_intents(order_id,channel_id,provider,app_id,method,purpose) VALUES($1,$2,$3,$4,$5,$6)", id, b.ChannelID, b.Provider, b.AppID, b.Method, b.Purpose); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func validOAuthBrowser(v string) bool {
	if len(v) != 64 {
		return false
	}
	_, e := hex.DecodeString(v)
	return e == nil
}
func (s *Store) IssueOAuthState(ctx context.Context, b OAuthBinding, browser string) (string, error) {
	if !validOAuthBinding(b) || !validOAuthBrowser(browser) {
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
	stored, e := scanOAuthBinding(tx.QueryRow(ctx, "SELECT "+oauthBindingColumns+" FROM oauth_payment_intents i JOIN orders o ON o.id=i.order_id WHERE i.order_id=$1", id))
	if e != nil || stored != b {
		return "", ErrOAuthState
	}
	var active int
	if e = tx.QueryRow(ctx, "SELECT count(*) FROM oauth_states WHERE order_id=$1 AND consumed_at IS NULL AND expires_at>now()", id).Scan(&active); e != nil {
		return "", e
	}
	if active >= 8 {
		return "", ErrState
	}
	_, e = tx.Exec(ctx, "INSERT INTO oauth_states(state_hash,order_id,merchant_id,channel_id,provider,app_id,method,purpose,browser_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)", s.oauthHash("state", state), id, b.MerchantID, b.ChannelID, b.Provider, b.AppID, b.Method, b.Purpose, s.oauthHash("browser", browser))
	if e != nil {
		return "", e
	}
	if e = tx.Commit(ctx); e != nil {
		return "", e
	}
	return state, nil
}
func (s *Store) ConsumeOAuthState(ctx context.Context, state, browser, code string, b OAuthBinding) error {
	if !validOAuthBinding(b) || !validOAuthBrowser(state) || !validOAuthBrowser(browser) || code == "" || len(code) > 4096 || strings.TrimSpace(code) != code || strings.HasPrefix(code, "@") || strings.ContainsAny(code, "\r\n\x00") {
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
	tag, e := tx.Exec(ctx, "UPDATE oauth_states SET consumed_at=clock_timestamp() WHERE state_hash=$1 AND order_id=$2 AND merchant_id=$3 AND channel_id=$4 AND provider=$5 AND app_id=$6 AND method=$7 AND purpose=$8 AND browser_hash=$9 AND consumed_at IS NULL AND expires_at>clock_timestamp()", s.oauthHash("state", state), id, b.MerchantID, b.ChannelID, b.Provider, b.AppID, b.Method, b.Purpose, s.oauthHash("browser", browser))
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrOAuthState
	}
	tag, e = tx.Exec(ctx, "INSERT INTO oauth_code_claims(provider,app_id,code_hash,state_hash) VALUES($1,$2,$3,$4) ON CONFLICT(provider,app_id,code_hash) DO NOTHING", b.Provider, b.AppID, s.oauthHash("code", code), s.oauthHash("state", state))
	if e != nil {
		return e
	}
	legacyClaimed := tag.RowsAffected() == 1
	globalClaimed, e := s.claimOAuthAuthorizationCode(ctx, tx, b.Provider, b.AppID, "web", code, s.oauthHash("state", state))
	if e != nil {
		return e
	}
	accepted := legacyClaimed && globalClaimed && status == "created" && enabled && !attempted
	var current bool
	if e = tx.QueryRow(ctx, "SELECT expires_at>clock_timestamp() FROM oauth_states WHERE state_hash=$1", s.oauthHash("state", state)).Scan(&current); e != nil {
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
