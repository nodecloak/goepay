package payment

// Port of epay_release/plugins/wxpayng (微信支付国际版V3, WeChatPay\V3\
// GlobalPaymentService). WeChat Pay APIv3: each request carries an
// Authorization: WECHATPAY2-SHA256-RSA2048 header whose signature is
// SHA256withRSA over "<METHOD>\n<url-path-with-query>\n<timestamp>\n<nonce>\n
// <body>\n" with the merchant API private key; responses and notifications are
// verified with the WeChat platform public key over "<ts>\n<nonce>\n<body>\n"
// and the resource is AES-256-GCM decrypted with the APIv3 key.
//
// The original keeps the private key / platform public key in /cert/ files; we
// take them as PEM inputs instead (the brief's rule). Product routing in the
// original is driven by apptype (1 Native, 2 JSAPI, 3 H5, 5 APP) and device
// sniffing; we expose one method per enabled apptype.
//   apptype 1 Native -> "native"  (/v3/global/transactions/native, code_url).
//   apptype 2 JSAPI  -> "jsapi"   (/v3/global/transactions/jsapi, needs openid).
//   apptype 3 H5     -> "h5"      (/v3/global/transactions/mweb, h5_url/mweb_url).
//   apptype 5 APP    -> "app"     (/v3/global/transactions/app, app params).
// Currency is CNY throughout (amount.total = round(realmoney*100)), so no
// cross-currency handling is needed.
//
// Query/Close/Refund use the documented endpoints. The kit's constant-time
// compares, bounded bodies and no-InsecureSkipVerify all apply.

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "wxpayng",
		Title:  "微信支付国际版V3",
		Author: "微信",
		Link:   "https://pay.weixin.qq.com/",
		Types:  []string{"wxpay"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "公众号或小程序APPID", Required: true},
			{Key: "appmchid", Label: "商户号", Required: true},
			{Key: "appsecret", Label: "商户APIv3密钥", Secret: true, Required: true},
			{Key: "appkey", Label: "商户API证书序列号", Required: true},
			{Key: "publickeyid", Label: "微信支付公钥ID", Note: "平台证书模式需要留空"},
			{Key: "appurl", Label: "商户行业编码", Required: true},
			// Keys that the original loads from /cert/ files; taken as PEM here.
			{Key: "merchant_private_key", Label: "商户API私钥(PEM)", PEM: true, Secret: true, Required: true},
			{Key: "platform_public_key", Label: "微信支付平台公钥/证书(PEM)", PEM: true, Required: true},
			{Key: "platform_serial", Label: "平台公钥ID或平台证书序列号", Required: true},
		},
		AppTypes: []PluginOption{
			{Value: "1", Label: "Native支付"},
			{Value: "2", Label: "JSAPI支付"},
			{Value: "3", Label: "H5支付"},
			{Value: "5", Label: "APP支付"},
		},
		Build: newWxpayngPlugin,
	})
}

const wxpayngGateway = "https://api.mch.weixin.qq.com"

type wxpayngPlugin struct {
	pluginBase
	appID, mchID   string
	apiV3Key       []byte // 32 bytes, AEAD_AES_256_GCM
	certSerial     string
	merchantPriv   *rsa.PrivateKey
	platformPub    *rsa.PublicKey
	platformSerial string
	merchantCat    string // merchant_category_code (appurl)
	appTypes       map[string]bool
	host           string // wxpayngGateway in production; overridable in tests
}

func newWxpayngPlugin(c PluginChannel) (Provider, error) {
	appID := strings.TrimSpace(c.Config["appid"])
	mchID := strings.TrimSpace(c.Config["appmchid"])
	apiV3 := c.Config["appsecret"]
	certSerial := strings.TrimSpace(c.Config["appkey"])
	merchantCat := strings.TrimSpace(c.Config["appurl"])
	if appID == "" || len(appID) > 64 || mchID == "" || len(mchID) > 32 || certSerial == "" || len(certSerial) > 128 || merchantCat == "" || len(merchantCat) > 32 {
		return nil, ErrInvalid
	}
	// APIv3 key must be exactly 32 bytes (BaseService constructor check).
	if len(apiV3) != 32 {
		return nil, ErrInvalid
	}
	priv, e := pluginPrivateKey(c.Certificates["merchant_private_key"])
	if e != nil {
		return nil, e
	}
	pub, e := pluginPublicKey(c.Certificates["platform_public_key"])
	if e != nil {
		return nil, e
	}
	platformSerial := strings.TrimSpace(c.Config["platform_serial"])
	if platformSerial == "" || len(platformSerial) > 128 {
		return nil, ErrInvalid
	}
	appTypes := map[string]bool{}
	for _, t := range c.AppTypes {
		appTypes[t] = true
	}
	if len(appTypes) == 0 {
		appTypes["1"] = true // default Native, matching the original fall-through
	}
	base, e := newPluginBase(c, pluginAccount(wxpayngGateway, mchID, appID))
	if e != nil {
		return nil, e
	}
	return &wxpayngPlugin{
		pluginBase: base, appID: appID, mchID: mchID, apiV3Key: []byte(apiV3), certSerial: certSerial,
		merchantPriv: priv, platformPub: pub, platformSerial: platformSerial, merchantCat: merchantCat, appTypes: appTypes, host: wxpayngGateway,
	}, nil
}

