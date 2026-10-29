package payment

// Port of epay_release/plugins/alipaysl (支付宝官方支付服务商版). Same Alipay
// OpenAPI gateway + RSA2 signing as the direct version, but SERVICE-PROVIDER
// mode: every request carries app_auth_token (the merchant's authorization to
// the ISV's third-party app). There is no 直付通 sub_merchant/settle_info.
//
// The console input holding the token is keyed "appmchid" (labelled
// 商户授权token). We keep that original key name. NOTE: this is conceptually
// Alipay's app_auth_token, but the channel-config key is "appmchid", so it
// does NOT collide with channelconfig.directIdentityChannel()'s reserved
// "app_auth_token" key — no conflict to report for this plugin.
//
// SCOPE: payment + refund + close + query.
//   - native -> alipay.trade.precreate (qrcode)
//   - page   -> alipay.trade.page.pay  (html form)
//   - wap    -> alipay.trade.wap.pay   (html form)
//   - Query/Close/Refund via alipay.trade.query/close/refund
// OUT OF SCOPE: 付款码(scanpay, needs order auth_code), jsapi/app/mini/预授权,
// transfers, complaint/applyment appgw callbacks. Public-key-certificate mode
// is not ported (plaintext 支付宝公钥 only).

import (
	"context"
	"crypto"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const alipayslGateway = "https://openapi.alipay.com/gateway.do"

var alipayslErrUnreadable = errors.New("unreadable or unverifiable alipay response")

func alipayslTimestamp() string {
	loc, e := time.LoadLocation("Asia/Shanghai")
	if e != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	return time.Now().In(loc).Format("2006-01-02 15:04:05")
}

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "alipaysl",
		Title:  "支付宝官方支付服务商版",
		Author: "支付宝",
		Link:   "https://b.alipay.com/signing/productSetV2.htm",
		Types:  []string{"alipay"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "应用APPID", Note: "必须使用第三方应用", Required: true},
			{Key: "appkey", Label: "支付宝公钥", Note: "填错也可以支付成功但会无法回调", Textarea: true, Required: true},
			{Key: "appsecret", Label: "应用私钥", Secret: true, Textarea: true, Required: true},
			{Key: "appmchid", Label: "商户授权token", Note: "在第三方应用-商家授权页面获取(app_auth_token)", Required: true},
		},
		AppTypes: []PluginOption{
			{Value: "1", Label: "电脑网站支付"},
			{Value: "2", Label: "手机网站支付"},
			{Value: "3", Label: "当面付扫码"},
			{Value: "8", Label: "订单码支付"},
		},
		Note:  "支付宝服务商(第三方应用)版。移植仅实现下单/退款/关闭/查询；付款码、JSAPI/APP/小程序/预授权、转账、投诉/进件回调不在范围内。公钥证书模式未移植。",
		Build: newAlipayslPlugin,
	})
}

type alipayslPlugin struct {
	pluginBase
	appID, appAuthToken string
	priv                *rsa.PrivateKey
	pub                 *rsa.PublicKey
	appTypes            map[string]bool
}

func newAlipayslPlugin(c PluginChannel) (Provider, error) {
	appID := strings.TrimSpace(c.Config["appid"])
	if appID == "" || len(appID) > 32 || strings.ContainsAny(appID, "&=?# ") {
		return nil, ErrInvalid
	}
	token := strings.TrimSpace(c.Config["appmchid"])
	if token == "" || len(token) > 64 || strings.ContainsAny(token, "&=?# ") {
		return nil, ErrInvalid
	}
	priv, e := pluginPrivateKey(c.Config["appsecret"])
	if e != nil {
		return nil, e
	}
	pub, e := pluginPublicKey(c.Config["appkey"])
	if e != nil {
		return nil, e
	}
	// app_auth_token is a credential; the account identity uses only the
	// gateway host and the ISV app id.
	base, e := newPluginBase(c, pluginAccount(alipayslGateway, appID))
	if e != nil {
		return nil, e
	}
	apptypes := map[string]bool{}
	for _, t := range c.AppTypes {
		apptypes[t] = true
	}
	return &alipayslPlugin{pluginBase: base, appID: appID, appAuthToken: token, priv: priv, pub: pub, appTypes: apptypes}, nil
}

func (p *alipayslPlugin) methods() []string {
	if len(p.appTypes) == 0 {
		return []string{"page", "wap", "native"}
	}
	var m []string
	if p.appTypes["1"] {
		m = append(m, "page")
	}
	if p.appTypes["2"] {
		m = append(m, "wap")
	}
	if p.appTypes["3"] || p.appTypes["8"] {
		m = append(m, "native")
	}
	return m
}

