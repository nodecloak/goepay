package payment

// Port of epay_release/plugins/lakala (拉卡拉). Two products share one channel,
// chosen by appselect: 聚合扫码 (scan, appselect=0) posts JSON to
// /api/v3/labs/trans/preorder and returns a QR code / jsapi params; 聚合收银台
// (cashier, appselect=1) posts to /api/v3/ccss/counter/order/special_create and
// redirects the payer to counter_url. Every request carries an Authorization
// header "LKLAPI-SHA256withRSA appid=..,serial_no=..,timestamp=..,nonce_str=..,
// signature=.." where the signature is RSA-SHA256 over
// appid\nserial_no\ntimestamp\nnonce_str\nbody\n using the merchant private key.
// Callbacks are JSON POST; the Authorization header is verified over
// timestamp\nnonce_str\nbody\n with the platform certificate.
//
// Credentials in the original live in cert/ (api_private_key.pem merchant key,
// api_cert.cer merchant certificate for its serial number, lkl-apigw-v1.cer
// platform public certificate). GOEPAY has no plugin filesystem, so they are
// PEM configuration here. Query is implemented; refund is implemented
// (/api/v3/labs/relation/refund). Transfers/complaints are out of scope.

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "lakala",
		Title:  "拉卡拉",
		Author: "拉卡拉",
		Link:   "https://www.lakala.com/",
		Types:  []string{"alipay", "wxpay", "bank"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "APPID", Required: true},
			{Key: "appmchid", Label: "商户号", Required: true},
			{Key: "appkey", Label: "终端号", Required: true},
			{Key: "appselect", Label: "接口类型", Options: []PluginOption{{Value: "0", Label: "聚合扫码"}, {Value: "1", Label: "聚合收银台"}}},
			{Key: "appswitch", Label: "环境选择", Options: []PluginOption{{Value: "0", Label: "生产环境"}, {Value: "1", Label: "测试环境"}}},
			{Key: "merchant_private_key", Label: "商户私钥(api_private_key.pem)", Note: "商户RSA私钥 PEM 文本", PEM: true, Required: true},
			{Key: "merchant_cert", Label: "商户证书(api_cert.cer)", Note: "商户证书 PEM，用于提取证书序列号", PEM: true, Required: true},
			{Key: "platform_cert", Label: "拉卡拉平台证书(lkl-apigw-v1.cer)", Note: "平台公钥证书 PEM，用于回调验签", PEM: true, Required: true},
		},
		Note:  "聚合扫码/聚合收银台。退款与查单已实现。转账/投诉未实现。测试环境需填证书对应的 lkl-apigw-v2.cer。",
		Build: newLakalaPlugin,
	})
}

const (
	lakalaProdGateway = "https://s2.lakala.com"
	lakalaTestGateway = "https://test.wsmsd.cn/sit"
	lakalaSchema      = "LKLAPI-SHA256withRSA"
)

type lakalaPlugin struct {
	pluginBase
	appid, mchNo, termNo string
	gateway              string
	serialNo             string
	privateKey           *rsa.PrivateKey
	platformKey          *rsa.PublicKey
	cashier              bool
}

func newLakalaPlugin(c PluginChannel) (Provider, error) {
	appid := strings.TrimSpace(c.Config["appid"])
	mchNo := strings.TrimSpace(c.Config["appmchid"])
	termNo := strings.TrimSpace(c.Config["appkey"])
	if lakalaBadField(appid) || lakalaBadField(mchNo) || termNo == "" || len(termNo) > 64 {
		return nil, ErrInvalid
	}
	key, e := pluginPrivateKey(c.Certificates["merchant_private_key"])
	if e != nil {
		return nil, e
	}
	serial, e := lakalaSerial(c.Certificates["merchant_cert"])
	if e != nil {
		return nil, e
	}
	platformKey, e := lakalaCertKey(c.Certificates["platform_cert"])
	if e != nil {
		return nil, e
	}
	gateway := lakalaProdGateway
	if c.Config["appswitch"] == "1" {
		gateway = lakalaTestGateway
	}
	base, e := newPluginBase(c, pluginAccount(gateway, mchNo, termNo))
	if e != nil {
		return nil, e
	}
	return &lakalaPlugin{
		pluginBase: base, appid: appid, mchNo: mchNo, termNo: termNo,
		gateway: gateway, serialNo: serial, privateKey: key, platformKey: platformKey,
		cashier: c.Config["appselect"] == "1",
	}, nil
}

