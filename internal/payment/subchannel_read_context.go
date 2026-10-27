package payment

import (
	"context"
	"crypto/hmac"
	"github.com/jackc/pgx/v5"
)

// SubchannelTestOrder reads the original typed route and born order proof. It
// never queries a provider, allocates an intent, or infers success from an attempt.
func (s *Store) SubchannelTestOrder(ctx context.Context, mid, child int64, no string) (Order, ProviderAttempt, error) {
	var o Order
	var a ProviderAttempt
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return o, a, e
	}
	defer tx.Rollback(ctx)
	o, e = scanOrder(tx.QueryRow(ctx, `SELECT `+orderColumns+` FROM orders WHERE merchant_id=$1 AND out_trade_no=$2`, mid, no))
	if e != nil {
		return o, a, e
	}
	route, e := s.readChildRoute(ctx, tx, mid, no)
	if e != nil {
		return o, a, e
	}
	if route == nil || route.child != child {
		return o, a, ErrConflict
	}
	quote, e := s.validateOrderRouteFeeTerms(ctx, tx, o)
	if e != nil {
		return o, a, e
	}
	if quote == nil || quote.facts.Child == nil || !hmac.Equal(quote.facts.Child.SourceProof, route.proof) {
		return o, a, ErrConflict
	}
	a, e = scanProviderAttempt(tx.QueryRow(ctx, `SELECT `+providerAttemptColumns+` FROM provider_attempts a JOIN orders o ON o.id=a.order_id WHERE o.id=$1 AND o.merchant_id=$2`, o.ID, mid))
	if e == ErrNotFound {
		a = ProviderAttempt{OrderID: o.ID, TradeNo: o.TradeNo, ChannelID: route.parent, State: "not_submitted"}
		e = nil
	}
	if e != nil {
		return o, a, e
	}
	if a.ChannelID != route.parent {
		return o, a, ErrConflict
	}
	return o, a, tx.Commit(ctx)
}

// Caller must already have authenticated the original collection-code bearer.
// The owner constraint cannot be replaced by a client merchant identifier.
func (s *Store) CollectionSubchannel(ctx context.Context, mid, id int64) (PaymentSubchannel, error) {
	return s.readSubchannel(ctx, s.pool, s.pool.QueryRow(ctx, `SELECT `+childColumns+` FROM payment_subchannels WHERE id=$1 AND merchant_id=$2`, id, mid))
}
