package payment

// Port of epay_release/plugins/kuaiqian (快钱支付, 99bill). The original drives
// several flows: a PC/H5 gateway that posts a signed form to 99bill's
// recvMerchantInfoAction and receives a GET callback (RSA-SHA256 over an
// ordered field string, signed with the merchant PFX, verified with the
// platform certificate), and a 当面付 (face-to-face) flow whose SDK
// (inc/PayApp.class.php + CryptoProcessor.php) wraps every request/response in
// PKCS#7/CMS envelopes (openssl_pkcs7_encrypt/sign/decrypt/verify).
//
// Go has no CMS/PKCS#7 primitive in the standard library and no module may be
// added, so plugin_kuaiqian_cms.go implements the minimal envelope that
// PayApp::execute uses. This port implements the gateway form flow (submit →
// recvMerchantInfoAction), its GET notify() callback, and the gateway order's
// query (F0003) and refund (F0001) over that envelope. The 当面付 flow (A7007,
// with its own A7006 query and A7003 refund) and transfers stay unported:
// GOEPAY does not carry the original's per-order combine flag to query/refund.
//
// Credentials: the original reads the merchant key (key.pfx, password =
// 商户证书密码/appkey) and the platform certificate (cert.cer) from the plugin
// cert/ folder. GOEPAY has no plugin filesystem at runtime, so they are
// configuration here: merchant_pfx (base64 PKCS#12 text) and platform_cert
// (PEM certificate). The RSA-SHA256 sign/verify logic matches PayApp exactly.

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/pkcs12"
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "kuaiqian",
		Title:  "快钱支付",
		Author: "快钱",
		Link:   "https://www.99bill.com/",
		Types:  []string{"alipay", "wxpay", "bank"},
		// transtypes 'bank' (转账) is out of scope for this wave.
		Inputs: []PluginInput{
			{Key: "appid", Label: "快钱账户号", Required: true},
			{Key: "appkey", Label: "商户证书密码", Secret: true, Required: true},
			{Key: "appsecret", Label: "SSL客户端证书密码", Secret: true},
			{Key: "merchant_id", Label: "当面付-商户号", Note: "仅当面付需要填写"},
			{Key: "terminal_id", Label: "当面付-终端号", Note: "仅当面付需要填写"},
			{Key: "appmchid", Label: "服务商-快钱子账户号", Note: "仅服务商需要填写"},
			{Key: "own_channel", Label: "是否自有渠道", Options: []PluginOption{{Value: "0", Label: "否"}, {Value: "1", Label: "是"}}},
			{Key: "merchant_pfx", Label: "商户证书(key.pfx, base64)", Note: "商户RSA证书 key.pfx 的 base64 文本，密码为商户证书密码", PEM: true, Required: true},
			{Key: "platform_cert", Label: "快钱平台公钥证书(cert.cer, PEM)", Note: "快钱平台公钥证书 cert.cer 的 PEM 文本", PEM: true, Required: true},
		},
		Note:  "网关表单支付，支持查单和退款。将商户证书key.pfx转base64填入商户证书，快钱平台公钥cert.cer填入平台公钥。当面付和转账未实现。",
		Build: newKuaiqianPlugin,
	})
}

const (
	kuaiqianBankGateway   = "https://www.99bill.com/gateway/recvMerchantInfoAction.htm"
	kuaiqianMobileGateway = "https://www.99bill.com/mobilegateway/recvMerchantInfoAction.htm"
	// kuaiqianUmgwGateway is PayApp::$gateway_url — the CMS (当面付/查单/退款)
	// endpoint.
	kuaiqianUmgwGateway = "https://umgw.99bill.com/umgw/common/distribute.html"
)

type kuaiqianPlugin struct {
	pluginBase
	appid        string
	privateKey   *rsa.PrivateKey
	publicKey    *rsa.PublicKey
	merchantCert *x509.Certificate
	platformCert *x509.Certificate
	ownChannel   bool
	gateway      string
}