func lakalaBadField(s string) bool {
	return s == "" || len(s) > 64 || strings.ContainsAny(s, "\x00\r\n")
}

// lakalaSerial parses the merchant certificate PEM and returns its serial
// number as a decimal string (matching PHP openssl_x509_parse()['serialNumber']).
func lakalaSerial(text string) (string, error) {
	der := kuaiqianPEMBlock(text)
	if der == nil {
		return "", ErrInvalid
	}
	cert, e := x509.ParseCertificate(der)
	if e != nil || cert.SerialNumber == nil {
		return "", ErrInvalid
	}
	return cert.SerialNumber.String(), nil
}

// lakalaCertKey parses a platform certificate PEM and returns its RSA key.
func lakalaCertKey(text string) (*rsa.PublicKey, error) {
	der := kuaiqianPEMBlock(text)
	if der == nil {
		return nil, ErrInvalid
	}
	cert, e := x509.ParseCertificate(der)
	if e != nil {
		return nil, ErrInvalid
	}
	key, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok || key.N.BitLen() < pluginMinRSABits {
		return nil, ErrInvalid
	}
	return key, nil
}

func (p *lakalaPlugin) methods() []string {
	if p.cashier {
		// 收银台 redirects to a hosted counter page.
		return []string{"h5"}
	}
	// 聚合扫码: QR (native) for all types, jsapi where the original offers JS.
	return []string{"native", "jsapi"}
}

func (p *lakalaPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Query: true, Refund: true}
}

func (p *lakalaPlugin) ValidateCreate(r CreateRequest) error {
	if e := validatePluginCreate(r, p.methods()); e != nil {
		return e
	}
	if r.Method == "jsapi" && strings.TrimSpace(r.OpenID) == "" {
		return ErrInvalid
	}
	return nil
}

// accountType maps the channel pay type to lakala's account_type.
func (p *lakalaPlugin) accountType() string {
	switch p.payType {
	case "alipay":
		return "ALIPAY"
	case "wxpay":
		return "WECHAT"
	case "bank":
		return "UQRCODEPAY"
	}
	return ""
}

// lakalaNonce returns a 32-hex-char nonce for the Authorization header.
func lakalaNonce() (string, error) {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", e
	}
	return hex.EncodeToString(b[:]), nil
}

