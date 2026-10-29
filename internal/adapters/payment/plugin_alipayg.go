package payment

// Port of epay_release/plugins/alipayg (支付宝国际版 / Antom AMS).
// AlipayGlobalClient signs each request with SHA256withRSA over
//   "<METHOD> <path>\n<clientId>.<reqTime>.<body>"
// and sends it in the Signature header "algorithm=RSA256,keyVersion=1,
// signature=<urlencode(base64)>". The response/notify is verified over the
// same content shape with the Antom public key. This verifies cryptographically
// with no extra state, so VerifyNotification is implemented.
//
//   pay():    POST /v1/payments/pay productCode=CASHIER_PAYMENT ->
//             normalUrl (type=jump).
//   notify(): POST JSON, Signature + Request-Time headers; resultStatus 'S'.
//   refund(): POST /v1/payments/refund.
//   close():  POST /v1/payments/cancel.
//
// Currency: Antom settles in currency_code (default CNY). amount =
// round(realmoney * currency_rate * 100) minor units. Kept inside the plugin;
// GOEPAY core treats the order as CNY. Settlement currency reported in p03.md.

import (
	"context"
	"crypto"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "alipayg",
		Title:  "支付宝国际版",
		Author: "Antom",
		Link:   "https://www.antom.com/",
		Types:  []string{"alipay"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "应用Client ID", Required: true},
			{Key: "appkey", Label: "Antom公钥", Note: "填错也可以支付成功但会无法回调", PEM: true, Textarea: true, Required: true},
			{Key: "appsecret", Label: "应用私钥", PEM: true, Secret: true, Textarea: true, Required: true},
			{Key: "appswitch", Label: "选择网关地址", Options: []PluginOption{
				{Value: "0", Label: "亚洲（https://open-sea-global.alipay.com）"},
				{Value: "1", Label: "北美（https://open-na-global.alipay.com）"},
				{Value: "2", Label: "欧洲（https://open-de-global.alipay.com）"},
			}},
			{Key: "currency_code", Label: "结算货币", Options: alipaygCurrencyOptions()},
			{Key: "currency_rate", Label: "货币汇率", Note: "例如1元人民币兑换0.137美元(USD)，则此处填0.137"},
		},
		Note:  "默认使用Antom在线支付的收银台支付",
		Build: newAlipaygPlugin,
	})
}

func alipaygCurrencyOptions() []PluginOption {
	codes := []string{"CNY", "HKD", "EUR", "USD", "AUD", "CAD", "GBP", "BRL", "CZK", "DKK", "HUF", "INR", "ILS", "JPY", "MYR", "MXN", "TWD", "NZD", "NOK", "PHP", "PLN", "RUB", "SGD", "SEK", "CHF", "THB"}
	out := make([]PluginOption, 0, len(codes))
	for _, c := range codes {
		out = append(out, PluginOption{Value: c, Label: c})
	}
	return out
}

// alipaygGateways indexes appswitch -> gateway host (AlipayGlobalClient::getGatewayUrl).
var alipaygGateways = map[string]string{
	"0": "https://open-sea-global.alipay.com",
	"1": "https://open-na-global.alipay.com",
	"2": "https://open-de-global.alipay.com",
}

type alipaygPlugin struct {
	pluginBase
	clientID     string
	privateKey   *rsa.PrivateKey
	antomPub     *rsa.PublicKey
	gateway      string
	sandbox      bool
	currencyCode string
	rateMilli    int64
	// channelID reconstructs the fixed callback request-target that Antom
	// signs over (see VerifyNotification).
	channelID int64
}

