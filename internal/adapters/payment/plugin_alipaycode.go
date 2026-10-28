package payment

// Port of epay_release/plugins/alipaycode (支付宝免签约码支付). This is a
// SIGNATURE-FREE, DAEMON-CONFIRMED product: the payer transfers money to the
// merchant's Alipay UID with the trade number in the remark, and the original
// plugin's server.php daemon polls the account bill (AlipayBillService.
// accountlogQuery) to match trans_memo==trade_no and mark the order paid.
//
// Consequently there is NO upstream create API for the plain-transfer mode and
// NO signed asynchronous callback at all. What CAN be produced at create time
// is the 转账确认单 (appswitch==1) deep link to render.alipay.com, which is an
// unsigned URL built entirely from config + order data, exactly as the PHP
// pay() method does.
//
// SCOPE of this port:
//   - native (qrcode): the 转账确认单 render.alipay.com transfer URL, carrying
//     the payee UID and the trade number as the transfer remark. Served only
//     when appswitch==1.
//   - Query: reproduces the server.php confirmation. It calls
//     alipay.data.bill.accountlog.query (RSA2 OpenAPI, same engine shape as the
//     other Alipay ports) over the original daemon's window (now-180s .. now+60s,
//     page 1, size 2000), and returns SUCCESS for an incoming transfer whose
//     memo equals our trade number (ProviderTradeNo = the Alipay account-log
//     id), carrying the transfer amount for the core to validate; otherwise
//     NOTPAY. Requires the 支付宝公钥 (appkey) for response verification.
// NOT PORTED (documented in the report):
//   - appswitch==0 普通转账: the original shows an internal pay page and relies
//     on the server.php polling daemon; there is no code-constructable,
//     self-contained checkout material, so this mode returns ErrUnsupported.
//   - payment confirmation via a signed notify: the original has none, so
//     VerifyNotification is explicitly unsupported (use Query instead).
//   - close / refund: the original plugin implements none.

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

const alipaycodeRenderBase = "https://render.alipay.com/p/yuyan/180020010001206672/rent-index.html"
const alipaycodeGateway = "https://openapi.alipay.com/gateway.do"

var alipaycodeErrUnreadable = errors.New("unreadable or unverifiable alipay response")

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "alipaycode",
		Title:  "支付宝免签约码支付",
		Author: "支付宝",
		Link:   "https://open.alipay.com/",
		Types:  []string{"alipay"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "应用APPID", Required: true},
			{Key: "appkey", Label: "支付宝公钥", Note: "供对账守护进程使用", Textarea: true},
			{Key: "appsecret", Label: "应用私钥", Secret: true, Textarea: true, Required: true},
			{Key: "appmchid", Label: "支付宝UID", Note: "2088开头的16位纯数字，收款方UID", Required: true},
			{Key: "apptoken", Label: "商户授权token", Note: "只有第三方应用需要填写"},
			{Key: "appswitch", Label: "支付类型", Options: []PluginOption{{Value: "0", Label: "普通转账"}, {Value: "1", Label: "转账确认单"}}},
		},
		Note:  "免签约码支付，靠对账(account bill)轮询确认到账。本移植生成「转账确认单」(appswitch=1) 的 render.alipay.com 收款链接，并以 alipay.data.bill.accountlog.query 复现守护进程的到账确认(Query)；普通转账下单、退款、签名回调不在范围内。",
		Build: newAlipaycodePlugin,
	})
}

type alipaycodePlugin struct {
	pluginBase
	appID    string
	payeeUID string
	confirm  bool // appswitch == 1 (转账确认单)
	priv     *rsa.PrivateKey
	pub      *rsa.PublicKey // 支付宝公钥 (appkey), nil when not configured
}

