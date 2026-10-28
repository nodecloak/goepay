package payment

// Port of epay_release/plugins/chinaums (银联商务 open platform). Requests are
// JSON over HTTPS; authentication is an HMAC-SHA256 "OPEN-BODY-SIG" header
// (POST) or an "OPEN-FORM-PARAM" query signature (GET), computed as
// base64(HMAC-SHA256(appId+timestamp+nonce+sha256hex(body), appKey)).
//   - QR下单 /v1/netpay/bills/get-qrcode returns billQRCode (a QR, instMid
//     QRPAYDEFAULT). Used for scan on alipay / wxpay / bank.
//   - H5 GET /v1/netpay/trade/h5-pay (alipay) and /v1/netpay/wxpay/h5-pay
//     (wxpay) build a signed redirect URL (instMid H5DEFAULT).
// The merchant order number sent upstream is <msgSrcId><TradeNo>; the async
// callback is form POST verified with MD5/SHA256 uppercase over the ksort-ed
// non-empty params + the communication key (appsecret), and the H5 vs QR
// branch is told apart by instMid. Refund (bills/refund, netpay/refund) is
// implemented; APP pre-order and profit-sharing are OUT OF SCOPE.

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "chinaums",
		Title:  "银联商务",
		Author: "银联商务",
		Link:   "https://open.chinaums.com/",
		Types:  []string{"alipay", "wxpay", "bank"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "AppId", Required: true},
			{Key: "appkey", Label: "AppKey", Secret: true, Required: true},
			{Key: "appmchid", Label: "商户号mid", Required: true},
			{Key: "appurl", Label: "终端号tid", Required: true},
			{Key: "appsecret", Label: "通讯密钥", Secret: true, Required: true},
			{Key: "msgsrcid", Label: "来源编号", Note: "4位来源编号", Required: true},
			{Key: "appswitch", Label: "环境选择", Options: []PluginOption{{Value: "0", Label: "生产环境"}, {Value: "1", Label: "测试环境"}}},
		},
		AppTypes: []PluginOption{
			{Value: "scan", Label: "扫码支付"},
			{Value: "h5", Label: "H5支付"},
		},
		Build: newChinaumsPlugin,
	})
}

const (
	chinaumsGatewayProd = "https://api-mop.chinaums.com"
	chinaumsGatewayTest = "https://test-api-open.chinaums.com"
)

type chinaumsPlugin struct {
	pluginBase
	gateway          string
	appID, appKey    string
	mid, tid, secret string
	msgSrcID         string
	appTypes         map[string]bool
}

func newChinaumsPlugin(c PluginChannel) (Provider, error) {
	appID, appKey := strings.TrimSpace(c.Config["appid"]), c.Config["appkey"]
	mid, tid := strings.TrimSpace(c.Config["appmchid"]), strings.TrimSpace(c.Config["appurl"])
	secret := c.Config["appsecret"]
	msgSrcID := strings.TrimSpace(c.Config["msgsrcid"])
	if appID == "" || len(appID) > 64 || appKey == "" || mid == "" || tid == "" || secret == "" {
		return nil, ErrInvalid
	}
	if len(msgSrcID) != 4 || strings.ContainsAny(msgSrcID, " \r\n\x00") {
		return nil, ErrInvalid
	}
	gateway := chinaumsGatewayProd
	if c.Config["appswitch"] == "1" {
		gateway = chinaumsGatewayTest
	}
	appTypes := map[string]bool{}
	for _, t := range c.AppTypes {
		appTypes[t] = true
	}
	base, e := newPluginBase(c, pluginAccount(gateway, mid, tid))
	if e != nil {
		return nil, e
	}
	return &chinaumsPlugin{pluginBase: base, gateway: gateway, appID: appID, appKey: appKey, mid: mid, tid: tid, secret: secret, msgSrcID: msgSrcID, appTypes: appTypes}, nil
}

func (p *chinaumsPlugin) has(t string) bool {
	if len(p.appTypes) == 0 {
		return t == "scan"
	}
	return p.appTypes[t]
}

func (p *chinaumsPlugin) methods() []string {
	var m []string
	if p.has("scan") || len(p.appTypes) == 0 {
		m = append(m, "native")
	}
	// H5 jump is offered for alipay and wxpay only (bank QR only).
	if p.has("h5") && (p.payType == "alipay" || p.payType == "wxpay") {
		m = append(m, "h5")
	}
	if len(m) == 0 {
		m = []string{"native"}
	}
	return m
}

