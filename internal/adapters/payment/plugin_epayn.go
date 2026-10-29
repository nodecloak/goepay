package payment

// Port of epay_release/plugins/epayn (彩虹易支付V2): the RSA-signed V2 API of
// 彩虹易支付. Requests carry pid + timestamp, are signed with the merchant
// private key (SHA256, PKCS#1 v1.5, base64) over the ksort'd "k=v&" base, and
// replies are verified with the platform public key plus a 300s timestamp
// window (EpayCore::verify). Page mode (appswitch=0) posts a signed form to
// api/pay/submit; mapi mode (appswitch=1) calls api/pay/create and shows the
// returned pay_info by pay_type. Query/refund/refundquery use api/pay/*.
//
// Scope: payments only. The original's transfer / transfer_query /
// balance_query (api/transfer/*) are out of scope for this wave.

import (
	"context"
	"crypto"
	"crypto/rsa"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "epayn",
		Title:  "彩虹易支付V2",
		Author: "彩虹",
		Types:  []string{"alipay", "qqpay", "wxpay", "bank", "jdpay", "douyinpay"},
		// transtypes alipay/wxpay/qqpay/bank are transfers — out of scope.
		Inputs: []PluginInput{
			{Key: "appurl", Label: "接口地址", Note: "必须以http://或https://开头，以/结尾", Required: true},
			{Key: "appid", Label: "商户ID", Required: true},
			{Key: "appkey", Label: "平台公钥", Textarea: true, Required: true},
			{Key: "appsecret", Label: "商户私钥", Secret: true, Textarea: true, Required: true},
			{Key: "appswitch", Label: "是否使用mapi接口", Options: []PluginOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}}},
		},
		Build: newEpaynPlugin,
	})
}

type epaynPlugin struct {
	pluginBase
	apiURL, pid string
	priv        *rsa.PrivateKey
	pub         *rsa.PublicKey
	mapi        bool
}

// epaynNow is overridable in tests for deterministic timestamps.
var epaynNow = func() int64 { return time.Now().Unix() }

func newEpaynPlugin(c PluginChannel) (Provider, error) {
	api := strings.TrimSpace(c.Config["appurl"])
	host, ok := gatewayHost(api)
	if !ok || !strings.HasSuffix(api, "/") || strings.ContainsAny(api, "?#") {
		return nil, ErrInvalid
	}
	pid := strings.TrimSpace(c.Config["appid"])
	if pid == "" || len(pid) > 32 || strings.ContainsAny(pid, "&=?# ") {
		return nil, ErrInvalid
	}
	priv, e := pluginPrivateKey(c.Config["appsecret"])
	if e != nil {
		return nil, ErrInvalid
	}
	pub, e := pluginPublicKey(c.Config["appkey"])
	if e != nil {
		return nil, ErrInvalid
	}
	base, e := newPluginBase(c, pluginAccount(host, pid))
	if e != nil {
		return nil, e
	}
	return &epaynPlugin{pluginBase: base, apiURL: api, pid: pid, priv: priv, pub: pub, mapi: c.Config["appswitch"] == "1"}, nil
}

// methods: the V2 API picks the product by type+device server side, so a single
// "web" create call serves each pay type. Page mode offers a page/h5 form flow;
// mapi mode additionally produces a native QR for the scan pay types.
func (p *epaynPlugin) methods() []string {
	if p.payType == "alipay" {
		if p.mapi {
			return []string{"page", "wap", "native"}
		}
		return []string{"page", "wap"}
	}
	if p.mapi {
		return []string{"native", "h5"}
	}
	return []string{"h5"}
}

func (p *epaynPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Query: true, Refund: true, RefundQuery: true}
}

// signContent builds EpayCore::getSignContent (ksort, skip sign/sign_type and
// empty values, "k=v" joined by "&") — epaySignString does exactly this.
func (p *epaynPlugin) signContent(params map[string]string) string {
	return epaySignString(params)
}

// sign adds pid+timestamp, signs with the merchant private key (SHA256) and
// sets sign/sign_type, mirroring EpayCore::buildRequestParam.
func (p *epaynPlugin) sign(params map[string]string) error {
	params["pid"] = p.pid
	params["timestamp"] = strconv.FormatInt(epaynNow(), 10)
	sig, e := rsaSign(p.priv, crypto.SHA256, p.signContent(params))
	if e != nil {
		return e
	}
	params["sign"] = sig
	params["sign_type"] = "RSA"
	return nil
}

