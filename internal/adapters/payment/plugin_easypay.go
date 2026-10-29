package payment

// Port of epay_release/plugins/easypay (易生易企通). EasypayClient wraps every
// call as {reqHeader, reqBody, reqSign} JSON POSTed to
// https://phoenix.eycard.cn/yqt<path>. The signature base is
//   canonicalJSON(sortedHeader) + strtoupper(md5(canonicalJSON(sortedBody)))
// where canonicalJSON is PHP json_encode with UNESCAPED_SLASHES |
// UNESCAPED_UNICODE | FORCE_OBJECT and recursively key-sorted objects, signed
// RSA-SHA256/base64 with the merchant private key; the reply's rspSign is
// verified with 易生's public key. QR (/trade/native -> qrCode) maps to
// "native"; JSAPI (/trade/jsapi: AliPay -> tradeNo, WeChat -> wcPayData,
// UnionPay -> qrRedirectUrl) maps to "jsapi". Callbacks are a JSON POST
// (reqHeader/reqBody/reqSign); ack is JSON {"code":"000000","msg":"Success"}.
//
// Out of scope (reported): the merchant/机构 refundquery and query diagnostics
// were debug stubs upstream (they print_r); refund (/trade/refund/apply) is
// implemented. UnionPay JSAPI needs a getQrUserId auth step (session-bound)
// that is a separate business callback and is not ported.

import (
	"context"
	"crypto"
	"crypto/rsa"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

const easypayProdGateway = "https://phoenix.eycard.cn/yqt"
const easypayTestGateway = "https://d-phoenix-gap.easypay.com.cn:24443/yqt"

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "easypay",
		Title:  "易生易企通",
		Author: "易生",
		Link:   "https://www.easypay.com.cn/",
		Types:  []string{"alipay", "wxpay", "bank"},
		Inputs: []PluginInput{
			{Key: "reqtype", Label: "接入模式", Note: "reqId", Options: []PluginOption{{Value: "2", Label: "机构模式"}, {Value: "1", Label: "商户模式"}}, Required: true},
			{Key: "appid", Label: "机构号/商户号", Note: "reqId", Required: true},
			{Key: "appmchid", Label: "子商户号", Note: "机构模式下填写子商户号，非机构模式请勿填写"},
			{Key: "appkey", Label: "易生公钥", Note: "不能有换行和标签", PEM: true, Required: true},
			{Key: "appsecret", Label: "商户私钥", Note: "不能有换行和标签", Secret: true, PEM: true, Required: true},
			{Key: "appswitch", Label: "环境选择", Options: []PluginOption{{Value: "0", Label: "生产环境"}, {Value: "1", Label: "测试环境"}}},
		},
		AppTypes: []PluginOption{{Value: "1", Label: "主扫支付"}, {Value: "2", Label: "JSAPI支付"}},
		Build:    newEasypayPlugin,
	})
}

type easypayPlugin struct {
	pluginBase
	gateway        string
	reqID, reqType string
	mchtCode       string
	priv           *rsa.PrivateKey
	platform       *rsa.PublicKey
	apps           []string
}

func newEasypayPlugin(c PluginChannel) (Provider, error) {
	reqType := strings.TrimSpace(c.Config["reqtype"])
	reqID := strings.TrimSpace(c.Config["appid"])
	subMch := strings.TrimSpace(c.Config["appmchid"])
	if reqType != "1" && reqType != "2" {
		return nil, ErrInvalid
	}
	if reqID == "" || len(reqID) > 64 {
		return nil, ErrInvalid
	}
	// 机构模式 (reqType==2) settles against the sub-merchant code.
	mchtCode := reqID
	if reqType == "2" {
		if subMch == "" {
			return nil, ErrInvalid
		}
		mchtCode = subMch
	}
	priv, e := easypayPrivateKey(c.Certificates["appsecret"])
	if e != nil {
		return nil, e
	}
	platform, e := easypayPublicKey(c.Certificates["appkey"])
	if e != nil {
		return nil, e
	}
	gateway := easypayProdGateway
	if c.Config["appswitch"] == "1" {
		gateway = easypayTestGateway
	}
	base, e := newPluginBase(c, pluginAccount(easypayProdGateway, reqID, mchtCode))
	if e != nil {
		return nil, e
	}
	return &easypayPlugin{pluginBase: base, gateway: gateway, reqID: reqID, reqType: reqType, mchtCode: mchtCode, priv: priv, platform: platform, apps: c.AppTypes}, nil
}

