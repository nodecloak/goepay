package payment

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"goepay/internal/merchantpolicy"
)

// MatchesOrderIntent is read-only evidence for an authenticated wire replay.
// Every signed immutable field is compared by the canonical creation matcher;
// an external order number alone never exempts a new request from policy.
func (s *Store) MatchesOrderIntent(ctx context.Context, in CreateOrderInput) (bool, error) {
	in, err := normalizeCreateOrderInput(in)
	if err != nil {
		return false, err
	}
	order, err := s.Order(ctx, in.MerchantID, in.OutTradeNo)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err = validateOrderReplay(order, in); err != nil {
		return false, err
	}
	return true, nil
}

func paymentDomainAdmissionTx(ctx context.Context, tx pgx.Tx, in CreateOrderInput) (*merchantpolicy.PaymentDomainPolicy, error) {
	if in.NotifyURL == "" && in.ReturnURL == "" {
		return nil, nil
	}
	policy, err := merchantpolicy.ReadPaymentDomainPolicy(ctx, tx, in.MerchantID, true)
	if err == nil {
		err = policy.CheckURLs(ctx, tx, in.MerchantID, true, in.NotifyURL, in.ReturnURL)
	}
	if err != nil {
		return nil, paymentDomainError(err)
	}
	return &policy, nil
}

func paymentDomainError(err error) error {
	if errors.Is(err, merchantpolicy.ErrPaymentDomainChanged) {
		return ErrConflict
	}
	if errors.Is(err, merchantpolicy.ErrPaymentDomain) || errors.Is(err, merchantpolicy.ErrPaymentDomainPolicy) || errors.Is(err, merchantpolicy.ErrGroupPolicy) {
		return errors.Join(ErrState, err)
	}
	return err
}