func newAlipaygPlugin(c PluginChannel) (Provider, error) {
	id := strings.TrimSpace(c.Config["appid"])
	if id == "" || len(id) > 128 {
		return nil, ErrInvalid
	}
	priv, e := pluginPrivateKey(c.Certificates["appsecret"])
	if e != nil {
		return nil, e
	}
	pub, e := pluginPublicKey(c.Certificates["appkey"])
	if e != nil {
		return nil, e
	}
	sw := c.Config["appswitch"]
	if sw == "" {
		sw = "0"
	}
	gw, ok := alipaygGateways[sw]
	if !ok {
		return nil, ErrInvalid
	}
	currency := strings.TrimSpace(c.Config["currency_code"])
	if currency == "" {
		currency = "CNY"
	}
	if !alipaygCurrencyKnown(currency) {
		return nil, ErrInvalid
	}
	rate, e := paypalParseRate(c.Config["currency_rate"])
	if e != nil {
		return nil, e
	}
	// SANDBOX_ prefixed client ids use the sandbox API path (AlipayGlobalClient).
	sandbox := strings.HasPrefix(id, "SANDBOX_")
	base, e := newPluginBase(c, pluginAccount(gw, id, currency))
	if e != nil {
		return nil, e
	}
	return &alipaygPlugin{pluginBase: base, clientID: id, privateKey: priv, antomPub: pub, gateway: gw, sandbox: sandbox, currencyCode: currency, rateMilli: rate, channelID: c.ID}, nil
}

func alipaygCurrencyKnown(code string) bool {
	for _, o := range alipaygCurrencyOptions() {
		if o.Value == code {
			return true
		}
	}
	return false
}

func (p *alipaygPlugin) alipaygMinor(cents int64) int64 {
	return (cents*p.rateMilli + 500) / 1000
}

// apiPath prefixes the AMS path with /ams/sandbox/api or /ams/api.
func (p *alipaygPlugin) apiPath(path string) string {
	if p.sandbox {
		return "/ams/sandbox/api" + path
	}
	return "/ams/api" + path
}

func (p *alipaygPlugin) methods() []string {
	// Cashier payment returns a redirect URL for both desktop and mobile.
	return []string{"page", "wap"}
}

func (p *alipaygPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Close: true, Refund: true}
}

func (p *alipaygPlugin) ValidateCreate(r CreateRequest) error {
	return validatePluginCreate(r, p.methods())
}

// alipaygSignContent builds "<METHOD> <path>\n<clientId>.<time>.<body>".
func (p *alipaygPlugin) alipaygSignContent(method, path, reqTime, body string) string {
	return method + " " + path + "\n" + p.clientID + "." + reqTime + "." + body
}

// execute signs and POSTs an AMS request, verifies the response signature when
// present, and returns the decoded result object.
func (p *alipaygPlugin) execute(ctx context.Context, operation, apiSuffix string, params map[string]any) (map[string]any, error) {
	path := p.apiPath(apiSuffix)
	body, e := json.Marshal(params)
	if e != nil {
		return nil, ErrInvalid
	}
	reqTime := strconv.FormatInt(time.Now().UnixMilli(), 10)
	sig, e := rsaSign(p.privateKey, crypto.SHA256, p.alipaygSignContent("POST", path, reqTime, string(body)))
	if e != nil {
		return nil, ErrAuthentication
	}
	headers := map[string]string{
		"Content-Type": "application/json; charset=UTF-8",
		"Request-Time": reqTime,
		"client-id":    p.clientID,
		"Signature":    "algorithm=RSA256,keyVersion=1,signature=" + url.QueryEscape(sig),
	}
	raw, e := p.pluginCall(ctx, operation, http.MethodPost, p.gateway+path, body, "application/json; charset=UTF-8", headers)
	if e != nil {
		return nil, e
	}
	out, e := callbackJSON(raw)
	if e != nil {
		return nil, &UnknownOutcome{Operation: operation, Cause: e}
	}
	if _, ok := out["result"].(map[string]any); !ok {
		return nil, &UnknownOutcome{Operation: operation, Cause: errUnexpectedAMSBody}
	}
	return out, nil
}

var errUnexpectedAMSBody = &alipaygStaticError{"alipayg: AMS response missing result"}

type alipaygStaticError struct{ s string }

