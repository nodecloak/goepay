package payment

import (
	"context"
	"goepay/internal/ledger"
	"strings"
	"unicode/utf8"
)

type AlipayTransferSceneReport struct {
	InfoType    string `json:"info_type"`
	InfoContent string `json:"info_content"`
}

func validateAlipayBankConfig(c AlipayConfig, certificates bool) error {
	if !c.EnableBankTransfer {
		return nil
	}
	if !certificates || !bankSceneText(c.BankTransferScene, 64) || len(c.BankTransferReports) == 0 || len(c.BankTransferReports) > 20 {
		return ErrInvalid
	}
	for _, r := range c.BankTransferReports {
		if !bankSceneText(r.InfoType, 64) || !bankSceneText(r.InfoContent, 256) {
			return ErrInvalid
		}
	}
	return nil
}
func bankSceneText(value string, max int) bool {
	return value != "" && len(value) <= max && utf8.ValidString(value) && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\x00\r\n")
}
func (a *Alipay) BankTransferProduct() string {
	if a.certificates != nil {
		return BankProductAlipay
	}
	return ""
}
func (a *Alipay) validateBankTransfer(r TransferRequest) error {
	if !a.config.EnableBankTransfer || a.certificates == nil {
		return ErrUnsupported
	}
	return a.validateBankTransferQuery(r)
}
func (a *Alipay) validateBankTransferQuery(r TransferRequest) error {
	if a.certificates == nil {
		return ErrUnsupported
	}
	if e := a.validateCertificates(); e != nil {
		return e
	}
	if !validTransfer(r) || !strings.HasPrefix(r.TransferNo, "TF") || !onlyDigits(strings.TrimPrefix(r.TransferNo, "TF")) || !onlyDigits(r.Account) || len(r.Account) < 12 || len(r.Account) > 32 || !bankSceneText(r.Name, 128) || r.BankCode != "" {
		return ErrInvalid
	}
	return nil
}
func (a *Alipay) bankTransfer(ctx context.Context, r TransferRequest, query bool) (TransferResult, error) {
	var result TransferResult
	if query {
		if e := a.validateBankTransferQuery(r); e != nil {
			return result, e
		}
	} else if e := a.validateBankTransfer(r); e != nil {
		return result, e
	}
	if strings.TrimLeft(strings.TrimPrefix(r.TransferNo, "TF"), "0") == "" {
		return result, ErrInvalid
	}
	biz := map[string]any{"out_biz_no": r.TransferNo, "product_code": "TRANS_BANKCARD_NO_PWD", "biz_scene": "DIRECT_TRANSFER"}
	method := "alipay.fund.trans.common.query"
	if !query {
		method = "alipay.fund.trans.uni.transfer"
		biz["trans_amount"] = amountString(r.Amount)
		biz["order_title"] = r.Title
		biz["payee_info"] = map[string]any{"identity_type": "BANKCARD_ACCOUNT", "identity": r.Account, "name": r.Name, "bankcard_ext_info": map[string]any{"account_type": "2"}}
		biz["transfer_scene_name"] = a.config.BankTransferScene
		biz["transfer_scene_report_infos"] = a.config.BankTransferReports
		if r.Remark != "" {
			biz["remark"] = r.Remark
		}
	}
	out, e := a.call(ctx, method, biz, !query)
	if e != nil {
		if !query {
			e = a.bindTransferFailure(e, r, method, BankProductAlipay)
		}
		return result, e
	}
	if text(out, "out_biz_no") != r.TransferNo || text(out, "order_id") == "" {
		return result, a.responseFailure(method, !query)
	}
	if product := text(out, "product_code"); product != "" && product != "TRANS_BANKCARD_NO_PWD" {
		return result, a.responseFailure(method, !query)
	}
	if scene := text(out, "biz_scene"); scene != "" && scene != "DIRECT_TRANSFER" {
		return result, a.responseFailure(method, !query)
	}
	evidence := &AlipayBankEvidence{Product: BankProductAlipay, SubmissionBound: !query}
	amount := text(out, "trans_amount")
	if !query {
		amount = text(out, "amount")
	}
	if amount != "" {
		n, pe := parseAmount(amount)
		if pe != nil || n != r.Amount {
			return result, a.responseFailure(method, !query)
		}
		evidence.AmountKnown = true
	}
	if raw := text(out, "order_fee"); raw != "" {
		n, pe := ledger.Parse(raw)
		if pe != nil || n > 999999999999 {
			return result, a.responseFailure(method, !query)
		}
		evidence.EstimatedFeeKnown = true
		evidence.EstimatedFee = n
	}
	state := text(out, "status")
	switch state {
	case "SUCCESS", "FAIL", "DEALING", "REFUND":
	default:
		state = "UNKNOWN"
	}
	// Synchronous acknowledgement fixes the original request/order identity; it
	// does not prove the final amount. All final decisions use a later query.
	if !query {
		state = "PROCESSING"
	} else if state == "DEALING" {
		state = "PROCESSING"
	}
	date := text(out, "pay_date")
	if !query {
		date = text(out, "trans_date")
	}
	return TransferResult{TransferNo: r.TransferNo, ProviderTransferNo: text(out, "order_id"), State: state, Amount: r.Amount, PayDate: date, AlipayBank: evidence}, nil
}
