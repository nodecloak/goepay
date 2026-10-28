package payment

// Port of epay_release/plugins/wxpaynp (微信官方支付V3服务商版 / WeChat APIv3
// partner). Scope for this wave (per the brief): payments (native/jsapi/h5/app),
// refund, close and query ONLY. Combined (合单) orders, refund_combine,
// close_combine, complaint and merchant-risk notifications are OUT OF SCOPE.
//
// Signing reuses the shared WeChat v3 helpers in signature.go:
//   SignWechatV3Request  — merchant RSA-SHA256 over METHOD\nURL\nts\nnonce\nbody
//   VerifyWechatV3Message — platform RSA-SHA256 over ts\nnonce\nbody
//   DecryptWechatResource — AEAD_AES_256_GCM with the APIv3 key
// The service-provider fields sp_appid/sp_mchid/sub_mchid ride in the JSON body
// and query path, matching PartnerPaymentService.
//
// channelconfig: original input key names are kept (appid, appmchid, appsecret,
// appkey, publickeyid, appurl, appswitch). None is a literal sub_* / sp_* /
// app_auth_token config key, so directIdentityChannel() is NOT tripped — the
// sub-merchant number rides under the original "appurl" key and is only mapped
// to sub_mchid / sp_* on the wire. Reported in p05.md.

import (
	"context"
	"crypto"
	"crypto/rsa"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "wxpaynp",
		Title:  "微信官方支付V3服务商版",
		Author: "微信",
		Link:   "https://pay.weixin.qq.com/partner/public/home",
		Types:  []string{"wxpay", "qqpay"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "服务号/小程序/开放平台AppID", Required: true},
			{Key: "appmchid", Label: "服务商商户号", Required: true},
			{Key: "appsecret", Label: "商户APIv3密钥", Secret: true, Required: true},
			{Key: "appkey", Label: "商户API证书序列号", Required: true},
			{Key: "publickeyid", Label: "微信支付公钥ID", Note: "平台证书模式需要留空"},
			{Key: "appurl", Label: "子商户号", Required: true},
			{Key: "appswitch", Label: "服务商类型", Options: []PluginOption{{Value: "0", Label: "普通服务商"}, {Value: "1", Label: "平台收付通"}}},
			// Not in the original 'inputs' (loaded from cert/ on disk); added
			// because GOEPAY has no plugin filesystem.
			{Key: "merchant_privatekey", Label: "商户API私钥 (apiclient_key.pem)", PEM: true, Required: true},
			{Key: "platform_pubkey", Label: "微信支付公钥/平台证书公钥 (pub_key.pem)", PEM: true, Required: true},
		},
		AppTypes: []PluginOption{
			{Value: "1", Label: "Native支付"},
			{Value: "2", Label: "JSAPI支付"},
			{Value: "3", Label: "H5支付"},
			{Value: "5", Label: "APP支付"},
		},
		Note:  "子商户号填写特约商户号。商户API私钥与微信支付公钥/平台证书请填入对应输入框。合单支付、投诉与风险通知暂不支持。",
		Build: newWxpaynpPlugin,
	})
}

const wxpaynpGateway = "https://api.mch.weixin.qq.com"

type wxpaynpPlugin struct {
	pluginBase
	spAppID, spMchID, subMchID string
	apiV3Key                   []byte
	certSerial                 string
	platformSerial             string // publickeyid or platform cert serial (opaque; not validated here)
	privateKey                 *rsa.PrivateKey
	platformKey                *rsa.PublicKey
	appTypes                   map[string]bool
}