func (p *alipayslPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Query: true, Close: true, Refund: true}
}

func (p *alipayslPlugin) ValidateCreate(r CreateRequest) error {
	return validatePluginCreate(r, p.methods())
}

func (p *alipayslPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	biz := map[string]any{
		"out_trade_no":    r.TradeNo,
		"total_amount":    yuan(r.Amount),
		"subject":         r.Subject,
		"business_params": map[string]string{"mc_create_trade_ip": r.ClientIP},
	}
	switch r.Method {
	case "native":
		if p.appTypes["8"] && !p.appTypes["3"] {
			biz["product_code"] = "QR_CODE_OFFLINE"
		}
		raw, e := p.execute(ctx, "alipay.trade.precreate", biz, r.NotifyURL, "")
		if e != nil {
			return CreateResult{}, e
		}
		data, e := p.resultNode(raw, "alipay.trade.precreate")
		if e != nil {
			return CreateResult{}, e
		}
		qr := jsonText(data, "qr_code")
		if qr == "" {
			return CreateResult{}, &UnknownOutcome{Operation: "alipaysl.precreate", Cause: alipayslErrUnreadable}
		}
		return CreateResult{MaterialType: "qrcode", Material: qr, ProviderTradeNo: jsonText(data, "out_trade_no")}, nil
	case "page":
		biz["product_code"] = "FAST_INSTANT_TRADE_PAY"
		return p.pageForm("alipay.trade.page.pay", biz, r)
	case "wap":
		biz["product_code"] = "QUICK_WAP_WAY"
		return p.pageForm("alipay.trade.wap.pay", biz, r)
	}
	return CreateResult{}, ErrInvalid
}

func (p *alipayslPlugin) pageForm(method string, biz map[string]any, r CreateRequest) (CreateResult, error) {
	params, e := p.sysParams(method, biz, r.NotifyURL, r.ReturnURL)
	if e != nil {
		return CreateResult{}, e
	}
	return CreateResult{MaterialType: "html", Material: pluginForm(alipayslGateway+"?charset=UTF-8", params, "正在跳转")}, nil
}

func (p *alipayslPlugin) sysParams(method string, biz map[string]any, notifyURL, returnURL string) (map[string]string, error) {
	bizJSON, e := json.Marshal(biz)
	if e != nil {
		return nil, ErrInvalid
	}
	params := map[string]string{
		"app_id":         p.appID,
		"method":         method,
		"format":         "json",
		"charset":        "UTF-8",
		"sign_type":      "RSA2",
		"version":        "1.0",
		"timestamp":      alipayslTimestamp(),
		"app_auth_token": p.appAuthToken,
		"biz_content":    string(bizJSON),
	}
	if notifyURL != "" {
		params["notify_url"] = notifyURL
	}
	if returnURL != "" {
		params["return_url"] = returnURL
	}
	sig, e := rsaSign(p.priv, crypto.SHA256, alipayslSignContent(params))
	if e != nil {
		return nil, e
	}
	params["sign"] = sig
	return params, nil
}

func (p *alipayslPlugin) execute(ctx context.Context, method string, biz map[string]any, notifyURL, returnURL string) ([]byte, error) {
	params, e := p.sysParams(method, biz, notifyURL, returnURL)
	if e != nil {
		return nil, e
	}
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	return p.postForm(ctx, "alipaysl."+method, alipayslGateway+"?charset=UTF-8", form)
}

func (p *alipayslPlugin) resultNode(raw []byte, method string) (map[string]any, error) {
	node := strings.ReplaceAll(method, ".", "_") + "_response"
	outer, e := callbackJSON(raw)
	if e != nil {
		return nil, &UnknownOutcome{Operation: "alipaysl." + method, Cause: alipayslErrUnreadable}
	}
	presentNode := node
	if _, ok := outer[node]; !ok {
		if _, isErr := outer["error_response"]; isErr {
			presentNode = "error_response"
		}
	}
	signData, ok := alipayslResponseSignData(raw, presentNode)
	sig := jsonText(outer, "sign")
	if !ok || sig == "" || !rsaVerify(p.pub, crypto.SHA256, signData, sig) {
		return nil, &UnknownOutcome{Operation: "alipaysl." + method, Cause: alipayslErrUnreadable}
	}
	inner, ok := outer[node].(map[string]any)
	if !ok {
		if errNode, ok := outer["error_response"].(map[string]any); ok {
			return nil, rejected("ALIPAY_" + jsonText(errNode, "code"))
		}
		return nil, &UnknownOutcome{Operation: "alipaysl." + method, Cause: alipayslErrUnreadable}
	}
	if jsonText(inner, "code") != "10000" {
		return nil, rejected("ALIPAY_" + jsonText(inner, "sub_code"))
	}
	return inner, nil
}

