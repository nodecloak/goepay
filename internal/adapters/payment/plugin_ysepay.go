package payment

// Port of epay_release/plugins/ysepay (银盛支付). Signing uses the merchant RSA
// private key from a PKCS#12 (.pfx) bundle (password = 私钥证书密码), and the
// 银盛 platform public key is the bundled businessgate.cer. Requests are
// RSA-SHA1 signed over the ksort'd "k=v&k=v" string. Create:
//   native  -> qrcode.ysepay.com/gateway.do ysepay.online.qrcodepay (QR),
//   jsapi   -> ysepay.online.weixin.pay / alijsapi.pay / cupmulapp.qrcodepay,
//   wap     -> openapi.ysepay.com/gateway.do ysepay.online.wap.directpay.
//              createbyuser, rendered as an auto-submit form.
// Callbacks POST the sorted parameters with an RSA-SHA1 signature; success is
// trade_status=TRADE_SUCCESS and a matching out_trade_no + total_amount.
// Refund posts ysepay.online.trade.refund.

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/pkcs12"
)

// pkcs12DecodeKey extracts the private key from a PFX/P12 bundle.
func pkcs12DecodeKey(der []byte, password string) (any, error) {
	key, _, e := pkcs12.Decode(der, password)
	return key, e
}

// ysepayTimestamp reproduces date("Y-m-d H:i:s") in local time.
func ysepayTimestamp() string { return time.Now().Format("2006-01-02 15:04:05") }

// ysepayShopDate reproduces date("Ymd"); our TRADE_NO is yyyymmdd-prefixed, so
// its first 8 digits are the order date, matching the gateway's expectation.
func ysepayShopDate(tradeNo string) string {
	if len(tradeNo) >= 8 && ysepayAllDigits(tradeNo[:8]) {
		return tradeNo[:8]
	}
	return time.Now().Format("20060102")
}

func ysepayAllDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}

const (
	ysepayQRGateway  = "https://qrcode.ysepay.com/gateway.do"
	ysepayWapGateway = "https://openapi.ysepay.com/gateway.do"
	// Bundled 银盛 platform certificate (businessgate.cer, DER base64).
	ysepayBusinessgateCert = "MIICizCCAfSgAwIBAgIEIL4JYzANBgkqhkiG9w0BAQQFADBcMQ8wDQYDVQQDDAZ5c2VwYXkxDzANBgNVBAsMBnlzZXBheTERMA8GA1UECgwIb3JnYW5pemUxCzAJBgNVBAcMAnN6MQswCQYDVQQGEwJjbjELMAkGA1UECAwCZ2QwHhcNMTMwMzA0MDgwMzUyWhcNNDMwMjI1MDgwMzUyWjBcMQ8wDQYDVQQDDAZ5c2VwYXkxDzANBgNVBAsMBnlzZXBheTERMA8GA1UECgwIb3JnYW5pemUxCzAJBgNVBAcMAnN6MQswCQYDVQQGEwJjbjELMAkGA1UECAwCZ2QwgZ8wDQYJKoZIhvcNAQEBBQADgY0AMIGJAoGBALtI+SYhR/Zq7CSBtVyEzJ94IOAgK4DUYqtLBFsMjbhQxWHMOBwjEJSYUbn8N9w8nPR3ehX0aCR3j/s2a15G+Y2c44Y06eZHoAYQpp8OkvWYB2md2amQBnR/o5Wjc/LkkODbC3x367d0XQAVbB3auuuqxxa/ElUHnkOe5l9eXYLJAgMBAAGjWjBYMB0GA1UdDgQWBBQBrC+ShJLjpMYmmEbcr6p6Ma6FTDAfBgNVHSMEGDAWgBTNRc1cSoLvynQarb/pETbgWClymDAJBgNVHRMEAjAAMAsGA1UdDwQEAwIEkDANBgkqhkiG9w0BAQQFAAOBgQBB5m8oxrt1KnfbFP+akJvctjhbta5JzenDlCfYcZtykYLSMwkmhd670/y4tJc35P0ZX/x1bJiAS+2i90L6f8P/WuBMxeI1KuX4Grk22ivw3nweWbGOUCN/PEC5YwUqHopeqHzs09O12ANXXgS35L2wgUxQOB1jcuhLXy2Z94pGYg=="
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "ysepay",
		Title:  "银盛支付",
		Author: "银盛支付",
		Link:   "https://www.ysepay.com/",
		Types:  []string{"alipay", "qqpay", "wxpay", "bank"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "服务商商户号", Required: true},
			{Key: "appkey", Label: "私钥证书密码", Secret: true, Required: true},
			{Key: "appmchid", Label: "收款商户号", Note: "不填写则和服务商商户号相同"},
			{Key: "appurl", Label: "业务代码", Required: true},
			{Key: "appcert", Label: "商户私钥证书client.pfx", Note: "上传的 .pfx 证书内容（base64）", PEM: true, Required: true},
		},
		Note:  "只能使用RSA证书！将商户私钥证书client.pfx内容（base64编码）填入；证书密码填入私钥证书密码",
		Build: newYsepayPlugin,
	})
}

