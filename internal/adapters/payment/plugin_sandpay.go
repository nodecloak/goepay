package payment

// Port of epay_release/plugins/sandpay (杉德支付, 杉德 openapi v4). JSON gateway
// https://openapi.sandpay.com.cn (UAT https://openapi-uat01.sand.com.cn). Each
// request (SandpayClient::execute) is:
//   1. ksort the business params, JSON-encode (unescaped unicode+slashes);
//   2. AES-128-ECB encrypt that plaintext under a fresh random 16-byte key,
//      base64 -> bizData;
//   3. RSA (PKCS#1 v1.5) encrypt the AES key with the 杉德 public key,
//      base64 -> encryptKey;
//   4. RSA-SHA256 sign the bizData string with the merchant private key,
//      base64 -> sign;
//   5. POST {accessMid,timestamp,version,signType,encryptType,encryptKey,
//      bizData,sign}.
// Replies with respCode==success are RSA-SHA256 verified over bizData with the
// 杉德 public key, the AES key is RSA-decrypted with the merchant key, bizData
// is AES-decrypted and parsed; resultStatus==fail is a business refusal.
//
// Credentials in GOEPAY: appid = 商户编号 (accessMid/mid), appkey = PFX password,
// and two PEM inputs — sandcert = the 杉德 public certificate (sand.cer, PEM or
// base64 DER) and sandpfx = the merchant PKCS#12 (.pfx) as base64 text. The PHP
// reads these from cert/ on disk; GOEPAY takes them as channel config so Build
// stays offline. x/crypto/pkcs12 decodes the PFX.
//
// Implemented: native QR (payType ALIPAY/WXPAY/CUPPAY, payMode QR) for
// alipay/wxpay/bank, and alipay JSAPI (payMode JSAPI, needs the payer uid in
// r.OpenID) which returns the tradeNo as the JSAPI material. Refund is
// trans.order.refund. Callback is a POST of sign+bizData (plaintext JSON);
// ack echoes "respCode=000000" / "respCode=020002".
//
// Out of scope (per wave brief): fastpay (快捷/SANDH5), transfers and
// transfer/balance queries (/v4/sd-payment/*), WeChat JSAPI/小程序 (need a bound
// WeChat sub_appid the platform supplies) and order query (query is only used
// internally by the notify handler for logging).

import (
	"context"
	"crypto"
	"crypto/aes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/pkcs12"
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "sandpay",
		Title:  "杉德支付",
		Author: "杉德",
		Link:   "https://www.sandpay.com.cn/",
		Types:  []string{"alipay", "wxpay", "bank"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "商户编号", Required: true},
			{Key: "appkey", Label: "私钥证书密码", Secret: true, Required: true},
			{Key: "appswitch", Label: "环境选择", Options: []PluginOption{{Value: "0", Label: "生产环境"}, {Value: "1", Label: "测试环境"}}},
			{Key: "product", Label: "市场产品", Options: []PluginOption{{Value: "QZF", Label: "标准线上收款"}, {Value: "CSDB", Label: "企业杉德宝"}}},
			{Key: "sandcert", Label: "杉德公钥证书(sand.cer)", PEM: true, Required: true},
			{Key: "sandpfx", Label: "商户私钥证书(client.pfx, base64)", PEM: true, Secret: true, Required: true},
		},
		Note:  "将杉德公钥证书sand.cer与商户私钥证书client.pfx内容填入对应证书输入框（PFX以base64形式填写），私钥证书密码填入“私钥证书密码”",
		Build: newSandpayPlugin,
	})
}

const (
	sandpayProdURL = "https://openapi.sandpay.com.cn"
	sandpayUATURL  = "https://openapi-uat01.sand.com.cn"
	sandpayVersion = "4.0.0"
)

var (
	errSandpaySign    = errors.New("sandpay: reply signature invalid")
	errSandpayDecrypt = errors.New("sandpay: reply decryption failed")
	errSandpayNoQR    = errors.New("sandpay: reply missing credential")
)

type sandpayPlugin struct {
	pluginBase
	apiURL, mid, product string
	privateKey           *rsa.PrivateKey
	publicKey            *rsa.PublicKey
}

func newSandpayPlugin(c PluginChannel) (Provider, error) {
	mid := strings.TrimSpace(c.Config["appid"])
	if mid == "" || len(mid) > 64 {
		return nil, ErrInvalid
	}
	product := strings.TrimSpace(c.Config["product"])
	if product == "" {
		product = "QZF"
	}
	pub, e := sandpayCertPublicKey(c.Certificates["sandcert"])
	if e != nil {
		return nil, ErrInvalid
	}
	priv, e := sandpayPFXPrivateKey(c.Certificates["sandpfx"], c.Config["appkey"])
	if e != nil {
		return nil, ErrInvalid
	}
	apiURL := sandpayProdURL
	if c.Config["appswitch"] == "1" {
		apiURL = sandpayUATURL
	}
	host, _ := gatewayHost(apiURL)
	base, e := newPluginBase(c, pluginAccount(host, mid, product))
	if e != nil {
		return nil, e
	}
	return &sandpayPlugin{pluginBase: base, apiURL: apiURL, mid: mid, product: product, privateKey: priv, publicKey: pub}, nil
}

