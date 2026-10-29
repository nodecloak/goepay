package payment

// Port of epay_release/plugins/alipayrp (支付宝现金红包). Uses the Alipay fund
// transfer APIs on the OpenAPI gateway with RSA2 signing.
//
// Payment (pagepay): the original signs an alipay.fund.trans.app.pay request
// (aopSdkExecute -> http_build_query of the signed system params), wraps it in
// an `alipays://platformapi/startApp?...&orderSuffix=<encoded params>` deep
// link, and opens it inside the Alipay client. The payer's bound Alipay UID
// (from the platform's OAuth) is carried as payer_binded_alipay_uid. GOEPAY
// supplies that payer UID in r.OpenID, so this is a jsapi-style method that
// returns a "urlscheme" material. No network call at create time.
//
// SCOPE (per brief: payment/refund/query):
//   - jsapi -> alipay.fund.trans.app.pay  -> urlscheme deep link
//   - Query -> alipay.fund.trans.common.query (STD_RED_PACKET)
//   - Refund-> alipay.fund.trans.refund (redPacketRefund)
//   - VerifyNotification: the signed "alipay.fund.trans.order.changed" message
//     (RSA-verified biz_content), accepting only a SUCCESS STD_RED_PACKET.
// OUT OF SCOPE (documented): the post-notify redPacketTansfer PAYOUT/settle to
// the merchant (a transfer, explicitly excluded), balance/quota queries,
// transfer-to-account/bankcard, Close (none).

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

const alipayrpGateway = "https://openapi.alipay.com/gateway.do"

var alipayrpErrUnreadable = errors.New("unreadable or unverifiable alipay response")

func alipayrpTimestamp() string {
	loc, e := time.LoadLocation("Asia/Shanghai")
	if e != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	return time.Now().In(loc).Format("2006-01-02 15:04:05")
}

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "alipayrp",
		Title:  "支付宝现金红包",
		Author: "支付宝",
		Link:   "https://b.alipay.com/signing/productSetV2.htm",
		Types:  []string{"alipay"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "应用APPID", Required: true},
			{Key: "appsecret", Label: "应用私钥", Secret: true, Textarea: true, Required: true},
			{Key: "appkey", Label: "支付宝公钥", Note: "回调验签使用", Textarea: true, Required: true},
			{Key: "appmchid", Label: "收款方支付宝UID", Note: "留空则使用商户绑定的支付宝UID"},
		},
		Note:  "支付宝现金红包。移植实现下单(红包转账deep link)/退款/查询与到账回调验签；回调成功后对商户的红包转账结算(redPacketTansfer)属转账范畴，不在本移植范围内。需签约现金红包。",
		Build: newAlipayrpPlugin,
	})
}

type alipayrpPlugin struct {
	pluginBase
	appID string
	priv  *rsa.PrivateKey
	pub   *rsa.PublicKey
}

func newAlipayrpPlugin(c PluginChannel) (Provider, error) {
	appID := strings.TrimSpace(c.Config["appid"])
	if appID == "" || len(appID) > 32 || strings.ContainsAny(appID, "&=?# ") {
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
	base, e := newPluginBase(c, pluginAccount(alipayrpGateway, appID))
	if e != nil {
		return nil, e
	}
	return &alipayrpPlugin{pluginBase: base, appID: appID, priv: priv, pub: pub}, nil
}

func (p *alipayrpPlugin) methods() []string { return []string{"jsapi"} }

func (p *alipayrpPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Query: true, Refund: true}
}

func (p *alipayrpPlugin) ValidateCreate(r CreateRequest) error {
	if e := validatePluginCreate(r, p.methods()); e != nil {
		return e
	}
	// The red-packet transfer requires the payer's bound Alipay UID.
	if r.OpenID == "" || len(r.OpenID) > 64 || strings.ContainsAny(r.OpenID, "&=?# ") {
		return ErrInvalid
	}
	return nil
}

