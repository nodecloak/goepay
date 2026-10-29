package payment

// Port of epay_release/plugins/hlpay (汇联支付): the 汇联 open API
// (https://api.huilianlink.com). JSON with an RSA2 (SHA256) signature: the
// outer envelope {appId,subSn,timestamp,requestId,version,signType,bizContent}
// is ksort'ed into "k=v&..." of its non-empty fields, signed with the merchant
// private key; the reply {code:200,data,sign} and the async callback are
// verified with the platform public key over the SAME envelope, EXCEPT the
// "data" field is first canonicalised (parse JSON, drop empties, deep-sort
// keys, compact re-encode) before being put back into the sign base. create
// returns payInfo (QR native / page+wap jump / jsapi). refund is
// /openapi/pay/refund. Callback data.state=="3" is paid; the original echoes
// "success".
//
// Out of scope: transfers (transfer/transfer_query/balance_query) and their
// /openapi/payment/* endpoints.

import (
	"context"
	"crypto"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:          "hlpay",
		Title:         "汇联支付",
		Author:        "汇联",
		Link:          "https://www.huilianlink.com/",
		Types:         []string{"alipay", "wxpay", "bank"},
		TransferTypes: []string{"alipay", "wxpay"}, // declared by original; not implemented this wave
		Inputs: []PluginInput{
			{Key: "appid", Label: "应用APPID", Required: true},
			{Key: "appkey", Label: "商户私钥", Textarea: true, Secret: true, Required: true},
			{Key: "appsecret", Label: "平台公钥", Textarea: true, Required: true},
			{Key: "channelcode", Label: "通道编码", Note: "可留空，留空为随机路由"},
			{Key: "appmchid", Label: "子商户编码", Note: "仅服务商可传"},
			{Key: "appswitch", Label: "场景类型", Options: []PluginOption{{Value: "1", Label: "线下"}, {Value: "2", Label: "线上"}}},
		},
		// select_alipay: 1=扫码, 2=JS, 3=PC, 4=H5. select_wxpay: 1=扫码, 2=公众号/小程序.
		AppTypes: []PluginOption{{Value: "1", Label: "扫码支付"}, {Value: "2", Label: "JS/公众号支付"}, {Value: "3", Label: "PC支付"}, {Value: "4", Label: "H5支付"}},
		Build:    newHlpayPlugin,
	})
}

const hlpayHost = "https://api.huilianlink.com"

type hlpayPlugin struct {
	pluginBase
	appID, subSn, channelCode, scene string
	priv                             *rsa.PrivateKey
	pub                              *rsa.PublicKey
	appTypes                         []string
	testHost                         string // set only in tests to redirect the gateway
}

func (p *hlpayPlugin) hlpayBase() string {
	if p.testHost != "" {
		return p.testHost
	}
	return hlpayHost
}

func newHlpayPlugin(c PluginChannel) (Provider, error) {
	appID := strings.TrimSpace(c.Config["appid"])
	if appID == "" || len(appID) > 64 {
		return nil, ErrInvalid
	}
	priv, e := pluginPrivateKey(c.Config["appkey"])
	if e != nil {
		return nil, ErrInvalid
	}
	pub, e := pluginPublicKey(c.Config["appsecret"])
	if e != nil {
		return nil, ErrInvalid
	}
	scene := strings.TrimSpace(c.Config["appswitch"])
	if scene == "" {
		scene = "2"
	}
	subSn := strings.TrimSpace(c.Config["appmchid"])
	channelCode := strings.TrimSpace(c.Config["channelcode"])
	if len(subSn) > 64 || len(channelCode) > 64 {
		return nil, ErrInvalid
	}
	base, e := newPluginBase(c, pluginAccount(hlpayHost, appID, subSn))
	if e != nil {
		return nil, e
	}
	return &hlpayPlugin{pluginBase: base, appID: appID, subSn: subSn, channelCode: channelCode, scene: scene, priv: priv, pub: pub, appTypes: append([]string(nil), c.AppTypes...)}, nil
}