func (p *sandpayPlugin) payTypeCode() string {
	switch p.payType {
	case "alipay":
		return "ALIPAY"
	case "wxpay":
		return "WXPAY"
	case "bank":
		return "CUPPAY"
	}
	return ""
}

func (p *sandpayPlugin) methods() []string {
	if p.payType == "alipay" {
		return []string{"native", "jsapi"}
	}
	return []string{"native"}
}

func (p *sandpayPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

func (p *sandpayPlugin) ValidateCreate(r CreateRequest) error {
	if e := validatePluginCreate(r, p.methods()); e != nil {
		return e
	}
	if r.Method == "jsapi" && strings.TrimSpace(r.OpenID) == "" {
		return ErrInvalid
	}
	return nil
}

func (p *sandpayPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	payMode := "QR"
	if r.Method == "jsapi" {
		payMode = "JSAPI"
	}
	params := map[string]any{
		"marketProduct": p.product,
		"outReqTime":    time.Now().Format("20060102150405"),
		"mid":           p.mid,
		"outOrderNo":    r.TradeNo,
		"description":   r.Subject,
		"goodsClass":    "01",
		"amount":        yuan(r.Amount),
		"payType":       p.payTypeCode(),
		"payMode":       payMode,
		"notifyUrl":     r.NotifyURL,
		"riskmgtInfo":   map[string]any{"sourceIp": r.ClientIP},
	}
	if r.Method == "jsapi" {
		params["payerInfo"] = map[string]any{"userId": r.OpenID, "frontUrl": r.ReturnURL}
	} else {
		params["payerInfo"] = map[string]any{"payAccLimit": ""}
	}
	reply, e := p.execute(ctx, "sandpay.create", "/v4/sd-receipts/api/trans/trans.order.create", params)
	if e != nil {
		return CreateResult{}, e
	}
	credential, _ := reply["credential"].(map[string]any)
	if r.Method == "jsapi" {
		tradeNo := jsonText(reply, "tradeNo")
		if tradeNo == "" && credential != nil {
			tradeNo = jsonText(credential, "tradeNo")
		}
		if tradeNo == "" {
			return CreateResult{}, &UnknownOutcome{Operation: "sandpay.create", Cause: errSandpayNoQR}
		}
		return CreateResult{MaterialType: "jsapi", Material: tradeNo, ProviderTradeNo: jsonText(reply, "sandSerialNo")}, nil
	}
	qr := ""
	if credential != nil {
		qr = jsonText(credential, "qrCode")
	}
	res, ok := materialFromLinks("", qr, "")
	if !ok {
		return CreateResult{}, &UnknownOutcome{Operation: "sandpay.create", Cause: errSandpayNoQR}
	}
	res.ProviderTradeNo = jsonText(reply, "sandSerialNo")
	return res, nil
}

// execute performs the full encrypt/sign/post/verify/decrypt round trip.
func (p *sandpayPlugin) execute(ctx context.Context, op, path string, params map[string]any) (map[string]any, error) {
	plaintext, e := sandpayCanonicalJSON(params)
	if e != nil {
		return nil, ErrInvalid
	}
	var aesKey [16]byte
	if _, e := rand.Read(aesKey[:]); e != nil {
		return nil, e
	}
	cipherText, e := sandpayAESEncrypt([]byte(plaintext), aesKey[:])
	if e != nil {
		return nil, e
	}
	bizData := base64.StdEncoding.EncodeToString(cipherText)
	encKey, e := rsa.EncryptPKCS1v15(rand.Reader, p.publicKey, aesKey[:])
	if e != nil {
		return nil, e
	}
	sig, e := rsaSign(p.privateKey, crypto.SHA256, bizData)
	if e != nil {
		return nil, e
	}
	envelope := map[string]string{
		"accessMid":   p.mid,
		"timestamp":   time.Now().Format("2006-01-02 15:04:05"),
		"version":     sandpayVersion,
		"signType":    "RSA",
		"encryptType": "AES",
		"encryptKey":  base64.StdEncoding.EncodeToString(encKey),
		"bizData":     bizData,
		"sign":        sig,
	}
	body, _ := json.Marshal(envelope)
	raw, e := p.pluginCall(ctx, op, http.MethodPost, p.apiURL+path, body, "application/json; charset=utf-8", nil)
	if e != nil {
		return nil, e
	}
	top, e := callbackJSON(raw)
	if e != nil {
		return nil, &UnknownOutcome{Operation: op, Cause: e}
	}
	switch jsonText(top, "respCode") {
	case "success":
		// proceed
	case "fail":
		return nil, rejected("SANDPAY_FAIL")
	default:
		return nil, &UnknownOutcome{Operation: op, Cause: errors.New("sandpay: unparseable reply")}
	}
	replyBiz := jsonText(top, "bizData")
	if !rsaVerify(p.publicKey, crypto.SHA256, replyBiz, jsonText(top, "sign")) {
		return nil, &UnknownOutcome{Operation: op, Cause: errSandpaySign}
	}
	encKeyB64 := jsonText(top, "encryptKey")
	encKeyBytes, e := base64.StdEncoding.DecodeString(encKeyB64)
	if e != nil {
		return nil, &UnknownOutcome{Operation: op, Cause: errSandpayDecrypt}
	}
	replyAESKey, e := rsa.DecryptPKCS1v15(rand.Reader, p.privateKey, encKeyBytes)
	if e != nil {
		return nil, &UnknownOutcome{Operation: op, Cause: errSandpayDecrypt}
	}
	cipherBytes, e := base64.StdEncoding.DecodeString(replyBiz)
	if e != nil {
		return nil, &UnknownOutcome{Operation: op, Cause: errSandpayDecrypt}
	}
	plain, e := sandpayAESDecrypt(cipherBytes, replyAESKey)
	if e != nil {
		return nil, &UnknownOutcome{Operation: op, Cause: errSandpayDecrypt}
	}
	arr, e := callbackJSON(plain)
	if e != nil {
		return nil, &UnknownOutcome{Operation: op, Cause: errSandpayDecrypt}
	}
	if jsonText(arr, "resultStatus") == "fail" {
		return nil, rejected("SANDPAY_" + sandpayFirst(jsonText(arr, "errorCode"), "BIZ"))
	}
	return arr, nil
}

// notify: POST of sign + bizData, where bizData is the plaintext JSON (not
// encrypted); verify RSA-SHA256 with the 杉德 public key over bizData.
func (p *sandpayPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	form, e := callbackParams(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	bizData, sign := form["bizData"], form["sign"]
	if bizData == "" || sign == "" {
		return PaymentEvent{}, ErrInvalid
	}
	if !rsaVerify(p.publicKey, crypto.SHA256, bizData, sign) {
		return PaymentEvent{}, ErrAuthentication
	}
	data, e := callbackJSON([]byte(bizData))
	if e != nil {
		return PaymentEvent{}, ErrInvalid
	}
	if jsonText(data, "orderStatus") != "success" {
		return PaymentEvent{}, ErrInvalid
	}
	amount, e := parseYuan(jsonText(data, "amount"))
	if e != nil || expected.Amount <= 0 || amount != expected.Amount || jsonText(data, "outOrderNo") != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	buyer := ""
	if payer, ok := data["payer"].(map[string]any); ok {
		buyer = jsonText(payer, "payerAccNo")
	}
	return paidEvent(expected.TradeNo, jsonText(data, "sandSerialNo"), buyer, amount), nil
}

// The original echoes "respCode=000000" on success, "respCode=020002" otherwise.
func (p *sandpayPlugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return http.StatusOK, "text/plain; charset=utf-8", "respCode=020002"
	}
	return http.StatusOK, "text/plain; charset=utf-8", "respCode=000000"
}

func (p *sandpayPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	params := map[string]any{
		"marketProduct": p.product,
		"outReqTime":    time.Now().Format("20060102150405"),
		"mid":           p.mid,
		"outOrderNo":    r.RefundNo,
		"oriOutOrderNo": r.TradeNo,
		"amount":        yuan(r.Amount),
	}
	reply, e := p.execute(ctx, "sandpay.refund", "/v4/sd-receipts/api/trans/trans.order.refund", params)
	if e != nil {
		return RefundResult{}, e
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(reply, "sandSerialNo"), State: "SUCCESS", Amount: r.Amount}, nil
}

// ---- helpers (sandpay-local) ----

// sandpayCanonicalJSON ksorts the top-level keys and renders compact JSON with
// unescaped slashes/unicode (PHP json_encode(JSON_UNESCAPED_UNICODE |
// JSON_UNESCAPED_SLASHES) after ksort). Nested objects keep their own key order.
func sandpayCanonicalJSON(params map[string]any) (string, error) {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(sandpayEncodeString(k))
		b.WriteByte(':')
		frag, e := sandpayEncodeValue(params[k])
		if e != nil {
			return "", e
		}
		b.WriteString(frag)
	}
	b.WriteByte('}')
	return b.String(), nil
}

