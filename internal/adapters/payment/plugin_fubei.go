package payment

// Port of epay_release/plugins/fubei (付呗聚合支付). FubeiClient wraps a
// request as a JSON object {app_id, method, format, sign_method, nonce,
// version, biz_content, sign} POSTed to
// https://shq-api.51fubei.com/gateway/agent. The sign is
//   upper(md5( ksort("k=v&...", skip sign & empty) . app_secret ))
// over the COMMON parameters (biz_content is already a JSON string field).
// Alipay JSAPI/wxpay JSAPI use fbpay.order.create -> {prepay_id | sign_package};
// alipay H5 uses fbpay.order.wap.create -> {html} (a URL or a form);
// unionpay QR uses fbpay.order.create with pay_type=unionpay -> code url.
// Callbacks are a form POST ({data, sign, ...}) verified with the same sign
// over all posted fields; ack is a plain "success".
//
// Out of scope (reported): the wxconfig / wxconfig.query merchant tool, the
// wechat网页授权 (openapi.agent.merchant.wechat.payment.auth) flow, and the
// wxplugin mini-program entry. Refund (fbpay.order.refund) is implemented.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
)

const fubeiGateway = "https://shq-api.51fubei.com/gateway/agent"

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "fubei",
		Title:  "付呗聚合支付",
		Author: "付呗",
		Link:   "https://www.51fubei.com/",
		Types:  []string{"alipay", "wxpay", "bank"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "开放平台ID", Required: true},
			{Key: "appkey", Label: "接口密钥", Secret: true, Required: true},
			{Key: "appmchid", Label: "门店ID", Required: true},
			{Key: "mchid", Label: "商户ID", Required: true},
		},
		AppTypes: []PluginOption{{Value: "1", Label: "生活号支付"}, {Value: "2", Label: "H5支付"}},
		Build:    newFubeiPlugin,
	})
}

type fubeiPlugin struct {
	pluginBase
	appID, appSecret, storeID, mchID string
	apps                             []string
	gateway                          string
}

func newFubeiPlugin(c PluginChannel) (Provider, error) {
	appID := strings.TrimSpace(c.Config["appid"])
	appSecret := c.Config["appkey"]
	storeID := strings.TrimSpace(c.Config["appmchid"])
	mchID := strings.TrimSpace(c.Config["mchid"])
	if appID == "" || len(appID) > 64 || appSecret == "" || len(appSecret) > 256 || storeID == "" || len(storeID) > 64 || mchID == "" || len(mchID) > 64 {
		return nil, ErrInvalid
	}
	base, e := newPluginBase(c, pluginAccount(fubeiGateway, mchID, storeID))
	if e != nil {
		return nil, e
	}
	return &fubeiPlugin{pluginBase: base, appID: appID, appSecret: appSecret, storeID: storeID, mchID: mchID, apps: c.AppTypes, gateway: fubeiGateway}, nil
}

// methods: alipay offers JSAPI (生活号, app "1") and H5 (app "2"); wxpay and
// bank are JSAPI-only in the mapi flow (wx needs openid; bank QR via create).
func (p *fubeiPlugin) methods() []string {
	switch p.payType {
	case "alipay":
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
			m = append(m, "jsapi")
		}
		if has("2") {
			m = append(m, "h5")
		}
		if len(m) == 0 {
			m = []string{"jsapi"}
		}
		return m
	case "wxpay":
		return []string{"jsapi"}
	case "bank":
		return []string{"native"}
	}
	return nil
}

func (p *fubeiPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

func (p *fubeiPlugin) ValidateCreate(r CreateRequest) error {
	if e := validatePluginCreate(r, p.methods()); e != nil {
		return e
	}
	if r.Method == "jsapi" && p.payType != "bank" && strings.TrimSpace(r.OpenID) == "" {
		return ErrInvalid
	}
	return nil
}

// sign reproduces make_sign: ksort, k=v joined by '&' skipping sign/empty,
// then append app_secret, md5, uppercase.
func (p *fubeiPlugin) sign(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if k == "sign" || v == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(params[k])
		sb.WriteByte('&')
	}
	s := sb.String()
	s = strings.TrimSuffix(s, "&")
	return strings.ToUpper(md5Hex(s + p.appSecret))
}

