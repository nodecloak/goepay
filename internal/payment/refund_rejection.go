package payment

import (
	"errors"

	adapter "goepay/internal/adapters/payment"
)

// A failed refund releases its original funding. Only an authenticated refusal
// from this provider's refund submission can justify that irreversible choice;
// query errors and uncertainty wrappers must keep the reserve available for
// reconciliation under the original refund number.
func definiteAuthenticatedRefundRejection(err error, p adapter.Provider) bool {
	if p == nil {
		return false
	}
	proof, ok := adapter.VerifiedProviderRejection(err)
	if !ok || proof.Provider != p.ID() {
		return false
	}
	switch proof.Provider {
	case "alipay":
		if proof.Operation != "alipay.trade.refund" {
			return false
		}
	case "wxpay":
		if proof.Operation != "/secapi/pay/refund" && proof.Operation != "/v3/refund/domestic/refunds" {
			return false
		}
	default:
		return false
	}
	var rejected *adapter.ProviderError
	return errors.As(err, &rejected) && definiteProviderRejection(rejected)
}