// easypayPrivateKey / easypayPublicKey accept the single-line PKCS keys the
// original wraps into PEM; pluginPrivateKey/pluginPublicKey already handle
// base64 bodies via compat.
func easypayPrivateKey(text string) (*rsa.PrivateKey, error) { return pluginPrivateKey(text) }
func easypayPublicKey(text string) (*rsa.PublicKey, error)   { return pluginPublicKey(text) }

func (p *easypayPlugin) methods() []string {
	has := func(t string) bool {
		if len(p.apps) == 0 {
			return true
		}
		for _, a := range p.apps {
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

func (p *easypayPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

func (p *easypayPlugin) ValidateCreate(r CreateRequest) error {
	if e := validatePluginCreate(r, p.methods()); e != nil {
		return e
	}
	if r.Method == "jsapi" && strings.TrimSpace(r.OpenID) == "" {
		return ErrInvalid
	}
	return nil
}

// payType maps the channel pay type + method to 易生's payType token.
func (p *easypayPlugin) nativePayType() (string, bool) {
	switch p.payType {
	case "alipay":
		return "AliPayNative", true
	case "bank":
		return "UnionPayNative", true
	}
	return "", false // wxpay has no native (QR) product upstream
}

func (p *easypayPlugin) jsapiPayType() (string, bool) {
	switch p.payType {
	case "alipay":
		return "AliPayJsapi", true
	case "wxpay":
		return "WeChatJsapi", true
	case "bank":
		return "UnionPayJsapi", true
	}
	return "", false
}

func (p *easypayPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	if r.Method == "jsapi" {
		return p.createJSAPI(ctx, r)
	}
	return p.createNative(ctx, r)
}

func (p *easypayPlugin) baseParams(r CreateRequest) map[string]any {
	return map[string]any{
		"reqInfo": map[string]any{"mchtCode": p.mchtCode},
		"reqOrderInfo": map[string]any{
			"orgTrace":    r.TradeNo,
			"transAmount": r.Amount, // cents
			"orderSub":    r.Subject,
			"backUrl":     r.NotifyURL,
		},
		"payInfo":         map[string]any{"transDate": time.Now().Format("20060102")},
		"settleParamInfo": map[string]any{"delaySettleFlag": "0", "patnerSettleFlag": "0", "splitSettleFlag": "0"},
		"riskData":        map[string]any{"customerIp": r.ClientIP},
	}
}

func (p *easypayPlugin) createNative(ctx context.Context, r CreateRequest) (CreateResult, error) {
	payType, ok := p.nativePayType()
	if !ok {
		return CreateResult{}, ErrInvalid
	}
	params := p.baseParams(r)
	params["payInfo"].(map[string]any)["payType"] = payType
	if strings.HasPrefix(payType, "UnionPay") {
		params["qrBizParam"] = map[string]any{"transType": "10", "areaInfo": "1561000"}
	}
	body, e := p.execute(ctx, "easypay.native", "/trade/native", params)
	if e != nil {
		return CreateResult{}, e
	}
	order, _ := body["respOrderInfo"].(map[string]any)
	qr := jsonMapText(order, "qrCode")
	if res, ok := materialFromLinks("", qr, ""); ok {
		res.ProviderTradeNo = jsonMapText(order, "outTrace")
		return res, nil
	}
	return CreateResult{}, rejected("EASYPAY_NO_QRCODE")
}

func (p *easypayPlugin) createJSAPI(ctx context.Context, r CreateRequest) (CreateResult, error) {
	payType, ok := p.jsapiPayType()
	if !ok {
		return CreateResult{}, ErrInvalid
	}
	params := p.baseParams(r)
	params["payInfo"].(map[string]any)["payType"] = payType
	switch {
	case strings.HasPrefix(payType, "AliPay"):
		params["aliBizParam"] = map[string]any{"buyerId": r.OpenID}
	case strings.HasPrefix(payType, "WeChat"):
		params["wxBizParam"] = map[string]any{"subOpenId": r.OpenID}
	case strings.HasPrefix(payType, "UnionPay"):
		params["qrBizParam"] = map[string]any{"userId": r.OpenID, "paymentValidTime": 1800, "transType": "10", "areaInfo": "1561000"}
	}
	body, e := p.execute(ctx, "easypay.jsapi", "/trade/jsapi", params)
	if e != nil {
		return CreateResult{}, e
	}
	order, _ := body["respOrderInfo"].(map[string]any)
	switch {
	case strings.HasPrefix(payType, "AliPay"):
		ali, _ := body["aliRespParamInfo"].(map[string]any)
		tn := jsonMapText(ali, "tradeNo")
		if tn == "" {
			return CreateResult{}, rejected("EASYPAY_NO_TRADE_NO")
		}
		return CreateResult{MaterialType: "jsapi", Material: tn, ProviderTradeNo: jsonMapText(order, "outTrace")}, nil
	case strings.HasPrefix(payType, "WeChat"):
		wx, _ := body["wxRespParamInfo"].(map[string]any)
		pay := jsonMapText(wx, "wcPayData")
		if pay == "" {
			return CreateResult{}, rejected("EASYPAY_NO_PAY_DATA")
		}
		return CreateResult{MaterialType: "jsapi", Material: pay, ProviderTradeNo: jsonMapText(order, "outTrace")}, nil
	default: // UnionPay
		qr, _ := body["qrRespParamInfo"].(map[string]any)
		if res, ok := materialFromLinks(jsonMapText(qr, "qrRedirectUrl"), "", ""); ok {
			res.ProviderTradeNo = jsonMapText(order, "outTrace")
			return res, nil
		}
		return CreateResult{}, rejected("EASYPAY_NO_REDIRECT")
	}
}

// execute wraps the request, signs it, posts JSON, verifies the response
// signature, and returns rspBody after checking the business state.
func (p *easypayPlugin) execute(ctx context.Context, operation, path string, data map[string]any) (map[string]any, error) {
	header := map[string]any{
		"transTime": time.Now().Format("20060102150405"),
		"reqId":     p.reqID,
		"reqType":   p.reqType,
	}
	sign, e := p.sign(header, data)
	if e != nil {
		return nil, ErrAuthentication
	}
	payload := map[string]any{"reqHeader": header, "reqBody": data, "reqSign": sign}
	raw, e := p.postJSON(ctx, operation, p.gateway+path, payload, map[string]string{"Content-Type": "application/json; charset=utf-8"})
	if e != nil {
		return nil, e
	}
	out, e := callbackJSON(raw)
	if e != nil {
		return nil, &UnknownOutcome{Operation: operation, Cause: e}
	}
	rspHeader, _ := out["rspHeader"].(map[string]any)
	if jsonMapText(rspHeader, "rspCode") != "000000" {
		code := jsonMapText(rspHeader, "rspCode")
		if code == "" {
			return nil, &UnknownOutcome{Operation: operation, Cause: errEasypayNoHeader}
		}
		return nil, rejected("EASYPAY_" + code)
	}
	rspBody, ok := out["rspBody"].(map[string]any)
	if !ok {
		return nil, &UnknownOutcome{Operation: operation, Cause: errEasypayNoBody}
	}
	if !p.verifyResponse(rspHeader, rspBody, jsonText(out, "rspSign")) {
		return nil, &UnknownOutcome{Operation: operation, Cause: errEasypayBadSign}
	}
	state, _ := rspBody["respStateInfo"].(map[string]any)
	if jsonMapText(state, "respCode") != "000000" {
		return nil, rejected("EASYPAY_" + jsonMapText(state, "respCode"))
	}
	if jsonMapText(state, "transState") == "X" {
		code := jsonMapText(state, "appendRetCode")
		if code == "" {
			code = "TRANS_FAILED"
		}
		return nil, rejected("EASYPAY_" + code)
	}
	return rspBody, nil
}

var errEasypayNoHeader = errorString("easypay missing rspHeader")
var errEasypayNoBody = errorString("easypay missing rspBody")
var errEasypayBadSign = errorString("easypay response signature verification failed")

// signContent reproduces getSignContent: canonicalJSON(header) +
// upper(md5(canonicalJSON(body))).
func (p *easypayPlugin) signContent(header, body map[string]any) string {
	return easypayCanonicalJSON(header) + strings.ToUpper(md5Hex(easypayCanonicalJSON(body)))
}

func (p *easypayPlugin) sign(header, body map[string]any) (string, error) {
	return rsaSign(p.priv, crypto.SHA256, p.signContent(header, body))
}

func (p *easypayPlugin) verifyResponse(header, body map[string]any, sign string) bool {
	return rsaVerify(p.platform, crypto.SHA256, p.signContent(header, body), sign)
}

// easypayCanonicalJSON serializes a value exactly like PHP json_encode with
// UNESCAPED_SLASHES | UNESCAPED_UNICODE | FORCE_OBJECT over a recursively
// key-sorted structure: object keys sorted (SORT_STRING), arrays encoded as
// objects with string indices, scalars emitted like PHP.
func easypayCanonicalJSON(v any) string {
	var sb strings.Builder
	easypayEncode(&sb, v)
	return sb.String()
}

func easypayEncode(sb *strings.Builder, v any) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		sb.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				sb.WriteByte(',')
			}
			easypayEncodeString(sb, k)
			sb.WriteByte(':')
			easypayEncode(sb, t[k])
		}
		sb.WriteByte('}')
	case []any: // FORCE_OBJECT: array -> {"0":..,"1":..}
		sb.WriteByte('{')
		for i, item := range t {
			if i > 0 {
				sb.WriteByte(',')
			}
			easypayEncodeString(sb, strconv.Itoa(i))
			sb.WriteByte(':')
			easypayEncode(sb, item)
		}
		sb.WriteByte('}')
	case string:
		easypayEncodeString(sb, t)
	case bool:
		if t {
			sb.WriteString("true")
		} else {
			sb.WriteString("false")
		}
	case int:
		sb.WriteString(strconv.Itoa(t))
	case int64:
		sb.WriteString(strconv.FormatInt(t, 10))
	case nil:
		sb.WriteString("null")
	default:
		easypayEncodeString(sb, "")
	}
}

