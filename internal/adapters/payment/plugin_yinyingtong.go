package payment

// Port of epay_release/plugins/yinyingtong (银盈通支付).
//
// The payment NOTIFICATION cannot be verified from the available SDK:
// PayClient::notify($data,$key) drops a 107-byte header of undocumented layout,
// takes a 4-char length prefix, 3DES-ECB decrypts the remainder and splits the
// plaintext on an unprintable control byte — none of it derivable from the SDK,
// and there is NO signature over the result. We therefore never trust the
// callback body. Instead VerifyNotification extracts only the trade number from
// the callback and CONFIRMS the payment through the upstream order-query API,
// signed and transported exactly as PayClient does (MD5 envelope over the
// GEPOS gateway). A paid event is returned only when the query confirms success
// for our trade number at the expected amount.
//
// Ported in scope: the扫码/H5 cashier pay entries alipay() and wxpay() through
// prepay(gepos.pre.pay) (QR content / jump to the h5.gomepay.com cashier), the
// order query (gepos.order.query) and refund (gepos.refund). Out of scope and
// deliberately unimplemented: the JSAPI/公众号 paths (需 openid/oauth), the
// mini-program and APP paths, and the 快捷 (bank, M2Client) path which needs a
// per-merchant <appid>.pfx PKCS#12 key plus RC4 + SM4-ECB. Transfers and the
// 进件/complaint/apply business callbacks are also out of scope.

import (
	"context"
	"encoding/json"
	"math/rand"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "yinyingtong",
		Title:  "银盈通支付",
		Author: "银盈通",
		Link:   "http://www.yinyingtong.com/",
		Types:  []string{"alipay", "wxpay", "bank"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "应用ID", Required: true},
			{Key: "appkey", Label: "应用KEY", Note: "同时是私钥证书密码", Secret: true, Required: true},
			{Key: "appmchid", Label: "交易商户企业号", Required: true},
			{Key: "productkey", Label: "产品密钥", Note: "原回调数据解密密钥（本平台改为主动查询确认，可留空）", Secret: true},
			{Key: "trade_platform_no", Label: "平台商企业号(参考号)", Note: "仅分账需要填写"},
			{Key: "terminal_number", Label: "终端号", Note: "仅快捷支付填写（本平台暂不支持快捷）"},
			{Key: "channel_merch_no", Label: "渠道商户号", Note: "可留空，多个渠道商户号可用,分隔"},
		},
		Note:  "已支持支付宝/微信扫码及H5收银台下单（gepos.pre.pay）、订单查询与退款。回调不可独立验签，改为按回调订单号主动查询上游确认。JSAPI、小程序、APP 与快捷支付暂不支持。",
		Build: newYinyingtongPlugin,
	})
}

// yinyingtongGateway is PayClient::$gateway_url.
const yinyingtongGateway = "https://gc-gw.gomepay.com/gpayCashApi"

const (
	yinyingtongQueryMethod  = "gepos.order.query"
	yinyingtongPrepayMethod = "gepos.pre.pay"
	yinyingtongRefundMethod = "gepos.refund"
)

type yinyingtongPlugin struct {
	pluginBase
	appID, appKey, mchID   string
	refNo, channelMerchNos string
	// gateway is the GEPOS endpoint; tests point it at an httptest server.
	gateway string
	// now is the clock for the request timestamp; nil means time.Now.
	now func() time.Time
	// rnd picks among comma-separated channel_merch_no; nil means math/rand.
	rnd func(n int) int
}

func newYinyingtongPlugin(c PluginChannel) (Provider, error) {
	appID := strings.TrimSpace(c.Config["appid"])
	appKey := c.Config["appkey"]
	mchID := strings.TrimSpace(c.Config["appmchid"])
	if appID == "" || len(appID) > 64 || strings.ContainsAny(appID, "&=?# ") ||
		appKey == "" || len(appKey) > 256 ||
		mchID == "" || len(mchID) > 64 || strings.ContainsAny(mchID, "&=?# ") {
		return nil, ErrInvalid
	}
	host, ok := gatewayHost(yinyingtongGateway)
	if !ok {
		return nil, ErrInvalid
	}
	base, e := newPluginBase(c, pluginAccount(host, mchID))
	if e != nil {
		return nil, e
	}
	return &yinyingtongPlugin{
		pluginBase:      base,
		appID:           appID,
		appKey:          appKey,
		mchID:           mchID,
		refNo:           strings.TrimSpace(c.Config["trade_platform_no"]),
		channelMerchNos: strings.TrimSpace(c.Config["channel_merch_no"]),
		gateway:         yinyingtongGateway,
	}, nil
}

