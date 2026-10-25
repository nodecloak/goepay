package payment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
)

type ScanObservationProvider interface {
	ObserveScan(context.Context, QueryRequest) (QueryResult, error)
}

type ScanObservationReceipt struct {
	Provider        string `json:"provider"`
	Product         string `json:"product"`
	Authentication  string `json:"authentication"`
	AccountScope    string `json:"account_scope"`
	TradeNo         string `json:"trade_no"`
	ProviderTradeNo string `json:"provider_trade_no"`
	State           string `json:"state"`
	Currency        string `json:"currency"`
	Amount          int64  `json:"amount,string"`
	RequestSHA256   string `json:"request_sha256"`
	ResponseSHA256  string `json:"response_sha256"`
}

func VerifiedScanObservation(result QueryResult) (ScanObservationReceipt, bool) {
	if result.scanObservation == nil {
		return ScanObservationReceipt{}, false
	}
	v := *result.scanObservation
	if v.Provider == "alipay" {
		if v.Product != "alipay.trade.query" || v.Authentication != "alipay_rsa2_tls" {
			return ScanObservationReceipt{}, false
		}
	} else if v.Provider == "wxpay" {
		if v.Product != "/pay/orderquery" || v.Authentication != "wechat_v2_md5_tls" {
			return ScanObservationReceipt{}, false
		}
	} else {
		return ScanObservationReceipt{}, false
	}
	if v.AccountScope == "" || len(v.RequestSHA256) != 64 || !scanObservationState(v.Provider, result.State) || result.TradeNo != v.TradeNo || result.ProviderTradeNo != v.ProviderTradeNo || result.Amount != v.Amount || result.Currency != v.Currency || result.State != v.State || result.Amount <= 0 || result.Currency != "CNY" {
		return ScanObservationReceipt{}, false
	}
	if scanObservationResultHash(result) != v.ResponseSHA256 {
		return ScanObservationReceipt{}, false
	}
	return v, true
}

type scanObservationContextKey struct{}

func scanObservationRequested(ctx context.Context) bool {
	v, _ := ctx.Value(scanObservationContextKey{}).(bool)
	return v
}

func scanObservationState(provider, state string) bool {
	if provider == "alipay" {
		return state == "WAIT_BUYER_PAY" || state == "TRADE_SUCCESS" || state == "TRADE_FINISHED" || state == "TRADE_CLOSED"
	}
	return provider == "wxpay" && (state == "SUCCESS" || state == "REFUND" || state == "NOTPAY" || state == "CLOSED" || state == "REVOKED" || state == "USERPAYING" || state == "PAYERROR")
}
func scanObservationResultHash(result QueryResult) string {
	raw, _ := json.Marshal([]any{result.TradeNo, result.ProviderTradeNo, result.State, result.Currency, result.Amount, result.BuyerID})
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
func ScanObservationMatchesRequest(result QueryResult, r QueryRequest) bool {
	receipt, valid := VerifiedScanObservation(result)
	if !valid || r.TradeNo != receipt.TradeNo {
		return false
	}
	raw, _ := json.Marshal([]any{receipt.Provider, receipt.Product, receipt.AccountScope, r.TradeNo, r.ProviderTradeNo})
	digest := sha256.Sum256(raw)
	return hmac.Equal([]byte(hex.EncodeToString(digest[:])), []byte(receipt.RequestSHA256))
}
func validateScanObservation(origin string, methods []string, r QueryRequest, maxNumber int) error {
	if !enabled("scan", methods) {
		return ErrUnsupported
	}
	u, e := url.Parse(origin)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !scanCancellationNumber.MatchString(r.TradeNo) || len(r.TradeNo) > maxNumber || r.ProviderTradeNo != "" && (!scanCancellationNumber.MatchString(r.ProviderTradeNo) || len(r.ProviderTradeNo) > maxNumber) {
		return ErrInvalid
	}
	return nil
}
func sealScanObservation(p Provider, r QueryRequest, result QueryResult) (QueryResult, error) {
	if !scanObservationState(p.ID(), result.State) || result.TradeNo != r.TradeNo || result.Amount <= 0 || result.Amount > 999999999999 || result.Currency != "CNY" || !trustedBuyer(result.BuyerID) || result.ProviderTradeNo != "" && !scanCancellationNumber.MatchString(result.ProviderTradeNo) || r.ProviderTradeNo != "" && result.ProviderTradeNo != r.ProviderTradeNo {
		return QueryResult{}, ErrAuthentication
	}
	if (result.State == "SUCCESS" || result.State == "TRADE_SUCCESS" || result.State == "TRADE_FINISHED") && result.ProviderTradeNo == "" {
		return QueryResult{}, ErrAuthentication
	}
	scope, e := TransferAccountScope(p)
	if e != nil {
		return QueryResult{}, e
	}
	product, authentication := "alipay.trade.query", "alipay_rsa2_tls"
	if p.ID() == "wxpay" {
		product, authentication = "/pay/orderquery", "wechat_v2_md5_tls"
		if len(result.ProviderTradeNo) > 32 {
			return QueryResult{}, ErrAuthentication
		}
	}
	raw, _ := json.Marshal([]any{p.ID(), product, scope, r.TradeNo, r.ProviderTradeNo})
	digest := sha256.Sum256(raw)
	result.scanObservation = &ScanObservationReceipt{Provider: p.ID(), Product: product, Authentication: authentication, AccountScope: scope, TradeNo: result.TradeNo, ProviderTradeNo: result.ProviderTradeNo, State: result.State, Currency: result.Currency, Amount: result.Amount, RequestSHA256: hex.EncodeToString(digest[:]), ResponseSHA256: scanObservationResultHash(result)}
	return result, nil
}

// These methods use the existing signed read endpoint exactly once. The private
// context flag requires actual verified TLS in that exchange, without changing
// the legacy Query contract or creating any cancellation/send opportunity.
func (a *Alipay) ObserveScan(ctx context.Context, r QueryRequest) (QueryResult, error) {
	if ctx == nil || ctx.Err() != nil {
		return QueryResult{}, ErrInvalid
	}
	if e := validateScanObservation(a.config.GatewayURL, a.config.Methods, r, 64); e != nil {
		return QueryResult{}, e
	}
	if a.config.SellerID == "" {
		return QueryResult{}, ErrInvalid
	}
	if e := a.validateCertificates(); e != nil {
		return QueryResult{}, e
	}
	result, e := a.Query(context.WithValue(ctx, scanObservationContextKey{}, true), r)
	if e != nil {
		return result, e
	}
	return sealScanObservation(a, r, result)
}
func (w *WechatV2) ObserveScan(ctx context.Context, r QueryRequest) (QueryResult, error) {
	if ctx == nil || ctx.Err() != nil {
		return QueryResult{}, ErrInvalid
	}
	if e := validateScanObservation(w.config.BaseURL, w.config.Methods, r, 32); e != nil {
		return QueryResult{}, e
	}
	result, e := w.Query(context.WithValue(ctx, scanObservationContextKey{}, true), r)
	if e != nil {
		return result, e
	}
	return sealScanObservation(w, r, result)
}