// Create signs an alipay.fund.trans.app.pay request and returns the Alipay
// client deep link (urlscheme). No network call.
func (p *alipayrpPlugin) Create(_ context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	businessParams, _ := json.Marshal(map[string]string{"sub_biz_scene": "REDPACKET", "payer_binded_alipay_uid": r.OpenID})
	biz := map[string]any{
		"out_biz_no":      r.TradeNo,
		"trans_amount":    yuan(r.Amount),
		"product_code":    "STD_RED_PACKET",
		"biz_scene":       "PERSONAL_PAY",
		"order_title":     r.Subject,
		"business_params": string(businessParams),
	}
	params, e := p.sysParams("alipay.fund.trans.app.pay", biz, r.NotifyURL, "")
	if e != nil {
		return CreateResult{}, e
	}
	// aopSdkExecute: the SDK request string is http_build_query of the signed
	// system params; the plugin wraps it as orderSuffix in the app scheme.
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	scheme := "alipays://platformapi/startApp?appId=20000125&orderSuffix=" + url.QueryEscape(form.Encode()) +
		"#Intent;scheme=alipays;package=com.eg.android.AlipayGphone;end"
	return CreateResult{MaterialType: "urlscheme", Material: scheme}, nil
}

func (p *alipayrpPlugin) sysParams(method string, biz map[string]any, notifyURL, returnURL string) (map[string]string, error) {
	bizJSON, e := json.Marshal(biz)
	if e != nil {
		return nil, ErrInvalid
	}
	params := map[string]string{
		"app_id": p.appID, "method": method, "format": "json", "charset": "UTF-8",
		"sign_type": "RSA2", "version": "1.0", "timestamp": alipayrpTimestamp(),
		"biz_content": string(bizJSON),
	}
	if notifyURL != "" {
		params["notify_url"] = notifyURL
	}
	if returnURL != "" {
		params["return_url"] = returnURL
	}
	sig, e := rsaSign(p.priv, crypto.SHA256, alipayrpSignContent(params))
	if e != nil {
		return nil, e
	}
	params["sign"] = sig
	return params, nil
}

func (p *alipayrpPlugin) execute(ctx context.Context, method string, biz map[string]any) ([]byte, error) {
	params, e := p.sysParams(method, biz, "", "")
	if e != nil {
		return nil, e
	}
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	return p.postForm(ctx, "alipayrp."+method, alipayrpGateway+"?charset=UTF-8", form)
}

func (p *alipayrpPlugin) resultNode(raw []byte, method string) (map[string]any, error) {
	node := strings.ReplaceAll(method, ".", "_") + "_response"
	outer, e := callbackJSON(raw)
	if e != nil {
		return nil, &UnknownOutcome{Operation: "alipayrp." + method, Cause: alipayrpErrUnreadable}
	}
	presentNode := node
	if _, ok := outer[node]; !ok {
		if _, isErr := outer["error_response"]; isErr {
			presentNode = "error_response"
		}
	}
	signData, ok := alipayrpResponseSignData(raw, presentNode)
	sig := jsonText(outer, "sign")
	if !ok || sig == "" || !rsaVerify(p.pub, crypto.SHA256, signData, sig) {
		return nil, &UnknownOutcome{Operation: "alipayrp." + method, Cause: alipayrpErrUnreadable}
	}
	inner, ok := outer[node].(map[string]any)
	if !ok {
		if errNode, ok := outer["error_response"].(map[string]any); ok {
			return nil, rejected("ALIPAY_" + jsonText(errNode, "code"))
		}
		return nil, &UnknownOutcome{Operation: "alipayrp." + method, Cause: alipayrpErrUnreadable}
	}
	if jsonText(inner, "code") != "10000" {
		return nil, rejected("ALIPAY_" + jsonText(inner, "sub_code"))
	}
	return inner, nil
}

