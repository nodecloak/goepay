package payment

// Port of epay_release/plugins/fuiou (富友支付(前置商户)). Requests are JSON
// POSTed to aipay-cloud.fuioupay.com; the sign is md5 over a FIXED field
// order joined by '|' with the merchant key appended (no key=value, no sort).
// QR (preCreate, order_type ALIPAY/WECHAT/UNIONPAY -> qr_code) maps to
// "native"; JSAPI (wxPreCreate, trade_type FWC alipay / JSAPI wechat) maps to
// "jsapi". The merchant order number sent upstream is <order-prefix><TRADE_NO>.
// Callbacks are a JSON POST signed over their own fixed field order; ack is a
// plain "1" (success) / "0" (fail).
//
// Out of scope (reported): the mini-program LETPAY entry (wxminipay); query is
// not offered by the original. Refund (commonRefund) is implemented.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	fuiouPreCreate   = "https://aipay-cloud.fuioupay.com/aggregatePay/preCreate"
	fuiouWxPreCreate = "https://aipay-cloud.fuioupay.com/aggregatePay/wxPreCreate"
	fuiouRefundURL   = "https://aipay-cloud.fuioupay.com/aggregatePay/commonRefund"
	fuiouHost        = "https://aipay-cloud.fuioupay.com"
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "fuiou",
		Title:  "富友支付(前置商户)",
		Author: "富友",
		Link:   "https://www.fuiou.com/",
		Types:  []string{"alipay", "wxpay", "bank"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "商户号", Required: true},
			{Key: "appkey", Label: "商户密钥", Secret: true, Required: true},
			{Key: "appurl", Label: "订单号前缀"},
		},
		AppTypes: []PluginOption{{Value: "1", Label: "扫码支付"}, {Value: "2", Label: "JS支付"}},
		Build:    newFuiouPlugin,
	})
}

type fuiouPlugin struct {
	pluginBase
	mchntCD, key, prefix string
	apps                 []string
	preCreateURL         string
	wxPreCreateURL       string
	refundURL            string
}

func newFuiouPlugin(c PluginChannel) (Provider, error) {
	mchntCD := strings.TrimSpace(c.Config["appid"])
	key := c.Config["appkey"]
	prefix := strings.TrimSpace(c.Config["appurl"])
	if mchntCD == "" || len(mchntCD) > 32 || key == "" || len(key) > 256 || len(prefix) > 32 {
		return nil, ErrInvalid
	}
	if strings.ContainsAny(prefix, "|") || strings.ContainsAny(mchntCD, "|") {
		return nil, ErrInvalid
	}
	base, e := newPluginBase(c, pluginAccount(fuiouHost, mchntCD))
	if e != nil {
		return nil, e
	}
	return &fuiouPlugin{pluginBase: base, mchntCD: mchntCD, key: key, prefix: prefix, apps: c.AppTypes,
		preCreateURL: fuiouPreCreate, wxPreCreateURL: fuiouWxPreCreate, refundURL: fuiouRefundURL}, nil
}

func (p *fuiouPlugin) hasApp(t string) bool {
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

// methods: alipay/wxpay split QR (app "1") vs JSAPI (app "2"); bank is QR only.
func (p *fuiouPlugin) methods() []string {
	if p.payType == "bank" {
		return []string{"native"}
	}
	var m []string
	if p.hasApp("1") {
		m = append(m, "native")
	}
	if p.hasApp("2") {
		m = append(m, "jsapi")
	}
	if len(m) == 0 {
		m = []string{"native"}
	}
	return m
}

func (p *fuiouPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

func (p *fuiouPlugin) ValidateCreate(r CreateRequest) error {
	if e := validatePluginCreate(r, p.methods()); e != nil {
		return e
	}
	if r.Method == "jsapi" && strings.TrimSpace(r.OpenID) == "" {
		return ErrInvalid
	}
	return nil
}

// signPipe joins the named fields of params by '|' (each followed by '|'),
// appends the key and MD5s it — fuiou's signature scheme.
func (p *fuiouPlugin) signPipe(params map[string]string, order []string) string {
	var sb strings.Builder
	for _, k := range order {
		sb.WriteString(params[k])
		sb.WriteByte('|')
	}
	sb.WriteString(p.key)
	return md5Hex(sb.String())
}

func fuiouNonce() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(b[:])
}

// fuiouTerm returns an 8-digit terminal id (the original uses rand 10000000..
// 99999999). It is an opaque terminal label included in the signed fields.
func fuiouTerm() string {
	var b [4]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "10000000"
	}
	n := (uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])) % 90000000
	return strconv.FormatUint(uint64(n+10000000), 10)
}

