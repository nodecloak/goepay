package payment

// Port of epay_release/plugins/huolian (火脸支付). Requests go to a fixed
// gateway https://open.lianok.com/open/v1/api/biz/do as a JSON envelope
// {authCode, requestTime, resource, versionNo, sign, params}. The sign is
// md5( lower( sorted "k=v&" over the envelope + flattened params, skipping
// sign ) + salt ). Collection products: unified (payUrl), native (qrCodeUrl)
// and h5 (payUrl) for alipay/wechat/cloud. Callbacks are a JSON object signed
// the same way with a nested respBody; the ack is "SUCCESS"/"FAIL". Refund
// (api.hl.order.refund.operation) needs the management password (appsecret).
// Applet/host/plugin wechat products and complaint callbacks are OUT OF SCOPE.

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"
)

const huolianGateway = "https://open.lianok.com/open/v1/api/biz/do"

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "huolian",
		Title:  "火脸支付",
		Author: "火脸",
		Link:   "https://www.lianok.com/",
		Types:  []string{"alipay", "wxpay", "bank"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "对接商授权编号", Required: true},
			{Key: "appkey", Label: "对接商MD5加密盐", Secret: true, Required: true},
			{Key: "appmchid", Label: "商户ID", Required: true},
			{Key: "appurl", Label: "收银员手机号", Required: true},
			{Key: "appsecret", Label: "退款密码（管理密码）", Secret: true, Note: "如不需要退款功能可留空"},
		},
		Build: newHuolianPlugin,
	})
}

type huolianPlugin struct {
	pluginBase
	authCode, salt, merchantNo, operator, refundPwd string
	gateway                                         string
}

func newHuolianPlugin(c PluginChannel) (Provider, error) {
	authCode := strings.TrimSpace(c.Config["appid"])
	salt := strings.TrimSpace(c.Config["appkey"])
	merchantNo := strings.TrimSpace(c.Config["appmchid"])
	operator := strings.TrimSpace(c.Config["appurl"])
	refundPwd := strings.TrimSpace(c.Config["appsecret"])
	if authCode == "" || len(authCode) > 64 || salt == "" || len(salt) > 128 ||
		merchantNo == "" || len(merchantNo) > 64 || operator == "" || len(operator) > 64 {
		return nil, ErrInvalid
	}
	base, e := newPluginBase(c, pluginAccount("https://open.lianok.com", merchantNo, authCode))
	if e != nil {
		return nil, e
	}
	return &huolianPlugin{pluginBase: base, authCode: authCode, salt: salt, merchantNo: merchantNo, operator: operator, refundPwd: refundPwd, gateway: huolianGateway}, nil
}

// payWay maps the GOEPAY pay type to the upstream payWay value.
func (p *huolianPlugin) payWay() string {
	switch p.payType {
	case "alipay":
		return "alipay"
	case "wxpay":
		return "wechat"
	case "bank":
		return "cloud"
	}
	return ""
}

func (p *huolianPlugin) methods() []string {
	// unified (jump/qr), native (qr) and h5 (jump) all serve native and h5.
	return []string{"native", "h5"}
}

func (p *huolianPlugin) Capabilities() Capabilities {
	c := Capabilities{Methods: p.methods()}
	c.Refund = p.refundPwd != ""
	return c
}

// sign reproduces HuolianClient::make_sign: ksort, join "k=v&" skipping sign
// and nil, lowercase the whole string, append the salt, md5 (lowercase hex).
func (p *huolianPlugin) sign(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if k == "sign" || v == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(params[k])
		b.WriteByte('&')
	}
	return md5Hex(strings.ToLower(b.String()) + p.salt)
}

func (p *huolianPlugin) ValidateCreate(r CreateRequest) error {
	if e := validatePluginCreate(r, p.methods()); e != nil {
		return e
	}
	if p.payWay() == "" {
		return ErrInvalid
	}
	return nil
}

func (p *huolianPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	// native -> api.hl.order.pay.native (qrCodeUrl); h5 -> api.hl.order.pay.h5
	// (payUrl). This matches the original's prepay()/h5pay() for a normal order.
	resource := "api.hl.order.pay.native"
	payParams := map[string]string{
		"businessOrderNo": r.TradeNo,
		"payAmount":       yuan(r.Amount),
		"merchantNo":      p.merchantNo,
		"operatorAccount": p.operator,
		"notifyUrl":       r.NotifyURL,
		"subject":         r.Subject,
		"payWay":          p.payWay(),
		"clientIp":        r.ClientIP,
	}
	if r.Method == "h5" {
		resource = "api.hl.order.pay.h5"
		if r.ReturnURL != "" {
			payParams["pageNotifyUrl"] = r.ReturnURL
		}
	}
	data, e := p.execute(ctx, "huolian.create", resource, payParams)
	if e != nil {
		return CreateResult{}, e
	}
	provider := jsonText(data, "orderNo")
	if r.Method == "h5" {
		if u := jsonText(data, "payUrl"); validPaymentURL(u) {
			return CreateResult{MaterialType: "jump", Material: u, ProviderTradeNo: provider}, nil
		}
		return CreateResult{}, rejected("HUOLIAN_NO_PAY_URL")
	}
	qr := jsonText(data, "qrCodeUrl")
	if qr == "" || len(qr) > 4096 || strings.ContainsAny(qr, "\x00\r\n") {
		return CreateResult{}, rejected("HUOLIAN_NO_QR")
	}
	return CreateResult{MaterialType: "qrcode", Material: qr, ProviderTradeNo: provider}, nil
}