func (p *hlpayPlugin) hlpayHasApp(v string) bool {
	for _, a := range p.appTypes {
		if a == v {
			return true
		}
	}
	return false
}

func (p *hlpayPlugin) methods() []string {
	if p.payType == "alipay" {
		var m []string
		if p.hlpayHasApp("3") {
			m = append(m, "page") // PC
		}
		if p.hlpayHasApp("1") {
			m = append(m, "native")
		}
		if p.hlpayHasApp("2") {
			m = append(m, "jsapi")
		}
		if p.hlpayHasApp("4") {
			m = append(m, "wap") // H5
		}
		if len(m) == 0 {
			m = []string{"native"}
		}
		return m
	}
	if p.payType == "wxpay" {
		var m []string
		if p.hlpayHasApp("2") && !p.hlpayHasApp("1") {
			m = append(m, "jsapi")
		} else {
			m = append(m, "native")
			if p.hlpayHasApp("2") {
				m = append(m, "jsapi")
			}
		}
		return m
	}
	return []string{"native"} // UNION_PAY
}

func (p *hlpayPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

// hlpaySignContent is the original getSignContent: ksort, skip sign and empty,
// join "k=v&".
func hlpaySignContent(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if k == "sign" || v == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte('&')
		}
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(params[k])
	}
	return sb.String()
}

func (p *hlpayPlugin) hlpaySign(params map[string]string) (string, error) {
	return rsaSign(p.priv, crypto.SHA256, hlpaySignContent(params))
}

// hlpayExecute builds the signed envelope, posts it, verifies the reply sign
// and returns the decoded data object. result code 200 is success.
func (p *hlpayPlugin) hlpayExecute(ctx context.Context, operation, path string, biz map[string]any) (map[string]any, error) {
	bizJSON, e := hlpayCompactJSON(biz)
	if e != nil {
		return nil, ErrInvalid
	}
	env := map[string]string{
		"appId":      p.appID,
		"subSn":      p.subSn,
		"timestamp":  strconv.FormatInt(time.Now().Unix(), 10),
		"requestId":  hlpayRequestID(),
		"version":    "1.01",
		"signType":   "RSA2",
		"bizContent": bizJSON,
	}
	sign, e := p.hlpaySign(env)
	if e != nil {
		return nil, e
	}
	env["sign"] = sign
	raw, e := p.postJSON(ctx, operation, p.hlpayBase()+path, env, map[string]string{"Content-Type": "application/json"})
	if e != nil {
		return nil, e
	}
	reply, e := callbackJSON(raw)
	if e != nil {
		return nil, &UnknownOutcome{Operation: operation, Cause: e}
	}
	if jsonText(reply, "code") != "200" {
		if jsonText(reply, "msg") != "" {
			return nil, rejected("HLPAY_" + jsonText(reply, "code"))
		}
		return nil, &UnknownOutcome{Operation: operation, Cause: errHlpayBadReply}
	}
	if !p.hlpayVerifyReply(reply) {
		return nil, ErrAuthentication
	}
	data, _ := reply["data"].(map[string]any)
	if data == nil {
		// data may be absent on some ok replies; treat as unknown rather than crash.
		return map[string]any{}, nil
	}
	return data, nil
}

// hlpayVerifyReply recomputes the envelope sign over the reply, canonicalising
// the "data" field the way the original processDataField does.
func (p *hlpayPlugin) hlpayVerifyReply(reply map[string]any) bool {
	sign := jsonText(reply, "sign")
	if sign == "" {
		return false
	}
	base := map[string]string{}
	for k, v := range reply {
		if k == "sign" {
			continue
		}
		if k == "data" {
			base[k] = hlpayCanonicalData(v)
			continue
		}
		base[k] = hlpayScalar(v)
	}
	return rsaVerify(p.pub, crypto.SHA256, hlpaySignContent(base), sign)
}

func (p *hlpayPlugin) ValidateCreate(r CreateRequest) error {
	if e := validatePluginCreate(r, p.methods()); e != nil {
		return e
	}
	if r.Method == "jsapi" && r.OpenID == "" {
		return ErrInvalid
	}
	return nil
}

