package payment

// Port of epay_release/plugins/allinpay (通联支付 / Allinpay VSP). Requests are
// form-encoded with public fields orgid/appid/cusid/version/randomstr/signtype
// merged with the business params, signed RSA (openssl_sign default SHA1) over
// ksort(params) joined "k=v&…" (empty values skipped), base64 in the "sign"
// field, verified against the platform public key. The unified order endpoint
// returns JSON {retcode:SUCCESS, trxstatus:'0000', payinfo:<qr or url>}.
//
// paytype codes: A01 支付宝扫码, A02 支付宝JS, W01 微信扫码, W02 微信公众号,
// W06 微信小程序, Q01 QQ扫码, U01 云闪付扫码, U02 云闪付JS.
//
// channelconfig: original input keys are kept (appmchid, appid, appkey,
// appsecret, orgid). None is a literal sub_* / sp_* / app_auth_token key, so
// directIdentityChannel() is unaffected (reported in p05.md).

import (
	"context"
	"crypto"
	"crypto/rsa"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "allinpay",
		Title:  "通联支付",
		Author: "通联",
		Link:   "https://www.allinpay.com/",
		Types:  []string{"alipay", "wxpay", "qqpay", "bank"},
		Inputs: []PluginInput{
			{Key: "appmchid", Label: "商户号", Required: true},
			{Key: "appid", Label: "应用ID", Required: true},
			{Key: "appkey", Label: "通联公钥", PEM: true, Required: true},
			{Key: "appsecret", Label: "商户私钥", PEM: true, Required: true},
			{Key: "orgid", Label: "代理商商户号", Note: "仅代理商需要填写"},
		},
		AppTypes: []PluginOption{
			{Value: "1", Label: "扫码支付"},
			{Value: "2", Label: "JS支付/公众号小程序"},
		},
		Build: newAllinpayPlugin,
	})
}

const (
	allinpayPayURL    = "https://vsp.allinpay.com/apiweb/unitorder/pay"
	allinpayRefundURL = "https://vsp.allinpay.com/apiweb/tranx/refund"
	allinpayGateway   = "https://vsp.allinpay.com"
)

type allinpayPlugin struct {
	pluginBase
	orgID, cusID, appID string
	privateKey          *rsa.PrivateKey
	publicKey           *rsa.PublicKey
	appTypes            map[string]bool
}

func newAllinpayPlugin(c PluginChannel) (Provider, error) {
	cusID := strings.TrimSpace(c.Config["appmchid"])
	appID := strings.TrimSpace(c.Config["appid"])
	orgID := strings.TrimSpace(c.Config["orgid"])
	if cusID == "" || len(cusID) > 32 || appID == "" || len(appID) > 32 ||
		len(orgID) > 32 || strings.ContainsAny(cusID+appID+orgID, " \t\r\n&=") {
		return nil, ErrInvalid
	}
	priv, e := pluginPrivateKey(c.Certificates["appsecret"])
	if e != nil {
		return nil, ErrInvalid
	}
	pub, e := pluginPublicKey(c.Certificates["appkey"])
	if e != nil {
		return nil, ErrInvalid
	}
	base, e := newPluginBase(c, pluginAccount(allinpayGateway, cusID, appID))
	if e != nil {
		return nil, e
	}
	p := &allinpayPlugin{pluginBase: base, orgID: orgID, cusID: cusID, appID: appID, privateKey: priv, publicKey: pub, appTypes: map[string]bool{}}
	for _, t := range c.AppTypes {
		p.appTypes[t] = true
	}
	return p, nil
}

func (p *allinpayPlugin) has(t string) bool {
	if len(p.appTypes) == 0 {
		return t == "1"
	}
	return p.appTypes[t]
}

