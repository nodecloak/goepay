package payment

import (
	"crypto/hmac"
	adapter "goepay/internal/adapters/payment"
)

func parentExecutionAccountHash(s *Store, p adapter.Provider) ([]byte, error) {
	scope, err := adapter.TransferAccountScope(p)
	if err != nil {
		return nil, err
	}
	return s.providerAccountHash(scope)
}

// SelectParentPayment changes only the request's pure-parent execution SDK.
// A child account's authenticated SDK is never replaced by a parent profile.
func (g ProviderFlow) SelectParentPayment(mid, id int64, method string, mini bool) (ProviderFlow, error) {
	if bound, ok := g.boundParents[id]; ok && bound.child != nil {
		return g, nil
	}
	b := g.parentBindings[id]
	if b == nil || b.execution.PaymentSelection == nil {
		return g, nil
	}
	if b.merchant != mid {
		return g, ErrConflict
	}
	selected, variant, err := b.execution.PaymentSelection(method, mini, mid)
	if err != nil || selected == nil || selected.ID() != b.account.kind {
		return g, ErrConflict
	}
	if b.variantFrozen && variant != b.paymentVariant {
		return g, ErrConflict
	}
	if !b.variantFrozen && b.execution.PaymentFreshAllowed != nil && !b.execution.PaymentFreshAllowed(variant) {
		return g, ErrConflict
	}
	if !b.variantFrozen && b.execution.PaymentAllowed != nil && !b.execution.PaymentAllowed(variant, mid) {
		return g, ErrConflict
	}
	next := *b
	next.paymentVariant = variant
	execution := b.execution
	execution.Payment = selected
	return g.withParentExecution(&next, execution), nil
}

func (g ProviderFlow) selectOriginalParentPayment(a parentChannelAccount, source parentPaymentSource, execution adapter.ParentChannelExecution) (adapter.ParentChannelExecution, error) {
	selected := execution.Payment
	variant := ""
	if execution.PaymentSelection != nil {
		var err error
		selected, variant, err = execution.PaymentSelection(source.Method, source.PaymentVariant == "mini", source.Merchant)
		if err != nil {
			return adapter.ParentChannelExecution{}, ErrConflict
		}
	}
	if variant != source.PaymentVariant {
		return adapter.ParentChannelExecution{}, ErrConflict
	}
	hash, err := parentExecutionAccountHash(g.Store, selected)
	if err != nil {
		return adapter.ParentChannelExecution{}, err
	}
	expected := a.accountHash
	if source.PaymentVariant != "" {
		expected = source.ExecutionAccountHash
	}
	if !hmac.Equal(hash, expected) {
		return adapter.ParentChannelExecution{}, ErrConflict
	}
	execution.Payment = selected
	return execution, nil
}