// verify checks the platform signature and the 300s timestamp window exactly
// as EpayCore::verify does.
func (p *epaynPlugin) verify(params map[string]string) bool {
	ts, e := strconv.ParseInt(params["timestamp"], 10, 64)
	if params["sign"] == "" || params["timestamp"] == "" || e != nil {
		return false
	}
	now := epaynNow()
	if ts <= 0 || ts < now-300 || ts > now+300 {
		return false
	}
	return rsaVerify(p.pub, crypto.SHA256, p.signContent(params), params["sign"])
}

func (p *epaynPlugin) ValidateCreate(r CreateRequest) error {
	return validatePluginCreate(r, p.methods())
}

func (p *epaynPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	if !p.mapi {
		// Page mode: submit.php-style signed form to api/pay/submit.
		params := map[string]string{
			"type":         p.payType,
			"notify_url":   r.NotifyURL,
			"return_url":   r.ReturnURL,
			"out_trade_no": r.TradeNo,
			"name":         r.Subject,
			"money":        yuan(r.Amount),
		}
		if e := p.sign(params); e != nil {
			return CreateResult{}, e
		}
		return CreateResult{MaterialType: "html", Material: pluginForm(p.apiURL+"api/pay/submit", params, "正在跳转")}, nil
	}
	// mapi mode: api/pay/create with method=web; the upstream chooses the
	// product by type+device and returns pay_type + pay_info.
	device := "pc"
	if r.Method == "h5" || r.Method == "wap" {
		device = "mobile"
	}
	params := map[string]string{
		"method":       "web",
		"type":         p.payType,
		"device":       device,
		"clientip":     r.ClientIP,
		"notify_url":   r.NotifyURL,
		"return_url":   r.ReturnURL,
		"out_trade_no": r.TradeNo,
		"name":         r.Subject,
		"money":        yuan(r.Amount),
	}
	if e := p.sign(params); e != nil {
		return CreateResult{}, e
	}
	out, e := p.call(ctx, "epayn.create", "api/pay/create", params)
	if e != nil {
		return CreateResult{}, e
	}
	payType, payInfo := jsonText(out, "pay_type"), jsonText(out, "pay_info")
	res, ok := epaynMaterial(payType, payInfo)
	if !ok {
		return CreateResult{}, rejected("EPAYN_NO_PAY_INFO")
	}
	res.ProviderTradeNo = jsonText(out, "trade_no")
	return res, nil
}

// epaynMaterial maps the V2 pay_type/pay_info reply to checkout material.
func epaynMaterial(payType, payInfo string) (CreateResult, bool) {
	switch payType {
	case "jump":
		if validPaymentURL(payInfo) {
			return CreateResult{MaterialType: "jump", Material: payInfo}, true
		}
	case "html":
		if payInfo != "" && len(payInfo) <= 1<<20 {
			return CreateResult{MaterialType: "html", Material: payInfo}, true
		}
	case "urlscheme", "scheme":
		if payInfo != "" && len(payInfo) <= 4096 && strings.Contains(payInfo, "://") && !strings.ContainsAny(payInfo, "\x00\r\n") {
			return CreateResult{MaterialType: "urlscheme", Material: payInfo}, true
		}
	case "jsapi":
		if payInfo != "" && len(payInfo) <= 4096 && !strings.ContainsAny(payInfo, "\x00\r\n") {
			return CreateResult{MaterialType: "jsapi", Material: payInfo}, true
		}
	default: // qrcode (default branch in the original)
		if payInfo != "" && len(payInfo) <= 4096 && !strings.ContainsAny(payInfo, "\x00\r\n") {
			return CreateResult{MaterialType: "qrcode", Material: payInfo}, true
		}
	}
	return CreateResult{}, false
}

// call sends an api/pay/* request, parses the reply, enforces code==0 and
// verifies the platform signature over the whole reply (EpayCore::execute).
func (p *epaynPlugin) call(ctx context.Context, op, path string, params map[string]string) (map[string]any, error) {
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	raw, e := p.postForm(ctx, op, p.apiURL+path, form)
	if e != nil {
		return nil, e
	}
	out, e := callbackJSON(raw)
	if e != nil {
		return nil, &UnknownOutcome{Operation: op, Cause: e}
	}
	if jsonText(out, "code") != "0" {
		return nil, rejected("EPAYN_" + jsonText(out, "code"))
	}
	flat := epaynFlatten(out)
	if !p.verify(flat) {
		// A 2xx reply we cannot authenticate: upstream may have acted.
		return nil, &UnknownOutcome{Operation: op, Cause: ErrAuthentication}
	}
	return out, nil
}

