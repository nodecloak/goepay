package payment

// Port of epay_release/plugins/suixingpay (随行付, 天阙 openapi). JSON gateway
// https://openapi.tianquetech.com. Every request is an envelope
// {orgId,reqId,reqData,timestamp,version,signType,sign}; reqData is the JSON
// object of business fields. The sign (Suixingpay::getSignContent) is the
// ksort-joined "k=v&…" of the envelope — arrays/objects json-encoded with
// unescaped slashes+unicode — signed RSA-SHA1 (PHP openssl_sign default) with
// the merchant private key (appsecret) and base64-encoded. Replies
// {code,respData,msg,sign} are verified RSA-SHA1 with the platform public key
// (appkey) over the same envelope shape; respData is verified against the exact
// bytes the upstream sent (not a re-encode). bizCode 0000 is success.
//
// Implemented: native QR (/order/activeScan) for alipay/wxpay/bank, and alipay
// JSAPI (/order/jsapiScan, payWay 02) which only needs the payer uid in
// r.OpenID. Refund is /order/refund.
//
// Out of scope (per wave brief): WeChat/UnionPay JSAPI and 小程序 (need a bound
// WeChat sub_appid / openid from OAuth the platform performs), profit sharing
// (fusruleId), merchant onboarding (applynotify), and order query (the SDK has
// no query service).

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "suixingpay",
		Title:  "随行付",
		Author: "随行付",
		Link:   "https://www.suixingpay.com/",
		Types:  []string{"alipay", "wxpay", "bank"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "机构编号", Required: true},
			{Key: "appkey", Label: "平台公钥", PEM: true, Required: true},
			{Key: "appsecret", Label: "商户私钥", PEM: true, Secret: true, Required: true},
			{Key: "appmchid", Label: "商户编号", Required: true},
		},
		Build: newSuixingpayPlugin,
	})
}

const (
	suixingpayGateway = "https://openapi.tianquetech.com"
	suixingpayVersion = "1.0"
)

var (
	errSuixingpaySign   = errors.New("suixingpay: reply signature invalid")
	errSuixingpayNoLink = errors.New("suixingpay: reply missing pay link")
)

type suixingpayPlugin struct {
	pluginBase
	orgID, mno string
	gateway    string
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
}

// hostOverride points the plugin at a test server (test seam only).
func (p *suixingpayPlugin) hostOverride(base string) { p.gateway = base }