type ysepayPlugin struct {
	pluginBase
	partnerID  string
	sellerID   string
	businessNo string
	priv       *rsa.PrivateKey
	pub        *rsa.PublicKey
	// qrGateway defaults to the QR production gateway; overridden in tests.
	qrGateway string
}

func newYsepayPlugin(c PluginChannel) (Provider, error) {
	partner := strings.TrimSpace(c.Config["appid"])
	business := strings.TrimSpace(c.Config["appurl"])
	if partner == "" || len(partner) > 32 || business == "" || len(business) > 64 {
		return nil, ErrInvalid
	}
	seller := strings.TrimSpace(c.Config["appmchid"])
	if seller == "" {
		seller = partner
	}
	priv, e := ysepayParsePFX(c.Certificates["appcert"], c.Config["appkey"])
	if e != nil {
		return nil, ErrInvalid
	}
	pub, e := ysepayBundledPublic()
	if e != nil {
		return nil, ErrInvalid
	}
	host, _ := gatewayHost(ysepayQRGateway)
	base, e := newPluginBase(c, pluginAccount(host, partner, seller, business))
	if e != nil {
		return nil, e
	}
	return &ysepayPlugin{pluginBase: base, partnerID: partner, sellerID: seller, businessNo: business, priv: priv, pub: pub, qrGateway: ysepayQRGateway}, nil
}

