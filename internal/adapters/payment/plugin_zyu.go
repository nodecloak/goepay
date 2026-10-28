package payment

// Port of epay_release/plugins/zyu (知宇支付). Form POST to the configured
// gateway with an UPPERCASE MD5 sign over ALL non-empty fields in ksort order
// ("k=v&"...) plus "key=<appkey>" (zyu_plugin::make_sign). The sign covers the
// seven base fields only; pay_productname and pay_md5sign are added afterward.
// appswitch=0 posts the form straight to the gateway (page flow); appswitch=1/2
// calls the gateway and then jumps (1) or shows a QR (2) from the reply pay URL.
// Callbacks (GET sync / POST async) sign a fixed field set; returncode "00" is
// success; ack is "OK"/"FAIL".
//
// Scope: payments only. The original plugin wires no refund or query.

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "zyu",
		Title:  "知宇支付",
		Author: "知宇",
		Types:  []string{"alipay", "qqpay", "wxpay", "bank"},
		Inputs: []PluginInput{
			{Key: "appurl", Label: "支付网关地址", Required: true},
			{Key: "appid", Label: "商户号", Required: true},
			{Key: "appkey", Label: "商户密钥", Secret: true, Required: true},
			{Key: "appmchid", Label: "通道编码", Required: true},
			{Key: "appswitch", Label: "支付跳转模式", Options: []PluginOption{
				{Value: "0", Label: "直接跳转接口（默认）"},
				{Value: "1", Label: "请求接口后跳转"},
				{Value: "2", Label: "请求接口后扫码"},
			}},
		},
		Build: newZyuPlugin,
	})
}

type zyuPlugin struct {
	pluginBase
	apiURL, memberid, key, bankcode string
	mode                            string // "0" page, "1" jump, "2" qr
}

// zyuNow supplies pay_applydate (overridable in tests).
var zyuNow = func() time.Time { return time.Now() }

func newZyuPlugin(c PluginChannel) (Provider, error) {
	api := strings.TrimSpace(c.Config["appurl"])
	host, ok := gatewayHost(api)
	if !ok {
		return nil, ErrInvalid
	}
	memberid := strings.TrimSpace(c.Config["appid"])
	key := c.Config["appkey"]
	bankcode := strings.TrimSpace(c.Config["appmchid"])
	if memberid == "" || len(memberid) > 64 || key == "" || len(key) > 256 || bankcode == "" || len(bankcode) > 64 {
		return nil, ErrInvalid
	}
	mode := c.Config["appswitch"]
	if mode == "" {
		mode = "0"
	}
	base, e := newPluginBase(c, pluginAccount(host, memberid, bankcode))
	if e != nil {
		return nil, e
	}
	return &zyuPlugin{pluginBase: base, apiURL: api, memberid: memberid, key: key, bankcode: bankcode, mode: mode}, nil
}

func (p *zyuPlugin) methods() []string {
	// Page mode submits a PC form; request modes jump or show a QR.
	if p.mode == "2" {
		return []string{"native"}
	}
	if p.payType == "alipay" {
		return []string{"page"}
	}
	return []string{"h5"}
}

func (p *zyuPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods()}
}

// zyuSign: UPPERCASE md5 over ksort'd non-empty fields ("k=v&"...) + "key=<key>".
func zyuSign(param map[string]string, key string) string {
	return strings.ToUpper(md5Hex(epaySignStringZyu(param) + "key=" + key))
}

// epaySignStringZyu builds the "k=v&"...(trailing &) base used by make_sign.
// It skips the "sign"/"pay_md5sign" output field and empty values.
func epaySignStringZyu(param map[string]string) string {
	keys := make([]string, 0, len(param))
	for k, v := range param {
		if k == "sign" || k == "pay_md5sign" || v == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(param[k])
		sb.WriteByte('&')
	}
	return sb.String()
}

// zyuAmount renders cents as the original's (float)$realmoney would stringify:
// two-decimal yuan with trailing zeros and a trailing dot trimmed
// ("1.00"->"1", "1.50"->"1.5", "1.23"->"1.23"). The signature must match this.
func zyuAmount(cents int64) string {
	s := yuan(cents) // e.g. "1.00"
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return s
}