func newKuaiqianPlugin(c PluginChannel) (Provider, error) {
	appid := strings.TrimSpace(c.Config["appid"])
	if appid == "" || len(appid) > 32 || strings.ContainsAny(appid, "&=?# ") {
		return nil, ErrInvalid
	}
	pfxText := strings.TrimSpace(c.Certificates["merchant_pfx"])
	key, cert, e := kuaiqianLoadPFX(pfxText, c.Config["appkey"])
	if e != nil {
		return nil, e
	}
	pub, platformCert, e := kuaiqianLoadCert(c.Certificates["platform_cert"])
	if e != nil {
		return nil, e
	}
	base, e := newPluginBase(c, pluginAccount("https://www.99bill.com", appid))
	if e != nil {
		return nil, e
	}
	return &kuaiqianPlugin{pluginBase: base, appid: appid, privateKey: key, publicKey: pub, merchantCert: cert, platformCert: platformCert, ownChannel: c.Config["own_channel"] == "1", gateway: kuaiqianUmgwGateway}, nil
}

// kuaiqianLoadPFX decodes the merchant PKCS#12 (base64 text) with the 商户证书密码
// and returns its RSA private key and certificate (the latter is needed as the
// SignedData signer; it is nil-safe for the gateway form flow).
func kuaiqianLoadPFX(text, password string) (*rsa.PrivateKey, *x509.Certificate, error) {
	text = strings.Join(strings.Fields(text), "")
	if text == "" {
		return nil, nil, ErrInvalid
	}
	der, e := base64.StdEncoding.DecodeString(text)
	if e != nil {
		return nil, nil, ErrInvalid
	}
	priv, cert, e := pkcs12.Decode(der, password)
	if e != nil {
		return nil, nil, ErrInvalid
	}
	key, ok := priv.(*rsa.PrivateKey)
	if !ok || key.N.BitLen() < pluginMinRSABits {
		return nil, nil, ErrInvalid
	}
	return key, cert, nil
}

// kuaiqianLoadCert parses the platform certificate (PEM/cert) and returns its
// RSA public key and the parsed certificate (used as the EnvelopedData
// recipient). A bare public key has no certificate, so CMS query/refund then
// return ErrUnsupported at call time.
func kuaiqianLoadCert(text string) (*rsa.PublicKey, *x509.Certificate, error) {
	block := kuaiqianPEMBlock(text)
	if block != nil {
		if cert, e := x509.ParseCertificate(block); e == nil {
			key, ok := cert.PublicKey.(*rsa.PublicKey)
			if ok && key.N.BitLen() >= pluginMinRSABits {
				return key, cert, nil
			}
		}
	}
	// A bare public key (no certificate): the form flow still works, but CMS
	// envelope recipient resolution does not.
	if pub, e := pluginPublicKey(text); e == nil {
		return pub, nil, nil
	}
	return nil, nil, ErrInvalid
}

// kuaiqianPEMBlock extracts the DER bytes of the first PEM block, or decodes a
// bare base64 certificate body.
func kuaiqianPEMBlock(text string) []byte {
	text = strings.TrimSpace(text)
	if i := strings.Index(text, "-----BEGIN"); i >= 0 {
		if j := strings.Index(text, "-----END"); j > i {
			if k := strings.Index(text[i:], "\n"); k >= 0 {
				body := text[i+k : j]
				der, e := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(body), ""))
				if e == nil {
					return der
				}
			}
		}
		return nil
	}
	der, e := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(text), ""))
	if e != nil {
		return nil
	}
	return der
}

func (p *kuaiqianPlugin) methods() []string {
	// The gateway form flow produces a PC page form ("h5", there is no QR
	// product) and a mobile form; both are HTML auto-submit forms. For every
	// pay type the implemented product is the page/mobile gateway form.
	return []string{"h5"}
}

func (p *kuaiqianPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Query: true, Refund: true}
}

func (p *kuaiqianPlugin) ValidateCreate(r CreateRequest) error {
	return validatePluginCreate(r, p.methods())
}

