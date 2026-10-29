package payment

// Port of epay_release/plugins/wxpaysl (微信官方支付服务商版 / WeChat APIv2
// service-provider). The upstream is WeChat Pay APIv2: XML bodies, MD5 sign
// over ksort(params)+"&key="+apikey, uppercased. The service-provider fields
// sub_mch_id (the original channel input "appurl") and sub_appid ride in the
// public params. Create maps to unifiedorder (NATIVE/JSAPI/MWEB/APP) and the
// returned code_url / prepay_id. Query/Close hit pay/orderquery (matching the
// original SDK, whose closeOrder also calls orderquery). Refund/reverse need
// the merchant API client certificate (mutual-TLS), so they are implemented
// only when the optional apiclient_cert/apiclient_key PEM inputs are present.
//
// NOTE on channelconfig: the original config key names are kept verbatim
// (appid, appmchid, appkey, appurl, apptype via AppTypes). None of them is a
// literal sub_appid/sub_mchid/sub_mch_id/sp_appid/sp_mchid/app_auth_token key,
// so channelconfig.directIdentityChannel() is NOT tripped — the sub-merchant
// number is carried under the original "appurl" key and only mapped to
// sub_mch_id on the wire. Reported in p05.md.

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "wxpaysl",
		Title:  "微信官方支付服务商版",
		Author: "微信",
		Link:   "https://pay.weixin.qq.com/partner/public/home",
		Types:  []string{"wxpay"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "服务号/小程序/开放平台AppID", Required: true},
			{Key: "appmchid", Label: "商户号", Required: true},
			{Key: "appkey", Label: "商户API密钥", Note: "APIv2密钥", Secret: true, Required: true},
			{Key: "appurl", Label: "子商户号", Required: true},
			// Not in the original 'inputs' (the original loads certificates
			// from /plugins/wxpaysl/cert/); added here because GOEPAY has no
			// plugin filesystem. Optional — only refund/reverse need them.
			{Key: "apiclient_cert", Label: "商户API证书 (apiclient_cert.pem)", PEM: true},
			{Key: "apiclient_key", Label: "商户API证书私钥 (apiclient_key.pem)", PEM: true},
		},
		AppTypes: []PluginOption{
			{Value: "1", Label: "Native支付"},
			{Value: "2", Label: "JSAPI支付"},
			{Value: "3", Label: "H5支付"},
			{Value: "5", Label: "APP支付"},
		},
		Note:  "子商户号填写服务商模式下的特约商户号。如需退款，请在证书输入框填入商户API证书与私钥。",
		Build: newWxpayslPlugin,
	})
}

const wxpayslGateway = "https://api.mch.weixin.qq.com"

type wxpayslPlugin struct {
	pluginBase
	appID, mchID, apiKey, subMchID string
	appTypes                       map[string]bool
	certClient                     *http.Client // mutual-TLS client for refund/reverse, nil when no cert
}

func newWxpayslPlugin(c PluginChannel) (Provider, error) {
	appID := strings.TrimSpace(c.Config["appid"])
	mchID := strings.TrimSpace(c.Config["appmchid"])
	apiKey := c.Config["appkey"]
	subMchID := strings.TrimSpace(c.Config["appurl"])
	if appID == "" || len(appID) > 64 || mchID == "" || len(mchID) > 32 ||
		apiKey == "" || len(apiKey) > 128 || subMchID == "" || len(subMchID) > 32 ||
		strings.ContainsAny(appID+mchID+subMchID, " \t\r\n") {
		return nil, ErrInvalid
	}
	base, e := newPluginBase(c, pluginAccount(wxpayslGateway, mchID, subMchID))
	if e != nil {
		return nil, e
	}
	p := &wxpayslPlugin{pluginBase: base, appID: appID, mchID: mchID, apiKey: apiKey, subMchID: subMchID, appTypes: map[string]bool{}}
	for _, t := range c.AppTypes {
		p.appTypes[t] = true
	}
	if len(c.AppTypes) == 0 {
		// Original default: NATIVE QR when no apptype restriction is set.
		p.appTypes["1"] = true
	}
	// Optional mutual-TLS certificate for refund/reverse.
	certPEM, keyPEM := c.Certificates["apiclient_cert"], c.Certificates["apiclient_key"]
	if certPEM != "" || keyPEM != "" {
		if certPEM == "" || keyPEM == "" {
			return nil, ErrInvalid
		}
		pair, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
		if err != nil {
			return nil, ErrInvalid
		}
		client := *base.client
		tr := &http.Transport{TLSClientConfig: &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}}
		client.Transport = tr
		p.certClient = &client
	}
	return p, nil
}

