package payment

// Port of epay_release/plugins/swiftpass2 (威富通MD5): WeChat-style XML gateway
// at <appurl> (default https://pay.swiftpass.cn/pay/gateway). Every request is
// an <xml> document of mch_id/version/sign_type/nonce_str + business fields;
// the sign is uppercase md5 over the ksort-joined "k=v&…" string with
// "&key=<appkey>" appended (SwiftpassClient::makeSign, signType MD5). The
// native/QR services (pay.alipay.native / pay.weixin.native / pay.tenpay.native
// / pay.unionpay.native / pay.jdpay.native) return code_url, shown as a QR.
// Callbacks are an XML POST verified the same way; the plugin echoes the plain
// strings "success"/"failure". Refund is unified.trade.refund.
//
// Out of scope (per wave brief): JSAPI/公众号 (needs a bound WeChat appid and
// openid from OAuth the platform performs), H5/小程序, and order query (the
// SDK has no query service).

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "swiftpass2",
		Title:  "威富通MD5",
		Author: "威富通",
		Link:   "https://www.swiftpass.cn/",
		Types:  []string{"alipay", "wxpay", "qqpay", "bank", "jdpay"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "商户号", Required: true},
			{Key: "appkey", Label: "商户密钥", Secret: true, Required: true},
			{Key: "appurl", Label: "自定义网关URL", Note: "可不填,默认是https://pay.swiftpass.cn/pay/gateway"},
			{Key: "appswitch", Label: "微信是否支持H5", Options: []PluginOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}}},
		},
		Build: newSwiftpass2Plugin,
	})
}

const (
	swiftpass2DefaultGateway = "https://pay.swiftpass.cn/pay/gateway"
	swiftpass2Version        = "2.0"
)

var (
	errSwiftpass2Sign   = errors.New("swiftpass2: reply signature invalid")
	errSwiftpass2NoCode = errors.New("swiftpass2: reply missing code_url")
)

type swiftpass2Plugin struct {
	pluginBase
	gateway, mchID, key string
}

func newSwiftpass2Plugin(c PluginChannel) (Provider, error) {
	mch := strings.TrimSpace(c.Config["appid"])
	key := c.Config["appkey"]
	if mch == "" || len(mch) > 64 || strings.ContainsAny(mch, "&=?# ") || key == "" || len(key) > 256 {
		return nil, ErrInvalid
	}
	gateway := strings.TrimSpace(c.Config["appurl"])
	if gateway == "" {
		gateway = swiftpass2DefaultGateway
	}
	host, ok := gatewayHost(gateway)
	if !ok {
		return nil, ErrInvalid
	}
	base, e := newPluginBase(c, pluginAccount(host, mch))
	if e != nil {
		return nil, e
	}
	return &swiftpass2Plugin{pluginBase: base, gateway: gateway, mchID: mch, key: key}, nil
}

