package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"
	"goepay/internal/backoffice"
	"goepay/internal/cryptopay"
	"goepay/internal/payment"
)

// Absence selects the authorized channel policy. An explicit value is never
// silently discarded, including when a deployment has no configured channels.
func cryptoChannelID(raw string) (int64, error) {
	if raw == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != raw {
		return 0, cryptopay.ErrInvalid
	}
	return id, nil
}

var errCryptoVerificationRequired = errors.New("请通过标准支付提交入口完成支付安全验证")

// Native collection and cashier choices have no interactive verification proof.
// Apply the current policy in the invoice transaction; a new collection that
// needs a challenge must return to the standard signed payment entry point.
// This is deliberately not installed on that entry point, whose consumed proof
// must survive unchanged through cryptoPaymentContext.
func (s *Server) cryptoUnverifiedPaymentContext(ctx context.Context, r *http.Request, merchant int64, outTradeNo, clientIP string) context.Context {
	if clientIP == "" {
		clientIP, _, _ = net.SplitHostPort(r.RemoteAddr)
		if clientIP == "" {
			clientIP = r.RemoteAddr
		}
	}
	checked := s.paymentDefendContext206(ctx, &defend206Admission{merchantID: merchant, outTradeNo: outTradeNo, clientIP: clientIP})
	inspected, existing := false, false
	return payment.WithPaymentDefend206Guard(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if !inspected {
			// Evaluate before insertion and retain the result for this request's
			// final authorization. Its own new row is never a replay exemption.
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM crypto_invoices WHERE merchant_id=$1 AND out_trade_no=$2)`, merchant, outTradeNo).Scan(&existing); err != nil {
				return err
			}
			inspected = true
		}
		if existing {
			// The invoice service still checks the actor and exact original
			// payload; retrieving it cannot allocate a new payment address.
			return nil
		}
		if err := payment.CheckPaymentDefend206Tx(checked, tx); err != nil {
			if errors.Is(err, payment.ErrState) {
				return errCryptoVerificationRequired
			}
			return err
		}
		return nil
	})
}

func (s *Server) cryptoPaymentContext(ctx context.Context, r *http.Request, merchant int64, name, notifyURL, returnURL, buyerIP, buyer string) context.Context {
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peer = r.RemoteAddr
	}
	return cryptopay.WithAuthority(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.cryptoChannelNamespace(ctx, tx); err != nil {
			return err
		}
		var qq string
		if err := tx.QueryRow(ctx, `SELECT COALESCE(CASE WHEN jsonb_typeof(profile->'qq')='string' THEN profile->>'qq' END,'') FROM merchants WHERE id=$1 FOR UPDATE`, merchant).Scan(&qq); err != nil {
			return err
		}
		policy := backoffice.New(s.Pool, s.Store)
		p := backoffice.PaymentContext{MerchantID: merchant, ClientIP: peer, MerchantQQ: qq, Name: name, NotifyURL: notifyURL, ReturnURL: returnURL, Buyer: buyer}
		if err := policy.CheckPaymentTx(ctx, tx, p); err != nil {
			return err
		}
		if buyerIP != "" && buyerIP != peer {
			if net.ParseIP(buyerIP) == nil {
				return cryptopay.ErrInvalid
			}
			p.ClientIP = buyerIP
			if err := policy.CheckPaymentTx(ctx, tx, p); err != nil {
				return err
			}
		}
		return payment.CheckPaymentDefend206Tx(ctx, tx)
	})
}

func (s *Server) cryptoChannelNamespace(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) error {
	ids := s.reservedPaymentChannelIDs()
	if len(ids) == 0 {
		return nil
	}
	var collision bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM crypto_channels WHERE id=ANY($1::bigint[]))`, ids).Scan(&collision); err != nil {
		return err
	}
	if collision {
		return cryptopay.ErrConflict
	}
	return nil
}

// Only public metadata for this merchant is returned. Grants, another
// merchant's policy, wallet addresses and signing configuration stay private.
func (s *Server) cryptoChannelChoices(w http.ResponseWriter, r *http.Request, merchant int64) {
	items := []map[string]any{}
	if s.Crypto != nil && merchant > 0 {
		if err := s.cryptoChannelNamespace(r.Context(), s.Pool); err != nil {
			cryptoError(w, err)
			return
		}
		channels, err := s.Crypto.AvailableChannels(r.Context(), merchant, "")
		if err != nil {
			cryptoError(w, err)
			return
		}
		for _, c := range channels {
			items = append(items, map[string]any{"id": strconv.FormatInt(c.ID, 10), "name": c.Name, "network": c.Network, "fee_bps": c.FeeBPS, "min_amount": c.MinAmount, "max_amount": c.MaxAmount, "day_limit": c.DayLimit})
		}
	}
	response(w, 200, 0, "", map[string]any{"items": items})
}
