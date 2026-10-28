package payment

// Port of epay_release/plugins/fuiou3 (富友支付(互联网)): the 富友 internet
// aggregate gateway (hlwnets.fuioupay.com/aggpos). There is no field-level
// signature: the whole JSON request is RSA/PKCS1v15 encrypted with the 富友
// public key (chunked at keyBytes-11), base64'd, and posted as
// {"mchnt_cd":..,"message":..}. A reply {"resp_code":"0000","message":..} and
// the async callback {"message":..} are RSA/PKCS1v15 decrypted with the
// merchant private key (chunked at keyBytes). order_info / payInfo is the QR
// (native) or the JSAPI material. Amounts are integer cents. Refund posts to a
// distinct refund host. Callbacks: POST raw JSON body, order_st==1 is paid,
// acked "success".
//
// Out of scope: transfers (the plugin has none) and WeChat mini-program
// jump-token (TAPPLET) which depends on a bound 公众号/小程序.

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "fuiou3",
		Title:  "富友支付(互联网)",
		Author: "富友",
		Link:   "https://www.fuiou.com/",
		Types:  []string{"alipay", "wxpay", "bank"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "商户号", Required: true},
			{Key: "appsecret", Label: "商户私钥", Textarea: true, Secret: true, Required: true},
			{Key: "appkey", Label: "富友公钥", Textarea: true, Required: true},
			{Key: "appswitch", Label: "环境选择", Options: []PluginOption{{Value: "0", Label: "生产环境"}, {Value: "1", Label: "测试环境"}}},
		},
		// select_wxpay: 1=扫码, 2=公众号/小程序, 3=跳转富友小程序.
		AppTypes: []PluginOption{{Value: "1", Label: "扫码支付"}, {Value: "2", Label: "公众号/小程序支付"}, {Value: "3", Label: "跳转富友小程序"}},
		Build:    newFuiou3Plugin,
	})
}

type fuiou3Endpoints struct {
	order  string
	refund string
}

var fuiou3Prod = fuiou3Endpoints{
	order:  "https://hlwnets.fuioupay.com/aggpos/order.fuiou",
	refund: "https://refund-transfer.fuioupay.com/refund_transfer/aggposRefund.fuiou",
}
var fuiou3Test = fuiou3Endpoints{
	order:  "https://hlwnets-test.fuioupay.com/aggpos/order.fuiou",
	refund: "https://refund-transfer-test.fuioupay.com/refund_transfer/aggposRefund.fuiou",
}

type fuiou3Plugin struct {
	pluginBase
	mchntCd string
	priv    *rsa.PrivateKey
	pub     *rsa.PublicKey
	ep      fuiou3Endpoints
}

func newFuiou3Plugin(c PluginChannel) (Provider, error) {
	mchntCd := strings.TrimSpace(c.Config["appid"])
	if mchntCd == "" || len(mchntCd) > 32 {
		return nil, ErrInvalid
	}
	priv, e := pluginPrivateKey(c.Config["appsecret"])
	if e != nil {
		return nil, ErrInvalid
	}
	pub, e := pluginPublicKey(c.Config["appkey"])
	if e != nil {
		return nil, ErrInvalid
	}
	ep := fuiou3Prod
	if c.Config["appswitch"] == "1" {
		ep = fuiou3Test
	}
	host, _ := gatewayHost(ep.order)
	base, e := newPluginBase(c, pluginAccount(host, mchntCd))
	if e != nil {
		return nil, e
	}
	return &fuiou3Plugin{pluginBase: base, mchntCd: mchntCd, priv: priv, pub: pub, ep: ep}, nil
}

func (p *fuiou3Plugin) fuiou3OrderType() string {
	switch p.payType {
	case "alipay":
		return "ALIPAY"
	case "wxpay":
		return "WECHAT"
	default:
		return "UNIONPAY"
	}
}

func (p *fuiou3Plugin) methods() []string {
	// native (QR) for every pay type; jsapi for alipay (FWC) and wxpay (JSAPI).
	m := []string{"native"}
	if p.payType == "alipay" || p.payType == "wxpay" {
		m = append(m, "jsapi")
	}
	return m
}