func (p *alipayrpPlugin) Query(ctx context.Context, r QueryRequest) (QueryResult, error) {
	if r.TradeNo == "" {
		return QueryResult{}, ErrInvalid
	}
	biz := map[string]any{"out_biz_no": r.TradeNo, "product_code": "STD_RED_PACKET", "biz_scene": "PERSONAL_PAY"}
	raw, e := p.execute(ctx, "alipay.fund.trans.common.query", biz)
	if e != nil {
		return QueryResult{}, e
	}
	data, e := p.resultNode(raw, "alipay.fund.trans.common.query")
	if e != nil {
		return QueryResult{}, e
	}
	if jsonText(data, "out_biz_no") != r.TradeNo {
		return QueryResult{}, ErrAuthentication
	}
	amount, e := parseYuan(jsonText(data, "trans_amount"))
	if e != nil {
		return QueryResult{}, ErrAuthentication
	}
	state := "NOTPAY"
	switch jsonText(data, "status") {
	case "SUCCESS":
		state = "SUCCESS"
	case "REFUND":
		state = "REFUND"
	case "FAIL", "CLOSED":
		state = "CLOSED"
	}
	return QueryResult{TradeNo: r.TradeNo, ProviderTradeNo: jsonText(data, "order_id"), State: state, Currency: "CNY", Amount: amount}, nil
}

func (p *alipayrpPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.ProviderTradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	biz := map[string]any{"order_id": r.ProviderTradeNo, "out_request_no": r.RefundNo, "refund_amount": yuan(r.Amount)}
	raw, e := p.execute(ctx, "alipay.fund.trans.refund", biz)
	if e != nil {
		return RefundResult{}, e
	}
	data, e := p.resultNode(raw, "alipay.fund.trans.refund")
	if e != nil {
		return RefundResult{}, e
	}
	state := "SUCCESS"
	if s := jsonText(data, "status"); s != "" && s != "SUCCESS" {
		state = "PROCESSING"
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(data, "refund_order_id"), State: state, Amount: r.Amount}, nil
}

// VerifyNotification verifies the fund-order-changed message: $_POST is RSA
// verified (ksort join), the message must be alipay.fund.trans.order.changed,
// and biz_content must describe a SUCCESS STD_RED_PACKET/PERSONAL_PAY order
// matching our trade number and amount. The merchant payout (redPacketTansfer)
// that the original performs afterwards is OUT OF SCOPE here.
func (p *alipayrpPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	params, e := callbackParams(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	if params["app_id"] != "" && params["app_id"] != p.appID {
		return PaymentEvent{}, ErrAuthentication
	}
	if !alipayrpVerifyNotify(p.pub, params) {
		return PaymentEvent{}, ErrAuthentication
	}
	if params["msg_method"] != "alipay.fund.trans.order.changed" {
		return PaymentEvent{}, ErrInvalid
	}
	biz, e := callbackJSON([]byte(params["biz_content"]))
	if e != nil {
		return PaymentEvent{}, ErrInvalid
	}
	if jsonText(biz, "product_code") != "STD_RED_PACKET" || jsonText(biz, "biz_scene") != "PERSONAL_PAY" {
		return PaymentEvent{}, ErrInvalid
	}
	if jsonText(biz, "status") != "SUCCESS" {
		return PaymentEvent{}, ErrInvalid
	}
	amount, e := parseYuan(jsonText(biz, "trans_amount"))
	if e != nil || expected.Amount <= 0 || amount != expected.Amount || jsonText(biz, "out_biz_no") != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, jsonText(biz, "order_id"), "", amount), nil
}

func alipayrpSignContent(params map[string]string) string {
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

func alipayrpVerifyNotify(pub *rsa.PublicKey, params map[string]string) bool {
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

func alipayrpResponseSignData(raw []byte, node string) (string, bool) {
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

var _ Provider = (*alipayrpPlugin)(nil)
var _ NotificationVerifier = (*alipayrpPlugin)(nil)
var _ CreateValidator = (*alipayrpPlugin)(nil)
