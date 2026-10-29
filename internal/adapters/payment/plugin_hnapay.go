package payment

// Port of epay_release/plugins/hnapay (新生支付). Two collection products are
// ported:
//   - JSAPI (ITA10, method "jsapi", needs openid/buyer id): posts a form with
//     an encrypted msgCiphertext and a base64 RSA-SHA1 signValue to
//     gateway.hnapay.com/ita/inCharge.do; returns the JSAPI payInfo.
//   - Scan/QR (WS01, method "native"): posts scanPay.do with a hex RSA-SHA1
//     signMsg (the "old" key format) and returns a qrCodeUrl.
// Keys are RSA. appsecret is the merchant private key (PEM, bare base64 DER, or
// a base64 PKCS#12/PFX blob decoded via golang.org/x/crypto/pkcs12); appkey is
// the 新生 platform public key for the JSAPI (new) key family. The scan (old)
// family verifies replies with the baked-in platform public key hnapay.pem.
// The sign base is key=[value] concatenated in a fixed field order.
//
// OUT OF SCOPE (not implemented): quickpay (EXP12/EXP13 two-step + pay.page /
// agreement.page), the H5 alipay product (MUP11), transfers (SGP01/02/03,
// singlePay), balance query and all fukuan callbacks. Refund (EXP09) IS ported.

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/crypto/pkcs12"
)

// hnapayPlatformScanPubPEM is cert/hnapay.pem, the baked-in platform public key
// used to verify the scan (old shoukuan key) product's replies and callbacks.
const hnapayPlatformScanPubPEM = `-----BEGIN PUBLIC KEY-----
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQCf21zJo95Uf9KKPLxagqzaT+L0
fvsKuLG5cW5rzzHKwgfe8TkU2/ZnI2T0DowRvz7w98kfKBKxu0q/VV8QV21Ui/Ax
OXdfrbQEQ/QVSXtF8NtCpaXqcSOdNQF9dDNpx/VrDpaarv6xp/4nfbeAlf+t6IdU
kfo8Rz0Ne5fihpsSRwIDAQAB
-----END PUBLIC KEY-----`

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "hnapay",
		Title:  "新生支付",
		Author: "新生支付",
		Link:   "https://www.hnapay.com/",
		Types:  []string{"alipay", "wxpay", "bank"},
		// transtypes bank exists upstream but transfers are out of scope.
		Inputs: []PluginInput{
			{Key: "appid", Label: "商户ID", Note: "新生用户ID", Required: true},
			{Key: "appkey", Label: "新生公钥(新收款密钥)", Textarea: true, Required: true},
			{Key: "appsecret", Label: "商户私钥(新收款密钥)", Secret: true, Textarea: true, Required: true},
			{Key: "appmchid", Label: "报备编号", Note: "仅支付宝&微信需要填写"},
		},
		Build: newHnapayPlugin,
	})
}

type hnapayPlugin struct {
	pluginBase
	merID, orgCode, weChatMchID string
	gatewayBase                 string
	privKey                     *rsa.PrivateKey
	platformPub                 *rsa.PublicKey // JSAPI (new key) platform public key
	scanPub                     *rsa.PublicKey // baked-in scan (old key) platform public key
}

func newHnapayPlugin(c PluginChannel) (Provider, error) {
	merID := strings.TrimSpace(c.Config["appid"])
	if merID == "" || len(merID) > 64 {
		return nil, ErrInvalid
	}
	priv, e := hnapayPrivateKey(c.Config["appsecret"])
	if e != nil {
		return nil, e
	}
	pub, e := pluginPublicKey(c.Config["appkey"])
	if e != nil {
		return nil, e
	}
	scanPub, e := pluginPublicKey(hnapayPlatformScanPubPEM)
	if e != nil {
		return nil, e
	}
	orgCode := hnapayOrgCode(c.PayType)
	if orgCode == "" {
		return nil, ErrInvalid
	}
	mchID := strings.TrimSpace(c.Config["appmchid"])
	base, e := newPluginBase(c, pluginAccount("https://gateway.hnapay.com", merID, orgCode))
	if e != nil {
		return nil, e
	}
	return &hnapayPlugin{pluginBase: base, merID: merID, orgCode: orgCode, weChatMchID: mchID, gatewayBase: "https://gateway.hnapay.com", privKey: priv, platformPub: pub, scanPub: scanPub}, nil
}