func newSuixingpayPlugin(c PluginChannel) (Provider, error) {
	org := strings.TrimSpace(c.Config["appid"])
	mno := strings.TrimSpace(c.Config["appmchid"])
	if org == "" || len(org) > 64 || mno == "" || len(mno) > 64 {
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
	host, _ := gatewayHost(suixingpayGateway)
	base, e := newPluginBase(c, pluginAccount(host, org, mno))
	if e != nil {
		return nil, e
	}
	return &suixingpayPlugin{pluginBase: base, orgID: org, mno: mno, gateway: suixingpayGateway, privateKey: priv, publicKey: pub}, nil
}

func (p *suixingpayPlugin) payTypeCode() string {
	switch p.payType {
	case "alipay":
		return "ALIPAY"
	case "wxpay":
		return "WECHAT"
	case "bank":
		return "UNIONPAY"
	}
	return ""
}

func (p *suixingpayPlugin) methods() []string {
	// alipay JSAPI needs only the payer uid (r.OpenID); WeChat/UnionPay JSAPI
	// need a bound sub_appid the platform supplies, so they stay native-only.
	if p.payType == "alipay" {
		return []string{"native", "jsapi"}
	}
	return []string{"native"}
}

func (p *suixingpayPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

func (p *suixingpayPlugin) ValidateCreate(r CreateRequest) error {
	if e := validatePluginCreate(r, p.methods()); e != nil {
		return e
	}
	if r.Method == "jsapi" && strings.TrimSpace(r.OpenID) == "" {
		return ErrInvalid
	}
	return nil
}

func (p *suixingpayPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	if r.Method == "jsapi" {
		return p.createJSAPI(ctx, r)
	}
	return p.createNative(ctx, r)
}

func (p *suixingpayPlugin) createNative(ctx context.Context, r CreateRequest) (CreateResult, error) {
	reqData := map[string]string{
		"mno":       p.mno,
		"ordNo":     r.TradeNo,
		"amt":       yuan(r.Amount),
		"payType":   p.payTypeCode(),
		"subject":   r.Subject,
		"trmIp":     r.ClientIP,
		"notifyUrl": r.NotifyURL,
	}
	reply, e := p.request(ctx, "suixingpay.native", "/order/activeScan", reqData)
	if e != nil {
		return CreateResult{}, e
	}
	res, ok := materialFromLinks("", jsonText(reply, "payUrl"), "")
	if !ok {
		return CreateResult{}, &UnknownOutcome{Operation: "suixingpay.native", Cause: errSuixingpayNoLink}
	}
	res.ProviderTradeNo = jsonText(reply, "transactionId")
	return res, nil
}

func (p *suixingpayPlugin) createJSAPI(ctx context.Context, r CreateRequest) (CreateResult, error) {
	reqData := map[string]string{
		"mno":       p.mno,
		"ordNo":     r.TradeNo,
		"amt":       yuan(r.Amount),
		"payType":   "ALIPAY",
		"payWay":    "02",
		"subject":   r.Subject,
		"trmIp":     r.ClientIP,
		"userId":    r.OpenID,
		"notifyUrl": r.NotifyURL,
	}
	reply, e := p.request(ctx, "suixingpay.jsapi", "/order/jsapiScan", reqData)
	if e != nil {
		return CreateResult{}, e
	}
	// For ALIPAY the SDK returns result['source'] as the JSAPI parameters.
	source := jsonText(reply, "source")
	if source == "" {
		return CreateResult{}, &UnknownOutcome{Operation: "suixingpay.jsapi", Cause: errSuixingpayNoLink}
	}
	return CreateResult{MaterialType: "jsapi", Material: source, ProviderTradeNo: jsonText(reply, "transactionId")}, nil
}

// request builds the signed envelope, posts it and verifies+unwraps respData.
func (p *suixingpayPlugin) request(ctx context.Context, op, path string, reqData map[string]string) (map[string]any, error) {
	reqDataJSON := suixingpayCompactJSON(reqData)
	env := map[string]string{
		"orgId":     p.orgID,
		"reqId":     suixingpayReqID(),
		"reqData":   reqDataJSON,
		"timestamp": time.Now().Format("20060102150405"),
		"version":   suixingpayVersion,
		"signType":  "RSA",
	}
	sig, e := rsaSign(p.privateKey, crypto.SHA1, suixingpaySignContent(env))
	if e != nil {
		return nil, e
	}
	// Assemble the JSON body preserving reqData as an embedded object.
	body := fmt.Sprintf(
		`{"orgId":%s,"reqId":%s,"reqData":%s,"timestamp":%s,"version":%s,"signType":%s,"sign":%s}`,
		suixingpayQuote(env["orgId"]), suixingpayQuote(env["reqId"]), reqDataJSON,
		suixingpayQuote(env["timestamp"]), suixingpayQuote(env["version"]),
		suixingpayQuote(env["signType"]), suixingpayQuote(sig),
	)
	raw, e := p.pluginCall(ctx, op, http.MethodPost, p.gateway+path, []byte(body), "application/json; charset=utf-8", nil)
	if e != nil {
		return nil, e
	}
	return p.verifyReply(op, raw)
}

// verifyReply checks the top-level RSA-SHA1 signature over the envelope shape,
// verifying respData against the exact bytes received, then returns respData.
func (p *suixingpayPlugin) verifyReply(op string, raw []byte) (map[string]any, error) {
	top, e := callbackJSON(raw)
	if e != nil {
		return nil, &UnknownOutcome{Operation: op, Cause: e}
	}
	sign := jsonText(top, "sign")
	if sign == "" {
		return nil, &UnknownOutcome{Operation: op, Cause: errSuixingpaySign}
	}
	content := suixingpayReplySignContent(raw)
	if !rsaVerify(p.publicKey, crypto.SHA1, content, sign) {
		return nil, &UnknownOutcome{Operation: op, Cause: errSuixingpaySign}
	}
	if jsonText(top, "code") != "0000" {
		return nil, rejected("SXF_" + suixingpayFirst(jsonText(top, "code"), "ERR"))
	}
	respData, ok := top["respData"].(map[string]any)
	if !ok {
		return nil, &UnknownOutcome{Operation: op, Cause: errors.New("suixingpay: respData missing")}
	}
	if jsonText(respData, "bizCode") != "0000" {
		return nil, rejected("SXF_" + suixingpayFirst(jsonText(respData, "bizCode"), "BIZ"))
	}
	return respData, nil
}

func (p *suixingpayPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	top, e := callbackJSON(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	sign := jsonText(top, "sign")
	if sign == "" || !rsaVerify(p.publicKey, crypto.SHA1, suixingpayNotifySignContent(raw), sign) {
		return PaymentEvent{}, ErrAuthentication
	}
	if jsonText(top, "bizCode") != "0000" {
		return PaymentEvent{}, ErrInvalid
	}
	amount, e := parseYuan(jsonText(top, "amt"))
	if e != nil || expected.Amount <= 0 || amount != expected.Amount || jsonText(top, "ordNo") != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, jsonText(top, "sxfUuid"), jsonText(top, "buyerId"), amount), nil
}

// The original echoes a JSON ack; success and failure differ only by content.
func (p *suixingpayPlugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return http.StatusOK, "application/json; charset=utf-8", `{"code":"fail","msg":"签名错误"}`
	}
	return http.StatusOK, "application/json; charset=utf-8", `{"code":"success","msg":"成功"}`
}

func (p *suixingpayPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	reqData := map[string]string{
		"mno":         p.mno,
		"ordNo":       r.RefundNo,
		"origOrderNo": r.TradeNo,
		"amt":         yuan(r.Amount),
	}
	reply, e := p.request(ctx, "suixingpay.refund", "/order/refund", reqData)
	if e != nil {
		return RefundResult{}, e
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(reply, "origOrderNo"), State: "SUCCESS", Amount: r.Amount}, nil
}

// ---- helpers (suixingpay-local) ----

// suixingpaySignContent is Suixingpay::getSignContent over a map whose values
// are already strings (reqData is pre-serialized JSON text). ksort, "k=v&…",
// skip "sign".
func suixingpaySignContent(env map[string]string) string {
	keys := make([]string, 0, len(env))
	for k := range env {
		if k == "sign" {
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
		b.WriteString(k + "=" + env[k])
	}
	return b.String()
}

// suixingpayReplySignContent reconstructs the sign base for a reply
// {code,respData,msg,sign}: ksort over the top-level fields, with respData
// rendered as the EXACT JSON text the upstream sent, not a re-encode.
func suixingpayReplySignContent(raw []byte) string {
	return suixingpayEnvelopeSignContent(raw, map[string]bool{"respData": true})
}

// suixingpayNotifySignContent: the notify body is a flat object; ksort "k=v&…"
// over every field but "sign", values taken from their raw JSON text.
func suixingpayNotifySignContent(raw []byte) string {
	return suixingpayEnvelopeSignContent(raw, nil)
}

// suixingpayEnvelopeSignContent builds the "k=v&…" sign base from the raw JSON
// bytes, preserving the exact textual form of each value (objects/arrays kept
// verbatim-compacted, strings unquoted). ksort reorders keys lexically as PHP
// does. objectFields is retained for API symmetry but object detection is
// automatic from the raw value's first byte.
func suixingpayEnvelopeSignContent(raw []byte, _ map[string]bool) string {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return ""
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		if k == "sign" {
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
		b.WriteString(k + "=" + suixingpayRawValue(fields[k]))
	}
	return b.String()
}

// suixingpayRawValue renders a raw JSON value as PHP places it in the sign
// string: a JSON string becomes its unquoted content; an object/array keeps its
// compact JSON text (whitespace removed, slashes unescaped), matching PHP
// json_encode(JSON_UNESCAPED_SLASHES|JSON_UNESCAPED_UNICODE); null becomes "".
func suixingpayRawValue(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return ""
	}
	switch trimmed[0] {
	case '"':
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return s
		}
		return trimmed
	case '{', '[':
		return suixingpayCanonicalJSON(trimmed)
	default:
		return trimmed
	}
}