// methods: scan -> native; JS -> jsapi (needs openid). bank/qqpay only scan.
func (p *allinpayPlugin) methods() []string {
	switch p.payType {
	case "alipay", "wxpay":
		var m []string
		if p.has("1") || len(p.appTypes) == 0 {
			m = append(m, "native")
		}
		if p.has("2") {
			m = append(m, "jsapi")
		}
		return m
	case "qqpay", "bank":
		return []string{"native"}
	}
	return nil
}

func (p *allinpayPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

func (p *allinpayPlugin) ValidateCreate(r CreateRequest) error {
	if e := validatePluginCreate(r, p.methods()); e != nil {
		return e
	}
	if r.Method == "jsapi" && (r.OpenID == "" || len(r.OpenID) > 128) {
		return ErrInvalid
	}
	return nil
}

// payType code resolves the Allinpay paytype for this channel + method.
func (p *allinpayPlugin) paytypeCode(r CreateRequest) string {
	switch p.payType {
	case "alipay":
		if r.Method == "jsapi" {
			return "A02"
		}
		return "A01"
	case "wxpay":
		if r.Method == "jsapi" {
			return "W02" // 公众号 (W06 would be mini program; treated as JSAPI)
		}
		return "W01"
	case "qqpay":
		return "Q01"
	case "bank":
		if r.Method == "jsapi" {
			return "U02"
		}
		return "U01"
	}
	return ""
}

func (p *allinpayPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	code := p.paytypeCode(r)
	if code == "" {
		return CreateResult{}, ErrInvalid
	}
	params := p.publicParams("11")
	params["trxamt"] = strconv.FormatInt(r.Amount, 10)
	params["reqsn"] = r.TradeNo
	params["paytype"] = code
	params["body"] = r.Subject
	params["validtime"] = "30"
	params["notify_url"] = r.NotifyURL
	params["cusip"] = wxpayslClientIP(r.ClientIP) // reuse the shared-safe default
	if r.Method == "jsapi" {
		params["acct"] = r.OpenID
		if r.ReturnURL != "" {
			params["front_url"] = r.ReturnURL
		}
	}
	result, e := p.submit(ctx, "allinpay.pay", allinpayPayURL, params)
	if e != nil {
		return CreateResult{}, e
	}
	if result["trxstatus"] != "0000" {
		return CreateResult{}, rejected("ALLINPAY_" + allinpayNonEmpty(result["trxstatus"], "FAIL"))
	}
	payinfo := result["payinfo"]
	if payinfo == "" {
		return CreateResult{}, &UnknownOutcome{Operation: "allinpay.pay", Cause: errAllinNoPayinfo}
	}
	providerNo := result["trxid"]
	switch r.Method {
	case "jsapi":
		// payinfo is a JSON string of the wallet/JSAPI parameters (or a URL for
		// union JS). Return it as jsapi material.
		return CreateResult{MaterialType: "jsapi", Material: payinfo, ProviderTradeNo: providerNo}, nil
	default:
		// Scan: payinfo is a QR code content / URL.
		res, ok := materialFromLinks("", payinfo, "")
		if !ok {
			// Some products return a pay URL rather than QR content.
			if res2, ok2 := materialFromLinks(payinfo, "", ""); ok2 {
				res2.ProviderTradeNo = providerNo
				return res2, nil
			}
			return CreateResult{}, rejected("ALLINPAY_NO_PAYINFO")
		}
		res.ProviderTradeNo = providerNo
		return res, nil
	}
}

func (p *allinpayPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.ProviderTradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	params := p.publicParams("11")
	params["trxamt"] = strconv.FormatInt(r.Amount, 10)
	params["reqsn"] = r.RefundNo
	params["oldtrxid"] = r.ProviderTradeNo
	result, e := p.submit(ctx, "allinpay.refund", allinpayRefundURL, params)
	if e != nil {
		return RefundResult{}, e
	}
	// Original treats 0000 (done), 2008/2000 (processing/accepted) as success.
	switch result["trxstatus"] {
	case "0000":
		return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: result["trxid"], State: "SUCCESS", Amount: r.Amount}, nil
	case "2008", "2000":
		return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: result["trxid"], State: "PROCESSING", Amount: r.Amount}, nil
	default:
		return RefundResult{}, rejected("ALLINPAY_" + allinpayNonEmpty(result["trxstatus"], "FAIL"))
	}
}

