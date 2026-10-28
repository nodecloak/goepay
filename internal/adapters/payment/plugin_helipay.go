package payment

// Port of epay_release/plugins/helipay (合利宝). The in-scope products (scan,
// public/JSAPI, H5) use an MD5 signature, not 国密: make_sign concatenates the
// parameter VALUES in insertion order, each prefixed with "&", skipping the
// "sign" field, then appends "&"+appkey and MD5-hex-es the whole string. The
// request is x-www-form-urlencoded to a single gateway action URL; the reply is
// JSON with rt2_retCode 0000/0001 on success.
//
// Notifications are form POST; the signature covers a FIXED ordered field list
// (rt1..rt8) with the same make_sign scheme. rt4_status SUCCESS, rt2_orderId is
// our TradeNo, rt5_orderAmount is yuan.
//
// Methods: QR (SCAN) → "native" for alipay/wxpay/qqpay/bank; WeChat/Alipay
// public (PUBLIC, needs openid) → "jsapi"; H5 (AppPayH5WFT) for wxpay → "h5".
// Refund (AppPayRefund) supported.
//
// The SM2 分账 (profit-sharing split) product, DES3 rule encryption, and the
// apply/settle/trade/complaint business callbacks are OUT OF SCOPE (reported).
// Because the in-scope paths are MD5 only, this plugin needs no SM primitive.

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "helipay",
		Title:  "合利宝",
		Author: "合利宝",
		Link:   "http://www.helipay.com/",
		Types:  []string{"alipay", "wxpay", "qqpay", "bank"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "商户编号", Required: true},
			{Key: "appkey", Label: "扫码产品-签名密钥", Secret: true, Required: true},
			{Key: "appsecret", Label: "扫码产品-加密密钥", Secret: true},
			{Key: "appmchid", Label: "子商户号", Note: "留空为使用商户编号发起支付"},
			{Key: "reportid", Label: "扫码支付-报备ID", Note: "可留空，多个报备ID可用,隔开"},
			{Key: "reportid2", Label: "公众号支付-报备ID", Note: "可留空，多个报备ID可用,隔开"},
			{Key: "reportid3", Label: "H5支付-报备ID", Note: "可留空，多个报备ID可用,隔开"},
			{Key: "h5appid", Label: "H5支付-AppId", Note: "可留空"},
		},
		Build: newHelipayPlugin,
	})
}

// helipayAPIURL is the gateway action URL. It is a var (not const) only so the
// test can point it at an httptest server; production never reassigns it.
var helipayAPIURL = "http://pay.trx.helipay.com/trx/app/interface.action"

type helipayPlugin struct {
	pluginBase
	custNo, signKey     string
	reportID, reportID2 string
	reportID3, h5appID  string
}

func newHelipayPlugin(c PluginChannel) (Provider, error) {
	custNo := strings.TrimSpace(c.Config["appid"])
	signKey := c.Config["appkey"]
	if custNo == "" || len(custNo) > 64 || signKey == "" || len(signKey) > 256 {
		return nil, ErrInvalid
	}
	// The original uses appmchid as the customerNumber when present.
	effective := custNo
	if sub := strings.TrimSpace(c.Config["appmchid"]); sub != "" {
		effective = sub
	}
	base, err := newPluginBase(c, pluginAccount(helipayAPIURL, effective))
	if err != nil {
		return nil, err
	}
	return &helipayPlugin{
		pluginBase: base,
		custNo:     effective,
		signKey:    signKey,
		reportID:   strings.TrimSpace(c.Config["reportid"]),
		reportID2:  strings.TrimSpace(c.Config["reportid2"]),
		reportID3:  strings.TrimSpace(c.Config["reportid3"]),
		h5appID:    strings.TrimSpace(c.Config["h5appid"]),
	}, nil
}

func (p *helipayPlugin) methods() []string {
	switch p.payType {
	case "wxpay":
		return []string{"native", "jsapi", "h5"}
	case "alipay":
		return []string{"native", "jsapi"}
	default: // qqpay, bank
		return []string{"native"}
	}
}