// generateSign mirrors PayApp::generateSign: iterate params in the given order,
// skip signMsg and empty values, join "k=v&", trim the trailing "&", RSA-SHA256
// sign with the merchant private key, base64.
func (p *kuaiqianPlugin) generateSign(order []string, params map[string]string) (string, error) {
	var sb strings.Builder
	for _, k := range order {
		v := params[k]
		if k == "signMsg" || v == "" {
			continue
		}
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(v)
		sb.WriteByte('&')
	}
	s := strings.TrimSuffix(sb.String(), "&")
	return rsaSign(p.privateKey, crypto.SHA256, s)
}

func (p *kuaiqianPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	// payType maps to 99bill's payType code. The original gateway form flow
	// (bankpay / mobilepay) sends numeric product codes; for the generic PC
	// gateway with 网银/快捷 the code is 21 (快捷) when own channel apptype is
	// not forced. alipay/wxpay via the gateway use the aggregated mobile codes
	// 27-3 / 26-2; bank uses 21. We use the mobile gateway for a signed HTML
	// form, which covers the normal order the original builds for a mobile
	// device.
	var payType string
	switch p.payType {
	case "alipay":
		payType = "27-3"
	case "wxpay":
		payType = "26-2"
	case "bank":
		payType = "21"
	default:
		return CreateResult{}, ErrInvalid
	}
	// Field order matches the original mobilepay() params array insertion
	// order so the signed string is byte-identical.
	order := []string{"inputCharset", "pageUrl", "bgUrl", "version", "language", "signType", "merchantAcctId", "orderId", "orderAmount", "orderTime", "productName", "payType", "extDataType", "extDataContent"}
	params := map[string]string{
		"inputCharset":   "1",
		"pageUrl":        r.ReturnURL,
		"bgUrl":          r.NotifyURL,
		"version":        "mobile1.0",
		"language":       "1",
		"signType":       "4",
		"merchantAcctId": p.appid + "01",
		"orderId":        r.TradeNo,
		"orderAmount":    kuaiqianFen(r.Amount),
		"orderTime":      kuaiqianOrderTime(r),
		"productName":    r.Subject,
		"payType":        payType,
	}
	if p.ownChannel {
		params["extDataType"] = "NB2"
		params["extDataContent"] = `<NB2>{"customAuthNetInfo":{"own_channel":"1"}}</NB2>`
	}
	sig, e := p.generateSign(order, params)
	if e != nil {
		return CreateResult{}, e
	}
	params["signMsg"] = sig
	params["terminalIp"] = r.ClientIP
	// The original posts the signed form to the mobile gateway in the payer's
	// browser (an HTML auto-submit form), so return "html" material.
	return CreateResult{MaterialType: "html", Material: pluginForm(kuaiqianMobileGateway, params, "正在跳转")}, nil
}

// kuaiqianFen renders cents as the original's strval(realmoney*100) — the
// amount in fen with no decimals.
func kuaiqianFen(cents int64) string {
	return strconv.FormatInt(cents, 10)
}

// kuaiqianExecute mirrors PayApp::execute: seal the body into
// {signedData, envelopedData}, POST {head, requestBody} to the umgw gateway,
// then on responseCode 0000 unseal responseBody into the plaintext reply object.
func (p *kuaiqianPlugin) kuaiqianExecute(ctx context.Context, operation string, head map[string]string, body map[string]string) (map[string]any, error) {
	if p.merchantCert == nil || p.platformCert == nil {
		return nil, ErrUnsupported
	}
	plain, e := json.Marshal(body)
	if e != nil {
		return nil, ErrInvalid
	}
	signedDER, envelopedDER, e := p.kuaiqianSeal(plain)
	if e != nil {
		return nil, e
	}
	request := map[string]any{
		"head": head,
		"requestBody": map[string]string{
			"signedData":    base64.StdEncoding.EncodeToString(signedDER),
			"envelopedData": base64.StdEncoding.EncodeToString(envelopedDER),
		},
	}
	raw, e := p.postJSON(ctx, operation, p.gateway, request, nil)
	if e != nil {
		return nil, e
	}
	resp, e := callbackJSON(raw)
	if e != nil {
		return nil, &UnknownOutcome{Operation: operation, Cause: e}
	}
	rhead, _ := resp["head"].(map[string]any)
	code := ""
	if rhead != nil {
		code = jsonText(rhead, "responseCode")
	}
	if code != "0000" {
		if code == "" {
			return nil, &UnknownOutcome{Operation: operation, Cause: errors.New("missing responseCode")}
		}
		return nil, rejected("KUAIQIAN_" + code)
	}
	rbody, _ := resp["responseBody"].(map[string]any)
	if rbody == nil {
		return nil, &UnknownOutcome{Operation: operation, Cause: errors.New("missing responseBody")}
	}
	signed, e1 := base64.StdEncoding.DecodeString(jsonText(rbody, "signedData"))
	enveloped, e2 := base64.StdEncoding.DecodeString(jsonText(rbody, "envelopedData"))
	if e1 != nil || e2 != nil {
		return nil, &UnknownOutcome{Operation: operation, Cause: errors.New("bad response envelope")}
	}
	plainResp, e := p.kuaiqianUnseal(signed, enveloped)
	if e != nil {
		return nil, e
	}
	out, e := callbackJSON(plainResp)
	if e != nil {
		return nil, &UnknownOutcome{Operation: operation, Cause: e}
	}
	return out, nil
}

