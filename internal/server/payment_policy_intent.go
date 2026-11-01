package server

import (
	"goepay/internal/compat"
	"goepay/internal/ledger"
	"goepay/internal/payment"
)

func paymentPolicyRequestIntent(mid int64, legacy bool, p compat.Params) *payment.CreateOrderInput {
	amount, err := ledger.Parse(p["money"])
	if err != nil {
		return nil
	}
	name := p["name"]
	if p["type"] != "sandbox" {
		name = truncateUTF8Bytes(name, 127)
	}
	return &payment.CreateOrderInput{Version: wireVersion(legacy), MerchantID: mid, OutTradeNo: p["out_trade_no"], Name: name, Type: p["type"], Amount: amount, NotifyURL: p["notify_url"], ReturnURL: p["return_url"], Param: p["param"]}
}

func paymentPolicyOriginalIntent(o payment.Order) *payment.CreateOrderInput {
	return &payment.CreateOrderInput{Version: o.Version, MerchantID: o.MerchantID, OutTradeNo: o.OutTradeNo, Name: o.Name, Type: o.Type, Amount: o.Amount, NotifyURL: o.NotifyURL, ReturnURL: o.ReturnURL, Param: o.Param}
}
