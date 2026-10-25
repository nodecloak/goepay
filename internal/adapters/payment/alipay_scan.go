package payment

import "context"

func (a *Alipay) ValidateScan(r ScanRequest) error {
	if r.ProfitSharing {
		return ErrUnsupported
	}
	if e := a.validateCertificates(); e != nil {
		return e
	}
	if !enabled("scan", a.config.Methods) {
		return ErrUnsupported
	}
	if !validScan(r, "alipay") || !validPaymentURL(r.NotifyURL) {
		return ErrInvalid
	}
	return nil
}
func (a *Alipay) Scan(ctx context.Context, r ScanRequest) (PaymentEvent, error) {
	if e := a.ValidateScan(r); e != nil {
		return PaymentEvent{}, e
	}
	biz := map[string]any{"out_trade_no": r.TradeNo, "total_amount": amountString(r.Amount), "subject": r.Subject, "auth_code": r.AuthCode, "scene": "bar_code", "business_params": map[string]string{"mc_create_trade_ip": r.ClientIP}}
	if a.config.SellerID != "" {
		biz["seller_id"] = a.config.SellerID
	}
	out, e := a.call(ctx, "alipay.trade.pay", biz, true, map[string]string{"notify_url": r.NotifyURL})
	if e != nil {
		return PaymentEvent{}, bindScanPending(e, a, r)
	}
	amount, e := parseAmount(text(out, "total_amount"))
	buyer := text(out, "buyer_user_id")
	if buyer == "" {
		buyer = text(out, "buyer_open_id")
	}
	if e != nil || amount != r.Amount || text(out, "out_trade_no") != r.TradeNo || text(out, "trade_no") == "" || !trustedBuyer(buyer) || text(out, "trans_currency") != "" && text(out, "trans_currency") != "CNY" {
		return PaymentEvent{}, &UnknownOutcome{Operation: "scan", Cause: ErrAuthentication}
	}
	return PaymentEvent{EventID: text(out, "trade_no"), TradeNo: r.TradeNo, ProviderTradeNo: text(out, "trade_no"), BuyerID: buyer, State: "SUCCESS", Currency: "CNY", Amount: amount}, nil
}

var _ ScanProvider = (*Alipay)(nil)