// easypayEncodeString writes a JSON string with slashes and non-ASCII left
// unescaped (PHP UNESCAPED_SLASHES | UNESCAPED_UNICODE), escaping only the
// mandatory control/quote/backslash characters.
func easypayEncodeString(sb *strings.Builder, s string) {
	sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		case '\t':
			sb.WriteString(`\t`)
		case '\b':
			sb.WriteString(`\b`)
		case '\f':
			sb.WriteString(`\f`)
		default:
			if r < 0x20 {
				const hexdigits = "0123456789abcdef"
				sb.WriteString(`\u00`)
				sb.WriteByte(hexdigits[r>>4])
				sb.WriteByte(hexdigits[r&0xf])
			} else {
				sb.WriteRune(r)
			}
		}
	}
	sb.WriteByte('"')
}

func (p *easypayPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	out, e := callbackJSON(raw)
	if e != nil {
		return PaymentEvent{}, ErrInvalid
	}
	header, _ := out["reqHeader"].(map[string]any)
	body, ok := out["reqBody"].(map[string]any)
	if !ok || header == nil {
		return PaymentEvent{}, ErrInvalid
	}
	if !p.verifyResponse(header, body, jsonText(out, "reqSign")) {
		return PaymentEvent{}, ErrAuthentication
	}
	state, _ := body["respStateInfo"].(map[string]any)
	ts := jsonMapText(state, "transState")
	if ts != "0" && ts != "1" {
		return PaymentEvent{}, ErrInvalid
	}
	order, _ := body["respOrderInfo"].(map[string]any)
	if jsonMapText(order, "orgTrace") != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	amount, e := parseFen(jsonMapText(order, "transAmount"))
	if e != nil || expected.Amount <= 0 || amount != expected.Amount {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, jsonMapText(order, "outTrace"), jsonMapText(order, "userId"), amount), nil
}

