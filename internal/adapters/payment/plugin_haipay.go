package payment

// Port of epay_release/plugins/haipay (海科聚合支付): the 海科融通 SaaS gateway
// (https://saas-front.hkrt.cn). JSON over HTTP; the signature is
// strtoupper(md5( ksort "k=v&..." of the non-empty, non-sign fields, with
// nested objects recursively signed and list values joined by "&", then
// accesskey )). pre-pay returns ali_qr_code / uniqr_qr_code (native) or
// ali_trade_no / wc_pay_data (jsapi). order-query / close-order / refund share
// the same envelope; result_code 10000 is success. Callbacks are POST JSON,
// verified by the same sign, trade_status=="1" paid, acked with the JSON
// {"return_code":"SUCCESS"} / {"return_code":"FAIL",...}.
//
// Out of scope: the passive-pay (付款码/被扫) flow and the 进件/投诉
// (apply/complaint) callbacks; profit-sharing (ledger_biz) is not populated.

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "haipay",
		Title:  "海科聚合支付",
		Author: "海科融通",
		Link:   "https://www.hkrt.cn/",
		Types:  []string{"alipay", "wxpay", "bank"},
		Inputs: []PluginInput{
			{Key: "accessid", Label: "accessid", Required: true},
			{Key: "accesskey", Label: "接入秘钥", Secret: true, Required: true},
			{Key: "agent_no", Label: "服务商编号", Required: true},
			{Key: "merch_no", Label: "商户编号", Required: true},
			{Key: "pn", Label: "终端号", Required: true},
		},
		// select_alipay: 1=扫码支付, 2=JS支付.
		AppTypes: []PluginOption{{Value: "1", Label: "扫码支付"}, {Value: "2", Label: "JS支付"}},
		Note:     "需要先加服务器IP白名单，否则无法调用支付",
		Build:    newHaipayPlugin,
	})
}

const (
	haipayProdHost = "https://saas-front.hkrt.cn"
	haipayTestHost = "http://39.106.187.68:8080"
)

type haipayPlugin struct {
	pluginBase
	accessid, accesskey    string
	agentNo, merchNo, term string
	host                   string
	appTypes               []string
}

func newHaipayPlugin(c PluginChannel) (Provider, error) {
	accessid := strings.TrimSpace(c.Config["accessid"])
	accesskey := c.Config["accesskey"]
	agentNo := strings.TrimSpace(c.Config["agent_no"])
	merchNo := strings.TrimSpace(c.Config["merch_no"])
	term := strings.TrimSpace(c.Config["pn"])
	if accessid == "" || accesskey == "" || agentNo == "" || merchNo == "" || term == "" {
		return nil, ErrInvalid
	}
	if len(accessid) > 64 || len(accesskey) > 128 || len(agentNo) > 64 || len(merchNo) > 64 || len(term) > 64 {
		return nil, ErrInvalid
	}
	base, e := newPluginBase(c, pluginAccount(haipayProdHost, agentNo, merchNo, term))
	if e != nil {
		return nil, e
	}
	return &haipayPlugin{pluginBase: base, accessid: accessid, accesskey: accesskey, agentNo: agentNo, merchNo: merchNo, term: term, host: haipayProdHost, appTypes: append([]string(nil), c.AppTypes...)}, nil
}

func (p *haipayPlugin) haipayHasApp(v string) bool {
	if len(p.appTypes) == 0 {
		return v == "1" // original: empty apptype falls through to NATIVE (扫码)
	}
	for _, a := range p.appTypes {
		if a == v {
			return true
		}
	}
	return false
}

func (p *haipayPlugin) methods() []string {
	switch p.payType {
	case "alipay":
		var m []string
		if p.haipayHasApp("1") {
			m = append(m, "native")
		}
		if p.haipayHasApp("2") {
			m = append(m, "jsapi")
		}
		if len(m) == 0 {
			m = []string{"native"}
		}
		return m
	case "wxpay":
		return []string{"jsapi"} // WX routes through JSAPI (needs openid+appid)
	default:
		return []string{"native"} // UNIONQR
	}
}

func (p *haipayPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Query: true, Close: true, Refund: true}
}

