package payment

// Port of epay_release/plugins/swiftpass (威富通RSA): same WeChat-style XML
// gateway as swiftpass2, but signType RSA_1_256 — the request is signed with
// the merchant RSA private key (appsecret) over SHA256 and the reply is
// verified with the platform RSA public key (appkey). Sign base is
// SwiftpassClient::getSignContent (ksort, "k=v&…", skip "sign" and empty
// values). The native/QR services return code_url, shown as a QR; callbacks
// are an XML POST verified with the platform key; the plugin echoes the plain
// strings "success"/"failure". Refund is unified.trade.refund.
//
// Out of scope (per wave brief): JSAPI/公众号/小程序/H5 (need a bound WeChat
// appid and openid from OAuth the platform performs) and order query (the SDK
// has no query service).

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/hex"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "swiftpass",
		Title:  "威富通RSA",
		Author: "威富通",
		Link:   "https://www.swiftpass.cn/",
		Types:  []string{"alipay", "wxpay", "qqpay", "bank", "jdpay"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "商户号", Required: true},
			{Key: "appkey", Label: "平台RSA公钥", PEM: true, Required: true},
			{Key: "appsecret", Label: "商户RSA私钥", PEM: true, Secret: true, Required: true},
			{Key: "appurl", Label: "自定义网关URL", Note: "可不填,默认是https://pay.swiftpass.cn/pay/gateway"},
			{Key: "appswitch", Label: "微信是否支持H5", Options: []PluginOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}}},
		},
		Build: newSwiftpassPlugin,
	})
}

const (
	swiftpassDefaultGateway = "https://pay.swiftpass.cn/pay/gateway"
	swiftpassVersion        = "2.0"
)

var (
	errSwiftpassSign   = errors.New("swiftpass: reply signature invalid")
	errSwiftpassNoCode = errors.New("swiftpass: reply missing code_url")
)

type swiftpassPlugin struct {
	pluginBase
	gateway, mchID string
	privateKey     *rsa.PrivateKey
	publicKey      *rsa.PublicKey
}