// hnapayPrivateKey accepts a PEM/bare-base64 RSA key, or a base64 PKCS#12 blob.
func hnapayPrivateKey(text string) (*rsa.PrivateKey, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, ErrInvalid
	}
	if k, e := pluginPrivateKey(text); e == nil {
		return k, nil
	}
	// Try PKCS#12 (PFX): the field is base64-encoded DER with no passphrase.
	der, e := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(text), ""))
	if e != nil {
		return nil, ErrInvalid
	}
	priv, _, e := pkcs12.Decode(der, "")
	if e != nil {
		return nil, ErrInvalid
	}
	k, ok := priv.(*rsa.PrivateKey)
	if !ok || k.N.BitLen() < pluginMinRSABits {
		return nil, ErrInvalid
	}
	return k, nil
}

func hnapayOrgCode(payType string) string {
	switch payType {
	case "alipay":
		return "ALIPAY"
	case "wxpay":
		return "WECHATPAY"
	case "bank":
		return "UNIONPAY"
	}
	return ""
}

func (p *hnapayPlugin) methods() []string {
	switch p.payType {
	case "alipay", "wxpay":
		return []string{"native", "jsapi"}
	case "bank":
		return []string{"native"}
	}
	return nil
}

func (p *hnapayPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

// hnapaySignContent reproduces HnaPayApi::getSignContent: "key=[value]" with no
// separator, in the exact sign_order. A missing field is an error.
func hnapaySignContent(params map[string]string, order []string) (string, bool) {
	var b strings.Builder
	for _, k := range order {
		v, ok := params[k]
		if !ok {
			return "", false
		}
		b.WriteString(k)
		b.WriteString("=[")
		b.WriteString(v)
		b.WriteByte(']')
	}
	return b.String(), true
}

func hnapaySHA1(message string) []byte {
	h := sha1.Sum([]byte(message))
	return h[:]
}

// signBase64 signs with RSA-SHA1 and base64 (the "new" key generateSign).
func (p *hnapayPlugin) signBase64(message string) (string, error) {
	sig, e := rsa.SignPKCS1v15(rand.Reader, p.privKey, crypto.SHA1, hnapaySHA1(message))
	if e != nil {
		return "", e
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

// signHex signs with RSA-SHA1 and hex (the "old" key generateSignOld).
func (p *hnapayPlugin) signHex(message string) (string, error) {
	sig, e := rsa.SignPKCS1v15(rand.Reader, p.privKey, crypto.SHA1, hnapaySHA1(message))
	if e != nil {
		return "", e
	}
	return hex.EncodeToString(sig), nil
}

func (p *hnapayPlugin) verifyBase64(message, sig string) bool {
	raw, e := base64.StdEncoding.DecodeString(sig)
	if e != nil {
		return false
	}
	return rsa.VerifyPKCS1v15(p.platformPub, crypto.SHA1, hnapaySHA1(message), raw) == nil
}

func (p *hnapayPlugin) verifyHex(message, sig string) bool {
	raw, e := hex.DecodeString(sig)
	if e != nil {
		return false
	}
	return rsa.VerifyPKCS1v15(p.scanPub, crypto.SHA1, hnapaySHA1(message), raw) == nil
}

// encryptParams mirrors HnaPayApi::encryptParams: RSA-PKCS1v15 public-encrypt
// json_encode($params) in 117-byte chunks against the platform public key,
// concatenated and base64-encoded.
func (p *hnapayPlugin) encryptParams(params any) (string, error) {
	data, e := json.Marshal(params)
	if e != nil {
		return "", ErrInvalid
	}
	var out []byte
	for len(data) > 0 {
		n := 117
		if len(data) < n {
			n = len(data)
		}
		chunk, e := rsa.EncryptPKCS1v15(rand.Reader, p.platformPub, data[:n])
		if e != nil {
			return "", e
		}
		out = append(out, chunk...)
		data = data[n:]
	}
	return base64.StdEncoding.EncodeToString(out), nil
}

func (p *hnapayPlugin) ValidateCreate(r CreateRequest) error {
	if e := validatePluginCreate(r, p.methods()); e != nil {
		return e
	}
	if r.Method == "jsapi" {
		if r.OpenID == "" || len(r.OpenID) > 128 {
			return ErrInvalid
		}
		// JSAPI requires the report number (appmchid) and is alipay/wxpay only.
		if p.weChatMchID == "" || p.orgCode == "UNIONPAY" {
			return ErrInvalid
		}
	}
	return nil
}

func (p *hnapayPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	if r.Method == "jsapi" {
		return p.createJSAPI(ctx, r)
	}
	return p.createScan(ctx, r)
}

// createScan posts WS01 (QRCODE_B2C). Amount is fen; sign is the old hex form.
func (p *hnapayPlugin) createScan(ctx context.Context, r CreateRequest) (CreateResult, error) {
	params := map[string]string{
		"tranCode":    "WS01",
		"version":     "2.1",
		"merId":       p.merID,
		"payType":     "QRCODE_B2C",
		"charset":     "1",
		"signType":    "1",
		"merOrderNum": r.TradeNo,
		"tranAmt":     parseFenString(r.Amount),
		"submitTime":  hnapaySubmitTime(r.TradeNo),
		"orgCode":     p.orgCode,
		"goodsName":   r.Subject,
		"tranIP":      r.ClientIP,
		"notifyUrl":   r.NotifyURL,
	}
	if p.weChatMchID != "" {
		params["weChatMchId"] = p.weChatMchID
	}
	order := []string{"tranCode", "version", "merId", "submitTime", "merOrderNum", "tranAmt", "payType", "orgCode", "notifyUrl", "charset", "signType"}
	content, ok := hnapaySignContent(params, order)
	if !ok {
		return CreateResult{}, ErrInvalid
	}
	sign, e := p.signHex(content)
	if e != nil {
		return CreateResult{}, e
	}
	params["signMsg"] = sign
	raw, e := p.postForm(ctx, "hnapay.scanPay", p.gatewayBase+"/website/scanPay.do", hnapayForm(params))
	if e != nil {
		return CreateResult{}, e
	}
	out, e := callbackJSON(raw)
	if e != nil {
		return CreateResult{}, &UnknownOutcome{Operation: "hnapay.scanPay", Cause: e}
	}
	if jsonText(out, "resultCode") != "0000" {
		if code := jsonText(out, "resultCode"); code != "" {
			return CreateResult{}, rejected("HNAPAY_" + code)
		}
		return CreateResult{}, &UnknownOutcome{Operation: "hnapay.scanPay", Cause: ErrInvalid}
	}
	// Verify the reply sign (old hex) when present.
	if sm := jsonText(out, "signMsg"); sm != "" {
		vorder := []string{"tranCode", "version", "merId", "merOrderNum", "tranAmt", "submitTime", "qrCodeUrl", "hnapayOrderId", "resultCode", "charset", "signType"}
		m := hnapayStrings(out)
		content, ok := hnapaySignContent(m, vorder)
		if !ok || !p.verifyHex(content, sm) {
			return CreateResult{}, ErrAuthentication
		}
	}
	qr := hnapayQRContent(jsonText(out, "qrCodeUrl"))
	if qr == "" || len(qr) > 4096 || strings.ContainsAny(qr, "\x00\r\n") {
		return CreateResult{}, rejected("HNAPAY_NO_QR")
	}
	return CreateResult{MaterialType: "qrcode", Material: qr, ProviderTradeNo: jsonText(out, "hnapayOrderId")}, nil
}

// createJSAPI posts ITA10. Amount is yuan inside the encrypted params; sign is
// the new base64 form over the outer fields.
func (p *hnapayPlugin) createJSAPI(ctx context.Context, r CreateRequest) (CreateResult, error) {
	inner := map[string]string{
		"tranAmt":         yuan(r.Amount),
		"orgCode":         p.orgCode,
		"notifyServerUrl": r.NotifyURL,
		"merUserIp":       r.ClientIP,
		"goodsInfo":       r.Subject,
		"orderSubject":    r.Subject,
		"merchantId":      p.weChatMchID,
	}
	// The original forwards the channel's bound appid; GOEPAY has only the
	// payer identity, so appId/aliAppId is left empty and openId/buyerId carry
	// r.OpenID. This matches a normal order where the bound mp/life-account
	// appid is supplied by the platform.
	if p.orgCode == "WECHATPAY" {
		inner["appId"] = ""
		inner["openId"] = r.OpenID
	} else {
		inner["aliAppId"] = ""
		inner["buyerId"] = r.OpenID
	}
	cipher, e := p.encryptParams(inner)
	if e != nil {
		return CreateResult{}, e
	}
	params := map[string]string{
		"version":       "2.0",
		"tranCode":      "ITA10",
		"merId":         p.merID,
		"merOrderId":    r.TradeNo,
		"submitTime":    hnapaySubmitTime(r.TradeNo),
		"signType":      "1",
		"charset":       "1",
		"msgCiphertext": cipher,
	}
	order := []string{"version", "tranCode", "merId", "merOrderId", "submitTime", "msgCiphertext"}
	content, ok := hnapaySignContent(params, order)
	if !ok {
		return CreateResult{}, ErrInvalid
	}
	sign, e := p.signBase64(content)
	if e != nil {
		return CreateResult{}, e
	}
	params["signValue"] = sign
	raw, e := p.postForm(ctx, "hnapay.jsapiPay", p.gatewayBase+"/ita/inCharge.do", hnapayForm(params))
	if e != nil {
		return CreateResult{}, e
	}
	out, e := callbackJSON(raw)
	if e != nil {
		return CreateResult{}, &UnknownOutcome{Operation: "hnapay.jsapiPay", Cause: e}
	}
	if jsonText(out, "resultCode") != "0000" {
		if code := jsonText(out, "errorCode"); code != "" {
			return CreateResult{}, rejected("HNAPAY_" + code)
		}
		return CreateResult{}, &UnknownOutcome{Operation: "hnapay.jsapiPay", Cause: ErrInvalid}
	}
	if sv := jsonText(out, "signValue"); sv != "" {
		vorder := []string{"version", "tranCode", "merOrderId", "merId", "charset", "signType", "resultCode", "errorCode", "hnapayOrderId", "payInfo"}
		m := hnapayStrings(out)
		content, ok := hnapaySignContent(m, vorder)
		if !ok || !p.verifyBase64(content, sv) {
			return CreateResult{}, ErrAuthentication
		}
	}
	payInfo := jsonText(out, "payInfo")
	if payInfo == "" || len(payInfo) > 8192 || strings.ContainsRune(payInfo, 0) {
		return CreateResult{}, rejected("HNAPAY_NO_PAYINFO")
	}
	return CreateResult{MaterialType: "jsapi", Material: payInfo, ProviderTradeNo: jsonText(out, "hnapayOrderId")}, nil
}

func (p *hnapayPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	params, e := callbackParams(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	// Two callback families: scan (notifys, respCode, old hex sign) and JSAPI
	// (notify, resultCode, new base64 sign). The sign field present tells them
	// apart.
	var okSign bool
	var statusField, statusOK string
	if sm := params["signMsg"]; sm != "" {
		order := []string{"tranCode", "version", "merId", "merOrderNum", "tranAmt", "submitTime", "hnapayOrderId", "tranFinishTime", "respCode", "charset", "signType"}
		content, ok := hnapaySignContent(params, order)
		okSign = ok && p.verifyHex(content, sm)
		statusField, statusOK = params["respCode"], "0000"
	} else if sv := params["signValue"]; sv != "" {
		order := []string{"version", "tranCode", "merOrderId", "merId", "merAttach", "charset", "signType", "hnapayOrderId", "resultCode", "tranAmt", "submitTime", "tranFinishTime"}
		content, ok := hnapaySignContent(params, order)
		okSign = ok && p.verifyBase64(content, sv)
		statusField, statusOK = params["resultCode"], "0000"
	} else {
		return PaymentEvent{}, ErrInvalid
	}
	if !okSign {
		return PaymentEvent{}, ErrAuthentication
	}
	if statusField != statusOK {
		return PaymentEvent{}, ErrInvalid
	}
	outTradeNo := firstNonEmpty(params["merOrderNum"], params["merOrderId"])
	// tranAmt in callbacks is fen for scan, yuan for jsapi; detect by field set.
	var amount int64
	if params["signMsg"] != "" {
		amount, e = parseFen(params["tranAmt"])
	} else {
		amount, e = parseYuan(params["tranAmt"])
	}
	if e != nil || expected.Amount <= 0 || amount != expected.Amount || outTradeNo != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, params["hnapayOrderId"], params["userId"], amount), nil
}

// NotificationAck: both notify handlers echo plain "200" on success.
func (p *hnapayPlugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return 400, "text/plain; charset=utf-8", "sign_error"
	}
	return 200, "text/plain; charset=utf-8", "200"
}

// Refund posts EXP09 with the encrypted refund params; new base64 sign.
func (p *hnapayPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	inner := map[string]string{
		"orgMerOrderId":  r.TradeNo,
		"orgSubmitTime":  hnapaySubmitTime(r.TradeNo),
		"orderAmt":       yuan(r.TotalAmount),
		"refundOrderAmt": yuan(r.Amount),
		"notifyUrl":      "",
	}
	cipher, e := p.encryptParams(inner)
	if e != nil {
		return RefundResult{}, e
	}
	params := map[string]string{
		"version":       "2.0",
		"tranCode":      "EXP09",
		"merId":         p.merID,
		"merOrderId":    r.RefundNo,
		"submitTime":    time.Now().Format("20060102150405"),
		"signType":      "1",
		"charset":       "1",
		"msgCiphertext": cipher,
	}
	order := []string{"version", "tranCode", "merId", "merOrderId", "submitTime", "msgCiphertext"}
	content, ok := hnapaySignContent(params, order)
	if !ok {
		return RefundResult{}, ErrInvalid
	}
	sign, e := p.signBase64(content)
	if e != nil {
		return RefundResult{}, e
	}
	params["signValue"] = sign
	raw, e := p.postForm(ctx, "hnapay.refund", p.gatewayBase+"/exp/refund.do", hnapayForm(params))
	if e != nil {
		return RefundResult{}, e
	}
	out, e := callbackJSON(raw)
	if e != nil {
		return RefundResult{}, &UnknownOutcome{Operation: "hnapay.refund", Cause: e}
	}
	if jsonText(out, "resultCode") != "0000" {
		if code := jsonText(out, "errorCode"); code != "" {
			return RefundResult{}, rejected("HNAPAY_" + code)
		}
		return RefundResult{}, &UnknownOutcome{Operation: "hnapay.refund", Cause: ErrInvalid}
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(out, "hnapayOrderId"), State: "SUCCESS", Amount: r.Amount}, nil
}

// hnapayForm encodes params for an application/x-www-form-urlencoded POST.
func hnapayForm(params map[string]string) url.Values {
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	return form
}

// hnapayStrings flattens a decoded reply to string fields for sign verification.
func hnapayStrings(m map[string]any) map[string]string {
	out := map[string]string{}
	for k := range m {
		out[k] = jsonText(m, k)
	}
	return out
}

func hnapaySubmitTime(tradeNo string) string {
	if len(tradeNo) >= 14 {
		return tradeNo[:14]
	}
	return time.Now().Format("20060102150405")
}

// hnapayQRContent extracts qrContent from the returned qrCodeUrl, matching the
// original getSubstr($url,'qrContent=','&sign='). When the reply is already a
// bare QR string it is returned unchanged.
func hnapayQRContent(s string) string {
	i := strings.Index(s, "qrContent=")
	if i < 0 {
		return s
	}
	rest := s[i+len("qrContent="):]
	if j := strings.Index(rest, "&sign="); j >= 0 {
		rest = rest[:j]
	}
	if dec, e := url.QueryUnescape(rest); e == nil {
		return dec
	}
	return rest
}

var _ Provider = (*hnapayPlugin)(nil)
var _ NotificationVerifier = (*hnapayPlugin)(nil)
var _ CreateValidator = (*hnapayPlugin)(nil)
var _ NotificationAcker = (*hnapayPlugin)(nil)
