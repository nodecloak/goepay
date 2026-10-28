package payment

// Port of epay_release/plugins/alipayhk (AlipayHK, legacy intlmapi gateway).
// Signs with MD5 over the ksort-joined "k=v&k=v" + MD5 key (sign_type=MD5),
// exactly the epaySignString shape. The original posts an auto-submit form to
// https://intlmapi.alipay.com/gateway.do (create_forex_trade for PC,
// create_forex_trade_wap for WAP, mobile.securitypay.pay for APP). The notify
// and the sync return are MD5-verified over $_POST / $_GET.
//
//   PC  (apptype 1): create_forex_trade  -> html auto-submit form -> "page".
//   WAP (apptype 2): create_forex_trade_wap -> html form -> "wap".
//   APP (apptype 3): mobile.securitypay.pay -> signed SDK string -> "app".
// The PC QR sub-flow (buildRequestForm REDIRECT -> scrape qrCode) and the APP
// scheme page are page-rendering concerns we map to the base product above.
//
// Currency: the original always sends currency=HKD and rmb_fee=realmoney (yuan,
// CNY) — i.e. the payer is charged in HKD but the merchant quotes the RMB
// amount. We keep that exactly: amount goes out as rmb_fee in yuan and
// currency=HKD. The settlement/charge currency Alipay reports back is HKD
// (total_fee) while rmb_fee is the CNY amount; we verify against rmb_fee so the
// GOEPAY CNY order reconciles. Reported in p03.md.

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "alipayhk",
		Title:  "AlipayHK",
		Author: "支付宝",
		Link:   "https://global.alipay.com/",
		Types:  []string{"alipay"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "Partner ID", Required: true},
			{Key: "appkey", Label: "MD5 Key", Secret: true, Required: true},
			{Key: "appswitch", Label: "支付时选择钱包类型", Options: []PluginOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}}},
		},
		AppTypes: []PluginOption{
			{Value: "1", Label: "PC支付"},
			{Value: "2", Label: "WAP支付"},
			{Value: "3", Label: "APP支付"},
		},
		Note:  "支付时选择钱包类型开启后，支付时可选择Alipay或AlipayHK，关闭则默认使用Alipay",
		Build: newAlipayhkPlugin,
	})
}

const alipayhkGateway = "https://intlmapi.alipay.com/gateway.do"

type alipayhkPlugin struct {
	pluginBase
	partner, key string
	appTypes     map[string]bool
	// gateway is alipayhkGateway in production; tests point it at httptest.
	gateway string
	// refundGatewayOverride, when set, is used for the refund POST only (the
	// refund is the sole server-side call; create builds a client-side form).
	refundGatewayOverride string
}

func newAlipayhkPlugin(c PluginChannel) (Provider, error) {
	partner := strings.TrimSpace(c.Config["appid"])
	key := c.Config["appkey"]
	if partner == "" || len(partner) > 64 || strings.ContainsAny(partner, "&=?# ") || key == "" || len(key) > 256 {
		return nil, ErrInvalid
	}
	appTypes := map[string]bool{}
	for _, t := range c.AppTypes {
		appTypes[t] = true
	}
	// Default (no apptype selected) behaves like PC in the original fall-through.
	if len(appTypes) == 0 {
		appTypes["1"] = true
	}
	base, e := newPluginBase(c, pluginAccount(alipayhkGateway, partner))
	if e != nil {
		return nil, e
	}
	return &alipayhkPlugin{pluginBase: base, partner: partner, key: key, appTypes: appTypes, gateway: alipayhkGateway}, nil
}

// methods maps the enabled apptype products to GOEPAY methods for pay type
// alipay: PC -> page, WAP -> wap, APP -> app.
func (p *alipayhkPlugin) methods() []string {
	var m []string
	if p.appTypes["1"] {
		m = append(m, "page")
	}
	if p.appTypes["2"] {
		m = append(m, "wap")
	}
	if p.appTypes["3"] {
		m = append(m, "app")
	}
	return m
}