func sandpayEncodeValue(v any) (string, error) {
	switch t := v.(type) {
	case string:
		return sandpayEncodeString(t), nil
	case map[string]any:
		// Preserve insertion-independent but stable order by sorting keys.
		return sandpayCanonicalJSON(t)
	default:
		raw, e := json.Marshal(t)
		if e != nil {
			return "", e
		}
		return string(raw), nil
	}
}

// sandpayEncodeString JSON-quotes with unescaped slashes/unicode.
func sandpayEncodeString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				b.WriteString("\\u")
				const hexdigits = "0123456789abcdef"
				b.WriteByte(hexdigits[(r>>12)&0xF])
				b.WriteByte(hexdigits[(r>>8)&0xF])
				b.WriteByte(hexdigits[(r>>4)&0xF])
				b.WriteByte(hexdigits[r&0xF])
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// sandpayAESEncrypt / sandpayAESDecrypt implement AES-128-ECB with PKCS#7
// padding, matching PHP openssl_encrypt($data,'AES-128-ECB',$key) (base64 is
// applied by the caller).
func sandpayAESEncrypt(plaintext, key []byte) ([]byte, error) {
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	bs := block.BlockSize()
	padded := sandpayPKCS7Pad(plaintext, bs)
	out := make([]byte, len(padded))
	for i := 0; i < len(padded); i += bs {
		block.Encrypt(out[i:i+bs], padded[i:i+bs])
	}
	return out, nil
}

