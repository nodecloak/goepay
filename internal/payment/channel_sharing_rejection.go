package payment

import (
	"errors"
	"slices"

	adapter "goepay/internal/adapters/payment"
)

// Marking a sharing operation failed can reopen recipient capacity, authorize
// unfreezing or allow a new relationship request. Require the original kind of
// authenticated submission refusal before granting any of those rights.
func definiteAuthenticatedSharingRejection(err error, provider, action string) bool {
	proof, ok := adapter.VerifiedProviderRejection(err)
	if !ok || proof.Provider != provider {
		return false
	}
	var operations []string
	switch provider + ":" + action {
	case "alipay:submit":
		operations = []string{"alipay.trade.order.settle"}
	case "alipay:return":
		operations = []string{"alipay.trade.refund"}
	case "alipay:bind":
		operations = []string{"alipay.trade.royalty.relation.bind"}
	case "alipay:unbind":
		operations = []string{"alipay.trade.royalty.relation.unbind"}
	case "wxpay:submit":
		operations = []string{"/v3/profitsharing/orders", "/secapi/pay/profitsharing"}
	case "wxpay:return":
		operations = []string{"/v3/profitsharing/return-orders", "/secapi/pay/profitsharingreturn"}
	case "wxpay:finish":
		operations = []string{"/v3/profitsharing/orders/unfreeze", "/secapi/pay/profitsharingfinish"}
	case "wxpay:bind":
		operations = []string{"/v3/profitsharing/receivers/add", "/pay/profitsharingaddreceiver"}
	case "wxpay:unbind":
		operations = []string{"/v3/profitsharing/receivers/delete", "/pay/profitsharingremovereceiver"}
	}
	if !slices.Contains(operations, proof.Operation) {
		return false
	}
	var rejected *adapter.ProviderError
	if !errors.As(err, &rejected) {
		return false
	}
	if definiteProviderRejection(rejected) {
		return true
	}
	return (action == "bind" || action == "unbind") && (rejected.Code == "SHARING_RELATION_FAIL" || rejected.Code == "NO_AUTH" || rejected.Code == "LACK_PARAMS" || rejected.Code == "APPID_MCHID_NOT_MATCH")
}
