package payment

// Port of epay_release/plugins/adapay (AdaPay聚合支付 / 汇付 AdaPay). Requests
// are signed SHA1withRSA over (gatewayURL + endpoint + body) with the merchant
// RSA private key, base64 in the "Signature" header; the API key goes in the
// "Authorization" header. Responses wrap the business object in a JSON string
// field "data". Callbacks POST form fields sign + data; the signature is
// SHA1withRSA over the raw "data" JSON verified with AdaPay's fixed public key.
//
// Payment creation: alipay_qr / union_qr QR (native), wx_pub/wx_lite &
// alipay_pub JSAPI (needs buyer id / openid), and page/checkout flows
// (alipay_lite h5, wx qr page, bank online/fast) that return a redirect URL.
// Refund hits POST /v1/payments/<id>/refunds.
//
// channelconfig: original input keys are kept (appid, appkey, appsecret,
// appmchid). None is a literal sub_* / sp_* / app_auth_token key, so
// directIdentityChannel() is unaffected. The sub-merchant API key rides in the
// original "appmchid" input (reported in p05.md).

import (
	"context"
	"crypto"
	"crypto/rsa"
	"net/http"
	"strings"
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "adapay",
		Title:  "AdaPay聚合支付",
		Author: "AdaPay",
		Link:   "https://www.adapay.tech/",
		Types:  []string{"alipay", "wxpay", "bank"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "应用App_ID", Required: true},
			{Key: "appkey", Label: "prod模式API_KEY", Secret: true, Required: true},
			{Key: "appsecret", Label: "商户RSA私钥", PEM: true, Required: true},
			{Key: "appmchid", Label: "子商户API_KEY", Note: "非进件模式请留空", Secret: true},
		},
		AppTypes: []PluginOption{
			// Union of select_alipay / select_wxpay / select_bank products.
			{Value: "1", Label: "扫码/银联/自有公众号"},
			{Value: "2", Label: "JS/快捷/动态二维码"},
			{Value: "3", Label: "托管小程序/网银"},
		},
		Build: newAdapayPlugin,
	})
}

const (
	adapayGateway     = "https://api.adapay.tech"
	adapayPageGateway = "https://page.adapay.tech"
	// AdaPay's fixed platform RSA public key (from inc/AdapayClient.php).
	adapayPublicKeyB64 = "MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQCwN6xgd6Ad8v2hIIsQVnbt8a3JituR8o4Tc3B5WlcFR55bz4OMqrG/356Ur3cPbc2Fe8ArNd/0gZbC9q56Eb16JTkVNA/fye4SXznWxdyBPR7+guuJZHc/VW2fKH2lfZ2P3Tt0QkKZZoawYOGSMdIvO+WqK44updyax0ikK6JlNQIDAQAB"
)

type adapayPlugin struct {
	pluginBase
	appID, apiKey string
	privateKey    *rsa.PrivateKey
	publicKey     *rsa.PublicKey
	appTypes      map[string]bool
}

func newAdapayPlugin(c PluginChannel) (Provider, error) {
	appID := strings.TrimSpace(c.Config["appid"])
	apiKey := strings.TrimSpace(c.Config["appkey"])
	if appID == "" || len(appID) > 64 || apiKey == "" || len(apiKey) > 128 ||
		strings.ContainsAny(appID+apiKey, " \t\r\n") {
		return nil, ErrInvalid
	}
	// Sub-merchant API key overrides the API key when present (进件模式).
	if sub := strings.TrimSpace(c.Config["appmchid"]); sub != "" {
		if len(sub) > 128 || strings.ContainsAny(sub, " \t\r\n") {
			return nil, ErrInvalid
		}
		apiKey = sub
	}
	priv, e := pluginPrivateKey(c.Certificates["appsecret"])
	if e != nil {
		return nil, ErrInvalid
	}
	pub, e := pluginPublicKey(adapayPublicKeyB64)
	if e != nil {
		return nil, ErrInvalid
	}
	base, e := newPluginBase(c, pluginAccount(adapayGateway, appID))
	if e != nil {
		return nil, e
	}
	p := &adapayPlugin{pluginBase: base, appID: appID, apiKey: apiKey, privateKey: priv, publicKey: pub, appTypes: map[string]bool{}}
	for _, t := range c.AppTypes {
		p.appTypes[t] = true
	}
	return p, nil
}