func (p *swiftpass2Plugin) service() string {
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

func (p *swiftpass2Plugin) methods() []string { return []string{"native"} }

func (p *swiftpass2Plugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

// sign is SwiftpassClient::makeSign for signType MD5: uppercase md5 of the
// sorted "k=v&…" string (empty values skipped) with "&key=<key>" appended.
func (p *swiftpass2Plugin) sign(params map[string]string) string {
	return strings.ToUpper(md5Hex(swiftpass2SignContent(params) + "&key=" + p.key))
}

func (p *swiftpass2Plugin) verifyReply(reply map[string]string) bool {
	want := reply["sign"]
	if want == "" {
		return false
	}
	return signEqual(p.sign(reply), want, true)
}

func (p *swiftpass2Plugin) ValidateCreate(r CreateRequest) error {
	return validatePluginCreate(r, p.methods())
}

func (p *swiftpass2Plugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	params := map[string]string{
		"mch_id":        p.mchID,
		"version":       swiftpass2Version,
		"sign_type":     "MD5",
		"nonce_str":     swiftpass2Nonce(),
		"service":       p.service(),
		"body":          r.Subject,
		"total_fee":     strconv.FormatInt(r.Amount, 10),
		"mch_create_ip": r.ClientIP,
		"out_trade_no":  r.TradeNo,
		"notify_url":    r.NotifyURL,
	}
	params["sign"] = p.sign(params)
	return p.requestNative(ctx, "swiftpass2.native", params)
}

// requestNative posts the XML request, verifies the reply signature and status
// exactly as SwiftpassClient::requestApi does, then maps code_url to a QR.
func (p *swiftpass2Plugin) requestNative(ctx context.Context, op string, params map[string]string) (CreateResult, error) {
	raw, e := p.pluginCall(ctx, op, http.MethodPost, p.gateway, []byte(swiftpass2ToXML(params)), "text/xml; charset=utf-8", nil)
	if e != nil {
		return CreateResult{}, e
	}
	reply, e := xmlFlat(raw)
	if e != nil {
		return CreateResult{}, &UnknownOutcome{Operation: op, Cause: e}
	}
	if !p.verifyReply(reply) {
		return CreateResult{}, &UnknownOutcome{Operation: op, Cause: errSwiftpass2Sign}
	}
	if reply["status"] != "0" {
		return CreateResult{}, rejected("SWIFTPASS_" + swiftpass2First(reply["message"], reply["status"]))
	}
	if reply["result_code"] != "0" {
		return CreateResult{}, rejected("SWIFTPASS_" + swiftpass2First(reply["err_code"], "RESULT"))
	}
	res, ok := materialFromLinks("", swiftpass2FixQQ(reply["code_url"]), "")
	if !ok {
		return CreateResult{}, &UnknownOutcome{Operation: op, Cause: errSwiftpass2NoCode}
	}
	res.ProviderTradeNo = reply["transaction_id"]
	return res, nil
}

// notify (XML POST) verified identically; success iff status==0 && result_code==0.
func (p *swiftpass2Plugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
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

// The original echoes the literal strings "success" / "failure".
func (p *swiftpass2Plugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return http.StatusOK, "text/plain; charset=utf-8", "failure"
	}
	return http.StatusOK, "text/plain; charset=utf-8", "success"
}

func (p *swiftpass2Plugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.ProviderTradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	params := map[string]string{
		"mch_id":         p.mchID,
		"version":        swiftpass2Version,
		"sign_type":      "MD5",
		"nonce_str":      swiftpass2Nonce(),
		"service":        "unified.trade.refund",
		"transaction_id": r.ProviderTradeNo,
		"out_refund_no":  r.RefundNo,
		"total_fee":      strconv.FormatInt(r.TotalAmount, 10),
		"refund_fee":     strconv.FormatInt(r.Amount, 10),
		"op_user_id":     p.mchID,
	}
	params["sign"] = p.sign(params)
	raw, e := p.pluginCall(ctx, "swiftpass2.refund", http.MethodPost, p.gateway, []byte(swiftpass2ToXML(params)), "text/xml; charset=utf-8", nil)
	if e != nil {
		return RefundResult{}, e
	}
	reply, e := xmlFlat(raw)
	if e != nil {
		return RefundResult{}, &UnknownOutcome{Operation: "swiftpass2.refund", Cause: e}
	}
	if !p.verifyReply(reply) {
		return RefundResult{}, &UnknownOutcome{Operation: "swiftpass2.refund", Cause: errSwiftpass2Sign}
	}
	if reply["status"] != "0" {
		return RefundResult{}, rejected("SWIFTPASS_" + swiftpass2First(reply["message"], reply["status"]))
	}
	if reply["result_code"] != "0" {
		return RefundResult{}, rejected("SWIFTPASS_" + swiftpass2First(reply["err_code"], "RESULT"))
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: reply["refund_id"], State: "SUCCESS", Amount: r.Amount}, nil
}

// ---- helpers (swiftpass2-local) ----

// swiftpass2SignContent is SwiftpassClient::getSignContent: ksort, join
// "k=v&…", skipping the "sign" field and empty/whitespace-only values.
func swiftpass2SignContent(params map[string]string) string {
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

// swiftpass2ToXML renders the WeChat-style <xml> request (CDATA for non-numeric
// values, matching SwiftpassClient::array2Xml). Keys are sorted for stable
// output; order is irrelevant to the gateway.
func swiftpass2ToXML(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("<xml>")
	for _, k := range keys {
		v := params[k]
		if swiftpass2Numeric(v) {
			b.WriteString("<" + k + ">" + v + "</" + k + ">")
		} else {
			b.WriteString("<" + k + "><![CDATA[" + swiftpass2CDATA(v) + "]]></" + k + ">")
		}
	}
	b.WriteString("</xml>")
	return b.String()
}

func swiftpass2Numeric(s string) bool {
	if s == "" {
		return false
	}
	if _, e := strconv.ParseFloat(s, 64); e != nil {
		return false
	}
	// PHP is_numeric rejects leading '+' here only loosely; a value that would
	// break out of CDATA must still be escaped, so treat anything with markup
	// characters as non-numeric.
	return !strings.ContainsAny(s, "<>&]")
}

// swiftpass2CDATA neutralises an embedded "]]>" so a hostile value cannot
// break out of the CDATA section.
func swiftpass2CDATA(s string) string {
	return strings.ReplaceAll(s, "]]>", "]]]]><![CDATA[>")
}

func swiftpass2Nonce() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// swiftpass2FixQQ mirrors the original's rewrite of a myun.tenpay.com code_url
// into a qpay.qq.com QR link.
func swiftpass2FixQQ(codeURL string) string {
	if strings.Contains(codeURL, "myun.tenpay.com") {
		if i := strings.Index(codeURL, "&t="); i >= 0 {
			return "https://qpay.qq.com/qr/" + codeURL[i+len("&t="):]
		}
	}
	return codeURL
}

func swiftpass2First(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

var _ Provider = (*swiftpass2Plugin)(nil)
var _ NotificationVerifier = (*swiftpass2Plugin)(nil)
var _ CreateValidator = (*swiftpass2Plugin)(nil)
var _ NotificationAcker = (*swiftpass2Plugin)(nil)
