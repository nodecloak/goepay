package payment

import (
	"context"

	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
)

// These readers never acquire another connection. Authorization checks that
// already own a transaction must observe the original order/account on it.
func (s *Store) OrderByTradeNoTx(ctx context.Context, tx pgx.Tx, tradeNo string) (Order, error) {
	if s == nil || tx == nil {
		return Order{}, ErrInvalid
	}
	return scanOrder(tx.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE trade_no=$1", tradeNo))
}

func (s *Store) ProviderAttemptTx(ctx context.Context, tx pgx.Tx, tradeNo string) (ProviderAttempt, error) {
	if s == nil || tx == nil {
		return ProviderAttempt{}, ErrInvalid
	}
	return scanProviderAttempt(tx.QueryRow(ctx, "SELECT "+providerAttemptColumns+" FROM provider_attempts a JOIN orders o ON o.id=a.order_id WHERE o.trade_no=$1", tradeNo))
}

func (g ProviderFlow) ownedTx(ctx context.Context, tx pgx.Tx, merchantID int64, tradeNo string) (Order, ProviderAttempt, adapter.Provider, error) {
	if g.Store == nil || tx == nil {
		return Order{}, ProviderAttempt{}, nil, ErrInvalid
	}
	o, e := g.Store.OrderByTradeNoTx(ctx, tx, tradeNo)
	if e != nil || o.MerchantID != merchantID {
		return o, ProviderAttempt{}, nil, ErrNotFound
	}
	a, e := g.Store.ProviderAttemptTx(ctx, tx, tradeNo)
	if e != nil {
		return o, a, nil, e
	}
	// ForPaymentIntentTx restores both the sealed original parent and child
	// account. Calling either public wrapper here would acquire a second pool
	// connection while the original order may already be locked by this tx.
	g, _, e = g.ForPaymentIntentTx(ctx, tx, o.MerchantID, o.OutTradeNo, a.ChannelID)
	if e != nil {
		return o, a, nil, e
	}
	p := g.Providers[a.ChannelID]
	if p == nil || p.ID() != o.Type {
		return o, a, nil, adapter.ErrUnsupported
	}
	if e = g.Store.checkProviderAccount(ctx, tx, a.ID, p); e != nil {
		return o, a, nil, e
	}
	return o, a, p, nil
}

// ValidatePaymentAccountTx retains the original account authentication while
// using the caller's transaction. It performs no provider I/O.
func (g ProviderFlow) ValidatePaymentAccountTx(ctx context.Context, tx pgx.Tx, merchantID int64, tradeNo string) error {
	_, _, _, e := g.ownedTx(ctx, tx, merchantID, tradeNo)
	return e
}

func observationCheckTx(checks []func(context.Context, pgx.Tx) error) (func(context.Context, pgx.Tx) error, error) {
	if len(checks) != 1 || checks[0] == nil {
		return nil, ErrPreauthObservation
	}
	return checks[0], nil
}
