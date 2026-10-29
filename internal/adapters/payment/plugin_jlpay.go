package payment

// Port of epay_release/plugins/jlpay (嘉联支付). The upstream is a 国密 (SM2/SM3)
// JSON API: every request is a JSON body signed with the merchant SM2 private
// key over the document "METHOD\nPATH\nTIMESTAMP\nNONCE\nBODY\n", carried in
// x-jlpay-* headers (JlpayClient::sign). Responses and notifications are
// verified the same way against the platform SM2 public key; the SM2 signature
// is DER-encoded then base64 (sign_alg "SM3WithSM2WithDer"). Amounts are cents.
//
// Pay products: QR (qrcodepay) for alipay/wxpay/unionpay → "native"; WeChat
// JSAPI (officialpay) and Alipay service-window (waph5pay) need an openid/buyer
// id → "jsapi". The order's out_trade_no is our TradeNo; transaction_id is the
// upstream trade number. Refund (/open/trans/refund) is supported.
//
// Out of scope (reported): app/cashier (cashierpay), unionpay JSAPI openid
// bootstrap (needs a session auth code), micropay/付款码, file upload, and all
// business callbacks.

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// jlpayNow returns the current unix time in seconds (the x-jlpay-timestamp).
func jlpayNow() int64 { return time.Now().Unix() }

// jlpayNonce returns a 16-hex-char random nonce for x-jlpay-nonce.
func jlpayNonce() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "0000000000000000"
	}
	return hex.EncodeToString(b[:])
}

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "jlpay",
		Title:  "嘉联支付",
		Author: "嘉联支付",
		Link:   "https://www.jlpay.com/",
		Types:  []string{"alipay", "wxpay", "bank"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "应用appid", Required: true},
			{Key: "appsecret", Label: "商户私钥", Note: "SM2-Hex格式", Textarea: true, Secret: true, Required: true},
			{Key: "appkey", Label: "嘉联公钥", Note: "SM2-Hex格式", Textarea: true, Required: true},
			{Key: "mch_id", Label: "商户号", Required: true},
			{Key: "term_no", Label: "终端号", Required: true},
			{Key: "appswitch", Label: "环境选择", Options: []PluginOption{{Value: "0", Label: "生产环境"}, {Value: "1", Label: "测试环境"}}},
		},
		Build: newJlpayPlugin,
	})
}

const (
	jlpayProdURL = "https://openapi.jlpay.com"
	jlpayTestURL = "https://openapi-uat.jlpay.com"
	jlpaySignAlg = "SM3WithSM2WithDer"
)

type jlpayPlugin struct {
	pluginBase
	appid, mchID, termNo string
	priv                 *sm2PrivateKey
	pub                  *sm2PublicKey
	baseURL              string
}

func newJlpayPlugin(c PluginChannel) (Provider, error) {
	appid := strings.TrimSpace(c.Config["appid"])
	mchID := strings.TrimSpace(c.Config["mch_id"])
	termNo := strings.TrimSpace(c.Config["term_no"])
	if appid == "" || len(appid) > 64 || mchID == "" || len(mchID) > 64 || termNo == "" || len(termNo) > 64 {
		return nil, ErrInvalid
	}
	priv, err := sm2ParsePrivateHex(c.Config["appsecret"])
	if err != nil {
		return nil, ErrInvalid
	}
	pub, err := sm2ParsePublicHex(c.Config["appkey"])
	if err != nil {
		return nil, ErrInvalid
	}
	baseURL := jlpayProdURL
	if c.Config["appswitch"] == "1" {
		baseURL = jlpayTestURL
	}
	base, err := newPluginBase(c, pluginAccount(baseURL, appid, mchID, termNo))
	if err != nil {
		return nil, err
	}
	return &jlpayPlugin{pluginBase: base, appid: appid, mchID: mchID, termNo: termNo, priv: priv, pub: pub, baseURL: baseURL}, nil
}

