package payment

// Port of epay_release/plugins/jeepay (Jeepay聚合支付): a JSON aggregator.
// Create posts to <appurl>api/pay/unifiedOrder with an MD5 sign
// (UPPER(md5(sorted k=v&... + "key="+appkey))); the reply's payDataType selects
// the material (payurl->jump, codeurl/none->qrcode, form->html). Callbacks are
// JSON (or form/GET) with the same MD5 sign, state==2 meaning paid; the ack is
// plain "success". Refund posts api/refund/refundOrder. Transfers
// (transferOrder / transfer/query / transfernotify) are OUT OF SCOPE.

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "jeepay",
		Title:  "Jeepay聚合支付",
		Author: "Jeepay",
		Link:   "https://www.jeequan.com/",
		Types:  []string{"alipay", "wxpay", "bank"},
		// transtypes alipay/wxpay/bank exist upstream but transfers are out of scope.
		Inputs: []PluginInput{
			{Key: "appurl", Label: "接口地址", Note: "必须以http://或https://开头，以/结尾", Required: true},
			{Key: "appmchid", Label: "商户号", Required: true},
			{Key: "appid", Label: "应用AppId", Required: true},
			{Key: "appkey", Label: "私钥AppSecret", Secret: true, Textarea: true, Required: true},
		},
		Build: newJeepayPlugin,
	})
}

type jeepayPlugin struct {
	pluginBase
	apiURL, mchNo, appID, key string
}

func newJeepayPlugin(c PluginChannel) (Provider, error) {
	api := strings.TrimSpace(c.Config["appurl"])
	host, ok := gatewayHost(api)
	if !ok || !strings.HasSuffix(api, "/") || strings.ContainsAny(api, "?#") {
		return nil, ErrInvalid
	}
	mchNo := strings.TrimSpace(c.Config["appmchid"])
	appID := strings.TrimSpace(c.Config["appid"])
	key := strings.TrimSpace(c.Config["appkey"])
	if mchNo == "" || len(mchNo) > 64 || appID == "" || len(appID) > 64 || key == "" || len(key) > 256 {
		return nil, ErrInvalid
	}
	base, e := newPluginBase(c, pluginAccount(host, mchNo, appID))
	if e != nil {
		return nil, e
	}
	return &jeepayPlugin{pluginBase: base, apiURL: api, mchNo: mchNo, appID: appID, key: key}, nil
}

// wayCode maps the GOEPAY (payType, method) to the Jeepay product code. Only
// collection products are served; the original's apptype selection collapses to
// the canonical product for each device, which is what a normal order uses.
func (p *jeepayPlugin) wayCode(method string) string {
	switch p.payType {
	case "alipay":
		switch method {
		case "native":
			return "ALI_QR"
		case "page":
			return "ALI_PC"
		case "wap":
			return "ALI_WAP"
		case "app":
			return "ALI_APP"
		case "jsapi":
			return "ALI_JSAPI"
		}
	case "wxpay":
		switch method {
		case "native":
			return "WX_NATIVE"
		case "h5":
			return "WX_H5"
		case "app":
			return "WX_APP"
		case "jsapi":
			return "WX_JSAPI"
		}
	case "bank":
		if method == "native" {
			return "YSF_NATIVE"
		}
	}
	return ""
}

func (p *jeepayPlugin) methods() []string {
	switch p.payType {
	case "alipay":
		return []string{"page", "wap", "native", "app", "jsapi"}
	case "wxpay":
		return []string{"native", "h5", "app", "jsapi"}
	case "bank":
		return []string{"native"}
	}
	return nil
}