// execute builds the signed envelope, posts it, verifies the envelope outcome
// (code==0 && status==200) and returns the decoded data object.
func (p *huolianPlugin) execute(ctx context.Context, operation, resource string, params map[string]string) (map[string]any, error) {
	envelope := map[string]string{
		"authCode":    p.authCode,
		"requestTime": time.Now().Format("20060102150405"),
		"resource":    resource,
		"versionNo":   "1",
	}
	// sign is over the envelope merged with the flattened params (both scalar).
	signInput := map[string]string{}
	for k, v := range envelope {
		signInput[k] = v
	}
	for k, v := range params {
		signInput[k] = v
	}
	sign := p.sign(signInput)
	body := map[string]any{}
	for k, v := range envelope {
		body[k] = v
	}
	body["sign"] = sign
	// params is sent as a JSON string, matching json_encode($params).
	body["params"] = huolianParamsJSON(params)
	raw, e := p.postJSON(ctx, operation, p.gateway, body, nil)
	if e != nil {
		return nil, e
	}
	out, e := callbackJSON(raw)
	if e != nil {
		return nil, &UnknownOutcome{Operation: operation, Cause: e}
	}
	if jsonText(out, "code") != "0" || jsonText(out, "status") != "200" {
		return nil, rejected("HUOLIAN_" + firstNonEmpty(jsonText(out, "code"), "ERR"))
	}
	data, _ := out["data"].(map[string]any)
	if data == nil {
		return nil, &UnknownOutcome{Operation: operation, Cause: ErrInvalid}
	}
	return data, nil
}

// huolianParamsJSON serializes params as a compact JSON object with sorted keys
// so the value is deterministic; the upstream reads it as an opaque JSON string.
func huolianParamsJSON(params map[string]string) string {
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
		b.WriteString(huolianJSONString(k))
		b.WriteByte(':')
		b.WriteString(huolianJSONString(params[k]))
	}
	b.WriteByte('}')
	return b.String()
}

// huolianJSONString emits a JSON string with JSON_UNESCAPED_SLASHES |
// JSON_UNESCAPED_UNICODE semantics (no \/ and no \uXXXX for non-ASCII).
func huolianJSONString(s string) string {
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
				const hexdigits = "0123456789abcdef"
				b.WriteString(`\u00`)
				b.WriteByte(hexdigits[r>>4])
				b.WriteByte(hexdigits[r&0xF])
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// NotificationAck answers with the original's plain SUCCESS/FAIL.
func (p *huolianPlugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return 400, "text/plain; charset=utf-8", "FAIL"
	}
	return 200, "text/plain; charset=utf-8", "SUCCESS"
}

func (p *huolianPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	obj, e := callbackJSON(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	// verify(): sign over the object minus code/message/sign (string fields).
	signInput := map[string]string{}
	for k := range obj {
		if k == "sign" || k == "code" || k == "message" {
			continue
		}
		signInput[k] = jsonText(obj, k)
	}
	if !signEqual(p.sign(signInput), jsonText(obj, "sign"), false) {
		return PaymentEvent{}, ErrAuthentication
	}
	data, e := callbackJSON([]byte(jsonText(obj, "respBody")))
	if e != nil {
		return PaymentEvent{}, ErrInvalid
	}
	if jsonText(data, "orderStatus") != "2" {
		return PaymentEvent{}, ErrInvalid
	}
	amount, e := parseYuan(jsonText(data, "payAmount"))
	if e != nil || expected.Amount <= 0 || amount != expected.Amount || jsonText(data, "businessOrderNo") != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, jsonText(data, "orderNo"), jsonText(data, "userId"), amount), nil
}

func (p *huolianPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if p.refundPwd == "" {
		return RefundResult{}, ErrUnsupported
	}
	if r.TradeNo == "" || r.ProviderTradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	params := map[string]string{
		"orderNo":          r.ProviderTradeNo,
		"businessRefundNo": r.RefundNo,
		"refundAmount":     yuan(r.Amount),
		"refundPassword":   p.refundPwd,
		"merchantNo":       p.merchantNo,
		"operatorAccount":  p.operator,
	}
	data, e := p.execute(ctx, "huolian.refund", "api.hl.order.refund.operation", params)
	if e != nil {
		return RefundResult{}, e
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(data, "refundNo"), State: "SUCCESS", Amount: r.Amount}, nil
}

var _ Provider = (*huolianPlugin)(nil)
var _ NotificationVerifier = (*huolianPlugin)(nil)
var _ CreateValidator = (*huolianPlugin)(nil)
var _ NotificationAcker = (*huolianPlugin)(nil)
