package payment

// Port of epay_release/plugins/unionpay (银联前置, Swiftpass 威富通 gateway).
// All products post a WeChat-style <xml> document to the gateway
// (default https://qra.95516.com/pay/gateway) signed MD5 uppercase over the
// ksort-ed non-empty params plus "&key=<appkey>". The create reply and the
// async callback carry the same signature. Native下单 (unified.trade.native)
// returns a code_url for a QR; service-window / JSAPI / H5 products need a
// buyer identity in r.OpenID. Refund uses unified.trade.refund.
//
// select_type is the original per-pay-type product switch (select_alipay /
// select_wxpay). We fold them into one AppTypes list: "scan" (native, every
// type), "jsapi" (服务窗/公众号, needs openid) and "h5" (wxpay H5 jump). When
// AppTypes is empty we behave like the original default (native only).

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "unionpay",
		Title:  "银联前置",
		Author: "银联",
		Link:   "http://www.95516.com/",
		Types:  []string{"alipay", "wxpay", "qqpay", "bank", "jdpay"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "商户号", Required: true},
			{Key: "appkey", Label: "商户密钥", Secret: true, Required: true},
			{Key: "appurl", Label: "自定义网关URL", Note: "可不填,默认是https://qra.95516.com/pay/gateway"},
		},
		AppTypes: []PluginOption{
			{Value: "scan", Label: "扫码支付"},
			{Value: "jsapi", Label: "服务窗/公众号支付"},
			{Value: "h5", Label: "H5支付"},
		},
		Build: newUnionpayPlugin,
	})
}

const unionpayDefaultGateway = "https://qra.95516.com/pay/gateway"

type unionpayPlugin struct {
	pluginBase
	gateway, mchID, key string
	appTypes            map[string]bool
}

func newUnionpayPlugin(c PluginChannel) (Provider, error) {
	gw := strings.TrimSpace(c.Config["appurl"])
	if gw == "" {
		gw = unionpayDefaultGateway
	}
	host, ok := gatewayHost(gw)
	if !ok {
		return nil, ErrInvalid
	}
	mchID, key := strings.TrimSpace(c.Config["appid"]), c.Config["appkey"]
	if mchID == "" || len(mchID) > 32 || strings.ContainsAny(mchID, "&=?# ") || key == "" || len(key) > 256 {
		return nil, ErrInvalid
	}
	appTypes := map[string]bool{}
	for _, t := range c.AppTypes {
		appTypes[t] = true
	}
	base, e := newPluginBase(c, pluginAccount(host, mchID))
	if e != nil {
		return nil, e
	}
	return &unionpayPlugin{pluginBase: base, gateway: gw, mchID: mchID, key: key, appTypes: appTypes}, nil
}

// has reports whether an app type is enabled; empty config means native only.
func (p *unionpayPlugin) has(t string) bool {
	if len(p.appTypes) == 0 {
		return t == "scan"
	}
	return p.appTypes[t]
}

func (p *unionpayPlugin) methods() []string {
	var m []string
	if p.has("scan") || len(p.appTypes) == 0 {
		m = append(m, "native")
	}
	// JSAPI (服务窗/公众号) only for types the original offered it on.
	if p.has("jsapi") && (p.payType == "alipay" || p.payType == "wxpay" || p.payType == "bank") {
		m = append(m, "jsapi")
	}
	if p.has("h5") && p.payType == "wxpay" {
		m = append(m, "h5")
	}
	if len(m) == 0 {
		m = []string{"native"}
	}
	return m
}