func (p *fubeiPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	switch {
	case p.payType == "alipay" && r.Method == "h5":
		return p.createAlipayH5(ctx, r)
	case p.payType == "bank":
		return p.createUnionQR(ctx, r)
	default: // jsapi (alipay生活号 / wxpay公众号)
		return p.createJSAPI(ctx, r)
	}
}

func (p *fubeiPlugin) createJSAPI(ctx context.Context, r CreateRequest) (CreateResult, error) {
	payType := "alipay"
	if p.payType == "wxpay" {
		payType = "wxpay"
	}
	biz := map[string]any{
		"merchant_id":       p.mchID,
		"merchant_order_sn": r.TradeNo,
		"pay_type":          payType,
		"total_amount":      yuan(r.Amount),
		"store_id":          p.storeID,
		"user_id":           r.OpenID,
		"body":              r.Subject,
		"notify_url":        r.NotifyURL,
	}
	data, e := p.execute(ctx, "fubei.create", "fbpay.order.create", biz)
	if e != nil {
		return CreateResult{}, e
	}
	if p.payType == "wxpay" {
		pkg, ok := data["sign_package"]
		if !ok {
			return CreateResult{}, rejected("FUBEI_NO_SIGN_PACKAGE")
		}
		payload, e := json.Marshal(pkg)
		if e != nil || len(payload) == 0 || len(payload) > 8192 {
			return CreateResult{}, rejected("FUBEI_BAD_SIGN_PACKAGE")
		}
		return CreateResult{MaterialType: "jsapi", Material: string(payload), ProviderTradeNo: jsonMapText(data, "order_sn")}, nil
	}
	prepay := jsonMapText(data, "prepay_id")
	if prepay == "" {
		return CreateResult{}, rejected("FUBEI_NO_PREPAY_ID")
	}
	return CreateResult{MaterialType: "jsapi", Material: prepay, ProviderTradeNo: jsonMapText(data, "order_sn")}, nil
}

func (p *fubeiPlugin) createAlipayH5(ctx context.Context, r CreateRequest) (CreateResult, error) {
	biz := map[string]any{
		"merchant_id":       p.mchID,
		"merchant_order_sn": r.TradeNo,
		"total_amount":      yuan(r.Amount),
		"store_id":          p.storeID,
		"body":              r.Subject,
		"user_ip":           r.ClientIP,
		"notify_url":        r.NotifyURL,
		"return_url":        r.ReturnURL,
	}
	data, e := p.execute(ctx, "fubei.wap", "fbpay.order.wap.create", biz)
	if e != nil {
		return CreateResult{}, e
	}
	html := jsonMapText(data, "html")
	if html == "" {
		return CreateResult{}, rejected("FUBEI_NO_HTML")
	}
	if strings.HasPrefix(html, "http") {
		if res, ok := materialFromLinks(html, "", ""); ok {
			return res, nil
		}
		return CreateResult{}, rejected("FUBEI_BAD_URL")
	}
	// The original returns the raw HTML form body for the checkout page.
	return CreateResult{MaterialType: "html", Material: html, ProviderTradeNo: jsonMapText(data, "order_sn")}, nil
}

