package payment

// Port of epay_release/plugins/dinpay (智付). The upstream is a 国密 JSON API
// (DinpayClient): the business JSON is SM4-CBC encrypted under a per-request
// random 16-byte SM4 key (fixed IV base64 "T172oxqWwkr8wqB9D7aR7g=="), the SM4
// key is SM2-encrypted to the platform public key (base64 of 0x04||C1C3C2),
// the ciphertext "data" is SM2-signed with the merchant private key (base64 of
// DER), and the envelope { merchantId, timestamp, data, encryptionKey,
// signatureMethod:"SM3WITHSM2", sign } is POSTed as JSON.
//
// Notifications are form POSTed: $_POST['data'] (plaintext business JSON) and
// $_POST['sign'] (SM2 signature over that JSON string with the platform public
// key). We verify the signature, then orderStatus SUCCESS + orderNo==TradeNo +
// payAmount (yuan) == expected.
//
// Products: QR (AppPay/SCAN) for alipay/wxpay/unionpay → "native"; WeChat/Alipay
// public (payPublic, needs openid/appid) → "jsapi". Create returns the upstream
// qrcode (native) or payInfo JSON string (jsapi). Refund (payRefund) supported.
//
// Out of scope (reported): applet (payApplet), H5/WAP (payH5), profit sharing,
// and the refund/transfer/complaint business callbacks.

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

// smErrUnreadable marks a 2xx reply we could not parse (unknown outcome).
var smErrUnreadable = errors.New("unreadable upstream response")

// randomBytesN returns n cryptographically random bytes.
func randomBytesN(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "dinpay",
		Title:  "智付",
		Author: "智付",
		Link:   "https://www.dinpay.com/",
		Types:  []string{"alipay", "wxpay", "bank"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "商户号", Required: true},
			{Key: "appsecret", Label: "商户私钥", Note: "SM2-Hex格式", Textarea: true, Secret: true, Required: true},
			{Key: "appkey", Label: "平台公钥", Note: "SM2-Hex格式", Textarea: true, Required: true},
			{Key: "appmchid", Label: "子商户号", Note: "可留空"},
			{Key: "reportid", Label: "渠道商户报备ID", Note: "可留空，多个报备ID可用,隔开"},
		},
		Note:  "智付SM2公私钥，Hex 格式",
		Build: newDinpayPlugin,
	})
}

const (
	dinpayURL       = "https://payment.dinpay.com/trx"
	dinpaySignAlg   = "SM3WITHSM2"
	dinpaySM4IVBase = "T172oxqWwkr8wqB9D7aR7g=="
)

type dinpayPlugin struct {
	pluginBase
	mchID    string
	reportID string
	priv     *sm2PrivateKey
	pub      *sm2PublicKey
	sm4IV    []byte
	baseURL  string
}

func newDinpayPlugin(c PluginChannel) (Provider, error) {
	appid := strings.TrimSpace(c.Config["appid"])
	// The original replaces appid with a non-"[" appmchid when present.
	if sub := strings.TrimSpace(c.Config["appmchid"]); sub != "" && !strings.HasPrefix(sub, "[") {
		appid = sub
	}
	if appid == "" || len(appid) > 64 {
		return nil, ErrInvalid
	}
	priv, err := sm2ParsePrivateHex(c.Config["appsecret"])
	if err != nil {
		return nil, ErrInvalid
	}
	pub, err := sm2ParsePublicHex(c.Config["appkey"])
	if err != nil {
		return nil, ErrInvalid
	}
	iv, err := base64.StdEncoding.DecodeString(dinpaySM4IVBase)
	if err != nil || len(iv) != 16 {
		return nil, ErrInvalid
	}
	base, err := newPluginBase(c, pluginAccount(dinpayURL, appid))
	if err != nil {
		return nil, err
	}
	return &dinpayPlugin{pluginBase: base, mchID: appid, reportID: strings.TrimSpace(c.Config["reportid"]), priv: priv, pub: pub, sm4IV: iv, baseURL: dinpayURL}, nil
}

func (p *dinpayPlugin) methods() []string {
	// QR for every type; JSAPI (public pay) for alipay/wxpay only.
	if p.payType == "bank" {
		return []string{"native"}
	}
	return []string{"native", "jsapi"}
}