func (p *wxpayngPlugin) methods() []string {
	var m []string
	if p.appTypes["1"] {
		m = append(m, "native")
	}
	if p.appTypes["2"] {
		m = append(m, "jsapi")
	}
	if p.appTypes["3"] {
		m = append(m, "h5")
	}
	if p.appTypes["5"] {
		m = append(m, "app")
	}
	return m
}

func (p *wxpayngPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Query: true, Close: true, Refund: true, RefundQuery: true}
}

func (p *wxpayngPlugin) ValidateCreate(r CreateRequest) error {
	if e := validatePluginCreate(r, p.methods()); e != nil {
		return e
	}
	// JSAPI needs the payer openid.
	if r.Method == "jsapi" && r.OpenID == "" {
		return ErrInvalid
	}
	return nil
}

// wxpayngNonce returns a 32-char lowercase alphanumeric nonce.
func wxpayngNonce() (string, error) {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b), nil
}

// authorization builds the WECHATPAY2-SHA256-RSA2048 Authorization value for a
// request (BaseService::getAuthorization + makeSign).
func (p *wxpayngPlugin) authorization(method, pathWithQuery, body string) (string, error) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce, e := wxpayngNonce()
	if e != nil {
		return "", e
	}
	message := method + "\n" + pathWithQuery + "\n" + ts + "\n" + nonce + "\n" + body + "\n"
	sig, e := rsaSign(p.merchantPriv, crypto.SHA256, message)
	if e != nil {
		return "", e
	}
	return `mchid="` + p.mchID + `",nonce_str="` + nonce + `",timestamp="` + ts +
		`",serial_no="` + p.certSerial + `",signature="` + sig + `"`, nil
}

// execute signs and sends a V3 request, verifying the response signature with
// the platform public key, and returns the decoded body (or an error).
func (p *wxpayngPlugin) execute(ctx context.Context, operation, method, pathWithQuery string, params any) (map[string]any, error) {
	var body []byte
	if params != nil {
		var e error
		body, e = json.Marshal(params)
		if e != nil {
			return nil, ErrInvalid
		}
	}
	auth, e := p.authorization(method, pathWithQuery, string(body))
	if e != nil {
		return nil, ErrAuthentication
	}
	headers := map[string]string{
		"Authorization": "WECHATPAY2-SHA256-RSA2048 " + auth,
		"Accept":        "application/json",
	}
	raw, e := p.pluginCall(ctx, operation, method, p.host+pathWithQuery, body, wxpayngContentType(method), headers)
	if e != nil {
		return nil, e
	}
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	out, e := callbackJSON(raw)
	if e != nil {
		return nil, &UnknownOutcome{Operation: operation, Cause: e}
	}
	return out, nil
}

func wxpayngContentType(method string) string {
	if method == http.MethodPost || method == http.MethodPut {
		return "application/json; charset=utf-8"
	}
	return ""
}

// wxpayngPublicParams are the appid/mchid pair sent on create calls.
func (p *wxpayngPlugin) wxpayngPublicParams() map[string]any {
	return map[string]any{"appid": p.appID, "mchid": p.mchID}
}