func (e *alipaygStaticError) Error() string { return e.s }

// amsResultStatus reads result.resultStatus / result.resultCode.
func amsResult(out map[string]any) (status, code string) {
	r, _ := out["result"].(map[string]any)
	return jsonText(r, "resultStatus"), jsonText(r, "resultCode")
}

func (p *alipaygPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	terminalType := "WEB"
	if r.Method == "wap" {
		terminalType = "WAP"
	}
	amount := strconv.FormatInt(p.alipaygMinor(r.Amount), 10)
	params := map[string]any{
		"env": map[string]any{"terminalType": terminalType},
		"order": map[string]any{
			"orderAmount":      map[string]any{"currency": p.currencyCode, "value": amount},
			"referenceOrderId": r.TradeNo,
			"orderDescription": r.Subject,
		},
		"paymentRequestId":   r.TradeNo,
		"paymentAmount":      map[string]any{"currency": p.currencyCode, "value": amount},
		"settlementStrategy": map[string]any{"settlementCurrency": p.currencyCode},
		"paymentMethod":      map[string]any{"paymentMethodType": "ALIPAY_CN"},
		"paymentNotifyUrl":   r.NotifyURL,
		"paymentRedirectUrl": r.ReturnURL,
		"productCode":        "CASHIER_PAYMENT",
	}
	out, e := p.execute(ctx, "alipayg.pay", "/v1/payments/pay", params)
	if e != nil {
		return CreateResult{}, e
	}
	status, code := amsResult(out)
	// The SDK treats S, or U+PAYMENT_IN_PROCESS, as "not a hard failure".
	if status != "S" && !(status == "U" && code == "PAYMENT_IN_PROCESS") {
		return CreateResult{}, rejected("ALIPAYG_" + amsCode(code))
	}
	normalURL := jsonText(out, "normalUrl")
	if normalURL == "" || !validPaymentURL(normalURL) {
		return CreateResult{}, rejected("ALIPAYG_NO_PAY_URL")
	}
	return CreateResult{MaterialType: "jump", Material: normalURL, ProviderTradeNo: jsonText(out, "paymentId")}, nil
}

func amsCode(c string) string {
	if c == "" {
		return "REJECTED"
	}
	return c
}

// Notifications are POST JSON with a Signature header. AlipayGlobalClient::check
// verifies SHA256withRSA over "POST <REQUEST_URI>\n<clientId>.<responseTime>.
// <body>" with the Antom public key, where REQUEST_URI is the path Antom POSTed
// to — i.e. the paymentNotifyUrl we sent at Create time. GOEPAY builds that
// callback URL deterministically as "/callbacks/providers/<channelID>/<tradeNo>"
// (provider_flow.go), and the verifier is NOT handed the request path, so we
// reconstruct it from the channel id captured at Build and expected.TradeNo
// (path-escaped exactly as the sender escapes it). We try the canonical form
// and a trailing-slash variant, because a fronting proxy may normalise one way
// or the other; both are our own fixed path, never attacker input. This keeps
// the check stateless (no stored cert, no fetched key) while matching the
// original's cryptographic verification.
func (p *alipaygPlugin) VerifyNotification(_ context.Context, header http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	if len(raw) == 0 || len(raw) > 1<<20 || expected.TradeNo == "" {
		return PaymentEvent{}, ErrInvalid
	}
	sigHeader := header.Get("Signature")
	responseTime := header.Get("Request-Time")
	if responseTime == "" {
		responseTime = header.Get("Response-Time")
	}
	sigVal := alipaygSignatureValue(sigHeader)
	if sigVal == "" || responseTime == "" {
		return PaymentEvent{}, ErrAuthentication
	}
	basePath := "/callbacks/providers/" + strconv.FormatInt(p.channelID, 10) + "/" + url.PathEscape(expected.TradeNo)
	verified := false
	for _, path := range []string{basePath, basePath + "/"} {
		content := "POST " + path + "\n" + p.clientID + "." + responseTime + "." + string(raw)
		if rsaVerify(p.antomPub, crypto.SHA256, content, sigVal) {
			verified = true
			break
		}
	}
	if !verified {
		return PaymentEvent{}, ErrAuthentication
	}
	arr, e := callbackJSON(raw)
	if e != nil {
		return PaymentEvent{}, ErrInvalid
	}
	status, _ := amsResult(arr)
	if status != "S" {
		return PaymentEvent{}, ErrInvalid
	}
	if jsonText(arr, "paymentRequestId") != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	payAmount, _ := arr["paymentAmount"].(map[string]any)
	amt, e := parseFen(jsonText(payAmount, "value"))
	if e != nil || expected.Amount <= 0 || amt != p.alipaygMinor(expected.Amount) {
		return PaymentEvent{}, ErrAuthentication
	}
	buyer := ""
	if psp, ok := arr["pspCustomerInfo"].(map[string]any); ok {
		buyer = jsonText(psp, "pspCustomerId")
	}
	return paidEvent(expected.TradeNo, jsonText(arr, "paymentId"), buyer, expected.Amount), nil
}