// haipaySignValue renders one value exactly as the original get_sign_str:
// scalars as-is, a list joined by "&" of each element's sign string, a map
// recursively (ksort'ed) joined by "&" of "k=v".
func haipaySignValue(v any) string {
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
	case []any:
		parts := make([]string, 0, len(t))
		for _, e := range t {
			parts = append(parts, haipaySignValue(e))
		}
		return strings.Join(parts, "&")
	case map[string]any:
		return haipaySignMap(t)
	default:
		return ""
	}
}

func haipaySignMap(m map[string]any) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		if k == "sign" {
			continue
		}
		s := haipaySignValue(m[k])
		if s == "" {
			continue
		}
		parts = append(parts, k+"="+s)
	}
	return strings.Join(parts, "&")
}

func (p *haipayPlugin) haipaySign(m map[string]any) string {
	return strings.ToUpper(md5Hex(haipaySignMap(m) + p.accesskey))
}

// haipayPost signs the params, posts JSON, and returns the decoded reply with
// its result_code checked against the success / rejection / unknown contract.
func (p *haipayPlugin) haipayPost(ctx context.Context, operation, path string, params map[string]any) (map[string]any, error) {
	params["accessid"] = p.accessid
	params["req_id"] = haipayReqID()
	params["sign"] = p.haipaySign(params)
	raw, e := p.postJSON(ctx, operation, p.host+path, params, nil)
	if e != nil {
		return nil, e
	}
	out, e := callbackJSON(raw)
	if e != nil {
		return nil, &UnknownOutcome{Operation: operation, Cause: e}
	}
	if jsonText(out, "result_code") != "10000" {
		code := jsonText(out, "result_code")
		if code == "" {
			return nil, &UnknownOutcome{Operation: operation, Cause: errHaipayBadReply}
		}
		return nil, rejected("HAIPAY_" + code)
	}
	return out, nil
}

func (p *haipayPlugin) ValidateCreate(r CreateRequest) error {
	if e := validatePluginCreate(r, p.methods()); e != nil {
		return e
	}
	if r.Method == "jsapi" && r.OpenID == "" {
		return ErrInvalid
	}
	return nil
}

func (p *haipayPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	var payType, payMode string
	switch p.payType {
	case "alipay":
		payType = "ALI"
	case "wxpay":
		payType = "WX"
	default:
		payType = "UNIONQR"
	}
	if r.Method == "jsapi" {
		payMode = "JSAPI"
	} else {
		payMode = "NATIVE"
	}
	params := map[string]any{
		"agent_no":     p.agentNo,
		"merch_no":     p.merchNo,
		"pay_type":     payType,
		"pay_mode":     payMode,
		"out_trade_no": r.TradeNo,
		"total_amount": yuan(r.Amount),
		"pn":           p.term,
		"notify_url":   r.NotifyURL,
	}
	switch payType {
	case "WX":
		params["extend_params"] = map[string]any{"body": r.Subject}
	case "ALI":
		params["extend_params"] = map[string]any{"subject": r.Subject}
	}
	if r.Method == "jsapi" {
		params["openid"] = r.OpenID
		if p.payType == "wxpay" {
			if i := strings.IndexByte(r.OpenID, ':'); i > 0 {
				params["appid"] = r.OpenID[:i]
				params["openid"] = r.OpenID[i+1:]
			}
		}
	}
	out, e := p.haipayPost(ctx, "haipay.prePay", "/api/v2/pay/pre-pay", params)
	if e != nil {
		return CreateResult{}, e
	}
	providerTrade := jsonText(out, "trade_no")
	switch {
	case r.Method == "native" && p.payType == "alipay":
		return haipayQR(jsonText(out, "ali_qr_code"), providerTrade)
	case r.Method == "native":
		return haipayQR(jsonText(out, "uniqr_qr_code"), providerTrade)
	case p.payType == "alipay": // jsapi
		tn := jsonText(out, "ali_trade_no")
		if tn == "" {
			return CreateResult{}, rejected("HAIPAY_NO_TRADE_NO")
		}
		return CreateResult{MaterialType: "jsapi", Material: tn, ProviderTradeNo: providerTrade}, nil
	default: // wxpay jsapi
		js := jsonText(out, "wc_pay_data")
		if js == "" {
			return CreateResult{}, rejected("HAIPAY_NO_JSAPI")
		}
		return CreateResult{MaterialType: "jsapi", Material: js, ProviderTradeNo: providerTrade}, nil
	}
}

func haipayQR(code, providerTrade string) (CreateResult, error) {
	if code == "" || len(code) > 4096 || strings.ContainsAny(code, "\x00\r\n") {
		return CreateResult{}, rejected("HAIPAY_NO_QR")
	}
	return CreateResult{MaterialType: "qrcode", Material: code, ProviderTradeNo: providerTrade}, nil
}