func (p *adapayPlugin) has(t string) bool {
	if len(p.appTypes) == 0 {
		return t == "1" // original default product
	}
	return p.appTypes[t]
}

// methods maps AdaPay products to GOEPAY methods per pay type.
func (p *adapayPlugin) methods() []string {
	switch p.payType {
	case "alipay":
		// 1 扫码 -> native; 2 JS -> jsapi; 3 托管小程序 -> h5 redirect (ali_h5_pay_url)
		var m []string
		if p.has("1") {
			m = append(m, "native")
		}
		if p.has("2") {
			m = append(m, "jsapi")
		}
		if p.has("3") {
			m = append(m, "h5")
		}
		return m
	case "wxpay":
		// 1 自有公众号/小程序 -> jsapi; 2 动态二维码 -> native; 3 托管小程序 -> h5 scheme
		var m []string
		if p.has("2") {
			m = append(m, "native")
		}
		if p.has("1") {
			m = append(m, "jsapi")
		}
		if p.has("3") {
			m = append(m, "h5")
		}
		return m
	case "bank":
		// 1 银联扫码 -> native; 2 快捷 -> h5 (checkout pay_url); 3 网银 -> h5
		var m []string
		if p.has("1") {
			m = append(m, "native")
		}
		if p.has("2") || p.has("3") {
			m = append(m, "h5")
		}
		return m
	}
	return nil
}

