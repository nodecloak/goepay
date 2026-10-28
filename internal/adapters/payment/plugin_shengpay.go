package payment

// Port of epay_release/plugins/shengpay (盛付通). JSON gateway
// https://mchapi.shengpay.com. Requests carry the business fields plus
// mchId/nonceStr/signType and a sign that is RSA-SHA1 (PHP openssl_sign
// default) over the ksort-joined "k=v&…" string (ShengPayClient::getSignContent,
// skipping "sign" and empty/null values) with the merchant private key
// (appkey), base64-encoded. Replies are verified with the 盛付通 public key
// (appsecret) over the same shape when they carry a sign; success is
// returnCode==SUCCESS && resultCode==SUCCESS. Online uses /pay/unifiedorder
// (offline /pay/unifiedorderOffline). The reply's payInfo is the QR content /
// redirect URL / JSAPI string depending on tradeType.
//
// Implemented: native QR (alipay_qr / wx_native), page/h5 jump (alipay_pc /
// alipay_wap / wx_wap) and UnionPay QR (upacp_qr). Refund is /refund/orderRefund.
// Callback is a JSON POST; ack echoes "SUCCESS" / "FAIL" / "SIGN FAIL".
//
// Out of scope (per wave brief): transfers (/merchant/fund/*), settle, profit
// sharing (sharingnotify), merchant onboarding (applynotify) and complaint
// callbacks (complainnotify, which also needs the AES event key), JSAPI/公众号
// /小程序 (need a bound WeChat/Alipay appid and openid from OAuth the platform
// performs), the 小程序收银台, and order query (the SDK has no query service).

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
		Name:   "shengpay",
		Title:  "盛付通",
		Author: "盛付通",
		Link:   "https://www.shengpay.com/",
		Types:  []string{"alipay", "wxpay", "bank"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "商户号", Required: true},
			{Key: "appkey", Label: "商户私钥", PEM: true, Secret: true, Required: true},
			{Key: "appsecret", Label: "盛付通公钥", PEM: true, Required: true},
			{Key: "appswitch", Label: "收单接口类型", Options: []PluginOption{{Value: "0", Label: "线上"}, {Value: "1", Label: "线下"}}},
			{Key: "appmchid", Label: "子商户号", Note: "非代理商户可留空"},
		},
		Build: newShengpayPlugin,
	})
}

const shengpayGateway = "https://mchapi.shengpay.com"

var (
	errShengpaySign   = errors.New("shengpay: reply signature invalid")
	errShengpayNoInfo = errors.New("shengpay: reply missing payInfo")
)

type shengpayPlugin struct {
	pluginBase
	mchID, subMchID string
	gateway         string
	offline         bool
	privateKey      *rsa.PrivateKey
	publicKey       *rsa.PublicKey
}

// hostOverride points the plugin at a test server (test seam only).
func (p *shengpayPlugin) hostOverride(base string) { p.gateway = base }

func newShengpayPlugin(c PluginChannel) (Provider, error) {
	mch := strings.TrimSpace(c.Config["appid"])
	if mch == "" || len(mch) > 64 {
		return nil, ErrInvalid
	}
	priv, e := pluginPrivateKey(c.Certificates["appkey"])
	if e != nil {
		return nil, ErrInvalid
	}
	pub, e := pluginPublicKey(c.Certificates["appsecret"])
	if e != nil {
		return nil, ErrInvalid
	}
	host, _ := gatewayHost(shengpayGateway)
	sub := strings.TrimSpace(c.Config["appmchid"])
	base, e := newPluginBase(c, pluginAccount(host, mch, sub))
	if e != nil {
		return nil, e
	}
	return &shengpayPlugin{pluginBase: base, mchID: mch, subMchID: sub, gateway: shengpayGateway, offline: c.Config["appswitch"] == "1", privateKey: priv, publicKey: pub}, nil
}

// tradeType maps the channel pay type + method to the shengpay tradeType, and
// reports whether the reply payInfo is a QR (true) or a redirect URL (false).
func (p *shengpayPlugin) tradeType(method string) (string, bool) {
	switch p.payType {
	case "alipay":
		switch method {
		case "native":
			return "alipay_qr", true
		case "page":
			return "alipay_pc", false
		case "wap":
			return "alipay_wap", false
		}
	case "wxpay":
		switch method {
		case "native":
			return "wx_native", true
		case "h5":
			return "wx_wap", false
		}
	case "bank":
		if method == "native" {
			return "upacp_qr", true
		}
	}
	return "", false
}

func (p *shengpayPlugin) methods() []string {
	switch p.payType {
	case "alipay":
		return []string{"page", "wap", "native"}
	case "wxpay":
		return []string{"native", "h5"}
	default: // bank
		return []string{"native"}
	}
}