func newWxpaynpPlugin(c PluginChannel) (Provider, error) {
	spAppID := strings.TrimSpace(c.Config["appid"])
	spMchID := strings.TrimSpace(c.Config["appmchid"])
	subMchID := strings.TrimSpace(c.Config["appurl"])
	apiV3 := c.Config["appsecret"]
	serial := strings.TrimSpace(c.Config["appkey"])
	pubKeyID := strings.TrimSpace(c.Config["publickeyid"])
	if spAppID == "" || len(spAppID) > 64 || spMchID == "" || len(spMchID) > 32 ||
		subMchID == "" || len(subMchID) > 64 || serial == "" || len(serial) > 128 ||
		strings.ContainsAny(spAppID+spMchID+subMchID+serial, " \t\r\n") {
		return nil, ErrInvalid
	}
	// The sub_mchid input may be a comma list (合单); pick the first for the
	// single-order flows this port supports. Combined orders are out of scope.
	if i := strings.IndexByte(subMchID, ','); i >= 0 {
		subMchID = strings.TrimSpace(subMchID[:i])
		if subMchID == "" {
			return nil, ErrInvalid
		}
	}
	if len(apiV3) != 32 {
		return nil, ErrInvalid // APIv3 key is exactly 32 bytes
	}
	priv, e := pluginPrivateKey(c.Certificates["merchant_privatekey"])
	if e != nil {
		return nil, ErrInvalid
	}
	pub, e := pluginPublicKey(c.Certificates["platform_pubkey"])
	if e != nil {
		return nil, ErrInvalid
	}
	platformSerial := pubKeyID
	base, e := newPluginBase(c, pluginAccount(wxpaynpGateway, spMchID, subMchID))
	if e != nil {
		return nil, e
	}
	p := &wxpaynpPlugin{
		pluginBase: base, spAppID: spAppID, spMchID: spMchID, subMchID: subMchID,
		apiV3Key: []byte(apiV3), certSerial: serial, platformSerial: platformSerial,
		privateKey: priv, platformKey: pub, appTypes: map[string]bool{},
	}
	for _, t := range c.AppTypes {
		p.appTypes[t] = true
	}
	if len(c.AppTypes) == 0 {
		p.appTypes["1"] = true
	}
	return p, nil
}

func (p *wxpaynpPlugin) methods() []string {
	var m []string
	if p.appTypes["1"] {
		m = append(m, "native")
	}
	if p.appTypes["3"] {
		m = append(m, "h5")
	}
	if p.appTypes["5"] {
		m = append(m, "app")
	}
	if p.appTypes["2"] {
		m = append(m, "jsapi")
	}
	return m
}

func (p *wxpaynpPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Query: true, Close: true, Refund: true}
}

func (p *wxpaynpPlugin) ValidateCreate(r CreateRequest) error {
	if e := validatePluginCreate(r, p.methods()); e != nil {
		return e
	}
	if r.Method == "jsapi" && (r.OpenID == "" || len(r.OpenID) > 128) {
		return ErrInvalid
	}
	return nil
}

func (p *wxpaynpPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	var path string
	switch r.Method {
	case "native":
		path = "/v3/pay/partner/transactions/native"
	case "h5":
		path = "/v3/pay/partner/transactions/h5"
	case "app":
		path = "/v3/pay/partner/transactions/app"
	case "jsapi":
		path = "/v3/pay/partner/transactions/jsapi"
	default:
		return CreateResult{}, ErrInvalid
	}
	body := p.orderBody(r)
	resp, e := p.execute(ctx, "wxpaynp.pay", http.MethodPost, path, body)
	if e != nil {
		return CreateResult{}, e
	}
	out, e := callbackJSON(resp)
	if e != nil {
		return CreateResult{}, &UnknownOutcome{Operation: "wxpaynp.pay", Cause: e}
	}
	switch r.Method {
	case "native":
		codeURL := jsonText(out, "code_url")
		res, ok := materialFromLinks("", codeURL, "")
		if !ok {
			return CreateResult{}, rejected("WXPAYNP_NO_CODE_URL")
		}
		return res, nil
	case "h5":
		h5 := jsonText(out, "h5_url")
		if !validPaymentURL(h5) {
			return CreateResult{}, rejected("WXPAYNP_NO_H5_URL")
		}
		if r.ReturnURL != "" {
			h5 += "&redirect_url=" + url.QueryEscape(r.ReturnURL)
		}
		return CreateResult{MaterialType: "jump", Material: h5}, nil
	case "jsapi":
		prepay := jsonText(out, "prepay_id")
		if prepay == "" {
			return CreateResult{}, rejected("WXPAYNP_NO_PREPAY_ID")
		}
		return CreateResult{MaterialType: "jsapi", Material: p.jsapiParameters(prepay), ProviderTradeNo: prepay}, nil
	case "app":
		prepay := jsonText(out, "prepay_id")
		if prepay == "" {
			return CreateResult{}, rejected("WXPAYNP_NO_PREPAY_ID")
		}
		return CreateResult{MaterialType: "app", Material: p.appParameters(prepay), ProviderTradeNo: prepay}, nil
	}
	return CreateResult{}, ErrInvalid
}