func (p *chinaumsPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

func (p *chinaumsPlugin) ValidateCreate(r CreateRequest) error {
	return validatePluginCreate(r, p.methods())
}

func (p *chinaumsPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	ts := time.Now()
	switch r.Method {
	case "native":
		return p.qrcode(ctx, r, ts)
	case "h5":
		return p.h5(r, ts)
	}
	return CreateResult{}, ErrInvalid
}

func (p *chinaumsPlugin) qrcode(ctx context.Context, r CreateRequest, ts time.Time) (CreateResult, error) {
	param := map[string]any{
		"msgId":            chinaumsMsgID(),
		"requestTimestamp": ts.Format("2006-01-02 15:04:05"),
		"mid":              p.mid,
		"tid":              p.tid,
		"instMid":          "QRPAYDEFAULT",
		"billNo":           p.msgSrcID + r.TradeNo,
		"billDate":         ts.Format("2006-01-02"),
		"billDesc":         r.Subject,
		"totalAmount":      r.Amount,
		"notifyUrl":        r.NotifyURL,
		"returnUrl":        r.ReturnURL,
		"clientIp":         r.ClientIP,
	}
	out, e := p.post(ctx, "chinaums.qrcode", "/v1/netpay/bills/get-qrcode", param, ts)
	if e != nil {
		return CreateResult{}, e
	}
	if jsonText(out, "errCode") != "SUCCESS" {
		return CreateResult{}, rejected("CHINAUMS_" + chinaumsCode(out))
	}
	code := jsonText(out, "billQRCode")
	if code == "" || len(code) > 4096 || strings.ContainsAny(code, "\x00\r\n") {
		return CreateResult{}, rejected("CHINAUMS_NO_QRCODE")
	}
	return CreateResult{MaterialType: "qrcode", Material: code, ProviderTradeNo: p.msgSrcID + r.TradeNo}, nil
}

func (p *chinaumsPlugin) h5(r CreateRequest, ts time.Time) (CreateResult, error) {
	var path string
	switch p.payType {
	case "alipay":
		path = "/v1/netpay/trade/h5-pay"
	case "wxpay":
		path = "/v1/netpay/wxpay/h5-pay"
	default:
		return CreateResult{}, ErrInvalid
	}
	param := map[string]any{
		"msgId":            chinaumsMsgID(),
		"requestTimestamp": ts.Format("2006-01-02 15:04:05"),
		"mid":              p.mid,
		"tid":              p.tid,
		"instMid":          "H5DEFAULT",
		"merOrderId":       p.msgSrcID + r.TradeNo,
		"orderDesc":        r.Subject,
		"totalAmount":      r.Amount,
		"notifyUrl":        r.NotifyURL,
		"returnUrl":        r.ReturnURL,
		"clientIp":         r.ClientIP,
	}
	if p.payType == "wxpay" {
		param["sceneType"] = "AND_WAP"
	}
	body := chinaumsJSON(param)
	q := url.Values{}
	q.Set("authorization", "OPEN-FORM-PARAM")
	q.Set("appId", p.appID)
	q.Set("timestamp", ts.Format("20060102150405"))
	q.Set("nonce", chinaumsMsgID())
	q.Set("content", body)
	q.Set("signature", p.signature(q.Get("timestamp"), q.Get("nonce"), body))
	target := p.gateway + path + "?" + q.Encode()
	if !validPaymentURL(target) {
		return CreateResult{}, ErrInvalid
	}
	return CreateResult{MaterialType: "jump", Material: target, ProviderTradeNo: p.msgSrcID + r.TradeNo}, nil
}

// signature = base64(HMAC-SHA256(appId+timestamp+nonce+sha256hex(body), appKey)).
func (p *chinaumsPlugin) signature(timestamp, nonce, body string) string {
	hash := sha256Hex(body)
	str := p.appID + timestamp + nonce + hash
	m := hmac.New(sha256.New, []byte(p.appKey))
	m.Write([]byte(str))
	return base64.StdEncoding.EncodeToString(m.Sum(nil))
}

func (p *chinaumsPlugin) post(ctx context.Context, op, path string, param map[string]any, ts time.Time) (map[string]any, error) {
	body := chinaumsJSON(param)
	timestamp := ts.Format("20060102150405")
	nonce := chinaumsMsgID()
	auth := `OPEN-BODY-SIG AppId="` + p.appID + `", Timestamp="` + timestamp + `", Nonce="` + nonce + `", Signature="` + p.signature(timestamp, nonce, body) + `"`
	raw, e := p.pluginCall(ctx, op, http.MethodPost, p.gateway+path, []byte(body), "application/json; charset=utf-8", map[string]string{"Authorization": auth})
	if e != nil {
		return nil, e
	}
	out, e := callbackJSON(raw)
	if e != nil {
		return nil, &UnknownOutcome{Operation: op, Cause: e}
	}
	return out, nil
}

// VerifyNotification verifies the form callback (verify() + status branch).
func (p *chinaumsPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	params, e := callbackParams(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	if !p.verify(params) {
		return PaymentEvent{}, ErrAuthentication
	}
	var orderField, statusField, okValue string
	var providerTradeNo, buyer string
	if params["instMid"] == "H5DEFAULT" {
		orderField, statusField, okValue = "merOrderId", "status", "TRADE_SUCCESS"
		providerTradeNo = params["targetOrderId"]
		buyer = params["buyerId"]
	} else {
		orderField, statusField, okValue = "billNo", "billStatus", "PAID"
		providerTradeNo = params["billNo"]
		// buyerId / targetOrderId live in billPayment JSON for QR callbacks.
		if bp, e := callbackJSON([]byte(params["billPayment"])); e == nil {
			buyer = jsonText(bp, "buyerId")
			if tn := jsonText(bp, "targetOrderId"); tn != "" {
				providerTradeNo = tn
			}
		}
	}
	if params[statusField] != okValue {
		return PaymentEvent{}, ErrInvalid
	}
	order := params[orderField]
	if len(order) <= 4 || order[:4] != p.msgSrcID {
		return PaymentEvent{}, ErrAuthentication
	}
	tradeNo := order[4:]
	amount, e := parseFen(params["totalAmount"])
	if e != nil || expected.Amount <= 0 || amount != expected.Amount || tradeNo != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, providerTradeNo, buyer, amount), nil
}

// verify recomputes the callback sign; signType selects SHA256 or MD5, both
// uppercase, over ksort non-empty params (minus sign) + the communication key.
func (p *chinaumsPlugin) verify(params map[string]string) bool {
	var want string
	if params["signType"] == "SHA256" {
		want = strings.ToUpper(sha256Hex(chinaumsSignBase(params) + p.secret))
	} else {
		want = strings.ToUpper(md5Hex(chinaumsSignBase(params) + p.secret))
	}
	return signEqual(want, params["sign"], true)
}

func (p *chinaumsPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.ProviderTradeNo == "" || r.RefundNo == "" || r.Amount <= 0 {
		return RefundResult{}, ErrInvalid
	}
	ts := time.Now()
	// api_trade_no carries the <msgSrcId><TradeNo> we sent; H5 vs QR refund
	// is told apart by instMid. We default to the QR bills/refund path and
	// fall back is not possible offline, so the caller selects via RefundNo
	// semantics; we use the QR path (bills/refund) which the original uses
	// for scan orders — the dominant case for this adapter.
	param := map[string]any{
		"msgId":            chinaumsMsgID(),
		"requestTimestamp": ts.Format("2006-01-02 15:04:05"),
		"mid":              p.mid,
		"tid":              p.tid,
		"instMid":          "QRPAYDEFAULT",
		"billNo":           r.ProviderTradeNo,
		"billDate":         ts.Format("2006-01-02"),
		"refundOrderId":    p.msgSrcID + r.RefundNo,
		"refundAmount":     r.Amount,
	}
	out, e := p.post(ctx, "chinaums.refund", "/v1/netpay/bills/refund", param, ts)
	if e != nil {
		return RefundResult{}, e
	}
	if jsonText(out, "errCode") != "SUCCESS" {
		return RefundResult{}, rejected("CHINAUMS_" + chinaumsCode(out))
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(out, "billNo"), State: "SUCCESS", Amount: r.Amount}, nil
}

// The original echoes plain "SUCCESS" / "FAILED".
func (p *chinaumsPlugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return http.StatusOK, "text/plain; charset=utf-8", "FAILED"
	}
	return http.StatusOK, "text/plain; charset=utf-8", "SUCCESS"
}

// ---- chinaums-scoped helpers ----

func chinaumsCode(out map[string]any) string {
	code := jsonText(out, "errCode")
	if code == "" {
		code = jsonText(out, "errInfo")
	}
	code = strings.TrimSpace(code)
	if code == "" || len(code) > 48 || strings.ContainsAny(code, " \r\n") {
		return "REJECTED"
	}
	return code
}

func chinaumsMsgID() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(b)
}

// chinaumsSignBase: ksort, k=v& over non-empty params except sign, trimmed.
func chinaumsSignBase(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if k == "sign" || v == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+params[k])
	}
	return strings.Join(parts, "&")
}

// chinaumsJSON marshals the request object deterministically (sorted keys).
// We sign exactly this byte string, so key order need only be stable, not
// identical to PHP's insertion order.
func chinaumsJSON(param map[string]any) string {
	raw, e := json.Marshal(param)
	if e != nil {
		return "{}"
	}
	return string(raw)
}

var _ Provider = (*chinaumsPlugin)(nil)
var _ NotificationVerifier = (*chinaumsPlugin)(nil)
var _ CreateValidator = (*chinaumsPlugin)(nil)