// publicParams builds the signed public fields for one request. randomstr is a
// fresh nonce; version is "11" for pay/refund, "12" for the H5 cashier.
func (p *allinpayPlugin) publicParams(version string) map[string]string {
	m := map[string]string{
		"appid":     p.appID,
		"cusid":     p.cusID,
		"version":   version,
		"randomstr": allinpayNonce(),
		"signtype":  "RSA",
	}
	if p.orgID != "" {
		m["orgid"] = p.orgID
	}
	return m
}

// signContent is ksort(params), "k=v" joined by "&", skipping sign and empty.
func allinpaySignContent(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if k == "sign" || v == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+params[k])
	}
	return strings.Join(parts, "&")
}

func (p *allinpayPlugin) sign(params map[string]string) (string, error) {
	return rsaSign(p.privateKey, crypto.SHA1, allinpaySignContent(params))
}

// submit signs, posts form, parses JSON, applies retcode=SUCCESS gate.
func (p *allinpayPlugin) submit(ctx context.Context, op, target string, params map[string]string) (map[string]string, error) {
	sig, e := p.sign(params)
	if e != nil {
		return nil, ErrInvalid
	}
	params["sign"] = sig
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	raw, e := p.postForm(ctx, op, target, form)
	if e != nil {
		return nil, e
	}
	out, e := callbackJSON(raw)
	if e != nil {
		return nil, &UnknownOutcome{Operation: op, Cause: e}
	}
	retcode := jsonText(out, "retcode")
	if retcode != "SUCCESS" {
		// Communication-level refusal (definitive).
		return nil, rejected("ALLINPAY_" + allinpayNonEmpty(retcode, "COMM_FAIL"))
	}
	// Flatten to string map (values are strings/numbers).
	flat := map[string]string{}
	for k := range out {
		flat[k] = jsonText(out, k)
	}
	return flat, nil
}

// VerifyNotification verifies the RSA signature over the POSTed form, then the
// success status, our trade number (cusorderid) and amount (initamt, cents).
func (p *allinpayPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	params, e := callbackParams(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	sig := params["sign"]
	if sig == "" || !rsaVerify(p.publicKey, crypto.SHA1, allinpaySignContent(params), sig) {
		return PaymentEvent{}, ErrAuthentication
	}
	if params["trxstatus"] != "0000" {
		return PaymentEvent{}, ErrInvalid
	}
	if params["cusid"] != "" && params["cusid"] != p.cusID {
		return PaymentEvent{}, ErrAuthentication
	}
	if params["appid"] != "" && params["appid"] != p.appID {
		return PaymentEvent{}, ErrAuthentication
	}
	amount, e := parseFen(params["initamt"])
	if e != nil || expected.Amount <= 0 || amount != expected.Amount || params["cusorderid"] != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, params["trxid"], params["acct"], amount), nil
}

// NotificationAck: the original echoes plain "success"/"fail" — pluginBase's
// default. Kept explicit for clarity.
func (p *allinpayPlugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return http.StatusOK, "text/plain; charset=utf-8", "fail"
	}
	return http.StatusOK, "text/plain; charset=utf-8", "success"
}

// ---- local helpers ----

func allinpayNonce() string {
	// 20-char alphanumeric nonce (randomstr); reuse wxpaysl's safe generator.
	return wxpayslNonce()[:20]
}

func allinpayNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

var errAllinNoPayinfo = &ProviderError{Code: "ALLINPAY_NO_PAYINFO"}

var _ Provider = (*allinpayPlugin)(nil)
var _ NotificationVerifier = (*allinpayPlugin)(nil)
var _ CreateValidator = (*allinpayPlugin)(nil)
var _ NotificationAcker = (*allinpayPlugin)(nil)
