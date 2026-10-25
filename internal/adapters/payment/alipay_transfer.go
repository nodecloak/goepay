package payment

import (
	"context"
	"strings"
)

func (a *Alipay) TransferTypes() []string {
	if a.config.EnableBankTransfer && a.certificates != nil {
		return []string{"alipay", "bank"}
	}
	return []string{"alipay"}
}
func (a *Alipay) ValidateTransfer(r TransferRequest) error {
	if e := a.validateCertificates(); e != nil {
		return e
	}
	if r.Type == "bank" {
		return a.validateBankTransfer(r)
	}
	if r.Type != "alipay" {
		return ErrUnsupported
	}
	if !validTransfer(r) {
		return ErrInvalid
	}
	return nil
}
func (a *Alipay) Transfer(ctx context.Context, r TransferRequest) (TransferResult, error) {
	if r.Type == "bank" {
		return a.bankTransfer(ctx, r, false)
	}
	if r.Type != "alipay" {
		return TransferResult{}, ErrUnsupported
	}
	if !validTransfer(r) {
		return TransferResult{}, ErrInvalid
	}
	if a.certificates != nil {
		return a.certificateTransfer(ctx, r)
	}
	accountType := "ALIPAY_LOGONID"
	if strings.HasPrefix(r.Account, "2088") && onlyDigits(r.Account) {
		accountType = "ALIPAY_USERID"
	}
	biz := map[string]any{"out_biz_no": r.TransferNo, "payee_type": accountType, "payee_account": r.Account, "amount": amountString(r.Amount), "payer_show_name": r.Title}
	if r.Name != "" {
		biz["payee_real_name"] = r.Name
	}
	if r.Remark != "" {
		biz["remark"] = r.Remark
	}
	out, e := a.call(ctx, "alipay.fund.trans.toaccount.transfer", biz, true)
	if e != nil {
		return TransferResult{}, a.bindTransferFailure(e, r, "alipay.fund.trans.toaccount.transfer", a.TransferProductIdentity())
	}
	if text(out, "out_biz_no") != r.TransferNo || text(out, "order_id") == "" || text(out, "pay_date") == "" {
		return TransferResult{}, &UnknownOutcome{"transfer", ErrAuthentication}
	}
	return TransferResult{TransferNo: r.TransferNo, ProviderTransferNo: text(out, "order_id"), State: "SUCCESS", PayDate: text(out, "pay_date"), Amount: r.Amount}, nil
}
func (a *Alipay) QueryTransfer(ctx context.Context, r TransferRequest) (TransferResult, error) {
	if r.Type == "bank" {
		return a.bankTransfer(ctx, r, true)
	}
	if r.Type != "alipay" {
		return TransferResult{}, ErrUnsupported
	}
	if !validTransfer(r) {
		return TransferResult{}, ErrInvalid
	}
	out, e := a.call(ctx, "alipay.fund.trans.common.query", map[string]any{"out_biz_no": r.TransferNo, "product_code": "TRANS_ACCOUNT_NO_PWD", "biz_scene": "DIRECT_TRANSFER"}, false)
	if e != nil {
		return TransferResult{}, e
	}
	n, e := parseAmount(text(out, "trans_amount"))
	if e != nil || n != r.Amount || text(out, "out_biz_no") != r.TransferNo || text(out, "order_id") == "" {
		return TransferResult{}, ErrAuthentication
	}
	return TransferResult{TransferNo: r.TransferNo, ProviderTransferNo: text(out, "order_id"), State: transferState(text(out, "status")), PayDate: text(out, "pay_date"), Amount: n}, nil
}
func onlyDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

var _ TransferProvider = (*Alipay)(nil)
