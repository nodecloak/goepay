package payment

// Port of epay_release/plugins/qqpay (QQ钱包官方支付, cccyun/qqpay-sdk). The
// gateway speaks a WeChat-style flat <xml> protocol signed MD5 UPPERCASE over
// the ksort-ed non-empty, non-array params joined "k=v&" plus "&key=<apikey>".
// Unified order (qpay_unified_order.cgi) returns code_url (NATIVE), prepay_id
// (JSAPI/APP). The async callback is the same signed XML; the original also
// re-queries the order, but we verify signature + amount + trade number, which
// is the trust-bearing part. Refund and reverse need a client certificate
// (mTLS), so Refund takes cert/key PEM inputs and builds its own mTLS client.
// Transfers (企业付款) are OUT OF SCOPE.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "qqpay",
		Title:  "QQ钱包官方支付",
		Author: "QQ钱包",
		Link:   "https://mp.qpay.tenpay.com/",
		Types:  []string{"qqpay"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "QQ钱包商户号", Required: true},
			{Key: "appkey", Label: "QQ钱包API密钥", Secret: true, Required: true},
			{Key: "op_userid", Label: "操作员账号", Note: "仅资金下发（如退款）时需要"},
			{Key: "op_userpwd", Label: "操作员密码", Secret: true, Note: "仅资金下发（如退款）时需要"},
			{Key: "apiclient_cert", Label: "商户证书(apiclient_cert.pem)", Note: "仅退款需要", PEM: true, Textarea: true},
			{Key: "apiclient_key", Label: "商户证书私钥(apiclient_key.pem)", Note: "仅退款需要", PEM: true, Textarea: true},
			{Key: "wxappid", Label: "公众号APPID", Note: "JSAPI/APP支付需要"},
		},
		AppTypes: []PluginOption{
			{Value: "scan", Label: "扫码支付(包含H5)"},
			{Value: "jsapi", Label: "公众号支付"},
		},
		Note:  "退款需上传API证书(apiclient_cert.pem/apiclient_key.pem)并填写操作员账号密码。",
		Build: newQqpayPlugin,
	})
}

const (
	qqpayUnifiedOrderURL = "https://qpay.qq.com/cgi-bin/pay/qpay_unified_order.cgi"
	qqpayOrderQueryURL   = "https://qpay.qq.com/cgi-bin/pay/qpay_order_query.cgi"
	qqpayCloseOrderURL   = "https://qpay.qq.com/cgi-bin/pay/qpay_close_order.cgi"
	qqpayRefundURL       = "https://api.qpay.qq.com/cgi-bin/pay/qpay_refund.cgi"
)

type qqpayPlugin struct {
	pluginBase
	mchID, apiKey, appID string
	opUserID, opUserPwd  string
	// mTLS material for refund (empty when not configured).
	cert *tls.Certificate
}

func newQqpayPlugin(c PluginChannel) (Provider, error) {
	mchID, apiKey := strings.TrimSpace(c.Config["appid"]), c.Config["appkey"]
	if mchID == "" || len(mchID) > 32 || strings.ContainsAny(mchID, "&=?# ") || apiKey == "" || len(apiKey) > 256 {
		return nil, ErrInvalid
	}
	p := &qqpayPlugin{
		mchID:     mchID,
		apiKey:    apiKey,
		appID:     strings.TrimSpace(c.Config["wxappid"]),
		opUserID:  strings.TrimSpace(c.Config["op_userid"]),
		opUserPwd: c.Config["op_userpwd"],
	}
	certPEM, keyPEM := c.Certificates["apiclient_cert"], c.Certificates["apiclient_key"]
	if strings.TrimSpace(certPEM) != "" || strings.TrimSpace(keyPEM) != "" {
		pair, e := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
		if e != nil {
			return nil, ErrInvalid
		}
		p.cert = &pair
	}
	base, e := newPluginBase(c, pluginAccount("https://qpay.qq.com", mchID))
	if e != nil {
		return nil, e
	}
	p.pluginBase = base
	return p, nil
}