// methods are the pay methods ported for this channel's pay type. Only the
// 扫码 (native) + H5 (jump) cashier entries are supported; JSAPI/mini/app/快捷
// are intentionally omitted.
func (p *yinyingtongPlugin) methods() []string {
	switch p.payType {
	case "alipay":
		return []string{"native", "wap"}
	case "wxpay":
		return []string{"native", "h5"}
	default:
		// bank (快捷) is not ported.
		return nil
	}
}

func (p *yinyingtongPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Query: true, Refund: true}
}

func (p *yinyingtongPlugin) ValidateCreate(r CreateRequest) error {
	return validatePluginCreate(r, p.methods())
}

// yinyingtongPayType maps the GOEPAY pay type to the original prepay pay_type
// code (01 alipay, 02 wxpay).
func (p *yinyingtongPlugin) yinyingtongPayTypeCode() (string, bool) {
	switch p.payType {
	case "alipay":
		return "01", true
	case "wxpay":
		return "02", true
	default:
		return "", false
	}
}

// Create ports alipay()/wxpay(): prepay via gepos.pre.pay, then the
// h5.gomepay.com cashier URL. native => qrcode content; wap/h5 => jump.
func (p *yinyingtongPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	payType, ok := p.yinyingtongPayTypeCode()
	if !ok {
		return CreateResult{}, ErrUnsupported
	}
	biz := map[string]string{
		"merchant_number":         p.mchID,
		"order_number":            r.TradeNo,
		"amount":                  yuan(r.Amount),
		"pay_type":                payType,
		"currency":                "CNY",
		"order_title":             r.Subject,
		"channel_code":            payType,
		"async_notification_addr": r.NotifyURL,
		"notify_key_mode":         "03",
	}
	if p.refNo != "" {
		biz["ref_no"] = p.refNo
	}
	if r.ProfitSharing {
		biz["profit_sharing"] = "1"
	}
	if merch := p.pickChannelMerchNo(); merch != "" {
		biz["bank_mch_id"] = merch
	}

	inner, e := p.yinyingtongExecute(ctx, "yinyingtong.prepay", yinyingtongPrepayMethod, biz)
	if e != nil {
		return CreateResult{}, e
	}
	orderID := jsonText(inner, "order_id")
	if orderID == "" || strings.ContainsAny(orderID, "\x00\r\n &?#") {
		// A 2xx reply with op_ret_code 000 but no usable order id: the upstream
		// may have created the order; treat as unknown rather than a refusal.
		return CreateResult{}, &UnknownOutcome{Operation: "yinyingtong.prepay", Cause: errNoOrderID}
	}

	route := "orderPay"
	if p.payType == "wxpay" {
		route = "wxPublicOrder"
	}
	cashier := yinyingtongCashierURL(route, orderID)
	material := "qrcode"
	if r.Method == "wap" || r.Method == "h5" {
		material = "jump"
	}
	return CreateResult{MaterialType: material, Material: cashier, ProviderTradeNo: orderID}, nil
}

