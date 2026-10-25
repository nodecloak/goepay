package payment

import (
	"context"
	"net/http"
	"net/url"
)

const wechatScanReversePath = "/secapi/pay/reverse"

func wechatClientCertificateConfigured(client *http.Client) bool {
	if client == nil {
		return false
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.TLSClientConfig == nil || transport.TLSClientConfig.InsecureSkipVerify {
		return false
	}
	for _, certificate := range transport.TLSClientConfig.Certificates {
		if len(certificate.Certificate) > 0 && certificate.PrivateKey != nil {
			return true
		}
	}
	return false
}
func (w *WechatV2) CanCancelScan() bool {
	return w != nil && w.config.EnableScanCancellation && enabled("scan", w.config.Methods) && wechatClientCertificateConfigured(w.config.RefundClient)
}
func (w *WechatV2) ValidateScanCancellation(r ScanCancellationRequest) error {
	if !w.CanCancelScan() {
		return ErrUnsupported
	}
	u, e := url.Parse(w.config.BaseURL)
	if e != nil || u.Scheme != "https" || !scanCancellationNumber.MatchString(r.TradeNo) || len(r.TradeNo) > 32 || r.Amount <= 0 || r.Amount > 999999999999 || r.ProviderTradeNo != "" && (!scanCancellationNumber.MatchString(r.ProviderTradeNo) || len(r.ProviderTradeNo) > 32) {
		return ErrInvalid
	}
	_, e = TransferAccountScope(w)
	return e
}
func (w *WechatV2) CancelScan(ctx context.Context, r ScanCancellationRequest) (ScanCancellationResult, error) {
	if ctx == nil || ctx.Err() != nil {
		return ScanCancellationResult{}, ErrInvalid
	}
	if e := w.ValidateScanCancellation(r); e != nil {
		return ScanCancellationResult{}, e
	}
	out, e := w.call(ctx, wechatScanReversePath, map[string]string{"out_trade_no": r.TradeNo}, true, true)
	if e != nil || len(out["nonce_str"]) == 0 || len(out["nonce_str"]) > 32 || !scanCancellationNumber.MatchString(out["nonce_str"]) || out["recall"] != "" && out["recall"] != "N" || out["out_trade_no"] != "" && out["out_trade_no"] != r.TradeNo || out["transaction_id"] != "" && (r.ProviderTradeNo == "" || out["transaction_id"] != r.ProviderTradeNo) {
		return ScanCancellationResult{}, &UnknownOutcome{Operation: "scan cancellation", Cause: ErrAuthentication}
	}
	return sealScanCancellation(w, r, r.ProviderTradeNo, "close_or_refund", wechatScanReversePath, "wechat_v2_md5_mtls", out)
}

var _ ScanCancellationProvider = (*WechatV2)(nil)
