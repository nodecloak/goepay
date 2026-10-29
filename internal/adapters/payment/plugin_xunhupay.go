package payment

// Port of epay_release/plugins/xunhupay (虎皮椒支付). JSON POST to
// <gateway>/payment/do.html with an MD5 hash over the ksort'd "k=v&" base plus
// the API key (XunhupayClient::generate_hash). Reply carries url (mobile jump)
// and url_qrcode (a QR image URL whose data= query holds the real pay URL).
// Callbacks are POST form with the same hash; ack is plain text. Refund posts
// open_order_id to /payment/refund.html.
//
// Scope: payments + refund. No query is wired by the original plugin.

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "xunhupay",
		Title:  "虎皮椒支付",
		Author: "虎皮椒",
		Link:   "https://www.xunhupay.com/",
		Types:  []string{"alipay", "wxpay"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "商户ID", Required: true},
			{Key: "appkey", Label: "API密钥", Secret: true, Required: true},
			{Key: "appurl", Label: "网关地址", Note: "不填写默认为https://api.xunhupay.com/payment/do.html"},
		},
		Build: newXunhupayPlugin,
	})
}

type xunhupayPlugin struct {
	pluginBase
	apiURL, appid, key string
}

// xunhupayNonce / xunhupayNow are overridable in tests.
var xunhupayNow = func() int64 { return time.Now().Unix() }
var xunhupayNonce = func() string { return strconv.FormatInt(time.Now().UnixNano(), 10) }

const xunhupayDefaultURL = "https://api.xunhupay.com/payment/do.html"

func newXunhupayPlugin(c PluginChannel) (Provider, error) {
	api := strings.TrimSpace(c.Config["appurl"])
	if api == "" {
		api = xunhupayDefaultURL
	}
	host, ok := gatewayHost(api)
	if !ok || !strings.HasSuffix(api, "/payment/do.html") {
		return nil, ErrInvalid
	}
	appid, key := strings.TrimSpace(c.Config["appid"]), c.Config["appkey"]
	if appid == "" || len(appid) > 64 || strings.ContainsAny(appid, "&=?# ") || key == "" || len(key) > 256 {
		return nil, ErrInvalid
	}
	base, e := newPluginBase(c, pluginAccount(host, appid))
	if e != nil {
		return nil, e
	}
	return &xunhupayPlugin{pluginBase: base, apiURL: api, appid: appid, key: key}, nil
}

func (p *xunhupayPlugin) methods() []string { return []string{"native", "h5"} }