func newAlipaycodePlugin(c PluginChannel) (Provider, error) {
	uid := strings.TrimSpace(c.Config["appmchid"])
	// 2088 + 12 digits = 16-digit numeric UID.
	if !alipaycodeValidUID(uid) {
		return nil, ErrInvalid
	}
	appID := strings.TrimSpace(c.Config["appid"])
	if appID == "" || len(appID) > 32 || strings.ContainsAny(appID, "&=?# ") {
		return nil, ErrInvalid
	}
	// The private key signs the account-bill query (and the confirmation
	// daemon the original runs). Required even though create signs nothing.
	priv, e := pluginPrivateKey(c.Config["appsecret"])
	if e != nil {
		return nil, e
	}
	// 支付宝公钥 is optional in the console (the render-link create path never
	// needs it); when present it is parsed once and used to verify the Query
	// response. A malformed key is rejected rather than silently dropped.
	var pub *rsa.PublicKey
	if strings.TrimSpace(c.Config["appkey"]) != "" {
		pub, e = pluginPublicKey(c.Config["appkey"])
		if e != nil {
			return nil, e
		}
	}
	base, e := newPluginBase(c, pluginAccount("alipaycode", appID, uid))
	if e != nil {
		return nil, e
	}
	return &alipaycodePlugin{pluginBase: base, appID: appID, payeeUID: uid, confirm: c.Config["appswitch"] == "1", priv: priv, pub: pub}, nil
}