// orderBody builds the v3 partner order JSON (as a canonical byte string).
func (p *wxpaynpPlugin) orderBody(r CreateRequest) []byte {
	b := wxpaynpJSON{}
	b.str("sp_appid", p.spAppID)
	b.str("sp_mchid", p.spMchID)
	b.str("sub_mchid", p.subMchID)
	b.str("description", r.Subject)
	b.str("out_trade_no", r.TradeNo)
	b.str("notify_url", r.NotifyURL)
	b.raw("amount", `{"total":`+strconv.FormatInt(r.Amount, 10)+`,"currency":"CNY"}`)
	if r.Method == "jsapi" {
		// sub_openid under a sub app, else sp_openid. We have no sub_appid
		// input, so bind to the service-provider appid (sp_openid).
		b.raw("payer", `{"sp_openid":`+wxpaynpQuote(r.OpenID)+`}`)
	}
	b.raw("scene_info", `{"payer_client_ip":`+wxpaynpQuote(wxpayslClientIP(r.ClientIP))+`}`)
	return []byte(b.done())
}

func (p *wxpaynpPlugin) jsapiParameters(prepayID string) string {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := wxpaynpNonce()
	pkg := "prepay_id=" + prepayID
	appID := p.spAppID
	// paySign = RSA-SHA256 over appId\ntimeStamp\nnonceStr\npackage\n
	msg := appID + "\n" + ts + "\n" + nonce + "\n" + pkg + "\n"
	sig, _ := rsaSign(p.privateKey, crypto.SHA256, msg)
	m := map[string]string{
		"appId": appID, "timeStamp": ts, "nonceStr": nonce,
		"package": pkg, "signType": "RSA", "paySign": sig,
	}
	return wxpayslJSON(m)
}

func (p *wxpaynpPlugin) appParameters(prepayID string) string {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := wxpaynpNonce()
	appID := p.spAppID
	// sign = RSA-SHA256 over appid\ntimestamp\nnoncestr\nprepayid\n
	msg := appID + "\n" + ts + "\n" + nonce + "\n" + prepayID + "\n"
	sig, _ := rsaSign(p.privateKey, crypto.SHA256, msg)
	m := map[string]string{
		"appid": appID, "partnerid": p.spMchID, "prepayid": prepayID,
		"package": "Sign=WXPay", "noncestr": nonce, "timestamp": ts, "sign": sig,
	}
	return wxpayslJSON(m)
}

func (p *wxpaynpPlugin) Query(ctx context.Context, r QueryRequest) (QueryResult, error) {
	var path string
	if r.ProviderTradeNo != "" {
		path = "/v3/pay/partner/transactions/id/" + url.PathEscape(r.ProviderTradeNo) + "?sp_mchid=" + p.spMchID + "&sub_mchid=" + p.subMchID
	} else if r.TradeNo != "" {
		path = "/v3/pay/partner/transactions/out-trade-no/" + url.PathEscape(r.TradeNo) + "?sp_mchid=" + p.spMchID + "&sub_mchid=" + p.subMchID
	} else {
		return QueryResult{}, ErrInvalid
	}
	resp, e := p.execute(ctx, "wxpaynp.query", http.MethodGet, path, nil)
	if e != nil {
		return QueryResult{}, e
	}
	out, e := callbackJSON(resp)
	if e != nil {
		return QueryResult{}, &UnknownOutcome{Operation: "wxpaynp.query", Cause: e}
	}
	if r.TradeNo != "" && jsonText(out, "out_trade_no") != "" && jsonText(out, "out_trade_no") != r.TradeNo {
		return QueryResult{}, ErrAuthentication
	}
	amount, _ := parseFen(jsonText(wxpaynpObject(out, "amount"), "payer_total"))
	if amount == 0 {
		amount, _ = parseFen(jsonText(wxpaynpObject(out, "amount"), "total"))
	}
	state := "NOTPAY"
	switch jsonText(out, "trade_state") {
	case "SUCCESS":
		state = "SUCCESS"
	case "REFUND":
		state = "REFUND"
	case "CLOSED", "REVOKED", "PAYERROR":
		state = "CLOSED"
	}
	buyer := jsonText(wxpaynpObject(out, "payer"), "sp_openid")
	if buyer == "" {
		buyer = jsonText(wxpaynpObject(out, "payer"), "sub_openid")
	}
	return QueryResult{TradeNo: r.TradeNo, ProviderTradeNo: jsonText(out, "transaction_id"), State: state, Currency: "CNY", BuyerID: buyer, Amount: amount}, nil
}