func (p *alipayhkPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

func (p *alipayhkPlugin) ValidateCreate(r CreateRequest) error {
	return validatePluginCreate(r, p.methods())
}

// alipayhkSign computes md5(sortedContent + key), the AlipayGlobalClient MD5
// path. epaySignString already skips sign/sign_type and empty values and sorts.
func (p *alipayhkPlugin) alipayhkSign(params map[string]string) string {
	return md5Hex(epaySignString(params) + p.key)
}

var alipayhkTradeInfo = `{"business_type":"5","other_business_type":"\u5728\u7ebf\u5145\u503c"}`

func (p *alipayhkPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	params := map[string]string{
		"partner":           p.partner,
		"notify_url":        r.NotifyURL,
		"return_url":        r.ReturnURL,
		"out_trade_no":      r.TradeNo,
		"subject":           r.Subject,
		"currency":          "HKD",
		"rmb_fee":           yuan(r.Amount),
		"product_code":      "NEW_WAP_OVERSEAS_SELLER",
		"trade_information": alipayhkTradeInfo,
		"_input_charset":    "utf-8",
	}
	switch r.Method {
	case "page":
		params["service"] = "create_forex_trade"
		params["qr_pay_mode"] = "4"
		params["qrcode_width"] = "230"
		params["refer_url"] = originOf(r.ReturnURL)
	case "wap":
		params["service"] = "create_forex_trade_wap"
		params["refer_url"] = originOf(r.ReturnURL)
	case "app":
		params["service"] = "mobile.securitypay.pay"
		params["payment_type"] = "1"
		params["seller_id"] = p.partner
		params["forex_biz"] = "FP"
		params["refer_url"] = originOf(r.ReturnURL)
	default:
		return CreateResult{}, ErrInvalid
	}
	params["sign"] = p.alipayhkSign(params)
	params["sign_type"] = "MD5"

	if r.Method == "app" {
		// buildSdkParam: the urlencoded signed query string for the app SDK.
		form := url.Values{}
		for k, v := range params {
			form.Set(k, v)
		}
		return CreateResult{MaterialType: "app", Material: form.Encode()}, nil
	}
	// PC/WAP: an auto-submit form to the gateway (buildRequestForm POST).
	action := alipayhkGateway + "?_input_charset=utf-8"
	return CreateResult{MaterialType: "html", Material: pluginForm(action, params, "正在跳转")}, nil
}

// originOf returns scheme://host of a URL for refer_url; "" if unparseable.
func originOf(raw string) string {
	if h, ok := gatewayHost(raw); ok {
		return h + "/"
	}
	return ""
}

// The original reads $_POST in notify() and $_GET in return(); accept both.
func (p *alipayhkPlugin) NotificationMethods() []string {
	return []string{http.MethodGet, http.MethodPost}
}

// VerifyNotification mirrors notify(): MD5-verify the callback, require
// trade_status TRADE_SUCCESS/TRADE_FINISHED, then match out_trade_no and the
// amount. The original does not re-check the amount; we add the rmb_fee ==
// expected amount check that GOEPAY requires. rmb_fee is the CNY amount we
// sent; total_fee is the HKD charge, which we do not reconcile against the CNY
// order.
func (p *alipayhkPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	params, e := callbackParams(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	if !signEqual(p.alipayhkSign(params), params["sign"], false) {
		return PaymentEvent{}, ErrAuthentication
	}
	if params["partner"] != "" && params["partner"] != p.partner {
		return PaymentEvent{}, ErrAuthentication
	}
	status := params["trade_status"]
	if status != "TRADE_SUCCESS" && status != "TRADE_FINISHED" {
		return PaymentEvent{}, ErrInvalid
	}
	if params["out_trade_no"] != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	// rmb_fee is the CNY amount; fall back to total_fee only if rmb_fee absent
	// (older gateways). Both are yuan strings.
	amountField := params["rmb_fee"]
	if amountField == "" {
		amountField = params["total_fee"]
	}
	amount, e := parseYuan(amountField)
	if e != nil || expected.Amount <= 0 || amount != expected.Amount {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, params["trade_no"], params["buyer_id"], amount), nil
}

// Refund posts forex_refund to the gateway; the XML reply's is_success == 'T'
// means success (AlipayGlobalClient::sendRequest + xml2array). The original
// sends return_rmb_amount in yuan and currency HKD.
func (p *alipayhkPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	params := map[string]string{
		"service":           "forex_refund",
		"partner":           p.partner,
		"out_return_no":     r.RefundNo,
		"out_trade_no":      r.TradeNo,
		"return_rmb_amount": yuan(r.Amount),
		"currency":          "HKD",
		"gmt_return":        alipayhkNow(),
		"_input_charset":    "utf-8",
	}
	params["sign"] = p.alipayhkSign(params)
	params["sign_type"] = "MD5"
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	gw := p.gateway
	if p.refundGatewayOverride != "" {
		gw = p.refundGatewayOverride
	}
	raw, e := p.postForm(ctx, "alipayhk.refund", gw+"?_input_charset=utf-8", form)
	if e != nil {
		return RefundResult{}, e
	}
	flat, e := xmlFlat(raw)
	if e != nil {
		// Unreadable XML — the refund may have been accepted.
		return RefundResult{}, &UnknownOutcome{Operation: "alipayhk.refund", Cause: e}
	}
	if flat["is_success"] != "T" {
		code := flat["error"]
		if code == "" {
			code = "UNKNOWN"
		}
		return RefundResult{}, rejected("ALIPAYHK_" + code)
	}
	return RefundResult{RefundNo: r.RefundNo, State: "SUCCESS", Amount: r.Amount}, nil
}

// alipayhkNow renders gmt_return as the original date('Y-m-d H:i:s') (server
// local time); only used as a non-signed refund request field.
func alipayhkNow() string { return time.Now().Format("2006-01-02 15:04:05") }

var _ Provider = (*alipayhkPlugin)(nil)
var _ NotificationVerifier = (*alipayhkPlugin)(nil)
var _ CreateValidator = (*alipayhkPlugin)(nil)
