package payment

// Port of epay_release/plugins/lakalamoss (拉卡拉MOSS). The SDK (MossClient)
// talks to a single gateway https://moss.lakala.com/ord-api/unified/v3 with an
// RSA envelope: the request body JSON is RSA/ECB/PKCS1 encrypted (chunked) with
// the platform public key into requestEncrypted, the whole params object is
// RSA-SHA1 signed with the merchant private key into sign; the response is
// verified (RSA-SHA1, platform public key) then responseEncrypted is decrypted
// with the merchant private key. The platform public key is hard-coded in the
// SDK (1024-bit). All primitives are Go stdlib (rsa PKCS#1 v1.5 encrypt/decrypt,
// SHA1 sign/verify).
//
// Credential: appsecret is the merchant private key (客户私钥, PEM or bare
// base64 PKCS#8). appid is reqId/businessChannel, appmchid the merchant id.
// Refund is implemented (lfops.moss.order.ref). Transfers/complaints out of
// scope.

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// lakalamossPlatformPubB64 is the hard-coded platform public key (SPKI DER,
// base64) from MossClient::$platform_public_text.
const lakalamossPlatformPubB64 = "MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQD3E6H3qfgqF7aKypmSuzMIRuL/pRFMzsyqMlSEzzo2aJqN7w8Lb2tfVRfnAUVKMFyDxUzNWER4E/UfR4ymo0YHOaiIJI3AHWdJngJyGgK+SfvYDs9rqC++yisrzYv/TN3fZ93Ru1YWOYi4x4lBSCC9UX2b28hwx32MpJHT7gIrMQIDAQAB"

const lakalamossGateway = "https://moss.lakala.com/ord-api/unified/v3"

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "lakalamoss",
		Title:  "拉卡拉MOSS",
		Author: "拉卡拉",
		Link:   "https://moss.lakala.com/",
		Types:  []string{"alipay", "wxpay", "bank"},
		// transtypes 'bank' (转账) out of scope.
		Inputs: []PluginInput{
			{Key: "appid", Label: "APPID", Note: "reqId", Required: true},
			{Key: "appsecret", Label: "客户私钥", Note: "商户RSA私钥 PEM 或裸 base64", PEM: true, Required: true},
			{Key: "appmchid", Label: "商户ID", Required: true},
			{Key: "splitmchid", Label: "合单支付商户ID", Note: "不使用合单支付请留空"},
		},
		Note:  "主扫/JSAPI支付。退款已实现。转账/投诉未实现。合单支付(splitmchid)按原插件比例拆分。",
		Build: newLakalamossPlugin,
	})
}

type lakalamossPlugin struct {
	pluginBase
	appid, mchid, splitMchid string
	privateKey               *rsa.PrivateKey
	platformKey              *rsa.PublicKey
	gateway                  string
}

func newLakalamossPlugin(c PluginChannel) (Provider, error) {
	appid := strings.TrimSpace(c.Config["appid"])
	mchid := strings.TrimSpace(c.Config["appmchid"])
	if appid == "" || len(appid) > 64 || mchid == "" || len(mchid) > 64 {
		return nil, ErrInvalid
	}
	key, e := pluginPrivateKey(c.Certificates["appsecret"])
	if e != nil {
		return nil, e
	}
	platformKey, e := lakalamossPlatformKey()
	if e != nil {
		return nil, e
	}
	base, e := newPluginBase(c, pluginAccount("https://moss.lakala.com", mchid))
	if e != nil {
		return nil, e
	}
	return &lakalamossPlugin{
		pluginBase: base, appid: appid, mchid: mchid,
		splitMchid: strings.TrimSpace(c.Config["splitmchid"]),
		privateKey: key, platformKey: platformKey, gateway: lakalamossGateway,
	}, nil
}

func lakalamossPlatformKey() (*rsa.PublicKey, error) {
	der, e := base64.StdEncoding.DecodeString(lakalamossPlatformPubB64)
	if e != nil {
		return nil, ErrInvalid
	}
	parsed, e := x509.ParsePKIXPublicKey(der)
	if e != nil {
		return nil, ErrInvalid
	}
	key, ok := parsed.(*rsa.PublicKey)
	if !ok || key.N.BitLen() < pluginMinRSABits {
		return nil, ErrInvalid
	}
	return key, nil
}

func (p *lakalamossPlugin) methods() []string {
	// 主扫 (native QR) for all types; JSAPI where the original offers it.
	return []string{"native", "jsapi"}
}

