package payment

// Port of epay_release/plugins/alipayd (支付宝官方支付直付通版). Talks to the
// Alipay OpenAPI gateway (openapi.alipay.com/gateway.do) with RSA2 signing,
// exactly as the shared \Alipay\* SDK does: system params are ksort-joined
// into "k=v&..." (empty and sign skipped), signed SHA256/PKCS1v15, biz_content
// is JSON. Responses carry a "<method>_response" node that is RSA-verified
// against the Alipay public key; code "10000" is success.
//
// SCOPE (per the porting brief): payment + refund + close + query only.
//   - native  -> alipay.trade.precreate  (qrcode)
//   - page    -> alipay.trade.page.pay   (html auto-submit form)
//   - wap     -> alipay.trade.wap.pay    (html auto-submit form)
//   - Query   -> alipay.trade.query
//   - Close   -> alipay.trade.close
//   - Refund  -> alipay.trade.refund
// OUT OF SCOPE and NOT implemented here: 直付通结算确认 (settle_confirm) and
// royalty/freeze, 合单支付 (merge precreate/create + combine notify), jsapi /
// app / mini / 预授权 (those need buyer openid / SDK string / app gateway),
// transfers, and complaint/applyment business callbacks via appgw.
//
// Direct-settle (直付通) requires sub_merchant + settle_info on every order.
// That is the plugin's purpose, so Build requires the 子商户SMID (appmchid) and
// Create always attaches it; the settle CONFIRM step that the PHP notify()
// performs afterwards is out of scope (noted in the report).

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

const alipaydGateway = "https://openapi.alipay.com/gateway.do"

// alipaydErrUnreadable marks a create/refund reply the plugin could not read
// or verify: the upstream may have acted, so it becomes an *UnknownOutcome.
var alipaydErrUnreadable = errors.New("unreadable or unverifiable alipay response")

// alipaydTimestamp renders the OpenAPI request timestamp (Asia/Shanghai, the
// gateway's own zone, as the PHP SDK's date('Y-m-d H:i:s') does on a CN host).
func alipaydTimestamp() string {
	loc, e := time.LoadLocation("Asia/Shanghai")
	if e != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	return time.Now().In(loc).Format("2006-01-02 15:04:05")
}

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "alipayd",
		Title:  "支付宝官方支付直付通版",
		Author: "支付宝",
		Link:   "https://b.alipay.com/signing/productSetV2.htm",
		Types:  []string{"alipay"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "应用APPID", Required: true},
			{Key: "appkey", Label: "支付宝公钥", Note: "填错也可以支付成功但会无法回调", Textarea: true, Required: true},
			{Key: "appsecret", Label: "应用私钥", Secret: true, Textarea: true, Required: true},
			{Key: "appmchid", Label: "子商户SMID", Note: "互联网直付通子商户ID，多个用英文逗号分隔", Required: true},
		},
		AppTypes: []PluginOption{
			{Value: "1", Label: "电脑网站支付"},
			{Value: "2", Label: "手机网站支付"},
			{Value: "3", Label: "当面付扫码"},
			{Value: "8", Label: "订单码支付"},
		},
		Note:  "互联网平台直付通。本移植仅实现下单/退款/关闭/查询；结算确认(settle)、分账冻结、合单支付、JSAPI/APP/小程序/预授权不在范围内。公钥证书模式未移植。",
		Build: newAlipaydPlugin,
	})
}

type alipaydPlugin struct {
	pluginBase
	appID string
	// smid: the first configured 子商户SMID. The PHP SDK picks a random one
	// when several are comma-joined; we bind the channel to a single stable
	// SMID so the account identity and every order are deterministic.
	smid     string
	priv     *rsa.PrivateKey
	pub      *rsa.PublicKey
	appTypes map[string]bool
}