func (p *adapayPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

func (p *adapayPlugin) ValidateCreate(r CreateRequest) error {
	if e := validatePluginCreate(r, p.methods()); e != nil {
		return e
	}
	if r.Method == "jsapi" && (r.OpenID == "" || len(r.OpenID) > 128) {
		return ErrInvalid
	}
	return nil
}

func (p *adapayPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	payChannel := p.payChannel(r)
	if payChannel == "" && !p.isPageFlow(r) {
		return CreateResult{}, ErrInvalid
	}
	if p.isPageFlow(r) {
		return p.pagePay(ctx, r)
	}
	return p.addOrder(ctx, r, payChannel)
}

// isPageFlow reports create paths that use the page/checkout gateways which
// return a redirect URL (wx dynamic QR page, alipay/bank hosted flows).
func (p *adapayPlugin) isPageFlow(r CreateRequest) bool {
	switch {
	case p.payType == "wxpay" && r.Method == "native" && p.has("2"):
		return true // qrPrePay.qrPreOrder -> qr_pay_url
	case p.payType == "alipay" && r.Method == "h5":
		return true // prePay.preOrder -> ali_h5_pay_url
	case p.payType == "bank" && r.Method == "h5":
		return true // checkout online_pay / fast_pay -> pay_url
	}
	return false
}

// payChannel resolves the AdaPay pay_channel for the direct /v1/payments path.
func (p *adapayPlugin) payChannel(r CreateRequest) string {
	switch p.payType {
	case "alipay":
		switch r.Method {
		case "native":
			return "alipay_qr"
		case "jsapi":
			return "alipay_pub"
		}
	case "wxpay":
		switch r.Method {
		case "jsapi":
			return "wx_pub"
		}
	case "bank":
		if r.Method == "native" {
			return "union_qr"
		}
	}
	return ""
}

// addOrder calls POST /v1/payments (createPayment).
func (p *adapayPlugin) addOrder(ctx context.Context, r CreateRequest, payChannel string) (CreateResult, error) {
	body := p.orderBody(r, payChannel)
	data, e := p.request(ctx, "adapay.payments", http.MethodPost, adapayGateway, "/v1/payments", body)
	if e != nil {
		return CreateResult{}, e
	}
	expend := adapayObject(data, "expend")
	switch payChannel {
	case "alipay_qr", "union_qr":
		qr := jsonText(expend, "qrcode_url")
		res, ok := materialFromLinks("", qr, "")
		if !ok {
			return CreateResult{}, rejected("ADAPAY_NO_QRCODE")
		}
		res.ProviderTradeNo = jsonText(data, "id")
		return res, nil
	case "wx_pub", "wx_lite":
		info := jsonText(expend, "pay_info")
		if info == "" {
			return CreateResult{}, rejected("ADAPAY_NO_PAY_INFO")
		}
		return CreateResult{MaterialType: "jsapi", Material: info, ProviderTradeNo: jsonText(data, "id")}, nil
	case "alipay_pub":
		info := jsonText(expend, "pay_info")
		if info == "" {
			return CreateResult{}, rejected("ADAPAY_NO_PAY_INFO")
		}
		// pay_info is a JSON string {"tradeNO":"..."}; the browser needs tradeNO.
		obj, _ := callbackJSON([]byte(info))
		trade := jsonText(obj, "tradeNO")
		if trade == "" {
			trade = info
		}
		return CreateResult{MaterialType: "jsapi", Material: trade, ProviderTradeNo: jsonText(data, "id")}, nil
	}
	return CreateResult{}, ErrInvalid
}

// pagePay calls the page gateway (queryAdapay / checkout) returning a URL.
func (p *adapayPlugin) pagePay(ctx context.Context, r CreateRequest) (CreateResult, error) {
	var funcCode, payCh, urlField, gateway, endpoint string
	switch {
	case p.payType == "wxpay":
		funcCode, payCh, urlField = "qrPrePay.qrPreOrder", "", "qr_pay_url"
		gateway = adapayPageGateway
	case p.payType == "alipay":
		funcCode, payCh, urlField = "prePay.preOrder", "alipay_lite", "ali_h5_pay_url"
		gateway = adapayPageGateway
	case p.payType == "bank":
		funcCode, urlField = "checkout", "pay_url"
		payCh = "online_pay"
		if p.has("2") && !p.has("3") {
			payCh = "fast_pay"
		}
		gateway = adapayPageGateway
		endpoint = "/v1/checkout"
	}
	if endpoint == "" {
		endpoint = "/v1/" + strings.ReplaceAll(funcCode, ".", "/")
	}
	body := p.orderBody(r, payCh)
	body["adapay_func_code"] = funcCode
	body["callback_url"] = r.ReturnURL
	data, e := p.request(ctx, "adapay.page", http.MethodPost, gateway, endpoint, body)
	if e != nil {
		return CreateResult{}, e
	}
	expend := adapayObject(data, "expend")
	payURL := jsonText(expend, urlField)
	res, ok := materialFromLinks(payURL, "", "")
	if !ok {
		return CreateResult{}, rejected("ADAPAY_NO_PAY_URL")
	}
	res.ProviderTradeNo = jsonText(data, "id")
	return res, nil
}

// orderBody builds the common /v1/payments parameters. pay_amt is yuan.
func (p *adapayPlugin) orderBody(r CreateRequest, payChannel string) map[string]string {
	body := map[string]string{
		"app_id":      p.appID,
		"order_no":    r.TradeNo,
		"pay_amt":     yuan(r.Amount),
		"goods_title": r.Subject,
		"goods_desc":  r.Subject,
		"currency":    "cny",
		"notify_url":  r.NotifyURL,
	}
	if payChannel != "" {
		body["pay_channel"] = payChannel
	}
	return body
}

func (p *adapayPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.ProviderTradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	body := map[string]string{
		"payment_id":      r.ProviderTradeNo,
		"refund_order_no": r.RefundNo,
		"refund_amt":      yuan(r.Amount),
	}
	endpoint := "/v1/payments/" + r.ProviderTradeNo + "/refunds"
	data, e := p.request(ctx, "adapay.refund", http.MethodPost, adapayGateway, endpoint, body)
	if e != nil {
		return RefundResult{}, e
	}
	state := "PROCESSING"
	switch jsonText(data, "status") {
	case "succeeded":
		state = "SUCCESS"
	case "pending":
		state = "PROCESSING"
	case "failed":
		return RefundResult{}, rejected("ADAPAY_" + adapayErr(data))
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(data, "id"), State: state, Amount: r.Amount}, nil
}

// request signs, posts JSON, and unwraps the outer {data: "<json>"} envelope.
func (p *adapayPlugin) request(ctx context.Context, op, method, gateway, endpoint string, params map[string]string) (map[string]any, error) {
	bodyText := adapayCompactJSON(params)
	// Signature base = gateway + endpoint + body (the SDK's generateSignature).
	sig, e := rsaSign(p.privateKey, crypto.SHA1, gateway+endpoint+bodyText)
	if e != nil {
		return nil, ErrInvalid
	}
	headers := map[string]string{
		"Authorization": p.apiKey,
		"Signature":     sig,
		"sdk_version":   "v1.0.0",
	}
	raw, e := p.pluginCall(ctx, op, method, gateway+endpoint, []byte(bodyText), "application/json; charset=utf-8", headers)
	if e != nil {
		return nil, e
	}
	outer, e := callbackJSON(raw)
	if e != nil {
		return nil, &UnknownOutcome{Operation: op, Cause: e}
	}
	dataText := jsonText(outer, "data")
	if dataText == "" {
		// {message: ...} business/validation error with no data object.
		return nil, rejected("ADAPAY_" + adapayMessageCode(outer))
	}
	data, e := callbackJSON([]byte(dataText))
	if e != nil {
		return nil, &UnknownOutcome{Operation: op, Cause: e}
	}
	// SDK throws when status not succeeded/pending AND no expend.
	status := jsonText(data, "status")
	if status != "succeeded" && status != "pending" && adapayObject(data, "expend") == nil {
		return nil, rejected("ADAPAY_" + adapayErr(data))
	}
	return data, nil
}

// VerifyNotification verifies the SHA1withRSA signature over the raw "data"
// field with AdaPay's fixed public key, then checks status, trade no, amount.
func (p *adapayPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	form, e := callbackParams(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	sig, dataText := form["sign"], form["data"]
	if sig == "" || dataText == "" {
		return PaymentEvent{}, ErrInvalid
	}
	if !rsaVerify(p.publicKey, crypto.SHA1, dataText, sig) {
		return PaymentEvent{}, ErrAuthentication
	}
	data, e := callbackJSON([]byte(dataText))
	if e != nil {
		return PaymentEvent{}, ErrInvalid
	}
	if jsonText(data, "status") != "succeeded" {
		return PaymentEvent{}, ErrInvalid
	}
	amount, e := parseYuan(jsonText(data, "pay_amt"))
	if e != nil || expected.Amount <= 0 || amount != expected.Amount || jsonText(data, "order_no") != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	buyer := jsonText(adapayObject(data, "expend"), "sub_open_id")
	return paidEvent(expected.TradeNo, jsonText(data, "id"), buyer, amount), nil
}

// NotificationAck: the original echoes 'Ok' on success, 'No' on failure.
func (p *adapayPlugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return http.StatusOK, "text/plain; charset=utf-8", "No"
	}
	return http.StatusOK, "text/plain; charset=utf-8", "Ok"
}

// ---- local helpers ----

// adapayObject reads a nested JSON object field, nil when absent.
func adapayObject(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	if o, ok := m[key].(map[string]any); ok {
		return o
	}
	return nil
}

func adapayErr(data map[string]any) string {
	code := jsonText(data, "error_code")
	if code == "" {
		code = "FAILED"
	}
	return code
}

func adapayMessageCode(outer map[string]any) string {
	if c := jsonText(outer, "code"); c != "" {
		return c
	}
	return "NO_DATA"
}

// adapayCompactJSON serializes the body exactly as PHP json_encode would for a
// flat string map, with keys in INSERTION order. AdaPay signs the exact bytes,
// so order must match what we send — we therefore keep a fixed key order.
func adapayCompactJSON(m map[string]string) string {
	// Preserve a deterministic key order matching the PHP build order used by
	// AdapayClient (array_merge(params, {app_id})). The signature is over the
	// bytes we actually transmit, so any stable order the server accepts works
	// as long as sign and body agree — which they do here (same string).
	order := []string{"order_no", "pay_channel", "pay_amt", "goods_title", "goods_desc", "currency", "notify_url", "callback_url", "adapay_func_code", "payment_id", "refund_order_no", "refund_amt", "app_id"}
	seen := map[string]bool{}
	var b strings.Builder
	b.WriteByte('{')
	first := true
	emit := func(k string) {
		v, ok := m[k]
		if !ok || seen[k] {
			return
		}
		seen[k] = true
		if !first {
			b.WriteByte(',')
		}
		first = false
		b.WriteString(adapayQuote(k))
		b.WriteByte(':')
		b.WriteString(adapayQuote(v))
	}
	for _, k := range order {
		emit(k)
	}
	// Any keys not in the fixed order (future-proofing) appended stably.
	for k := range m {
		emit(k)
	}
	b.WriteByte('}')
	return b.String()
}

func adapayQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '/':
			b.WriteString(`\/`) // PHP json_encode escapes forward slashes by default
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

var _ Provider = (*adapayPlugin)(nil)
var _ NotificationVerifier = (*adapayPlugin)(nil)
var _ CreateValidator = (*adapayPlugin)(nil)
var _ NotificationAcker = (*adapayPlugin)(nil)
