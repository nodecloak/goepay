package payment

// Port of epay_release/plugins/epay (彩虹易支付): relays the order to another
// 彩虹易支付 site. Page mode posts a signed form to <appurl>submit.php; mapi
// mode calls <appurl>mapi.php and shows the returned link/QR. Callbacks are
// GET with an MD5 signature over the sorted parameters plus the key.
// Refund and query use <appurl>api.php (act=refund / act=order).

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "epay",
		Title:  "彩虹易支付",
		Author: "彩虹",
		Types:  []string{"alipay", "qqpay", "wxpay", "bank", "jdpay", "douyinpay"},
		Inputs: []PluginInput{
			{Key: "appurl", Label: "接口地址", Note: "必须以http://或https://开头，以/结尾", Required: true},
			{Key: "appid", Label: "商户ID", Required: true},
			{Key: "appkey", Label: "商户密钥", Secret: true, Required: true},
			{Key: "appswitch", Label: "是否使用mapi接口", Options: []PluginOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}}},
		},
		Build: newEpayPlugin,
	})
}

type epayPlugin struct {
	pluginBase
	apiURL, pid, key string
	mapi             bool
}

func newEpayPlugin(c PluginChannel) (Provider, error) {
	api := strings.TrimSpace(c.Config["appurl"])
	host, ok := gatewayHost(api)
	if !ok || !strings.HasSuffix(api, "/") || strings.ContainsAny(api, "?#") {
		return nil, ErrInvalid
	}
	pid, key := strings.TrimSpace(c.Config["appid"]), c.Config["appkey"]
	if pid == "" || len(pid) > 32 || strings.ContainsAny(pid, "&=?# ") || key == "" || len(key) > 256 {
		return nil, ErrInvalid
	}
	base, e := newPluginBase(c, pluginAccount(host, pid))
	if e != nil {
		return nil, e
	}
	return &epayPlugin{pluginBase: base, apiURL: api, pid: pid, key: key, mapi: c.Config["appswitch"] == "1"}, nil
}

func (p *epayPlugin) methods() []string {
	if p.payType == "alipay" {
		if p.mapi {
			return []string{"page", "wap", "native"}
		}
		return []string{"page", "wap"}
	}
	if p.mapi {
		return []string{"native", "h5"}
	}
	return []string{"h5"}
}

func (p *epayPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Query: true, Refund: true}
}

func (p *epayPlugin) sign(params map[string]string) string {
	return md5Hex(epaySignString(params) + p.key)
}

func (p *epayPlugin) ValidateCreate(r CreateRequest) error {
	return validatePluginCreate(r, p.methods())
}

func (p *epayPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	params := map[string]string{
		"pid":          p.pid,
		"type":         p.payType,
		"notify_url":   r.NotifyURL,
		"return_url":   r.ReturnURL,
		"out_trade_no": r.TradeNo,
		"name":         r.Subject,
		"money":        yuan(r.Amount),
	}
	if !p.mapi {
		params["sign"] = p.sign(params)
		params["sign_type"] = "MD5"
		return CreateResult{MaterialType: "html", Material: pluginForm(p.apiURL+"submit.php", params, "正在跳转")}, nil
	}
	// Original getDevice(): the upstream picks the product by device.
	device := "pc"
	if r.Method == "h5" || r.Method == "wap" {
		device = "mobile"
	}
	params["device"] = device
	params["clientip"] = r.ClientIP
	params["sign"] = p.sign(params)
	params["sign_type"] = "MD5"
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	raw, e := p.postForm(ctx, "epay.mapi", p.apiURL+"mapi.php", form)
	if e != nil {
		return CreateResult{}, e
	}
	out, e := callbackJSON(raw)
	if e != nil {
		// The upstream may have created the order; the reply is unreadable.
		return CreateResult{}, &UnknownOutcome{Operation: "epay.mapi", Cause: e}
	}
	if jsonText(out, "code") != "1" {
		return CreateResult{}, rejected("EPAY_" + jsonText(out, "code"))
	}
	result, ok := materialFromLinks(jsonText(out, "payurl"), jsonText(out, "qrcode"), jsonText(out, "urlscheme"))
	if !ok {
		return CreateResult{}, rejected("EPAY_NO_PAY_URL")
	}
	result.ProviderTradeNo = jsonText(out, "trade_no")
	return result, nil
}