func (p *fubeiPlugin) createUnionQR(ctx context.Context, r CreateRequest) (CreateResult, error) {
	biz := map[string]any{
		"merchant_id":       p.mchID,
		"merchant_order_sn": r.TradeNo,
		"pay_type":          "unionpay",
		"total_amount":      yuan(r.Amount),
		"store_id":          p.storeID,
		"user_id":           "",
		"body":              r.Subject,
		"notify_url":        r.NotifyURL,
	}
	data, e := p.execute(ctx, "fubei.union", "fbpay.order.create", biz)
	if e != nil {
		return CreateResult{}, e
	}
	// The original uses the returned data string directly as the QR content.
	code := jsonMapText(data, "qr_code")
	if code == "" {
		code = jsonMapText(data, "code_url")
	}
	if res, ok := materialFromLinks("", code, ""); ok {
		res.ProviderTradeNo = jsonMapText(data, "order_sn")
		return res, nil
	}
	return CreateResult{}, rejected("FUBEI_NO_QRCODE")
}

// execute signs and POSTs the common envelope, returning the data object.
func (p *fubeiPlugin) execute(ctx context.Context, operation, method string, biz map[string]any) (map[string]any, error) {
	bizJSON, e := json.Marshal(biz)
	if e != nil {
		return nil, ErrInvalid
	}
	common := map[string]string{
		"app_id":      p.appID,
		"method":      method,
		"format":      "json",
		"sign_method": "md5",
		"nonce":       fubeiNonce(),
		"version":     "1.0",
		"biz_content": string(bizJSON),
	}
	common["sign"] = p.sign(common)
	payload := map[string]any{}
	for k, v := range common {
		payload[k] = v
	}
	raw, e := p.postJSON(ctx, operation, p.gateway, payload, map[string]string{"Content-Type": "application/json; charset=utf-8"})
	if e != nil {
		return nil, e
	}
	out, e := callbackJSON(raw)
	if e != nil {
		return nil, &UnknownOutcome{Operation: operation, Cause: e}
	}
	if jsonText(out, "result_code") == "200" {
		data, ok := out["data"].(map[string]any)
		if !ok {
			return nil, &UnknownOutcome{Operation: operation, Cause: errFubeiNoData}
		}
		return data, nil
	}
	code := jsonText(out, "result_code")
	if code == "" {
		return nil, &UnknownOutcome{Operation: operation, Cause: errFubeiNoCode}
	}
	return nil, rejected("FUBEI_" + code)
}

var errFubeiNoData = errorString("fubei missing data")
var errFubeiNoCode = errorString("fubei missing result_code")

// fubeiNonce builds a 12-char random nonce. It is sent and signed, never
// reproduced by our verification, so tests recompute the sign from the
// captured request params rather than from a fixed nonce.
func fubeiNonce() string {
	var b [6]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "000000000000"
	}
	return hex.EncodeToString(b[:])
}

func (p *fubeiPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	params, e := callbackParams(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	got := params["sign"]
	if !signEqual(p.sign(params), got, false) {
		return PaymentEvent{}, ErrAuthentication
	}
	data, e := callbackJSON([]byte(params["data"]))
	if e != nil {
		return PaymentEvent{}, ErrInvalid
	}
	if jsonText(data, "order_status") != "SUCCESS" {
		return PaymentEvent{}, ErrInvalid
	}
	if jsonText(data, "merchant_order_sn") != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	amount, e := parseYuan(jsonText(data, "total_amount"))
	if e != nil || expected.Amount <= 0 || amount != expected.Amount {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, jsonText(data, "order_sn"), jsonText(data, "user_id"), amount), nil
}

func (p *fubeiPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.ProviderTradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	biz := map[string]any{
		"order_sn":           r.ProviderTradeNo,
		"merchant_refund_sn": r.RefundNo,
		"refund_amount":      yuan(r.Amount),
	}
	data, e := p.execute(ctx, "fubei.refund", "fbpay.order.refund", biz)
	if e != nil {
		return RefundResult{}, e
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(data, "merchant_order_sn"), State: "SUCCESS", Amount: r.Amount}, nil
}

var _ Provider = (*fubeiPlugin)(nil)
var _ NotificationVerifier = (*fubeiPlugin)(nil)
var _ CreateValidator = (*fubeiPlugin)(nil)