func newAlipaydPlugin(c PluginChannel) (Provider, error) {
	appID := strings.TrimSpace(c.Config["appid"])
	if appID == "" || len(appID) > 32 || strings.ContainsAny(appID, "&=?# ") {
		return nil, ErrInvalid
	}
	smidRaw := strings.TrimSpace(c.Config["appmchid"])
	smid := smidRaw
	if i := strings.IndexByte(smid, ','); i >= 0 {
		smid = strings.TrimSpace(smid[:i])
	}
	if smid == "" || len(smid) > 64 || strings.ContainsAny(smid, "&=?# ") {
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
	base, e := newPluginBase(c, pluginAccount(alipaydGateway, appID, smid))
	if e != nil {
		return nil, e
	}
	apptypes := map[string]bool{}
	for _, t := range c.AppTypes {
		apptypes[t] = true
	}
	return &alipaydPlugin{pluginBase: base, appID: appID, smid: smid, priv: priv, pub: pub, appTypes: apptypes}, nil
}

// methods maps the original 'select' (apptype) products to the GOEPAY methods
// this port actually serves. When no AppTypes are configured (the console
// left it empty) we offer the server-constructable set.
func (p *alipaydPlugin) methods() []string {
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

func (p *alipaydPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Query: true, Close: true, Refund: true}
}

func (p *alipaydPlugin) ValidateCreate(r CreateRequest) error {
	return validatePluginCreate(r, p.methods())
}

// directSettle appends the 直付通 sub_merchant + settle_info that every order
// under this product must carry (SDK directPayParams).
func (p *alipaydPlugin) directSettle(biz map[string]any, amount string) {
	biz["sub_merchant"] = map[string]string{"merchant_id": p.smid}
	biz["settle_info"] = map[string]any{
		"settle_period_time": "1d",
		"settle_detail_infos": []map[string]string{
			{"trans_in_type": "defaultSettle", "amount": amount},
		},
	}
}

func (p *alipaydPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	amount := yuan(r.Amount)
	biz := map[string]any{
		"out_trade_no":    r.TradeNo,
		"total_amount":    amount,
		"subject":         r.Subject,
		"business_params": map[string]string{"mc_create_trade_ip": r.ClientIP},
	}
	p.directSettle(biz, amount)

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
			return CreateResult{}, &UnknownOutcome{Operation: "alipayd.precreate", Cause: alipaydErrUnreadable}
		}
		return CreateResult{MaterialType: "qrcode", Material: qr, ProviderTradeNo: jsonText(data, "out_trade_no")}, nil
	case "page":
		biz["product_code"] = "FAST_INSTANT_TRADE_PAY"
		return p.pageForm(ctx, "alipay.trade.page.pay", biz, r)
	case "wap":
		biz["product_code"] = "QUICK_WAP_WAY"
		return p.pageForm(ctx, "alipay.trade.wap.pay", biz, r)
	}
	return CreateResult{}, ErrInvalid
}

// pageForm builds the signed system-parameter set for a page-jump API and
// renders the SDK's auto-submit POST form (pluginForm escapes every field).
func (p *alipaydPlugin) pageForm(_ context.Context, method string, biz map[string]any, r CreateRequest) (CreateResult, error) {
	params, e := p.sysParams(method, biz, r.NotifyURL, r.ReturnURL)
	if e != nil {
		return CreateResult{}, e
	}
	return CreateResult{MaterialType: "html", Material: pluginForm(alipaydGateway+"?charset=UTF-8", params, "正在跳转")}, nil
}

// sysParams assembles and signs the Alipay OpenAPI system parameters.
func (p *alipaydPlugin) sysParams(method string, biz map[string]any, notifyURL, returnURL string) (map[string]string, error) {
	bizJSON, e := json.Marshal(biz)
	if e != nil {
		return nil, ErrInvalid
	}
	params := map[string]string{
		"app_id":      p.appID,
		"method":      method,
		"format":      "json",
		"charset":     "UTF-8",
		"sign_type":   "RSA2",
		"version":     "1.0",
		"timestamp":   alipaydTimestamp(),
		"biz_content": string(bizJSON),
	}
	if notifyURL != "" {
		params["notify_url"] = notifyURL
	}
	if returnURL != "" {
		params["return_url"] = returnURL
	}
	sig, e := rsaSign(p.priv, crypto.SHA256, alipaydSignContent(params))
	if e != nil {
		return nil, e
	}
	params["sign"] = sig
	return params, nil
}

// execute performs a server-side OpenAPI call and returns the raw JSON reply.
func (p *alipaydPlugin) execute(ctx context.Context, method string, biz map[string]any, notifyURL, returnURL string) ([]byte, error) {
	params, e := p.sysParams(method, biz, notifyURL, returnURL)
	if e != nil {
		return nil, e
	}
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	return p.postForm(ctx, "alipayd."+method, alipaydGateway+"?charset=UTF-8", form)
}

