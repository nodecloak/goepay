package payment

// Port of epay_release/plugins/douyinpay (抖音支付, lib\douyinpay v3-style SDK).
// Requests go to https://api.douyinpay.com with an "Authorization: DouyinPay-RSA"
// header whose signature is RSA-SHA256 (merchant private key) over
//   METHOD\nPATH[?query]\nTIMESTAMP\nNONCE\nBODY\n
// base64-encoded. The reply/callback are verified RSA-SHA256 with the douyin
// PLATFORM public key over TIMESTAMP\nNONCE\nBODY\n, and the callback resource
// is AES-256-GCM encrypted under the 32-byte apikey.
//
// The original DOWNLOADS the platform certificate on first use (network + a
// file write), which cannot happen inside offline Build/VerifyNotification.
// We therefore take the platform public key as a PEM input (platform_cert):
//   - create (native/h5/app) only needs the merchant key, so it works always;
//   - VerifyNotification works only when platform_cert is configured, else it
//     returns ErrUnsupported — the original cannot verify without it either.
// JSAPI (needs an openid obtained via a separate OAuth leg) and refund notify
// are OUT OF SCOPE; Refund itself is implemented (it needs no platform key).

import (
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "douyinpay",
		Title:  "抖音支付",
		Author: "抖音支付",
		Link:   "https://pay.douyinpay.com/",
		Types:  []string{"douyinpay"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "应用ID", Note: "抖音开放平台网站应用Client Key", Required: true},
			{Key: "appmchid", Label: "商户号", Required: true},
			{Key: "apikey", Label: "接口加密密钥", Note: "32位APIv3密钥", Secret: true, Required: true},
			{Key: "certserial", Label: "商户API证书序列号", Required: true},
			{Key: "rsa_private_key", Label: "商户API私钥(apiclient_key.pem)", PEM: true, Textarea: true, Required: true},
			{Key: "platform_cert", Label: "抖音支付平台公钥证书", Note: "回调验签需要", PEM: true, Textarea: true},
		},
		AppTypes: []PluginOption{
			{Value: "1", Label: "Native支付"},
			{Value: "3", Label: "H5支付"},
			{Value: "4", Label: "APP支付"},
		},
		Build: newDouyinpayPlugin,
	})
}

const douyinpayGateway = "https://api.douyinpay.com"

type douyinpayPlugin struct {
	pluginBase
	gateway              string
	appID, mchID, serial string
	apiKey               []byte
	privateKey           *rsa.PrivateKey
	platformPub          *rsa.PublicKey
	appTypes             map[string]bool
}

func newDouyinpayPlugin(c PluginChannel) (Provider, error) {
	appID, mchID := strings.TrimSpace(c.Config["appid"]), strings.TrimSpace(c.Config["appmchid"])
	serial := strings.TrimSpace(c.Config["certserial"])
	apiKey := c.Config["apikey"]
	if appID == "" || mchID == "" || serial == "" || len(apiKey) != 32 {
		return nil, ErrInvalid
	}
	priv, e := pluginPrivateKey(c.Certificates["rsa_private_key"])
	if e != nil {
		return nil, ErrInvalid
	}
	p := &douyinpayPlugin{gateway: douyinpayGateway, appID: appID, mchID: mchID, serial: serial, apiKey: []byte(apiKey), privateKey: priv, appTypes: map[string]bool{}}
	if plat := strings.TrimSpace(c.Certificates["platform_cert"]); plat != "" {
		pub, e := pluginPublicKey(plat)
		if e != nil {
			return nil, ErrInvalid
		}
		p.platformPub = pub
	}
	for _, t := range c.AppTypes {
		p.appTypes[t] = true
	}
	base, e := newPluginBase(c, pluginAccount(douyinpayGateway, mchID, appID))
	if e != nil {
		return nil, e
	}
	p.pluginBase = base
	return p, nil
}

func (p *douyinpayPlugin) has(t string) bool {
	if len(p.appTypes) == 0 {
		return t == "1" // native default
	}
	return p.appTypes[t]
}

func (p *douyinpayPlugin) methods() []string {
	var m []string
	if p.has("1") || len(p.appTypes) == 0 {
		m = append(m, "native")
	}
	if p.has("3") {
		m = append(m, "h5")
	}
	if p.has("4") {
		m = append(m, "app")
	}
	if len(m) == 0 {
		m = []string{"native"}
	}
	return m
}

