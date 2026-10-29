package payment

// Port of epay_release/plugins/baofu (宝付支付). The aggregated trade API
// (BaofuClient::execute) POSTs a form to https://juhe.baofoo.com/api: the
// bizContent JSON is signed with the merchant RSA private key (SHA256, hex),
// and the reply's dataContent is verified with baofu's platform certificate
// (the bundled baofu.cer, a fixed CFCA 1024-bit RSA cert). The merchant key
// ships as a PKCS#12 (.pfx) protected by a password; we take the PFX as a
// base64/PEM text input plus the password and decode it once in Build.
//
// Create maps:  QR (unified_order ALIPAY_NATIVE / pre_unified_order
// WECHAT_JSAPI aggregated code / QUICK_PASS_NATIVE) -> "native";
// JSAPI (ALIPAY_JSAPI buyer_id / WECHAT_JSAPI openid / QUICK_PASS_NATIVE_JS)
// -> "jsapi". Callbacks are POST (dataContent + signStr), ack is a plain
// "OK"/"FAIL" body. Refund uses order_refund.
//
// Out of scope (reported): transfers / transfer_query / transfer_proof /
// balance_query (fund-out via public.baofoo.com with RSA-encrypted envelopes),
// the applynotify onboarding callback, and profit-sharing (share_after_pay).

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/crypto/pkcs12"
)

const baofuGateway = "https://juhe.baofoo.com/api"

// baofuPlatformCert is baofu's platform public certificate (plugins/baofu/
// cert/baofu.cer), a fixed CFCA-issued cert used to verify every reply and
// callback signature. It is not merchant-specific, so it is embedded.
const baofuPlatformCert = `-----BEGIN CERTIFICATE-----
MIIDgDCCAmigAwIBAgIFEFmDdYIwDQYJKoZIhvcNAQEFBQAwITELMAkGA1UEBhMC
Q04xEjAQBgNVBAoTCUNGQ0EgT0NBMTAeFw0xNjEwMjQwNzIzMjRaFw0yMTEwMjQw
NzIzMjRaMHExCzAJBgNVBAYTAkNOMRIwEAYDVQQKEwlDRkNBIE9DQTExDzANBgNV
BAsTBkJhb2ZvbzEZMBcGA1UECxMQT3JnYW5pemF0aW9uYWwtMTEiMCAGA1UEAwwZ
QmFvZm9vQOWuneS7mEBaNjg4MTk5OTlAMTCBnzANBgkqhkiG9w0BAQEFAAOBjQAw
gYkCgYEA30bgCAY6agzuJccSKfLe/2NeOeUm2GZFuajai6CB9E0SOg6QymWuGw/f
XyL6rcZd/eavew1Ec2EDTeq4DgVudah6HZbI3EXt2TSxLpn6AwDMwKgSGhWpxR2V
1HnEiIyTOCjdWe5gUJl62UnE5acErjO0G9w+o+qGy13r0/ZZ6h0CAwEAAaOB8jCB
7zAfBgNVHSMEGDAWgBTR2+mIguXdGo9MqgCMvnzyqxv22TBIBgNVHSAEQTA/MD0G
CGCBHIbvKgEBMDEwLwYIKwYBBQUHAgEWI2h0dHA6Ly93d3cuY2ZjYS5jb20uY24v
dXMvdXMtMTQuaHRtMDcGA1UdHwQwMC4wLKAqoCiGJmh0dHA6Ly9jcmwuY2ZjYS5j
b20uY24vUlNBL2NybDQ0MDEuY3JsMAsGA1UdDwQEAwID6DAdBgNVHQ4EFgQURqt0
qRdCkRebpuVAmkiDlMQj95IwHQYDVR0lBBYwFAYIKwYBBQUHAwIGCCsGAQUFBwME
MA0GCSqGSIb3DQEBBQUAA4IBAQAHTgkQg47p4Fy5PCFcdGzX5LnHqyHK/cysjcpo
lHSj89/s/kX1pXU7Jqgp/FD1RJu/Ng6rmgHSNDR9ZHwapdc++PRSxDBN/+2BnR6J
4CoxHb0e7sjDddb7R1ceTuRXNjZuruV80j5HFycgrHSsSLI/LeZq1up0NLM+YOxt
/+zsnkAiGfLvRCVTlppwgfhpsT1LhdVvrDkYMedbgrA1zqL/kbXe0DIdb8kn2G8Q
7zR8SEYPXnWsDR2Ktno53mxKhx9kg3DQayWMudNY8qy+So0uBT1CqCsWR7SWoMpX
VSnKLXWBCRDqevVt4B2fpTHoo4uM4oZDggYaf14ky90d0lWH
-----END CERTIFICATE-----`

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "baofu",
		Title:  "宝付支付",
		Author: "宝付",
		Link:   "https://www.baofu.com/",
		Types:  []string{"alipay", "wxpay", "bank"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "商户号", Required: true},
			{Key: "appurl", Label: "终端号", Required: true},
			{Key: "appkey", Label: "私钥密码", Secret: true, Required: true},
			{Key: "appmchid", Label: "聚合交易商户号", Note: "在微信/支付宝报备的商户号"},
			{Key: "pfx", Label: "商户私钥证书(PFX)", Note: "商户私钥证书client.pfx的base64内容", PEM: true, Required: true},
		},
		AppTypes: []PluginOption{{Value: "1", Label: "扫码支付"}, {Value: "2", Label: "JS支付"}},
		Build:    newBaofuPlugin,
	})
}