// ysepayParsePFX decodes a base64-encoded PKCS#12 bundle and returns the RSA
// private key inside (the original reads client.pfx from disk; GOEPAY receives
// the same bytes base64-encoded in config).
func ysepayParsePFX(certB64, password string) (*rsa.PrivateKey, error) {
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

// ysepayBundledPublic parses the bundled businessgate certificate (1024-bit
// RSA, below the kit's 2048 floor, so it is parsed directly here).
func ysepayBundledPublic() (*rsa.PublicKey, error) {
	der, e := base64.StdEncoding.DecodeString(ysepayBusinessgateCert)
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

func (p *ysepayPlugin) bankType() (string, bool) {
	switch p.payType {
	case "alipay":
		return "1903000", true
	case "wxpay":
		return "1902000", true
	case "qqpay":
		return "1904000", true
	case "bank":
		return "9001002", true
	}
	return "", false
}

func (p *ysepayPlugin) methods() []string {
	switch p.payType {
	case "alipay":
		// QR scan, WAP html form, and 生活号 JSAPI.
		return []string{"native", "wap", "jsapi"}
	case "wxpay", "bank":
		return []string{"native", "jsapi"}
	default: // qqpay
		return []string{"native"}
	}
}

func (p *ysepayPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

// ysepaySignContent reproduces getSignContent: ksort, drop sign, skip empty and
// '@'-prefixed values, join "&k=v" then trim the leading '&'.
func ysepaySignContent(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if k == "sign" || strings.TrimSpace(v) == "" || strings.HasPrefix(v, "@") {
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
		sb.WriteString(k + "=" + params[k])
	}
	return sb.String()
}

func (p *ysepayPlugin) sign(params map[string]string) (string, error) {
	return rsaSign(p.priv, crypto.SHA1, ysepaySignContent(params))
}

// ysepayMarshal encodes biz_content without slash/unicode escaping.
func ysepayMarshal(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return strings.TrimRight(buf.String(), "\n")
}

func (p *ysepayPlugin) ValidateCreate(r CreateRequest) error {
	if e := validatePluginCreate(r, p.methods()); e != nil {
		return e
	}
	if r.Method == "jsapi" && strings.TrimSpace(r.OpenID) == "" {
		return ErrInvalid
	}
	return nil
}

func (p *ysepayPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	bank, ok := p.bankType()
	if !ok {
		return CreateResult{}, ErrUnsupported
	}
	switch r.Method {
	case "wap":
		if p.payType != "alipay" {
			return CreateResult{}, ErrInvalid
		}
		return p.createWap(r, bank)
	case "jsapi":
		return p.createJSAPI(ctx, r)
	default: // native
		return p.createQR(ctx, r, bank)
	}
}

// createQR calls ysepay.online.qrcodepay and returns source_qr_code_url.
func (p *ysepayPlugin) createQR(ctx context.Context, r CreateRequest, bank string) (CreateResult, error) {
	biz := map[string]string{
		"out_trade_no":    r.TradeNo,
		"shopdate":        ysepayShopDate(r.TradeNo),
		"subject":         r.Subject,
		"total_amount":    yuan(r.Amount),
		"currency":        "CNY",
		"seller_id":       p.sellerID,
		"timeout_express": "2h",
		"business_code":   p.businessNo,
		"bank_type":       bank,
		"submer_ip":       r.ClientIP,
	}
	result, e := p.execute(ctx, "ysepay.online.qrcodepay", "3.5", biz, r.NotifyURL, "")
	if e != nil {
		return CreateResult{}, e
	}
	res, ok := materialFromLinks("", jsonText(result, "source_qr_code_url"), "")
	if !ok {
		return CreateResult{}, rejected("YSE_NO_QR")
	}
	res.ProviderTradeNo = jsonText(result, "trade_no")
	return res, nil
}

// createJSAPI calls the weixin/alijsapi/cupmulapp order per pay type and
// returns the JSAPI parameter string (or the cup web_url as a jump).
func (p *ysepayPlugin) createJSAPI(ctx context.Context, r CreateRequest) (CreateResult, error) {
	switch p.payType {
	case "wxpay":
		biz := map[string]string{
			"out_trade_no": r.TradeNo, "shopdate": ysepayShopDate(r.TradeNo), "subject": r.Subject,
			"total_amount": yuan(r.Amount), "currency": "CNY", "seller_id": p.sellerID,
			"timeout_express": "2h", "business_code": p.businessNo, "sub_openid": r.OpenID,
			"is_minipg": "2", "payer_ip": r.ClientIP,
		}
		result, e := p.execute(ctx, "ysepay.online.weixin.pay", "3.5", biz, r.NotifyURL, "")
		if e != nil {
			return CreateResult{}, e
		}
		return ysepayJSAPIResult(result, "jsapi_pay_info")
	case "alipay":
		biz := map[string]string{
			"out_trade_no": r.TradeNo, "shopdate": ysepayShopDate(r.TradeNo), "subject": r.Subject,
			"total_amount": yuan(r.Amount), "currency": "CNY", "seller_id": p.sellerID,
			"timeout_express": "2h", "business_code": p.businessNo, "buyer_id": r.OpenID, "payer_ip": r.ClientIP,
		}
		result, e := p.execute(ctx, "ysepay.online.alijsapi.pay", "3.5", biz, r.NotifyURL, "")
		if e != nil {
			return CreateResult{}, e
		}
		return ysepayJSAPIResult(result, "jsapi_pay_info")
	case "bank":
		biz := map[string]string{
			"out_trade_no": r.TradeNo, "shopdate": ysepayShopDate(r.TradeNo), "subject": r.Subject,
			"total_amount": yuan(r.Amount), "currency": "CNY", "seller_id": p.sellerID,
			"timeout_express": "2h", "business_code": p.businessNo, "spbill_create_ip": r.ClientIP,
			"bank_type": "9001002", "userId": r.OpenID,
		}
		result, e := p.execute(ctx, "ysepay.online.cupmulapp.qrcodepay", "3.5", biz, r.NotifyURL, "")
		if e != nil {
			return CreateResult{}, e
		}
		webURL := jsonText(result, "web_url")
		if !validPaymentURL(webURL) {
			return CreateResult{}, rejected("YSE_NO_WEBURL")
		}
		return CreateResult{MaterialType: "jump", Material: webURL, ProviderTradeNo: jsonText(result, "trade_no")}, nil
	}
	return CreateResult{}, ErrUnsupported
}

func ysepayJSAPIResult(result map[string]any, field string) (CreateResult, error) {
	info := jsonText(result, field)
	if info == "" {
		return CreateResult{}, rejected("YSE_NO_JSAPI")
	}
	return CreateResult{MaterialType: "jsapi", Material: info, ProviderTradeNo: jsonText(result, "trade_no")}, nil
}

// createWap renders the auto-submit form for the directpay (WAP) product.
func (p *ysepayPlugin) createWap(r CreateRequest, bank string) (CreateResult, error) {
	params := map[string]string{
		"method":          "ysepay.online.wap.directpay.createbyuser",
		"partner_id":      p.partnerID,
		"timestamp":       ysepayTimestamp(),
		"charset":         "UTF-8",
		"sign_type":       "RSA",
		"version":         "3.0",
		"notify_url":      r.NotifyURL,
		"out_trade_no":    r.TradeNo,
		"shopdate":        ysepayShopDate(r.TradeNo),
		"subject":         r.Subject,
		"total_amount":    yuan(r.Amount),
		"seller_id":       p.sellerID,
		"timeout_express": "7d",
		"business_code":   p.businessNo,
		"pay_mode":        "native",
		"bank_type":       bank,
	}
	if r.ReturnURL != "" {
		params["return_url"] = r.ReturnURL
	}
	sig, e := p.sign(params)
	if e != nil {
		return CreateResult{}, e
	}
	params["sign"] = sig
	return CreateResult{MaterialType: "html", Material: pluginForm(ysepayWapGateway, params, "正在跳转")}, nil
}

// execute signs and sends a gateway request with a JSON biz_content, parses the
// <method>_response envelope, verifies the response signature, and returns the
// business node. code 10000 is success.
func (p *ysepayPlugin) execute(ctx context.Context, method, version string, biz map[string]string, notifyURL, returnURL string) (map[string]any, error) {
	params := map[string]string{
		"method":      method,
		"partner_id":  p.partnerID,
		"timestamp":   ysepayTimestamp(),
		"charset":     "UTF-8",
		"sign_type":   "RSA",
		"version":     version,
		"biz_content": ysepayMarshal(biz),
	}
	if notifyURL != "" {
		params["notify_url"] = notifyURL
	}
	if returnURL != "" {
		params["return_url"] = returnURL
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
	raw, e := p.postForm(ctx, method, p.qrGateway, form)
	if e != nil {
		return nil, e
	}
	node, signData, sign, e := ysepayParseResponse(raw, method)
	if e != nil {
		return nil, &UnknownOutcome{Operation: method, Cause: e}
	}
	// Verify the response signature with the platform public key.
	if !ysepayVerifyResponse(p.pub, signData, sign) {
		return nil, &UnknownOutcome{Operation: method, Cause: errYsepayReply}
	}
	code := jsonText(node, "code")
	if code == "10000" {
		return node, nil
	}
	if sub := jsonText(node, "sub_code"); sub != "" {
		return nil, rejected("YSE_" + sub)
	}
	return nil, &UnknownOutcome{Operation: method, Cause: errYsepayReply}
}

// ysepayParseResponse mirrors YsepayResponse: find the "<method>_response"
// node, extract the raw signed substring (node body) and the top-level sign.
func ysepayParseResponse(raw []byte, method string) (node map[string]any, signData, sign string, err error) {
	top, e := callbackJSON(raw)
	if e != nil {
		return nil, "", "", errYsepayReply
	}
	nodeName := strings.ReplaceAll(method, ".", "_") + "_response"
	obj, ok := top[nodeName].(map[string]any)
	if !ok {
		return nil, "", "", errYsepayReply
	}
	sign = jsonText(top, "sign")
	if sign == "" {
		return nil, "", "", errYsepayReply
	}
	// Signed data is the raw text between `"<nodeName>":` and `,"sign"`.
	s := string(raw)
	marker := `"` + nodeName + `"`
	ni := strings.Index(s, marker)
	if ni < 0 {
		return nil, "", "", errYsepayReply
	}
	start := ni + len(marker) + 1 // skip the ':'
	si := strings.LastIndex(s, `"sign"`)
	end := si - 1 // drop the ',' before "sign"
	if start >= end || end < 0 {
		// Fallback: node body re-serialized compactly.
		enc, _ := json.Marshal(obj)
		return obj, string(enc), sign, nil
	}
	signData = s[start:end]
	return obj, signData, sign, nil
}

func ysepayVerifyResponse(pub *rsa.PublicKey, signData, sign string) bool {
	if rsaVerify(pub, crypto.SHA1, signData, sign) {
		return true
	}
	// The SDK retries with escaped slashes unescaped.
	if strings.Contains(signData, `\/`) {
		return rsaVerify(pub, crypto.SHA1, strings.ReplaceAll(signData, `\/`, "/"), sign)
	}
	return false
}

func (p *ysepayPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	params, e := callbackParams(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	if params["sign"] == "" || !rsaVerify(p.pub, crypto.SHA1, ysepaySignContent(params), params["sign"]) {
		return PaymentEvent{}, ErrAuthentication
	}
	if params["trade_status"] != "TRADE_SUCCESS" {
		return PaymentEvent{}, ErrInvalid
	}
	if params["out_trade_no"] != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	amount, e := parseYuan(params["total_amount"])
	if e != nil || expected.Amount <= 0 || amount != expected.Amount {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, params["trade_no"], params["buyer_user_id"], amount), nil
}

// NotificationAck answers plain "success"/"fail".
func (p *ysepayPlugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return http.StatusOK, "text/plain; charset=utf-8", "fail"
	}
	return http.StatusOK, "text/plain; charset=utf-8", "success"
}

// Refund posts ysepay.online.trade.refund; code 10000 is success.
func (p *ysepayPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.ProviderTradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	biz := map[string]string{
		"out_trade_no":   r.TradeNo,
		"shopdate":       ysepayShopDate(r.TradeNo),
		"trade_no":       r.ProviderTradeNo,
		"refund_amount":  yuan(r.Amount),
		"refund_reason":  "申请退款",
		"out_request_no": r.RefundNo,
	}
	result, e := p.execute(ctx, "ysepay.online.trade.refund", "3.5", biz, "", "")
	if e != nil {
		return RefundResult{}, e
	}
	amount := r.Amount
	if f, fe := parseYuan(jsonText(result, "refund_amount")); fe == nil {
		amount = f
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(result, "trade_no"), State: "SUCCESS", Amount: amount}, nil
}

type ysepayReplyError string

func (e ysepayReplyError) Error() string { return string(e) }

var errYsepayReply = ysepayReplyError("ysepay: unreadable upstream reply")

var _ Provider = (*ysepayPlugin)(nil)
var _ NotificationVerifier = (*ysepayPlugin)(nil)
var _ CreateValidator = (*ysepayPlugin)(nil)
