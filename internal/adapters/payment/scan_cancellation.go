package payment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
)

type ScanCancellationRequest struct {
	TradeNo         string `json:"trade_no"`
	ProviderTradeNo string `json:"provider_trade_no"`
	Amount          int64  `json:"amount,string"`
}
type ScanCancellationResult struct {
	TradeNo         string `json:"trade_no"`
	ProviderTradeNo string `json:"provider_trade_no"`
	State           string `json:"state"`
	Action          string `json:"action"`
	Amount          int64  `json:"amount,string"`
	receipt         *ScanCancellationReceipt
}

// Amount is original request context, not an upstream echoed or refunded
// amount. The domain must bind it to the immutable original order before I/O.
type ScanCancellationReceipt struct {
	Provider        string `json:"provider"`
	Product         string `json:"product"`
	AccountScope    string `json:"account_scope"`
	Authentication  string `json:"authentication"`
	TradeNo         string `json:"trade_no"`
	ProviderTradeNo string `json:"provider_trade_no"`
	State           string `json:"state"`
	Action          string `json:"action"`
	Amount          int64  `json:"amount,string"`
	RequestSHA256   string `json:"request_sha256"`
	ResponseSHA256  string `json:"response_sha256"`
}
type ScanCancellationProvider interface {
	CanCancelScan() bool
	ValidateScanCancellation(ScanCancellationRequest) error
	CancelScan(context.Context, ScanCancellationRequest) (ScanCancellationResult, error)
}

var scanCancellationNumber = regexp.MustCompile(`^[A-Za-z0-9_*-]{1,64}$`)

// Only transport adapters can seal this provenance. Marshaling a public result
// or changing its original identifiers must never retain authentication.
func VerifiedScanCancellation(result ScanCancellationResult) (ScanCancellationReceipt, bool) {
	if result.receipt == nil {
		return ScanCancellationReceipt{}, false
	}
	v := *result.receipt
	switch v.Provider {
	case "alipay":
		if v.Product != "alipay.trade.cancel" || v.Authentication != "alipay_rsa2" || (v.Action != "close" && v.Action != "refund") {
			return ScanCancellationReceipt{}, false
		}
	case "wxpay":
		if v.Product != wechatScanReversePath || v.Authentication != "wechat_v2_md5_mtls" || v.Action != "close_or_refund" {
			return ScanCancellationReceipt{}, false
		}
	default:
		return ScanCancellationReceipt{}, false
	}
	if result.TradeNo != v.TradeNo || result.ProviderTradeNo != v.ProviderTradeNo || result.State != v.State || result.Action != v.Action || result.Amount != v.Amount || v.State != "cancelled" || v.Amount <= 0 || v.AccountScope == "" || len(v.RequestSHA256) != 64 || len(v.ResponseSHA256) != 64 {
		return ScanCancellationReceipt{}, false
	}
	return v, true
}

func sealScanCancellation(p Provider, r ScanCancellationRequest, providerNo, action, product, authentication string, reply any) (ScanCancellationResult, error) {
	scope, e := TransferAccountScope(p)
	if e != nil {
		return ScanCancellationResult{}, e
	}
	requestHash, e := scanCancellationRequestHash(p.ID(), product, scope, r)
	if e != nil {
		return ScanCancellationResult{}, e
	}
	response, e := json.Marshal(reply)
	if e != nil || len(response) > 1<<20 {
		return ScanCancellationResult{}, ErrAuthentication
	}
	responseHash := sha256.Sum256(response)
	proof := &ScanCancellationReceipt{Provider: p.ID(), Product: product, AccountScope: scope, Authentication: authentication, TradeNo: r.TradeNo, ProviderTradeNo: providerNo, State: "cancelled", Action: action, Amount: r.Amount, RequestSHA256: requestHash, ResponseSHA256: hex.EncodeToString(responseHash[:])}
	return ScanCancellationResult{TradeNo: r.TradeNo, ProviderTradeNo: providerNo, State: "cancelled", Action: action, Amount: r.Amount, receipt: proof}, nil
}

func scanCancellationRequestHash(provider, product, scope string, r ScanCancellationRequest) (string, error) {
	raw, e := json.Marshal([]any{provider, product, scope, r.TradeNo, r.ProviderTradeNo, r.Amount})
	if e != nil {
		return "", e
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

// The upstream can echo an institution number which was absent from a caller's
// request. Matching just the public response tuple cannot prove the original
// expected number; compare the adapter-sealed original request digest as well.
func ScanCancellationMatchesRequest(result ScanCancellationResult, r ScanCancellationRequest) bool {
	receipt, valid := VerifiedScanCancellation(result)
	if !valid {
		return false
	}
	digest, e := scanCancellationRequestHash(receipt.Provider, receipt.Product, receipt.AccountScope, r)
	return e == nil && hmac.Equal([]byte(digest), []byte(receipt.RequestSHA256))
}

func authenticatedTLSExchange(req *http.Request, res *http.Response) bool {
	return req != nil && req.URL != nil && req.URL.Scheme == "https" && res != nil && res.TLS != nil && res.TLS.HandshakeComplete && len(res.TLS.VerifiedChains) > 0 && res.Request != nil && res.Request.URL != nil && res.Request.URL.Scheme == req.URL.Scheme && res.Request.URL.Host == req.URL.Host && res.Request.URL.Path == req.URL.Path
}