type baofuPlugin struct {
	pluginBase
	merID, terID, subMchID string
	appTypes               []string
	priv                   *rsa.PrivateKey
	platform               *rsa.PublicKey
	gateway                string
}

// baofuParsePFX decodes a PKCS#12 blob (accepting a base64 text, with or
// without the stray PEM armor some consoles add) into its RSA private key.
func baofuParsePFX(text, password string) (*rsa.PrivateKey, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, ErrInvalid
	}
	var der []byte
	if block, _ := pem.Decode([]byte(text)); block != nil {
		der = block.Bytes
	} else {
		raw, e := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(text), ""))
		if e != nil {
			return nil, ErrInvalid
		}
		der = raw
	}
	key, _, e := pkcs12.Decode(der, password)
	if e != nil {
		return nil, ErrInvalid
	}
	priv, ok := key.(*rsa.PrivateKey)
	if !ok || priv == nil || priv.N.BitLen() < pluginMinRSABits {
		return nil, ErrInvalid
	}
	if e := priv.Validate(); e != nil {
		return nil, ErrInvalid
	}
	return priv, nil
}

func baofuParseCert(text string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(text)))
	if block == nil {
		return nil, ErrInvalid
	}
	cert, e := x509.ParseCertificate(block.Bytes)
	if e != nil {
		return nil, ErrInvalid
	}
	pub, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok || pub == nil || pub.N.BitLen() < pluginMinRSABits {
		return nil, ErrInvalid
	}
	return pub, nil
}

func newBaofuPlugin(c PluginChannel) (Provider, error) {
	merID := strings.TrimSpace(c.Config["appid"])
	terID := strings.TrimSpace(c.Config["appurl"])
	pwd := c.Config["appkey"]
	subMch := strings.TrimSpace(c.Config["appmchid"])
	if merID == "" || len(merID) > 32 || terID == "" || len(terID) > 32 || pwd == "" {
		return nil, ErrInvalid
	}
	if strings.ContainsAny(merID, "&=?# ") || strings.ContainsAny(terID, "&=?# ") {
		return nil, ErrInvalid
	}
	priv, e := baofuParsePFX(c.Certificates["pfx"], pwd)
	if e != nil {
		return nil, e
	}
	platform, e := baofuParseCert(baofuPlatformCert)
	if e != nil {
		return nil, e
	}
	base, e := newPluginBase(c, pluginAccount(baofuGateway, merID, terID))
	if e != nil {
		return nil, e
	}
	return &baofuPlugin{pluginBase: base, merID: merID, terID: terID, subMchID: subMch, appTypes: c.AppTypes, priv: priv, platform: platform, gateway: baofuGateway}, nil
}