// methods maps the original 'apptype' products to GOEPAY methods for wxpay.
func (p *wxpayslPlugin) methods() []string {
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

func (p *wxpayslPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Query: true, Close: true, Refund: p.certClient != nil, RefundQuery: false}
}

func (p *wxpayslPlugin) ValidateCreate(r CreateRequest) error {
	if e := validatePluginCreate(r, p.methods()); e != nil {
		return e
	}
	if r.Method == "jsapi" && (r.OpenID == "" || len(r.OpenID) > 128) {
		return ErrInvalid
	}
	return nil
}

// sign computes the APIv2 MD5 signature (uppercase hex).
func (p *wxpayslPlugin) sign(params map[string]string) string {
	s, _ := SignWechatV2(params, p.apiKey, "MD5")
	return s
}

// publicParams are the fields every APIv2 service-provider request carries.
func (p *wxpayslPlugin) publicParams() map[string]string {
	return map[string]string{
		"appid":      p.appID,
		"mch_id":     p.mchID,
		"sub_mch_id": p.subMchID,
		"nonce_str":  wxpayslNonce(),
		"sign_type":  "MD5",
	}
}

func (p *wxpayslPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	params := p.publicParams()
	params["body"] = r.Subject
	params["out_trade_no"] = r.TradeNo
	params["total_fee"] = strconv.FormatInt(r.Amount, 10)
	params["spbill_create_ip"] = wxpayslClientIP(r.ClientIP)
	params["notify_url"] = r.NotifyURL
	switch r.Method {
	case "native":
		params["trade_type"] = "NATIVE"
		params["product_id"] = "01001"
	case "h5":
		params["trade_type"] = "MWEB"
	case "app":
		params["trade_type"] = "APP"
	case "jsapi":
		params["trade_type"] = "JSAPI"
		params["openid"] = r.OpenID
	default:
		return CreateResult{}, ErrInvalid
	}
	result, e := p.execute(ctx, "wxpaysl.unifiedorder", wxpayslGateway+"/pay/unifiedorder", params, false)
	if e != nil {
		return CreateResult{}, e
	}
	switch r.Method {
	case "native":
		codeURL := result["code_url"]
		if codeURL == "" || len(codeURL) > 4096 || strings.ContainsAny(codeURL, "\x00\r\n") {
			return CreateResult{}, &UnknownOutcome{Operation: "wxpaysl.unifiedorder", Cause: errors.New("missing code_url")}
		}
		return CreateResult{MaterialType: "qrcode", Material: codeURL, ProviderTradeNo: result["prepay_id"]}, nil
	case "h5":
		mweb := result["mweb_url"]
		if !validPaymentURL(mweb) {
			return CreateResult{}, &UnknownOutcome{Operation: "wxpaysl.unifiedorder", Cause: errors.New("missing mweb_url")}
		}
		if r.ReturnURL != "" {
			mweb += "&redirect_url=" + url.QueryEscape(r.ReturnURL)
		}
		return CreateResult{MaterialType: "jump", Material: mweb, ProviderTradeNo: result["prepay_id"]}, nil
	case "jsapi":
		prepay := result["prepay_id"]
		if prepay == "" {
			return CreateResult{}, &UnknownOutcome{Operation: "wxpaysl.unifiedorder", Cause: errors.New("missing prepay_id")}
		}
		return CreateResult{MaterialType: "jsapi", Material: p.jsapiParameters(prepay), ProviderTradeNo: prepay}, nil
	case "app":
		prepay := result["prepay_id"]
		if prepay == "" {
			return CreateResult{}, &UnknownOutcome{Operation: "wxpaysl.unifiedorder", Cause: errors.New("missing prepay_id")}
		}
		return CreateResult{MaterialType: "app", Material: p.appParameters(prepay), ProviderTradeNo: prepay}, nil
	}
	return CreateResult{}, ErrInvalid
}

// jsapiParameters replicates getJsApiParameters(): sub_appid preferred as appId.
func (p *wxpayslPlugin) jsapiParameters(prepayID string) string {
	appID := p.appID // original uses subAppId when present; we have no sub_appid input.
	params := map[string]string{
		"appId":     appID,
		"timeStamp": strconv.FormatInt(time.Now().Unix(), 10),
		"nonceStr":  wxpayslNonce(),
		"package":   "prepay_id=" + prepayID,
		"signType":  "MD5",
	}
	params["paySign"] = p.sign(params)
	return wxpayslJSON(params)
}

// appParameters replicates getAppParameters().
func (p *wxpayslPlugin) appParameters(prepayID string) string {
	params := map[string]string{
		"appid":     p.appID,
		"partnerid": p.mchID,
		"prepayid":  prepayID,
		"package":   "Sign=WXPay",
		"noncestr":  wxpayslNonce(),
		"timestamp": strconv.FormatInt(time.Now().Unix(), 10),
	}
	params["sign"] = p.sign(params)
	return wxpayslJSON(params)
}