// NotificationAck answers with the JSON the original echoes.
func (p *easypayPlugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return http.StatusOK, "application/json; charset=utf-8", `{"code":"100001","msg":"sign error"}`
	}
	return http.StatusOK, "application/json; charset=utf-8", `{"code":"000000","msg":"Success"}`
}

func (p *easypayPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.ProviderTradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	oriDate := ""
	if len(r.TradeNo) >= 8 {
		oriDate = r.TradeNo[:8]
	}
	params := map[string]any{
		"reqInfo": map[string]any{"mchtCode": p.mchtCode},
		"reqOrderInfo": map[string]any{
			"orgTrace":     r.RefundNo,
			"oriOutTrace":  r.ProviderTradeNo,
			"oriTransDate": oriDate,
			"refundAmount": r.Amount,
		},
		"payInfo": map[string]any{"transDate": time.Now().Format("20060102")},
	}
	body, e := p.execute(ctx, "easypay.refund", "/trade/refund/apply", params)
	if e != nil {
		return RefundResult{}, e
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(body, "outTrace"), State: "SUCCESS", Amount: r.Amount}, nil
}

var _ Provider = (*easypayPlugin)(nil)
var _ NotificationVerifier = (*easypayPlugin)(nil)
var _ CreateValidator = (*easypayPlugin)(nil)
var _ NotificationAcker = (*easypayPlugin)(nil)
