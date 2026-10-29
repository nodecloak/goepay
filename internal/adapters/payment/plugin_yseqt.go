package payment

// Port of epay_release/plugins/yseqt (银盛e企通). Gateway
// https://eqt.ysepay.com/api/trade, serviceNo-dispatched. The merchant RSA key
// (from a .pfx bundle, password = 私钥证书密码) signs the ksort'd envelope
// RSA-SHA1; the 银盛 platform public key (bundled businessgate.cer) verifies
// responses and callbacks. Create:
//   native -> serviceNo scanPay (QR content qrCode),
//   jsapi  -> serviceNo jsPay (payData: a JSAPI string or redirectUrl).
// Callbacks POST field bizResponseJson plus the sorted envelope with sign;
// success is state=SUCCESS and a matching requestNo + amount. Refund uses
// serviceNo refund. TRANSFERS (transfer / transfer_query / balance_query) and
// 进件/签约 (applynotify) are OUT OF SCOPE for this wave and not implemented.

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	yseqtGateway = "https://eqt.ysepay.com/api/trade"
	// Same bundled 银盛 platform certificate as ysepay.
	yseqtBusinessgateCert = "MIICizCCAfSgAwIBAgIEIL4JYzANBgkqhkiG9w0BAQQFADBcMQ8wDQYDVQQDDAZ5c2VwYXkxDzANBgNVBAsMBnlzZXBheTERMA8GA1UECgwIb3JnYW5pemUxCzAJBgNVBAcMAnN6MQswCQYDVQQGEwJjbjELMAkGA1UECAwCZ2QwHhcNMTMwMzA0MDgwMzUyWhcNNDMwMjI1MDgwMzUyWjBcMQ8wDQYDVQQDDAZ5c2VwYXkxDzANBgNVBAsMBnlzZXBheTERMA8GA1UECgwIb3JnYW5pemUxCzAJBgNVBAcMAnN6MQswCQYDVQQGEwJjbjELMAkGA1UECAwCZ2QwgZ8wDQYJKoZIhvcNAQEBBQADgY0AMIGJAoGBALtI+SYhR/Zq7CSBtVyEzJ94IOAgK4DUYqtLBFsMjbhQxWHMOBwjEJSYUbn8N9w8nPR3ehX0aCR3j/s2a15G+Y2c44Y06eZHoAYQpp8OkvWYB2md2amQBnR/o5Wjc/LkkODbC3x367d0XQAVbB3auuuqxxa/ElUHnkOe5l9eXYLJAgMBAAGjWjBYMB0GA1UdDgQWBBQBrC+ShJLjpMYmmEbcr6p6Ma6FTDAfBgNVHSMEGDAWgBTNRc1cSoLvynQarb/pETbgWClymDAJBgNVHRMEAjAAMAsGA1UdDwQEAwIEkDANBgkqhkiG9w0BAQQFAAOBgQBB5m8oxrt1KnfbFP+akJvctjhbta5JzenDlCfYcZtykYLSMwkmhd670/y4tJc35P0ZX/x1bJiAS+2i90L6f8P/WuBMxeI1KuX4Grk22ivw3nweWbGOUCN/PEC5YwUqHopeqHzs09O12ANXXgS35L2wgUxQOB1jcuhLXy2Z94pGYg=="
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "yseqt",
		Title:  "银盛e企通",
		Author: "银盛",
		Link:   "https://www.ysepay.com/",
		Types:  []string{"alipay", "wxpay", "bank"},
		// transtypes ['bank'] (transfers) are intentionally not declared:
		// transfers are out of scope for this wave.
		Inputs: []PluginInput{
			{Key: "appid", Label: "服务商商户号", Required: true},
			{Key: "appkey", Label: "私钥证书密码", Secret: true, Required: true},
			{Key: "appmchid", Label: "收款商户号", Required: true},
			{Key: "appcert", Label: "商户私钥证书client.pfx", Note: "上传的 .pfx 证书内容（base64）", PEM: true, Required: true},
		},
		Note:  "只能使用RSA证书！将商户私钥证书client.pfx内容（base64编码）填入；证书密码填入私钥证书密码。转账功能暂未支持。",
		Build: newYseqtPlugin,
	})
}

