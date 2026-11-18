package server

import (
	"context"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/backoffice"
	"goepay/internal/compat"
	"goepay/internal/cryptopay"
	"goepay/internal/ledger"
	"goepay/internal/payment"
)

const cryptoQuoteUnavailable = "USDT CNY quote expired or unavailable"

func (s *Server) cryptoQuoteOpen(now time.Time) bool {
	return s.Crypto != nil && s.CryptoCNYPerUSDT != "" && now.Before(s.CryptoQuoteValidUntil)
}

// CNY stays an integer number of cents throughout. Round the USDT quote upward
// to one millionth so a quoted payment never underfunds the original price.
func CryptoQuoteCNY(cents int64, rate string) (string, error) {
	price, e := cryptopay.ParseAmount(rate)
	if e != nil || cents <= 0 {
		return "", cryptopay.ErrInvalid
	}
	numerator := new(big.Int).Mul(big.NewInt(cents), big.NewInt(10000000000))
	amount, remainder := new(big.Int), new(big.Int)
	amount.QuoRem(numerator, big.NewInt(price), remainder)
	if remainder.Sign() > 0 {
		amount.Add(amount, big.NewInt(1))
	}
	if !amount.IsInt64() {
		return "", cryptopay.ErrInvalid
	}
	result := cryptopay.FormatAmount(amount.Int64())
	if _, e = cryptopay.ParseAmount(result); e != nil {
		return "", e
	}
	return result, nil
}

// NormalizeCryptoPublicOrigin accepts a deployment origin, never a request Host
// or forwarding header. Rebuilding the value discards no ambiguous URL parts.
func NormalizeCryptoPublicOrigin(raw string) (string, error) {
	if raw == "" || raw != strings.TrimSpace(raw) || strings.Contains(raw, "#") {
		return "", cryptopay.ErrInvalid
	}
	u, e := url.Parse(raw)
	if e != nil || u.Opaque != "" || u.Host == "" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || u.Path != "" && u.Path != "/" {
		return "", cryptopay.ErrInvalid
	}
	scheme, host := strings.ToLower(u.Scheme), strings.ToLower(u.Hostname())
	if scheme != "https" && scheme != "http" {
		return "", cryptopay.ErrInvalid
	}
	address := net.ParseIP(host)
	if strings.Contains(host, ":") && address == nil || strings.HasSuffix(u.Host, ":") {
		return "", cryptopay.ErrInvalid
	}
	if port := u.Port(); port != "" {
		number, e := strconv.Atoi(port)
		if e != nil || number < 1 || number > 65535 {
			return "", cryptopay.ErrInvalid
		}
	}
	if scheme == "http" && host != "localhost" && (address == nil || !address.IsLoopback()) {
		return "", cryptopay.ErrInvalid
	}
	return scheme + "://" + strings.ToLower(u.Host), nil
}

// Invoked after the existing merchant signature and payment policy gates.
func (s *Server) cryptoCompatibilityCreate(w http.ResponseWriter, r *http.Request, legacy bool, merchant payment.Merchant, p compat.Params, route string) {
	if s.Crypto == nil {
		s.wire(w, legacy, nil, "USDT collection is not configured")
		return
	}
	origin, originError := NormalizeCryptoPublicOrigin(s.CryptoPublicOrigin)
	if originError != nil {
		s.wire(w, legacy, nil, "trusted USDT public origin unavailable")
		return
	}
	cents, e := ledger.Parse(p["money"])
	channelID, channelError := cryptoChannelID(p["channel_id"])
	if e != nil || channelError != nil || p["subchannel_id"] != "" || cents <= 0 || p["out_trade_no"] == "" || !outTradeNoPattern.MatchString(p["out_trade_no"]) || p["method"] != "" && p["method"] != "jump" && p["method"] != "native" {
		s.wire(w, legacy, nil, "invalid USDT payment request")
		return
	}
	previous, lookupError := s.Crypto.FindInvoice(r.Context(), merchant.ID, "", p["out_trade_no"])
	if lookupError == nil {
		network, name := p["network"], p["name"]
		if network == "" {
			network = previous.Network
		}
		if name == "" {
			name = "product"
		}
		if channelID != 0 && previous.ChannelID != channelID || previous.CNYAmount != cents || previous.Network != network || previous.Name != name || previous.Param != p["param"] || previous.NotifyURL != p["notify_url"] || previous.ReturnURL != p["return_url"] || previous.Version != wireVersion(legacy) {
			s.wire(w, legacy, nil, "USDT request conflicts with the original order")
			return
		}
		s.cryptoCompatibilityPayment(w, r, legacy, previous, route, origin)
		return
	}
	if !errors.Is(lookupError, cryptopay.ErrNotFound) {
		s.wire(w, legacy, nil, "USDT order namespace unavailable")
		return
	}
	if !s.cryptoQuoteOpen(time.Now()) {
		s.wire(w, legacy, nil, cryptoQuoteUnavailable)
		return
	}
	amount, quoteError := CryptoQuoteCNY(cents, s.CryptoCNYPerUSDT)
	network := p["network"]
	if network == "" && channelID == 0 && !s.Crypto.RequireChannels {
		network = s.CryptoDefaultNetwork
	}
	if quoteError != nil {
		s.wire(w, legacy, nil, "invalid USDT payment request")
		return
	}
	ctx := cryptopay.WithAuthority(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		var revision int64
		var enabled bool
		var pay int
		if e := tx.QueryRow(ctx, "SELECT config_revision,enabled,COALESCE((to_jsonb(merchants)->>'pay')::int,1) FROM merchants WHERE id=$1 FOR UPDATE", merchant.ID).Scan(&revision, &enabled, &pay); e != nil {
			return e
		}
		if !enabled || pay != 1 || revision != merchant.ConfigRevision {
			return cryptopay.ErrConflict
		}
		return nil
	})
	ctx = s.cryptoPaymentContext(ctx, r, merchant.ID, p["name"], p["notify_url"], p["return_url"], p["clientip"], p["sub_openid"])
	invoice, e := s.Crypto.CreateInvoice(ctx, cryptopay.CreateInvoiceInput{
		QuoteRate: s.CryptoCNYPerUSDT, QuoteValidUntil: s.CryptoQuoteValidUntil, ChannelID: channelID,
		MerchantID: merchant.ID, OutTradeNo: p["out_trade_no"], Network: network, Amount: amount, CNYAmount: cents,
		NotifyURL: p["notify_url"], ReturnURL: p["return_url"], Name: p["name"], Param: p["param"],
		Version: wireVersion(legacy), ExpiresIn: 15 * time.Minute,
	})
	if e != nil {
		if errors.Is(e, cryptopay.ErrQuoteExpired) {
			s.wire(w, legacy, nil, cryptoQuoteUnavailable)
			return
		}
		if errors.Is(e, errCryptoVerificationRequired) {
			s.wire(w, legacy, nil, errCryptoVerificationRequired.Error())
			return
		}
		if errors.Is(e, backoffice.ErrForbidden) {
			s.wire(w, legacy, nil, paymentPolicyMessage(e))
			return
		}
		s.wire(w, legacy, nil, "USDT invoice could not be created; query the original order before retrying")
		return
	}
	s.cryptoCompatibilityPayment(w, r, legacy, invoice, route, origin)
}