// Query reads the order over the CMS gateway (F0003, gateway order query keyed
// by our orderId). 当面付 combine orders (A7006) are not reachable because
// GOEPAY does not carry the combine flag; this covers the normal gateway order.
func (p *kuaiqianPlugin) Query(ctx context.Context, r QueryRequest) (QueryResult, error) {
	if r.TradeNo == "" {
		return QueryResult{}, ErrInvalid
	}
	head := map[string]string{
		"version":           "1.0.0",
		"messageType":       "F0003",
		"memberCode":        p.appid,
		"externalRefNumber": "QUE" + r.TradeNo,
	}
	body := map[string]string{
		"merchantAcctId": p.appid + "01",
		"queryType":      "0",
		"queryMode":      "1",
		"orderId":        r.TradeNo,
	}
	out, e := p.kuaiqianExecute(ctx, "kuaiqian.query", head, body)
	if e != nil {
		return QueryResult{}, e
	}
	if oid := jsonText(out, "orderId"); oid != "" && oid != r.TradeNo {
		return QueryResult{}, ErrAuthentication
	}
	// payResult/orderStatus: '10' (gateway) or 'S'/'SUCCESS' (face) mean paid.
	state := "NOTPAY"
	switch jsonText(out, "payResult") {
	case "10":
		state = "SUCCESS"
	}
	switch jsonText(out, "orderStatus") {
	case "S", "SUCCESS":
		state = "SUCCESS"
	case "R", "REFUND":
		state = "REFUND"
	}
	amount, _ := parseFen(kuaiqianFirstNonEmpty(jsonText(out, "orderAmount"), jsonText(out, "payAmount"), jsonText(out, "amount")))
	return QueryResult{TradeNo: r.TradeNo, ProviderTradeNo: kuaiqianFirstNonEmpty(jsonText(out, "dealId"), jsonText(out, "idOrderCtrl")), State: state, Currency: "CNY", BuyerID: jsonText(out, "thirdPartyBuyerId"), Amount: amount}, nil
}

// Refund posts the CMS gateway refund (F0001: txnType bill_drawback_api_1,
// amount in fen, orgOrderId the original trade number). bizResponseCode 0000 is
// success, as the original checks.
func (p *kuaiqianPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	entryTime := r.TradeNo
	if len(entryTime) > 14 {
		entryTime = entryTime[:14]
	}
	head := map[string]string{
		"version":           "1.0.0",
		"messageType":       "F0001",
		"memberCode":        p.appid,
		"externalRefNumber": r.RefundNo,
	}
	body := map[string]string{
		"merchantAcctId": p.appid,
		"txnType":        "bill_drawback_api_1",
		"amount":         kuaiqianFen(r.Amount),
		"entryTime":      entryTime,
		"orgOrderId":     r.TradeNo,
	}
	out, e := p.kuaiqianExecute(ctx, "kuaiqian.refund", head, body)
	if e != nil {
		return RefundResult{}, e
	}
	if jsonText(out, "bizResponseCode") != "0000" {
		return RefundResult{}, rejected("KUAIQIAN_" + jsonText(out, "bizResponseCode"))
	}
	return RefundResult{RefundNo: r.RefundNo, State: "SUCCESS", Amount: r.Amount}, nil
}