func (p *fuiouPlugin) orderType() (string, bool) {
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

func (p *fuiouPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	if r.Method == "jsapi" {
		return p.createJSAPI(ctx, r)
	}
	return p.createQR(ctx, r)
}

func (p *fuiouPlugin) createQR(ctx context.Context, r CreateRequest) (CreateResult, error) {
	orderType, ok := p.orderType()
	if !ok {
		return CreateResult{}, ErrInvalid
	}
	param := map[string]string{
		"version":        "1.0",
		"mchnt_cd":       p.mchntCD,
		"random_str":     fuiouNonce(),
		"order_type":     orderType,
		"order_amt":      strconv.FormatInt(r.Amount, 10),
		"mchnt_order_no": p.prefix + r.TradeNo,
		"txn_begin_ts":   time.Now().Format("20060102150405"),
		"goods_des":      r.Subject,
		"term_id":        fuiouTerm(),
		"term_ip":        r.ClientIP,
		"notify_url":     r.NotifyURL,
	}
	order := []string{"mchnt_cd", "order_type", "order_amt", "mchnt_order_no", "txn_begin_ts", "goods_des", "term_id", "term_ip", "notify_url", "random_str", "version"}
	param["sign"] = p.signPipe(param, order)
	out, e := p.send(ctx, "fuiou.precreate", p.preCreateURL, param)
	if e != nil {
		return CreateResult{}, e
	}
	if res, ok := materialFromLinks("", jsonText(out, "qr_code"), ""); ok {
		return res, nil
	}
	return CreateResult{}, rejected("FUIOU_NO_QRCODE")
}

func (p *fuiouPlugin) createJSAPI(ctx context.Context, r CreateRequest) (CreateResult, error) {
	tradeType := ""
	switch p.payType {
	case "alipay":
		tradeType = "FWC"
	case "wxpay":
		tradeType = "JSAPI"
	default:
		return CreateResult{}, ErrInvalid
	}
	param := map[string]string{
		"version":        "1.0",
		"mchnt_cd":       p.mchntCD,
		"random_str":     fuiouNonce(),
		"order_amt":      strconv.FormatInt(r.Amount, 10),
		"mchnt_order_no": p.prefix + r.TradeNo,
		"txn_begin_ts":   time.Now().Format("20060102150405"),
		"goods_des":      r.Subject,
		"term_id":        fuiouTerm(),
		"term_ip":        r.ClientIP,
		"notify_url":     r.NotifyURL,
		"trade_type":     tradeType,
		"sub_openid":     r.OpenID,
	}
	order := []string{"mchnt_cd", "trade_type", "order_amt", "mchnt_order_no", "txn_begin_ts", "goods_des", "term_id", "term_ip", "notify_url", "random_str", "version"}
	param["sign"] = p.signPipe(param, order)
	out, e := p.send(ctx, "fuiou.wxprecreate", p.wxPreCreateURL, param)
	if e != nil {
		return CreateResult{}, e
	}
	if p.payType == "alipay" {
		tn := jsonText(out, "reserved_transaction_id")
		if tn == "" {
			return CreateResult{}, rejected("FUIOU_NO_TRADE_NO")
		}
		return CreateResult{MaterialType: "jsapi", Material: tn, ProviderTradeNo: tn}, nil
	}
	// wxpay JSAPI: assemble the WeChat jsapi parameter object.
	payinfo := map[string]string{
		"appId":     jsonText(out, "sdk_appid"),
		"timeStamp": jsonText(out, "sdk_timestamp"),
		"nonceStr":  jsonText(out, "sdk_noncestr"),
		"package":   jsonText(out, "sdk_package"),
		"signType":  jsonText(out, "sdk_signtype"),
		"paySign":   jsonText(out, "sdk_paysign"),
	}
	if payinfo["package"] == "" || payinfo["paySign"] == "" {
		return CreateResult{}, rejected("FUIOU_NO_PAY_DATA")
	}
	payload, e := json.Marshal(payinfo)
	if e != nil {
		return CreateResult{}, rejected("FUIOU_BAD_PAY_DATA")
	}
	return CreateResult{MaterialType: "jsapi", Material: string(payload)}, nil
}

// send POSTs the JSON param, parses the reply, and maps the fuiou outcome.
func (p *fuiouPlugin) send(ctx context.Context, operation, target string, param map[string]string) (map[string]any, error) {
	body := map[string]any{}
	for k, v := range param {
		body[k] = v
	}
	raw, e := p.postJSON(ctx, operation, target, body, map[string]string{"Content-Type": "application/json"})
	if e != nil {
		return nil, e
	}
	out, e := callbackJSON(raw)
	if e != nil {
		return nil, &UnknownOutcome{Operation: operation, Cause: e}
	}
	if jsonText(out, "result_code") == "000000" {
		return out, nil
	}
	code := jsonText(out, "result_code")
	if code == "" {
		return nil, &UnknownOutcome{Operation: operation, Cause: errFuiouNoCode}
	}
	return nil, rejected("FUIOU_" + code)
}

var errFuiouNoCode = errorString("fuiou missing result_code")

func (p *fuiouPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	// Callbacks arrive as a JSON body (the original reads php://input).
	out, e := callbackJSON(raw)
	if e != nil {
		return PaymentEvent{}, ErrInvalid
	}
	fields := map[string]string{}
	for _, k := range []string{"mchnt_cd", "mchnt_order_no", "settle_order_amt", "order_amt", "txn_fin_ts", "reserved_fy_settle_dt", "random_str"} {
		fields[k] = jsonText(out, k)
	}
	order := []string{"mchnt_cd", "mchnt_order_no", "settle_order_amt", "order_amt", "txn_fin_ts", "reserved_fy_settle_dt", "random_str"}
	if !signEqual(p.signPipe(fields, order), jsonText(out, "sign"), false) {
		return PaymentEvent{}, ErrAuthentication
	}
	mchntOrderNo := jsonText(out, "mchnt_order_no")
	if !strings.HasPrefix(mchntOrderNo, p.prefix) {
		return PaymentEvent{}, ErrAuthentication
	}
	outTradeNo := mchntOrderNo[len(p.prefix):]
	if outTradeNo != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	amount, e := parseFen(jsonText(out, "order_amt"))
	if e != nil || expected.Amount <= 0 || amount != expected.Amount {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, jsonText(out, "transaction_id"), jsonText(out, "reserved_buyer_logon_id"), amount), nil
}

// NotificationAck answers exactly as the original notify(): "1" success, "0" fail.
func (p *fuiouPlugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return http.StatusOK, "text/plain; charset=utf-8", "0"
	}
	return http.StatusOK, "text/plain; charset=utf-8", "1"
}

