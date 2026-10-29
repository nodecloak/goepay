package payment

// Port of epay_release/plugins/passpay (精秀支付). RSA2 (SHA256, PKCS#1 v1.5,
// base64) signed JSON-over-form API (PasspayClient). Each request wraps a
// biz_content JSON plus mchid/method/charset/sign_type/timestamp/version, signs
// the ksort'd "k=v&" base with the merchant private key, and verifies replies
// (code==1 -> data) and callbacks with the platform public key. The product is
// chosen by a trade_type derived from pay type + selected app type. Callbacks
// are POST with order_status "SUCCESS"; ack is "success". Refund calls
// pay.order/refund.
//
// Scope: payments + refund. Query is not wired by the original plugin. The
// WeChat 公众号/小程序 JSAPI flows that need an upstream openid are mapped to the
// "jsapi" method (material "jsapi"); the oauth/openid acquisition the original
// does server-side is the platform's responsibility (r.OpenID).

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
		Name:   "passpay",
		Title:  "精秀支付",
		Author: "精秀",
		Link:   "https://www.jxpays.com/",
		Types:  []string{"alipay", "wxpay", "qqpay", "bank"},
		Inputs: []PluginInput{
			{Key: "appurl", Label: "API接口地址", Note: "以http://或https://开头，以/结尾", Required: true},
			{Key: "appid", Label: "商户编号", Required: true},
			{Key: "appkey", Label: "商户私钥", Secret: true, Textarea: true, Required: true},
			{Key: "appsecret", Label: "平台公钥", Textarea: true, Required: true},
			{Key: "appmchid", Label: "通道ID", Note: "不填写将进行子商户号轮训"},
		},
		// Union of select_alipay (1-4) and select_wxpay (1-4).
		AppTypes: []PluginOption{
			{Value: "1", Label: "扫码/当面付"},
			{Value: "2", Label: "电脑/公众号"},
			{Value: "3", Label: "H5"},
			{Value: "4", Label: "生活号/小程序"},
		},
		Build: newPasspayPlugin,
	})
}

type passpayPlugin struct {
	pluginBase
	apiURL, mchid, channelID string
	priv                     *rsa.PrivateKey
	pub                      *rsa.PublicKey
	appTypes                 map[string]bool
}

var passpayNow = func() int64 { return time.Now().Unix() }

func newPasspayPlugin(c PluginChannel) (Provider, error) {
	api := strings.TrimSpace(c.Config["appurl"])
	host, ok := gatewayHost(api)
	if !ok || !strings.HasSuffix(api, "/") || strings.ContainsAny(api, "?#") {
		return nil, ErrInvalid
	}
	mchid := strings.TrimSpace(c.Config["appid"])
	if mchid == "" || len(mchid) > 64 || strings.ContainsAny(mchid, "&=?# ") {
		return nil, ErrInvalid
	}
	priv, e := pluginPrivateKey(c.Config["appkey"])
	if e != nil {
		return nil, ErrInvalid
	}
	pub, e := pluginPublicKey(c.Config["appsecret"])
	if e != nil {
		return nil, ErrInvalid
	}
	at := map[string]bool{}
	for _, t := range c.AppTypes {
		at[t] = true
	}
	base, e := newPluginBase(c, pluginAccount(host, mchid))
	if e != nil {
		return nil, e
	}
	return &passpayPlugin{pluginBase: base, apiURL: api, mchid: mchid, channelID: strings.TrimSpace(c.Config["appmchid"]), priv: priv, pub: pub, appTypes: at}, nil
}

// methods derives GOEPAY methods from pay type + selected app types.
func (p *passpayPlugin) methods() []string {
	at := p.appTypes
	all := len(at) == 0
	var m []string
	add := func(x string) {
		for _, e := range m {
			if e == x {
				return
			}
		}
		m = append(m, x)
	}
	switch p.payType {
	case "alipay":
		if all || at["2"] {
			add("page")
		}
		if all || at["3"] {
			add("wap")
		}
		if all || at["4"] {
			add("jsapi")
		}
		if all || at["1"] {
			add("native")
		}
	case "wxpay":
		if all || at["1"] {
			add("native")
		}
		if all || at["2"] || at["4"] {
			add("jsapi")
		}
		if all || at["3"] {
			add("h5")
		}
	case "qqpay", "bank":
		add("native")
	}
	if len(m) == 0 {
		m = []string{"native"}
	}
	return m
}

