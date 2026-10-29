package payment

// Port of epay_release/plugins/duolabao (哆啦宝支付). PayApp signs every
// request with a SHA1 token: token = upper(sha1("secretKey=..&timestamp=..&
// path=..&body=..")) where empty path/body segments are dropped, sent as the
// "token" header beside accessKey/timestamp; the JSON body goes to
// https://openapi.duolabao.com. QR orders use /api/generateQRCodeUrl -> url;
// JSAPI orders use /api/createPayWithCheck -> bankRequest. Callbacks are a
// JSON POST verified by recomputing the token over the TIMESTAMP header and
// the raw body (no path); ack is a plain "success".
//
// Out of scope (reported): profit-sharing (LedgerRequest / handleProfits),
// the onboarding applynotify callback, and refund profit-share splitting (the
// base refund is implemented).

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const duolabaoGateway = "https://openapi.duolabao.com"

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "duolabao",
		Title:  "哆啦宝支付",
		Author: "哆啦宝",
		Link:   "http://www.duolabao.com/",
		Types:  []string{"alipay", "wxpay", "qqpay", "bank", "jdpay"},
		Inputs: []PluginInput{
			{Key: "agentNum", Label: "代理商编号", Note: "非代理商不需要填写"},
			{Key: "customerNum", Label: "商户编号", Required: true},
			{Key: "shopNum", Label: "店铺编号", Note: "此项可留空"},
			{Key: "accessKey", Label: "公钥", Required: true},
			{Key: "secretKey", Label: "私钥", Secret: true, Required: true},
		},
		AppTypes: []PluginOption{{Value: "1", Label: "扫码支付"}, {Value: "2", Label: "JS支付"}},
		Build:    newDuolabaoPlugin,
	})
}

type duolabaoPlugin struct {
	pluginBase
	agentNum, customerNum, shopNum string
	accessKey, secretKey           string
	apps                           []string
	gateway                        string
}

func newDuolabaoPlugin(c PluginChannel) (Provider, error) {
	customerNum := strings.TrimSpace(c.Config["customerNum"])
	accessKey := strings.TrimSpace(c.Config["accessKey"])
	secretKey := c.Config["secretKey"]
	if customerNum == "" || len(customerNum) > 64 || accessKey == "" || len(accessKey) > 128 || secretKey == "" || len(secretKey) > 256 {
		return nil, ErrInvalid
	}
	base, e := newPluginBase(c, pluginAccount(duolabaoGateway, customerNum))
	if e != nil {
		return nil, e
	}
	return &duolabaoPlugin{
		pluginBase:  base,
		agentNum:    strings.TrimSpace(c.Config["agentNum"]),
		customerNum: customerNum,
		shopNum:     strings.TrimSpace(c.Config["shopNum"]),
		accessKey:   accessKey,
		secretKey:   secretKey,
		apps:        c.AppTypes,
		gateway:     duolabaoGateway,
	}, nil
}

// token reproduces PayApp::get_token: concatenate in order secretKey,
// timestamp, then path and body when present, as k=v joined by '&', SHA1 hex
// uppercased.
func (p *duolabaoPlugin) token(timestamp, path, body string) string {
	var sb strings.Builder
	sb.WriteString("secretKey=")
	sb.WriteString(p.secretKey)
	sb.WriteString("&timestamp=")
	sb.WriteString(timestamp)
	if path != "" {
		sb.WriteString("&path=")
		sb.WriteString(path)
	}
	if body != "" {
		sb.WriteString("&body=")
		sb.WriteString(body)
	}
	return strings.ToUpper(sha1Hex(sb.String()))
}

// methods: the original only splits QR vs JS by app type for alipay/wxpay;
// qqpay/bank/jdpay are QR-only. native always available, jsapi when app "2".
func (p *duolabaoPlugin) methods() []string {
	switch p.payType {
	case "alipay", "wxpay":
		return duolabaoAppMethods(p.apps)
	default:
		return []string{"native"}
	}
}

func duolabaoAppMethods(appTypes []string) []string {
	has := func(t string) bool {
		if len(appTypes) == 0 {
			return true
		}
		for _, a := range appTypes {
			if a == t {
				return true
			}
		}
		return false
	}
	var m []string
	if has("1") {
		m = append(m, "native")
	}
	if has("2") {
		m = append(m, "jsapi")
	}
	if len(m) == 0 {
		m = []string{"native"}
	}
	return m
}