func (p *xunhupayPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

// sign mirrors generate_hash: ksort, skip "hash"/empty, "k=v&" then md5(base+key).
// The xunhupay params never use "sign"/"sign_type" keys, so epaySignString's
// default skips are harmless; "hash" is skipped explicitly.
func (p *xunhupayPlugin) sign(params map[string]string) string {
	return md5Hex(epaySignString(params, "hash") + p.key)
}

func (p *xunhupayPlugin) ValidateCreate(r CreateRequest) error {
	return validatePluginCreate(r, p.methods())
}

func (p *xunhupayPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	payment := "alipay"
	if p.payType == "wxpay" {
		payment = "wechat"
	}
	params := map[string]string{
		"version":        "1.1",
		"appid":          p.appid,
		"trade_order_id": r.TradeNo,
		"payment":        payment,
		"total_fee":      yuan(r.Amount),
		"title":          r.Subject,
		"time":           strconv.FormatInt(xunhupayNow(), 10),
		"nonce_str":      xunhupayNonce(),
		"notify_url":     r.NotifyURL,
		"return_url":     r.ReturnURL,
	}
	if payment == "wechat" && r.Method == "h5" {
		host := ""
		if u, e := url.Parse(r.NotifyURL); e == nil {
			host = u.Host
		}
		params["type"] = "WAP"
		params["wap_url"] = host
		params["wap_name"] = r.Subject
	}
	params["hash"] = p.sign(params)
	raw, e := p.postJSON(ctx, "xunhupay.create", p.apiURL, params, nil)
	if e != nil {
		return CreateResult{}, e
	}
	out, e := callbackJSON(raw)
	if e != nil {
		return CreateResult{}, &UnknownOutcome{Operation: "xunhupay.create", Cause: e}
	}
	if jsonText(out, "errcode") != "0" {
		return CreateResult{}, rejected("XUNHU_" + jsonText(out, "errcode"))
	}
	// Mobile/H5: redirect to the pay URL. QR: the real pay URL lives in the
	// data= query of url_qrcode (XunhupayClient::parseQrcode); fall back to
	// url_qrcode itself when it is already a pay URL.
	if r.Method == "h5" {
		if u := jsonText(out, "url"); validPaymentURL(u) {
			return CreateResult{MaterialType: "jump", Material: u}, nil
		}
		return CreateResult{}, rejected("XUNHU_NO_URL")
	}
	qr := xunhupayQRContent(jsonText(out, "url_qrcode"))
	if qr == "" {
		return CreateResult{}, rejected("XUNHU_NO_QRCODE")
	}
	return CreateResult{MaterialType: "qrcode", Material: qr}, nil
}

// xunhupayQRContent extracts and base64-decodes the data= query parameter of a
// QR image URL (the original parseQrcode); if absent, returns the URL unchanged
// when it is itself a usable pay URL.
func xunhupayQRContent(qrURL string) string {
	qrURL = strings.TrimSpace(qrURL)
	if qrURL == "" {
		return ""
	}
	if u, e := url.Parse(qrURL); e == nil {
		if data := u.Query().Get("data"); data != "" {
			if dec, e := base64.StdEncoding.DecodeString(data); e == nil {
				s := string(dec)
				if len(s) <= 4096 && !strings.ContainsAny(s, "\x00\r\n") {
					return s
				}
			}
		}
	}
	if len(qrURL) <= 4096 && !strings.ContainsAny(qrURL, "\x00\r\n") {
		return qrURL
	}
	return ""
}

func (p *xunhupayPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	params, e := callbackParams(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	if params["hash"] == "" || params["trade_order_id"] == "" {
		return PaymentEvent{}, ErrInvalid
	}
	if !signEqual(p.sign(params), params["hash"], false) {
		return PaymentEvent{}, ErrAuthentication
	}
	if params["status"] != "OD" {
		return PaymentEvent{}, ErrInvalid
	}
	amount, e := parseYuan(params["total_fee"])
	if e != nil || expected.Amount <= 0 || amount != expected.Amount || params["trade_order_id"] != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, params["open_order_id"], "", amount), nil
}

func (p *xunhupayPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.ProviderTradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	refundURL := strings.Replace(p.apiURL, "/payment/do.html", "/payment/refund.html", 1)
	params := map[string]string{
		"appid":         p.appid,
		"time":          strconv.FormatInt(xunhupayNow(), 10),
		"nonce_str":     xunhupayNonce(),
		"open_order_id": r.ProviderTradeNo,
	}
	params["hash"] = p.sign(params)
	raw, e := p.postJSON(ctx, "xunhupay.refund", refundURL, params, nil)
	if e != nil {
		return RefundResult{}, e
	}
	out, e := callbackJSON(raw)
	if e != nil {
		return RefundResult{}, &UnknownOutcome{Operation: "xunhupay.refund", Cause: e}
	}
	if jsonText(out, "errcode") != "0" {
		return RefundResult{}, rejected("XUNHU_" + jsonText(out, "errcode"))
	}
	amount := r.Amount
	if a, e := parseYuan(jsonText(out, "refund_fee")); e == nil {
		amount = a
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(out, "transaction_id"), State: "SUCCESS", Amount: amount}, nil
}

var _ Provider = (*xunhupayPlugin)(nil)
var _ NotificationVerifier = (*xunhupayPlugin)(nil)
var _ CreateValidator = (*xunhupayPlugin)(nil)
