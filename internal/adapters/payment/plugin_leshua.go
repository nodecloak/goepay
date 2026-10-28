package payment

// Port of epay_release/plugins/leshua (乐刷聚合支付). Form-urlencoded requests to
// a single CGI gateway, XML replies. The signature is uppercase MD5 over the
// ksort'd "k=v&" string of the params (skipping sign and error_code, arrays
// rendered as empty) with "key=<交易密钥>" appended. Requests sign with the
// trade key (appkey); the async notify is XML, verified with the notify key
// (appsecret) and compared as a lowercase MD5, success when status == 2.
//
// Native QR (get_tdcode jspay_flag 0/2) and JSAPI (jspay_flag 1/3) are
// implemented. Refund (unified_refund) is implemented. No separate query API in
// the original SDK, so Query is left unsupported.

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

const leshuaAPIURL = "https://paygate.leshuazf.com/cgi-bin/lepos_pay_gateway.cgi"

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "leshua",
		Title:  "乐刷聚合支付",
		Author: "乐刷",
		Link:   "http://www.leshuazf.com/",
		Types:  []string{"alipay", "wxpay", "bank"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "商户号", Required: true},
			{Key: "appkey", Label: "交易密钥", Secret: true, Required: true},
			{Key: "appsecret", Label: "异步通知密钥", Secret: true, Required: true},
		},
		Note:  "Native/JSAPI聚合支付。退款已实现。无查单接口。",
		Build: newLeshuaPlugin,
	})
}

type leshuaPlugin struct {
	pluginBase
	merchantID string
	tradeKey   string
	notifyKey  string
	apiURL     string
}

func newLeshuaPlugin(c PluginChannel) (Provider, error) {
	mid := strings.TrimSpace(c.Config["appid"])
	tradeKey := c.Config["appkey"]
	notifyKey := c.Config["appsecret"]
	if mid == "" || len(mid) > 64 || strings.ContainsAny(mid, "&=?# ") || tradeKey == "" || len(tradeKey) > 256 || notifyKey == "" || len(notifyKey) > 256 {
		return nil, ErrInvalid
	}
	base, e := newPluginBase(c, pluginAccount("https://paygate.leshuazf.com", mid))
	if e != nil {
		return nil, e
	}
	return &leshuaPlugin{pluginBase: base, merchantID: mid, tradeKey: tradeKey, notifyKey: notifyKey, apiURL: leshuaAPIURL}, nil
}

func (p *leshuaPlugin) methods() []string {
	// Native (scan QR) for all types; JSAPI where the original offers it.
	return []string{"native", "jsapi"}
}

func (p *leshuaPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

func (p *leshuaPlugin) ValidateCreate(r CreateRequest) error {
	if e := validatePluginCreate(r, p.methods()); e != nil {
		return e
	}
	if r.Method == "jsapi" && strings.TrimSpace(r.OpenID) == "" {
		return ErrInvalid
	}
	return nil
}

// makeSign mirrors leshua_plugin::make_sign: ksort, skip sign and error_code,
// array values become empty, join "k=v&", append "key=<key>",
// strtoupper(md5(...)).
func leshuaMakeSign(params map[string]string, key string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		if k == "sign" || k == "error_code" {
			continue
		}
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(params[k])
		sb.WriteByte('&')
	}
	sb.WriteString("key=")
	sb.WriteString(key)
	return strings.ToUpper(md5Hex(sb.String()))
}

// payWay maps the channel pay type to leshua's pay_way.
func (p *leshuaPlugin) payWay() string {
	switch p.payType {
	case "alipay":
		return "ZFBZF"
	case "wxpay":
		return "WXZF"
	case "bank":
		return "UPSMZF"
	}
	return ""
}

// jspayFlag chooses the get_tdcode jspay_flag for the requested method and pay
// type, matching the original addOrder() call sites: alipay native=0 jsapi=1;
// wxpay native=2 jsapi=1; bank native=0.
func (p *leshuaPlugin) jspayFlag(method string) string {
	if method == "jsapi" {
		return "1"
	}
	if p.payType == "wxpay" {
		return "2"
	}
	return "0"
}