func (p *duolabaoPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

func (p *duolabaoPlugin) ValidateCreate(r CreateRequest) error {
	if e := validatePluginCreate(r, p.methods()); e != nil {
		return e
	}
	if r.Method == "jsapi" && strings.TrimSpace(r.OpenID) == "" {
		return ErrInvalid
	}
	return nil
}

// bankType maps GOEPAY pay type to duolabao JSAPI bankType.
func (p *duolabaoPlugin) bankType() string {
	switch p.payType {
	case "alipay":
		return "ALIPAY"
	case "wxpay":
		return "WX"
	default:
		return strings.ToUpper(p.payType)
	}
}

func (p *duolabaoPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	if r.Method == "jsapi" {
		return p.createJSAPI(ctx, r)
	}
	return p.createQR(ctx, r)
}

func (p *duolabaoPlugin) createQR(ctx context.Context, r CreateRequest) (CreateResult, error) {
	param := map[string]any{
		"version":      "V4.0",
		"agentNum":     p.agentNum,
		"customerNum":  p.customerNum,
		"shopNum":      p.shopNum,
		"requestNum":   r.TradeNo,
		"orderAmount":  yuan(r.Amount),
		"subOrderType": "NORMAL",
		"orderType":    "SALES",
		"timeExpire":   time.Now().Add(2 * time.Hour).Format("2006-01-02 15:04:05"),
		"businessType": "QRCODE_TRAD",
		"payModel":     "ONCE",
		"source":       "API",
		"callbackUrl":  r.NotifyURL,
		"completeUrl":  r.ReturnURL,
		"clientIp":     r.ClientIP,
	}
	data, e := p.submit(ctx, "duolabao.qr", "/api/generateQRCodeUrl", param)
	if e != nil {
		return CreateResult{}, e
	}
	u := jsonMapText(data, "url")
	if res, ok := materialFromLinks("", u, ""); ok {
		return res, nil
	}
	return CreateResult{}, rejected("DUOLABAO_NO_URL")
}

func (p *duolabaoPlugin) createJSAPI(ctx context.Context, r CreateRequest) (CreateResult, error) {
	bt := p.bankType()
	param := map[string]any{
		"version":      "V4.0",
		"agentNum":     p.agentNum,
		"customerNum":  p.customerNum,
		"shopNum":      p.shopNum,
		"bankType":     bt,
		"paySource":    bt,
		"authCode":     r.OpenID,
		"requestNum":   r.TradeNo,
		"orderAmount":  yuan(r.Amount),
		"subOrderType": "NORMAL",
		"orderType":    "SALES",
		"payType":      "ACTIVE",
		"businessType": "QRCODE_TRAD",
		"payModel":     "ONCE",
		"source":       "API",
		"timeExpire":   time.Now().Add(2 * time.Hour).Format("2006-01-02 15:04:05"),
		"callbackUrl":  r.NotifyURL,
		"clientIp":     r.ClientIP,
	}
	data, e := p.submit(ctx, "duolabao.jsapi", "/api/createPayWithCheck", param)
	if e != nil {
		return CreateResult{}, e
	}
	bankRequest, ok := data["bankRequest"]
	if !ok {
		return CreateResult{}, rejected("DUOLABAO_NO_BANKREQUEST")
	}
	payload, e := json.Marshal(bankRequest)
	if e != nil || len(payload) == 0 || len(payload) > 8192 {
		return CreateResult{}, rejected("DUOLABAO_BAD_BANKREQUEST")
	}
	return CreateResult{MaterialType: "jsapi", Material: string(payload)}, nil
}

// submit signs and POSTs a JSON body, verifies the aggregator outcome, and
// returns the data object. submitNew's success contract: top-level
// success==true or result==true (the aggregator echoes the whole object).
func (p *duolabaoPlugin) submit(ctx context.Context, operation, path string, param map[string]any) (map[string]any, error) {
	body, e := json.Marshal(param)
	if e != nil {
		return nil, ErrInvalid
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	headers := map[string]string{
		"accessKey": p.accessKey,
		"timestamp": ts,
		"token":     p.token(ts, path, string(body)),
	}
	raw, e := p.pluginCall(ctx, operation, http.MethodPost, p.gateway+path, body, "application/json; charset=utf-8", headers)
	if e != nil {
		return nil, e
	}
	out, e := callbackJSON(raw)
	if e != nil {
		return nil, &UnknownOutcome{Operation: operation, Cause: e}
	}
	if s, ok := out["success"].(bool); ok && s {
		return out, nil
	}
	if s, ok := out["result"].(bool); ok && s {
		return out, nil
	}
	code := jsonText(out, "errorCode")
	if code == "" {
		code = jsonText(out, "code")
	}
	if code == "" {
		// No success flag and no error code: the order may have been created.
		return nil, &UnknownOutcome{Operation: operation, Cause: errDuolabaoAmbiguous}
	}
	return nil, rejected("DUOLABAO_" + code)
}

var errDuolabaoAmbiguous = errorString("duolabao ambiguous reply")

type errorString string

func (e errorString) Error() string { return string(e) }

// Notifications are a JSON POST; the token is computed over the TIMESTAMP
// request header and the raw body (no path).
func (p *duolabaoPlugin) VerifyNotification(_ context.Context, header http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	if len(raw) == 0 || len(raw) > 1<<20 {
		return PaymentEvent{}, ErrInvalid
	}
	timestamp := header.Get("Timestamp")
	gotToken := header.Get("Token")
	if timestamp == "" || gotToken == "" {
		return PaymentEvent{}, ErrAuthentication
	}
	if !signEqual(p.token(timestamp, "", string(raw)), gotToken, false) {
		return PaymentEvent{}, ErrAuthentication
	}
	data, e := callbackJSON(raw)
	if e != nil {
		return PaymentEvent{}, ErrInvalid
	}
	if jsonText(data, "status") != "SUCCESS" {
		return PaymentEvent{}, ErrInvalid
	}
	if jsonText(data, "requestNum") != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	amount, e := parseYuan(jsonText(data, "orderAmount"))
	if e != nil || expected.Amount <= 0 || amount != expected.Amount {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, jsonText(data, "orderNum"), jsonText(data, "subOpenId"), amount), nil
}

func (p *duolabaoPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	param := map[string]any{
		"requestVersion":   "V4.0",
		"agentNum":         p.agentNum,
		"customerNum":      p.customerNum,
		"shopNum":          p.shopNum,
		"requestNum":       r.TradeNo,
		"refundPartAmount": yuan(r.Amount),
		"refundRequestNum": r.RefundNo,
		"extMap":           map[string]any{"refund_status_type": "1"},
	}
	data, e := p.submit(ctx, "duolabao.refund", "/api/refundByRequestNum", param)
	if e != nil {
		return RefundResult{}, e
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(data, "orderNum"), State: "SUCCESS", Amount: r.Amount}, nil
}

var _ Provider = (*duolabaoPlugin)(nil)
var _ NotificationVerifier = (*duolabaoPlugin)(nil)
var _ CreateValidator = (*duolabaoPlugin)(nil)