func (p *dinpayPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

func (p *dinpayPlugin) ValidateCreate(r CreateRequest) error {
	if e := validatePluginCreate(r, p.methods()); e != nil {
		return e
	}
	if r.Method == "jsapi" && (r.OpenID == "" || len(r.OpenID) > 128) {
		return ErrInvalid
	}
	return nil
}

// paymentType maps the GOEPAY pay type to dinpay's paymentType field.
func (p *dinpayPlugin) paymentType() string {
	switch p.payType {
	case "alipay":
		return "ALIPAY"
	case "wxpay":
		return "WXPAY"
	default:
		return "UNIONPAY"
	}
}

// dinpayRandomKey returns a 16-byte random SM4 key.
func dinpayRandomKey() ([]byte, error) { return randomBytesN(16) }

// call sends an assembled envelope and parses the { code, data, sign } reply.
// code 0000/0001 is success; data is SM4-decrypted... but the response SM4 key
// is the SAME per-request key, so we keep it to decrypt the reply.
func (p *dinpayPlugin) call(ctx context.Context, operation, path string, data map[string]string) (map[string]any, error) {
	sm4Key, err := dinpayRandomKey()
	if err != nil {
		return nil, err
	}
	enc, err := sm2EncryptC1C3C2(p.pub, sm4Key)
	if err != nil {
		return nil, err
	}
	js, err := json.Marshal(data)
	if err != nil {
		return nil, ErrInvalid
	}
	ct, err := sm4EncryptCBC(sm4Key, p.sm4IV, js)
	if err != nil {
		return nil, err
	}
	encData := base64.StdEncoding.EncodeToString(ct)
	der, err := sm2SignASN1(p.priv, "", []byte(encData))
	if err != nil {
		return nil, err
	}
	envelope := map[string]string{
		"merchantId":      p.mchID,
		"timestamp":       time.Now().Format("20060102150405"),
		"data":            encData,
		"encryptionKey":   base64.StdEncoding.EncodeToString(append([]byte{0x04}, enc...)),
		"signatureMethod": dinpaySignAlg,
		"sign":            base64.StdEncoding.EncodeToString(der),
	}
	raw, err := p.postJSON(ctx, operation, p.baseURL+path, envelope, nil)
	if err != nil {
		return nil, err
	}
	reply, err := callbackJSON(raw)
	if err != nil {
		return nil, &UnknownOutcome{Operation: operation, Cause: err}
	}
	code := jsonText(reply, "code")
	if code != "0000" && code != "0001" {
		if code != "" {
			return nil, rejected("DINPAY_" + code)
		}
		return nil, &UnknownOutcome{Operation: operation, Cause: smErrUnreadable}
	}
	// data is the plaintext business JSON (verified by the platform sign when
	// present). If it is SM4 ciphertext, decrypt it with the per-request key.
	payload := jsonText(reply, "data")
	if payload == "" {
		return nil, rejected("DINPAY_EMPTY")
	}
	// Verify the platform signature over data when present.
	if sign := jsonText(reply, "sign"); sign != "" {
		if sigDER, e := base64.StdEncoding.DecodeString(sign); e != nil || !sm2VerifyASN1(p.pub, "", []byte(payload), sigDER) {
			return nil, ErrAuthentication
		}
	}
	obj, err := dinpayParseData(payload, sm4Key, p.sm4IV)
	if err != nil {
		return nil, &UnknownOutcome{Operation: operation, Cause: err}
	}
	return obj, nil
}

// dinpayParseData reads the reply data field, which is either plaintext JSON or
// base64 SM4-CBC ciphertext under the per-request key.
func dinpayParseData(payload string, sm4Key, iv []byte) (map[string]any, error) {
	if strings.HasPrefix(strings.TrimSpace(payload), "{") {
		return callbackJSON([]byte(payload))
	}
	ct, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, smErrUnreadable
	}
	pt, err := sm4DecryptCBC(sm4Key, iv, ct)
	if err != nil {
		return nil, err
	}
	return callbackJSON(pt)
}

