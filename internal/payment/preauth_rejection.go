package payment

import (
	"errors"

	adapter "goepay/internal/adapters/payment"
)

func authenticatedPreauthorizationRejection(err error, p adapter.Provider, operation string) bool {
	if p == nil || p.ID() != "alipay" {
		return false
	}
	proof, ok := adapter.VerifiedProviderRejection(err)
	return ok && proof.Provider == p.ID() && proof.Operation == operation
}

// A terminal capture failure can admit an unfreeze, so ordinary error codes
// and errors carrying uncertain outcomes cannot establish that failure.
func definiteAuthenticatedPreauthorizationRejection(err error, p adapter.Provider, operation string) bool {
	var rejected *adapter.ProviderError
	return authenticatedPreauthorizationRejection(err, p, operation) && errors.As(err, &rejected) && definiteProviderRejection(rejected)
}

func authorizationTradeNotFound(err error, p adapter.Provider) bool {
	proof, ok := adapter.VerifiedProviderRejection(err)
	return ok && p != nil && p.ID() == "alipay" && proof.Provider == p.ID() && proof.Operation == "alipay.trade.query" && proof.Code == "40004:ACQ.TRADE_NOT_EXIST"
}