// execute signs, posts XML, parses XML, and applies APIv2's return_code /
// result_code success gate and response sign check.
func (p *wxpayslPlugin) execute(ctx context.Context, op, target string, params map[string]string, useCert bool) (map[string]string, error) {
	params["sign"] = p.sign(params)
	body := wxpayslArrayToXML(params)
	client := p.client
	if useCert {
		if p.certClient == nil {
			return nil, ErrUnsupported
		}
		client = p.certClient
	}
	raw, e := wxpayslPost(ctx, client, op, target, body)
	if e != nil {
		return nil, e
	}
	result, e := xmlFlat(raw)
	if e != nil {
		return nil, &UnknownOutcome{Operation: op, Cause: e}
	}
	if result["return_code"] != "SUCCESS" {
		// Communication-level failure (e.g. SYSTEMERROR): definitive refusal.
		return nil, rejected("WXPAYSL_" + wxpayslNonEmpty(result["return_code"], "RETURN_FAIL"))
	}
	if result["result_code"] != "SUCCESS" {
		return nil, rejected("WXPAYSL_" + wxpayslNonEmpty(result["err_code"], "RESULT_FAIL"))
	}
	if result["sign"] != "" && !signEqual(p.sign(result), result["sign"], false) {
		return nil, &UnknownOutcome{Operation: op, Cause: errors.New("response sign mismatch")}
	}
	return result, nil
}

func (p *wxpayslPlugin) Query(ctx context.Context, r QueryRequest) (QueryResult, error) {
	if r.TradeNo == "" && r.ProviderTradeNo == "" {
		return QueryResult{}, ErrInvalid
	}
	params := p.publicParams()
	if r.ProviderTradeNo != "" {
		params["transaction_id"] = r.ProviderTradeNo
	} else {
		params["out_trade_no"] = r.TradeNo
	}
	result, e := p.execute(ctx, "wxpaysl.orderquery", wxpayslGateway+"/pay/orderquery", params, false)
	if e != nil {
		return QueryResult{}, e
	}
	if r.TradeNo != "" && result["out_trade_no"] != "" && result["out_trade_no"] != r.TradeNo {
		return QueryResult{}, ErrAuthentication
	}
	amount, ae := parseFen(result["total_fee"])
	if ae != nil {
		amount = 0
	}
	state := "NOTPAY"
	switch result["trade_state"] {
	case "SUCCESS":
		state = "SUCCESS"
	case "REFUND":
		state = "REFUND"
	case "CLOSED", "REVOKED", "PAYERROR":
		state = "CLOSED"
	case "NOTPAY", "USERPAYING":
		state = "NOTPAY"
	}
	return QueryResult{TradeNo: r.TradeNo, ProviderTradeNo: result["transaction_id"], State: state, Currency: "CNY", BuyerID: result["openid"], Amount: amount}, nil
}

// Close: the original closeOrder() calls pay/orderquery (not pay/closeorder).
// Mirror that exactly — a successful query means the order exists; there is no
// real close on the wire in the original.
func (p *wxpayslPlugin) Close(ctx context.Context, r QueryRequest) error {
	if r.TradeNo == "" {
		return ErrInvalid
	}
	params := p.publicParams()
	params["out_trade_no"] = r.TradeNo
	_, e := p.execute(ctx, "wxpaysl.close", wxpayslGateway+"/pay/orderquery", params, false)
	return e
}

func (p *wxpayslPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if p.certClient == nil {
		return RefundResult{}, ErrUnsupported
	}
	if r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount || (r.TradeNo == "" && r.ProviderTradeNo == "") {
		return RefundResult{}, ErrInvalid
	}
	params := p.publicParams()
	if r.ProviderTradeNo != "" {
		params["transaction_id"] = r.ProviderTradeNo
	} else {
		params["out_trade_no"] = r.TradeNo
	}
	params["out_refund_no"] = r.RefundNo
	params["total_fee"] = strconv.FormatInt(r.TotalAmount, 10)
	params["refund_fee"] = strconv.FormatInt(r.Amount, 10)
	result, e := p.execute(ctx, "wxpaysl.refund", wxpayslGateway+"/secapi/pay/refund", params, true)
	if e != nil {
		return RefundResult{}, e
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: result["refund_id"], State: "SUCCESS", Amount: r.Amount}, nil
}

