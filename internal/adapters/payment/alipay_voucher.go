package payment

import (
	"context"
	"net/url"
	"strconv"
	"time"
)

// A voucher is only material for a later buyer-authorized FREEZE. It is not a
// successful authorization or a payment receipt, even with code=10000.
func (a *Alipay) freezeVoucher(ctx context.Context, r CreateRequest, biz map[string]any) (CreateResult, error) {
	delete(biz, "time_expire")
	biz["product_code"] = "PRE_AUTH"
	if a.config.PreauthVoucherProductCode != "" {
		biz["product_code"] = a.config.PreauthVoucherProductCode
	}
	biz["pay_timeout"] = "2h"
	if r.ExpiresAt != nil {
		minutes := (r.ExpiresAt.Sub(a.config.Now()) + time.Minute - 1) / time.Minute
		biz["pay_timeout"] = strconv.FormatInt(int64(minutes), 10) + "m"
	}
	out, e := a.call(ctx, "alipay.fund.auth.order.voucher.create", biz, true, map[string]string{"notify_url": r.NotifyURL})
	if e != nil {
		return CreateResult{}, e
	}
	material := text(out, "code_value")
	u, parseErr := url.Parse(material)
	if text(out, "out_order_no") != r.TradeNo || text(out, "out_request_no") != r.TradeNo || text(out, "code_type") != "qrCode" || parseErr != nil || u.Scheme != "https" || u.Hostname() != "qr.alipay.com" || u.Port() != "" || u.User != nil || u.Fragment != "" || u.Path == "" || len(material) > 2048 {
		return CreateResult{}, &UnknownOutcome{Operation: "create authorization voucher", Cause: ErrAuthentication}
	}
	return CreateResult{MaterialType: "qrcode", Material: material}, nil
}