// Refund ports refund(): gepos.refund. scene is 0615 when the original
// payment's transcode is T61, else 0606. The transcode MUST be known; when it
// is not, refuse with a clear error rather than guessing the scene.
func (p *yinyingtongPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.ProviderTradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	// transcode decides the refund scene. We read it from the order query
	// (which surfaces it); without it the scene is unknown and we must not
	// guess — refuse with a definitive error.
	queryBiz := map[string]string{"merchant_number": p.mchID, "order_number": r.TradeNo}
	qinner, e := p.yinyingtongExecute(ctx, "yinyingtong.query", yinyingtongQueryMethod, queryBiz)
	if e != nil {
		return RefundResult{}, e
	}
	if on := jsonText(qinner, "order_number"); on != "" && on != r.TradeNo {
		return RefundResult{}, ErrAuthentication
	}
	scene, ok := yinyingtongRefundScene(jsonText(qinner, "transcode"))
	if !ok {
		return RefundResult{}, rejected("YYT_UNKNOWN_TRANSCODE")
	}
	biz := map[string]string{
		"scene":            scene,
		"merchant_number":  p.mchID,
		"order_number":     r.RefundNo,
		"old_order_number": r.TradeNo,
		"old_order_id":     r.ProviderTradeNo,
		"amount":           yuan(r.Amount),
		"currency":         "CNY",
		"memo":             "订单退款",
	}
	inner, e := p.yinyingtongExecute(ctx, "yinyingtong.refund", yinyingtongRefundMethod, biz)
	if e != nil {
		return RefundResult{}, e
	}
	return RefundResult{
		RefundNo:         r.RefundNo,
		ProviderRefundNo: jsonText(inner, "order_id"),
		State:            "SUCCESS",
		Amount:           r.Amount,
	}, nil
}

// pickChannelMerchNo reproduces the original's random pick among a
// comma-separated channel_merch_no list.
func (p *yinyingtongPlugin) pickChannelMerchNo() string {
	if p.channelMerchNos == "" {
		return ""
	}
	if !strings.Contains(p.channelMerchNos, ",") {
		return p.channelMerchNos
	}
	parts := strings.Split(p.channelMerchNos, ",")
	clean := make([]string, 0, len(parts))
	for _, s := range parts {
		if s = strings.TrimSpace(s); s != "" {
			clean = append(clean, s)
		}
	}
	if len(clean) == 0 {
		return ""
	}
	idx := 0
	if p.rnd != nil {
		idx = p.rnd(len(clean))
	} else {
		idx = rand.Intn(len(clean))
	}
	if idx < 0 || idx >= len(clean) {
		idx = 0
	}
	return clean[idx]
}

// yinyingtongSign reproduces PayClient::get_sign: of the public envelope, only
// the fixed sign_keys with non-empty values are taken, ksorted, joined as
// "k=v&", then "key=<app_secret>" appended; the digest is upper-case MD5.
func (p *yinyingtongPlugin) yinyingtongSign(params map[string]string) string {
	signKeys := map[string]bool{
		"req_no": true, "app_id": true, "sign_type": true, "charset": true,
		"format": true, "version": true, "data": true, "timestamp": true, "method": true,
	}
	keys := make([]string, 0, len(signKeys))
	for k, v := range params {
		if signKeys[k] && v != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(params[k])
		b.WriteByte('&')
	}
	b.WriteString("key=" + p.appKey)
	return strings.ToUpper(md5Hex(b.String()))
}

// yinyingtongReqNo is a deterministic, upstream-safe request id derived from
// an input (the original uses a PHP session id; we need no session).
func yinyingtongReqNo(seed string) string {
	h := md5Hex("yyt:" + seed)
	return "Q" + strings.ToUpper(h)[:24]
}

// yinyingtongExecute performs one PayClient::execute call: it builds the
// MD5-signed JSON envelope around the biz data, POSTs it, and returns the inner
// data map after checking the top-level and op_ret_code results. The outcome
// rules mirror PayClient and the kit: transport/5xx/unreadable => UnknownOutcome;
// an explicit business failure => ProviderError(YYT_<code>); success => inner.
func (p *yinyingtongPlugin) yinyingtongExecute(ctx context.Context, operation, method string, biz map[string]string) (map[string]any, error) {
	bizJSON, e := json.Marshal(biz)
	if e != nil {
		return nil, ErrInvalid
	}
	params := map[string]string{
		"app_id":        p.appID,
		"method":        method,
		"format":        "JSON",
		"charset":       "UTF-8",
		"sign_type":     "MD5",
		"timestamp":     p.yinyingtongNow().UTC().Format("2006-01-02 15:04:05"),
		"version":       "1.0",
		"data":          string(bizJSON),
		"req_no":        yinyingtongReqNo(method + ":" + biz["order_number"]),
		"terminal_type": "3",
		"browser_brand": "99",
	}
	params["sign"] = p.yinyingtongSign(params)
	envelope, e := json.Marshal(params)
	if e != nil {
		return nil, ErrInvalid
	}
	raw, e := p.pluginCall(ctx, operation, http.MethodPost, p.gateway, envelope,
		"application/json; charset=utf-8", map[string]string{"method": "cash-api@" + method})
	if e != nil {
		return nil, e
	}
	outer, e := callbackJSON(raw)
	if e != nil {
		return nil, &UnknownOutcome{Operation: operation, Cause: e}
	}
	// PayClient accepts these top-level codes as "carrying data".
	switch jsonText(outer, "code") {
	case "000000", "900888", "900889", "900001":
		// ok, data follows
	default:
		if sub := jsonText(outer, "sub_code"); sub != "" {
			return nil, rejected("YYT_" + sub)
		}
		return nil, rejected("YYT_" + yinyingtongFirstNonEmpty(jsonText(outer, "code"), "NO_CODE"))
	}
	inner, e := yinyingtongInner(outer)
	if e != nil {
		return nil, &UnknownOutcome{Operation: operation, Cause: e}
	}
	if ret := jsonText(inner, "op_ret_code"); ret != "000" && ret != "701" {
		if sub := jsonText(inner, "op_ret_subcode"); sub != "" {
			return nil, rejected("YYT_" + sub)
		}
		return nil, rejected("YYT_" + yinyingtongFirstNonEmpty(ret, "NO_RET"))
	}
	return inner, nil
}