func (p *shengpayPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

func (p *shengpayPlugin) sign(params map[string]string) (string, error) {
	return rsaSign(p.privateKey, crypto.SHA1, shengpaySignContent(params))
}

func (p *shengpayPlugin) verify(params map[string]string) bool {
	want := params["sign"]
	if want == "" {
		return false
	}
	return rsaVerify(p.publicKey, crypto.SHA1, shengpaySignContent(params), want)
}

func (p *shengpayPlugin) ValidateCreate(r CreateRequest) error {
	return validatePluginCreate(r, p.methods())
}

func (p *shengpayPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	tradeType, isQR := p.tradeType(r.Method)
	if tradeType == "" {
		return CreateResult{}, ErrInvalid
	}
	params := map[string]string{
		"outTradeNo": r.TradeNo,
		"totalFee":   strconv.FormatInt(r.Amount, 10),
		"currency":   "CNY",
		"tradeType":  tradeType,
		"notifyUrl":  r.NotifyURL,
		"body":       r.Subject,
		"clientIp":   r.ClientIP,
	}
	if r.ReturnURL != "" {
		params["pageUrl"] = r.ReturnURL
	}
	if p.subMchID != "" {
		params["subMchId"] = p.subMchID
	}
	reply, e := p.execute(ctx, "shengpay.create", params)
	if e != nil {
		return CreateResult{}, e
	}
	payInfo := reply["payInfo"]
	if payInfo == "" {
		return CreateResult{}, &UnknownOutcome{Operation: "shengpay.create", Cause: errShengpayNoInfo}
	}
	var res CreateResult
	var ok bool
	if isQR {
		res, ok = materialFromLinks("", payInfo, "")
	} else {
		res, ok = materialFromLinks(payInfo, "", "")
	}
	if !ok {
		return CreateResult{}, &UnknownOutcome{Operation: "shengpay.create", Cause: errShengpayNoInfo}
	}
	res.ProviderTradeNo = reply["transactionId"]
	return res, nil
}

// execute signs the params, posts the JSON body, verifies the reply signature
// (when present) and checks returnCode/resultCode exactly as
// ShengPayClient::execute does, returning the reply's string fields.
func (p *shengpayPlugin) execute(ctx context.Context, op string, params map[string]string) (map[string]string, error) {
	path := "/pay/unifiedorder"
	if p.offline && op == "shengpay.create" {
		path = "/pay/unifiedorderOffline"
	}
	if op == "shengpay.refund" {
		path = "/refund/orderRefund"
	}
	params["mchId"] = p.mchID
	params["nonceStr"] = shengpayNonce()
	params["signType"] = "RSA"
	sig, e := p.sign(params)
	if e != nil {
		return nil, e
	}
	params["sign"] = sig
	raw, e := p.postJSON(ctx, op, p.gateway+path, shengpayStringMap(params), nil)
	if e != nil {
		return nil, e
	}
	reply, e := shengpayFlatten(raw)
	if e != nil {
		return nil, &UnknownOutcome{Operation: op, Cause: e}
	}
	if reply["returnCode"] != "SUCCESS" {
		if reply["returnMsg"] != "" || reply["returnCode"] != "" {
			return nil, rejected("SHENGPAY_" + shengpayFirst(reply["returnCode"], "RETURN"))
		}
		return nil, &UnknownOutcome{Operation: op, Cause: errors.New("shengpay: unparseable reply")}
	}
	if reply["resultCode"] != "SUCCESS" {
		return nil, rejected("SHENGPAY_" + shengpayFirst(reply["errorCode"], "RESULT"))
	}
	if reply["sign"] != "" && !p.verify(reply) {
		return nil, &UnknownOutcome{Operation: op, Cause: errShengpaySign}
	}
	return reply, nil
}

func (p *shengpayPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	data, e := shengpayFlatten(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	if !p.verify(data) {
		return PaymentEvent{}, ErrAuthentication
	}
	if data["status"] != "PAY_SUCCESS" {
		return PaymentEvent{}, ErrInvalid
	}
	amount, e := parseFen(data["totalFee"])
	if e != nil || expected.Amount <= 0 || amount != expected.Amount || data["outTradeNo"] != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	buyer := ""
	if info := data["payerInfo"]; info != "" {
		if m, e := callbackJSON([]byte(info)); e == nil {
			buyer = jsonText(m, "openid")
		}
	}
	return paidEvent(expected.TradeNo, data["transactionId"], buyer, amount), nil
}

// The original echoes "SUCCESS" on success, "FAIL"/"SIGN FAIL" otherwise.
func (p *shengpayPlugin) NotificationAck(err error) (int, string, string) {
	if err == nil {
		return http.StatusOK, "text/plain; charset=utf-8", "SUCCESS"
	}
	if errors.Is(err, ErrAuthentication) {
		return http.StatusOK, "text/plain; charset=utf-8", "SIGN FAIL"
	}
	return http.StatusOK, "text/plain; charset=utf-8", "FAIL"
}

func (p *shengpayPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	params := map[string]string{
		"outTradeNo":  r.TradeNo,
		"outRefundNo": r.RefundNo,
		"refundFee":   strconv.FormatInt(r.Amount, 10),
	}
	reply, e := p.execute(ctx, "shengpay.refund", params)
	if e != nil {
		return RefundResult{}, e
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: reply["refundId"], State: "SUCCESS", Amount: r.Amount}, nil
}

// ---- helpers (shengpay-local) ----

// shengpaySignContent is ShengPayClient::getSignContent: ksort, "k=v&…",
// skipping "sign" and empty/null values.
func shengpaySignContent(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if k == "sign" || v == "" {
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

// shengpayFlatten decodes a flat JSON reply/callback object into string fields.
// Nested objects/arrays (e.g. payerInfo) are kept as their compact JSON text,
// which is how the upstream sends them (a JSON string in the signed set).
func shengpayFlatten(raw []byte) (map[string]string, error) {
	m, e := callbackJSON(raw)
	if e != nil {
		return nil, e
	}
	out := make(map[string]string, len(m))
	for k := range m {
		out[k] = jsonText(m, k)
	}
	return out, nil
}

func shengpayStringMap(params map[string]string) map[string]string {
	cp := make(map[string]string, len(params))
	for k, v := range params {
		cp[k] = v
	}
	return cp
}

func shengpayNonce() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// shengpayFirst returns a when non-blank, else b.
func shengpayFirst(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

var _ Provider = (*shengpayPlugin)(nil)
var _ NotificationVerifier = (*shengpayPlugin)(nil)
var _ CreateValidator = (*shengpayPlugin)(nil)
var _ NotificationAcker = (*shengpayPlugin)(nil)