func (p *zyuPlugin) ValidateCreate(r CreateRequest) error {
	return validatePluginCreate(r, p.methods())
}

func (p *zyuPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	data := map[string]string{
		"pay_memberid":    p.memberid,
		"pay_orderid":     r.TradeNo,
		"pay_amount":      zyuAmount(r.Amount),
		"pay_applydate":   zyuNow().Format("2006-01-02 15:04:05"),
		"pay_bankcode":    p.bankcode,
		"pay_notifyurl":   r.NotifyURL,
		"pay_callbackurl": r.ReturnURL,
	}
	data["pay_md5sign"] = zyuSign(data, p.key)
	data["pay_productname"] = r.Subject

	if p.mode == "0" {
		return CreateResult{MaterialType: "html", Material: pluginForm(p.apiURL, data, "正在跳转")}, nil
	}
	form := url.Values{}
	for k, v := range data {
		form.Set(k, v)
	}
	raw, e := p.postForm(ctx, "zyu.create", p.apiURL, form)
	if e != nil {
		return CreateResult{}, e
	}
	out, e := callbackJSON(raw)
	if e != nil {
		return CreateResult{}, &UnknownOutcome{Operation: "zyu.create", Cause: e}
	}
	if !zyuOK(out) {
		return CreateResult{}, rejected("ZYU_" + zyuRespCode(out))
	}
	payURL := zyuPayURL(out)
	if payURL == "" {
		return CreateResult{}, rejected("ZYU_NO_PAY_URL")
	}
	if p.mode == "2" {
		if len(payURL) > 4096 || strings.ContainsAny(payURL, "\x00\r\n") {
			return CreateResult{}, rejected("ZYU_NO_PAY_URL")
		}
		return CreateResult{MaterialType: "qrcode", Material: payURL}, nil
	}
	if !validPaymentURL(payURL) {
		return CreateResult{}, rejected("ZYU_NO_PAY_URL")
	}
	return CreateResult{MaterialType: "jump", Material: payURL}, nil
}

// zyuOK mirrors the plugin's permissive status check.
func zyuOK(out map[string]any) bool {
	switch jsonText(out, "status") {
	case "200", "success", "1":
		return true
	}
	return jsonText(out, "code") == "200"
}

func zyuRespCode(out map[string]any) string {
	if c := jsonText(out, "code"); c != "" {
		return c
	}
	return jsonText(out, "status")
}

// zyuPayURL extracts the pay URL from the several shapes the gateway may use.
func zyuPayURL(out map[string]any) string {
	if d, ok := out["data"].(map[string]any); ok {
		if u := jsonText(d, "payUrl"); u != "" {
			return u
		}
	}
	if u := jsonText(out, "data"); u != "" {
		if _, isObj := out["data"].(map[string]any); !isObj {
			return u
		}
	}
	for _, k := range []string{"payurl", "payUrl", "pay_url"} {
		if u := jsonText(out, k); u != "" {
			return u
		}
	}
	return ""
}

func (p *zyuPlugin) NotificationMethods() []string {
	return []string{http.MethodGet, http.MethodPost}
}

func (p *zyuPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	params, e := callbackParams(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	data := map[string]string{
		"memberid":       params["memberid"],
		"orderid":        params["orderid"],
		"amount":         params["amount"],
		"datetime":       params["datetime"],
		"transaction_id": params["transaction_id"],
		"returncode":     params["returncode"],
	}
	if !signEqual(zyuSign(data, p.key), params["sign"], false) {
		return PaymentEvent{}, ErrAuthentication
	}
	if data["returncode"] != "00" {
		return PaymentEvent{}, ErrInvalid
	}
	amount, e := parseYuan(data["amount"])
	if e != nil || expected.Amount <= 0 || amount != expected.Amount || data["orderid"] != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, data["transaction_id"], "", amount), nil
}

// The original echoes "OK"/"FAIL".
func (p *zyuPlugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return http.StatusBadRequest, "text/plain; charset=utf-8", "FAIL"
	}
	return http.StatusOK, "text/plain; charset=utf-8", "OK"
}

var _ Provider = (*zyuPlugin)(nil)
var _ NotificationVerifier = (*zyuPlugin)(nil)
var _ CreateValidator = (*zyuPlugin)(nil)