func (p *haipayPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	arr, e := haipayCallbackMap(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	want := jsonText(arr, "sign")
	if !signEqual(p.haipaySign(arr), want, false) {
		return PaymentEvent{}, ErrAuthentication
	}
	if jsonText(arr, "trade_status") != "1" {
		return PaymentEvent{}, ErrInvalid
	}
	amount, e := parseYuan(jsonText(arr, "order_amount"))
	if e != nil || expected.Amount <= 0 || amount != expected.Amount || jsonText(arr, "out_trade_no") != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, jsonText(arr, "trade_no"), jsonText(arr, "openid"), amount), nil
}

// The original notify() answers JSON: SUCCESS on accept, FAIL on sign error.
func (p *haipayPlugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return http.StatusOK, "application/json; charset=utf-8", `{"return_code":"FAIL","return_msg":"SIGN ERROR"}`
	}
	return http.StatusOK, "application/json; charset=utf-8", `{"return_code":"SUCCESS"}`
}

func (p *haipayPlugin) Query(ctx context.Context, r QueryRequest) (QueryResult, error) {
	if r.ProviderTradeNo == "" {
		return QueryResult{}, ErrInvalid
	}
	params := map[string]any{"merch_no": p.merchNo, "trade_no": r.ProviderTradeNo}
	out, e := p.haipayPost(ctx, "haipay.orderQuery", "/api/v2/pay/order-query", params)
	if e != nil {
		return QueryResult{}, e
	}
	if r.TradeNo != "" && jsonText(out, "out_trade_no") != "" && jsonText(out, "out_trade_no") != r.TradeNo {
		return QueryResult{}, ErrAuthentication
	}
	amount, _ := parseYuan(jsonText(out, "order_amount"))
	state := "NOTPAY"
	switch jsonText(out, "trade_status") {
	case "1":
		state = "SUCCESS"
	case "3":
		state = "CLOSED"
	case "4":
		state = "REFUND"
	}
	return QueryResult{TradeNo: r.TradeNo, ProviderTradeNo: r.ProviderTradeNo, State: state, Currency: "CNY", BuyerID: jsonText(out, "openid"), Amount: amount}, nil
}

func (p *haipayPlugin) Close(ctx context.Context, r QueryRequest) error {
	if r.ProviderTradeNo == "" {
		return ErrInvalid
	}
	params := map[string]any{"merch_no": p.merchNo, "trade_no": r.ProviderTradeNo}
	_, e := p.haipayPost(ctx, "haipay.closeOrder", "/api/v2/pay/close-order", params)
	return e
}

func (p *haipayPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.ProviderTradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	params := map[string]any{
		"agent_no":      p.agentNo,
		"merch_no":      p.merchNo,
		"trade_no":      r.ProviderTradeNo,
		"out_refund_no": r.RefundNo,
		"refund_amount": yuan(r.Amount),
		"pn":            p.term,
	}
	out, e := p.haipayPost(ctx, "haipay.refund", "/api/v2/pay/refund", params)
	if e != nil {
		return RefundResult{}, e
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(out, "refund_no"), State: "SUCCESS", Amount: r.Amount}, nil
}

var _ Provider = (*haipayPlugin)(nil)
var _ NotificationVerifier = (*haipayPlugin)(nil)
var _ CreateValidator = (*haipayPlugin)(nil)
var _ NotificationAcker = (*haipayPlugin)(nil)

var errHaipayBadReply = newHaipayError("haipay: unexpected upstream reply")

func newHaipayError(s string) error { return &haipayError{s} }

type haipayError struct{ s string }

func (e *haipayError) Error() string { return e.s }

// haipayReqID is the per-request id (original date('YmdHis').rand). It is not
// part of identity; derived from the clock so it varies per call.
func haipayReqID() string {
	return time.Now().UTC().Format("20060102150405") + strconv.FormatInt(time.Now().UnixNano()%9000+1000, 10)
}

// haipayCallbackMap parses a JSON callback into a flat string map suitable for
// re-signing (the callback is a flat object of scalar fields).
func haipayCallbackMap(raw []byte) (map[string]any, error) {
	obj, e := callbackJSON(raw)
	if e != nil {
		return nil, e
	}
	return obj, nil
}