type yseqtPlugin struct {
	pluginBase
	srcMerc string
	payeeNo string
	priv    *rsa.PrivateKey
	pub     *rsa.PublicKey
	gateway string
}

func newYseqtPlugin(c PluginChannel) (Provider, error) {
	src := strings.TrimSpace(c.Config["appid"])
	payee := strings.TrimSpace(c.Config["appmchid"])
	if src == "" || len(src) > 32 || payee == "" || len(payee) > 32 {
		return nil, ErrInvalid
	}
	priv, e := yseqtParsePFX(c.Certificates["appcert"], c.Config["appkey"])
	if e != nil {
		return nil, ErrInvalid
	}
	pub, e := yseqtBundledPublic()
	if e != nil {
		return nil, ErrInvalid
	}
	host, _ := gatewayHost(yseqtGateway)
	base, e := newPluginBase(c, pluginAccount(host, src, payee))
	if e != nil {
		return nil, e
	}
	return &yseqtPlugin{pluginBase: base, srcMerc: src, payeeNo: payee, priv: priv, pub: pub, gateway: yseqtGateway}, nil
}

func yseqtParsePFX(certB64, password string) (*rsa.PrivateKey, error) {
	certB64 = strings.Join(strings.Fields(certB64), "")
	der, e := base64.StdEncoding.DecodeString(certB64)
	if e != nil || len(der) == 0 {
		return nil, ErrInvalid
	}
	key, e := pkcs12DecodeKey(der, password)
	if e != nil {
		return nil, ErrInvalid
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok || rsaKey.N.BitLen() < pluginMinRSABits {
		return nil, ErrInvalid
	}
	if e := rsaKey.Validate(); e != nil {
		return nil, ErrInvalid
	}
	return rsaKey, nil
}

func yseqtBundledPublic() (*rsa.PublicKey, error) {
	der, e := base64.StdEncoding.DecodeString(yseqtBusinessgateCert)
	if e != nil {
		return nil, e
	}
	cert, e := x509.ParseCertificate(der)
	if e != nil {
		return nil, e
	}
	key, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return nil, ErrInvalid
	}
	return key, nil
}

func (p *yseqtPlugin) bankType() (string, bool) {
	switch p.payType {
	case "alipay":
		return "1903000", true
	case "wxpay":
		return "1902000", true
	case "bank":
		return "9001002", true
	}
	return "", false
}

// jsPay payMode per pay type (from the original): alipay 26, wxpay 28
// (public account), bank 30.
func (p *yseqtPlugin) jsPayMode() (string, bool) {
	switch p.payType {
	case "alipay":
		return "26", true
	case "wxpay":
		return "28", true
	case "bank":
		return "30", true
	}
	return "", false
}

func (p *yseqtPlugin) methods() []string { return []string{"native", "jsapi"} }

func (p *yseqtPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

// yseqtSignContent reproduces getSignContent: ksort, drop sign, skip
// '@'-prefixed and null values, join "k=v&" and trim trailing '&'.
func yseqtSignContent(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if k == "sign" || strings.HasPrefix(v, "@") {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k + "=" + params[k] + "&")
	}
	return strings.TrimSuffix(sb.String(), "&")
}

func (p *yseqtPlugin) sign(params map[string]string) (string, error) {
	return rsaSign(p.priv, crypto.SHA1, yseqtSignContent(params))
}

func yseqtMarshal(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return strings.TrimRight(buf.String(), "\n")
}

func (p *yseqtPlugin) ValidateCreate(r CreateRequest) error {
	if e := validatePluginCreate(r, p.methods()); e != nil {
		return e
	}
	if r.Method == "jsapi" && strings.TrimSpace(r.OpenID) == "" {
		return ErrInvalid
	}
	return nil
}