// Query confirms an order through gepos.order.query. Success requires the
// returned order_number (when present) to equal our trade number and the amount
// to match the reply. The transcode, when returned, is carried for Refund.
func (p *yinyingtongPlugin) Query(ctx context.Context, r QueryRequest) (QueryResult, error) {
	if r.TradeNo == "" || len(r.TradeNo) > 64 {
		return QueryResult{}, ErrInvalid
	}
	biz := map[string]string{
		"merchant_number": p.mchID,
		"order_number":    r.TradeNo,
	}
	inner, e := p.yinyingtongExecute(ctx, "yinyingtong.query", yinyingtongQueryMethod, biz)
	if e != nil {
		return QueryResult{}, e
	}
	// Bind the reply to our trade number; order_number is our out_trade_no.
	if on := jsonText(inner, "order_number"); on != "" && on != r.TradeNo {
		return QueryResult{}, ErrAuthentication
	}
	amount, e := parseYuan(yinyingtongFirstNonEmpty(jsonText(inner, "amount"), jsonText(inner, "total_amount")))
	if e != nil {
		return QueryResult{}, ErrAuthentication
	}
	state := yinyingtongMapState(jsonText(inner, "orderstatus"), jsonText(inner, "status"), jsonText(inner, "order_status"))
	return QueryResult{
		TradeNo:         r.TradeNo,
		ProviderTradeNo: yinyingtongFirstNonEmpty(jsonText(inner, "order_id"), jsonText(inner, "orderid")),
		State:           state,
		Currency:        "CNY",
		Amount:          amount,
	}, nil
}

// VerifyNotification does NOT trust the callback body (it cannot be verified).
// It extracts only a trade number from the callback and confirms the result by
// querying the upstream. A paid event is returned only when the query confirms
// SUCCESS for our trade number at exactly the expected amount.
func (p *yinyingtongPlugin) VerifyNotification(ctx context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	if expected.TradeNo == "" || expected.Amount <= 0 {
		return PaymentEvent{}, ErrInvalid
	}
	tradeNo := yinyingtongCallbackTradeNo(raw)
	if tradeNo == "" {
		return PaymentEvent{}, ErrInvalid
	}
	// The callback must at least name OUR trade number; anything else is not
	// our order and must not drive a query bound to our expected amount.
	if tradeNo != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}

	res, e := p.Query(ctx, QueryRequest{TradeNo: expected.TradeNo})
	if e != nil {
		// Transport/unknown bubbles up as UnknownOutcome; a refusal or
		// mismatch comes back as ProviderError/ErrAuthentication. The endpoint
		// must not ack success on any of these.
		return PaymentEvent{}, e
	}
	if res.State != "SUCCESS" {
		return PaymentEvent{}, ErrInvalid
	}
	if res.TradeNo != expected.TradeNo || res.Amount != expected.Amount {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, res.ProviderTradeNo, res.BuyerID, res.Amount), nil
}