// resultNode parses a server-side reply: verifies the response signature
// against the Alipay public key, then returns the "<method>_response" node
// when code == "10000", a definitive rejection for a business code, or an
// unknown outcome when the reply is unreadable/unverifiable.
func (p *alipaydPlugin) resultNode(raw []byte, method string) (map[string]any, error) {
	node := strings.ReplaceAll(method, ".", "_") + "_response"
	outer, e := callbackJSON(raw)
	if e != nil {
		return nil, &UnknownOutcome{Operation: "alipayd." + method, Cause: alipaydErrUnreadable}
	}
	// The gateway returns either "<method>_response" or "error_response"; the
	// signature covers whichever node is present (SDK parseResponseData).
	presentNode := node
	if _, ok := outer[node]; !ok {
		if _, isErr := outer["error_response"]; isErr {
			presentNode = "error_response"
		}
	}
	// Verify the signature over the exact response node substring, as the SDK
	// does (sign is over the raw bytes between the node and the "sign" key).
	signData, ok := alipaydResponseSignData(raw, presentNode)
	sig := jsonText(outer, "sign")
	if !ok || sig == "" || !rsaVerify(p.pub, crypto.SHA256, signData, sig) {
		return nil, &UnknownOutcome{Operation: "alipayd." + method, Cause: alipaydErrUnreadable}
	}
	inner, ok := outer[node].(map[string]any)
	if !ok {
		// error_response (signed envelope) — a definitive gateway rejection.
		if errNode, ok := outer["error_response"].(map[string]any); ok {
			return nil, rejected("ALIPAY_" + jsonText(errNode, "code"))
		}
		return nil, &UnknownOutcome{Operation: "alipayd." + method, Cause: alipaydErrUnreadable}
	}
	if jsonText(inner, "code") != "10000" {
		return nil, rejected("ALIPAY_" + jsonText(inner, "sub_code"))
	}
	return inner, nil
}

func (p *alipaydPlugin) Query(ctx context.Context, r QueryRequest) (QueryResult, error) {
	if r.TradeNo == "" {
		return QueryResult{}, ErrInvalid
	}
	biz := map[string]any{"out_trade_no": r.TradeNo}
	raw, e := p.execute(ctx, "alipay.trade.query", biz, "", "")
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

func (p *alipaydPlugin) Close(ctx context.Context, r QueryRequest) error {
	if r.TradeNo == "" {
		return ErrInvalid
	}
	biz := map[string]any{"out_trade_no": r.TradeNo}
	raw, e := p.execute(ctx, "alipay.trade.close", biz, "", "")
	if e != nil {
		return e
	}
	_, e = p.resultNode(raw, "alipay.trade.close")
	return e
}

func (p *alipaydPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.ProviderTradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	biz := map[string]any{
		"trade_no":       r.ProviderTradeNo,
		"refund_amount":  yuan(r.Amount),
		"out_request_no": r.RefundNo,
	}
	raw, e := p.execute(ctx, "alipay.trade.refund", biz, "", "")
	if e != nil {
		return RefundResult{}, e
	}
	data, e := p.resultNode(raw, "alipay.trade.refund")
	if e != nil {
		return RefundResult{}, e
	}
	// fund_change "Y" means the refund actually moved money; "N" (already
	// refunded / no change) is still a success state for the request.
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(data, "trade_no"), State: "SUCCESS", Amount: r.Amount}, nil
}

// VerifyNotification mirrors notify(): $_POST is RSA-verified (ksort join),
// then trade_status must be TRADE_SUCCESS and the order number + amount must
// match our expected payment.
func (p *alipaydPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	params, e := callbackParams(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	if params["app_id"] != "" && params["app_id"] != p.appID {
		return PaymentEvent{}, ErrAuthentication
	}
	if !alipaydVerifyNotify(p.pub, params) {
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

// ---- Alipay OpenAPI signing helpers (file-local) ----

// alipaydSignContent is ksort + "&k=v" join, skipping sign and empty values
// (SDK getSignContent). The '@'-prefix file skip is irrelevant for our params.
func alipaydSignContent(params map[string]string) string {
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

// alipaydVerifyNotify verifies an async-callback signature: ksort over all
// params except sign/sign_type, "&k=v" join, RSA2 (SDK AopClient::verify).
func alipaydVerifyNotify(pub *rsa.PublicKey, params map[string]string) bool {
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

// alipaydResponseSignData extracts the exact substring the gateway signed:
// the bytes of the "<node>":{...} value, i.e. from the first '{' after the
// node name up to the character before the "sign" key (SDK parseResponseData).
func alipaydResponseSignData(raw []byte, node string) (string, bool) {
	s := string(raw)
	idx := strings.Index(s, node)
	if idx < 0 {
		return "", false
	}
	start := idx + len(node) + 2 // skip node name plus `":`
	signIdx := strings.LastIndex(s, `"sign"`)
	if signIdx < 0 || signIdx-1 <= start {
		return "", false
	}
	return s[start : signIdx-1], true
}

var _ Provider = (*alipaydPlugin)(nil)
var _ NotificationVerifier = (*alipaydPlugin)(nil)
var _ CreateValidator = (*alipaydPlugin)(nil)
