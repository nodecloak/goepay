package payment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

// ScanPendingReceipt identifies an authenticated response to the first barcode
// submission. Amount describes that original request, not an upstream payment.
// It authorizes neither a new submission nor cancellation without domain checks.
type ScanPendingReceipt struct {
	Provider       string `json:"provider"`
	Product        string `json:"product"`
	AccountScope   string `json:"account_scope"`
	Authentication string `json:"authentication"`
	TradeNo        string `json:"trade_no"`
	Amount         int64  `json:"amount,string"`
	Code           string `json:"code"`
	RequestSHA256  string `json:"request_sha256"`
	ResponseSHA256 string `json:"response_sha256"`
}

func VerifiedScanPending(err error) (ScanPendingReceipt, bool) {
	var rejected *ProviderError
	var uncertain *UnknownOutcome
	if err == nil || errors.As(err, &uncertain) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &rejected) || rejected.rejection == nil || rejected.scanPending == nil {
		return ScanPendingReceipt{}, false
	}
	origin, v := rejected.rejection, *rejected.scanPending
	if !origin.scanPendingTransport || !scanPendingCode(origin.provider, origin.operation, origin.code, origin.authentication) || rejected.Code != origin.code || v.Code != origin.code || v.Provider != origin.provider || v.Product != origin.operation || v.Authentication != origin.authentication || v.ResponseSHA256 != origin.responseSHA256 || v.Amount <= 0 || v.TradeNo == "" || v.AccountScope == "" || len(v.RequestSHA256) != 64 || len(v.ResponseSHA256) != 64 {
		return ScanPendingReceipt{}, false
	}
	return v, true
}

func scanPendingCode(provider, operation, code, authentication string) bool {
	switch provider {
	case "alipay":
		primary, _, _ := strings.Cut(code, ":")
		return operation == "alipay.trade.pay" && authentication == "alipay_rsa2" && (primary == "10003" || primary == "20000")
	case "wxpay":
		return operation == "/pay/micropay" && authentication == "wechat_v2_md5" && (code == "USERPAYING" || code == "SYSTEMERROR")
	}
	return false
}

func bindScanPending(err error, p Provider, r ScanRequest) error {
	if uncertainProviderOutcome(err) {
		return err
	}
	var rejected *ProviderError
	if !errors.As(err, &rejected) {
		return err
	}
	if rejected.scanPending != nil {
		return err
	} // Never rebind a sealed old response.
	if rejected.rejection == nil || !rejected.rejection.scanPendingTransport || rejected.rejection.provider != p.ID() || rejected.rejection.code != rejected.Code || !validScan(r, p.ID()) || !scanPendingCode(p.ID(), rejected.rejection.operation, rejected.Code, rejected.rejection.authentication) {
		if r.AuthCode != "" && strings.Contains(rejected.Code, r.AuthCode) {
			return &ProviderError{Code: "SCAN_REJECTED"}
		}
		return err
	}
	scope, e := TransferAccountScope(p)
	if e != nil {
		return err
	}
	// Only this digest leaves the adapter. No bearer code or raw upstream body
	// is retained in the error, receipt, logs, or future persistence interface.
	digest, e := scanPendingRequestHash(p.ID(), rejected.rejection.operation, scope, r)
	if e != nil {
		return err
	}
	copy := *rejected
	origin := *rejected.rejection
	if p.ID() == "alipay" {
		origin.code, _, _ = strings.Cut(origin.code, ":")
	}
	copy.Code, copy.rejection = origin.code, &origin
	// The Pro recovery branch consumes the primary return code only. Do not
	// retain arbitrary upstream sub-code text alongside the bearer fingerprint.
	copy.scanPending = &ScanPendingReceipt{Provider: p.ID(), Product: origin.operation, AccountScope: scope, Authentication: origin.authentication, TradeNo: r.TradeNo, Amount: r.Amount, Code: origin.code, RequestSHA256: digest, ResponseSHA256: origin.responseSHA256}
	return &copy
}

// ScanPendingMatchesRequest validates the private response against the exact
// in-memory original request before the bearer code is discarded. Neither a
// caller-created receipt nor a later code can become the original submission.
func ScanPendingMatchesRequest(err error, r ScanRequest) bool {
	receipt, valid := VerifiedScanPending(err)
	if !valid || !validScan(r, receipt.Provider) {
		return false
	}
	digest, e := scanPendingRequestHash(receipt.Provider, receipt.Product, receipt.AccountScope, r)
	return e == nil && hmac.Equal([]byte(digest), []byte(receipt.RequestSHA256))
}

func scanPendingRequestHash(provider, operation, scope string, r ScanRequest) (string, error) {
	terms := []any{provider, operation, scope, r.TradeNo, r.Amount, r.Subject, r.ClientIP, r.NotifyURL, r.AuthCode}
	// Preserve every preexisting false-declaration receipt byte for byte.
	if r.ProfitSharing {
		terms = append(terms, "profit-sharing79", true)
	}
	raw, e := json.Marshal(terms)
	if e != nil {
		return "", e
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}