func (p *lakalamossPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

func (p *lakalamossPlugin) ValidateCreate(r CreateRequest) error {
	if e := validatePluginCreate(r, p.methods()); e != nil {
		return e
	}
	if r.Method == "jsapi" && strings.TrimSpace(r.OpenID) == "" {
		return ErrInvalid
	}
	return nil
}

func (p *lakalamossPlugin) accountType() string {
	switch p.payType {
	case "alipay":
		return "ALIPAY"
	case "wxpay":
		return "WECHAT"
	case "bank":
		return "UQRCODEPAY"
	}
	return ""
}

// lakalamossSignContent mirrors MossClient::getSignContent: sort keys, skip
// "sign" and nil values; a nested object is ksort'd and JSON-encoded
// (UNESCAPED_SLASHES|UNESCAPED_UNICODE); join "k=v" with "&".
func lakalamossSignContent(params map[string]any) (string, error) {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "sign" || params[k] == nil {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		v := params[k]
		var val string
		switch vv := v.(type) {
		case string:
			val = vv
		case map[string]any:
			enc, e := lakalamossEncodeSorted(vv)
			if e != nil {
				return "", e
			}
			val = enc
		default:
			raw, e := lakalamossMarshal(vv)
			if e != nil {
				return "", e
			}
			val = string(raw)
		}
		if sb.Len() > 0 {
			sb.WriteByte('&')
		}
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(val)
	}
	return sb.String(), nil
}

// lakalamossEncodeSorted JSON-encodes a nested object with its keys sorted,
// matching PHP's ksort()+json_encode (slashes and unicode unescaped — Go's
// encoder already leaves slashes alone and emits UTF-8, but escapes <>& which
// PHP leaves; to stay byte-identical we disable HTML escaping).
func lakalamossEncodeSorted(m map[string]any) (string, error) {
	raw, e := lakalamossMarshal(m)
	if e != nil {
		return "", e
	}
	return string(raw), nil
}

// lakalamossMarshal marshals with HTML escaping off and map keys sorted (Go's
// encoding/json already sorts map keys), matching PHP JSON_UNESCAPED_SLASHES|
// JSON_UNESCAPED_UNICODE.
func lakalamossMarshal(v any) ([]byte, error) {
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	if e := enc.Encode(v); e != nil {
		return nil, e
	}
	// Encoder appends a trailing newline.
	return []byte(strings.TrimSuffix(sb.String(), "\n")), nil
}

// sign RSA-SHA1-signs the content with the merchant private key, base64.
func (p *lakalamossPlugin) sign(content string) (string, error) {
	h := sha1.Sum([]byte(content))
	sig, e := rsa.SignPKCS1v15(rand.Reader, p.privateKey, crypto.SHA1, h[:])
	if e != nil {
		return "", e
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

// verify RSA-SHA1-verifies content against a base64 signature with the platform
// public key.
func (p *lakalamossPlugin) verify(content, signature string) bool {
	sig, e := base64.StdEncoding.DecodeString(signature)
	if e != nil {
		return false
	}
	h := sha1.Sum([]byte(content))
	return rsa.VerifyPKCS1v15(p.platformKey, crypto.SHA1, h[:], sig) == nil
}

// encryptRequest RSA/PKCS1-encrypts the request JSON in chunks of (bits/8 - 11)
// with the platform public key, concatenates the ciphertext, base64.
func (p *lakalamossPlugin) encryptRequest(data []byte) (string, error) {
	chunk := p.platformKey.Size() - 11
	if chunk <= 0 {
		return "", ErrInvalid
	}
	var out []byte
	for len(data) > 0 {
		n := chunk
		if n > len(data) {
			n = len(data)
		}
		enc, e := rsa.EncryptPKCS1v15(rand.Reader, p.platformKey, data[:n])
		if e != nil {
			return "", e
		}
		out = append(out, enc...)
		data = data[n:]
	}
	return base64.StdEncoding.EncodeToString(out), nil
}

// decryptResponse base64-decodes then RSA/PKCS1-decrypts in chunks of bits/8
// with the merchant private key.
func (p *lakalamossPlugin) decryptResponse(encoded string) ([]byte, error) {
	raw, e := base64.StdEncoding.DecodeString(encoded)
	if e != nil {
		return nil, ErrInvalid
	}
	size := p.privateKey.Size()
	if size <= 0 || len(raw)%size != 0 {
		return nil, ErrInvalid
	}
	var out []byte
	for len(raw) > 0 {
		dec, e := rsa.DecryptPKCS1v15(rand.Reader, p.privateKey, raw[:size])
		if e != nil {
			return nil, ErrInvalid
		}
		out = append(out, dec...)
		raw = raw[size:]
	}
	return out, nil
}

// execute performs one MossClient::execute round trip and returns the decrypted
// response object.
func (p *lakalamossPlugin) execute(ctx context.Context, operation, service string, data map[string]any) (map[string]any, string, error) {
	reqRaw, e := lakalamossMarshal(data)
	if e != nil {
		return nil, "", ErrInvalid
	}
	enc, e := p.encryptRequest(reqRaw)
	if e != nil {
		return nil, "", e
	}
	params := map[string]any{
		"head": map[string]any{
			"versionId":       "1.0",
			"serviceId":       service,
			"channelId":       "API",
			"requestTime":     time.Now().Format("20060102150405"),
			"serviceSn":       lakalamossSn(),
			"businessChannel": p.appid,
		},
		"requestEncrypted": enc,
	}
	content, e := lakalamossSignContent(params)
	if e != nil {
		return nil, "", e
	}
	sig, e := p.sign(content)
	if e != nil {
		return nil, "", e
	}
	params["sign"] = sig
	body, e := lakalamossMarshal(params)
	if e != nil {
		return nil, "", ErrInvalid
	}
	raw, e := p.pluginCall(ctx, operation, http.MethodPost, p.gateway, body, "application/json; charset=utf-8", nil)
	if e != nil {
		return nil, "", e
	}
	result, e := callbackJSON(raw)
	if e != nil {
		return nil, "", &UnknownOutcome{Operation: operation, Cause: e}
	}
	head, _ := result["head"].(map[string]any)
	code := ""
	if head != nil {
		code = jsonText(head, "code")
	}
	if code != "000000" {
		if code == "" {
			return nil, "", &UnknownOutcome{Operation: operation, Cause: ErrInvalid}
		}
		return nil, code, rejected("MOSS_" + code)
	}
	// Verify the response signature then decrypt responseEncrypted.
	respContent, e := lakalamossSignContent(result)
	if e != nil {
		return nil, "", &UnknownOutcome{Operation: operation, Cause: e}
	}
	if !p.verify(respContent, jsonText(result, "sign")) {
		return nil, "", ErrAuthentication
	}
	plain, e := p.decryptResponse(jsonText(result, "responseEncrypted"))
	if e != nil {
		return nil, "", &UnknownOutcome{Operation: operation, Cause: e}
	}
	out, e := callbackJSON(plain)
	if e != nil {
		return nil, "", &UnknownOutcome{Operation: operation, Cause: e}
	}
	return out, code, nil
}

func (p *lakalamossPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	// addOrder: pay_scene '1' with trans_type (41 QR / 51 jsapi); account_type
	// per pay type. WeChat native in the original uses pay_scene '0' (hosted
	// counter) but for a channel-level QR we send pay_scene '1' trans_type 41
	// uniformly, which is the normal 主扫 order shape.
	transType := "41"
	payScene := "1"
	data := map[string]any{
		"mer_no":        p.mchid,
		"order_no":      r.TradeNo,
		"total_amount":  strconv.FormatInt(r.Amount, 10),
		"pay_scene":     payScene,
		"account_type":  p.accountType(),
		"subject":       r.Subject,
		"notify_url":    r.NotifyURL,
		"trans_type":    transType,
		"location_info": map[string]any{"request_ip": r.ClientIP},
	}
	if r.Method == "jsapi" {
		data["trans_type"] = "51"
		data["user_id"] = r.OpenID
	}
	if p.splitMchid != "" {
		data["split_info"] = []any{
			map[string]any{"mer_no": p.splitMchid, "amount": strconv.FormatInt(r.Amount-1, 10)},
			map[string]any{"mer_no": p.mchid, "amount": "1"},
		}
	}
	out, _, e := p.execute(ctx, "lakalamoss.pay", "lfops.moss.order.pay", data)
	if e != nil {
		return CreateResult{}, e
	}
	providerTradeNo := jsonText(out, "pay_serial")
	resp, _ := out["acc_resp_fields"].(map[string]any)
	if r.Method == "jsapi" {
		if resp == nil {
			return CreateResult{}, rejected("MOSS_NO_PAYINFO")
		}
		payload, e := lakalamossMarshal(resp)
		if e != nil {
			return CreateResult{}, &UnknownOutcome{Operation: "lakalamoss.pay", Cause: e}
		}
		return CreateResult{MaterialType: "jsapi", Material: string(payload), ProviderTradeNo: providerTradeNo}, nil
	}
	if resp == nil {
		return CreateResult{}, rejected("MOSS_NO_QRCODE")
	}
	qr := jsonText(resp, "code")
	if qr == "" {
		return CreateResult{}, rejected("MOSS_NO_QRCODE")
	}
	return CreateResult{MaterialType: "qrcode", Material: qr, ProviderTradeNo: providerTradeNo}, nil
}

// Refund posts lfops.moss.order.ref.
func (p *lakalamossPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	data := map[string]any{
		"order_no":        r.RefundNo,
		"origin_order_no": r.TradeNo,
		"refund_amount":   strconv.FormatInt(r.Amount, 10),
		"refund_reason":   "订单退款",
		"location_info":   map[string]any{"request_ip": "127.0.0.1"},
	}
	_, _, e := p.execute(ctx, "lakalamoss.refund", "lfops.moss.order.ref", data)
	if e != nil {
		return RefundResult{}, e
	}
	return RefundResult{RefundNo: r.RefundNo, State: "SUCCESS", Amount: r.Amount}, nil
}

// VerifyNotification: the notify body is the same envelope. Verify the sign over
// the whole object with the platform key, decrypt requestEncrypted, then check
// trade_state SUCCESS plus our order and amount.
func (p *lakalamossPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	arr, e := callbackJSON(raw)
	if e != nil {
		return PaymentEvent{}, ErrInvalid
	}
	content, e := lakalamossSignContent(arr)
	if e != nil {
		return PaymentEvent{}, ErrInvalid
	}
	if !p.verify(content, jsonText(arr, "sign")) {
		return PaymentEvent{}, ErrAuthentication
	}
	plain, e := p.decryptResponse(jsonText(arr, "requestEncrypted"))
	if e != nil {
		return PaymentEvent{}, ErrAuthentication
	}
	data, e := callbackJSON(plain)
	if e != nil {
		return PaymentEvent{}, ErrInvalid
	}
	if jsonText(data, "trade_state") != "SUCCESS" {
		return PaymentEvent{}, ErrInvalid
	}
	if jsonText(data, "order_no") != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	amount, ae := parseFen(jsonText(data, "total_amount"))
	if ae != nil || expected.Amount <= 0 || amount != expected.Amount {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, jsonText(data, "pay_serial"), jsonText(data, "user_id2"), amount), nil
}

// NotificationResponse echoes the callback head back, signed, exactly like
// the original MossClient::echoNotify: head{code 000000|000001, desc,
// serviceTime, serviceSn, serviceId} plus sign over the sorted content. The
// failure text never carries internal error details.
func (p *lakalamossPlugin) NotificationResponse(_ http.Header, raw []byte, verifyErr error) (int, string, string) {
	arr, e := callbackJSON(raw)
	if e != nil {
		return 0, "", ""
	}
	head, _ := arr["head"].(map[string]any)
	sn, id := jsonText(head, "serviceSn"), jsonText(head, "serviceId")
	if head == nil || !lakalamossEchoField(sn) || !lakalamossEchoField(id) {
		return 0, "", ""
	}
	code, desc := "000000", "success"
	if verifyErr != nil {
		code, desc = "000001", "failure"
	}
	params := map[string]any{"head": map[string]any{"code": code, "desc": desc, "serviceTime": lakalamossNow().In(lakalamossZone).Format("20060102150405"), "serviceSn": sn, "serviceId": id}}
	content, e := lakalamossSignContent(params)
	if e != nil {
		return 0, "", ""
	}
	sig, e := p.sign(content)
	if e != nil {
		return 0, "", ""
	}
	params["sign"] = sig
	body, e := lakalamossMarshal(params)
	if e != nil {
		return 0, "", ""
	}
	status := 200
	if verifyErr != nil {
		status = 400
	}
	return status, "application/json; charset=utf-8", string(body)
}

// lakalamossEchoField limits what is echoed back to plain identifiers.
func lakalamossEchoField(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c == '-' || c == '.') {
			return false
		}
	}
	return true
}

var lakalamossNow = time.Now
var lakalamossZone = time.FixedZone("CST", 8*3600)

// lakalamossSn returns a time-based serviceSn (getSid() analogue).
func lakalamossSn() string {
	return strconv.FormatInt(time.Now().UnixNano(), 10)
}

var _ Provider = (*lakalamossPlugin)(nil)
var _ NotificationVerifier = (*lakalamossPlugin)(nil)
var _ CreateValidator = (*lakalamossPlugin)(nil)