func (p *hlpayPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	var payType, subType string
	switch p.payType {
	case "alipay":
		payType = "ALIPAY"
	case "wxpay":
		payType = "WECHAT"
	default:
		payType = "UNION_PAY"
	}
	switch r.Method {
	case "native":
		subType = "NATIVE"
	case "page":
		subType = "PC"
	case "wap":
		subType = "H5"
	case "jsapi":
		subType = "JSAPI"
	}
	biz := map[string]any{
		"payType":     payType,
		"paySubType":  subType,
		"sceneType":   p.scene,
		"mchOrderNo":  r.TradeNo,
		"amount":      yuan(r.Amount),
		"clientIp":    r.ClientIP,
		"subject":     r.Subject,
		"notifyUrl":   r.NotifyURL,
		"redirectUrl": r.ReturnURL,
	}
	if p.channelCode != "" {
		biz["channelCode"] = p.channelCode
	}
	if r.Method == "jsapi" {
		extra := map[string]any{"userId": r.OpenID}
		if p.payType == "wxpay" {
			if i := strings.IndexByte(r.OpenID, ':'); i > 0 {
				extra["subAppid"] = r.OpenID[:i]
				extra["userId"] = r.OpenID[i+1:]
			}
		}
		biz["extra"] = extra
	}
	out, e := p.hlpayExecute(ctx, "hlpay.create", "/openapi/pay/create", biz)
	if e != nil {
		return CreateResult{}, e
	}
	info := jsonText(out, "payInfo")
	providerTrade := jsonText(out, "payOrderNo")
	if info == "" {
		return CreateResult{}, rejected("HLPAY_NO_PAY_INFO")
	}
	switch r.Method {
	case "native":
		if len(info) > 4096 || strings.ContainsAny(info, "\x00\r\n") {
			return CreateResult{}, rejected("HLPAY_BAD_QR")
		}
		return CreateResult{MaterialType: "qrcode", Material: info, ProviderTradeNo: providerTrade}, nil
	case "page", "wap":
		if !validPaymentURL(info) {
			return CreateResult{}, rejected("HLPAY_BAD_JUMP")
		}
		return CreateResult{MaterialType: "jump", Material: info, ProviderTradeNo: providerTrade}, nil
	default: // jsapi
		return CreateResult{MaterialType: "jsapi", Material: info, ProviderTradeNo: providerTrade}, nil
	}
}

func (p *hlpayPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	arr, e := callbackJSON(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	if !p.hlpayVerifyReply(arr) {
		return PaymentEvent{}, ErrAuthentication
	}
	data, _ := arr["data"].(map[string]any)
	if data == nil {
		return PaymentEvent{}, ErrInvalid
	}
	if jsonText(data, "state") != "3" {
		return PaymentEvent{}, ErrInvalid
	}
	amount, e := parseYuan(jsonText(data, "amount"))
	if e != nil || expected.Amount <= 0 || amount != expected.Amount || jsonText(data, "mchOrderNo") != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, jsonText(data, "payOrderNo"), "", amount), nil
}

// The original notify() echoes plain "success" for a verified paid callback.
func (p *hlpayPlugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return http.StatusOK, "text/plain; charset=utf-8", "sign fail"
	}
	return http.StatusOK, "text/plain; charset=utf-8", "success"
}

func (p *hlpayPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.ProviderTradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	biz := map[string]any{
		"payOrderNo":       r.ProviderTradeNo,
		"mchRefundOrderNo": r.RefundNo,
		"amount":           yuan(r.Amount),
	}
	out, e := p.hlpayExecute(ctx, "hlpay.refund", "/openapi/pay/refund", biz)
	if e != nil {
		return RefundResult{}, e
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(out, "instOrderNo"), State: "SUCCESS", Amount: r.Amount}, nil
}

var _ Provider = (*hlpayPlugin)(nil)
var _ NotificationVerifier = (*hlpayPlugin)(nil)
var _ CreateValidator = (*hlpayPlugin)(nil)
var _ NotificationAcker = (*hlpayPlugin)(nil)