func (p *fuiouPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	orderType, ok := p.orderType()
	if !ok {
		return RefundResult{}, ErrInvalid
	}
	param := map[string]string{
		"version":         "1.0",
		"mchnt_cd":        p.mchntCD,
		"term_id":         fuiouTerm(),
		"random_str":      fuiouNonce(),
		"mchnt_order_no":  p.prefix + r.TradeNo,
		"refund_order_no": p.prefix + r.RefundNo,
		"order_type":      orderType,
		"total_amt":       strconv.FormatInt(r.TotalAmount, 10),
		"refund_amt":      strconv.FormatInt(r.Amount, 10),
	}
	order := []string{"mchnt_cd", "order_type", "mchnt_order_no", "refund_order_no", "total_amt", "refund_amt", "term_id", "random_str", "version"}
	param["sign"] = p.signPipe(param, order)
	out, e := p.send(ctx, "fuiou.refund", p.refundURL, param)
	if e != nil {
		return RefundResult{}, e
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(out, "mchnt_order_no"), State: "SUCCESS", Amount: r.Amount}, nil
}

var _ Provider = (*fuiouPlugin)(nil)
var _ NotificationVerifier = (*fuiouPlugin)(nil)
var _ CreateValidator = (*fuiouPlugin)(nil)
var _ NotificationAcker = (*fuiouPlugin)(nil)