func newSwiftpassPlugin(c PluginChannel) (Provider, error) {
	mch := strings.TrimSpace(c.Config["appid"])
	if mch == "" || len(mch) > 64 || strings.ContainsAny(mch, "&=?# ") {
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
	gateway := strings.TrimSpace(c.Config["appurl"])
	if gateway == "" {
		gateway = swiftpassDefaultGateway
	}
	host, ok := gatewayHost(gateway)
	if !ok {
		return nil, ErrInvalid
	}
	base, e := newPluginBase(c, pluginAccount(host, mch))
	if e != nil {
		return nil, e
	}
	return &swiftpassPlugin{pluginBase: base, gateway: gateway, mchID: mch, privateKey: priv, publicKey: pub}, nil
}

func (p *swiftpassPlugin) service() string {
	switch p.payType {
	case "alipay":
		return "pay.alipay.native"
	case "wxpay":
		return "pay.weixin.native"
	case "qqpay":
		return "pay.tenpay.native"
	case "bank":
		return "pay.unionpay.native"
	case "jdpay":
		return "pay.jdpay.native"
	}
	return ""
}

func (p *swiftpassPlugin) methods() []string { return []string{"native"} }

func (p *swiftpassPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

// sign is SwiftpassClient::makeSign for RSA_1_256: RSA-SHA256 (PKCS#1 v1.5)
// over the sorted "k=v&…" string, base64-encoded.
func (p *swiftpassPlugin) sign(params map[string]string) (string, error) {
	return rsaSign(p.privateKey, crypto.SHA256, swiftpassSignContent(params))
}

func (p *swiftpassPlugin) verifyReply(reply map[string]string) bool {
	want := reply["sign"]
	if want == "" {
		return false
	}
	return rsaVerify(p.publicKey, crypto.SHA256, swiftpassSignContent(reply), want)
}

func (p *swiftpassPlugin) ValidateCreate(r CreateRequest) error {
	return validatePluginCreate(r, p.methods())
}

func (p *swiftpassPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	params := map[string]string{
		"mch_id":        p.mchID,
		"version":       swiftpassVersion,
		"sign_type":     "RSA_1_256",
		"nonce_str":     swiftpassNonce(),
		"service":       p.service(),
		"body":          r.Subject,
		"total_fee":     strconv.FormatInt(r.Amount, 10),
		"mch_create_ip": r.ClientIP,
		"out_trade_no":  r.TradeNo,
		"notify_url":    r.NotifyURL,
	}
	sig, e := p.sign(params)
	if e != nil {
		return CreateResult{}, e
	}
	params["sign"] = sig
	return p.requestNative(ctx, "swiftpass.native", params)
}

func (p *swiftpassPlugin) requestNative(ctx context.Context, op string, params map[string]string) (CreateResult, error) {
	raw, e := p.pluginCall(ctx, op, http.MethodPost, p.gateway, []byte(swiftpassToXML(params)), "text/xml; charset=utf-8", nil)
	if e != nil {
		return CreateResult{}, e
	}
	reply, e := xmlFlat(raw)
	if e != nil {
		return CreateResult{}, &UnknownOutcome{Operation: op, Cause: e}
	}
	if !p.verifyReply(reply) {
		return CreateResult{}, &UnknownOutcome{Operation: op, Cause: errSwiftpassSign}
	}
	if reply["status"] != "0" {
		return CreateResult{}, rejected("SWIFTPASS_" + swiftpassFirst(reply["message"], reply["status"]))
	}
	if reply["result_code"] != "0" {
		return CreateResult{}, rejected("SWIFTPASS_" + swiftpassFirst(reply["err_code"], "RESULT"))
	}
	res, ok := materialFromLinks("", swiftpassFixQQ(reply["code_url"]), "")
	if !ok {
		return CreateResult{}, &UnknownOutcome{Operation: op, Cause: errSwiftpassNoCode}
	}
	res.ProviderTradeNo = reply["transaction_id"]
	return res, nil
}

func (p *swiftpassPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	reply, e := xmlFlat(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	if !p.verifyReply(reply) {
		return PaymentEvent{}, ErrAuthentication
	}
	if reply["status"] != "0" || reply["result_code"] != "0" {
		return PaymentEvent{}, ErrInvalid
	}
	amount, e := parseFen(reply["total_fee"])
	if e != nil || expected.Amount <= 0 || amount != expected.Amount || reply["out_trade_no"] != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, reply["transaction_id"], reply["openid"], amount), nil
}

func (p *swiftpassPlugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return http.StatusOK, "text/plain; charset=utf-8", "failure"
	}
	return http.StatusOK, "text/plain; charset=utf-8", "success"
}

func (p *swiftpassPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.ProviderTradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	params := map[string]string{
		"mch_id":         p.mchID,
		"version":        swiftpassVersion,
		"sign_type":      "RSA_1_256",
		"nonce_str":      swiftpassNonce(),
		"service":        "unified.trade.refund",
		"transaction_id": r.ProviderTradeNo,
		"out_refund_no":  r.RefundNo,
		"total_fee":      strconv.FormatInt(r.TotalAmount, 10),
		"refund_fee":     strconv.FormatInt(r.Amount, 10),
		"op_user_id":     p.mchID,
	}
	sig, e := p.sign(params)
	if e != nil {
		return RefundResult{}, e
	}
	params["sign"] = sig
	raw, e := p.pluginCall(ctx, "swiftpass.refund", http.MethodPost, p.gateway, []byte(swiftpassToXML(params)), "text/xml; charset=utf-8", nil)
	if e != nil {
		return RefundResult{}, e
	}
	reply, e := xmlFlat(raw)
	if e != nil {
		return RefundResult{}, &UnknownOutcome{Operation: "swiftpass.refund", Cause: e}
	}
	if !p.verifyReply(reply) {
		return RefundResult{}, &UnknownOutcome{Operation: "swiftpass.refund", Cause: errSwiftpassSign}
	}
	if reply["status"] != "0" {
		return RefundResult{}, rejected("SWIFTPASS_" + swiftpassFirst(reply["message"], reply["status"]))
	}
	if reply["result_code"] != "0" {
		return RefundResult{}, rejected("SWIFTPASS_" + swiftpassFirst(reply["err_code"], "RESULT"))
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: reply["refund_id"], State: "SUCCESS", Amount: r.Amount}, nil
}

// ---- helpers (swiftpass-local) ----

func swiftpassSignContent(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if k == "sign" || strings.TrimSpace(v) == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(k + "=" + params[k])
	}
	return b.String()
}

func swiftpassToXML(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("<xml>")
	for _, k := range keys {
		v := params[k]
		if swiftpassNumeric(v) {
			b.WriteString("<" + k + ">" + v + "</" + k + ">")
		} else {
			b.WriteString("<" + k + "><![CDATA[" + swiftpassCDATA(v) + "]]></" + k + ">")
		}
	}
	b.WriteString("</xml>")
	return b.String()
}

func swiftpassNumeric(s string) bool {
	if s == "" {
		return false
	}
	if _, e := strconv.ParseFloat(s, 64); e != nil {
		return false
	}
	return !strings.ContainsAny(s, "<>&]")
}

func swiftpassCDATA(s string) string {
	return strings.ReplaceAll(s, "]]>", "]]]]><![CDATA[>")
}

func swiftpassNonce() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func swiftpassFixQQ(codeURL string) string {
	if strings.Contains(codeURL, "myun.tenpay.com") {
		if i := strings.Index(codeURL, "&t="); i >= 0 {
			return "https://qpay.qq.com/qr/" + codeURL[i+len("&t="):]
		}
	}
	return codeURL
}

func swiftpassFirst(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

var _ Provider = (*swiftpassPlugin)(nil)
var _ NotificationVerifier = (*swiftpassPlugin)(nil)
var _ CreateValidator = (*swiftpassPlugin)(nil)
var _ NotificationAcker = (*swiftpassPlugin)(nil)