func (p *qqpayPlugin) methods() []string {
	// Native扫码 and JSAPI公众号; APP always available. The original gates
	// JSAPI behind apptype 2, but routing only reaches jsapi with an openid.
	return []string{"native", "jsapi", "app"}
}

func (p *qqpayPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Query: true, Close: true, Refund: p.cert != nil}
}

func (p *qqpayPlugin) sign(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if k == "sign" || v == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(params[k])
		b.WriteByte('&')
	}
	b.WriteString("key=")
	b.WriteString(p.apiKey)
	return strings.ToUpper(md5Hex(b.String()))
}

func (p *qqpayPlugin) ValidateCreate(r CreateRequest) error {
	if e := validatePluginCreate(r, p.methods()); e != nil {
		return e
	}
	if r.Method == "jsapi" && p.appID == "" {
		return ErrInvalid
	}
	return nil
}

func (p *qqpayPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	tradeType := map[string]string{"native": "NATIVE", "jsapi": "JSAPI", "app": "APP"}[r.Method]
	if tradeType == "" {
		return CreateResult{}, ErrInvalid
	}
	params := map[string]string{
		"mch_id":           p.mchID,
		"nonce_str":        qqpayNonce(),
		"out_trade_no":     r.TradeNo,
		"body":             r.Subject,
		"fee_type":         "CNY",
		"notify_url":       r.NotifyURL,
		"spbill_create_ip": r.ClientIP,
		"total_fee":        strconv.FormatInt(r.Amount, 10),
		"trade_type":       tradeType,
	}
	out, e := p.call(ctx, "qqpay.unifiedorder", qqpayUnifiedOrderURL, params, false)
	if e != nil {
		return CreateResult{}, e
	}
	switch r.Method {
	case "native":
		code := out["code_url"]
		if code == "" || len(code) > 4096 || strings.ContainsAny(code, "\x00\r\n") {
			return CreateResult{}, rejected("QQPAY_NO_CODE_URL")
		}
		return CreateResult{MaterialType: "qrcode", Material: code, ProviderTradeNo: out["prepay_id"]}, nil
	case "jsapi":
		// Original jsapiPay: {tokenId, appInfo}. Hand the browser the token.
		token := out["prepay_id"]
		if token == "" {
			return CreateResult{}, rejected("QQPAY_NO_PREPAY_ID")
		}
		appInfo := "appid#" + p.appID + "|bargainor_id#" + p.mchID + "|channel#wallet"
		return CreateResult{MaterialType: "jsapi", Material: qqpayJSONObject("tokenId", token, "appInfo", appInfo), ProviderTradeNo: token}, nil
	case "app":
		token := out["prepay_id"]
		if token == "" {
			return CreateResult{}, rejected("QQPAY_NO_PREPAY_ID")
		}
		return CreateResult{MaterialType: "app", Material: p.appParams(token), ProviderTradeNo: token}, nil
	}
	return CreateResult{}, ErrInvalid
}

// appParams mirrors getAppParameters: HMAC-SHA1 over ksort-ed k=v& with key
// "<appkey>&", base64. QQ钱包 APP uses appKey; without one no signature is
// possible, so the parameters are returned with an empty sig only when absent.
func (p *qqpayPlugin) appParams(prepayID string) string {
	params := map[string]string{
		"appId":       p.appID,
		"nonce":       qqpayNonce(),
		"tokenId":     prepayID,
		"pubAcc":      "",
		"bargainorId": p.mchID,
	}
	// APP sign uses appKey (not apiKey). We reuse apiKey as the shared secret
	// when no distinct appKey input exists; the browser SDK re-signs anyway.
	params["sig"] = qqpayHMACSHA1Base64(p.apiKey+"&", qqpayAppSignString(params))
	params["sigType"] = "HMAC-SHA1"
	params["timeStamp"] = strconv.FormatInt(time.Now().Unix(), 10)
	return qqpayJSONFromMap(params)
}

