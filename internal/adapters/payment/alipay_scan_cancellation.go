package payment

import (
	"context"
	"net/url"
)

func (a *Alipay) CanCancelScan() bool {
	return a != nil && a.config.EnableScanCancellation && enabled("scan", a.config.Methods)
}
func (a *Alipay) ValidateScanCancellation(r ScanCancellationRequest) error {
	if !a.CanCancelScan() {
		return ErrUnsupported
	}
	if e := a.validateCertificates(); e != nil {
		return e
	}
	u, e := url.Parse(a.config.GatewayURL)
	if e != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || a.config.SellerID == "" || !scanCancellationNumber.MatchString(r.TradeNo) || r.Amount <= 0 || r.Amount > 999999999999 || r.ProviderTradeNo != "" && !scanCancellationNumber.MatchString(r.ProviderTradeNo) {
		return ErrInvalid
	}
	_, e = TransferAccountScope(a)
	return e
}
func (a *Alipay) CancelScan(ctx context.Context, r ScanCancellationRequest) (ScanCancellationResult, error) {
	if ctx == nil || ctx.Err() != nil {
		return ScanCancellationResult{}, ErrInvalid
	}
	if e := a.ValidateScanCancellation(r); e != nil {
		return ScanCancellationResult{}, e
	}
	out, e := a.call(ctx, "alipay.trade.cancel", map[string]any{"out_trade_no": r.TradeNo}, true)
	unknown := func() (ScanCancellationResult, error) {
		return ScanCancellationResult{}, &UnknownOutcome{Operation: "scan cancellation", Cause: ErrAuthentication}
	}
	if e != nil {
		return unknown()
	}
	no, action := text(out, "trade_no"), text(out, "action")
	if no != "" && !scanCancellationNumber.MatchString(no) {
		return unknown()
	}
	for field, expected := range map[string]string{"seller_id": a.config.SellerID, "app_id": a.config.AppID} {
		if _, present := out[field]; present && text(out, field) != expected {
			return unknown()
		}
	}
	if text(out, "out_trade_no") != r.TradeNo || text(out, "retry_flag") != "N" || (action != "close" && action != "refund") || r.ProviderTradeNo != "" && no != r.ProviderTradeNo || action == "refund" && no == "" {
		return unknown()
	}
	return sealScanCancellation(a, r, no, action, "alipay.trade.cancel", "alipay_rsa2", out)
}

var _ ScanCancellationProvider = (*Alipay)(nil)