func (p *helipayPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

func (p *helipayPlugin) ValidateCreate(r CreateRequest) error {
	if e := validatePluginCreate(r, p.methods()); e != nil {
		return e
	}
	if r.Method == "jsapi" && (r.OpenID == "" || len(r.OpenID) > 128) {
		return ErrInvalid
	}
	return nil
}

// helipaySign reproduces make_sign: "&"+value for each entry in the given order
// (skipping "sign"), then "&"+key, MD5-hex.
func helipaySign(ordered []helipayKV, key string) string {
	var sb strings.Builder
	for _, kv := range ordered {
		if kv.k == "sign" {
			continue
		}
		sb.WriteByte('&')
		sb.WriteString(kv.v)
	}
	sb.WriteByte('&')
	sb.WriteString(key)
	return md5Hex(sb.String())
}

type helipayKV struct{ k, v string }

// appType maps a GOEPAY pay type to helipay's appType field.
func (p *helipayPlugin) appType() string {
	switch p.payType {
	case "alipay":
		return "ALIPAY"
	case "wxpay":
		return "WXPAY"
	case "qqpay":
		return "QQPAY"
	default:
		return "UNIONPAY"
	}
}

// post sends an ordered param list (sign appended) as a form and returns the
// JSON reply, honoring rt2_retCode success semantics.
func (p *helipayPlugin) post(ctx context.Context, operation string, ordered []helipayKV) (map[string]any, error) {
	sign := helipaySign(ordered, p.signKey)
	form := url.Values{}
	for _, kv := range ordered {
		form.Set(kv.k, kv.v)
	}
	form.Set("sign", sign)
	form.Set("signatureType", "MD5")
	raw, err := p.postForm(ctx, operation, helipayAPIURL, form)
	if err != nil {
		return nil, err
	}
	out, err := callbackJSON(raw)
	if err != nil {
		return nil, &UnknownOutcome{Operation: operation, Cause: err}
	}
	code := jsonText(out, "rt2_retCode")
	if code != "0000" && code != "0001" {
		if code == "" {
			return nil, &UnknownOutcome{Operation: operation, Cause: smErrUnreadable}
		}
		return nil, rejected("HELIPAY_" + code)
	}
	return out, nil
}

func (p *helipayPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	switch r.Method {
	case "native":
		ordered := []helipayKV{
			{"P1_bizType", "AppPay"},
			{"P2_orderId", r.TradeNo},
			{"P3_customerNumber", p.custNo},
			{"P4_payType", "SCAN"},
			{"P5_orderAmount", yuan(r.Amount)},
			{"P6_currency", "CNY"},
			{"P7_authcode", "1"},
			{"P8_appType", p.appType()},
			{"P9_notifyUrl", r.NotifyURL},
			{"P10_successToUrl", ""},
			{"P11_orderIp", r.ClientIP},
			{"P12_goodsName", r.Subject},
			{"P13_goodsDetail", ""},
			{"P14_desc", ""},
		}
		if p.reportID != "" {
			ordered = append(ordered, helipayKV{"P15_subMerchantId", p.reportID})
		}
		out, err := p.post(ctx, "helipay.scan", ordered)
		if err != nil {
			return CreateResult{}, err
		}
		qr := jsonText(out, "rt8_qrcode")
		res, ok := materialFromLinks("", qr, "")
		if !ok {
			return CreateResult{}, rejected("HELIPAY_NO_QRCODE")
		}
		res.ProviderTradeNo = jsonText(out, "rt6_serialNumber")
		return res, nil
	case "jsapi":
		isRaw := "1"
		ordered := []helipayKV{
			{"P1_bizType", "AppPayPublic"},
			{"P2_orderId", r.TradeNo},
			{"P3_customerNumber", p.custNo},
			{"P4_payType", "PUBLIC"},
			{"P5_appid", ""},
			{"P6_deviceInfo", "WEB"},
			{"P7_isRaw", isRaw},
			{"P8_openid", r.OpenID},
			{"P9_orderAmount", yuan(r.Amount)},
			{"P10_currency", "CNY"},
			{"P11_appType", p.appType()},
			{"P12_notifyUrl", r.NotifyURL},
			{"P13_successToUrl", r.ReturnURL},
			{"P14_orderIp", r.ClientIP},
			{"P15_goodsName", r.Subject},
			{"P16_goodsDetail", ""},
			{"P17_limitCreditPay", ""},
			{"P18_desc", ""},
		}
		if p.reportID2 != "" {
			ordered = append(ordered, helipayKV{"P20_subMerchantId", p.reportID2})
		}
		out, err := p.post(ctx, "helipay.public", ordered)
		if err != nil {
			return CreateResult{}, err
		}
		payInfo := jsonText(out, "rt10_payInfo")
		if payInfo == "" || len(payInfo) > 8192 {
			return CreateResult{}, rejected("HELIPAY_NO_PAYINFO")
		}
		return CreateResult{MaterialType: "jsapi", Material: payInfo, ProviderTradeNo: jsonText(out, "rt6_serialNumber")}, nil
	case "h5":
		ordered := []helipayKV{
			{"P1_bizType", "AppPayH5WFT"},
			{"P2_orderId", r.TradeNo},
			{"P3_customerNumber", p.custNo},
			{"P4_orderAmount", yuan(r.Amount)},
			{"P5_currency", "CNY"},
			{"P6_orderIp", r.ClientIP},
			{"P7_notifyUrl", r.NotifyURL},
			{"P8_appPayType", p.appType()},
			{"P9_payType", "WAP"},
			{"P10_appName", ""},
			{"P11_deviceInfo", "AND_WAP"},
			{"P12_applicationId", ""},
			{"P13_goodsName", r.Subject},
			{"P14_goodsDetail", ""},
			{"P15_desc", ""},
		}
		// The original appends these AFTER signing (they are not in make_sign's
		// scope because make_sign is computed on the 15-field array).
		out, err := p.postH5(ctx, ordered, r)
		if err != nil {
			return CreateResult{}, err
		}
		payInfo := jsonText(out, "rt8_payInfo")
		res, ok := materialFromLinks(payInfo, "", "")
		if !ok {
			if payInfo == "" || len(payInfo) > 8192 {
				return CreateResult{}, rejected("HELIPAY_NO_PAYINFO")
			}
			return CreateResult{MaterialType: "jump", Material: payInfo, ProviderTradeNo: jsonText(out, "rt6_serialNumber")}, nil
		}
		res.ProviderTradeNo = jsonText(out, "rt6_serialNumber")
		return res, nil
	}
	return CreateResult{}, ErrInvalid
}

// postH5 signs the 15-field array, then adds the extra post-sign fields the
// original sets (subMerchantId, isRaw, nonRawMode, appId, successToUrl).
func (p *helipayPlugin) postH5(ctx context.Context, ordered []helipayKV, r CreateRequest) (map[string]any, error) {
	sign := helipaySign(ordered, p.signKey)
	form := url.Values{}
	for _, kv := range ordered {
		form.Set(kv.k, kv.v)
	}
	form.Set("sign", sign)
	form.Set("signatureType", "MD5")
	if p.reportID3 != "" {
		form.Set("subMerchantId", p.reportID3)
	}
	form.Set("isRaw", "0")
	form.Set("nonRawMode", "0")
	form.Set("appId", p.h5appID)
	form.Set("successToUrl", r.ReturnURL)
	raw, err := p.postForm(ctx, "helipay.h5", helipayAPIURL, form)
	if err != nil {
		return nil, err
	}
	out, err := callbackJSON(raw)
	if err != nil {
		return nil, &UnknownOutcome{Operation: "helipay.h5", Cause: err}
	}
	code := jsonText(out, "rt2_retCode")
	if code != "0000" && code != "0001" {
		if code == "" {
			return nil, &UnknownOutcome{Operation: "helipay.h5", Cause: smErrUnreadable}
		}
		return nil, rejected("HELIPAY_" + code)
	}
	return out, nil
}

// Notifications are form POST; the signature covers the fixed rt1..rt8 fields.
func (p *helipayPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	params, err := callbackParams(raw)
	if err != nil {
		return PaymentEvent{}, err
	}
	ordered := []helipayKV{
		{"rt1_customerNumber", params["rt1_customerNumber"]},
		{"rt2_orderId", params["rt2_orderId"]},
		{"rt3_systemSerial", params["rt3_systemSerial"]},
		{"rt4_status", params["rt4_status"]},
		{"rt5_orderAmount", params["rt5_orderAmount"]},
		{"rt6_currency", params["rt6_currency"]},
		{"rt7_timestamp", params["rt7_timestamp"]},
		{"rt8_desc", params["rt8_desc"]},
	}
	if !signEqual(helipaySign(ordered, p.signKey), params["sign"], true) {
		return PaymentEvent{}, ErrAuthentication
	}
	if params["rt4_status"] != "SUCCESS" {
		return PaymentEvent{}, ErrInvalid
	}
	amount, err := parseYuan(params["rt5_orderAmount"])
	if err != nil || expected.Amount <= 0 || amount != expected.Amount {
		return PaymentEvent{}, ErrAuthentication
	}
	if params["rt2_orderId"] != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, params["rt3_systemSerial"], params["rt19_subOpenId"], amount), nil
}

// Refund posts AppPayRefund; rt2_retCode 0000/0001 is success.
func (p *helipayPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	ordered := []helipayKV{
		{"P1_bizType", "AppPayRefund"},
		{"P2_orderId", r.TradeNo},
		{"P3_customerNumber", p.custNo},
		{"P4_refundOrderId", r.RefundNo},
		{"P5_amount", yuan(r.Amount)},
		{"P6_callbackUrl", ""},
	}
	out, err := p.post(ctx, "helipay.refund", ordered)
	if err != nil {
		return RefundResult{}, err
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(out, "rt7_serialNumber"), State: "SUCCESS", Amount: r.Amount}, nil
}

var _ Provider = (*helipayPlugin)(nil)
var _ NotificationVerifier = (*helipayPlugin)(nil)
var _ CreateValidator = (*helipayPlugin)(nil)