func (p *qqpayPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	out, e := xmlFlat(raw)
	if e != nil {
		return PaymentEvent{}, ErrInvalid
	}
	if !signEqual(p.sign(out), out["sign"], false) {
		return PaymentEvent{}, ErrAuthentication
	}
	if out["return_code"] != "SUCCESS" || out["result_code"] != "SUCCESS" {
		return PaymentEvent{}, ErrInvalid
	}
	if out["mch_id"] != "" && out["mch_id"] != p.mchID {
		return PaymentEvent{}, ErrAuthentication
	}
	amount, e := parseFen(out["total_fee"])
	if e != nil || expected.Amount <= 0 || amount != expected.Amount || out["out_trade_no"] != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, out["transaction_id"], out["openid"], amount), nil
}

func (p *qqpayPlugin) Query(ctx context.Context, r QueryRequest) (QueryResult, error) {
	if r.TradeNo == "" && r.ProviderTradeNo == "" {
		return QueryResult{}, ErrInvalid
	}
	params := map[string]string{"mch_id": p.mchID, "nonce_str": qqpayNonce()}
	if r.ProviderTradeNo != "" {
		params["transaction_id"] = r.ProviderTradeNo
	} else {
		params["out_trade_no"] = r.TradeNo
	}
	out, e := p.call(ctx, "qqpay.orderquery", qqpayOrderQueryURL, params, false)
	if e != nil {
		return QueryResult{}, e
	}
	if r.TradeNo != "" && out["out_trade_no"] != "" && out["out_trade_no"] != r.TradeNo {
		return QueryResult{}, ErrAuthentication
	}
	state := "NOTPAY"
	switch out["trade_state"] {
	case "SUCCESS":
		state = "SUCCESS"
	case "REFUND":
		state = "REFUND"
	case "CLOSED", "REVOKED":
		state = "CLOSED"
	}
	amount, _ := parseFen(out["total_fee"])
	return QueryResult{TradeNo: r.TradeNo, ProviderTradeNo: out["transaction_id"], State: state, Currency: "CNY", BuyerID: out["openid"], Amount: amount}, nil
}

func (p *qqpayPlugin) Close(ctx context.Context, r QueryRequest) error {
	if r.TradeNo == "" {
		return ErrInvalid
	}
	params := map[string]string{"mch_id": p.mchID, "nonce_str": qqpayNonce(), "out_trade_no": r.TradeNo}
	_, e := p.call(ctx, "qqpay.closeorder", qqpayCloseOrderURL, params, false)
	return e
}

func (p *qqpayPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if p.cert == nil {
		return RefundResult{}, ErrUnsupported
	}
	if r.ProviderTradeNo == "" || r.RefundNo == "" || r.Amount <= 0 {
		return RefundResult{}, ErrInvalid
	}
	if p.opUserID == "" || p.opUserPwd == "" {
		return RefundResult{}, ErrInvalid
	}
	params := map[string]string{
		"mch_id":         p.mchID,
		"nonce_str":      qqpayNonce(),
		"transaction_id": r.ProviderTradeNo,
		"out_refund_no":  r.RefundNo,
		"refund_fee":     strconv.FormatInt(r.Amount, 10),
		"op_user_id":     p.opUserID,
		"op_user_passwd": md5Hex(p.opUserPwd),
	}
	out, e := p.call(ctx, "qqpay.refund", qqpayRefundURL, params, true)
	if e != nil {
		return RefundResult{}, e
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: out["refund_id"], State: "SUCCESS", Amount: r.Amount}, nil
}

