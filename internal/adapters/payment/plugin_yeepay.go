package payment

// Port of epay_release/plugins/yeepay (易宝支付). Requests use the YOP gateway
// openapi.yeepay.com/yop-center with a YOP-RSA2048-SHA256 Authorization header:
// the merchant RSA key signs a canonical request string, body params are
// rawurlencoded form fields. Create posts /rest/v1.0/aggpay/pre-pay; the QR /
// JSAPI payload is result.prePayTn. Callbacks arrive as POST field "response"
// in the form encRandomKey$encData$symAlg$digestAlg: the merchant key RSA-
// decrypts the random key, AES-128-ECB decrypts the data, and the trailing
// RSA-SHA256 signature is verified with YOP's public key. Refund posts
// /rest/v1.0/trade/refund (success code OPR00000).

import (
	"context"
	"crypto"
	"crypto/aes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// yeepayTimestamp reproduces gmdate('Y-m-d\TH:i:s\Z').
func yeepayTimestamp() string { return time.Now().UTC().Format("2006-01-02T15:04:05Z") }

// yeepayRequestID is a unique request id; the exact shape is not validated by
// YOP, only that it is present and signed.
func yeepayRequestID() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return time.Now().UTC().Format("20060102150405.000000")
	}
	return hex.EncodeToString(b)
}

const (
	yeepayServerRoot = "https://openapi.yeepay.com/yop-center"
	// YOP platform public key (PKIX base64), shipped with the SDK.
	yeepayYopPublicKey = "MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA6p0XWjscY+gsyqKRhw9MeLsEmhFdBRhT2emOck/F1Omw38ZWhJxh9kDfs5HzFJMrVozgU+SJFDONxs8UB0wMILKRmqfLcfClG9MyCNuJkkfm0HFQv1hRGdOvZPXj3Bckuwa7FrEXBRYUhK7vJ40afumspthmse6bs6mZxNn/mALZ2X07uznOrrc2rk41Y2HftduxZw6T4EmtWuN2x4CZ8gwSyPAW5ZzZJLQ6tZDojBK4GZTAGhnn3bg5bBsBlw2+FLkCQBuDsJVsFPiGh/b6K/+zGTvWyUcu+LUj2MejYQELDO3i2vQXVDk7lVi2/TcUYefvIcssnzsfCfjaorxsuwIDAQAB"
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "yeepay",
		Title:  "易宝支付",
		Author: "易宝支付",
		Link:   "https://www.yeepay.com/",
		Types:  []string{"alipay", "wxpay", "bank"},
		Inputs: []PluginInput{
			{Key: "appkey", Label: "应用标识", Required: true},
			{Key: "appsecret", Label: "商户私钥", Note: "RSA格式", Textarea: true, Secret: true, Required: true},
			{Key: "appid", Label: "发起方商户编号", Required: true},
			{Key: "appmchid", Label: "收款商户编号", Note: "留空则与发起方商户编号一致"},
			{Key: "appswitch", Label: "支付场景", Options: []PluginOption{{Value: "0", Label: "线上"}, {Value: "1", Label: "线下"}}},
		},
		Note:  "密钥需要选RSA格式的",
		Build: newYeepayPlugin,
	})
}

type yeepayPlugin struct {
	pluginBase
	appKey     string
	parentMerc string
	mercNo     string
	scene      string
	priv       *rsa.PrivateKey
	yopPub     *rsa.PublicKey
	// serverRoot defaults to the production YOP gateway; overridden in tests.
	serverRoot string
}

func newYeepayPlugin(c PluginChannel) (Provider, error) {
	appKey := strings.TrimSpace(c.Config["appkey"])
	parent := strings.TrimSpace(c.Config["appid"])
	if appKey == "" || len(appKey) > 64 || parent == "" || len(parent) > 32 {
		return nil, ErrInvalid
	}
	merc := strings.TrimSpace(c.Config["appmchid"])
	if merc == "" {
		merc = parent
	}
	priv, e := pluginPrivateKey(yeepayWrapKey(c.Config["appsecret"]))
	if e != nil || priv.N.BitLen() < 2048 {
		return nil, ErrInvalid
	}
	yopPub, e := pluginPublicKey(yeepayWrapPub(yeepayYopPublicKey))
	if e != nil {
		return nil, ErrInvalid
	}
	scene := "ONLINE"
	if c.Config["appswitch"] == "1" {
		scene = "OFFLINE"
	}
	host, _ := gatewayHost(yeepayServerRoot)
	base, e := newPluginBase(c, pluginAccount(host, parent, merc))
	if e != nil {
		return nil, e
	}
	return &yeepayPlugin{pluginBase: base, appKey: appKey, parentMerc: parent, mercNo: merc, scene: scene, priv: priv, yopPub: yopPub, serverRoot: yeepayServerRoot}, nil
}