func (p *dinpayPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	switch r.Method {
	case "native":
		data := map[string]string{
			"interfaceName":  "AppPay",
			"paymentType":    p.paymentType(),
			"paymentMethods": "SCAN",
			"paymentCode":    "1",
			"payAmount":      yuan(r.Amount),
			"currency":       "CNY",
			"orderNo":        r.TradeNo,
			"orderIp":        r.ClientIP,
			"goodsName":      r.Subject,
			"notifyUrl":      r.NotifyURL,
		}
		if p.reportID != "" {
			data["reportId"] = p.reportID
		}
		out, err := p.call(ctx, "dinpay.pay", "/api/appPay/pay", data)
		if err != nil {
			return CreateResult{}, err
		}
		qr := jsonText(out, "qrcode")
		res, ok := materialFromLinks("", qr, "")
		if !ok {
			return CreateResult{}, rejected("DINPAY_NO_QRCODE")
		}
		res.ProviderTradeNo = jsonText(out, "channelNumber")
		return res, nil
	case "jsapi":
		data := map[string]string{
			"interfaceName":  "AppPayPublic",
			"paymentType":    p.paymentType(),
			"paymentMethods": "PUBLIC",
			"openid":         r.OpenID,
			"payAmount":      yuan(r.Amount),
			"currency":       "CNY",
			"orderNo":        r.TradeNo,
			"orderIp":        r.ClientIP,
			"goodsName":      r.Subject,
			"isNative":       "1",
			"notifyUrl":      r.NotifyURL,
			"successToUrl":   r.ReturnURL,
		}
		if p.payType == "alipay" {
			data["appid"] = "1"
		}
		if p.reportID != "" {
			data["reportId"] = p.reportID
		}
		out, err := p.call(ctx, "dinpay.payPublic", "/api/appPay/payPublic", data)
		if err != nil {
			return CreateResult{}, err
		}
		payInfo := jsonText(out, "payInfo")
		if payInfo == "" || len(payInfo) > 8192 {
			return CreateResult{}, rejected("DINPAY_NO_PAYINFO")
		}
		return CreateResult{MaterialType: "jsapi", Material: payInfo, ProviderTradeNo: jsonText(out, "channelNumber")}, nil
	}
	return CreateResult{}, ErrInvalid
}

// Notifications are form POSTed with data (plaintext JSON) + sign (SM2 over it).
func (p *dinpayPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	params, err := callbackParams(raw)
	if err != nil {
		return PaymentEvent{}, err
	}
	dataStr, sign := params["data"], params["sign"]
	if dataStr == "" || sign == "" {
		return PaymentEvent{}, ErrAuthentication
	}
	sigDER, err := base64.StdEncoding.DecodeString(sign)
	if err != nil || !sm2VerifyASN1(p.pub, "", []byte(dataStr), sigDER) {
		return PaymentEvent{}, ErrAuthentication
	}
	data, err := callbackJSON([]byte(dataStr))
	if err != nil {
		return PaymentEvent{}, ErrInvalid
	}
	if jsonText(data, "orderStatus") != "SUCCESS" {
		return PaymentEvent{}, ErrInvalid
	}
	amount, err := parseYuan(jsonText(data, "payAmount"))
	if err != nil || expected.Amount <= 0 || amount != expected.Amount {
		return PaymentEvent{}, ErrAuthentication
	}
	if jsonText(data, "orderNo") != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, jsonText(data, "channelNumber"), jsonText(data, "subOpenId"), amount), nil
}

// The original echoes plain "success"/"fail" for notifications.
func (p *dinpayPlugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return http.StatusOK, "text/plain; charset=utf-8", "fail"
	}
	return http.StatusOK, "text/plain; charset=utf-8", "success"
}

// Refund posts /api/appPay/payRefund; code 0000/0001 is success.
func (p *dinpayPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	data := map[string]string{
		"interfaceName": "AppPayRefund",
		"payOrderNo":    r.TradeNo,
		"refundOrderNo": r.RefundNo,
		"refundAmount":  yuan(r.Amount),
	}
	out, err := p.call(ctx, "dinpay.payRefund", "/api/appPay/payRefund", data)
	if err != nil {
		return RefundResult{}, err
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(out, "refundChannelNumber"), State: "SUCCESS", Amount: r.Amount}, nil
}

var _ Provider = (*dinpayPlugin)(nil)
var _ NotificationVerifier = (*dinpayPlugin)(nil)
var _ CreateValidator = (*dinpayPlugin)(nil)
var _ NotificationAcker = (*dinpayPlugin)(nil)