func (p *wxpaynpPlugin) Close(ctx context.Context, r QueryRequest) error {
	if r.TradeNo == "" {
		return ErrInvalid
	}
	path := "/v3/pay/partner/transactions/out-trade-no/" + url.PathEscape(r.TradeNo) + "/close"
	body := []byte(`{"sp_mchid":` + wxpaynpQuote(p.spMchID) + `,"sub_mchid":` + wxpaynpQuote(p.subMchID) + `}`)
	_, e := p.execute(ctx, "wxpaynp.close", http.MethodPost, path, body)
	return e
}

func (p *wxpaynpPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.ProviderTradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	b := wxpaynpJSON{}
	b.str("sub_mchid", p.subMchID)
	b.str("transaction_id", r.ProviderTradeNo)
	b.str("out_refund_no", r.RefundNo)
	b.raw("amount", `{"refund":`+strconv.FormatInt(r.Amount, 10)+`,"total":`+strconv.FormatInt(r.TotalAmount, 10)+`,"currency":"CNY"}`)
	resp, e := p.execute(ctx, "wxpaynp.refund", http.MethodPost, "/v3/refund/domestic/refunds", []byte(b.done()))
	if e != nil {
		return RefundResult{}, e
	}
	out, e := callbackJSON(resp)
	if e != nil {
		return RefundResult{}, &UnknownOutcome{Operation: "wxpaynp.refund", Cause: e}
	}
	state := "PROCESSING"
	switch jsonText(out, "status") {
	case "SUCCESS":
		state = "SUCCESS"
	case "PROCESSING":
		state = "PROCESSING"
	case "ABNORMAL", "CLOSED":
		return RefundResult{}, rejected("WXPAYNP_REFUND_" + jsonText(out, "status"))
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(out, "refund_id"), State: state, Amount: r.Amount}, nil
}

// execute signs the v3 request, sends it, and applies the outcome rules. It
// targets the production gateway; executeAt lets tests target a stub server.
func (p *wxpaynpPlugin) execute(ctx context.Context, op, method, path string, body []byte) ([]byte, error) {
	return p.executeAt(ctx, op, method, wxpaynpGateway, path, body)
}

// executeAt signs over `path` (which includes any query) and posts to
// gateway+path. The signature base and the Authorization header are identical
// to the SDK's getAuthorization.
func (p *wxpaynpPlugin) executeAt(ctx context.Context, op, method, gateway, path string, body []byte) ([]byte, error) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := wxpaynpNonce()
	sig, e := SignWechatV3Request(method, path, ts, nonce, body, p.privateKey)
	if e != nil {
		return nil, ErrInvalid
	}
	auth := "WECHATPAY2-SHA256-RSA2048 " + `mchid="` + p.spMchID + `",nonce_str="` + nonce + `",timestamp="` + ts + `",serial_no="` + p.certSerial + `",signature="` + sig + `"`
	headers := map[string]string{
		"Authorization": auth,
		"Accept":        "application/json",
	}
	raw, e := p.pluginCall(ctx, op, method, gateway+path, wxpaynpBody(body), wxpaynpContentType(method, body), headers)
	if e != nil {
		// A non-2xx business reply is a definitive ProviderError from
		// pluginCall (its body is discarded, so the generic HTTP code stands);
		// 5xx/transport already map to UnknownOutcome.
		return nil, e
	}
	return raw, nil
}