func sandpayAESDecrypt(ciphertext, key []byte) ([]byte, error) {
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	bs := block.BlockSize()
	if len(ciphertext) == 0 || len(ciphertext)%bs != 0 {
		return nil, errSandpayDecrypt
	}
	out := make([]byte, len(ciphertext))
	for i := 0; i < len(ciphertext); i += bs {
		block.Decrypt(out[i:i+bs], ciphertext[i:i+bs])
	}
	return sandpayPKCS7Unpad(out, bs)
}

func sandpayPKCS7Pad(data []byte, bs int) []byte {
	pad := bs - len(data)%bs
	out := make([]byte, len(data)+pad)
	copy(out, data)
	for i := len(data); i < len(out); i++ {
		out[i] = byte(pad)
	}
	return out
}

func sandpayPKCS7Unpad(data []byte, bs int) ([]byte, error) {
	n := len(data)
	if n == 0 {
		return nil, errSandpayDecrypt
	}
	pad := int(data[n-1])
	if pad == 0 || pad > bs || pad > n {
		return nil, errSandpayDecrypt
	}
	for _, c := range data[n-pad:] {
		if int(c) != pad {
			return nil, errSandpayDecrypt
		}
	}
	return data[:n-pad], nil
}

// sandpayCertPublicKey parses the 杉德 public certificate (sand.cer): PEM
// CERTIFICATE, or base64/DER, and returns its RSA public key.
func sandpayCertPublicKey(text string) (*rsa.PublicKey, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, ErrInvalid
	}
	var der []byte
	if block, _ := pem.Decode([]byte(text)); block != nil {
		der = block.Bytes
	} else {
		b, e := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(text), ""))
		if e != nil {
			return nil, ErrInvalid
		}
		der = b
	}
	cert, e := x509.ParseCertificate(der)
	if e != nil {
		return nil, ErrInvalid
	}
	key, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok || key.N.BitLen() < pluginMinRSABits {
		return nil, ErrInvalid
	}
	return key, nil
}

// sandpayPFXPrivateKey decodes a base64/PEM-wrapped PKCS#12 (.pfx) with the
// given password and returns the merchant RSA private key.
func sandpayPFXPrivateKey(text, password string) (*rsa.PrivateKey, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, ErrInvalid
	}
	var raw []byte
	if block, _ := pem.Decode([]byte(text)); block != nil {
		raw = block.Bytes
	} else {
		b, e := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(text), ""))
		if e != nil {
			return nil, ErrInvalid
		}
		raw = b
	}
	key, _, e := pkcs12.Decode(raw, password)
	if e != nil {
		return nil, ErrInvalid
	}
	priv, ok := key.(*rsa.PrivateKey)
	if !ok || priv.N.BitLen() < pluginMinRSABits {
		return nil, ErrInvalid
	}
	if e := priv.Validate(); e != nil {
		return nil, ErrInvalid
	}
	return priv, nil
}

func sandpayFirst(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

var _ Provider = (*sandpayPlugin)(nil)
var _ NotificationVerifier = (*sandpayPlugin)(nil)
var _ CreateValidator = (*sandpayPlugin)(nil)
var _ NotificationAcker = (*sandpayPlugin)(nil)