func (p *wxpayngPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	params := p.wxpayngPublicParams()
	params["description"] = r.Subject
	params["out_trade_no"] = r.TradeNo
	params["notify_url"] = r.NotifyURL
	params["merchant_category_code"] = p.merchantCat
	params["amount"] = map[string]any{"total": r.Amount, "currency": "CNY"}
	scene := map[string]any{"payer_client_ip": wxpayngClientIP(r.ClientIP)}

	switch r.Method {
	case "native":
		params["trade_type"] = "NATIVE"
		params["scene_info"] = scene
		out, e := p.execute(ctx, "wxpayng.native", http.MethodPost, "/v3/global/transactions/native", params)
		if e != nil {
			return CreateResult{}, e
		}
		codeURL := jsonText(out, "code_url")
		if codeURL == "" {
			return CreateResult{}, wxpayngRejectOrUnknown(out, "wxpayng.native")
		}
		return CreateResult{MaterialType: "qrcode", Material: codeURL}, nil
	case "h5":
		params["trade_type"] = "MWEB"
		params["scene_info"] = scene
		out, e := p.execute(ctx, "wxpayng.h5", http.MethodPost, "/v3/global/transactions/mweb", params)
		if e != nil {
			return CreateResult{}, e
		}
		h5URL := jsonText(out, "mweb_url")
		if h5URL == "" {
			h5URL = jsonText(out, "h5_url")
		}
		if h5URL == "" || !validPaymentURL(h5URL) {
			return CreateResult{}, wxpayngRejectOrUnknown(out, "wxpayng.h5")
		}
		return CreateResult{MaterialType: "jump", Material: h5URL}, nil
	case "jsapi":
		params["trade_type"] = "JSAPI"
		params["payer"] = map[string]any{"openid": r.OpenID}
		params["scene_info"] = scene
		out, e := p.execute(ctx, "wxpayng.jsapi", http.MethodPost, "/v3/global/transactions/jsapi", params)
		if e != nil {
			return CreateResult{}, e
		}
		prepay := jsonText(out, "prepay_id")
		if prepay == "" {
			return CreateResult{}, wxpayngRejectOrUnknown(out, "wxpayng.jsapi")
		}
		jsapi, e := p.jsapiParameters(prepay)
		if e != nil {
			return CreateResult{}, e
		}
		return CreateResult{MaterialType: "jsapi", Material: jsapi}, nil
	case "app":
		params["trade_type"] = "APP"
		params["scene_info"] = scene
		out, e := p.execute(ctx, "wxpayng.app", http.MethodPost, "/v3/global/transactions/app", params)
		if e != nil {
			return CreateResult{}, e
		}
		prepay := jsonText(out, "prepay_id")
		if prepay == "" {
			return CreateResult{}, wxpayngRejectOrUnknown(out, "wxpayng.app")
		}
		appParams, e := p.appParameters(prepay)
		if e != nil {
			return CreateResult{}, e
		}
		return CreateResult{MaterialType: "app", Material: appParams}, nil
	}
	return CreateResult{}, ErrInvalid
}

func wxpayngClientIP(ip string) string {
	if ip == "" {
		return "0.0.0.0"
	}
	return ip
}

// wxpayngRejectOrUnknown classifies a 2xx reply that lacked the expected field:
// a WeChat error body ({"code":"...","message":"..."}) is a definitive refusal;
// anything else means the order may exist (unknown outcome).
func wxpayngRejectOrUnknown(out map[string]any, op string) error {
	if code := jsonText(out, "code"); code != "" {
		return rejected("WXPAYNG_" + code)
	}
	return &UnknownOutcome{Operation: op, Cause: errUnexpectedWxBody}
}

var errUnexpectedWxBody = &wxpayngStaticError{"wxpayng: unexpected response body"}

type wxpayngStaticError struct{ s string }

func (e *wxpayngStaticError) Error() string { return e.s }

// wxpayngPathSafe rejects an id that could escape the URL path (trade/refund
// numbers are our own, but we defend against injected separators).
func wxpayngPathSafe(id string) string {
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c == '-' || c == '_') {
			return "invalid"
		}
	}
	if len(id) == 0 || len(id) > 64 {
		return "invalid"
	}
	return id
}