// NotificationMethods: the original callback is a POST (dstbdata/dstbdatasign).
func (p *yinyingtongPlugin) NotificationMethods() []string { return []string{http.MethodPost} }

// NotificationAck mirrors the original notify(): "00" success, "01" failure.
func (p *yinyingtongPlugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return http.StatusOK, "text/plain; charset=utf-8", "01"
	}
	return http.StatusOK, "text/plain; charset=utf-8", "00"
}

// ---- helpers (yinyingtong-prefixed) ----

var errNoOrderID = errPluginNoOrderID{}

type errPluginNoOrderID struct{}

func (errPluginNoOrderID) Error() string { return "yinyingtong: prepay returned no order id" }

// yinyingtongCashierURL builds the h5.gomepay.com cashier URL for a route and
// order id, url-escaping the order id into the query.
func yinyingtongCashierURL(route, orderID string) string {
	return "https://h5.gomepay.com/cashier-h5/index.html#/pages/preOrder/" + route +
		"?orderId=" + url.QueryEscape(orderID) + "&showPayButton=0"
}

// yinyingtongRefundScene maps the payment's transcode to the refund scene.
// Only a known transcode yields a scene; an empty/unknown one returns ok=false
// so Refund can refuse rather than guess.
func yinyingtongRefundScene(transcode string) (string, bool) {
	switch strings.TrimSpace(transcode) {
	case "":
		return "", false
	case "T61":
		return "0615", true
	default:
		return "0606", true
	}
}

// yinyingtongInner unwraps the gateway envelope's `data`, which PayClient
// decodes with json_decode($result['data'], true). It may already be an object
// (some gateways) or a JSON string; both are handled.
func yinyingtongInner(outer map[string]any) (map[string]any, error) {
	switch v := outer["data"].(type) {
	case map[string]any:
		return v, nil
	case string:
		if strings.TrimSpace(v) == "" {
			return nil, ErrInvalid
		}
		return callbackJSON([]byte(v))
	default:
		return nil, ErrInvalid
	}
}

// yinyingtongCallbackTradeNo pulls a trade number out of the callback without
// trusting it for anything but naming the order to query. It accepts a JSON
// body ({"dsorderid":...}/{"order_number":...}) or a form/query callback.
func yinyingtongCallbackTradeNo(raw []byte) string {
	if len(raw) == 0 || len(raw) > 1<<20 {
		return ""
	}
	if m, e := callbackJSON(raw); e == nil {
		for _, k := range []string{"dsorderid", "order_number", "out_trade_no", "orderNumber"} {
			if v := strings.TrimSpace(jsonText(m, k)); v != "" {
				return yinyingtongSafeTradeNo(v)
			}
		}
		return ""
	}
	if m, e := callbackParams(raw); e == nil {
		for _, k := range []string{"dsorderid", "order_number", "out_trade_no", "orderNumber"} {
			if v := strings.TrimSpace(m[k]); v != "" {
				return yinyingtongSafeTradeNo(v)
			}
		}
	}
	return ""
}

func yinyingtongSafeTradeNo(v string) string {
	if len(v) > 64 || strings.ContainsAny(v, "\x00\r\n &=?#") {
		return ""
	}
	return v
}

// yinyingtongMapState maps whatever status field the query reply carries to a
// GOEPAY state. "00" is the original's paid marker.
func yinyingtongMapState(statuses ...string) string {
	for _, s := range statuses {
		switch strings.ToUpper(strings.TrimSpace(s)) {
		case "00", "SUCCESS", "PAID", "TRADE_SUCCESS":
			return "SUCCESS"
		case "CLOSED", "TRADE_CLOSED":
			return "CLOSED"
		case "REFUND", "REFUNDED":
			return "REFUND"
		}
	}
	return "NOTPAY"
}

func yinyingtongFirstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// yinyingtongNow is the request-timestamp clock; tests pin it via p.now.
func (p *yinyingtongPlugin) yinyingtongNow() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now()
}

var _ Provider = (*yinyingtongPlugin)(nil)
var _ NotificationVerifier = (*yinyingtongPlugin)(nil)
var _ NotificationMethodsProvider = (*yinyingtongPlugin)(nil)
var _ NotificationAcker = (*yinyingtongPlugin)(nil)
var _ CreateValidator = (*yinyingtongPlugin)(nil)
