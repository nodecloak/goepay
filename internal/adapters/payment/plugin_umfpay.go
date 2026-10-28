package payment

// Port of epay_release/plugins/umfpay (联动优势). Create calls
// http://pay.soopay.net/spay/pay/payservice.do with service
// active_scancode_order_new; the upstream answers an HTML page carrying a
// <META name="MobilePayPlatform" content="k=v&k=v…"> string whose bank_payurl
// field (base64) is the QR content. Callbacks are GET, RSA (SHA1) signed over
// the sorted parameters with the platform public key; the plugin answers with
// the same META HTML carrying a signed ret_code=0000 string.
//
// Config model: the original reads cert/cert.pem (platform public key) and
// cert/key.pem (merchant private key) from disk. GOEPAY passes both as PEM
// inputs so Build stays offline and file-free. cert.pem may be a PKIX public
// key or an X.509 certificate.

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const umfpayGateway = "http://pay.soopay.net/spay/pay/payservice.do"

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "umfpay",
		Title:  "联动优势",
		Author: "联动优势",
		Link:   "https://xy.umfintech.com/",
		Types:  []string{"alipay", "wxpay", "bank"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "商户编号", Required: true},
			{Key: "appkey", Label: "商户密钥", Note: "此项随便填写", Secret: true},
			{Key: "cert_pem", Label: "平台公钥cert.pem", Note: "联动优势平台公钥证书内容", PEM: true, Required: true},
			{Key: "key_pem", Label: "商户私钥key.pem", Note: "商户私钥内容", PEM: true, Required: true},
		},
		Note:  "将平台公钥cert.pem和商户私钥key.pem的内容分别填入对应输入框",
		Build: newUmfpayPlugin,
	})
}

type umfpayPlugin struct {
	pluginBase
	merID string
	priv  *rsa.PrivateKey
	pub   *rsa.PublicKey
}

func newUmfpayPlugin(c PluginChannel) (Provider, error) {
	merID := strings.TrimSpace(c.Config["appid"])
	if merID == "" || len(merID) > 32 || strings.ContainsAny(merID, "&=?# ") {
		return nil, ErrInvalid
	}
	priv, e := pluginPrivateKey(c.Certificates["key_pem"])
	if e != nil {
		return nil, ErrInvalid
	}
	pub, e := umfpayParsePublic(c.Certificates["cert_pem"])
	if e != nil {
		return nil, ErrInvalid
	}
	host, _ := gatewayHost(umfpayGateway)
	base, e := newPluginBase(c, pluginAccount(host, merID))
	if e != nil {
		return nil, e
	}
	return &umfpayPlugin{pluginBase: base, merID: merID, priv: priv, pub: pub}, nil
}

// umfpayParsePublic accepts a PKIX public key PEM or an X.509 certificate PEM
// (the original stores a .pem cert) and returns the RSA public key inside.
func umfpayParsePublic(text string) (*rsa.PublicKey, error) {
	t := strings.TrimSpace(text)
	if strings.Contains(t, "BEGIN CERTIFICATE") {
		block, _ := pem.Decode([]byte(t))
		if block == nil {
			return nil, ErrInvalid
		}
		cert, e := x509.ParseCertificate(block.Bytes)
		if e != nil {
			return nil, ErrInvalid
		}
		key, ok := cert.PublicKey.(*rsa.PublicKey)
		if !ok || key.N.BitLen() < pluginMinRSABits {
			return nil, ErrInvalid
		}
		return key, nil
	}
	return pluginPublicKey(t)
}

func (p *umfpayPlugin) scancodeType() (string, bool) {
	switch p.payType {
	case "alipay":
		return "ALIPAY", true
	case "wxpay":
		return "WECHAT", true
	case "bank":
		return "UNION", true
	}
	return "", false
}

// Every served pay type is a QR/scan product.
func (p *umfpayPlugin) methods() []string { return []string{"native"} }