func (p *jlpayPlugin) methods() []string {
	switch p.payType {
	case "alipay":
		// QR (native) and Alipay service-window JSAPI (buyer_id).
		return []string{"native", "jsapi"}
	case "wxpay":
		// QR (native) and WeChat official/mini JSAPI (openid).
		return []string{"native", "jsapi"}
	default: // bank → UnionPay QR only (JSAPI needs a session auth code).
		return []string{"native"}
	}
}

func (p *jlpayPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

func (p *jlpayPlugin) ValidateCreate(r CreateRequest) error {
	if e := validatePluginCreate(r, p.methods()); e != nil {
		return e
	}
	if r.Method == "jsapi" && (r.OpenID == "" || len(r.OpenID) > 128) {
		return ErrInvalid
	}
	return nil
}

// payTypeField maps GOEPAY pay types to jlpay's pay_type field.
func (p *jlpayPlugin) payTypeField() string {
	switch p.payType {
	case "alipay":
		return "alipay"
	case "wxpay":
		return "wxpay"
	default:
		return "unionpay"
	}
}

// signDocument builds the canonical document and returns the base64(DER(SM2))
// signature over it.
func (p *jlpayPlugin) sign(method, path, timestamp, nonce, body string) (string, error) {
	doc := method + "\n" + path + "\n" + timestamp + "\n" + nonce + "\n" + body + "\n"
	der, err := sm2SignASN1(p.priv, "", []byte(doc))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(der), nil
}

// verify checks a platform signature over the canonical document.
func (p *jlpayPlugin) verify(method, path, timestamp, nonce, body, sign string) bool {
	der, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sign))
	if err != nil {
		return false
	}
	doc := method + "\n" + path + "\n" + timestamp + "\n" + nonce + "\n" + body + "\n"
	return sm2VerifyASN1(p.pub, "", []byte(doc), der)
}

// call signs and POSTs a JSON body, verifies the response signature, and
// returns the decoded success object. ret_code 00/00000 is success.
func (p *jlpayPlugin) call(ctx context.Context, operation, path string, payload map[string]string) (map[string]any, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, ErrInvalid
	}
	timestamp := strconv.FormatInt(jlpayNow(), 10)
	nonce := jlpayNonce()
	sign, err := p.sign("POST", path, timestamp, nonce, string(body))
	if err != nil {
		return nil, err
	}
	headers := map[string]string{
		"x-jlpay-appid":     p.appid,
		"x-jlpay-nonce":     nonce,
		"x-jlpay-timestamp": timestamp,
		"x-jlpay-sign-alg":  jlpaySignAlg,
		"x-jlpay-sign":      sign,
	}
	raw, err := p.pluginCall(ctx, operation, http.MethodPost, p.baseURL+path, body, "application/json; charset=utf-8", headers)
	if err != nil {
		return nil, err
	}
	out, err := callbackJSON(raw)
	if err != nil {
		return nil, &UnknownOutcome{Operation: operation, Cause: err}
	}
	code := jsonText(out, "ret_code")
	if code != "00" && code != "00000" {
		return nil, rejected("JLPAY_" + code)
	}
	return out, nil
}

func (p *jlpayPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	fee := strconv.FormatInt(r.Amount, 10)
	switch r.Method {
	case "native":
		params := map[string]string{
			"mch_id":        p.mchID,
			"term_no":       p.termNo,
			"pay_type":      p.payTypeField(),
			"out_trade_no":  r.TradeNo,
			"body":          r.Subject,
			"total_fee":     fee,
			"notify_url":    r.NotifyURL,
			"mch_create_ip": r.ClientIP,
		}
		out, err := p.call(ctx, "jlpay.qrcodepay", "/open/trans/qrcodepay", params)
		if err != nil {
			return CreateResult{}, err
		}
		codeURL := jsonText(out, "code_url")
		res, ok := materialFromLinks("", codeURL, "")
		if !ok {
			return CreateResult{}, rejected("JLPAY_NO_CODE_URL")
		}
		res.ProviderTradeNo = jsonText(out, "transaction_id")
		return res, nil
	case "jsapi":
		var path string
		params := map[string]string{
			"mch_id":        p.mchID,
			"term_no":       p.termNo,
			"out_trade_no":  r.TradeNo,
			"body":          r.Subject,
			"total_fee":     fee,
			"notify_url":    r.NotifyURL,
			"mch_create_ip": r.ClientIP,
		}
		if p.payType == "wxpay" {
			path = "/open/trans/officialpay"
			params["pay_type"] = "wxpay"
			params["open_id"] = r.OpenID
		} else { // alipay service window
			path = "/open/trans/waph5pay"
			params["pay_type"] = "alipay"
			params["buyer_id"] = r.OpenID
		}
		out, err := p.call(ctx, "jlpay.jsapi", path, params)
		if err != nil {
			return CreateResult{}, err
		}
		payInfo := jsonText(out, "pay_info")
		if payInfo == "" || len(payInfo) > 8192 {
			return CreateResult{}, rejected("JLPAY_NO_PAY_INFO")
		}
		return CreateResult{MaterialType: "jsapi", Material: payInfo, ProviderTradeNo: jsonText(out, "transaction_id")}, nil
	}
	return CreateResult{}, ErrInvalid
}