func alipaycodeValidUID(uid string) bool {
	if len(uid) != 16 || !strings.HasPrefix(uid, "2088") {
		return false
	}
	for _, r := range uid {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (p *alipaycodePlugin) methods() []string {
	if p.confirm {
		return []string{"native"}
	}
	// Plain-transfer mode has no code-constructable material in this port.
	return nil
}

func (p *alipaycodePlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Query: true}
}

func (p *alipaycodePlugin) ValidateCreate(r CreateRequest) error {
	return validatePluginCreate(r, p.methods())
}

// Create builds the 转账确认单 render.alipay.com link (appswitch==1). The trade
// number is carried as the transfer remark, which the daemon matches to
// confirm payment. No signature, no network call.
func (p *alipaycodePlugin) Create(_ context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	if !p.confirm || r.Method != "native" {
		return CreateResult{}, ErrUnsupported
	}
	params := map[string]any{
		"productCode": "TRANSFER_TO_ALIPAY_ACCOUNT",
		"bizScene":    "YUEBAO",
		"transAmount": yuan(r.Amount),
		"remark":      r.TradeNo,
		"businessParams": map[string]string{
			"returnUrl": "alipays://platformapi/startapp?appId=2021001167654035&nbupdate=syncforce",
		},
		"payeeInfo": map[string]string{
			"identity":     p.payeeUID,
			"identityType": "ALIPAY_USER_ID",
		},
	}
	formData, e := json.Marshal(params)
	if e != nil {
		return CreateResult{}, ErrInvalid
	}
	target := alipaycodeRenderBase + "?formData=" + url.QueryEscape(string(formData))
	return CreateResult{MaterialType: "qrcode", Material: target}, nil
}

// Query reproduces the server.php confirmation: it pulls the account-bill log
// over the daemon's window and looks for an INCOMING transfer whose memo is our
// trade number. No order time or amount is carried in QueryRequest, so we use
// the original's relative window (now-180s .. now+60s), page 1, size 2000, and
// — as with every other provider's Query — RETURN the matched transfer's amount
// in the result for the payment core to validate against the order.
func (p *alipaycodePlugin) Query(ctx context.Context, r QueryRequest) (QueryResult, error) {
	if r.TradeNo == "" {
		return QueryResult{}, ErrInvalid
	}
	if p.pub == nil {
		// Response verification is mandatory; without 支付宝公钥 we cannot.
		return QueryResult{}, ErrUnsupported
	}
	now := time.Now()
	loc := alipaycodeLocation()
	biz := map[string]any{
		"start_time": now.Add(-180 * time.Second).In(loc).Format("2006-01-02 15:04:05"),
		"end_time":   now.Add(60 * time.Second).In(loc).Format("2006-01-02 15:04:05"),
		"page_no":    1,
		"page_size":  2000,
	}
	raw, e := p.execute(ctx, "alipay.data.bill.accountlog.query", biz)
	if e != nil {
		return QueryResult{}, e
	}
	data, e := p.resultNode(raw, "alipay.data.bill.accountlog.query")
	if e != nil {
		return QueryResult{}, e
	}
	list, _ := data["detail_list"].([]any)
	for _, item := range list {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		// server.php strips a "请勿添加备注-" prefix before matching the memo.
		memo := strings.TrimPrefix(jsonText(row, "trans_memo"), "请勿添加备注-")
		if memo != r.TradeNo {
			continue
		}
		amount, aerr := parseYuan(jsonText(row, "trans_amount"))
		if aerr != nil {
			// Memo matches but the amount is unreadable — not a usable hit.
			continue
		}
		// QueryRequest carries no expected amount, so (as with every other
		// provider's Query) the account-log amount is RETURNED for the payment
		// core to compare against the order; a mismatch is rejected there.
		id := jsonText(row, "alipay_order_no")
		if id == "" {
			id = jsonText(row, "trade_no")
		}
		return QueryResult{TradeNo: r.TradeNo, ProviderTradeNo: id, State: "SUCCESS", Currency: "CNY", BuyerID: jsonText(row, "other_account"), Amount: amount}, nil
	}
	return QueryResult{TradeNo: r.TradeNo, State: "NOTPAY", Currency: "CNY"}, nil
}

// VerifyNotification: the original has no signed callback (confirmation is the
// accountlog-polling daemon, reproduced by Query), so verification is not
// supported.
func (p *alipaycodePlugin) VerifyNotification(context.Context, http.Header, []byte, ExpectedPayment) (PaymentEvent, error) {
	return PaymentEvent{}, ErrUnsupported
}

// ---- Alipay OpenAPI engine (file-local, RSA2) ----

func alipaycodeLocation() *time.Location {
	loc, e := time.LoadLocation("Asia/Shanghai")
	if e != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}

func alipaycodeTimestamp() string {
	return time.Now().In(alipaycodeLocation()).Format("2006-01-02 15:04:05")
}

func (p *alipaycodePlugin) sysParams(method string, biz map[string]any) (map[string]string, error) {
	bizJSON, e := json.Marshal(biz)
	if e != nil {
		return nil, ErrInvalid
	}
	params := map[string]string{
		"app_id": p.appID, "method": method, "format": "json", "charset": "UTF-8",
		"sign_type": "RSA2", "version": "1.0", "timestamp": alipaycodeTimestamp(),
		"biz_content": string(bizJSON),
	}
	sig, e := rsaSign(p.priv, crypto.SHA256, alipaycodeSignContent(params))
	if e != nil {
		return nil, e
	}
	params["sign"] = sig
	return params, nil
}

func (p *alipaycodePlugin) execute(ctx context.Context, method string, biz map[string]any) ([]byte, error) {
	params, e := p.sysParams(method, biz)
	if e != nil {
		return nil, e
	}
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	return p.postForm(ctx, "alipaycode."+method, alipaycodeGateway+"?charset=UTF-8", form)
}

func (p *alipaycodePlugin) resultNode(raw []byte, method string) (map[string]any, error) {
	node := strings.ReplaceAll(method, ".", "_") + "_response"
	outer, e := callbackJSON(raw)
	if e != nil {
		return nil, &UnknownOutcome{Operation: "alipaycode." + method, Cause: alipaycodeErrUnreadable}
	}
	presentNode := node
	if _, ok := outer[node]; !ok {
		if _, isErr := outer["error_response"]; isErr {
			presentNode = "error_response"
		}
	}
	signData, ok := alipaycodeResponseSignData(raw, presentNode)
	sig := jsonText(outer, "sign")
	if !ok || sig == "" || !rsaVerify(p.pub, crypto.SHA256, signData, sig) {
		return nil, &UnknownOutcome{Operation: "alipaycode." + method, Cause: alipaycodeErrUnreadable}
	}
	inner, ok := outer[node].(map[string]any)
	if !ok {
		if errNode, ok := outer["error_response"].(map[string]any); ok {
			code := jsonText(errNode, "sub_code")
			if code == "" {
				code = jsonText(errNode, "code")
			}
			return nil, rejected("ALIPAY_" + code)
		}
		return nil, &UnknownOutcome{Operation: "alipaycode." + method, Cause: alipaycodeErrUnreadable}
	}
	if jsonText(inner, "code") != "10000" {
		code := jsonText(inner, "sub_code")
		if code == "" {
			code = jsonText(inner, "code")
		}
		return nil, rejected("ALIPAY_" + code)
	}
	return inner, nil
}

func alipaycodeSignContent(params map[string]string) string {
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

func alipaycodeResponseSignData(raw []byte, node string) (string, bool) {
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

var _ Provider = (*alipaycodePlugin)(nil)
var _ NotificationVerifier = (*alipaycodePlugin)(nil)
var _ CreateValidator = (*alipaycodePlugin)(nil)