func (p *umfpayPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

// umfpaySignContent reproduces UmfService::getSignContent: ksort, skip
// sign/sign_type and empty values, join "k=v" with "&".
func umfpaySignContent(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if k == "sign" || k == "sign_type" || v == "" {
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

func (p *umfpayPlugin) signRSA(content string) (string, error) {
	return rsaSign(p.priv, crypto.SHA1, content)
}

func (p *umfpayPlugin) verifyRSA(content, signature string) bool {
	return rsaVerify(p.pub, crypto.SHA1, content, signature)
}

func (p *umfpayPlugin) ValidateCreate(r CreateRequest) error {
	return validatePluginCreate(r, p.methods())
}

func (p *umfpayPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	scan, ok := p.scancodeType()
	if !ok {
		return CreateResult{}, ErrUnsupported
	}
	ip := r.ClientIP
	params := map[string]string{
		"charset":       "UTF-8",
		"sign_type":     "RSA",
		"res_format":    "HTML",
		"version":       "4.0",
		"amt_type":      "RMB",
		"mer_id":        p.merID,
		"service":       "active_scancode_order_new",
		"notify_url":    r.NotifyURL,
		"goods_inf":     r.Subject,
		"order_id":      r.TradeNo,
		"mer_date":      umfpayMerDate(r.TradeNo),
		"amount":        strconv.FormatInt(r.Amount, 10),
		"user_ip":       ip,
		"scancode_type": scan,
		"mer_flag":      "KMER",
		"consumer_id":   strings.ReplaceAll(ip, ".", ""),
	}
	sig, e := p.signRSA(umfpaySignContent(params))
	if e != nil {
		return CreateResult{}, e
	}
	params["sign"] = sig
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	raw, e := p.postForm(ctx, "umfpay.scancode", umfpayGateway, form)
	if e != nil {
		return CreateResult{}, e
	}
	reply, e := umfpayParseMeta(raw)
	if e != nil {
		return CreateResult{}, &UnknownOutcome{Operation: "umfpay.scancode", Cause: e}
	}
	if reply["ret_code"] != "0000" {
		if code := reply["ret_code"]; code != "" {
			return CreateResult{}, rejected("UMF_" + code)
		}
		return CreateResult{}, &UnknownOutcome{Operation: "umfpay.scancode", Cause: errUmfpayReply}
	}
	qr, e := base64.StdEncoding.DecodeString(reply["bank_payurl"])
	if e != nil || len(qr) == 0 {
		return CreateResult{}, &UnknownOutcome{Operation: "umfpay.scancode", Cause: errUmfpayReply}
	}
	res, ok := materialFromLinks("", string(qr), "")
	if !ok {
		return CreateResult{}, rejected("UMF_NO_QR")
	}
	res.ProviderTradeNo = reply["trade_no"]
	return res, nil
}

func (p *umfpayPlugin) NotificationMethods() []string {
	return []string{http.MethodGet, http.MethodPost}
}

func (p *umfpayPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	params, e := callbackParams(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	if params["sign"] == "" || !p.verifyRSA(umfpaySignContent(params), params["sign"]) {
		return PaymentEvent{}, ErrAuthentication
	}
	if params["trade_state"] != "TRADE_SUCCESS" {
		return PaymentEvent{}, ErrInvalid
	}
	if params["order_id"] != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	amount, e := parseFen(params["amount"])
	if e != nil || expected.Amount <= 0 || amount != expected.Amount {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, params["trade_no"], params["mer_cust_id"], amount), nil
}

// NotificationAck answers with the META-tagged, signed response string the
// upstream expects (responseUMFstr). A failed verification still answers 200
// with ret_code 0001, as the original does.
func (p *umfpayPlugin) NotificationAck(err error) (int, string, string) {
	resp := map[string]string{
		"sign_type": "RSA",
		"version":   "4.0",
		"mer_id":    p.merID,
		"ret_code":  "0000",
		"ret_msg":   "success",
	}
	if err != nil {
		resp["ret_code"] = "0001"
		resp["ret_msg"] = "verify fail"
	}
	sig, e := p.signRSA(umfpaySignContent(resp))
	if e != nil {
		return http.StatusInternalServerError, "text/html; charset=utf-8", ""
	}
	resp["sign"] = sig
	order := []string{"sign_type", "version", "mer_id", "ret_code", "ret_msg", "sign"}
	var sb strings.Builder
	for i, k := range order {
		if i > 0 {
			sb.WriteByte('&')
		}
		sb.WriteString(k + "=" + resp[k])
	}
	html := `<!DOCTYPE HTML PUBLIC "-//W3C//DTD HTML 4.01 Transitional//EN"><html><head><META NAME="MobilePayPlatform" CONTENT="` + umfpayEscapeMeta(sb.String()) + `"></head><body></body></html>`
	return http.StatusOK, "text/html; charset=utf-8", html
}

// Refund posts service mer_refund; ret_code 0000 is success.
func (p *umfpayPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	params := map[string]string{
		"charset":       "UTF-8",
		"sign_type":     "RSA",
		"res_format":    "HTML",
		"version":       "4.0",
		"amt_type":      "RMB",
		"mer_id":        p.merID,
		"service":       "mer_refund",
		"refund_no":     r.RefundNo,
		"order_id":      r.TradeNo,
		"mer_date":      umfpayMerDate(r.TradeNo),
		"org_amount":    strconv.FormatInt(r.TotalAmount, 10),
		"refund_amount": strconv.FormatInt(r.Amount, 10),
	}
	sig, e := p.signRSA(umfpaySignContent(params))
	if e != nil {
		return RefundResult{}, e
	}
	params["sign"] = sig
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	raw, e := p.postForm(ctx, "umfpay.refund", umfpayGateway, form)
	if e != nil {
		return RefundResult{}, e
	}
	reply, e := umfpayParseMeta(raw)
	if e != nil {
		return RefundResult{}, &UnknownOutcome{Operation: "umfpay.refund", Cause: e}
	}
	if reply["ret_code"] != "0000" {
		if code := reply["ret_code"]; code != "" {
			return RefundResult{}, rejected("UMF_" + code)
		}
		return RefundResult{}, &UnknownOutcome{Operation: "umfpay.refund", Cause: errUmfpayReply}
	}
	amount := r.Amount
	if f, e := parseFen(reply["refund_amt"]); e == nil {
		amount = f
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: reply["order_id"], State: "SUCCESS", Amount: amount}, nil
}

// umfpayMerDate reproduces date("Ymd") for create and substr(trade_no,0,8) for
// refund: our TRADE_NO is yyyymmdd-prefixed, so its first 8 digits are the date.
func umfpayMerDate(tradeNo string) string {
	if len(tradeNo) >= 8 && umfpayDigits.MatchString(tradeNo[:8]) {
		return tradeNo[:8]
	}
	return ""
}

var umfpayDigits = regexp.MustCompile(`^[0-9]{8}$`)

// umfpayMetaRE extracts the content of the MobilePayPlatform META tag.
var umfpayMetaRE = regexp.MustCompile(`(?is)<META\s+name="MobilePayPlatform"\s+content="([^"]*)"`)

type umfpayReplyError string

func (e umfpayReplyError) Error() string { return string(e) }

var errUmfpayReply = umfpayReplyError("umfpay: unreadable upstream reply")

// umfpayParseMeta mirrors parseHTMLStr: pull the META content, split on "&",
// then "k=v". The create reply signature is not re-verified (the original
// comments out verifySign there), but the business ret_code is honoured.
func umfpayParseMeta(raw []byte) (map[string]string, error) {
	m := umfpayMetaRE.FindSubmatch(raw)
	if m == nil || len(m[1]) == 0 {
		return nil, errUmfpayReply
	}
	out := map[string]string{}
	for _, kv := range strings.Split(string(m[1]), "&") {
		i := strings.IndexByte(kv, '=')
		if i < 0 {
			out[kv] = ""
			continue
		}
		out[kv[:i]] = kv[i+1:]
	}
	return out, nil
}

func umfpayEscapeMeta(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	return strings.ReplaceAll(s, ">", "&gt;")
}

var _ Provider = (*umfpayPlugin)(nil)
var _ NotificationVerifier = (*umfpayPlugin)(nil)
var _ CreateValidator = (*umfpayPlugin)(nil)