func (p *unionpayPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

func (p *unionpayPlugin) sign(params map[string]string) string {
	return strings.ToUpper(md5Hex(unionpaySignBase(params) + "&key=" + p.key))
}

// publicParams mirrors SwiftpassClient::requestApi's injected fields. nonce_str
// is random; the signature covers the sent document so the value is free.
func (p *unionpayPlugin) publicParams() map[string]string {
	return map[string]string{
		"mch_id":    p.mchID,
		"version":   "2.0",
		"sign_type": "MD5",
		"nonce_str": unionpayNonce(),
	}
}

func (p *unionpayPlugin) ValidateCreate(r CreateRequest) error {
	if e := validatePluginCreate(r, p.methods()); e != nil {
		return e
	}
	if r.Method == "jsapi" && strings.TrimSpace(r.OpenID) == "" {
		return ErrInvalid
	}
	return nil
}

func (p *unionpayPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	params := p.publicParams()
	params["out_trade_no"] = r.TradeNo
	params["body"] = r.Subject
	params["total_fee"] = unionpayFen(r.Amount)
	params["mch_create_ip"] = r.ClientIP
	params["notify_url"] = r.NotifyURL

	switch r.Method {
	case "native":
		params["service"] = "unified.trade.native"
	case "h5": // wxpay only
		params["service"] = "pay.weixin.wappay"
		params["device_info"] = "AND_WAP"
		params["callback_url"] = r.ReturnURL
	case "jsapi":
		switch p.payType {
		case "alipay":
			params["service"] = "pay.alipay.jspay"
			params["buyer_id"] = r.OpenID
		case "wxpay":
			params["service"] = "pay.weixin.jspay"
			params["is_raw"] = "1"
			params["sub_openid"] = r.OpenID
			params["device_info"] = "AND_WAP"
		case "bank":
			params["service"] = "pay.unionpay.jspay"
			params["user_id"] = r.OpenID
		default:
			return CreateResult{}, ErrInvalid
		}
	default:
		return CreateResult{}, ErrInvalid
	}
	params["sign"] = p.sign(params)

	raw, e := p.pluginCall(ctx, "unionpay.create", http.MethodPost, p.gateway, []byte(unionpayXML(params)), "application/xml; charset=utf-8", nil)
	if e != nil {
		return CreateResult{}, e
	}
	out, e := xmlFlat(raw)
	if e != nil {
		return CreateResult{}, &UnknownOutcome{Operation: "unionpay.create", Cause: e}
	}
	if out["status"] != "0" {
		return CreateResult{}, rejected("UNIONPAY_" + unionpayCode(out["status"], out["message"]))
	}
	if !p.verify(out) {
		return CreateResult{}, &UnknownOutcome{Operation: "unionpay.create", Cause: errUnionpaySign}
	}
	if out["result_code"] != "0" {
		return CreateResult{}, rejected("UNIONPAY_" + unionpayCode(out["err_code"], out["err_msg"]))
	}
	return p.material(r.Method, out)
}

func (p *unionpayPlugin) material(method string, out map[string]string) (CreateResult, error) {
	providerTradeNo := out["transaction_id"]
	switch method {
	case "native":
		code := out["code_url"]
		// QQ wallet code rewrite the original performs for myun.tenpay.com.
		if strings.Contains(code, "myun.tenpay.com") {
			if i := strings.Index(code, "&t="); i >= 0 {
				code = "https://qpay.qq.com/qr/" + code[i+3:]
			}
		}
		if code == "" || len(code) > 4096 || strings.ContainsAny(code, "\x00\r\n") {
			return CreateResult{}, rejected("UNIONPAY_NO_CODE_URL")
		}
		return CreateResult{MaterialType: "qrcode", Material: code, ProviderTradeNo: providerTradeNo}, nil
	case "h5":
		if res, ok := materialFromLinks(out["pay_info"], "", ""); ok {
			res.ProviderTradeNo = providerTradeNo
			return res, nil
		}
		return CreateResult{}, rejected("UNIONPAY_NO_PAY_INFO")
	case "jsapi":
		info := out["pay_info"]
		if info == "" || len(info) > 8192 || strings.ContainsRune(info, 0) {
			return CreateResult{}, rejected("UNIONPAY_NO_PAY_INFO")
		}
		return CreateResult{MaterialType: "jsapi", Material: info, ProviderTradeNo: providerTradeNo}, nil
	}
	return CreateResult{}, ErrInvalid
}

func (p *unionpayPlugin) verify(out map[string]string) bool {
	return signEqual(p.sign(out), out["sign"], true)
}

func (p *unionpayPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	out, e := xmlFlat(raw)
	if e != nil {
		return PaymentEvent{}, ErrInvalid
	}
	if !p.verify(out) {
		return PaymentEvent{}, ErrAuthentication
	}
	if out["status"] != "0" || out["result_code"] != "0" {
		return PaymentEvent{}, ErrInvalid
	}
	amount, e := parseFen(out["total_fee"])
	if e != nil || expected.Amount <= 0 || amount != expected.Amount || out["out_trade_no"] != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, out["transaction_id"], out["openid"], amount), nil
}

