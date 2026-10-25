package payment

import (
	"context"
	"strconv"
)

func (w *WechatV2) ValidateScan(r ScanRequest) error {
	if r.ProfitSharing {
		if e := w.validateSharingV2Transport(false); e != nil {
			return e
		}
	}
	if !enabled("scan", w.config.Methods) {
		return ErrUnsupported
	}
	if !validScan(r, "wxpay") {
		return ErrInvalid
	}
	return nil
}
func (w *WechatV2) Scan(ctx context.Context, r ScanRequest) (PaymentEvent, error) {
	if e := w.ValidateScan(r); e != nil {
		return PaymentEvent{}, e
	}
	fields := map[string]string{"body": r.Subject, "out_trade_no": r.TradeNo, "total_fee": strconv.FormatInt(r.Amount, 10), "spbill_create_ip": r.ClientIP, "auth_code": r.AuthCode}
	if r.ProfitSharing {
		fields["profit_sharing"] = "Y"
	}
	out, e := w.call(ctx, "/pay/micropay", fields, true, false)
	if e != nil {
		return PaymentEvent{}, bindScanPending(e, w, r)
	}
	amount, e := strconv.ParseInt(out["total_fee"], 10, 64)
	if e != nil || amount != r.Amount || out["out_trade_no"] != r.TradeNo || out["transaction_id"] == "" || out["trade_type"] != "MICROPAY" || out["fee_type"] != "" && out["fee_type"] != "CNY" || out["openid"] == "" || !trustedBuyer(out["openid"]) {
		return PaymentEvent{}, &UnknownOutcome{Operation: "scan", Cause: ErrAuthentication}
	}
	return PaymentEvent{EventID: out["transaction_id"], TradeNo: r.TradeNo, ProviderTradeNo: out["transaction_id"], BuyerID: out["openid"], State: "SUCCESS", Currency: "CNY", Amount: amount}, nil
}

var _ ScanProvider = (*WechatV2)(nil)
