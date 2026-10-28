package payment

// Port of epay_release/plugins/ltzf (蓝兔支付). Form POST to https://api.ltzf.cn
// with an UPPERCASE MD5 sign over a FIXED NAMED SUBSET of fields in ksort order
// ("k=v&"...), then "key=<appkey>" appended (ltzf_plugin::make_sign). The
// product endpoint is chosen by pay type + selected app type:
//   wxpay: 1 扫码(/api/wxpay/native) 2 H5(/api/wxpay/jump_h5) 3 公众号(/api/wxpay/jsapi_convenient)
//   alipay: 1 扫码(/api/alipay/native) 2 H5(/api/alipay/h5)
// Callbacks are POST with a named-subset sign; ack is "SUCCESS"/"FAIL". Refund
// posts to /api/{wxpay,alipay}/refund_order.
//
// Scope: payments + refund. No query is wired by the original plugin.

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "ltzf",
		Title:  "蓝兔支付",
		Author: "蓝兔支付",
		Link:   "https://www.ltzf.cn/",
		Types:  []string{"alipay", "wxpay"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "商户号", Required: true},
			{Key: "appkey", Label: "商户密钥", Secret: true, Required: true},
		},
		// Union of select_wxpay (1,2,3) and select_alipay (1,2).
		AppTypes: []PluginOption{
			{Value: "1", Label: "扫码支付"},
			{Value: "2", Label: "H5支付"},
			{Value: "3", Label: "公众号支付"},
		},
		Build: newLtzfPlugin,
	})
}

const ltzfAPIURL = "https://api.ltzf.cn"

type ltzfPlugin struct {
	pluginBase
	appid, key string
	appTypes   map[string]bool
	// apiBase is the gateway origin; always ltzfAPIURL in production, redirected
	// to an httptest server in unit tests.
	apiBase string
}

var ltzfNow = func() int64 { return time.Now().Unix() }

func newLtzfPlugin(c PluginChannel) (Provider, error) {
	appid, key := strings.TrimSpace(c.Config["appid"]), c.Config["appkey"]
	if appid == "" || len(appid) > 64 || strings.ContainsAny(appid, "&=?# ") || key == "" || len(key) > 256 {
		return nil, ErrInvalid
	}
	at := map[string]bool{}
	for _, t := range c.AppTypes {
		at[t] = true
	}
	// alipay has no 公众号(3) product.
	if c.PayType == "alipay" && at["3"] {
		return nil, ErrInvalid
	}
	base, e := newPluginBase(c, pluginAccount(ltzfAPIURL, appid))
	if e != nil {
		return nil, e
	}
	return &ltzfPlugin{pluginBase: base, appid: appid, key: key, appTypes: at, apiBase: ltzfAPIURL}, nil
}

// methods derives GOEPAY methods from the selected app types (default native).
func (p *ltzfPlugin) methods() []string {
	at := p.appTypes
	if len(at) == 0 {
		return []string{"native"}
	}
	var m []string
	if at["1"] {
		m = append(m, "native")
	}
	if at["2"] {
		m = append(m, "h5")
	}
	if p.payType == "wxpay" && at["3"] {
		m = append(m, "jsapi")
	}
	if len(m) == 0 {
		m = []string{"native"}
	}
	return m
}

