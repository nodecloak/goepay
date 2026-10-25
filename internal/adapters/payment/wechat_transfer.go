package payment

import (
	"context"
	"strconv"
)

func (w *WechatV2) TransferTypes() []string {
	if w.config.RefundClient == nil {
		return nil
	}
	if w.config.BankPublicKey != nil {
		return []string{"wxpay", "bank"}
	}
	return []string{"wxpay"}
}
func (w *WechatV2) Transfer(ctx context.Context, r TransferRequest) (TransferResult, error) {
	if r.Type == "bank" {
		return w.transferBank(ctx, r)
	}
	if r.Type != "wxpay" || w.config.RefundClient == nil {
		return TransferResult{}, ErrUnsupported
	}
	if !validTransfer(r) || r.Remark == "" {
		return TransferResult{}, ErrInvalid
	}
	p := map[string]string{"mch_appid": w.config.AppID, "mchid": w.config.MerchantID, "partner_trade_no": r.TransferNo, "openid": r.Account, "check_name": "NO_CHECK", "amount": strconv.FormatInt(r.Amount, 10), "desc": r.Remark}
	if r.Name != "" {
		p["check_name"] = "FORCE_CHECK"
		p["re_user_name"] = r.Name
	}
	// This legacy endpoint returns unsigned XML. Server authentication and the
	// client certificate are both required; a supplied response signature is
	// verified. Payment endpoint responses still require their signatures.
	out, e := w.exchange(ctx, "/mmpaymkttransfers/promotion/transfers", p, true, true, false, map[string]string{"mch_appid": w.config.AppID, "mchid": w.config.MerchantID, "partner_trade_no": r.TransferNo})
	if e != nil {
		return TransferResult{}, w.bindTransferFailure(e, r, "/mmpaymkttransfers/promotion/transfers", w.TransferProductIdentity())
	}
	if out["payment_no"] == "" || out["payment_time"] == "" {
		return TransferResult{}, &UnknownOutcome{"transfer", ErrAuthentication}
	}
	return TransferResult{TransferNo: r.TransferNo, ProviderTransferNo: out["payment_no"], State: "SUCCESS", PayDate: out["payment_time"], Amount: r.Amount}, nil
}
func (w *WechatV2) QueryTransfer(ctx context.Context, r TransferRequest) (TransferResult, error) {
	if r.Type == "bank" {
		return w.queryBank(ctx, r)
	}
	if r.Type != "wxpay" || w.config.RefundClient == nil {
		return TransferResult{}, ErrUnsupported
	}
	if !validTransfer(r) {
		return TransferResult{}, ErrInvalid
	}
	// Official v2 query requires appid/mch_id and mTLS, even though the older
	// bundled PHP SDK passes its differently named payment request fields.
	out, e := w.exchange(ctx, "/mmpaymkttransfers/gettransferinfo", map[string]string{"appid": w.config.AppID, "mch_id": w.config.MerchantID, "partner_trade_no": r.TransferNo}, false, true, false, map[string]string{"appid": w.config.AppID, "mch_id": w.config.MerchantID, "partner_trade_no": r.TransferNo, "openid": r.Account})
	if e != nil {
		return TransferResult{}, e
	}
	n, e := strconv.ParseInt(out["payment_amount"], 10, 64)
	if e != nil || n != r.Amount || out["detail_id"] == "" {
		return TransferResult{}, ErrAuthentication
	}
	return TransferResult{TransferNo: r.TransferNo, ProviderTransferNo: out["detail_id"], State: transferState(out["status"]), PayDate: out["payment_time"], Amount: n}, nil
}

var _ TransferProvider = (*WechatV2)(nil)
