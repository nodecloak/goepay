package payment

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

func (*WechatV2) BankTransferProduct() string { return BankProductWechat }

// Bank codes are the explicit mapping shipped with the supplied wxpay plugin.
// This does not claim a particular merchant has current product permissions.
func ValidWechatBankCode(code string) bool {
	switch code {
	case "1002", "1005", "1003", "1026", "1020", "1001", "1066", "1006", "1010", "1021", "1004", "1009", "1022", "1027", "1025", "1056", "4836", "1024", "1054", "4755", "4216", "4051", "4753", "4761", "4036", "4752", "4756", "4767", "4115", "4150", "4156", "4166", "4157", "4153", "4113", "4108", "4076", "4052", "4764", "4217", "4072", "4769", "4778", "4766", "4758", "4763":
		return true
	}
	return false
}
func (w *WechatV2) ValidateTransfer(r TransferRequest) error {
	if w.config.RefundClient == nil {
		return ErrUnsupported
	}
	if !validTransfer(r) {
		return ErrInvalid
	}
	switch r.Type {
	case "wxpay":
		if r.Remark == "" {
			return ErrInvalid
		}
		return nil
	case "bank":
		if w.config.BankPublicKey == nil {
			return ErrUnsupported
		}
		if r.Name == "" || strings.TrimSpace(r.Name) != r.Name || len(r.Account) < 12 || len(r.Account) > 32 || !onlyDigits(r.Account) || !ValidWechatBankCode(r.BankCode) {
			return ErrInvalid
		}
		if !strings.HasPrefix(r.TransferNo, "TF") || !onlyDigits(strings.TrimPrefix(r.TransferNo, "TF")) {
			return ErrInvalid
		}
		return nil
	default:
		return ErrUnsupported
	}
}

// The bank endpoint requires 8..32 alphanumeric characters. Keep the business
// layer's TF{id} intact while deriving a collision-free fixed-width wire value.
func bankTradeNo(platformNo string) (string, error) {
	if !strings.HasPrefix(platformNo, "TF") {
		return "", ErrInvalid
	}
	digits := strings.TrimPrefix(platformNo, "TF")
	id, e := strconv.ParseInt(digits, 10, 64)
	if e != nil || id <= 0 || strconv.FormatInt(id, 10) != digits {
		return "", ErrInvalid
	}
	return fmt.Sprintf("TF%020d", id), nil
}
func (w *WechatV2) transferBank(ctx context.Context, r TransferRequest) (TransferResult, error) {
	if e := w.ValidateTransfer(r); e != nil {
		return TransferResult{}, e
	}
	number, e := bankTradeNo(r.TransferNo)
	if e != nil {
		return TransferResult{}, e
	}
	// APIv2 bank encryption specifically uses OpenSSL OAEP's SHA-1 parameters.
	// It is not interchangeable with OAEP-SHA256 used by newer protocols.
	card, e := rsa.EncryptOAEP(sha1.New(), rand.Reader, w.config.BankPublicKey, []byte(r.Account), nil)
	if e != nil {
		return TransferResult{}, ErrInvalid
	}
	name, e := rsa.EncryptOAEP(sha1.New(), rand.Reader, w.config.BankPublicKey, []byte(r.Name), nil)
	if e != nil {
		return TransferResult{}, ErrInvalid
	}
	out, e := w.exchange(ctx, "/mmpaysptrans/pay_bank", map[string]string{"mch_id": w.config.MerchantID, "partner_trade_no": number, "enc_bank_no": base64.StdEncoding.EncodeToString(card), "enc_true_name": base64.StdEncoding.EncodeToString(name), "bank_code": r.BankCode, "amount": strconv.FormatInt(r.Amount, 10), "desc": r.Remark}, true, true, false, map[string]string{"mch_id": w.config.MerchantID, "partner_trade_no": number, "amount": strconv.FormatInt(r.Amount, 10)})
	if e != nil {
		return TransferResult{}, w.bindTransferFailure(e, r, "/mmpaysptrans/pay_bank", BankProductWechat)
	}
	fee, e := bankFee(out["cmms_amt"])
	if e != nil || out["payment_no"] == "" {
		return TransferResult{}, &UnknownOutcome{"bank transfer", ErrAuthentication}
	}
	return TransferResult{TransferNo: r.TransferNo, ProviderTransferNo: out["payment_no"], State: "PROCESSING", Amount: r.Amount, BankFeeAmount: fee, BankFeeKnown: true}, nil
}
func (w *WechatV2) queryBank(ctx context.Context, r TransferRequest) (TransferResult, error) {
	if e := w.ValidateTransfer(r); e != nil {
		return TransferResult{}, e
	}
	number, e := bankTradeNo(r.TransferNo)
	if e != nil {
		return TransferResult{}, e
	}
	out, e := w.exchange(ctx, "/mmpaysptrans/query_bank", map[string]string{"mch_id": w.config.MerchantID, "partner_trade_no": number}, false, true, false, map[string]string{"mch_id": w.config.MerchantID, "partner_trade_no": number, "amount": strconv.FormatInt(r.Amount, 10)})
	if e != nil {
		return TransferResult{}, e
	}
	card := md5.Sum([]byte(r.Account))
	name := md5.Sum([]byte(r.Name))
	if !strings.EqualFold(out["bank_no_md5"], hex.EncodeToString(card[:])) || !strings.EqualFold(out["true_name_md5"], hex.EncodeToString(name[:])) || out["payment_no"] == "" {
		return TransferResult{}, ErrAuthentication
	}
	fee, e := bankFee(out["cmms_amt"])
	// A verified return remains evidence even if its fee proof is incomplete.
	// The domain must hold it for reconciliation, never retain a stale success
	// or infer a refund amount. All identity checks above remain mandatory.
	if e != nil && out["status"] != "BANK_FAIL" {
		return TransferResult{}, ErrAuthentication
	}
	state := out["status"]
	if state != "BANK_FAIL" {
		state = transferState(state)
	}
	return TransferResult{TransferNo: r.TransferNo, ProviderTransferNo: out["payment_no"], State: state, Amount: r.Amount, PayDate: out["pay_succ_time"], BankFeeAmount: fee, BankFeeKnown: e == nil}, nil
}
func bankFee(raw string) (int64, error) {
	if raw == "" || !onlyDigits(raw) {
		return 0, ErrAuthentication
	}
	n, e := strconv.ParseInt(raw, 10, 64)
	if e != nil || n < 0 {
		return 0, ErrAuthentication
	}
	return n, nil
}