var errHlpayBadReply = newHlpayError("hlpay: unexpected upstream reply")

func newHlpayError(s string) error { return &hlpayError{s} }

type hlpayError struct{ s string }

func (e *hlpayError) Error() string { return e.s }

// hlpayRequestID is the per-request millisecond id (original getMillisecond()).
func hlpayRequestID() string {
	return strconv.FormatInt(time.Now().UnixNano()/int64(time.Millisecond), 10)
}

// hlpayScalar renders a JSON scalar as the string that appeared on the wire
// for signing (json.Number keeps its literal, string as-is).
func hlpayScalar(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case bool:
		if t {
			return "true"
		}
		return "false"
	case nil:
		return ""
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

// hlpayCompactJSON marshals biz content exactly like PHP json_encode with
// JSON_UNESCAPED_UNICODE|JSON_UNESCAPED_SLASHES (no HTML escaping).
func hlpayCompactJSON(v any) (string, error) {
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	if e := enc.Encode(v); e != nil {
		return "", e
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

// hlpayCanonicalData reproduces processDataField: a string that is JSON is
// parsed; an object/array is deep-filtered of empty values, deep key-sorted,
// and re-encoded compact. A non-JSON scalar string is returned as-is.
func hlpayCanonicalData(v any) string {
	switch t := v.(type) {
	case string:
		trimmed := strings.TrimSpace(t)
		if trimmed == "" {
			return t
		}
		var parsed any
		dec := json.NewDecoder(strings.NewReader(trimmed))
		dec.UseNumber()
		if dec.Decode(&parsed) != nil {
			return t // not JSON; original returns the string unchanged
		}
		cleaned, ok := hlpayClean(parsed)
		if !ok {
			return t
		}
		out, _ := hlpayCompactJSON(cleaned)
		return out
	default:
		cleaned, ok := hlpayClean(v)
		if !ok {
			return hlpayScalar(v)
		}
		out, _ := hlpayCompactJSON(cleaned)
		return out
	}
}

// hlpayClean deep-filters empty values and deep-sorts object keys; it returns
// ok=false when the value is not a container (so the caller keeps the scalar).
// Maps are returned as *hlpaySortedMap so compact encoding preserves key order.
func hlpayClean(v any) (any, bool) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		m := &hlpaySortedMap{}
		for _, k := range keys {
			child := t[k]
			if hlpayIsEmpty(child) {
				continue
			}
			if cleaned, ok := hlpayClean(child); ok {
				if hlpayContainerEmpty(cleaned) {
					continue
				}
				m.keys = append(m.keys, k)
				m.vals = append(m.vals, cleaned)
			} else {
				m.keys = append(m.keys, k)
				m.vals = append(m.vals, child)
			}
		}
		return m, true
	case []any:
		var out []any
		for _, e := range t {
			if hlpayIsEmpty(e) {
				continue
			}
			if cleaned, ok := hlpayClean(e); ok {
				if hlpayContainerEmpty(cleaned) {
					continue
				}
				out = append(out, cleaned)
			} else {
				out = append(out, e)
			}
		}
		return out, true
	default:
		return v, false
	}
}

func hlpayContainerEmpty(v any) bool {
	switch t := v.(type) {
	case *hlpaySortedMap:
		return len(t.keys) == 0
	case []any:
		return len(t) == 0
	}
	return false
}

func hlpayIsEmpty(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return t == ""
	}
	return false
}

// hlpaySortedMap marshals its entries in insertion (sorted) order as a JSON
// object, so the canonical data string is deterministic.
type hlpaySortedMap struct {
	keys []string
	vals []any
}

func (m *hlpaySortedMap) MarshalJSON() ([]byte, error) {
	var sb strings.Builder
	sb.WriteByte('{')
	for i, k := range m.keys {
		if i > 0 {
			sb.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		sb.Write(kb)
		sb.WriteByte(':')
		vs, e := hlpayCompactJSON(m.vals[i])
		if e != nil {
			return nil, e
		}
		sb.WriteString(vs)
	}
	sb.WriteByte('}')
	return []byte(sb.String()), nil
}