func (p *passpayPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

// tradeType maps pay type + method to the upstream trade_type.
func (p *passpayPlugin) tradeType(method string) (string, bool) {
	switch p.payType {
	case "alipay":
		switch method {
		case "native":
			return "alipayQr", true
		case "page":
			return "alipayPc", true
		case "wap":
			return "alipayWap", true
		case "jsapi":
			return "alipayPub", true
		}
	case "wxpay":
		switch method {
		case "native":
			return "wechatQr", true
		case "jsapi":
			return "wechatPub", true
		case "h5":
			return "wechatWap", true
		}
	case "qqpay":
		if method == "native" {
			return "qqQr", true
		}
	case "bank":
		if method == "native" {
			return "unionQr", true
		}
	}
	return "", false
}

// signContent is PasspayClient::getSignContent: ksort, skip sign/empty, "k=v&"
// joined by "&" (epaySignString produces exactly this).
func (p *passpayPlugin) signContent(params map[string]string) string {
	return epaySignString(params)
}

// execute builds the signed envelope, posts it and verifies the reply.
func (p *passpayPlugin) execute(ctx context.Context, method string, biz map[string]string) (map[string]any, error) {
	bizJSON, e := json.Marshal(biz)
	if e != nil {
		return nil, ErrInvalid
	}
	params := map[string]string{
		"mchid":       p.mchid,
		"method":      method,
		"charset":     "utf-8",
		"sign_type":   "RSA2",
		"timestamp":   strconv.FormatInt(passpayNow(), 10),
		"version":     "1.0",
		"biz_content": string(bizJSON),
	}
	sig, e := rsaSign(p.priv, crypto.SHA256, p.signContent(params))
	if e != nil {
		return nil, e
	}
	params["sign"] = sig
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	raw, e := p.postForm(ctx, "passpay."+method, p.apiURL+method, form)
	if e != nil {
		return nil, e
	}
	out, e := callbackJSON(raw)
	if e != nil {
		return nil, &UnknownOutcome{Operation: "passpay." + method, Cause: e}
	}
	if jsonText(out, "code") != "1" {
		return nil, rejected("PASSPAY_" + jsonText(out, "code"))
	}
	data, ok := out["data"].(map[string]any)
	if !ok {
		return nil, &UnknownOutcome{Operation: "passpay." + method, Cause: ErrInvalid}
	}
	return data, nil
}

func (p *passpayPlugin) ValidateCreate(r CreateRequest) error {
	if e := validatePluginCreate(r, p.methods()); e != nil {
		return e
	}
	return nil
}

func (p *passpayPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	tradeType, ok := p.tradeType(r.Method)
	if !ok {
		return CreateResult{}, ErrInvalid
	}
	biz := map[string]string{
		"trade_type":     tradeType,
		"pay_channel_id": p.channelID,
		"out_trade_no":   r.TradeNo,
		"total_amount":   yuan(r.Amount),
		"subject":        r.Subject,
		"notify_url":     r.NotifyURL,
		"return_url":     r.ReturnURL,
		"client_ip":      r.ClientIP,
	}
	if r.Method == "jsapi" {
		if r.OpenID == "" {
			return CreateResult{}, ErrInvalid
		}
		biz["user_id"] = r.OpenID
	}
	data, e := p.execute(ctx, "pay.order/create", biz)
	if e != nil {
		return CreateResult{}, e
	}
	providerTradeNo := jsonText(data, "trade_no")
	if r.Method == "jsapi" {
		info := jsonText(data, "payInfo")
		if info == "" || len(info) > 4096 || strings.ContainsAny(info, "\x00\r\n") {
			return CreateResult{}, rejected("PASSPAY_NO_PAYINFO")
		}
		return CreateResult{MaterialType: "jsapi", Material: info, ProviderTradeNo: providerTradeNo}, nil
	}
	payURL := jsonText(data, "payurl")
	switch r.Method {
	case "native":
		if payURL == "" || len(payURL) > 4096 || strings.ContainsAny(payURL, "\x00\r\n") {
			return CreateResult{}, rejected("PASSPAY_NO_PAYURL")
		}
		return CreateResult{MaterialType: "qrcode", Material: payURL, ProviderTradeNo: providerTradeNo}, nil
	default: // page, wap, h5
		if !validPaymentURL(payURL) {
			return CreateResult{}, rejected("PASSPAY_NO_PAYURL")
		}
		return CreateResult{MaterialType: "jump", Material: payURL, ProviderTradeNo: providerTradeNo}, nil
	}
}

func (p *passpayPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	params, e := callbackParams(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	if params["sign"] == "" || !rsaVerify(p.pub, crypto.SHA256, p.signContent(params), params["sign"]) {
		return PaymentEvent{}, ErrAuthentication
	}
	if params["order_status"] != "SUCCESS" {
		return PaymentEvent{}, ErrInvalid
	}
	amount, e := parseYuan(params["total_amount"])
	if e != nil || expected.Amount <= 0 || amount != expected.Amount || params["out_trade_no"] != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, params["trade_no"], "", amount), nil
}

func (p *passpayPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.ProviderTradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	biz := map[string]string{
		"refund_amount": yuan(r.Amount),
		"refund_reason": "订单退款",
		"out_refund_no": r.RefundNo,
		"trade_no":      r.ProviderTradeNo,
	}
	data, e := p.execute(ctx, "pay.order/refund", biz)
	if e != nil {
		return RefundResult{}, e
	}
	amount := r.Amount
	if a, e := parseYuan(jsonText(data, "refund_amount")); e == nil {
		amount = a
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(data, "trade_no"), State: "SUCCESS", Amount: amount}, nil
}

var _ Provider = (*passpayPlugin)(nil)
var _ NotificationVerifier = (*passpayPlugin)(nil)
var _ CreateValidator = (*passpayPlugin)(nil)
