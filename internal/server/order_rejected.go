package server

import (
	"errors"

	"goepay/internal/payment"
)

// orderRejectedMessage is what the payment API tells the merchant when order
// creation is refused. A deposit below the platform minimum says so, as
// epay_pro's 当前平台要求最低缴纳保证金 notice does; every other refusal keeps the
// generic message.
func orderRejectedMessage(e error) string {
	if errors.Is(e, payment.ErrCertificationRequired) {
		return "商户尚未完成实名认证，暂不能发起支付"
	}
	if errors.Is(e, payment.ErrDepositBelowMinimum) {
		return "保证金低于平台最低要求，暂不能发起支付"
	}
	return "order rejected"
}
