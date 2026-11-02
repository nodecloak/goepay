package server

import (
	"errors"
	"goepay/internal/payment"
	"strings"
	"testing"
)

func TestKYC233RequiredPaymentMessageExplainsMerchantAction(t *testing.T) {
	message := orderRejectedMessage(errors.Join(payment.ErrCertificationRequired, errors.New("private database detail")))
	if !strings.Contains(message, "实名认证") || strings.Contains(message, "private") {
		t.Fatal("payment refusal lost merchant action or leaked internal error", message)
	}
	human, code := submitFailureMessage(message)
	if code != "merchant_certification_required" || !strings.Contains(human, "实名认证") {
		t.Fatal("browser submit did not explain certification requirement", human, code)
	}
}