// VerifyNotification verifies the APIv2 XML callback: sign, then SUCCESS state,
// then our trade number and amount (cents).
func (p *wxpayslPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	data, e := xmlFlat(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	if !signEqual(p.sign(data), data["sign"], false) {
		return PaymentEvent{}, ErrAuthentication
	}
	if data["return_code"] != "SUCCESS" || data["result_code"] != "SUCCESS" {
		return PaymentEvent{}, ErrInvalid
	}
	if data["appid"] != "" && data["appid"] != p.appID {
		return PaymentEvent{}, ErrAuthentication
	}
	if data["mch_id"] != "" && data["mch_id"] != p.mchID {
		return PaymentEvent{}, ErrAuthentication
	}
	if data["sub_mch_id"] != "" && data["sub_mch_id"] != p.subMchID {
		return PaymentEvent{}, ErrAuthentication
	}
	amount, ae := parseFen(data["total_fee"])
	if ae != nil || expected.Amount <= 0 || amount != expected.Amount || data["out_trade_no"] != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, data["transaction_id"], data["openid"], amount), nil
}

// NotificationAck answers the APIv2 XML the original replyNotify() echoes.
func (p *wxpayslPlugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return http.StatusOK, "application/xml; charset=utf-8", "<xml><return_code><![CDATA[FAIL]]></return_code><return_msg><![CDATA[FAIL]]></return_msg></xml>"
	}
	return http.StatusOK, "application/xml; charset=utf-8", "<xml><return_code><![CDATA[SUCCESS]]></return_code><return_msg><![CDATA[OK]]></return_msg></xml>"
}

// ---- local helpers (prefixed, no collisions) ----

// wxpayslArrayToXML renders a flat <xml> document like the SDK's array2Xml:
// numeric values bare, others CDATA. Keys are sorted for determinism.
func wxpayslArrayToXML(params map[string]string) []byte {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("<xml>")
	for _, k := range keys {
		v := params[k]
		if wxpayslIsNumeric(v) {
			b.WriteString("<" + k + ">" + v + "</" + k + ">")
		} else {
			b.WriteString("<" + k + "><![CDATA[" + wxpayslCDATA(v) + "]]></" + k + ">")
		}
	}
	b.WriteString("</xml>")
	return []byte(b.String())
}

func wxpayslIsNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// wxpayslCDATA neutralizes a CDATA terminator so values cannot break the XML.
func wxpayslCDATA(s string) string {
	return strings.ReplaceAll(s, "]]>", "]]]]><![CDATA[>")
}

// wxpayslPost sends an XML body with the pluginCall outcome rules, using the
// given client (plain or mutual-TLS). Mirrors pluginBase.pluginCall.
func wxpayslPost(ctx context.Context, client *http.Client, op, target string, body []byte) ([]byte, error) {
	if !validPaymentURL(target) {
		return nil, ErrInvalid
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if e != nil {
		return nil, ErrInvalid
	}
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	req.Header.Set("Accept", "*/*")
	resp, e := client.Do(req)
	if e != nil {
		return nil, &UnknownOutcome{Operation: op, Cause: e}
	}
	defer resp.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(resp.Body, 2<<20+1))
	if e != nil || len(raw) > 2<<20 {
		return nil, &UnknownOutcome{Operation: op, Cause: errors.New("unreadable upstream response")}
	}
	if resp.StatusCode >= 500 {
		return nil, &UnknownOutcome{Operation: op, Cause: fmt.Errorf("upstream HTTP %d", resp.StatusCode)}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &ProviderError{Code: fmt.Sprintf("HTTP_%d", resp.StatusCode)}
	}
	return raw, nil
}

// wxpayslNonce returns a 32-char lowercase alphanumeric nonce (SDK getNonceStr).
func wxpayslNonce() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return "00000000000000000000000000000000"
	}
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b)
}

var _ Provider = (*wxpayslPlugin)(nil)
var _ NotificationVerifier = (*wxpayslPlugin)(nil)
var _ CreateValidator = (*wxpayslPlugin)(nil)
var _ NotificationAcker = (*wxpayslPlugin)(nil)

// wxpayslJSON renders a flat string map as a compact JSON object with keys
// sorted, so jsapi/app material is deterministic and needs no struct. Values
// are JSON-string-escaped.
func wxpayslJSON(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(wxpayslQuote(k))
		b.WriteByte(':')
		b.WriteString(wxpayslQuote(m[k]))
	}
	b.WriteByte('}')
	return b.String()
}

// wxpayslQuote JSON-escapes a string (quotes included).
func wxpayslQuote(s string) string {
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
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func wxpayslClientIP(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" || len(ip) > 64 || strings.ContainsAny(ip, " \t\r\n") {
		return "127.0.0.1"
	}
	return ip
}

func wxpayslNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