// kuaiqianFirstNonEmpty returns the first non-empty string.
func kuaiqianFirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// kuaiqianNow is the clock used for orderTime; tests replace it so signature
// vectors stay deterministic.
var kuaiqianNow = time.Now

// kuaiqianShanghai is the Asia/Shanghai location the original date('YmdHis')
// renders in (PHP runs on China time). It falls back to a fixed +08:00 offset
// when the tz database is unavailable.
var kuaiqianShanghai = func() *time.Location {
	if loc, e := time.LoadLocation("Asia/Shanghai"); e == nil {
		return loc
	}
	return time.FixedZone("CST", 8*3600)
}()

// kuaiqianOrderTime renders the orderTime (date('YmdHis')) from the live
// Asia/Shanghai clock, exactly as the original PHP does. GOEPAY trade numbers
// do not carry the creation time, so the clock is the only correct source.
func kuaiqianOrderTime(CreateRequest) string {
	return kuaiqianNow().In(kuaiqianShanghai).Format("20060102150405")
}

// NotificationMethods: the original notify() reads $_GET.
func (p *kuaiqianPlugin) NotificationMethods() []string {
	return []string{http.MethodGet, http.MethodPost}
}

// verifyOrder is PayApp::verifyNotify's fixed field order.
var kuaiqianVerifyOrder = []string{"merchantAcctId", "version", "language", "signType", "payType", "bankId", "orderId", "orderTime", "orderAmount", "bindCard", "bindMobile", "dealId", "bankDealId", "dealTime", "payAmount", "fee", "ext1", "ext2", "payResult", "aggregatePay", "errCode", "period"}

func (p *kuaiqianPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	params, e := callbackParams(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	if params["signMsg"] == "" {
		return PaymentEvent{}, ErrAuthentication
	}
	// Build the verify string exactly as PayApp::verifyNotify: fixed order,
	// skip empty values, join "k=v&", trim trailing "&".
	var sb strings.Builder
	for _, k := range kuaiqianVerifyOrder {
		if v := params[k]; v != "" {
			sb.WriteString(k)
			sb.WriteByte('=')
			sb.WriteString(v)
			sb.WriteByte('&')
		}
	}
	msg := strings.TrimSuffix(sb.String(), "&")
	if !rsaVerify(p.publicKey, crypto.SHA256, msg, params["signMsg"]) {
		return PaymentEvent{}, ErrAuthentication
	}
	// Merchant account binds the callback to this channel's appid.
	if acct := params["merchantAcctId"]; acct != "" && !strings.HasPrefix(acct, p.appid) {
		return PaymentEvent{}, ErrAuthentication
	}
	// payResult '10' means success (original check).
	if params["payResult"] != "10" {
		return PaymentEvent{}, ErrInvalid
	}
	if params["orderId"] != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	// orderAmount is in fen; payAmount is the paid amount in fen when present.
	amtField := params["orderAmount"]
	if params["payAmount"] != "" {
		amtField = params["payAmount"]
	}
	amount, e := parseFen(amtField)
	if e != nil || expected.Amount <= 0 || amount != expected.Amount {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, params["dealId"], "", amount), nil
}

// NotificationAck: the original echoes an XML <result>1</result> document for a
// verified success and <result>0</result> for failure.
func (p *kuaiqianPlugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return http.StatusOK, "text/html; charset=utf-8", "<result>0</result>"
	}
	return http.StatusOK, "text/html; charset=utf-8", "<result>1</result>"
}

var _ Provider = (*kuaiqianPlugin)(nil)
var _ NotificationVerifier = (*kuaiqianPlugin)(nil)
var _ CreateValidator = (*kuaiqianPlugin)(nil)
var _ NotificationAcker = (*kuaiqianPlugin)(nil)