func (p *ltzfPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

// ltzfSign: UPPERCASE md5 over the ksort'd named subset ("k=v&"...) + "key=<key>".
func ltzfSign(param map[string]string, names []string, key string) string {
	allowed := map[string]bool{}
	for _, n := range names {
		allowed[n] = true
	}
	keys := make([]string, 0, len(names))
	for k, v := range param {
		if allowed[k] && v != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(param[k])
		sb.WriteByte('&')
	}
	sb.WriteString("key=")
	sb.WriteString(key)
	return strings.ToUpper(md5Hex(sb.String()))
}

func (p *ltzfPlugin) ValidateCreate(r CreateRequest) error {
	return validatePluginCreate(r, p.methods())
}

// path returns the create endpoint for the request method and pay type.
func (p *ltzfPlugin) path(method string) (string, bool) {
	switch p.payType {
	case "alipay":
		switch method {
		case "native":
			return "/api/alipay/native", true
		case "h5":
			return "/api/alipay/h5", true
		}
	case "wxpay":
		switch method {
		case "native":
			return "/api/wxpay/native", true
		case "h5":
			return "/api/wxpay/jump_h5", true
		case "jsapi":
			return "/api/wxpay/jsapi_convenient", true
		}
	}
	return "", false
}

func (p *ltzfPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	path, ok := p.path(r.Method)
	if !ok {
		return CreateResult{}, ErrInvalid
	}
	param := map[string]string{
		"mch_id":       p.appid,
		"out_trade_no": r.TradeNo,
		"total_fee":    yuan(r.Amount),
		"body":         r.Subject,
		"timestamp":    strconv.FormatInt(ltzfNow(), 10),
		"notify_url":   r.NotifyURL,
		"return_url":   r.ReturnURL,
	}
	param["sign"] = ltzfSign(param, []string{"mch_id", "out_trade_no", "total_fee", "body", "timestamp", "notify_url"}, p.key)
	form := url.Values{}
	for k, v := range param {
		form.Set(k, v)
	}
	raw, e := p.postForm(ctx, "ltzf.create", p.apiBase+path, form)
	if e != nil {
		return CreateResult{}, e
	}
	out, e := callbackJSON(raw)
	if e != nil {
		return CreateResult{}, &UnknownOutcome{Operation: "ltzf.create", Cause: e}
	}
	if jsonText(out, "code") != "0" {
		return CreateResult{}, rejected("LTZF_" + jsonText(out, "code"))
	}
	data, _ := out["data"].(map[string]any)
	// jump_h5 returns the H5 URL directly in data (a string).
	switch r.Method {
	case "h5":
		if p.payType == "wxpay" {
			if u := jsonText(out, "data"); validPaymentURL(u) {
				return CreateResult{MaterialType: "jump", Material: u}, nil
			}
		}
		if data != nil {
			if u := jsonText(data, "h5_url"); validPaymentURL(u) {
				return CreateResult{MaterialType: "jump", Material: u}, nil
			}
		}
		return CreateResult{}, rejected("LTZF_NO_URL")
	case "jsapi":
		if data != nil {
			if u := jsonText(data, "order_url"); validPaymentURL(u) {
				return CreateResult{MaterialType: "jump", Material: u}, nil
			}
		}
		return CreateResult{}, rejected("LTZF_NO_URL")
	default: // native: code_url QR content
		if data != nil {
			if u := jsonText(data, "code_url"); u != "" && len(u) <= 4096 && !strings.ContainsAny(u, "\x00\r\n") {
				return CreateResult{MaterialType: "qrcode", Material: u}, nil
			}
		}
		return CreateResult{}, rejected("LTZF_NO_QRCODE")
	}
}

func (p *ltzfPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	params, e := callbackParams(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	sign := ltzfSign(params, []string{"code", "timestamp", "mch_id", "order_no", "out_trade_no", "pay_no", "total_fee"}, p.key)
	if !signEqual(sign, params["sign"], false) {
		return PaymentEvent{}, ErrAuthentication
	}
	if params["code"] != "0" {
		return PaymentEvent{}, ErrInvalid
	}
	amount, e := parseYuan(params["total_fee"])
	if e != nil || expected.Amount <= 0 || amount != expected.Amount || params["out_trade_no"] != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, params["order_no"], params["openid"], amount), nil
}

// The original echoes uppercase "SUCCESS"/"FAIL".
func (p *ltzfPlugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return http.StatusBadRequest, "text/plain; charset=utf-8", "FAIL"
	}
	return http.StatusOK, "text/plain; charset=utf-8", "SUCCESS"
}

func (p *ltzfPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	path := "/api/alipay/refund_order"
	if p.payType == "wxpay" {
		path = "/api/wxpay/refund_order"
	}
	param := map[string]string{
		"mch_id":        p.appid,
		"out_trade_no":  r.TradeNo,
		"out_refund_no": r.RefundNo,
		"timestamp":     strconv.FormatInt(ltzfNow(), 10),
		"refund_fee":    yuan(r.Amount),
	}
	param["sign"] = ltzfSign(param, []string{"mch_id", "out_trade_no", "out_refund_no", "timestamp", "refund_fee"}, p.key)
	form := url.Values{}
	for k, v := range param {
		form.Set(k, v)
	}
	raw, e := p.postForm(ctx, "ltzf.refund", p.apiBase+path, form)
	if e != nil {
		return RefundResult{}, e
	}
	out, e := callbackJSON(raw)
	if e != nil {
		return RefundResult{}, &UnknownOutcome{Operation: "ltzf.refund", Cause: e}
	}
	if jsonText(out, "code") != "0" {
		return RefundResult{}, rejected("LTZF_" + jsonText(out, "code"))
	}
	return RefundResult{RefundNo: r.RefundNo, State: "SUCCESS", Amount: r.Amount}, nil
}

var _ Provider = (*ltzfPlugin)(nil)
var _ NotificationVerifier = (*ltzfPlugin)(nil)
var _ CreateValidator = (*ltzfPlugin)(nil)
