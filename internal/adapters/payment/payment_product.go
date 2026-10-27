package payment

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// PaymentProductIdentity describes the trusted receiving payment API, distinct
// from transfer products and independent of optional profit-sharing enablement.
type PaymentProductIdentity interface{ PaymentProductIdentity() string }

func (*WechatV2) PaymentProductIdentity() string { return "wechat.v2.payment.single-direct" }
func (*WechatV3) PaymentProductIdentity() string { return "wechat.v3.payment.direct" }
func (a *Alipay) PaymentProductIdentity() string {
	raw, _ := json.Marshal([]any{"alipay.openapi.payment", a.config.NativeOrderCode, a.config.PreauthVoucher, a.config.PreauthVoucherProductCode})
	return string(raw)
}

func PaymentProductScope(p Provider) (string, error) {
	if p == nil {
		return "", ErrInvalid
	}
	// Internal legacy providers expose only their actual concrete runtime type.
	// The deployed Alipay/Wechat adapters above declare their precise protocol.
	value := fmt.Sprintf("runtime:%T", p)
	if named, ok := p.(PaymentProductIdentity); ok {
		value = named.PaymentProductIdentity()
	}
	if value == "" || len(value) > 512 || !utf8.ValidString(value) || strings.ContainsAny(value, "\x00\r\n") {
		return "", ErrInvalid
	}
	return value, nil
}
