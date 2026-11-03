package server

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"goepay/internal/payment"
)

// A valid cashier token can resume its original payment type after the new
// collection policy closes. providerCreate still compares the entire original
// request and restores the sealed attempt; this does not admit another order.
func (s *Server) cashierIntentTypes236(r *http.Request, in cashierIntent) ([]cashierPayType, error) {
	if s.Pool == nil {
		return nil, payment.ErrInvalid
	}
	var kind string
	err := s.Pool.QueryRow(r.Context(), `SELECT o.type FROM orders o WHERE o.merchant_id=$1 AND o.out_trade_no=$2 AND EXISTS(SELECT 1 FROM provider_attempts a WHERE a.order_id=o.id) LIMIT 1`, in.MerchantID, in.OutTradeNo).Scan(&kind)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if err == nil {
		for _, t := range originalPayTypes {
			if t.Name == kind {
				return []cashierPayType{{Name: t.Name, ShowName: t.ShowName}}, nil
			}
		}
		return nil, payment.ErrInvalid
	}
	return s.cashierAllowedTypes(r, in.MerchantID), nil
}