func (p *yseqtPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	bank, ok := p.bankType()
	if !ok {
		return CreateResult{}, ErrUnsupported
	}
	if r.Method == "jsapi" {
		return p.createJSAPI(ctx, r, bank)
	}
	// native: scanPay, returns qrCode.
	biz := map[string]any{
		"requestNo":       r.TradeNo,
		"payeeMerchantNo": p.payeeNo,
		"orderDesc":       r.Subject,
		"amount":          yuan(r.Amount),
		"bankType":        bank,
		"notifyUrl":       r.NotifyURL,
	}
	resp, e := p.execute(ctx, "scanPay", biz)
	if e != nil {
		return CreateResult{}, e
	}
	res, ok := materialFromLinks("", jsonText(resp, "qrCode"), "")
	if !ok {
		return CreateResult{}, rejected("YSEQT_NO_QR")
	}
	res.ProviderTradeNo = jsonText(resp, "tradeSn")
	return res, nil
}

func (p *yseqtPlugin) createJSAPI(ctx context.Context, r CreateRequest, bank string) (CreateResult, error) {
	mode, ok := p.jsPayMode()
	if !ok {
		return CreateResult{}, ErrUnsupported
	}
	biz := map[string]any{
		"requestNo":       r.TradeNo,
		"payeeMerchantNo": p.payeeNo,
		"orderDesc":       r.Subject,
		"amount":          yuan(r.Amount),
		"bankType":        bank,
		"payMode":         mode,
		"notifyUrl":       r.NotifyURL,
	}
	switch p.payType {
	case "wxpay":
		if r.OpenID == "" {
			return CreateResult{}, ErrInvalid
		}
		biz["wxOpenId"] = r.OpenID
	case "alipay":
		biz["alipayId"] = r.OpenID
	case "bank":
		biz["unionUserId"] = r.OpenID
	}
	resp, e := p.execute(ctx, "jsPay", biz)
	if e != nil {
		return CreateResult{}, e
	}
	payData := jsonText(resp, "payData")
	if payData == "" {
		return CreateResult{}, rejected("YSEQT_NO_PAYDATA")
	}
	// bank jsPay returns a payData JSON carrying a redirectUrl (jump);
	// alipay/wxpay return a JSAPI parameter string.
	if p.payType == "bank" {
		if inner, pe := callbackJSON([]byte(payData)); pe == nil {
			if red := jsonText(inner, "redirectUrl"); validPaymentURL(red) {
				return CreateResult{MaterialType: "jump", Material: red, ProviderTradeNo: jsonText(resp, "tradeSn")}, nil
			}
		}
	}
	return CreateResult{MaterialType: "jsapi", Material: payData, ProviderTradeNo: jsonText(resp, "tradeSn")}, nil
}