func (p *douyinpayPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

func (p *douyinpayPlugin) ValidateCreate(r CreateRequest) error {
	return validatePluginCreate(r, p.methods())
}

func (p *douyinpayPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	var path string
	switch r.Method {
	case "native":
		path = "/v1/trade/transactions/native"
	case "h5":
		path = "/v1/trade/transactions/h5"
	case "app":
		path = "/v1/trade/transactions/app"
	default:
		return CreateResult{}, ErrInvalid
	}
	payload := map[string]any{
		"appid":        p.appID,
		"mchid":        p.mchID,
		"description":  r.Subject,
		"out_trade_no": r.TradeNo,
		"notify_url":   r.NotifyURL,
		"amount":       map[string]any{"total": r.Amount, "currency": "CNY"},
		"scene_info":   map[string]any{"payer_client_ip": r.ClientIP},
	}
	if r.Method == "h5" {
		payload["scene_info"] = map[string]any{
			"payer_client_ip": r.ClientIP,
			"h5_info":         map[string]any{"type": "Wap"},
		}
	}
	if r.ProfitSharing {
		payload["settle_info"] = map[string]any{"profit_sharing": true}
	}
	out, e := p.execute(ctx, "douyinpay."+r.Method, http.MethodPost, path, payload)
	if e != nil {
		return CreateResult{}, e
	}
	switch r.Method {
	case "native":
		code := jsonText(out, "code_url")
		if code == "" || len(code) > 4096 || strings.ContainsAny(code, "\x00\r\n") {
			return CreateResult{}, rejected("DOUYINPAY_NO_CODE_URL")
		}
		return CreateResult{MaterialType: "qrcode", Material: code, ProviderTradeNo: jsonText(out, "prepay_id")}, nil
	case "h5":
		if res, ok := materialFromLinks(jsonText(out, "h5_url"), "", ""); ok {
			res.ProviderTradeNo = jsonText(out, "prepay_id")
			return res, nil
		}
		return CreateResult{}, rejected("DOUYINPAY_NO_H5_URL")
	case "app":
		prepay := jsonText(out, "prepay_id")
		if prepay == "" {
			return CreateResult{}, rejected("DOUYINPAY_NO_PREPAY_ID")
		}
		return CreateResult{MaterialType: "app", Material: p.appParams(prepay), ProviderTradeNo: prepay}, nil
	}
	return CreateResult{}, ErrInvalid
}

// appParams mirrors getAppParameters; the sign covers appid\n timestamp\n
// noncestr\n prepayid with the merchant key, RSA-SHA256 base64.
func (p *douyinpayPlugin) appParams(prepayID string) string {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := douyinpayNonce()
	params := map[string]string{
		"appid":     p.appID,
		"partnerid": p.mchID,
		"prepayid":  prepayID,
		"package":   "Sign=DYPay",
		"noncestr":  nonce,
		"timestamp": ts,
	}
	params["sign"] = p.signFields(p.appID, ts, nonce, prepayID)
	raw, _ := json.Marshal(params)
	return string(raw)
}

func (p *douyinpayPlugin) signFields(fields ...string) string {
	msg := strings.Join(append(fields, ""), "\n")
	sig, e := rsaSign(p.privateKey, crypto.SHA256, msg)
	if e != nil {
		return ""
	}
	return sig
}

// execute signs the request, posts it and verifies the reply over the platform
// key when configured. The douyin envelope is {err_no, err_tips, data}.
func (p *douyinpayPlugin) execute(ctx context.Context, op, method, path string, payload any) (map[string]any, error) {
	var body []byte
	if payload != nil {
		b, e := json.Marshal(payload)
		if e != nil {
			return nil, ErrInvalid
		}
		body = b
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := douyinpayNonce()
	target := p.gateway + path
	u, e := url.Parse(target)
	if e != nil {
		return nil, ErrInvalid
	}
	signURL := u.Path
	if u.RawQuery != "" {
		signURL += "?" + u.RawQuery
	}
	sign := p.signFields(method, signURL, ts, nonce, string(body))
	auth := douyinpayAuthHeader(p.mchID, nonce, ts, p.serial, sign)
	headers := map[string]string{"Authorization": "DouyinPay-RSA " + auth}
	raw, e := p.pluginCall(ctx, op, method, target, body, "application/json; charset=utf-8", headers)
	if e != nil {
		return nil, e
	}
	envelope, e := callbackJSON(raw)
	if e != nil {
		return nil, &UnknownOutcome{Operation: op, Cause: e}
	}
	if jsonText(envelope, "err_no") != "0" {
		return nil, rejected("DOUYINPAY_" + douyinpayCode(jsonText(envelope, "err_no")))
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		return nil, &UnknownOutcome{Operation: op, Cause: ErrInvalid}
	}
	return data, nil
}

// VerifyNotification needs the platform public key (configured as platform_cert)
// to check the header signature, then AES-256-GCM decrypts the resource.
func (p *douyinpayPlugin) VerifyNotification(_ context.Context, header http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	if p.platformPub == nil {
		return PaymentEvent{}, ErrUnsupported
	}
	sig := header.Get("Douyinpay-Signature")
	ts := header.Get("Douyinpay-Timestamp")
	nonce := header.Get("Douyinpay-Nonce")
	if sig == "" || ts == "" || nonce == "" {
		return PaymentEvent{}, ErrAuthentication
	}
	message := ts + "\n" + nonce + "\n" + string(raw) + "\n"
	if !rsaVerify(p.platformPub, crypto.SHA256, message, sig) {
		return PaymentEvent{}, ErrAuthentication
	}
	body, e := callbackJSON(raw)
	if e != nil {
		return PaymentEvent{}, ErrInvalid
	}
	resource, ok := body["resource"].(map[string]any)
	if !ok {
		return PaymentEvent{}, ErrInvalid
	}
	plain, e := p.decryptResource(jsonText(resource, "ciphertext"), jsonText(resource, "nonce"), jsonText(resource, "associated_data"))
	if e != nil {
		return PaymentEvent{}, ErrAuthentication
	}
	data, e := callbackJSON(plain)
	if e != nil {
		return PaymentEvent{}, ErrInvalid
	}
	if jsonText(data, "trade_state") != "SUCCESS" {
		return PaymentEvent{}, ErrInvalid
	}
	amount := douyinpayTotal(data)
	if amount <= 0 || expected.Amount <= 0 || amount != expected.Amount || jsonText(data, "out_trade_no") != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	buyer := ""
	if payer, ok := data["payer"].(map[string]any); ok {
		buyer = jsonText(payer, "openid")
	}
	return paidEvent(expected.TradeNo, jsonText(data, "transaction_id"), buyer, amount), nil
}

func (p *douyinpayPlugin) decryptResource(ciphertextB64, nonce, aad string) ([]byte, error) {
	ciphertext, e := base64.StdEncoding.DecodeString(ciphertextB64)
	if e != nil || len(ciphertext) <= 16 {
		return nil, ErrInvalid
	}
	block, e := aes.NewCipher(p.apiKey)
	if e != nil {
		return nil, ErrInvalid
	}
	gcm, e := cipher.NewGCMWithNonceSize(block, len(nonce))
	if e != nil {
		return nil, ErrInvalid
	}
	plain, e := gcm.Open(nil, []byte(nonce), ciphertext, []byte(aad))
	if e != nil {
		return nil, ErrAuthentication
	}
	return plain, nil
}

func (p *douyinpayPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.ProviderTradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	payload := map[string]any{
		"mchid":          p.mchID,
		"transaction_id": r.ProviderTradeNo,
		"out_refund_no":  r.RefundNo,
		"amount": map[string]any{
			"refund":   r.Amount,
			"total":    r.TotalAmount,
			"currency": "CNY",
		},
	}
	out, e := p.execute(ctx, "douyinpay.refund", http.MethodPost, "/v1/trade/refund/domestic/refunds", payload)
	if e != nil {
		return RefundResult{}, e
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(out, "refund_id"), State: "PROCESSING", Amount: r.Amount}, nil
}

// The original replyNotify echoes JSON {"code":"SUCCESS"} / {"code":"FAIL"}.
func (p *douyinpayPlugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return http.StatusOK, "application/json; charset=utf-8", `{"code":"FAIL","message":"verify failed"}`
	}
	return http.StatusOK, "application/json; charset=utf-8", `{"code":"SUCCESS"}`
}

// ---- douyinpay-scoped helpers ----

func douyinpayAuthHeader(mchID, nonce, ts, serial, sign string) string {
	return "mchid=\"" + mchID + "\",nonce_str=\"" + nonce + "\",timestamp=\"" + ts + "\",serial_no=\"" + serial + "\",signature=\"" + sign + "\""
}

func douyinpayCode(code string) string {
	code = strings.TrimSpace(code)
	if code == "" || len(code) > 48 || strings.ContainsAny(code, " \r\n") {
		return "REJECTED"
	}
	return code
}

func douyinpayNonce() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(b)
}

// douyinpayTotal reads amount.total (cents) from a resource object.
func douyinpayTotal(data map[string]any) int64 {
	amt, ok := data["amount"].(map[string]any)
	if !ok {
		return 0
	}
	n, e := parseFen(jsonText(amt, "total"))
	if e != nil {
		return 0
	}
	return n
}

var _ Provider = (*douyinpayPlugin)(nil)
var _ NotificationVerifier = (*douyinpayPlugin)(nil)
var _ CreateValidator = (*douyinpayPlugin)(nil)