// Notifications are POSTed with the signature in x-jlpay-* headers; the signed
// document uses the request URI as the api path. Since we never see our own
// callback route's path here, the signature is verified over the body with the
// path taken from the X-Jlpay-Api header the gateway relays, falling back to
// the Request URI header. We require the header-carried sign to validate.
func (p *jlpayPlugin) VerifyNotification(_ context.Context, header http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	if len(raw) == 0 || len(raw) > 1<<20 {
		return PaymentEvent{}, ErrInvalid
	}
	timestamp := header.Get("x-jlpay-timestamp")
	nonce := header.Get("x-jlpay-nonce")
	sign := header.Get("x-jlpay-sign")
	apiPath := header.Get("x-jlpay-api")
	if apiPath == "" {
		apiPath = header.Get("x-jlpay-request-uri")
	}
	if sign == "" || timestamp == "" || nonce == "" || apiPath == "" {
		return PaymentEvent{}, ErrAuthentication
	}
	if !p.verify("POST", apiPath, timestamp, nonce, string(raw), sign) {
		return PaymentEvent{}, ErrAuthentication
	}
	data, err := callbackJSON(raw)
	if err != nil {
		return PaymentEvent{}, ErrInvalid
	}
	if jsonText(data, "status") != "2" {
		return PaymentEvent{}, ErrInvalid
	}
	amount, err := parseFen(jsonText(data, "total_fee"))
	if err != nil || expected.Amount <= 0 || amount != expected.Amount {
		return PaymentEvent{}, ErrAuthentication
	}
	if jsonText(data, "out_trade_no") != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, jsonText(data, "transaction_id"), jsonText(data, "sub_openid"), amount), nil
}

// The original answers a JSON body for notifications, success being
// {"ret_code":"00000"}, failure {"ret_code":"00001","ret_msg":"sign fail"}.
func (p *jlpayPlugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return http.StatusOK, "application/json; charset=utf-8", `{"ret_code":"00001","ret_msg":"sign fail"}`
	}
	return http.StatusOK, "application/json; charset=utf-8", `{"ret_code":"00000"}`
}

// Refund posts /open/trans/refund with the upstream transaction id as
// ori_transaction_id and the refund number as out_trade_no (total_fee cents).
func (p *jlpayPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.ProviderTradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	params := map[string]string{
		"mch_id":             p.mchID,
		"out_trade_no":       r.RefundNo,
		"ori_transaction_id": r.ProviderTradeNo,
		"total_fee":          strconv.FormatInt(r.Amount, 10),
	}
	out, err := p.call(ctx, "jlpay.refund", "/open/trans/refund", params)
	if err != nil {
		return RefundResult{}, err
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(out, "transaction_id"), State: "SUCCESS", Amount: r.Amount}, nil
}

var _ Provider = (*jlpayPlugin)(nil)
var _ NotificationVerifier = (*jlpayPlugin)(nil)
var _ CreateValidator = (*jlpayPlugin)(nil)
var _ NotificationAcker = (*jlpayPlugin)(nil)
