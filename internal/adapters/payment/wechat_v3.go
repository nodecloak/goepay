package payment

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type WechatV3 struct{ *wechatV3Client }

var v3TradeNumber = regexp.MustCompile(`^[A-Za-z0-9_*-]{1,32}$`)
var v3RefundNumber = regexp.MustCompile(`^[A-Za-z0-9_\-|*@]{1,64}$`)

func NewWechatV3(c WechatV3Config) (*WechatV3, error) {
	if c.Methods == nil {
		c.Methods = []string{"native", "jsapi"}
	}
	if len(c.Methods) == 0 {
		return nil, ErrInvalid
	}
	seen := map[string]bool{}
	for _, method := range c.Methods {
		if seen[method] {
			return nil, ErrInvalid
		}
		seen[method] = true
		switch method {
		case "native", "jsapi", "app":
		case "h5":
			if !v3HTTPS(c.H5SiteURL) || strings.TrimSpace(c.H5SiteName) == "" || len(c.H5SiteName) > 128 {
				return nil, ErrInvalid
			}
		default:
			return nil, ErrUnsupported
		}
	}
	client, e := newWechatV3Client(c)
	if e != nil {
		return nil, e
	}
	return &WechatV3{client}, nil
}
func (w *WechatV3) ID() string                      { return "wxpay" }
func (w *WechatV3) ApplicationID() string           { return w.config.AppID }
func (w *WechatV3) TransferAccountIdentity() string { return w.config.MerchantID }
func (w *WechatV3) NotificationFormat() string      { return "wechat-v3" }
func (w *WechatV3) Capabilities() Capabilities {
	return Capabilities{Methods: append([]string{}, w.config.Methods...), Query: true, Close: true, Refund: true, RefundQuery: true}
}
func v3HTTPS(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Opaque == "" && u.RawQuery == "" && !u.ForceQuery && u.Fragment == ""
}
func (w *WechatV3) ValidateCreate(r CreateRequest) error {
	if r.ProfitSharing {
		if _, e := w.ChannelSharingRefundMode(r.Method); e != nil {
			return e
		}
	}
	if !enabled(r.Method, w.config.Methods) {
		return ErrUnsupported
	}
	if !validCreate(r) || !v3TradeNumber.MatchString(r.TradeNo) || len(r.Subject) > 127 || !utf8.ValidString(r.Subject) || strings.ContainsAny(r.Subject, "\x00\r\n") || !v3HTTPS(r.NotifyURL) {
		return ErrInvalid
	}
	if r.Method == "jsapi" && (r.OpenID == "" || len(r.OpenID) > 128 || !trustedBuyer(r.OpenID)) {
		return ErrInvalid
	}
	if r.Method == "h5" && (net.ParseIP(r.ClientIP) == nil || r.ReturnURL != "" && !v3HTTPS(r.ReturnURL)) {
		return ErrInvalid
	}
	if r.ExpiresAt != nil && (!r.ExpiresAt.After(time.Now()) || r.ExpiresAt.After(time.Now().Add(24*time.Hour))) {
		return ErrInvalid
	}
	return nil
}
func v3Object(raw []byte) (map[string]json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > 1<<20 || providerUniqueJSON(raw) != nil {
		return nil, ErrAuthentication
	}
	out, e := oauthObject(raw)
	if e != nil {
		return nil, ErrAuthentication
	}
	return out, nil
}
func v3String(o map[string]json.RawMessage, key string) string {
	return v3Text(o, key, 256)
}
func v3Text(o map[string]json.RawMessage, key string, limit int) string {
	var value string
	if json.Unmarshal(o[key], &value) != nil || len(value) > limit || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\x00\r\n") {
		return ""
	}
	return value
}
func v3Int(o map[string]json.RawMessage, key string) int64 {
	var value int64
	if json.Unmarshal(o[key], &value) != nil {
		return 0
	}
	return value
}
func (w *WechatV3) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := w.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	request := map[string]any{"appid": w.config.AppID, "mchid": w.config.MerchantID, "description": r.Subject, "out_trade_no": r.TradeNo, "notify_url": r.NotifyURL, "amount": map[string]any{"total": r.Amount, "currency": "CNY"}}
	if r.ProfitSharing {
		request["settle_info"] = map[string]bool{"profit_sharing": true}
	}
	if r.ExpiresAt != nil {
		request["time_expire"] = r.ExpiresAt.Format(time.RFC3339)
	}
	if r.Method == "jsapi" {
		request["payer"] = map[string]string{"openid": r.OpenID}
	}
	if r.Method == "h5" {
		request["scene_info"] = map[string]any{"payer_client_ip": r.ClientIP, "h5_info": map[string]string{"type": "Wap", "app_name": w.config.H5SiteName, "app_url": w.config.H5SiteURL}}
	}
	target := "/v3/pay/transactions/" + r.Method
	raw, e := w.call(ctx, "POST", target, request)
	if e != nil {
		return CreateResult{}, e
	}
	fail := func() (CreateResult, error) {
		return CreateResult{}, &UnknownOutcome{Operation: target, Cause: ErrAuthentication}
	}
	out, e := v3Object(raw)
	if e != nil {
		return fail()
	}
	switch r.Method {
	case "native":
		material := v3Text(out, "code_url", 512)
		u, e := url.Parse(material)
		if e != nil || u.Scheme != "weixin" || u.Host != "wxpay" || u.Path != "/bizpayurl" || u.User != nil || u.Fragment != "" || u.RawQuery == "" {
			return fail()
		}
		return CreateResult{MaterialType: "qrcode", Material: material}, nil
	case "h5":
		u, e := url.Parse(v3Text(out, "h5_url", 512))
		if e != nil || u.Scheme != "https" || u.Host != "wx.tenpay.com" || u.Path != "/cgi-bin/mmpayweb-bin/checkmweb" || u.User != nil || u.Fragment != "" {
			return fail()
		}
		if r.ReturnURL != "" {
			q := u.Query()
			q.Set("redirect_url", r.ReturnURL)
			u.RawQuery = q.Encode()
		}
		return CreateResult{MaterialType: "jump", Material: u.String()}, nil
	default:
		prepay := v3String(out, "prepay_id")
		if prepay == "" || len(prepay) > 128 {
			return fail()
		}
		n, e := nonce()
		if e != nil {
			return fail()
		}
		stamp := strconv.FormatInt(time.Now().Unix(), 10)
		signedPrepay := prepay
		if r.Method == "jsapi" {
			signedPrepay = "prepay_id=" + prepay
		}
		signature, e := signRSA([]byte(w.config.AppID+"\n"+stamp+"\n"+n+"\n"+signedPrepay+"\n"), w.config.PrivateKey)
		if e != nil {
			return fail()
		}
		sdk := map[string]string{"appId": w.config.AppID, "timeStamp": stamp, "nonceStr": n, "package": signedPrepay, "signType": "RSA", "paySign": signature}
		if r.Method == "app" {
			sdk = map[string]string{"appid": w.config.AppID, "partnerid": w.config.MerchantID, "prepayid": prepay, "package": "Sign=WXPay", "noncestr": n, "timestamp": stamp, "sign": signature}
		}
		body, e := json.Marshal(sdk)
		if e != nil {
			return fail()
		}
		return CreateResult{MaterialType: r.Method, Material: string(body)}, nil
	}
}
func (w *WechatV3) transaction(raw []byte, r QueryRequest) (QueryResult, error) {
	out, e := v3Object(raw)
	if e != nil {
		return QueryResult{}, e
	}
	amount, e := v3Object(out["amount"])
	if e != nil {
		return QueryResult{}, e
	}
	total, currency := v3Int(amount, "total"), v3String(amount, "currency")
	trade, providerNo, state := v3String(out, "out_trade_no"), v3String(out, "transaction_id"), v3String(out, "trade_state")
	if v3String(out, "appid") != w.config.AppID || v3String(out, "mchid") != w.config.MerchantID || trade != r.TradeNo || total <= 0 || total > 999999999999 || currency != "CNY" || r.ProviderTradeNo != "" && providerNo != r.ProviderTradeNo {
		return QueryResult{}, ErrAuthentication
	}
	switch state {
	case "SUCCESS", "REFUND":
		if providerNo == "" {
			return QueryResult{}, ErrAuthentication
		}
	case "NOTPAY", "CLOSED", "REVOKED", "USERPAYING", "PAYERROR":
	default:
		return QueryResult{}, ErrAuthentication
	}
	buyer := ""
	if value, exists := out["payer"]; exists {
		payer, e := v3Object(value)
		if e != nil {
			return QueryResult{}, e
		}
		buyer = v3String(payer, "openid")
		if buyer == "" || !trustedBuyer(buyer) {
			return QueryResult{}, ErrAuthentication
		}
	}
	if state == "SUCCESS" && buyer == "" {
		return QueryResult{}, ErrAuthentication
	}
	return QueryResult{TradeNo: trade, ProviderTradeNo: providerNo, State: state, Currency: currency, BuyerID: buyer, Amount: total}, nil
}
func (w *WechatV3) Query(ctx context.Context, r QueryRequest) (QueryResult, error) {
	if !v3TradeNumber.MatchString(r.TradeNo) {
		return QueryResult{}, ErrInvalid
	}
	raw, e := w.call(ctx, "GET", "/v3/pay/transactions/out-trade-no/"+url.PathEscape(r.TradeNo)+"?mchid="+url.QueryEscape(w.config.MerchantID), nil)
	if e != nil {
		return QueryResult{}, e
	}
	return w.transaction(raw, r)
}
func (w *WechatV3) Close(ctx context.Context, r QueryRequest) error {
	if !v3TradeNumber.MatchString(r.TradeNo) {
		return ErrInvalid
	}
	raw, e := w.call(ctx, "POST", "/v3/pay/transactions/out-trade-no/"+url.PathEscape(r.TradeNo)+"/close", map[string]string{"mchid": w.config.MerchantID})
	if e != nil {
		return e
	}
	if len(raw) != 0 {
		return &UnknownOutcome{Operation: "close", Cause: ErrAuthentication}
	}
	return nil
}
func validV3Refund(r RefundRequest) bool {
	return v3TradeNumber.MatchString(r.TradeNo) && v3RefundNumber.MatchString(r.RefundNo) && r.Amount > 0 && r.TotalAmount >= r.Amount && r.TotalAmount <= 999999999999 && len(r.Reason) <= 80 && utf8.ValidString(r.Reason) && !strings.ContainsAny(r.Reason, "\x00\r\n")
}
func (w *WechatV3) refundResult(raw []byte, r RefundRequest) (RefundResult, error) {
	out, e := v3Object(raw)
	if e != nil {
		return RefundResult{}, e
	}
	amount, e := v3Object(out["amount"])
	if e != nil {
		return RefundResult{}, e
	}
	state, refundNo, providerNo := v3String(out, "status"), v3String(out, "out_refund_no"), v3String(out, "refund_id")
	if refundNo != r.RefundNo || providerNo == "" || v3String(out, "out_trade_no") != r.TradeNo || r.ProviderTradeNo != "" && v3String(out, "transaction_id") != r.ProviderTradeNo || v3Int(amount, "total") != r.TotalAmount || v3Int(amount, "refund") != r.Amount || v3String(amount, "currency") != "CNY" {
		return RefundResult{}, ErrAuthentication
	}
	switch state {
	case "SUCCESS", "CLOSED", "PROCESSING", "ABNORMAL":
	default:
		return RefundResult{}, ErrAuthentication
	}
	return RefundResult{RefundNo: refundNo, ProviderRefundNo: providerNo, State: state, Amount: r.Amount}, nil
}
func (w *WechatV3) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if !validV3Refund(r) {
		return RefundResult{}, ErrInvalid
	}
	request := map[string]any{"out_trade_no": r.TradeNo, "out_refund_no": r.RefundNo, "amount": map[string]any{"total": r.TotalAmount, "refund": r.Amount, "currency": "CNY"}}
	if r.Reason != "" {
		request["reason"] = r.Reason
	}
	raw, e := w.call(ctx, "POST", "/v3/refund/domestic/refunds", request)
	if e != nil {
		return RefundResult{}, e
	}
	result, e := w.refundResult(raw, r)
	if e != nil {
		return RefundResult{}, &UnknownOutcome{Operation: "refund", Cause: e}
	}
	return result, nil
}
func (w *WechatV3) QueryRefund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if !validV3Refund(r) {
		return RefundResult{}, ErrInvalid
	}
	raw, e := w.call(ctx, "GET", "/v3/refund/domestic/refunds/"+url.PathEscape(r.RefundNo), nil)
	if e != nil {
		return RefundResult{}, e
	}
	return w.refundResult(raw, r)
}
func (w *WechatV3) VerifyNotification(_ context.Context, h http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	if len(raw) > 1<<20 || VerifyWechatV3Message(h, raw, w.config.VerificationKeys, time.Now(), 5*time.Minute) != nil {
		return PaymentEvent{}, ErrAuthentication
	}
	envelope, e := v3Object(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	id := v3String(envelope, "id")
	if id == "" || len(id) > 128 || v3String(envelope, "event_type") != "TRANSACTION.SUCCESS" || v3String(envelope, "resource_type") != "encrypt-resource" {
		return PaymentEvent{}, ErrAuthentication
	}
	resource, e := v3Object(envelope["resource"])
	if e != nil || v3String(resource, "original_type") != "transaction" {
		return PaymentEvent{}, ErrAuthentication
	}
	decrypted, e := DecryptWechatResource(w.config.APIv3Key, EncryptedResource{Algorithm: v3String(resource, "algorithm"), Nonce: v3String(resource, "nonce"), AssociatedData: v3String(resource, "associated_data"), Ciphertext: v3Text(resource, "ciphertext", 1<<20)})
	if e != nil {
		return PaymentEvent{}, e
	}
	payment, e := w.transaction(decrypted, QueryRequest{TradeNo: expected.TradeNo})
	if e != nil || payment.State != "SUCCESS" || expected.Amount <= 0 || payment.Amount != expected.Amount || expected.AppID != "" && expected.AppID != w.config.AppID || expected.MerchantID != "" && expected.MerchantID != w.config.MerchantID {
		return PaymentEvent{}, ErrAuthentication
	}
	return PaymentEvent{EventID: id, TradeNo: payment.TradeNo, ProviderTradeNo: payment.ProviderTradeNo, BuyerID: payment.BuyerID, State: payment.State, Currency: payment.Currency, Amount: payment.Amount}, nil
}
