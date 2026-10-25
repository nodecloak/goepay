package payment

import (
	"context"
	"strings"
)

func (a *Alipay) certificateTransfer(ctx context.Context, r TransferRequest) (TransferResult, error) {
	identityType := "ALIPAY_OPEN_ID"
	if strings.HasPrefix(r.Account, "2088") && onlyDigits(r.Account) {
		identityType = "ALIPAY_USER_ID"
	} else if strings.Contains(r.Account, "@") || onlyDigits(r.Account) {
		identityType = "ALIPAY_LOGON_ID"
	}
	payee := map[string]any{"identity": r.Account, "identity_type": identityType}
	if r.Name != "" {
		payee["name"] = r.Name
	}
	biz := map[string]any{"out_biz_no": r.TransferNo, "trans_amount": amountString(r.Amount), "product_code": "TRANS_ACCOUNT_NO_PWD", "biz_scene": "DIRECT_TRANSFER", "order_title": r.Title, "payee_info": payee, "business_params": `{"payer_show_name_use_alias":"true"}`}
	if r.Remark != "" {
		biz["remark"] = r.Remark
	}
	out, e := a.call(ctx, "alipay.fund.trans.uni.transfer", biz, true)
	if e != nil {
		return TransferResult{}, a.bindTransferFailure(e, r, "alipay.fund.trans.uni.transfer", a.TransferProductIdentity())
	}
	if text(out, "out_biz_no") != r.TransferNo || text(out, "order_id") == "" {
		return TransferResult{}, &UnknownOutcome{"certificate transfer", ErrAuthentication}
	}
	if amount := text(out, "trans_amount"); amount != "" {
		n, e := parseAmount(amount)
		if e != nil || n != r.Amount {
			return TransferResult{}, &UnknownOutcome{"certificate transfer", ErrAuthentication}
		}
	}
	state := transferState(text(out, "status"))
	if state == "SUCCESS" && text(out, "trans_date") == "" {
		return TransferResult{}, &UnknownOutcome{"certificate transfer", ErrAuthentication}
	}
	return TransferResult{TransferNo: r.TransferNo, ProviderTransferNo: text(out, "order_id"), State: state, Amount: r.Amount, PayDate: text(out, "trans_date")}, nil
}