// Callbacks are GET (the original reads $_GET); POST forms are accepted too.
func (p *epayPlugin) NotificationMethods() []string {
	return []string{http.MethodGet, http.MethodPost}
}

func (p *epayPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	params, e := callbackParams(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	if !signEqual(p.sign(params), params["sign"], false) {
		return PaymentEvent{}, ErrAuthentication
	}
	if params["pid"] != "" && params["pid"] != p.pid {
		return PaymentEvent{}, ErrAuthentication
	}
	if params["trade_status"] != "TRADE_SUCCESS" {
		return PaymentEvent{}, ErrInvalid
	}
	amount, e := parseYuan(params["money"])
	if e != nil || expected.Amount <= 0 || amount != expected.Amount || params["out_trade_no"] != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, params["trade_no"], params["buyer"], amount), nil
}

// Query reads the order through the original SDK's queryOrder (act=order),
// keyed by our trade number (the upstream's out_trade_no).
func (p *epayPlugin) Query(ctx context.Context, r QueryRequest) (QueryResult, error) {
	if r.TradeNo == "" {
		return QueryResult{}, ErrInvalid
	}
	q := url.Values{"act": {"order"}, "pid": {p.pid}, "key": {p.key}, "out_trade_no": {r.TradeNo}}
	raw, e := p.getURL(ctx, "epay.order", p.apiURL+"api.php?"+q.Encode())
	if e != nil {
		return QueryResult{}, e
	}
	out, e := callbackJSON(raw)
	if e != nil {
		return QueryResult{}, ErrAuthentication
	}
	if jsonText(out, "code") != "1" {
		return QueryResult{}, rejected("EPAY_" + jsonText(out, "code"))
	}
	if jsonText(out, "out_trade_no") != r.TradeNo || jsonText(out, "pid") != "" && jsonText(out, "pid") != p.pid {
		return QueryResult{}, ErrAuthentication
	}
	amount, e := parseYuan(jsonText(out, "money"))
	if e != nil {
		return QueryResult{}, ErrAuthentication
	}
	// status: 0 unpaid, 1 paid, 2 refunded, 3 frozen (all but 0 were paid).
	state := "NOTPAY"
	switch jsonText(out, "status") {
	case "1", "3":
		state = "SUCCESS"
	case "2":
		state = "REFUND"
	}
	return QueryResult{TradeNo: r.TradeNo, ProviderTradeNo: jsonText(out, "trade_no"), State: state, Currency: "CNY", BuyerID: jsonText(out, "buyer"), Amount: amount}, nil
}

// Refund posts act=refund with the upstream trade number; the original treats
// code 0 as success.
func (p *epayPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.ProviderTradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	form := url.Values{"pid": {p.pid}, "key": {p.key}, "refund_no": {r.RefundNo}, "trade_no": {r.ProviderTradeNo}, "money": {yuan(r.Amount)}}
	raw, e := p.postForm(ctx, "epay.refund", p.apiURL+"api.php?act=refund", form)
	if e != nil {
		return RefundResult{}, e
	}
	out, e := callbackJSON(raw)
	if e != nil {
		return RefundResult{}, &UnknownOutcome{Operation: "epay.refund", Cause: e}
	}
	if jsonText(out, "code") != "0" {
		return RefundResult{}, rejected("EPAY_" + jsonText(out, "code"))
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(out, "refund_no"), State: "SUCCESS", Amount: r.Amount}, nil
}

var _ Provider = (*epayPlugin)(nil)
var _ NotificationVerifier = (*epayPlugin)(nil)
var _ CreateValidator = (*epayPlugin)(nil)