func (p *alipayslPlugin) Query(ctx context.Context, r QueryRequest) (QueryResult, error) {
	if r.TradeNo == "" {
		return QueryResult{}, ErrInvalid
	}
	raw, e := p.execute(ctx, "alipay.trade.query", map[string]any{"out_trade_no": r.TradeNo}, "", "")
	if e != nil {
		return QueryResult{}, e
	}
	data, e := p.resultNode(raw, "alipay.trade.query")
	if e != nil {
		return QueryResult{}, e
	}
	if jsonText(data, "out_trade_no") != r.TradeNo {
		return QueryResult{}, ErrAuthentication
	}
	amount, e := parseYuan(jsonText(data, "total_amount"))
	if e != nil {
		return QueryResult{}, ErrAuthentication
	}
	state := "NOTPAY"
	switch jsonText(data, "trade_status") {
	case "TRADE_SUCCESS", "TRADE_FINISHED":
		state = "SUCCESS"
	case "TRADE_CLOSED":
		state = "CLOSED"
	}
	buyer := jsonText(data, "buyer_user_id")
	if buyer == "" {
		buyer = jsonText(data, "buyer_open_id")
	}
	return QueryResult{TradeNo: r.TradeNo, ProviderTradeNo: jsonText(data, "trade_no"), State: state, Currency: "CNY", BuyerID: buyer, Amount: amount}, nil
}

func (p *alipayslPlugin) Close(ctx context.Context, r QueryRequest) error {
	if r.TradeNo == "" {
		return ErrInvalid
	}
	raw, e := p.execute(ctx, "alipay.trade.close", map[string]any{"out_trade_no": r.TradeNo}, "", "")
	if e != nil {
		return e
	}
	_, e = p.resultNode(raw, "alipay.trade.close")
	return e
}

func (p *alipayslPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.ProviderTradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	biz := map[string]any{"trade_no": r.ProviderTradeNo, "refund_amount": yuan(r.Amount), "out_request_no": r.RefundNo}
	raw, e := p.execute(ctx, "alipay.trade.refund", biz, "", "")
	if e != nil {
		return RefundResult{}, e
	}
	data, e := p.resultNode(raw, "alipay.trade.refund")
	if e != nil {
		return RefundResult{}, e
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(data, "trade_no"), State: "SUCCESS", Amount: r.Amount}, nil
}

func (p *alipayslPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	params, e := callbackParams(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	if params["app_id"] != "" && params["app_id"] != p.appID {
		return PaymentEvent{}, ErrAuthentication
	}
	if !alipayslVerifyNotify(p.pub, params) {
		return PaymentEvent{}, ErrAuthentication
	}
	if params["trade_status"] != "TRADE_SUCCESS" {
		return PaymentEvent{}, ErrInvalid
	}
	amount, e := parseYuan(params["total_amount"])
	if e != nil || expected.Amount <= 0 || amount != expected.Amount || params["out_trade_no"] != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	buyer := params["buyer_id"]
	if buyer == "" {
		buyer = params["buyer_open_id"]
	}
	return paidEvent(expected.TradeNo, params["trade_no"], buyer, amount), nil
}

func alipayslSignContent(params map[string]string) string {
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

func alipayslVerifyNotify(pub *rsa.PublicKey, params map[string]string) bool {
	sig := params["sign"]
	if sig == "" {
		return false
	}
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if k == "sign" || k == "sign_type" || v == "" {
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
	return rsaVerify(pub, crypto.SHA256, sb.String(), sig)
}

func alipayslResponseSignData(raw []byte, node string) (string, bool) {
	s := string(raw)
	idx := strings.Index(s, node)
	if idx < 0 {
		return "", false
	}
	start := idx + len(node) + 2
	signIdx := strings.LastIndex(s, `"sign"`)
	if signIdx < 0 || signIdx-1 <= start {
		return "", false
	}
	return s[start : signIdx-1], true
}

var _ Provider = (*alipayslPlugin)(nil)
var _ NotificationVerifier = (*alipayslPlugin)(nil)
var _ CreateValidator = (*alipayslPlugin)(nil)