func (p *unionpayPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.ProviderTradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	params := p.publicParams()
	params["service"] = "unified.trade.refund"
	params["transaction_id"] = r.ProviderTradeNo
	params["out_refund_no"] = r.RefundNo
	params["total_fee"] = unionpayFen(r.TotalAmount)
	params["refund_fee"] = unionpayFen(r.Amount)
	params["op_user_id"] = p.mchID
	params["sign"] = p.sign(params)

	raw, e := p.pluginCall(ctx, "unionpay.refund", http.MethodPost, p.gateway, []byte(unionpayXML(params)), "application/xml; charset=utf-8", nil)
	if e != nil {
		return RefundResult{}, e
	}
	out, e := xmlFlat(raw)
	if e != nil {
		return RefundResult{}, &UnknownOutcome{Operation: "unionpay.refund", Cause: e}
	}
	if out["status"] != "0" {
		return RefundResult{}, rejected("UNIONPAY_" + unionpayCode(out["status"], out["message"]))
	}
	if !p.verify(out) {
		return RefundResult{}, &UnknownOutcome{Operation: "unionpay.refund", Cause: errUnionpaySign}
	}
	if out["result_code"] != "0" {
		return RefundResult{}, rejected("UNIONPAY_" + unionpayCode(out["err_code"], out["err_msg"]))
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: out["refund_id"], State: "SUCCESS", Amount: r.Amount}, nil
}

// The original callback echoes plain "success" / "failure".
func (p *unionpayPlugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return http.StatusOK, "text/plain; charset=utf-8", "failure"
	}
	return http.StatusOK, "text/plain; charset=utf-8", "success"
}

// ---- local helpers (unionpay-scoped) ----

var errUnionpaySign = unionpaySignError{}

type unionpaySignError struct{}

func (unionpaySignError) Error() string { return "unionpay: reply signature mismatch" }

func unionpayFen(cents int64) string { return strconv.FormatInt(cents, 10) }

// unionpaySignBase mirrors SwiftpassClient::getSignContent: ksort, join
// "k=v&" over non-empty params EXCEPT "sign" (sign_type IS included), trimmed.
func unionpaySignBase(params map[string]string) string {
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

// unionpayCode picks a short upstream code, never free text.
func unionpayCode(code, _ string) string {
	code = strings.TrimSpace(code)
	if code == "" || len(code) > 48 || strings.ContainsAny(code, " \r\n") {
		return "REJECTED"
	}
	return code
}

// unionpayXML renders a WeChat-style flat <xml> document; numeric values are
// emitted bare, the rest wrapped in CDATA, exactly as array2Xml does. Keys are
// sorted so the output is deterministic (the order never affects the MD5 sign,
// which re-sorts).
func unionpayXML(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("<xml>")
	for _, k := range keys {
		v := params[k]
		if unionpayNumeric(v) {
			b.WriteString("<" + k + ">" + v + "</" + k + ">")
		} else {
			b.WriteString("<" + k + "><![CDATA[" + unionpayCDATA(v) + "]]></" + k + ">")
		}
	}
	b.WriteString("</xml>")
	return b.String()
}

// unionpayNumeric mirrors PHP is_numeric for the plain integer/decimal values
// the gateway request carries.
func unionpayNumeric(s string) bool {
	if s == "" {
		return false
	}
	dot := false
	for i, c := range s {
		if c == '-' && i == 0 {
			continue
		}
		if c == '.' && !dot {
			dot = true
			continue
		}
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != "-" && s != "."
}

// unionpayCDATA defends against a "]]>" sequence breaking out of the section.
func unionpayCDATA(v string) string { return strings.ReplaceAll(v, "]]>", "]]]]><![CDATA[>") }

func unionpayNonce() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return "0000000000000000"
	}
	return hex.EncodeToString(b)
}