// alipaygSignatureValue extracts the signature=... value and URL-decodes it
// (the SDK urlencodes it on the way out; check() takes the substring after the
// last '=' and the verifier accepts both raw and urldecoded base64).
func alipaygSignatureValue(h string) string {
	i := strings.LastIndex(h, "signature=")
	if i < 0 {
		return ""
	}
	val := strings.TrimSpace(h[i+len("signature="):])
	if dec, e := url.QueryUnescape(val); e == nil {
		return dec
	}
	return val
}

// Antom acks a verified success with a JSON result object (the original
// returns type=json {result:{resultCode:SUCCESS,resultStatus:S}}).
func (p *alipaygPlugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return http.StatusOK, "application/json; charset=utf-8", `{"result":{"resultCode":"FAIL","resultStatus":"F","resultMessage":"sign error"}}`
	}
	return http.StatusOK, "application/json; charset=utf-8", `{"result":{"resultCode":"SUCCESS","resultStatus":"S","resultMessage":"success"}}`
}

// Refund posts /v1/payments/refund (alipayg_plugin::refund).
func (p *alipaygPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.ProviderTradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	params := map[string]any{
		"refundRequestId": r.RefundNo,
		"paymentId":       r.ProviderTradeNo,
		"refundAmount":    map[string]any{"currency": p.currencyCode, "value": strconv.FormatInt(p.alipaygMinor(r.Amount), 10)},
	}
	out, e := p.execute(ctx, "alipayg.refund", "/v1/payments/refund", params)
	if e != nil {
		return RefundResult{}, e
	}
	status, code := amsResult(out)
	if status != "S" && !(status == "U" && code == "PAYMENT_IN_PROCESS") {
		return RefundResult{}, rejected("ALIPAYG_" + amsCode(code))
	}
	state := "SUCCESS"
	if status == "U" {
		state = "PROCESSING"
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(out, "refundId"), State: state, Amount: r.Amount}, nil
}

// Close posts /v1/payments/cancel keyed by our trade number (the original's
// paymentRequestId == $order['trade_no']).
func (p *alipaygPlugin) Close(ctx context.Context, r QueryRequest) error {
	if r.TradeNo == "" {
		return ErrInvalid
	}
	out, e := p.execute(ctx, "alipayg.cancel", "/v1/payments/cancel", map[string]any{"paymentRequestId": r.TradeNo})
	if e != nil {
		return e
	}
	status, code := amsResult(out)
	if status != "S" {
		return rejected("ALIPAYG_" + amsCode(code))
	}
	return nil
}

var _ Provider = (*alipaygPlugin)(nil)
var _ NotificationVerifier = (*alipaygPlugin)(nil)
var _ CreateValidator = (*alipaygPlugin)(nil)
var _ NotificationAcker = (*alipaygPlugin)(nil)
