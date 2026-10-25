package payment

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type AlipayConfig struct {
	EnableScanCancellation                           bool
	EnableChannelSharing, EnableChannelSharingReturn bool
	MiniProgram                                      *MiniProgramConfig
	AppID, SellerID                                  string
	PrivateKey                                       *rsa.PrivateKey
	PublicKey                                        *rsa.PublicKey
	GatewayURL                                       string
	HTTPClient                                       *http.Client
	Methods                                          []string
	Now                                              func() time.Time
	CertificateMode                                  *AlipayCertificateConfig
	EnableBankTransfer                               bool
	BankTransferScene                                string
	BankTransferReports                              []AlipayTransferSceneReport
	PreauthVoucher                                   bool
	PreauthVoucherProductCode                        string
	// NativeOrderCode selects the original apptype8 precreate product. It is
	// trusted channel configuration, never a caller-supplied payment method.
	NativeOrderCode bool
}
type Alipay struct {
	config       AlipayConfig
	certificates *alipayCertificateState
}

func NewAlipay(c AlipayConfig) (*Alipay, error) {
	if c.EnableChannelSharing && !alipaySharingUID.MatchString(c.SellerID) {
		return nil, ErrInvalid
	}
	if c.EnableChannelSharingReturn && !c.EnableChannelSharing {
		return nil, ErrInvalid
	}
	if c.PreauthVoucherProductCode != "" && (!c.PreauthVoucher || c.PreauthVoucherProductCode != "PRE_AUTH" && c.PreauthVoucherProductCode != "PREAUTH_PAY") {
		return nil, ErrInvalid
	}
	if c.AppID == "" || c.PrivateKey == nil || c.PrivateKey.N.BitLen() < 2048 || c.CertificateMode == nil && (c.PublicKey == nil || c.PublicKey.N.BitLen() < 2048) {
		return nil, ErrInvalid
	}
	if c.GatewayURL == "" {
		c.GatewayURL = "https://openapi.alipay.com/gateway.do"
	}
	u, e := url.Parse(c.GatewayURL)
	if e != nil || u.Host == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
		return nil, ErrInvalid
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	var certificates *alipayCertificateState
	if c.CertificateMode != nil {
		certificates, e = newAlipayCertificateState(c.CertificateMode, c.PrivateKey, c.PublicKey, c.Now())
		if e != nil {
			return nil, e
		}
	}
	if c.Methods == nil {
		c.Methods = []string{"page", "wap"}
	}
	if c.NativeOrderCode && !enabled("native", c.Methods) {
		return nil, ErrInvalid
	}
	if e = validateAlipayBankConfig(c, certificates != nil); e != nil {
		return nil, e
	}
	c.BankTransferReports = append([]AlipayTransferSceneReport{}, c.BankTransferReports...)
	c.MiniProgram, c.Methods, e = configureMiniProgram(c.MiniProgram, c.AppID, "alipay", c.Methods)
	if e != nil {
		return nil, e
	}
	for _, method := range c.Methods {
		if method != "page" && method != "wap" && method != "native" && method != "app" && method != "jsapi" && method != "mini" && method != "preauth" && method != "scan" && method != "applet" {
			return nil, ErrUnsupported
		}
	}
	c.Methods = append([]string{}, c.Methods...)
	c.HTTPClient = boundedClient(c.HTTPClient)
	return &Alipay{config: c, certificates: certificates}, nil
}
func (a *Alipay) ID() string            { return "alipay" }
func (a *Alipay) ApplicationID() string { return a.config.AppID }
func (a *Alipay) Capabilities() Capabilities {
	return Capabilities{Methods: append([]string{}, a.config.Methods...), Query: true, Close: true, Refund: true, RefundQuery: true}
}
func (a *Alipay) params(method string, biz map[string]any) (map[string]string, error) {
	body, e := json.Marshal(biz)
	if e != nil {
		return nil, e
	}
	p := map[string]string{"app_id": a.config.AppID, "method": method, "format": "JSON", "charset": "utf-8", "sign_type": "RSA2", "timestamp": a.config.Now().In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02 15:04:05"), "version": "1.0", "biz_content": string(body)}
	if a.certificates != nil {
		if !a.certificates.trusted(a.certificates.application, a.config.Now()) {
			return nil, ErrAuthentication
		}
		p["app_cert_sn"] = a.certificates.appSN
		p["alipay_root_cert_sn"] = a.certificates.rootSN
	}
	return p, nil
}
func (a *Alipay) call(ctx context.Context, method string, biz map[string]any, write bool, topLevel ...map[string]string) (map[string]json.RawMessage, error) {
	p, e := a.params(method, biz)
	if e != nil {
		return nil, e
	}
	for _, fields := range topLevel {
		for key, value := range fields {
			if key != "notify_url" && key != "return_url" {
				return nil, ErrInvalid
			}
			if value != "" {
				p[key] = value
			}
		}
	}
	p["sign"], e = SignAlipayRSA2(p, a.config.PrivateKey)
	if e != nil {
		return nil, e
	}
	form := url.Values{}
	for k, v := range p {
		form.Set(k, v)
	}
	req, e := http.NewRequestWithContext(ctx, "POST", a.config.GatewayURL, strings.NewReader(form.Encode()))
	if e != nil {
		return nil, e
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	res, e := a.config.HTTPClient.Do(req)
	if e != nil {
		if write {
			return nil, &UnknownOutcome{method, e}
		}
		return nil, e
	}
	defer res.Body.Close()
	if (method == "alipay.trade.cancel" || method == "alipay.trade.query" && scanObservationRequested(ctx)) && !authenticatedTLSExchange(req, res) {
		return nil, a.responseFailure(method, write)
	}
	raw, e := io.ReadAll(io.LimitReader(res.Body, 1<<20+1))
	if e != nil || len(raw) > 1<<20 || res.StatusCode != 200 {
		if write {
			return nil, &UnknownOutcome{method, errors.New("invalid upstream HTTP response")}
		}
		return nil, errors.New("invalid upstream HTTP response")
	}
	var envelope map[string]json.RawMessage
	if providerUniqueJSON(raw) != nil {
		return nil, a.responseFailure(method, write)
	}
	if e := json.Unmarshal(raw, &envelope); e != nil {
		return nil, a.responseFailure(method, write)
	}
	field := strings.ReplaceAll(method, ".", "_") + "_response"
	node := envelope[field]
	if len(node) == 0 {
		node = envelope["error_response"]
	}
	var signature string
	if e := json.Unmarshal(envelope["sign"], &signature); e != nil || len(node) == 0 || !a.verifyResponse(node, signature, envelope) {
		return nil, a.responseFailure(method, write)
	}
	var out map[string]json.RawMessage
	if e := json.Unmarshal(node, &out); e != nil {
		return nil, a.responseFailure(method, write)
	}
	if method == "alipay.trade.query" && scanObservationRequested(ctx) {
		if len(envelope[field]) == 0 {
			return nil, ErrAuthentication
		}
		for field, expected := range map[string]string{"app_id": a.config.AppID, "seller_id": a.config.SellerID} {
			if _, present := out[field]; present && text(out, field) != expected {
				return nil, ErrAuthentication
			}
		}
	}
	if text(out, "code") != "10000" {
		rejected := authenticatedProviderError("alipay", method, text(out, "code")+":"+text(out, "sub_code"), "alipay_rsa2", node)
		rejected.rejection.verifiedTLS = authenticatedTLSExchange(req, res)
		if method == "alipay.trade.pay" && len(envelope[field]) > 0 && a.config.SellerID != "" && authenticatedTLSExchange(req, res) {
			trusted := true
			for field, expected := range map[string]string{"app_id": a.config.AppID, "seller_id": a.config.SellerID, "out_trade_no": fmt.Sprint(biz["out_trade_no"]), "total_amount": fmt.Sprint(biz["total_amount"])} {
				if _, present := out[field]; present && text(out, field) != expected {
					trusted = false
				}
			}
			rejected.rejection.scanPendingTransport = trusted
		}
		return nil, rejected
	}
	return out, nil
}
func (a *Alipay) responseFailure(operation string, write bool) error {
	if write {
		return &UnknownOutcome{operation, ErrAuthentication}
	}
	return ErrAuthentication
}
func text(p map[string]json.RawMessage, k string) string {
	var s string
	json.Unmarshal(p[k], &s)
	return s
}
func (a *Alipay) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if r.ProfitSharing {
		if _, e := a.ChannelSharingRefundMode(r.Method); e != nil {
			return CreateResult{}, e
		}
	}
	if r.Method == "preauth" {
		return a.Freeze(ctx, r)
	}
	if e := a.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	biz := map[string]any{"out_trade_no": r.TradeNo, "total_amount": amountString(r.Amount), "subject": r.Subject}
	if r.ProfitSharing {
		biz["extend_params"] = map[string]string{"royalty_freeze": "true"}
	}
	if a.config.SellerID != "" {
		biz["seller_id"] = a.config.SellerID
	}
	if r.ExpiresAt != nil {
		biz["time_expire"] = r.ExpiresAt.In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02 15:04:05")
	}
	method := ""
	switch r.Method {
	case "page":
		method = "alipay.trade.page.pay"
		biz["product_code"] = "FAST_INSTANT_TRADE_PAY"
	case "wap":
		method = "alipay.trade.wap.pay"
		biz["product_code"] = "QUICK_WAP_WAY"
	case "native":
		method = "alipay.trade.precreate"
		if a.config.NativeOrderCode {
			biz["product_code"] = "QR_CODE_OFFLINE"
		}
		if r.ClientIP != "" {
			biz["business_params"] = map[string]string{"mc_create_trade_ip": r.ClientIP}
		}
		out, e := a.call(ctx, method, biz, true, map[string]string{"notify_url": r.NotifyURL})
		if e != nil {
			return CreateResult{}, e
		}
		if text(out, "out_trade_no") != r.TradeNo || text(out, "qr_code") == "" {
			return CreateResult{}, &UnknownOutcome{method, ErrAuthentication}
		}
		return CreateResult{MaterialType: "qrcode", Material: text(out, "qr_code")}, nil
	case "app":
		method = "alipay.trade.app.pay"
		biz["product_code"] = "QUICK_MSECURITY_PAY"
		if r.ExpiresAt == nil {
			biz["time_expire"] = a.config.Now().Add(2 * time.Hour).In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02 15:04:05")
		}
		if r.ClientIP != "" {
			biz["business_params"] = map[string]string{"mc_create_trade_ip": r.ClientIP}
		}
		p, e := a.params(method, biz)
		if e != nil {
			return CreateResult{}, e
		}
		p["notify_url"] = r.NotifyURL
		p["sign"], e = SignAlipayRSA2(p, a.config.PrivateKey)
		if e != nil {
			return CreateResult{}, e
		}
		values := url.Values{}
		for k, v := range p {
			values.Set(k, v)
		}
		return CreateResult{MaterialType: "app", Material: values.Encode()}, nil
	case "jsapi", "mini":
		if r.ExpiresAt == nil {
			biz["time_expire"] = a.config.Now().Add(2 * time.Hour).In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02 15:04:05")
		}
		if r.ClientIP != "" {
			biz["business_params"] = map[string]string{"mc_create_trade_ip": r.ClientIP}
		}
		buyerKey := "buyer_open_id"
		if r.Method == "mini" {
			biz["product_code"] = "JSAPI_PAY"
			biz["op_app_id"] = a.config.AppID
			buyerKey = "op_buyer_open_id"
		}
		if strings.HasPrefix(r.OpenID, "2088") && onlyDigits(r.OpenID) {
			buyerKey = "buyer_id"
		}
		biz[buyerKey] = r.OpenID
		out, e := a.call(ctx, "alipay.trade.create", biz, true, map[string]string{"notify_url": r.NotifyURL})
		if e != nil {
			return CreateResult{}, e
		}
		if text(out, "out_trade_no") != r.TradeNo || text(out, "trade_no") == "" {
			return CreateResult{}, &UnknownOutcome{"alipay.trade.create", ErrAuthentication}
		}
		// A created transaction is client payment material, not evidence of
		// successful payment. Only separately verified Query/notification settles.
		return CreateResult{MaterialType: "jsapi", Material: text(out, "trade_no"), ProviderTradeNo: text(out, "trade_no")}, nil
	default:
		return CreateResult{}, ErrUnsupported
	}
	p, e := a.params(method, biz)
	if e != nil {
		return CreateResult{}, e
	}
	if r.NotifyURL != "" {
		p["notify_url"] = r.NotifyURL
	}
	if r.ReturnURL != "" {
		p["return_url"] = r.ReturnURL
	}
	p["sign"], e = SignAlipayRSA2(p, a.config.PrivateKey)
	if e != nil {
		return CreateResult{}, e
	}
	var b strings.Builder
	b.WriteString(`<form method="post" action="` + html.EscapeString(a.config.GatewayURL) + `">`)
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteString(`<input type="hidden" name="` + html.EscapeString(k) + `" value="` + html.EscapeString(p[k]) + `">`)
	}
	b.WriteString(`<button type="submit">支付宝支付</button></form>`)
	return CreateResult{MaterialType: "html", Material: b.String()}, nil
}