// authorization builds the LKLAPI-SHA256withRSA header over
// appid\nserial_no\ntimestamp\nnonce_str\nbody\n.
func (p *lakalaPlugin) authorization(body []byte) (string, error) {
	nonce, e := lakalaNonce()
	if e != nil {
		return "", e
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	message := p.appid + "\n" + p.serialNo + "\n" + ts + "\n" + nonce + "\n" + string(body) + "\n"
	sig, e := rsaSign(p.privateKey, crypto.SHA256, message)
	if e != nil {
		return "", e
	}
	return lakalaSchema + ` appid="` + p.appid + `",serial_no="` + p.serialNo + `",timestamp="` + ts + `",nonce_str="` + nonce + `",signature="` + sig + `"`, nil
}

// call wraps params in the public envelope (req_time/version/req_data), signs
// and posts them to path, returning the decoded reply object and top-level code.
func (p *lakalaPlugin) call(ctx context.Context, operation, path, version string, reqData map[string]any) (map[string]any, string, error) {
	envelope := map[string]any{
		"req_time": time.Now().Format("20060102150405"),
		"version":  version,
		"req_data": reqData,
	}
	body, e := json.Marshal(envelope)
	if e != nil {
		return nil, "", ErrInvalid
	}
	auth, e := p.authorization(body)
	if e != nil {
		return nil, "", e
	}
	raw, e := p.pluginCall(ctx, operation, http.MethodPost, p.gateway+path, body, "application/json; charset=utf-8", map[string]string{"Authorization": auth, "Accept": "application/json"})
	if e != nil {
		return nil, "", e
	}
	out, e := callbackJSON(raw)
	if e != nil {
		return nil, "", &UnknownOutcome{Operation: operation, Cause: e}
	}
	return out, jsonText(out, "code"), nil
}

func (p *lakalaPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	if p.cashier {
		return p.createCashier(ctx, r)
	}
	return p.createScan(ctx, r)
}

// createScan posts /api/v3/labs/trans/preorder (trans_type 41 QR / 51 jsapi).
func (p *lakalaPlugin) createScan(ctx context.Context, r CreateRequest) (CreateResult, error) {
	transType := "41"
	acc := map[string]any{}
	if r.Method == "jsapi" {
		transType = "51"
		acc["user_id"] = r.OpenID
	}
	reqData := map[string]any{
		"merchant_no":   p.mchNo,
		"term_no":       p.termNo,
		"out_trade_no":  r.TradeNo,
		"account_type":  p.accountType(),
		"trans_type":    transType,
		"total_amount":  strconv.FormatInt(r.Amount, 10),
		"location_info": map[string]any{"request_ip": r.ClientIP},
		"subject":       r.Subject,
		"notify_url":    r.NotifyURL,
	}
	if len(acc) > 0 {
		reqData["acc_busi_fields"] = acc
	}
	out, code, e := p.call(ctx, "lakala.preorder", "/api/v3/labs/trans/preorder", "3.0", reqData)
	if e != nil {
		return CreateResult{}, e
	}
	if code != "BBS00000" && code != "BBS10000" {
		return CreateResult{}, rejected("LKL_" + code)
	}
	resp, _ := out["resp_data"].(map[string]any)
	if resp == nil {
		return CreateResult{}, &UnknownOutcome{Operation: "lakala.preorder", Cause: ErrInvalid}
	}
	accResp, _ := resp["acc_resp_fields"].(map[string]any)
	if accResp == nil {
		return CreateResult{}, &UnknownOutcome{Operation: "lakala.preorder", Cause: ErrInvalid}
	}
	providerTradeNo := jsonText(resp, "trade_no")
	if r.Method == "jsapi" {
		payload, e := json.Marshal(accResp)
		if e != nil {
			return CreateResult{}, &UnknownOutcome{Operation: "lakala.preorder", Cause: e}
		}
		return CreateResult{MaterialType: "jsapi", Material: string(payload), ProviderTradeNo: providerTradeNo}, nil
	}
	qr := jsonText(accResp, "code")
	if qr == "" {
		return CreateResult{}, rejected("LKL_NO_QRCODE")
	}
	return CreateResult{MaterialType: "qrcode", Material: qr, ProviderTradeNo: providerTradeNo}, nil
}

// createCashier posts /api/v3/ccss/counter/order/special_create and redirects
// to counter_url.
func (p *lakalaPlugin) createCashier(ctx context.Context, r CreateRequest) (CreateResult, error) {
	var payMode string
	switch p.payType {
	case "alipay":
		payMode = "ALIPAY"
	case "wxpay":
		payMode = "WECHAT"
	case "bank":
		payMode = "UNION"
	}
	counter, e := json.Marshal(map[string]string{"pay_mode": payMode})
	if e != nil {
		return CreateResult{}, ErrInvalid
	}
	reqData := map[string]any{
		"out_order_no":         r.TradeNo,
		"merchant_no":          p.mchNo,
		"total_amount":         strconv.FormatInt(r.Amount, 10),
		"order_efficient_time": time.Now().Add(20 * time.Minute).Format("20060102150405"),
		"notify_url":           r.NotifyURL,
		"support_refund":       1,
		"callback_url":         r.ReturnURL,
		"order_info":           r.Subject,
		"counter_param":        string(counter),
	}
	out, code, e := p.call(ctx, "lakala.cashier", "/api/v3/ccss/counter/order/special_create", "1.0", reqData)
	if e != nil {
		return CreateResult{}, e
	}
	if code != "000000" {
		return CreateResult{}, rejected("LKL_" + code)
	}
	resp, _ := out["resp_data"].(map[string]any)
	if resp == nil {
		return CreateResult{}, &UnknownOutcome{Operation: "lakala.cashier", Cause: ErrInvalid}
	}
	counterURL := jsonText(resp, "counter_url")
	if counterURL == "" || !validPaymentURL(counterURL) {
		return CreateResult{}, rejected("LKL_NO_COUNTER_URL")
	}
	return CreateResult{MaterialType: "jump", Material: counterURL, ProviderTradeNo: jsonText(resp, "pay_order_no")}, nil
}

// Query reads /api/v3/labs/query/tradequery keyed by our out_trade_no.
func (p *lakalaPlugin) Query(ctx context.Context, r QueryRequest) (QueryResult, error) {
	if r.TradeNo == "" {
		return QueryResult{}, ErrInvalid
	}
	reqData := map[string]any{"merchant_no": p.mchNo, "term_no": p.termNo, "out_trade_no": r.TradeNo}
	out, code, e := p.call(ctx, "lakala.query", "/api/v3/labs/query/tradequery", "3.0", reqData)
	if e != nil {
		return QueryResult{}, e
	}
	if code != "BBS00000" && code != "BBS10000" {
		return QueryResult{}, rejected("LKL_" + code)
	}
	resp, _ := out["resp_data"].(map[string]any)
	if resp == nil {
		return QueryResult{}, ErrAuthentication
	}
	if on := jsonText(resp, "out_trade_no"); on != "" && on != r.TradeNo {
		return QueryResult{}, ErrAuthentication
	}
	state := "NOTPAY"
	switch jsonText(resp, "trade_state") {
	case "SUCCESS":
		state = "SUCCESS"
	case "CLOSED", "REVOKED":
		state = "CLOSED"
	case "REFUND", "PARTIAL_REFUND":
		state = "REFUND"
	}
	amount, _ := parseFen(jsonText(resp, "total_amount"))
	return QueryResult{TradeNo: r.TradeNo, ProviderTradeNo: jsonText(resp, "trade_no"), State: state, Currency: "CNY", BuyerID: jsonText(resp, "user_id2"), Amount: amount}, nil
}

// Refund posts /api/v3/labs/relation/refund.
func (p *lakalaPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	reqData := map[string]any{
		"merchant_no":         p.mchNo,
		"term_no":             p.termNo,
		"out_trade_no":        r.RefundNo,
		"refund_amount":       strconv.FormatInt(r.Amount, 10),
		"origin_out_trade_no": r.TradeNo,
		"location_info":       map[string]any{"request_ip": "127.0.0.1"},
	}
	if r.ProviderTradeNo != "" {
		reqData["origin_trade_no"] = r.ProviderTradeNo
	}
	out, code, e := p.call(ctx, "lakala.refund", "/api/v3/labs/relation/refund", "3.0", reqData)
	if e != nil {
		return RefundResult{}, e
	}
	if code != "BBS00000" && code != "BBS10000" {
		return RefundResult{}, rejected("LKL_" + code)
	}
	resp, _ := out["resp_data"].(map[string]any)
	if resp == nil {
		return RefundResult{}, &UnknownOutcome{Operation: "lakala.refund", Cause: ErrInvalid}
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(resp, "trade_no"), State: "SUCCESS", Amount: r.Amount}, nil
}

// VerifyNotification handles both /pay/notify (trade_status SUCCESS) and the
// cashier callback (order_status 2). The signature is in the Authorization
// header over timestamp\nnonce_str\nbody\n.
func (p *lakalaPlugin) VerifyNotification(_ context.Context, header http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	if len(raw) == 0 || len(raw) > 1<<20 {
		return PaymentEvent{}, ErrInvalid
	}
	auth := ""
	if header != nil {
		auth = header.Get("Authorization")
	}
	ts, nonce, sig, ok := lakalaParseAuth(auth)
	if !ok {
		return PaymentEvent{}, ErrAuthentication
	}
	message := ts + "\n" + nonce + "\n" + string(raw) + "\n"
	if !rsaVerify(p.platformKey, crypto.SHA256, message, sig) {
		return PaymentEvent{}, ErrAuthentication
	}
	data, e := callbackJSON(raw)
	if e != nil {
		return PaymentEvent{}, ErrInvalid
	}
	// Scan/micropay callback shape.
	if outTradeNo := jsonText(data, "out_trade_no"); outTradeNo != "" {
		if jsonText(data, "trade_status") != "SUCCESS" {
			return PaymentEvent{}, ErrInvalid
		}
		if outTradeNo != expected.TradeNo {
			return PaymentEvent{}, ErrAuthentication
		}
		amount, ae := parseFen(jsonText(data, "total_amount"))
		if ae != nil || expected.Amount <= 0 || amount != expected.Amount {
			return PaymentEvent{}, ErrAuthentication
		}
		return paidEvent(expected.TradeNo, jsonText(data, "trade_no"), jsonText(data, "user_id2"), amount), nil
	}
	// Cashier callback shape (out_order_no / order_status / order_trade_info).
	if outOrderNo := jsonText(data, "out_order_no"); outOrderNo != "" {
		if jsonText(data, "order_status") != "2" {
			return PaymentEvent{}, ErrInvalid
		}
		if outOrderNo != expected.TradeNo {
			return PaymentEvent{}, ErrAuthentication
		}
		amount, ae := parseFen(jsonText(data, "total_amount"))
		if ae != nil || expected.Amount <= 0 || amount != expected.Amount {
			return PaymentEvent{}, ErrAuthentication
		}
		info, _ := data["order_trade_info"].(map[string]any)
		providerTradeNo, buyer := "", ""
		if info != nil {
			providerTradeNo = jsonText(info, "trade_no")
			buyer = jsonText(info, "user_id2")
		}
		return paidEvent(expected.TradeNo, providerTradeNo, buyer, amount), nil
	}
	return PaymentEvent{}, ErrInvalid
}

// NotificationAck: the original echoes plain "success"/"fail" (pluginBase
// default already does this).

// lakalaParseAuth extracts timestamp, nonce_str and signature from the
// LKLAPI-SHA256withRSA Authorization header.
func lakalaParseAuth(auth string) (ts, nonce, sig string, ok bool) {
	auth = strings.TrimSpace(auth)
	if !strings.HasPrefix(auth, lakalaSchema) {
		return "", "", "", false
	}
	auth = strings.TrimSpace(strings.TrimPrefix(auth, lakalaSchema))
	fields := map[string]string{}
	for _, part := range strings.Split(auth, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.Trim(strings.TrimSpace(kv[1]), `"`)
		fields[key] = val
	}
	ts, nonce, sig = fields["timestamp"], fields["nonce_str"], fields["signature"]
	if ts == "" || nonce == "" || sig == "" {
		return "", "", "", false
	}
	return ts, nonce, sig, true
}

var _ Provider = (*lakalaPlugin)(nil)
var _ NotificationVerifier = (*lakalaPlugin)(nil)
var _ CreateValidator = (*lakalaPlugin)(nil)
