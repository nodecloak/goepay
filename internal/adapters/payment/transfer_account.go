package payment

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// TransferAccountIdentity is a stable public payer identity, not a credential.
// An API key/certificate rotation must not rebind the underlying payer account.
type TransferAccountIdentity interface{ TransferAccountIdentity() string }

// TransferProductIdentity identifies the concrete wallet transfer API, so an
// unattended authority cannot silently move to a different payment product.
type TransferProductIdentity interface{ TransferProductIdentity() string }

func (w *WechatV2) TransferProductIdentity() string { return "wechat.v2.promotion.transfers" }
func (a *Alipay) TransferProductIdentity() string {
	if a.certificates != nil {
		return "alipay.fund.trans.uni.transfer"
	}
	return "alipay.fund.trans.toaccount.transfer"
}

func (w *WechatV2) TransferAccountIdentity() string { return w.config.MerchantID }
func (a *Alipay) TransferAccountIdentity() string {
	if a.config.SellerID != "" {
		return "seller:" + a.config.SellerID
	}
	return a.config.AppID
}

// TransferAccountScope keeps the public provider/application/account dimensions
// unambiguous. Internal test providers without account identity retain only the
// weaker application/provider scope that they actually advertise.
func TransferAccountScope(p Provider) (string, error) {
	if p == nil {
		return "", ErrInvalid
	}
	app, account, level := "", "", "provider"
	if identity, ok := p.(ApplicationIdentity); ok {
		app = identity.ApplicationID()
		level = "application"
		if app == "" {
			return "", ErrInvalid
		}
	}
	if identity, ok := p.(TransferAccountIdentity); ok {
		account = identity.TransferAccountIdentity()
		level = "account"
		if account == "" {
			return "", ErrInvalid
		}
	}
	for _, value := range []string{p.ID(), app, account} {
		if len(value) > 512 || !utf8.ValidString(value) || strings.ContainsAny(value, "\x00\r\n") {
			return "", ErrInvalid
		}
	}
	raw, e := json.Marshal([]string{p.ID(), app, account, level})
	return string(raw), e
}