func yeepayWrapKey(body string) string { return yeepayWrap(body, "RSA PRIVATE KEY") }
func yeepayWrapPub(body string) string { return yeepayWrap(body, "PUBLIC KEY") }
func yeepayWrap(body, label string) string {
	body = strings.TrimSpace(body)
	if strings.Contains(body, "-----BEGIN") {
		return body
	}
	body = strings.Join(strings.Fields(body), "")
	var sb strings.Builder
	sb.WriteString("-----BEGIN " + label + "-----\n")
	for i := 0; i < len(body); i += 64 {
		end := i + 64
		if end > len(body) {
			end = len(body)
		}
		sb.WriteString(body[i:end] + "\n")
	}
	sb.WriteString("-----END " + label + "-----")
	return sb.String()
}

// Pay-type channel → the YOP channel the original USER_SCAN path uses. Only QR
// (native) is implemented in this wave.
func (p *yeepayPlugin) channelCode() (string, bool) {
	switch p.payType {
	case "alipay":
		return "ALIPAY", true
	case "wxpay":
		return "WECHAT", true
	case "bank":
		return "UNIONPAY", true
	}
	return "", false
}

func (p *yeepayPlugin) methods() []string { return []string{"native"} }

func (p *yeepayPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

func (p *yeepayPlugin) ValidateCreate(r CreateRequest) error {
	return validatePluginCreate(r, p.methods())
}

func (p *yeepayPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	ch, ok := p.channelCode()
	if !ok {
		return CreateResult{}, ErrUnsupported
	}
	params := map[string]string{
		"parentMerchantNo": p.parentMerc,
		"merchantNo":       p.mercNo,
		"orderId":          r.TradeNo,
		"orderAmount":      yuan(r.Amount),
		"goodsName":        r.Subject,
		"notifyUrl":        r.NotifyURL,
		"redirectUrl":      r.ReturnURL,
		"payWay":           "USER_SCAN",
		"channel":          ch,
		"scene":            p.scene,
		"userIp":           r.ClientIP,
	}
	out, e := p.post(ctx, "yeepay.prepay", "/rest/v1.0/aggpay/pre-pay", params)
	if e != nil {
		return CreateResult{}, e
	}
	if jsonText(out, "code") != "00000" {
		if code := jsonText(out, "code"); code != "" {
			return CreateResult{}, rejected("YOP_" + code)
		}
		return CreateResult{}, &UnknownOutcome{Operation: "yeepay.prepay", Cause: errYeepayReply}
	}
	tn := jsonText(out, "prePayTn")
	res, ok := materialFromLinks("", tn, "")
	if !ok {
		return CreateResult{}, rejected("YOP_NO_PREPAYTN")
	}
	res.ProviderTradeNo = jsonText(out, "uniqueOrderNo")
	return res, nil
}

// post signs and sends a YOP form request, returning the result object.
func (p *yeepayPlugin) post(ctx context.Context, op, path string, params map[string]string) (map[string]any, error) {
	form := url.Values{}
	signParams := map[string]string{}
	for k, v := range params {
		if v == "" {
			continue
		}
		form.Set(k, v)
		signParams[k] = v
	}
	auth, e := p.authorization(http.MethodPost, path, signParams)
	if e != nil {
		return nil, e
	}
	headers := map[string]string{
		"x-yop-appkey":     p.appKey,
		"x-yop-request-id": auth.requestID,
		"Authorization":    auth.header,
	}
	raw, e := p.pluginCall(ctx, op, http.MethodPost, p.serverRoot+path, []byte(form.Encode()), "application/x-www-form-urlencoded; charset=utf-8", headers)
	if e != nil {
		return nil, e
	}
	top, e := callbackJSON(raw)
	if e != nil {
		return nil, &UnknownOutcome{Operation: op, Cause: e}
	}
	if res, ok := top["result"].(map[string]any); ok {
		return res, nil
	}
	if code := jsonText(top, "subCode"); code != "" {
		return nil, rejected("YOP_" + code)
	}
	if _, ok := top["error"]; ok {
		return nil, rejected("YOP_ERROR")
	}
	return nil, &UnknownOutcome{Operation: op, Cause: errYeepayReply}
}

type yeepayAuth struct {
	header    string
	requestID string
}

// authorization builds the YOP-RSA2048-SHA256 header, reproducing
// getSignedHeaders: canonicalRequest = authString\nmethod\npath\n
// canonicalQuery\ncanonicalHeaders, signed base64url(RSA-SHA256)+"$SHA256".
func (p *yeepayPlugin) authorization(method, path string, params map[string]string) (yeepayAuth, error) {
	timestamp := yeepayTimestamp()
	requestID := yeepayRequestID()
	const protocol = "yop-auth-v2"
	const expired = "1800"
	authString := protocol + "/" + p.appKey + "/" + timestamp + "/" + expired

	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var q strings.Builder
	for i, k := range keys {
		if i > 0 {
			q.WriteByte('&')
		}
		q.WriteString(k + "=" + params[k])
	}

	canonicalHeader := "x-yop-request-id:" + requestID
	signedHeaders := "x-yop-request-id"

	canonicalRequest := authString + "\n" + method + "\n" + path + "\n" + q.String() + "\n" + canonicalHeader

	sig, e := yeepaySignYOP(p.priv, canonicalRequest)
	if e != nil {
		return yeepayAuth{}, e
	}
	header := "YOP-RSA2048-SHA256 " + protocol + "/" + p.appKey + "/" + timestamp + "/" + expired + "/" + signedHeaders + "/" + sig
	return yeepayAuth{header: header, requestID: requestID}, nil
}

// yeepaySignYOP signs with RSA-SHA256 and returns base64url(sig)+"$SHA256".
func yeepaySignYOP(key *rsa.PrivateKey, data string) (string, error) {
	sig, e := rsaSign(key, crypto.SHA256, data)
	if e != nil {
		return "", e
	}
	raw, e := base64.StdEncoding.DecodeString(sig)
	if e != nil {
		return "", e
	}
	return base64.RawURLEncoding.EncodeToString(raw) + "$SHA256", nil
}

// VerifyNotification decodes the "response" field (enc$data$alg$digest):
// RSA-decrypt the random key, AES-128-ECB decrypt the data, split the trailing
// "$sign", verify RSA-SHA256 with YOP's public key, then match our order.
func (p *yeepayPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	form, e := callbackParams(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	data, e := p.notifyDecrypt(form["response"])
	if e != nil {
		return PaymentEvent{}, e
	}
	obj, e := callbackJSON(data)
	if e != nil {
		return PaymentEvent{}, ErrInvalid
	}
	if jsonText(obj, "status") != "SUCCESS" {
		return PaymentEvent{}, ErrInvalid
	}
	if jsonText(obj, "orderId") != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	amount, e := parseYuan(jsonText(obj, "orderAmount"))
	if e != nil || expected.Amount <= 0 || amount != expected.Amount {
		return PaymentEvent{}, ErrAuthentication
	}
	buyer := ""
	if payer, ok := obj["payerInfo"]; ok {
		if s, isStr := payer.(string); isStr {
			if pi, pe := callbackJSON([]byte(s)); pe == nil {
				buyer = jsonText(pi, "userID")
			}
		}
	}
	return paidEvent(expected.TradeNo, jsonText(obj, "uniqueOrderNo"), buyer, amount), nil
}

// notifyDecrypt reproduces YopClient::notifyDecrypt.
func (p *yeepayPlugin) notifyDecrypt(source string) ([]byte, error) {
	if source == "" {
		return nil, ErrInvalid
	}
	args := strings.Split(source, "$")
	if len(args) != 4 {
		return nil, ErrInvalid
	}
	encKey, e := yeepayB64URL(args[0])
	if e != nil {
		return nil, ErrInvalid
	}
	randomKey, e := rsa.DecryptPKCS1v15(nil, p.priv, encKey)
	if e != nil || len(randomKey) != 16 {
		return nil, ErrAuthentication
	}
	encData, e := yeepayB64URL(args[1])
	if e != nil {
		return nil, ErrInvalid
	}
	plain, e := yeepayAESECBDecrypt(randomKey, encData)
	if e != nil {
		return nil, ErrAuthentication
	}
	idx := strings.LastIndexByte(string(plain), '$')
	if idx < 0 {
		return nil, ErrAuthentication
	}
	sourceData := string(plain[:idx])
	sign := string(plain[idx+1:])
	if !yeepayVerifyYOP(p.yopPub, sourceData, sign) {
		return nil, ErrAuthentication
	}
	return []byte(sourceData), nil
}

// yeepayAESECBDecrypt decrypts AES-128-ECB with PKCS#7 padding (stdlib has no
// ECB mode, so the block loop is written here).
func yeepayAESECBDecrypt(key, ciphertext []byte) ([]byte, error) {
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	bs := block.BlockSize()
	if len(ciphertext) == 0 || len(ciphertext)%bs != 0 {
		return nil, errors.New("yeepay: bad ECB ciphertext length")
	}
	out := make([]byte, len(ciphertext))
	for i := 0; i < len(ciphertext); i += bs {
		block.Decrypt(out[i:i+bs], ciphertext[i:i+bs])
	}
	pad := int(out[len(out)-1])
	if pad <= 0 || pad > bs || pad > len(out) {
		return nil, errors.New("yeepay: bad ECB padding")
	}
	for _, b := range out[len(out)-pad:] {
		if int(b) != pad {
			return nil, errors.New("yeepay: bad ECB padding")
		}
	}
	return out[:len(out)-pad], nil
}

// yeepayVerifyYOP verifies a base64url(RSA-SHA256) signature (the inner notify
// sign carries no "$SHA256" suffix).
func yeepayVerifyYOP(key *rsa.PublicKey, data, sign string) bool {
	raw, e := yeepayB64URL(sign)
	if e != nil {
		return false
	}
	h := sha256.Sum256([]byte(data))
	return rsa.VerifyPKCS1v15(key, crypto.SHA256, h[:], raw) == nil
}

func yeepayB64URL(s string) ([]byte, error) {
	s = strings.TrimRight(s, "=")
	if b, e := base64.RawURLEncoding.DecodeString(s); e == nil {
		return b, nil
	}
	return base64.RawStdEncoding.DecodeString(s)
}

// NotificationAck answers "SUCCESS"/"FAIL" as the original echoes.
func (p *yeepayPlugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return http.StatusOK, "text/plain; charset=utf-8", "FAIL"
	}
	return http.StatusOK, "text/plain; charset=utf-8", "SUCCESS"
}

// Refund posts /rest/v1.0/trade/refund; success code OPR00000.
func (p *yeepayPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	params := map[string]string{
		"parentMerchantNo": p.parentMerc,
		"merchantNo":       p.mercNo,
		"orderId":          r.TradeNo,
		"refundRequestId":  r.RefundNo,
		"refundAmount":     yuan(r.Amount),
	}
	out, e := p.post(ctx, "yeepay.refund", "/rest/v1.0/trade/refund", params)
	if e != nil {
		return RefundResult{}, e
	}
	if jsonText(out, "code") != "OPR00000" {
		if code := jsonText(out, "code"); code != "" {
			return RefundResult{}, rejected("YOP_" + code)
		}
		return RefundResult{}, &UnknownOutcome{Operation: "yeepay.refund", Cause: errYeepayReply}
	}
	amount := r.Amount
	if f, fe := parseYuan(jsonText(out, "refundAmount")); fe == nil {
		amount = f
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(out, "uniqueRefundNo"), State: "SUCCESS", Amount: amount}, nil
}

type yeepayReplyError string

func (e yeepayReplyError) Error() string { return string(e) }

var errYeepayReply = yeepayReplyError("yeepay: unreadable upstream reply")

var _ Provider = (*yeepayPlugin)(nil)
var _ NotificationVerifier = (*yeepayPlugin)(nil)
var _ CreateValidator = (*yeepayPlugin)(nil)