// execute signs and POSTs an envelope, parses the response, verifies its
// signature when present, and returns the decoded bizResponseJson object.
// code SYS000 is the transport-level success; subCode COM000/COM004 is the
// business success.
func (p *yseqtPlugin) execute(ctx context.Context, serviceNo string, biz map[string]any) (map[string]any, error) {
	params := map[string]string{
		"requestId":     yseqtRequestID(),
		"srcMerchantNo": p.srcMerc,
		"version":       "v2.0.0",
		"charset":       "UTF-8",
		"serviceNo":     serviceNo,
		"signType":      "RSA",
		"bizReqJson":    yseqtMarshal(biz),
	}
	sig, e := p.sign(params)
	if e != nil {
		return nil, e
	}
	params["sign"] = sig
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	raw, e := p.postForm(ctx, "yseqt."+serviceNo, p.gateway, form)
	if e != nil {
		return nil, e
	}
	top, e := callbackJSON(raw)
	if e != nil {
		return nil, &UnknownOutcome{Operation: "yseqt." + serviceNo, Cause: e}
	}
	// Verify the response signature when the gateway signed it.
	if sign := jsonText(top, "sign"); sign != "" {
		verify := map[string]string{}
		for k := range top {
			if k == "sign" {
				continue
			}
			verify[k] = jsonText(top, k)
		}
		if !rsaVerify(p.pub, crypto.SHA1, yseqtSignContent(verify), sign) {
			return nil, &UnknownOutcome{Operation: "yseqt." + serviceNo, Cause: errYseqtReply}
		}
	}
	if jsonText(top, "code") != "SYS000" {
		return nil, &UnknownOutcome{Operation: "yseqt." + serviceNo, Cause: errYseqtReply}
	}
	bizJSON := jsonText(top, "bizResponseJson")
	resp, e := callbackJSON([]byte(bizJSON))
	if e != nil {
		return nil, &UnknownOutcome{Operation: "yseqt." + serviceNo, Cause: e}
	}
	sub := jsonText(resp, "subCode")
	if sub == "COM000" || sub == "COM004" {
		return resp, nil
	}
	if sub != "" {
		return nil, rejected("YSEQT_" + sub)
	}
	return nil, &UnknownOutcome{Operation: "yseqt." + serviceNo, Cause: errYseqtReply}
}

func (p *yseqtPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	params, e := callbackParams(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	if params["sign"] == "" || !rsaVerify(p.pub, crypto.SHA1, yseqtSignContent(params), params["sign"]) {
		return PaymentEvent{}, ErrAuthentication
	}
	biz, e := callbackJSON([]byte(params["bizResponseJson"]))
	if e != nil {
		return PaymentEvent{}, ErrInvalid
	}
	if jsonText(biz, "state") != "SUCCESS" {
		return PaymentEvent{}, ErrInvalid
	}
	if jsonText(biz, "requestNo") != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	amount, e := parseYuan(jsonText(biz, "amount"))
	if e != nil || expected.Amount <= 0 || amount != expected.Amount {
		return PaymentEvent{}, ErrAuthentication
	}
	buyer := jsonText(biz, "openId")
	if buyer == "" {
		buyer = jsonText(biz, "userId")
	}
	return paidEvent(expected.TradeNo, jsonText(biz, "tradeSn"), buyer, amount), nil
}

func (p *yseqtPlugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return http.StatusOK, "text/plain; charset=utf-8", "fail"
	}
	return http.StatusOK, "text/plain; charset=utf-8", "success"
}

// Refund uses serviceNo refund; subCode COM000/COM004 is success.
func (p *yseqtPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.ProviderTradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	biz := map[string]any{
		"requestNo":     r.RefundNo,
		"origRequestNo": r.TradeNo,
		"origTradeSn":   r.ProviderTradeNo,
		"amount":        yuan(r.Amount),
		"reason":        "申请退款",
		"isDivision":    "N",
	}
	resp, e := p.execute(ctx, "refund", biz)
	if e != nil {
		return RefundResult{}, e
	}
	amount := r.Amount
	if f, fe := parseYuan(jsonText(resp, "amount")); fe == nil {
		amount = f
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(resp, "refundSn"), State: "SUCCESS", Amount: amount}, nil
}

func yseqtRequestID() string {
	return time.Now().Format("20060102150405") + yseqtRand(8)
}

// yseqtRand returns n random digits (crypto/rand backed), enough to keep
// request ids unique within a second.
func yseqtRand(n int) string {
	b := make([]byte, n)
	if _, e := rand.Read(b); e != nil {
		for i := range b {
			b[i] = '0'
		}
		return string(b)
	}
	for i := range b {
		b[i] = '0' + b[i]%10
	}
	return string(b)
}

type yseqtReplyError string

func (e yseqtReplyError) Error() string { return string(e) }

var errYseqtReply = yseqtReplyError("yseqt: unreadable upstream reply")

var _ Provider = (*yseqtPlugin)(nil)
var _ NotificationVerifier = (*yseqtPlugin)(nil)
var _ CreateValidator = (*yseqtPlugin)(nil)
