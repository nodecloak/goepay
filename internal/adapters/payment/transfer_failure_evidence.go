package payment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// A receipt describes one authenticated response to the original submit. It
// does not mean the transfer is definitely failed or its reserve may be freed.
type TransferFailureReceipt struct {
	Provider       string `json:"provider"`
	Product        string `json:"product"`
	Operation      string `json:"operation"`
	TransferNo     string `json:"transfer_no"`
	Type           string `json:"type"`
	AccountScope   string `json:"account_scope"`
	Code           string `json:"code"`
	ResponseSHA256 string `json:"response_sha256"`
	Authentication string `json:"authentication"`
	Amount         int64  `json:"amount,string"`
}
type authenticatedProviderRejection struct {
	provider, operation, code, responseSHA256, authentication string
	scanPendingTransport                                      bool
	verifiedTLS                                               bool
}

func authenticatedProviderError(provider, operation, code, authentication string, raw []byte) *ProviderError {
	digest := sha256.Sum256(raw)
	return &ProviderError{Code: code, rejection: &authenticatedProviderRejection{provider: provider, operation: operation, code: code, authentication: authentication, responseSHA256: hex.EncodeToString(digest[:])}}
}

// Only adapter submit implementations can attach this private receipt. A
// caller-created ProviderError or a read-only query error has no such source.
func VerifiedTransferFailure(err error) (TransferFailureReceipt, bool) {
	var zero TransferFailureReceipt
	var uncertain *UnknownOutcome
	var rejected *ProviderError
	if err == nil || errors.As(err, &uncertain) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &rejected) || rejected.rejection == nil || rejected.transferFailure == nil {
		return zero, false
	}
	origin, v := rejected.rejection, *rejected.transferFailure
	if !origin.verifiedTLS || rejected.Code != origin.code || v.Code != origin.code || v.Provider != origin.provider || v.Operation != origin.operation || v.ResponseSHA256 != origin.responseSHA256 || v.Authentication != origin.authentication || len(v.ResponseSHA256) != 64 || v.Amount <= 0 || v.AccountScope == "" {
		return zero, false
	}
	return v, true
}

func (a *Alipay) bindTransferFailure(err error, r TransferRequest, operation, product string) error {
	if uncertainProviderOutcome(err) {
		return err
	}
	var rejected *ProviderError
	if !errors.As(err, &rejected) || rejected.rejection == nil || rejected.rejection.provider != "alipay" || rejected.rejection.operation != operation || rejected.rejection.code != rejected.Code || a.config.SellerID == "" {
		return err
	}
	if operation != "alipay.fund.trans.toaccount.transfer" && operation != "alipay.fund.trans.uni.transfer" {
		return err
	}
	if r.Type == "bank" {
		if product != BankProductAlipay || operation != "alipay.fund.trans.uni.transfer" {
			return err
		}
	} else if r.Type != "alipay" || product != a.TransferProductIdentity() {
		return err
	}
	n, e := strconv.ParseInt(strings.TrimPrefix(r.TransferNo, "TF"), 10, 64)
	if e != nil || n <= 0 || r.TransferNo != fmt.Sprintf("TF%d", n) || r.Amount <= 0 {
		return err
	}
	scope, e := TransferAccountScope(a)
	if e != nil {
		return err
	}
	if previous, done := reuseTransferFailure(err, r, operation, product, scope); done {
		return previous
	}
	copy := *rejected
	copy.transferFailure = &TransferFailureReceipt{Provider: "alipay", Product: product, Operation: operation, TransferNo: r.TransferNo, Type: r.Type, AccountScope: scope, Amount: r.Amount, Code: rejected.Code, Authentication: rejected.rejection.authentication, ResponseSHA256: rejected.rejection.responseSHA256}
	return &copy
}

func (w *WechatV2) bindTransferFailure(err error, r TransferRequest, operation, product string) error {
	if uncertainProviderOutcome(err) {
		return err
	}
	var rejected *ProviderError
	if !errors.As(err, &rejected) || rejected.rejection == nil || rejected.rejection.provider != "wxpay" || rejected.rejection.operation != operation || rejected.rejection.code != rejected.Code {
		return err
	}
	if r.Type == "bank" {
		if operation != "/mmpaysptrans/pay_bank" || product != BankProductWechat {
			return err
		}
	} else if r.Type != "wxpay" || operation != "/mmpaymkttransfers/promotion/transfers" || product != w.TransferProductIdentity() {
		return err
	}
	n, e := strconv.ParseInt(strings.TrimPrefix(r.TransferNo, "TF"), 10, 64)
	if e != nil || n <= 0 || r.TransferNo != fmt.Sprintf("TF%d", n) || r.Amount <= 0 {
		return err
	}
	scope, e := TransferAccountScope(w)
	if e != nil {
		return err
	}
	if previous, done := reuseTransferFailure(err, r, operation, product, scope); done {
		return previous
	}
	copy := *rejected
	copy.transferFailure = &TransferFailureReceipt{Provider: "wxpay", Product: product, Operation: operation, TransferNo: r.TransferNo, Type: r.Type, AccountScope: scope, Amount: r.Amount, Code: rejected.Code, Authentication: rejected.rejection.authentication, ResponseSHA256: rejected.rejection.responseSHA256}
	return &copy
}

func reuseTransferFailure(err error, r TransferRequest, operation, product, scope string) (error, bool) {
	var rejected *ProviderError
	if !errors.As(err, &rejected) || rejected.transferFailure == nil {
		return nil, false
	}
	v, valid := VerifiedTransferFailure(err)
	if !valid || v.TransferNo != r.TransferNo || v.Type != r.Type || v.Amount != r.Amount || v.Operation != operation || v.Product != product || v.AccountScope != scope {
		return &UnknownOutcome{Operation: "transfer", Cause: ErrAuthentication}, true
	}
	return err, true
}