// jsapiParameters signs the JSAPI launch parameters (getJsApiParameters):
// paySign = RSA(appId\n timeStamp\n nonceStr\n package\n).
func (p *wxpayngPlugin) jsapiParameters(prepayID string) (string, error) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce, e := wxpayngNonce()
	if e != nil {
		return "", e
	}
	pkg := "prepay_id=" + prepayID
	sig, e := rsaSign(p.merchantPriv, crypto.SHA256, p.appID+"\n"+ts+"\n"+nonce+"\n"+pkg+"\n")
	if e != nil {
		return "", e
	}
	out := map[string]string{
		"appId": p.appID, "timeStamp": ts, "nonceStr": nonce, "package": pkg,
		"paySign": sig, "signType": "RSA",
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

// appParameters signs the APP launch parameters (getAppParameters):
// sign = RSA(appid\n timestamp\n noncestr\n prepayid\n).
func (p *wxpayngPlugin) appParameters(prepayID string) (string, error) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce, e := wxpayngNonce()
	if e != nil {
		return "", e
	}
	sig, e := rsaSign(p.merchantPriv, crypto.SHA256, p.appID+"\n"+ts+"\n"+nonce+"\n"+prepayID+"\n")
	if e != nil {
		return "", e
	}
	out := map[string]string{
		"appid": p.appID, "partnerid": p.mchID, "prepayid": prepayID,
		"package": "Sign=WXPay", "noncestr": nonce, "timestamp": ts, "sign": sig,
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

// Notifications are POST JSON; the signed message is "<ts>\n<nonce>\n<body>\n"
// over the Wechatpay-* headers, verified with the platform public key. The
// resource is then AES-256-GCM decrypted with the APIv3 key. This is exactly
// what BaseService::notify + checkSign do.
func (p *wxpayngPlugin) VerifyNotification(_ context.Context, header http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	if len(raw) == 0 || len(raw) > 1<<20 {
		return PaymentEvent{}, ErrInvalid
	}
	serial := header.Get("Wechatpay-Serial")
	signature := header.Get("Wechatpay-Signature")
	ts := header.Get("Wechatpay-Timestamp")
	nonce := header.Get("Wechatpay-Nonce")
	if serial == "" || signature == "" || ts == "" || nonce == "" {
		return PaymentEvent{}, ErrAuthentication
	}
	if serial != p.platformSerial {
		return PaymentEvent{}, ErrAuthentication
	}
	if strings.ContainsAny(ts+nonce, "\r\n") {
		return PaymentEvent{}, ErrAuthentication
	}
	message := ts + "\n" + nonce + "\n" + string(raw) + "\n"
	if !rsaVerify(p.platformPub, crypto.SHA256, message, signature) {
		return PaymentEvent{}, ErrAuthentication
	}
	// Signature verified: decrypt the resource and read the transaction.
	env, e := callbackJSON(raw)
	if e != nil {
		return PaymentEvent{}, ErrInvalid
	}
	res, ok := env["resource"].(map[string]any)
	if !ok {
		return PaymentEvent{}, ErrInvalid
	}
	plain, e := p.decryptResource(res)
	if e != nil {
		return PaymentEvent{}, ErrAuthentication
	}
	tx, e := callbackJSON(plain)
	if e != nil {
		return PaymentEvent{}, ErrInvalid
	}
	if jsonText(tx, "trade_state") != "SUCCESS" {
		return PaymentEvent{}, ErrInvalid
	}
	if jsonText(tx, "out_trade_no") != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	amt, ok := tx["amount"].(map[string]any)
	if !ok {
		return PaymentEvent{}, ErrInvalid
	}
	total, e := wxpayngInt(amt["total"])
	if e != nil || expected.Amount <= 0 || total != expected.Amount {
		return PaymentEvent{}, ErrAuthentication
	}
	if cur := jsonText(amt, "currency"); cur != "" && cur != "CNY" {
		return PaymentEvent{}, ErrAuthentication
	}
	buyer := ""
	if payer, ok := tx["payer"].(map[string]any); ok {
		buyer = jsonText(payer, "openid")
	}
	return paidEvent(expected.TradeNo, jsonText(tx, "id"), buyer, total), nil
}

// decryptResource AES-256-GCM decrypts the APIv3 resource with the APIv3 key.
func (p *wxpayngPlugin) decryptResource(res map[string]any) ([]byte, error) {
	enc := EncryptedResource{
		Algorithm:      jsonText(res, "algorithm"),
		Nonce:          jsonText(res, "nonce"),
		AssociatedData: jsonText(res, "associated_data"),
		Ciphertext:     jsonText(res, "ciphertext"),
	}
	return DecryptWechatResource(p.apiV3Key, enc)
}

// wxpayngInt reads a JSON number/string amount as int64 cents.
func wxpayngInt(v any) (int64, error) {
	s := jsonTextAny(v)
	if s == "" {
		return 0, ErrInvalid
	}
	return strconv.ParseInt(s, 10, 64)
}

// WeChat expects {"code":"SUCCESS"} on a verified callback and a 4xx JSON
// {"code":"FAIL","message":...} on failure (replyNotify).
func (p *wxpayngPlugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return http.StatusBadRequest, "application/json; charset=utf-8", `{"code":"FAIL","message":"FAIL"}`
	}
	return http.StatusOK, "application/json; charset=utf-8", `{"code":"SUCCESS"}`
}

// Query reads the order by our out_trade_no (orderQuery). Response signature is
// NOT re-verified here (the kit's HTTP path does not expose response headers);
// we rely on TLS + the trade number match. State mapping per WeChat trade_state.
func (p *wxpayngPlugin) Query(ctx context.Context, r QueryRequest) (QueryResult, error) {
	if r.TradeNo == "" {
		return QueryResult{}, ErrInvalid
	}
	path := "/v3/global/transactions/out-trade-no/" + wxpayngPathSafe(r.TradeNo) + "?mchid=" + p.mchID
	out, e := p.execute(ctx, "wxpayng.query", http.MethodGet, path, nil)
	if e != nil {
		return QueryResult{}, e
	}
	if jsonText(out, "out_trade_no") != r.TradeNo {
		if jsonText(out, "code") != "" {
			return QueryResult{}, rejected("WXPAYNG_" + jsonText(out, "code"))
		}
		return QueryResult{}, ErrAuthentication
	}
	state := wxpayngState(jsonText(out, "trade_state"))
	var amount int64
	if amt, ok := out["amount"].(map[string]any); ok {
		amount, _ = wxpayngInt(amt["total"])
	}
	buyer := ""
	if payer, ok := out["payer"].(map[string]any); ok {
		buyer = jsonText(payer, "openid")
	}
	return QueryResult{TradeNo: r.TradeNo, ProviderTradeNo: jsonText(out, "id"), State: state, Currency: "CNY", BuyerID: buyer, Amount: amount}, nil
}

func wxpayngState(trade string) string {
	switch trade {
	case "SUCCESS":
		return "SUCCESS"
	case "CLOSED", "REVOKED", "PAYERROR":
		return "CLOSED"
	case "REFUND":
		return "REFUND"
	default: // NOTPAY, USERPAYING
		return "NOTPAY"
	}
}

// Close posts the close endpoint keyed by our out_trade_no (closeOrder).
func (p *wxpayngPlugin) Close(ctx context.Context, r QueryRequest) error {
	if r.TradeNo == "" {
		return ErrInvalid
	}
	path := "/v3/global/transactions/out-trade-no/" + wxpayngPathSafe(r.TradeNo) + "/close"
	out, e := p.execute(ctx, "wxpayng.close", http.MethodPost, path, map[string]any{"mchid": p.mchID})
	if e != nil {
		return e
	}
	if code := jsonText(out, "code"); code != "" && code != "SUCCESS" {
		return rejected("WXPAYNG_" + code)
	}
	return nil
}

// Refund posts /v3/global/refunds (refund). transaction_id is the WeChat order
// id (api_trade_no), out_refund_no our refund number, amount in cents CNY.
func (p *wxpayngPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.ProviderTradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	params := p.wxpayngPublicParams()
	params["transaction_id"] = r.ProviderTradeNo
	params["out_refund_no"] = r.RefundNo
	params["amount"] = map[string]any{"refund": r.Amount, "total": r.TotalAmount, "currency": "CNY"}
	out, e := p.execute(ctx, "wxpayng.refund", http.MethodPost, "/v3/global/refunds", params)
	if e != nil {
		return RefundResult{}, e
	}
	status := jsonText(out, "status")
	if jsonText(out, "refund_id") == "" && jsonText(out, "out_refund_no") == "" {
		return RefundResult{}, wxpayngRejectOrUnknown(out, "wxpayng.refund")
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(out, "refund_id"), State: wxpayngRefundState(status), Amount: r.Amount}, nil
}

func wxpayngRefundState(status string) string {
	switch status {
	case "SUCCESS":
		return "SUCCESS"
	case "CLOSED", "ABNORMAL":
		return "FAIL"
	default: // PROCESSING or unknown-but-accepted
		return "PROCESSING"
	}
}

// QueryRefund reads a refund by our out_refund_no (refundQuery).
func (p *wxpayngPlugin) QueryRefund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.RefundNo == "" {
		return RefundResult{}, ErrInvalid
	}
	path := "/v3/global/refunds/out-refund-no/" + wxpayngPathSafe(r.RefundNo)
	out, e := p.execute(ctx, "wxpayng.refundquery", http.MethodGet, path, nil)
	if e != nil {
		return RefundResult{}, e
	}
	if jsonText(out, "out_refund_no") != r.RefundNo {
		if code := jsonText(out, "code"); code != "" {
			return RefundResult{}, rejected("WXPAYNG_" + code)
		}
		return RefundResult{}, ErrAuthentication
	}
	var amount int64
	if amt, ok := out["amount"].(map[string]any); ok {
		amount, _ = wxpayngInt(amt["refund"])
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(out, "refund_id"), State: wxpayngRefundState(jsonText(out, "status")), Amount: amount}, nil
}

var _ Provider = (*wxpayngPlugin)(nil)
var _ NotificationVerifier = (*wxpayngPlugin)(nil)
var _ CreateValidator = (*wxpayngPlugin)(nil)
var _ NotificationAcker = (*wxpayngPlugin)(nil)