// pickSubMchID mirrors the original's "subMchId with | picks one at random"
// but deterministically takes the first entry (offline-safe, no rand).
func (p *baofuPlugin) pickSubMchID() string {
	if i := strings.IndexByte(p.subMchID, '|'); i >= 0 {
		return p.subMchID[:i]
	}
	return p.subMchID
}

// hasApp reports whether an app-type product is enabled (empty AppTypes =
// original default: both products offered).
func (p *baofuPlugin) hasApp(t string) bool {
	if len(p.appTypes) == 0 {
		return true
	}
	for _, a := range p.appTypes {
		if a == t {
			return true
		}
	}
	return false
}

func (p *baofuPlugin) methods() []string {
	var m []string
	if p.hasApp("1") { // QR / aggregated code
		m = append(m, "native")
	}
	if p.hasApp("2") { // JS payment
		m = append(m, "jsapi")
	}
	if len(m) == 0 {
		m = []string{"native"}
	}
	return m
}

func (p *baofuPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

func (p *baofuPlugin) ValidateCreate(r CreateRequest) error {
	if e := validatePluginCreate(r, p.methods()); e != nil {
		return e
	}
	if r.Method == "jsapi" && strings.TrimSpace(r.OpenID) == "" {
		return ErrInvalid
	}
	return nil
}

// sign signs the bizContent JSON with the merchant private key (SHA256, hex).
func (p *baofuPlugin) sign(bizContent string) (string, error) {
	h := crypto.SHA256.New()
	h.Write([]byte(bizContent))
	sig, e := rsa.SignPKCS1v15(rand.Reader, p.priv, crypto.SHA256, h.Sum(nil))
	if e != nil {
		return "", e
	}
	return hex.EncodeToString(sig), nil
}

// verify checks a platform SHA256 signature (hex) over data.
func (p *baofuPlugin) verify(data, signHex string) bool {
	sig, e := hex.DecodeString(strings.TrimSpace(signHex))
	if e != nil || len(sig) == 0 {
		return false
	}
	h := crypto.SHA256.New()
	h.Write([]byte(data))
	return rsa.VerifyPKCS1v15(p.platform, crypto.SHA256, h.Sum(nil), sig) == nil
}

// payCode selects baofu's payCode for this channel's pay type and method.
func (p *baofuPlugin) payCode(method string) (string, bool) {
	switch p.payType {
	case "alipay":
		if method == "jsapi" {
			return "ALIPAY_JSAPI", true
		}
		return "ALIPAY_NATIVE", true
	case "wxpay":
		// aggregated QR and JSAPI both use WECHAT_JSAPI (pre_unified / unified).
		return "WECHAT_JSAPI", true
	case "bank":
		if method == "jsapi" {
			return "QUICK_PASS_NATIVE_JS", true
		}
		return "QUICK_PASS_NATIVE", true
	}
	return "", false
}

func (p *baofuPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	payCode, ok := p.payCode(r.Method)
	if !ok {
		return CreateResult{}, ErrInvalid
	}
	amt := r.Amount // already cents (fen)
	payExtend := map[string]any{}
	// wxpay aggregated QR scan uses the pre_unified_order endpoint.
	endpoint := "unified_order"
	switch payCode {
	case "ALIPAY_NATIVE":
		payExtend["subject"] = r.Subject
	case "ALIPAY_JSAPI":
		payExtend["subject"] = r.Subject
		payExtend["buyer_id"] = r.OpenID
	case "WECHAT_JSAPI":
		if r.Method == "native" {
			// aggregated code (pre_unified_order): returns a scannable token.
			endpoint = "pre_unified_order"
			payExtend = map[string]any{
				"goods_name":           r.Subject,
				"merchant_name":        "聚合商户",
				"area_info":            "110101",
				"wechat_sub_member_id": p.pickSubMchID(),
			}
		} else { // jsapi
			payExtend = map[string]any{"body": r.Subject, "area_info": "110101", "sub_openid": r.OpenID}
		}
	case "QUICK_PASS_NATIVE":
		payExtend["areaInfo"] = "1560001"
	case "QUICK_PASS_NATIVE_JS":
		payExtend = map[string]any{"userId": r.OpenID, "areaInfo": "1560001", "customerIp": r.ClientIP, "orderDesc": r.Subject}
	}

	biz := map[string]any{
		"merId":      p.merID,
		"terId":      p.terID,
		"outTradeNo": r.TradeNo,
		"txnAmt":     amt,
		"txnTime":    time.Now().Format("20060102150405"),
		"totalAmt":   amt,
		"prodType":   "ORDINARY",
		"payCode":    payCode,
		"payExtend":  payExtend,
		"subMchId":   p.pickSubMchID(),
		"notifyUrl":  r.NotifyURL,
		"pageUrl":    r.ReturnURL,
		"riskInfo":   map[string]any{"clientIp": r.ClientIP},
	}
	if endpoint == "pre_unified_order" {
		biz["cashierFlag"] = "0"
	}

	data, e := p.execute(ctx, "baofu.create", endpoint, biz)
	if e != nil {
		return CreateResult{}, e
	}
	return p.mapCreate(r, payCode, endpoint, data)
}

func (p *baofuPlugin) mapCreate(r CreateRequest, payCode, endpoint string, data map[string]any) (CreateResult, error) {
	switch {
	case endpoint == "pre_unified_order": // aggregated QR token
		token := jsonMapText(data, "token")
		if token == "" || len(token) > 4096 || strings.ContainsAny(token, "\x00\r\n") {
			return CreateResult{}, rejected("BAOFU_NO_TOKEN")
		}
		return CreateResult{MaterialType: "qrcode", Material: token}, nil
	case payCode == "ALIPAY_NATIVE":
		if res, ok := materialFromLinks("", jsonMapText(data, "qr_code"), ""); ok {
			res.ProviderTradeNo = jsonMapText(data, "trade_no")
			return res, nil
		}
		return CreateResult{}, rejected("BAOFU_NO_QRCODE")
	case payCode == "QUICK_PASS_NATIVE": // addOrder returns chlRetParam (string QR url)
		u := jsonMapText(data, "qr_code")
		if u == "" {
			u = jsonMapText(data, "qrCode")
		}
		if res, ok := materialFromLinks("", u, ""); ok {
			return res, nil
		}
		return CreateResult{}, rejected("BAOFU_NO_QRCODE")
	case payCode == "ALIPAY_JSAPI":
		tn := jsonMapText(data, "trade_no")
		if tn == "" {
			return CreateResult{}, rejected("BAOFU_NO_TRADE_NO")
		}
		return CreateResult{MaterialType: "jsapi", Material: tn, ProviderTradeNo: tn}, nil
	case payCode == "WECHAT_JSAPI": // jsapi
		pay := jsonMapText(data, "wc_pay_data")
		if pay == "" {
			return CreateResult{}, rejected("BAOFU_NO_PAY_DATA")
		}
		return CreateResult{MaterialType: "jsapi", Material: pay}, nil
	case payCode == "QUICK_PASS_NATIVE_JS":
		if res, ok := materialFromLinks(jsonMapText(data, "redirectUrl"), "", ""); ok {
			return res, nil
		}
		return CreateResult{}, rejected("BAOFU_NO_REDIRECT")
	}
	return CreateResult{}, rejected("BAOFU_UNSUPPORTED")
}

// execute sends an aggregated-trade request and returns the verified
// dataContent map.
func (p *baofuPlugin) execute(ctx context.Context, operation, method string, biz map[string]any) (map[string]any, error) {
	bizJSON, e := json.Marshal(biz)
	if e != nil {
		return nil, ErrInvalid
	}
	signStr, e := p.sign(string(bizJSON))
	if e != nil {
		return nil, ErrAuthentication
	}
	form := url.Values{
		"merId":      {p.merID},
		"terId":      {p.terID},
		"method":     {method},
		"charset":    {"UTF-8"},
		"version":    {"1.0"},
		"format":     {"json"},
		"timestamp":  {time.Now().Format("20060102150405")},
		"signType":   {"RSA"},
		"signSn":     {"1"},
		"ncrptnSn":   {"1"},
		"bizContent": {string(bizJSON)},
		"signStr":    {signStr},
	}
	raw, e := p.postForm(ctx, operation, p.gateway, form)
	if e != nil {
		return nil, e
	}
	envelope, e := callbackJSON(raw)
	if e != nil {
		return nil, &UnknownOutcome{Operation: operation, Cause: e}
	}
	if jsonText(envelope, "returnCode") != "SUCCESS" {
		code := jsonText(envelope, "returnCode")
		if code == "" {
			return nil, &UnknownOutcome{Operation: operation, Cause: errors.New("missing returnCode")}
		}
		return nil, rejected("BAOFU_" + code)
	}
	dataContent := jsonText(envelope, "dataContent")
	respSign := jsonText(envelope, "signStr")
	if dataContent == "" || !p.verify(dataContent, respSign) {
		return nil, &UnknownOutcome{Operation: operation, Cause: errors.New("response signature verification failed")}
	}
	data, e := callbackJSON([]byte(dataContent))
	if e != nil {
		return nil, &UnknownOutcome{Operation: operation, Cause: e}
	}
	return data, nil
}

func (p *baofuPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	params, e := callbackParams(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	dataContent := params["dataContent"]
	if dataContent == "" || !p.verify(dataContent, params["signStr"]) {
		return PaymentEvent{}, ErrAuthentication
	}
	data, e := callbackJSON([]byte(dataContent))
	if e != nil {
		return PaymentEvent{}, ErrInvalid
	}
	if jsonText(data, "txnState") != "SUCCESS" {
		return PaymentEvent{}, ErrInvalid
	}
	if jsonText(data, "outTradeNo") != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	amount, e := parseFen(jsonText(data, "txnAmt"))
	if e != nil || expected.Amount <= 0 || amount != expected.Amount {
		return PaymentEvent{}, ErrAuthentication
	}
	payCode := jsonText(data, "payCode")
	buyer, providerNo := "", jsonText(data, "tradeNo")
	chl, _ := data["chlRetParam"].(map[string]any)
	switch {
	case strings.HasPrefix(payCode, "ALIPAY_"):
		buyer = jsonMapText(chl, "buyer_id")
		if v := jsonMapText(chl, "trade_no"); v != "" {
			providerNo = v
		}
	case strings.HasPrefix(payCode, "WECHAT_"):
		if v := jsonMapText(chl, "transaction_id"); v != "" {
			providerNo = v
		}
	case strings.HasPrefix(payCode, "QUICK_PASS_"):
		if v := jsonMapText(chl, "voucherNum"); v != "" {
			providerNo = v
		}
	}
	return paidEvent(expected.TradeNo, providerNo, buyer, amount), nil
}

// NotificationAck answers exactly as the original notify(): plain "OK" on
// success, "FAIL" on a verification failure.
func (p *baofuPlugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return http.StatusOK, "text/plain; charset=utf-8", "FAIL"
	}
	return http.StatusOK, "text/plain; charset=utf-8", "OK"
}

func (p *baofuPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.ProviderTradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	biz := map[string]any{
		"merId":         p.merID,
		"terId":         p.terID,
		"originTradeNo": r.ProviderTradeNo,
		"outTradeNo":    r.RefundNo,
		"refundAmt":     r.Amount,
		"totalAmt":      r.Amount,
		"txnTime":       time.Now().Format("20060102150405"),
		"refundReason":  "申请退款",
	}
	data, e := p.execute(ctx, "baofu.refund", "order_refund", biz)
	if e != nil {
		return RefundResult{}, e
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(data, "tradeNo"), State: "SUCCESS", Amount: r.Amount}, nil
}

// jsonMapText reads a text field from a decoded JSON object, "" when absent.
func jsonMapText(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	return jsonText(m, key)
}

var _ Provider = (*baofuPlugin)(nil)
var _ NotificationVerifier = (*baofuPlugin)(nil)
var _ CreateValidator = (*baofuPlugin)(nil)
var _ NotificationAcker = (*baofuPlugin)(nil)