// epaynFlatten reduces a reply object to the scalar fields the signature covers
// (arrays/objects are excluded by getSignContent on the PHP side).
func epaynFlatten(out map[string]any) map[string]string {
	flat := map[string]string{}
	for k, v := range out {
		switch v.(type) {
		case map[string]any, []any:
			continue
		default:
			flat[k] = jsonText(out, k)
		}
	}
	return flat
}

// Callbacks are GET with the V2 RSA signature (the original reads $_GET).
func (p *epaynPlugin) NotificationMethods() []string {
	return []string{http.MethodGet, http.MethodPost}
}

func (p *epaynPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	params, e := callbackParams(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	if !p.verify(params) {
		return PaymentEvent{}, ErrAuthentication
	}
	if params["trade_status"] != "TRADE_SUCCESS" {
		return PaymentEvent{}, ErrInvalid
	}
	amount, e := parseYuan(params["money"])
	if e != nil || expected.Amount <= 0 || amount != expected.Amount || params["out_trade_no"] != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, params["trade_no"], params["buyer"], amount), nil
}

func (p *epaynPlugin) Query(ctx context.Context, r QueryRequest) (QueryResult, error) {
	if r.TradeNo == "" {
		return QueryResult{}, ErrInvalid
	}
	params := map[string]string{"out_trade_no": r.TradeNo}
	if r.ProviderTradeNo != "" {
		params["trade_no"] = r.ProviderTradeNo
	}
	if e := p.sign(params); e != nil {
		return QueryResult{}, e
	}
	out, e := p.call(ctx, "epayn.query", "api/pay/query", params)
	if e != nil {
		return QueryResult{}, e
	}
	if otn := jsonText(out, "out_trade_no"); otn != "" && otn != r.TradeNo {
		return QueryResult{}, ErrAuthentication
	}
	amount, e := parseYuan(jsonText(out, "money"))
	if e != nil {
		return QueryResult{}, ErrAuthentication
	}
	// status: 0 unpaid, 1 paid, 2 refunded (per EpayCore::orderStatus==1 paid).
	state := "NOTPAY"
	switch jsonText(out, "status") {
	case "1":
		state = "SUCCESS"
	case "2":
		state = "REFUND"
	}
	return QueryResult{TradeNo: r.TradeNo, ProviderTradeNo: jsonText(out, "trade_no"), State: state, Currency: "CNY", BuyerID: jsonText(out, "buyer"), Amount: amount}, nil
}

func (p *epaynPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.ProviderTradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	params := map[string]string{
		"trade_no":      r.ProviderTradeNo,
		"money":         yuan(r.Amount),
		"out_refund_no": r.RefundNo,
	}
	if e := p.sign(params); e != nil {
		return RefundResult{}, e
	}
	out, e := p.call(ctx, "epayn.refund", "api/pay/refund", params)
	if e != nil {
		return RefundResult{}, e
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(out, "refund_no"), State: "SUCCESS", Amount: r.Amount}, nil
}

func (p *epaynPlugin) QueryRefund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.RefundNo == "" {
		return RefundResult{}, ErrInvalid
	}
	params := map[string]string{"out_refund_no": r.RefundNo}
	if e := p.sign(params); e != nil {
		return RefundResult{}, e
	}
	out, e := p.call(ctx, "epayn.refundquery", "api/pay/refundquery", params)
	if e != nil {
		return RefundResult{}, e
	}
	// status: 1 success, 0 processing (anything else a failure).
	state := "FAIL"
	switch jsonText(out, "status") {
	case "1":
		state = "SUCCESS"
	case "0":
		state = "PROCESSING"
	}
	amount, _ := parseYuan(jsonText(out, "money"))
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(out, "refund_no"), State: state, Amount: amount}, nil
}

var _ Provider = (*epaynPlugin)(nil)
var _ NotificationVerifier = (*epaynPlugin)(nil)
var _ CreateValidator = (*epaynPlugin)(nil)