// VerifyNotification verifies the v3 callback signature with the platform key,
// decrypts the resource with the APIv3 key, then checks SUCCESS, trade no and
// amount. Combined notifications (combine_out_trade_no) are refused here —
// combined orders are out of scope for this port.
func (p *wxpaynpPlugin) VerifyNotification(_ context.Context, header http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	trusted := map[string]*rsa.PublicKey{}
	serial := header.Get("Wechatpay-Serial")
	trusted[serial] = p.platformKey
	if p.platformSerial != "" {
		trusted[p.platformSerial] = p.platformKey
	}
	if e := VerifyWechatV3Message(header, raw, trusted, time.Now(), 5*time.Minute); e != nil {
		return PaymentEvent{}, ErrAuthentication
	}
	outer, e := callbackJSON(raw)
	if e != nil {
		return PaymentEvent{}, ErrInvalid
	}
	res := wxpaynpObject(outer, "resource")
	if res == nil {
		return PaymentEvent{}, ErrInvalid
	}
	plain, e := DecryptWechatResource(p.apiV3Key, EncryptedResource{
		Algorithm:      jsonText(res, "algorithm"),
		Nonce:          jsonText(res, "nonce"),
		AssociatedData: jsonText(res, "associated_data"),
		Ciphertext:     jsonText(res, "ciphertext"),
	})
	if e != nil {
		return PaymentEvent{}, ErrAuthentication
	}
	data, e := callbackJSON(plain)
	if e != nil {
		return PaymentEvent{}, ErrInvalid
	}
	if _, combined := data["combine_out_trade_no"]; combined {
		return PaymentEvent{}, ErrUnsupported // 合单 out of scope
	}
	if jsonText(data, "trade_state") != "SUCCESS" {
		return PaymentEvent{}, ErrInvalid
	}
	if jsonText(data, "sp_mchid") != "" && jsonText(data, "sp_mchid") != p.spMchID {
		return PaymentEvent{}, ErrAuthentication
	}
	amount, ae := parseFen(jsonText(wxpaynpObject(data, "amount"), "payer_total"))
	if ae != nil {
		amount, ae = parseFen(jsonText(wxpaynpObject(data, "amount"), "total"))
	}
	if ae != nil || expected.Amount <= 0 || amount != expected.Amount || jsonText(data, "out_trade_no") != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	buyer := jsonText(wxpaynpObject(data, "payer"), "sp_openid")
	if buyer == "" {
		buyer = jsonText(wxpaynpObject(data, "payer"), "sub_openid")
	}
	return paidEvent(expected.TradeNo, jsonText(data, "transaction_id"), buyer, amount), nil
}

// NotificationAck: v3 answers JSON {"code":"SUCCESS"} on success; on failure a
// 4xx (the original sets HTTP 499) with {"code":"FAIL",...}.
func (p *wxpaynpPlugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return http.StatusBadRequest, "application/json; charset=utf-8", `{"code":"FAIL","message":"FAIL"}`
	}
	return http.StatusOK, "application/json; charset=utf-8", `{"code":"SUCCESS"}`
}

// ---- local helpers ----

// wxpaynpJSON builds a compact JSON object preserving insertion order. The v3
// signature is over the exact bytes transmitted, so any key order is valid as
// long as sign-base and body are identical (they are).
type wxpaynpJSON struct {
	b     strings.Builder
	first bool
	done_ bool
}

func (j *wxpaynpJSON) ensure() {
	if j.b.Len() == 0 {
		j.b.WriteByte('{')
		j.first = true
	}
}
func (j *wxpaynpJSON) sep() {
	j.ensure()
	if !j.first {
		j.b.WriteByte(',')
	}
	j.first = false
}
func (j *wxpaynpJSON) str(k, v string) {
	j.sep()
	j.b.WriteString(wxpaynpQuote(k))
	j.b.WriteByte(':')
	j.b.WriteString(wxpaynpQuote(v))
}
func (j *wxpaynpJSON) raw(k, rawJSON string) {
	j.sep()
	j.b.WriteString(wxpaynpQuote(k))
	j.b.WriteByte(':')
	j.b.WriteString(rawJSON)
}
func (j *wxpaynpJSON) done() string {
	j.ensure()
	j.b.WriteByte('}')
	return j.b.String()
}

func wxpaynpQuote(s string) string {
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
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func wxpaynpObject(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	if o, ok := m[key].(map[string]any); ok {
		return o
	}
	return nil
}

func wxpaynpNonce() string { return wxpayslNonce() }

// wxpaynpBody returns nil for an empty body so GET requests carry none.
func wxpaynpBody(body []byte) []byte {
	if len(body) == 0 {
		return nil
	}
	return body
}

// wxpaynpContentType sets JSON only for POST/PUT with a body (matches the SDK).
func wxpaynpContentType(method string, body []byte) string {
	if (method == http.MethodPost || method == http.MethodPut) && len(body) > 0 {
		return "application/json; charset=utf-8"
	}
	return ""
}

var _ Provider = (*wxpaynpPlugin)(nil)
var _ NotificationVerifier = (*wxpaynpPlugin)(nil)
var _ CreateValidator = (*wxpaynpPlugin)(nil)
var _ NotificationAcker = (*wxpaynpPlugin)(nil)