func (p *fuiou3Plugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

// fuiou3Encrypt RSA/PKCS1v15-encrypts data with the platform public key,
// chunked at keyBytes-11, then base64 (the original rsaPublicEncrypt).
func (p *fuiou3Plugin) fuiou3Encrypt(data []byte) (string, error) {
	chunk := p.pub.Size() - 11
	if chunk <= 0 {
		return "", ErrInvalid
	}
	var out []byte
	for len(data) > 0 {
		n := chunk
		if n > len(data) {
			n = len(data)
		}
		enc, e := fuiou3RSAEncrypt(p.pub, data[:n])
		if e != nil {
			return "", e
		}
		out = append(out, enc...)
		data = data[n:]
	}
	return base64.StdEncoding.EncodeToString(out), nil
}

// fuiou3Decrypt RSA/PKCS1v15-decrypts a base64 message with the merchant
// private key, chunked at keyBytes (the original rsaPrivateDecrypt).
func (p *fuiou3Plugin) fuiou3Decrypt(b64 string) ([]byte, error) {
	raw, e := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if e != nil {
		return nil, ErrInvalid
	}
	chunk := p.priv.Size()
	if chunk <= 0 || len(raw) == 0 || len(raw)%chunk != 0 {
		return nil, ErrInvalid
	}
	var out []byte
	for len(raw) > 0 {
		dec, e := fuiou3RSADecrypt(p.priv, raw[:chunk])
		if e != nil {
			return nil, ErrInvalid
		}
		out = append(out, dec...)
		raw = raw[chunk:]
	}
	return out, nil
}

// fuiou3Submit encrypts the request, posts it, and decrypts a 0000 reply.
func (p *fuiou3Plugin) fuiou3Submit(ctx context.Context, operation, target string, biz map[string]string) (map[string]any, error) {
	req := map[string]string{"ver": "1.0.0", "mchnt_cd": p.mchntCd}
	for k, v := range biz {
		req[k] = v
	}
	plain, e := json.Marshal(req)
	if e != nil {
		return nil, ErrInvalid
	}
	message, e := p.fuiou3Encrypt(plain)
	if e != nil {
		return nil, e
	}
	raw, e := p.postJSON(ctx, operation, target, map[string]string{"mchnt_cd": p.mchntCd, "message": message}, nil)
	if e != nil {
		return nil, e
	}
	envelope, e := callbackJSON(raw)
	if e != nil {
		return nil, &UnknownOutcome{Operation: operation, Cause: e}
	}
	if jsonText(envelope, "resp_code") != "0000" {
		if jsonText(envelope, "resp_desc") != "" {
			return nil, rejected("FUIOU3_" + jsonText(envelope, "resp_code"))
		}
		return nil, &UnknownOutcome{Operation: operation, Cause: errFuiou3BadReply}
	}
	decoded, e := p.fuiou3Decrypt(jsonText(envelope, "message"))
	if e != nil {
		return nil, &UnknownOutcome{Operation: operation, Cause: e}
	}
	out, e := callbackJSON(decoded)
	if e != nil {
		return nil, &UnknownOutcome{Operation: operation, Cause: e}
	}
	return out, nil
}

func (p *fuiou3Plugin) ValidateCreate(r CreateRequest) error {
	if e := validatePluginCreate(r, p.methods()); e != nil {
		return e
	}
	if r.Method == "jsapi" && r.OpenID == "" {
		return ErrInvalid
	}
	return nil
}

func (p *fuiou3Plugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	payType := p.fuiou3OrderType()
	var appid, openid string
	if r.Method == "jsapi" {
		if p.payType == "alipay" {
			payType, openid = "FWC", r.OpenID
		} else {
			payType = "JSAPI"
			if i := strings.IndexByte(r.OpenID, ':'); i > 0 {
				appid, openid = r.OpenID[:i], r.OpenID[i+1:]
			} else {
				openid = r.OpenID
			}
		}
	}
	biz := map[string]string{
		"order_date":      time.Now().UTC().Format("20060102"),
		"order_id":        r.TradeNo,
		"order_amt":       strconv.FormatInt(r.Amount, 10),
		"order_pay_type":  payType,
		"back_notify_url": r.NotifyURL,
		"goods_name":      r.Subject,
		"goods_detail":    r.Subject,
	}
	if appid != "" {
		biz["appid"] = appid
	}
	if openid != "" {
		biz["openid"] = openid
	}
	out, e := p.fuiou3Submit(ctx, "fuiou3.order", p.ep.order, biz)
	if e != nil {
		return CreateResult{}, e
	}
	info := jsonText(out, "order_info")
	if info == "" {
		return CreateResult{}, rejected("FUIOU3_NO_ORDER_INFO")
	}
	if r.Method == "jsapi" {
		if p.payType == "alipay" {
			// order_info is a JSON string carrying the alipay tradeNO.
			tn := info
			if obj, e := callbackJSON([]byte(info)); e == nil {
				if v := jsonText(obj, "tradeNO"); v != "" {
					tn = v
				}
			}
			return CreateResult{MaterialType: "jsapi", Material: tn, ProviderTradeNo: jsonText(out, "fy_order_id")}, nil
		}
		return CreateResult{MaterialType: "jsapi", Material: info, ProviderTradeNo: jsonText(out, "fy_order_id")}, nil
	}
	if len(info) > 4096 || strings.ContainsAny(info, "\x00\r\n") {
		return CreateResult{}, rejected("FUIOU3_NO_QR")
	}
	return CreateResult{MaterialType: "qrcode", Material: info, ProviderTradeNo: jsonText(out, "fy_order_id")}, nil
}

func (p *fuiou3Plugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	envelope, e := callbackJSON(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	message := jsonText(envelope, "message")
	if message == "" {
		return PaymentEvent{}, ErrInvalid
	}
	decoded, e := p.fuiou3Decrypt(message)
	if e != nil {
		// A message we cannot decrypt is not an authenticated callback.
		return PaymentEvent{}, ErrAuthentication
	}
	arr, e := callbackJSON(decoded)
	if e != nil {
		return PaymentEvent{}, ErrInvalid
	}
	if jsonText(arr, "order_st") != "1" {
		return PaymentEvent{}, ErrInvalid
	}
	amount, e := parseFen(jsonText(arr, "order_amt"))
	if e != nil || expected.Amount <= 0 || amount != expected.Amount || jsonText(arr, "order_id") != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	providerTrade := jsonText(arr, "order_fas_ssn")
	return paidEvent(expected.TradeNo, providerTrade, jsonText(arr, "openid"), amount), nil
}

// The original notify() echoes "success" for a verified paid callback.
func (p *fuiou3Plugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return http.StatusOK, "text/plain; charset=utf-8", "status_fail"
	}
	return http.StatusOK, "text/plain; charset=utf-8", "success"
}

func (p *fuiou3Plugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	today := time.Now().UTC().Format("20060102")
	biz := map[string]string{
		"refund_order_date": today,
		"refund_order_id":   r.RefundNo,
		"pay_order_date":    today,
		"pay_order_id":      r.TradeNo,
		"refund_amt":        strconv.FormatInt(r.Amount, 10),
	}
	out, e := p.fuiou3Submit(ctx, "fuiou3.refund", p.ep.refund, biz)
	if e != nil {
		return RefundResult{}, e
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(out, "pay_order_id"), State: "SUCCESS", Amount: r.Amount}, nil
}

var _ Provider = (*fuiou3Plugin)(nil)
var _ NotificationVerifier = (*fuiou3Plugin)(nil)
var _ CreateValidator = (*fuiou3Plugin)(nil)
var _ NotificationAcker = (*fuiou3Plugin)(nil)

var errFuiou3BadReply = newFuiou3Error("fuiou3: unexpected upstream reply")

func newFuiou3Error(s string) error { return &fuiou3Error{s} }

type fuiou3Error struct{ s string }

func (e *fuiou3Error) Error() string { return e.s }

// fuiou3RSAEncrypt / fuiou3RSADecrypt are the PKCS#1 v1.5 primitives the kit
// does not expose (it only signs/verifies). fuiou3 encrypts the whole request
// body with the platform public key and decrypts replies with its own key.
func fuiou3RSAEncrypt(pub *rsa.PublicKey, msg []byte) ([]byte, error) {
	return rsa.EncryptPKCS1v15(rand.Reader, pub, msg)
}
func fuiou3RSADecrypt(priv *rsa.PrivateKey, ct []byte) ([]byte, error) {
	return rsa.DecryptPKCS1v15(rand.Reader, priv, ct)
}