func (s *Server) cryptoCompatibilityPayment(w http.ResponseWriter, r *http.Request, legacy bool, invoice cryptopay.Invoice, route, origin string) {
	checkout := origin + "/crypto/checkout/" + url.PathEscape(invoice.ID) + "?" + url.Values{"token": {invoice.CheckoutToken}}.Encode()
	if route == "/api/pay/submit" || route == "/cashier" {
		http.Redirect(w, r, checkout, http.StatusFound)
		return
	}
	data := map[string]any{"trade_no": "USDT" + invoice.ID, "out_trade_no": invoice.OutTradeNo, "type": "usdt", "channel_id": strconv.FormatInt(invoice.ChannelID, 10), "network": invoice.Network, "usdt_amount": invoice.Amount, "money": ledger.Format(invoice.CNYAmount), "pay_type": "jump", "pay_info": checkout, "url": checkout}
	if legacy {
		data["payurl"] = checkout
	}
	s.wire(w, legacy, data, "")
}

// Query the independent USDT record before any legacy CNY operation. Explicit
// USDT identifiers fail closed even when the crypto service is disabled.
func (s *Server) cryptoCompatibilityOperation(w http.ResponseWriter, r *http.Request, legacy bool, merchant int64, p compat.Params, route string) bool {
	if route != "/api/pay/query" && route != "/api/pay/refund" && route != "/api/pay/refundquery" && route != "/api/pay/close" {
		return false
	}
	explicit := strings.HasPrefix(p["trade_no"], "USDT") || p["type"] == "usdt"
	if s.Crypto == nil {
		if explicit {
			s.wire(w, legacy, nil, "USDT service unavailable")
			return true
		}
		return false
	}
	invoice, e := s.Crypto.FindInvoice(r.Context(), merchant, p["trade_no"], p["out_trade_no"])
	if e != nil {
		if errors.Is(e, cryptopay.ErrNotFound) && !explicit {
			return false
		}
		s.wire(w, legacy, nil, "USDT order not found or unavailable")
		return true
	}
	if p["trade_no"] == "" && s.Store != nil {
		_, legacyError := s.Store.Order(r.Context(), merchant, p["out_trade_no"])
		if legacyError == nil {
			s.wire(w, legacy, nil, "ambiguous order number; provide the original trade_no")
			return true
		}
		if !errors.Is(legacyError, payment.ErrNotFound) {
			s.wire(w, legacy, nil, "order namespace unavailable")
			return true
		}
	}
	if route != "/api/pay/query" {
		s.wire(w, legacy, nil, "USDT orders use original-chain refund requests in the merchant console; legacy CNY refund and close are unavailable")
		return true
	}
	status := 0
	if invoice.State == "paid" || invoice.State == "overpaid" {
		status = 1
	}
	s.wire(w, legacy, map[string]any{"trade_no": "USDT" + invoice.ID, "out_trade_no": invoice.OutTradeNo, "pid": invoice.MerchantID, "name": invoice.Name, "type": "usdt", "money": ledger.Format(invoice.CNYAmount), "usdt_amount": invoice.Amount, "usdt_received": invoice.Received, "network": invoice.Network, "status": status, "crypto_state": invoice.State, "param": invoice.Param}, "")
	return true
}