// suixingpayCanonicalJSON compacts a JSON fragment (preserving key order from
// the input bytes) and unescapes slashes to match PHP json_encode with
// JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE.
func suixingpayCanonicalJSON(fragment string) string {
	var buf bytes.Buffer
	if json.Compact(&buf, []byte(fragment)) != nil {
		return strings.ReplaceAll(fragment, "\\/", "/")
	}
	return strings.ReplaceAll(buf.String(), "\\/", "/")
}

// suixingpayCompactJSON serializes a flat string map to compact JSON with
// sorted keys, unescaped slashes/unicode (for the request reqData we build).
func suixingpayCompactJSON(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(suixingpayQuote(k) + ":" + suixingpayQuote(m[k]))
	}
	b.WriteByte('}')
	return b.String()
}

// suixingpayQuote JSON-quotes a string with unescaped slashes and unicode
// (matching JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE).
func suixingpayQuote(s string) string {
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
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func suixingpayReqID() string {
	var x [12]byte
	_, _ = rand.Read(x[:])
	return hex.EncodeToString(x[:])
}

func suixingpayFirst(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

var _ Provider = (*suixingpayPlugin)(nil)
var _ NotificationVerifier = (*suixingpayPlugin)(nil)
var _ CreateValidator = (*suixingpayPlugin)(nil)
var _ NotificationAcker = (*suixingpayPlugin)(nil)
