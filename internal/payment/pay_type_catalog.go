package payment

import adapter "goepay/internal/adapters/payment"

// catalogPayType reports one of the original platform's pay types (pre_type)
// on the generic collection path: group routing, group rates and limits,
// fees and referral costs. Provider-specific products (scan recovery,
// OAuth, sub-channels, payouts, sharing …) keep their own narrower checks.
func catalogPayType(t string) bool { return adapter.CatalogPayType(t) }