func (p *jeepayPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

// sign reproduces make_sign: ksort, skip sign and empty values, join "k=v&",
// append "key="+appkey, md5, uppercase hex.
func (p *jeepayPlugin) sign(params map[string]string) string {
	base := epaySignString(params, "key")
	if base != "" {
		base += "&"
	}
	return strings.ToUpper(md5Hex(base + "key=" + p.key))
}

func (p *jeepayPlugin) ValidateCreate(r CreateRequest) error {
	if e := validatePluginCreate(r, p.methods()); e != nil {
		return e
	}
	if p.wayCode(r.Method) == "" {
		return ErrInvalid
	}
	if r.Method == "jsapi" && (r.OpenID == "" || len(r.OpenID) > 128) {
		return ErrInvalid
	}
	return nil
}

func (p *jeepayPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	param := map[string]string{
		"mchNo":      p.mchNo,
		"appId":      p.appID,
		"mchOrderNo": r.TradeNo,
		"wayCode":    p.wayCode(r.Method),
		"amount":     parseFenString(r.Amount),
		"currency":   "cny",
		"clientIp":   r.ClientIP,
		"subject":    r.Subject,
		"body":       r.Subject,
		"notifyUrl":  r.NotifyURL,
		"returnUrl":  r.ReturnURL,
		"version":    "1.0",
		"signType":   "MD5",
	}
	if r.Method == "jsapi" {
		// The original forwards the openid/buyerUserId in channelExtra.
		param["channelExtra"] = jeepayExtra(p.payType, r.OpenID)
	}
	param["sign"] = p.sign(param)
	raw, e := p.postJSON(ctx, "jeepay.unifiedOrder", p.apiURL+"api/pay/unifiedOrder", param, nil)
	if e != nil {
		return CreateResult{}, e
	}
	out, e := callbackJSON(raw)
	if e != nil {
		return CreateResult{}, &UnknownOutcome{Operation: "jeepay.unifiedOrder", Cause: e}
	}
	if jsonText(out, "code") != "0" {
		return CreateResult{}, rejected("JEEPAY_" + jsonText(out, "code"))
	}
	data, _ := out["data"].(map[string]any)
	if data == nil {
		return CreateResult{}, &UnknownOutcome{Operation: "jeepay.unifiedOrder", Cause: ErrInvalid}
	}
	if jsonText(data, "errCode") != "" || jsonText(data, "error") != "" {
		return CreateResult{}, rejected("JEEPAY_" + firstNonEmpty(jsonText(data, "errCode"), "ERR"))
	}
	provider := jsonText(data, "payOrderId")
	res, ok := jeepayMaterial(strings.ToLower(jsonText(data, "payDataType")), jsonText(data, "payData"), r.Method)
	if !ok {
		return CreateResult{}, rejected("JEEPAY_NO_PAY_DATA")
	}
	res.ProviderTradeNo = provider
	return res, nil
}

// jeepayMaterial maps the reply's payDataType to checkout material.
//
//	payurl -> jump, form -> html(url is already a full form document), codeurl
//	or absent -> qrcode. For jsapi the payData is the JSON the browser needs.
func jeepayMaterial(dataType, payData, method string) (CreateResult, bool) {
	if payData == "" {
		return CreateResult{}, false
	}
	switch dataType {
	case "payurl":
		if !validPaymentURL(payData) {
			return CreateResult{}, false
		}
		return CreateResult{MaterialType: "jump", Material: payData}, true
	case "form":
		if len(payData) > 1<<16 || strings.ContainsRune(payData, 0) {
			return CreateResult{}, false
		}
		return CreateResult{MaterialType: "html", Material: payData}, true
	default:
		if method == "jsapi" {
			if len(payData) > 4096 || strings.ContainsRune(payData, 0) {
				return CreateResult{}, false
			}
			return CreateResult{MaterialType: "jsapi", Material: payData}, true
		}
		if len(payData) > 4096 || strings.ContainsAny(payData, "\x00\r\n") {
			return CreateResult{}, false
		}
		return CreateResult{MaterialType: "qrcode", Material: payData}, true
	}
}

func (p *jeepayPlugin) NotificationMethods() []string {
	return []string{http.MethodGet, http.MethodPost}
}

func (p *jeepayPlugin) VerifyNotification(_ context.Context, header http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	params, e := jeepayCallback(header, raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	if !signEqual(p.sign(params), params["sign"], true) {
		return PaymentEvent{}, ErrAuthentication
	}
	if params["state"] != "2" {
		return PaymentEvent{}, ErrInvalid
	}
	amount, e := parseFen(params["amount"])
	if e != nil || expected.Amount <= 0 || amount != expected.Amount || params["mchOrderNo"] != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, params["payOrderId"], "", amount), nil
}

func (p *jeepayPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.ProviderTradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	param := map[string]string{
		"mchNo":        p.mchNo,
		"appId":        p.appID,
		"payOrderId":   r.ProviderTradeNo,
		"mchRefundNo":  r.RefundNo,
		"refundAmount": parseFenString(r.Amount),
		"currency":     "cny",
		"refundReason": "申请退款",
		"version":      "1.0",
		"signType":     "MD5",
	}
	param["sign"] = p.sign(param)
	raw, e := p.postJSON(ctx, "jeepay.refund", p.apiURL+"api/refund/refundOrder", param, nil)
	if e != nil {
		return RefundResult{}, e
	}
	out, e := callbackJSON(raw)
	if e != nil {
		return RefundResult{}, &UnknownOutcome{Operation: "jeepay.refund", Cause: e}
	}
	if jsonText(out, "code") != "0" {
		return RefundResult{}, rejected("JEEPAY_" + jsonText(out, "code"))
	}
	data, _ := out["data"].(map[string]any)
	if data == nil {
		return RefundResult{}, &UnknownOutcome{Operation: "jeepay.refund", Cause: ErrInvalid}
	}
	if jsonText(data, "errCode") != "" || jsonText(data, "error") != "" {
		return RefundResult{}, rejected("JEEPAY_" + firstNonEmpty(jsonText(data, "errCode"), "ERR"))
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(data, "refundOrderId"), State: "SUCCESS", Amount: r.Amount}, nil
}

// jeepayCallback accepts a JSON body (Content-Type application/json) or a
// form/query string, matching the original notify()'s three input branches.
func jeepayCallback(header http.Header, raw []byte) (map[string]string, error) {
	if header != nil && strings.Contains(strings.ToLower(header.Get("Content-Type")), "application/json") {
		obj, e := callbackJSON(raw)
		if e != nil {
			return nil, e
		}
		out := map[string]string{}
		for k := range obj {
			out[k] = jsonText(obj, k)
		}
		return out, nil
	}
	return callbackParams(raw)
}

func jeepayExtra(payType, openID string) string {
	// The upstream reads openid for WX_JSAPI and buyerUserId for ALI_JSAPI.
	key := "openid"
	if payType == "alipay" {
		key = "buyerUserId"
	}
	raw, _ := json.Marshal(map[string]string{key: openID})
	return string(raw)
}

func parseFenString(cents int64) string {
	return strconv.FormatInt(cents, 10)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

var _ Provider = (*jeepayPlugin)(nil)
var _ NotificationVerifier = (*jeepayPlugin)(nil)
var _ CreateValidator = (*jeepayPlugin)(nil)