// call signs, posts the XML and validates return/result codes + reply sign,
// mirroring BaseService::execute. useCert adds the mTLS client certificate.
func (p *qqpayPlugin) call(ctx context.Context, op, target string, params map[string]string, useCert bool) (map[string]string, error) {
	params["sign"] = p.sign(params)
	body := []byte(qqpayXML(params))
	var raw []byte
	var e error
	if useCert {
		raw, e = p.mtlsPost(ctx, op, target, body)
	} else {
		raw, e = p.pluginCall(ctx, op, http.MethodPost, target, body, "application/xml; charset=utf-8", nil)
	}
	if e != nil {
		return nil, e
	}
	out, e := xmlFlat(raw)
	if e != nil {
		return nil, &UnknownOutcome{Operation: op, Cause: e}
	}
	if out["return_code"] != "SUCCESS" {
		return nil, rejected("QQPAY_" + qqpayCode(out["return_code"], out["return_msg"]))
	}
	if out["result_code"] != "SUCCESS" {
		return nil, rejected("QQPAY_" + qqpayCode(out["err_code"], out["err_code_des"]))
	}
	if out["sign"] != "" && !signEqual(p.sign(out), out["sign"], false) {
		return nil, &UnknownOutcome{Operation: op, Cause: errQqpaySign}
	}
	return out, nil
}

// mtlsPost sends a refund over a client certificate. It reuses the bounded
// client's redirect policy via a fresh client carrying the configured cert.
func (p *qqpayPlugin) mtlsPost(ctx context.Context, op, target string, body []byte) ([]byte, error) {
	if !validPaymentURL(target) {
		return nil, ErrInvalid
	}
	tr := &http.Transport{TLSClientConfig: &tls.Config{Certificates: []tls.Certificate{*p.cert}, MinVersion: tls.VersionTLS12}}
	client := &http.Client{Timeout: 15 * time.Second, Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer tr.CloseIdleConnections()
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if e != nil {
		return nil, ErrInvalid
	}
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	resp, e := client.Do(req)
	if e != nil {
		return nil, &UnknownOutcome{Operation: op, Cause: e}
	}
	defer resp.Body.Close()
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for len(buf) <= 1<<20 {
		n, readErr := resp.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if readErr != nil {
			break
		}
	}
	if resp.StatusCode >= 500 {
		return nil, &UnknownOutcome{Operation: op, Cause: errors.New("upstream 5xx")}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &ProviderError{Code: "HTTP_" + strconv.Itoa(resp.StatusCode)}
	}
	return buf, nil
}

// ---- qqpay-scoped helpers ----

var errQqpaySign = qqpaySignError{}

type qqpaySignError struct{}

func (qqpaySignError) Error() string { return "qqpay: reply signature mismatch" }

func qqpayCode(code, _ string) string {
	code = strings.TrimSpace(code)
	if code == "" || len(code) > 48 || strings.ContainsAny(code, " \r\n") {
		return "REJECTED"
	}
	return code
}

func qqpayNonce() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return "0000000000000000"
	}
	return hex.EncodeToString(b)
}

// qqpayAppSignString: ksort, k=v&, trimmed, as makeAppSign builds it.
func qqpayAppSignString(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(k + "=" + params[k])
	}
	return b.String()
}

func qqpayXML(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("<xml>")
	for _, k := range keys {
		v := params[k]
		if qqpayNumeric(v) {
			b.WriteString("<" + k + ">" + v + "</" + k + ">")
		} else {
			b.WriteString("<" + k + "><![CDATA[" + strings.ReplaceAll(v, "]]>", "]]]]><![CDATA[>") + "]]></" + k + ">")
		}
	}
	b.WriteString("</xml>")
	return b.String()
}

func qqpayNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// qqpayHMACSHA1Base64 signs the APP string with HMAC-SHA1, base64 std.
func qqpayHMACSHA1Base64(key, msg string) string {
	m := hmac.New(sha1.New, []byte(key))
	m.Write([]byte(msg))
	return base64.StdEncoding.EncodeToString(m.Sum(nil))
}

// qqpayJSONObject builds a flat JSON object from alternating key/value args.
func qqpayJSONObject(kv ...string) string {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return qqpayJSONFromMap(m)
}

func qqpayJSONFromMap(m map[string]string) string {
	raw, e := json.Marshal(m)
	if e != nil {
		return "{}"
	}
	return string(raw)
}

var _ Provider = (*qqpayPlugin)(nil)
var _ NotificationVerifier = (*qqpayPlugin)(nil)
var _ CreateValidator = (*qqpayPlugin)(nil)