func (a *Alipay) ValidateCreate(r CreateRequest) error {
	if r.ProfitSharing {
		if _, e := a.ChannelSharingRefundMode(r.Method); e != nil {
			return e
		}
	}
	if r.Method == "scan" || r.Method == "applet" {
		return ErrUnsupported
	}
	if e := a.validateCertificates(); e != nil {
		return e
	}
	if r.Method == "preauth" {
		return a.ValidateFreeze(r)
	}
	if !validCreate(r) {
		return ErrInvalid
	}
	if !enabled(r.Method, a.config.Methods) {
		return ErrUnsupported
	}
	if r.Method == "native" && r.ClientIP != "" && net.ParseIP(r.ClientIP) == nil {
		return ErrInvalid
	}
	if r.NotifyURL != "" && !validPaymentURL(r.NotifyURL) || r.ReturnURL != "" && !validPaymentURL(r.ReturnURL) || (r.Method == "app" || r.Method == "jsapi" || r.Method == "mini") && r.NotifyURL == "" {
		return ErrInvalid
	}
	if r.Method == "jsapi" || r.Method == "mini" {
		if r.OpenID == "" || strings.TrimSpace(r.OpenID) != r.OpenID || len(r.OpenID) > 128 || strings.HasPrefix(r.OpenID, "@") || strings.ContainsAny(r.OpenID, "\r\n\x00") {
			return ErrInvalid
		}
	}
	return nil
}
func (a *Alipay) Query(ctx context.Context, r QueryRequest) (QueryResult, error) {
	if r.TradeNo == "" {
		return QueryResult{}, ErrInvalid
	}
	out, e := a.call(ctx, "alipay.trade.query", map[string]any{"out_trade_no": r.TradeNo}, false)
	if e != nil {
		return QueryResult{}, e
	}
	n, e := parseAmount(text(out, "total_amount"))
	buyer := text(out, "buyer_user_id")
	if buyer == "" {
		buyer = text(out, "buyer_open_id")
	}
	if e != nil || text(out, "out_trade_no") != r.TradeNo || !trustedBuyer(buyer) || text(out, "trans_currency") != "" && text(out, "trans_currency") != "CNY" {
		return QueryResult{}, ErrAuthentication
	}
	return QueryResult{TradeNo: r.TradeNo, ProviderTradeNo: text(out, "trade_no"), State: text(out, "trade_status"), Currency: "CNY", Amount: n, BuyerID: buyer}, nil
}
func (a *Alipay) Close(ctx context.Context, r QueryRequest) error {
	if r.TradeNo == "" {
		return ErrInvalid
	}
	out, e := a.call(ctx, "alipay.trade.close", map[string]any{"out_trade_no": r.TradeNo}, true)
	if e != nil {
		return e
	}
	if text(out, "out_trade_no") != r.TradeNo {
		return &UnknownOutcome{"alipay.trade.close", ErrAuthentication}
	}
	return nil
}
func (a *Alipay) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	out, e := a.call(ctx, "alipay.trade.refund", map[string]any{"out_trade_no": r.TradeNo, "out_request_no": r.RefundNo, "refund_amount": amountString(r.Amount), "refund_reason": r.Reason}, true)
	if e != nil {
		return RefundResult{}, e
	}
	n, e := parseAmount(text(out, "refund_fee"))
	if e != nil || n != r.Amount || text(out, "out_trade_no") != r.TradeNo {
		return RefundResult{}, &UnknownOutcome{"alipay.trade.refund", ErrAuthentication}
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: text(out, "trade_no"), State: "SUCCESS", Amount: n}, nil
}
func (a *Alipay) QueryRefund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.RefundNo == "" {
		return RefundResult{}, ErrInvalid
	}
	out, e := a.call(ctx, "alipay.trade.fastpay.refund.query", map[string]any{"out_trade_no": r.TradeNo, "out_request_no": r.RefundNo}, false)
	if e != nil {
		return RefundResult{}, e
	}
	n, e := parseAmount(text(out, "refund_amount"))
	if e != nil || text(out, "out_trade_no") != r.TradeNo || text(out, "out_request_no") != r.RefundNo {
		return RefundResult{}, ErrAuthentication
	}
	state := text(out, "refund_status")
	if state == "" {
		state = "PROCESSING"
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: text(out, "trade_no"), State: state, Amount: n}, nil
}
func (a *Alipay) VerifyNotification(_ context.Context, _ http.Header, body []byte, expected ExpectedPayment) (PaymentEvent, error) {
	if len(body) > 1<<20 {
		return PaymentEvent{}, ErrInvalid
	}
	raw, e := url.ParseQuery(string(body))
	if e != nil {
		return PaymentEvent{}, ErrInvalid
	}
	p := map[string]string{}
	for k, vs := range raw {
		if len(vs) != 1 || strings.ContainsAny(k, "[]\x00") {
			return PaymentEvent{}, ErrInvalid
		}
		p[k] = vs[0]
	}
	if !a.verifyNotification(p) {
		return PaymentEvent{}, ErrAuthentication
	}
	if p["trans_currency"] != "" && p["trans_currency"] != "CNY" {
		return PaymentEvent{}, ErrAuthentication
	}
	n, e := parseAmount(p["total_amount"])
	if e != nil || n != expected.Amount || expected.Amount <= 0 || p["out_trade_no"] != expected.TradeNo || p["app_id"] != a.config.AppID || expected.AppID != "" && p["app_id"] != expected.AppID || a.config.SellerID != "" && p["seller_id"] != a.config.SellerID || expected.MerchantID != "" && p["seller_id"] != expected.MerchantID || p["trade_no"] == "" {
		return PaymentEvent{}, ErrAuthentication
	}
	if p["trade_status"] != "TRADE_SUCCESS" && p["trade_status"] != "TRADE_FINISHED" {
		return PaymentEvent{}, ErrInvalid
	}
	buyer := p["buyer_id"]
	if buyer == "" {
		buyer = p["buyer_open_id"]
	}
	// The original AOP canonicalizer excludes @file marker values. A field
	// excluded from the signature cannot become trusted reporting identity.
	if buyer != "" && (strings.TrimSpace(buyer) == "" || strings.HasPrefix(buyer, "@") || len(buyer) > 256) {
		return PaymentEvent{}, ErrAuthentication
	}
	return PaymentEvent{EventID: p["trade_no"], TradeNo: p["out_trade_no"], ProviderTradeNo: p["trade_no"], State: "SUCCESS", BuyerID: buyer, Currency: "CNY", Amount: n}, nil
}
func parseAmount(s string) (int64, error) {
	parts := strings.Split(s, ".")
	if len(parts) > 2 || len(parts[0]) == 0 || len(parts[0]) > 10 {
		return 0, ErrInvalid
	}
	for _, c := range parts[0] {
		if c < '0' || c > '9' {
			return 0, ErrInvalid
		}
	}
	whole, e := strconv.ParseInt(parts[0], 10, 64)
	if e != nil {
		return 0, ErrInvalid
	}
	fraction := "00"
	if len(parts) == 2 {
		if len(parts[1]) < 1 || len(parts[1]) > 2 {
			return 0, ErrInvalid
		}
		fraction = parts[1]
		for _, c := range fraction {
			if c < '0' || c > '9' {
				return 0, ErrInvalid
			}
		}
		if len(fraction) == 1 {
			fraction += "0"
		}
	}
	cents, e := strconv.ParseInt(fraction, 10, 64)
	if e != nil {
		return 0, ErrInvalid
	}
	n := whole*100 + cents
	if n <= 0 || n > 999999999999 {
		return 0, ErrInvalid
	}
	return n, nil
}

var _ Provider = (*Alipay)(nil)
var _ NotificationVerifier = (*Alipay)(nil)