func (p *leshuaPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	params := map[string]string{
		"service":        "get_tdcode",
		"jspay_flag":     p.jspayFlag(r.Method),
		"pay_way":        p.payWay(),
		"merchant_id":    p.merchantID,
		"third_order_id": r.TradeNo,
		"amount":         strconv.FormatInt(r.Amount, 10),
		"body":           r.Subject,
		"notify_url":     r.NotifyURL,
		"client_ip":      r.ClientIP,
		"nonce_str":      leshuaNonce(r.TradeNo),
	}
	if r.Method == "jsapi" {
		params["sub_openid"] = r.OpenID
	}
	params["sign"] = leshuaMakeSign(params, p.tradeKey)

	out, e := p.post(ctx, "leshua.get_tdcode", params)
	if e != nil {
		return CreateResult{}, e
	}
	if e := leshuaBusinessOK(out); e != nil {
		return CreateResult{}, e
	}
	providerTradeNo := out["leshua_order_id"]
	if r.Method == "jsapi" {
		info := out["jspay_info"]
		if info == "" {
			return CreateResult{}, rejected("LESHUA_NO_PAYINFO")
		}
		return CreateResult{MaterialType: "jsapi", Material: info, ProviderTradeNo: providerTradeNo}, nil
	}
	// Native: wxpay returns jspay_url, alipay/bank return td_code.
	qr := out["td_code"]
	if qr == "" {
		qr = out["jspay_url"]
	}
	if qr == "" {
		return CreateResult{}, rejected("LESHUA_NO_QRCODE")
	}
	if strings.HasPrefix(qr, "http://") || strings.HasPrefix(qr, "https://") {
		return CreateResult{MaterialType: "jump", Material: qr, ProviderTradeNo: providerTradeNo}, nil
	}
	return CreateResult{MaterialType: "qrcode", Material: qr, ProviderTradeNo: providerTradeNo}, nil
}

// post sends the form and parses the flat XML reply into a map.
func (p *leshuaPlugin) post(ctx context.Context, operation string, params map[string]string) (map[string]string, error) {
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	raw, e := p.postForm(ctx, operation, p.apiURL, form)
	if e != nil {
		return nil, e
	}
	out, e := xmlFlat(raw)
	if e != nil {
		return nil, &UnknownOutcome{Operation: operation, Cause: e}
	}
	return out, nil
}

// leshuaBusinessOK maps the resp_code/result_code pair to an outcome: resp_code
// != 0 or result_code != 0 is a definitive business refusal.
func leshuaBusinessOK(out map[string]string) error {
	if out["resp_code"] != "0" {
		return rejected("LESHUA_" + out["resp_code"])
	}
	if out["result_code"] != "0" {
		return rejected("LESHUA_" + out["result_code"])
	}
	return nil
}

// Refund posts unified_refund.
func (p *leshuaPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.ProviderTradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	params := map[string]string{
		"service":            "unified_refund",
		"merchant_id":        p.merchantID,
		"leshua_order_id":    r.ProviderTradeNo,
		"merchant_refund_id": r.RefundNo,
		"refund_amount":      strconv.FormatInt(r.Amount, 10),
		"nonce_str":          leshuaNonce(r.RefundNo),
	}
	params["sign"] = leshuaMakeSign(params, p.tradeKey)
	out, e := p.post(ctx, "leshua.refund", params)
	if e != nil {
		return RefundResult{}, e
	}
	if e := leshuaBusinessOK(out); e != nil {
		return RefundResult{}, e
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: out["leshua_refund_id"], State: "SUCCESS", Amount: r.Amount}, nil
}

// VerifyNotification: XML POST, verified with the notify key (appsecret) as a
// lowercase MD5, success when status == 2.
func (p *leshuaPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	arr, e := xmlFlat(raw)
	if e != nil {
		return PaymentEvent{}, ErrInvalid
	}
	want := strings.ToLower(leshuaMakeSign(arr, p.notifyKey))
	if !signEqual(want, arr["sign"], true) {
		return PaymentEvent{}, ErrAuthentication
	}
	if arr["status"] != "2" {
		return PaymentEvent{}, ErrInvalid
	}
	if arr["third_order_id"] != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	amount, ae := parseFen(arr["account"])
	if ae != nil || expected.Amount <= 0 || amount != expected.Amount {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, arr["leshua_order_id"], arr["sub_openid"], amount), nil
}

// NotificationAck: the original echoes "000000" for success, "fail" otherwise.
func (p *leshuaPlugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return http.StatusOK, "text/plain; charset=utf-8", "fail"
	}
	return http.StatusOK, "text/plain; charset=utf-8", "000000"
}

// leshuaNonce derives a deterministic nonce_str from the trade/refund number
// (the original used getSid(), a random id; it is only required to be unique
// and is covered by the signature, so a stable per-order value is acceptable).
func leshuaNonce(seed string) string {
	return md5Hex("leshua:" + seed)
}

var _ Provider = (*leshuaPlugin)(nil)
var _ NotificationVerifier = (*leshuaPlugin)(nil)
var _ CreateValidator = (*leshuaPlugin)(nil)
var _ NotificationAcker = (*leshuaPlugin)(nil)
