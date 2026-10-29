package payment

// Port of epay_release/plugins/xsy (新生易 / 海航聚合). Requests POST a JSON
// envelope {reqId, orgNo, reqData:{…}, signType:"RSA", timestamp, version:"1.0",
// sign} to gateway-hpx.hnapay.com/order<path> (or the test host when
// appswitch=1). The signature is base64(RSA-SHA1) over the ksort'd envelope
// with reqData collapsed to its compact JSON string. Create uses /trade/
// activeScan (QR) and /trade/jsapiScan (JSAPI). Callbacks are a JSON POST whose
// respData object is verified with the platform public key.

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// xsyNowMillis is the millisecond wall clock used for the request timestamp.
func xsyNowMillis() int64 { return time.Now().UnixMilli() }

// xsyReqID reproduces random(60): a 60-char alphanumeric request id.
func xsyReqID() string {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	b := make([]byte, 60)
	if _, e := rand.Read(b); e != nil {
		// Fall back to a fixed-length deterministic id; collision risk is
		// irrelevant because the value is echoed back, not a security token.
		for i := range b {
			b[i] = alphabet[i%len(alphabet)]
		}
		return string(b)
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "xsy",
		Title:  "新生易",
		Author: "新生易",
		Link:   "https://www.hnapay.com/",
		Types:  []string{"wxpay", "alipay", "bank"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "机构代码", Required: true},
			{Key: "appkey", Label: "平台公钥", Textarea: true, Required: true},
			{Key: "appsecret", Label: "商户私钥", Textarea: true, Secret: true, Required: true},
			{Key: "appmchid", Label: "商户编号", Required: true},
			{Key: "appswitch", Label: "环境选择", Options: []PluginOption{{Value: "0", Label: "生产环境"}, {Value: "1", Label: "测试环境"}}},
		},
		Build: newXsyPlugin,
	})
}

type xsyPlugin struct {
	pluginBase
	orgNo, mchNo string
	priv         *rsa.PrivateKey
	pub          *rsa.PublicKey
	gateway      string
}

const (
	xsyGatewayProd = "https://gateway-hpx.hnapay.com/order"
	xsyGatewayTest = "https://gateway-hpxtest1.hnapay.com/order"
)

func newXsyPlugin(c PluginChannel) (Provider, error) {
	orgNo := strings.TrimSpace(c.Config["appid"])
	mchNo := strings.TrimSpace(c.Config["appmchid"])
	if orgNo == "" || len(orgNo) > 32 || mchNo == "" || len(mchNo) > 32 {
		return nil, ErrInvalid
	}
	priv, e := pluginPrivateKey(xsyWrapKey(c.Config["appsecret"], "RSA PRIVATE KEY"))
	if e != nil {
		return nil, ErrInvalid
	}
	pub, e := pluginPublicKey(xsyWrapKey(c.Config["appkey"], "PUBLIC KEY"))
	if e != nil {
		return nil, ErrInvalid
	}
	gw := xsyGatewayProd
	if c.Config["appswitch"] == "1" {
		gw = xsyGatewayTest
	}
	host, _ := gatewayHost(gw)
	base, e := newPluginBase(c, pluginAccount(host, orgNo, mchNo))
	if e != nil {
		return nil, e
	}
	return &xsyPlugin{pluginBase: base, orgNo: orgNo, mchNo: mchNo, priv: priv, pub: pub, gateway: gw}, nil
}

// xsyWrapKey mirrors the SDK: the config stores a bare base64 key body, wrapped
// into PEM at use. A key that is already PEM passes through.
func xsyWrapKey(body, label string) string {
	body = strings.TrimSpace(body)
	if strings.Contains(body, "-----BEGIN") {
		return body
	}
	body = strings.Join(strings.Fields(body), "")
	var sb strings.Builder
	sb.WriteString("-----BEGIN " + label + "-----\n")
	for i := 0; i < len(body); i += 64 {
		end := i + 64
		if end > len(body) {
			end = len(body)
		}
		sb.WriteString(body[i:end] + "\n")
	}
	sb.WriteString("-----END " + label + "-----")
	return sb.String()
}

func (p *xsyPlugin) payTypeCode() (string, bool) {
	switch p.payType {
	case "alipay":
		return "ALIPAY", true
	case "wxpay":
		return "WECHAT", true
	case "bank":
		return "UNIONPAY", true
	}
	return "", false
}

func (p *xsyPlugin) methods() []string {
	// activeScan QR for all; jsapiScan when an openid/buyer id is supplied.
	return []string{"native", "jsapi"}
}

func (p *xsyPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

// xsyMarshal encodes without HTML escaping, matching PHP's
// JSON_UNESCAPED_UNICODE|JSON_UNESCAPED_SLASHES.
func xsyMarshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if e := enc.Encode(v); e != nil {
		return nil, e
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// xsySignContent reproduces getSignContent for the request envelope: reqData is
// its compact JSON string, then ksort all keys, skip sign/empty/null, "k=v&".
func xsySignContent(envelope map[string]string) string {
	keys := make([]string, 0, len(envelope))
	for k, v := range envelope {
		if k == "sign" || v == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k + "=" + envelope[k] + "&")
	}
	return strings.TrimSuffix(sb.String(), "&")
}

func (p *xsyPlugin) signRequest(envelope map[string]string) (string, error) {
	return rsaSign(p.priv, crypto.SHA1, xsySignContent(envelope))
}

func (p *xsyPlugin) ValidateCreate(r CreateRequest) error {
	if e := validatePluginCreate(r, p.methods()); e != nil {
		return e
	}
	if r.Method == "jsapi" && strings.TrimSpace(r.OpenID) == "" {
		return ErrInvalid
	}
	return nil
}

// xsyRequest signs and sends one JSON envelope; returns the respData object and
// the business code. reqData field order is fixed by the caller's ordered slice.
func (p *xsyPlugin) xsyRequest(ctx context.Context, op, path string, reqData []byte) (map[string]any, string, error) {
	envelope := map[string]string{
		"reqId":     xsyReqID(),
		"orgNo":     p.orgNo,
		"reqData":   string(reqData),
		"signType":  "RSA",
		"timestamp": strconv.FormatInt(xsyNowMillis(), 10),
		"version":   "1.0",
	}
	sig, e := p.signRequest(envelope)
	if e != nil {
		return nil, "", e
	}
	envelope["sign"] = sig
	body, e := xsyMarshal(envelope)
	if e != nil {
		return nil, "", ErrInvalid
	}
	raw, e := p.pluginCall(ctx, op, http.MethodPost, p.gateway+path, body, "application/json; charset=utf-8", nil)
	if e != nil {
		return nil, "", e
	}
	out, e := callbackJSON(raw)
	if e != nil {
		return nil, "", &UnknownOutcome{Operation: op, Cause: e}
	}
	code := jsonText(out, "code")
	if code != "0000" && code != "0001" {
		if code != "" {
			return nil, code, rejected("XSY_" + code)
		}
		return nil, "", &UnknownOutcome{Operation: op, Cause: errXsyReply}
	}
	resp, _ := out["respData"].(map[string]any)
	if resp == nil {
		return nil, code, &UnknownOutcome{Operation: op, Cause: errXsyReply}
	}
	return resp, code, nil
}

func (p *xsyPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	payType, ok := p.payTypeCode()
	if !ok {
		return CreateResult{}, ErrUnsupported
	}
	if r.Method == "jsapi" {
		// jsapiScan: payWay 02 (public account); subAppId only for WeChat.
		reqData, _ := xsyMarshal(&xsyJSAPIReq{
			MerchantNo: p.mchNo, OrderNo: r.TradeNo, Amt: r.Amount, PayType: payType,
			PayWay: "02", UserID: r.OpenID, Subject: r.Subject, TrmIP: r.ClientIP,
			CustomerIP: r.ClientIP, NotifyURL: r.NotifyURL,
		})
		resp, _, e := p.xsyRequest(ctx, "xsy.jsapiScan", "/trade/jsapiScan", reqData)
		if e != nil {
			return CreateResult{}, e
		}
		// WeChat/Alipay return a JSAPI parameter string in "source";
		// UnionPay returns a redirectUrl.
		if src := jsonText(resp, "source"); src != "" {
			return CreateResult{MaterialType: "jsapi", Material: src, ProviderTradeNo: jsonText(resp, "outOrderNo")}, nil
		}
		if red := jsonText(resp, "redirectUrl"); red != "" && validPaymentURL(red) {
			return CreateResult{MaterialType: "jump", Material: red, ProviderTradeNo: jsonText(resp, "outOrderNo")}, nil
		}
		return CreateResult{}, rejected("XSY_NO_JSAPI")
	}
	// activeScan: QR content lives in payUrl, possibly wrapped as ...qrContent=X&sign=...
	reqData, _ := xsyMarshal(&xsyScanReq{
		MerchantNo: p.mchNo, OrderNo: r.TradeNo, Amt: r.Amount, PayType: payType,
		Subject: r.Subject, TrmIP: r.ClientIP, CustomerIP: r.ClientIP, NotifyURL: r.NotifyURL,
	})
	resp, _, e := p.xsyRequest(ctx, "xsy.activeScan", "/trade/activeScan", reqData)
	if e != nil {
		return CreateResult{}, e
	}
	payURL := jsonText(resp, "payUrl")
	if i := strings.Index(payURL, "qrContent="); i >= 0 {
		rest := payURL[i+len("qrContent="):]
		if j := strings.Index(rest, "&sign="); j >= 0 {
			rest = rest[:j]
		}
		payURL = rest
	}
	res, ok := materialFromLinks("", payURL, "")
	if !ok {
		return CreateResult{}, rejected("XSY_NO_QR")
	}
	res.ProviderTradeNo = jsonText(resp, "outOrderNo")
	return res, nil
}

// xsyScanReq / xsyJSAPIReq fix the reqData JSON field order so the request
// signature is reproducible (PHP serializes insertion order).
type xsyScanReq struct {
	MerchantNo string `json:"merchantNo"`
	OrderNo    string `json:"orderNo"`
	Amt        int64  `json:"amt"`
	PayType    string `json:"payType"`
	Subject    string `json:"subject"`
	TrmIP      string `json:"trmIp"`
	CustomerIP string `json:"customerIp"`
	NotifyURL  string `json:"notifyUrl"`
}

type xsyJSAPIReq struct {
	MerchantNo string `json:"merchantNo"`
	OrderNo    string `json:"orderNo"`
	Amt        int64  `json:"amt"`
	PayType    string `json:"payType"`
	PayWay     string `json:"payWay"`
	UserID     string `json:"userId"`
	Subject    string `json:"subject"`
	TrmIP      string `json:"trmIp"`
	CustomerIP string `json:"customerIp"`
	NotifyURL  string `json:"notifyUrl"`
}

// VerifyNotification follows notify(): parse the JSON body, verify respData
// with the platform public key over the ksort signContent, then match our
// order. The upstream notify has no amount field, so amount is not re-checked
// here (the original does not either); the trade number binds the event.
func (p *xsyPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	top, e := callbackJSON(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	sign := jsonText(top, "sign")
	if sign == "" {
		return PaymentEvent{}, ErrAuthentication
	}
	// Reconstruct the signed string: the SDK takes the raw substring between
	// "respData": and ,"sign". Reproduce it from the parsed object with the
	// same compact, unescaped encoding, then ksort the envelope.
	envelope := map[string]string{}
	for k, v := range top {
		if k == "sign" {
			continue
		}
		switch k {
		case "respData":
			if obj, ok := v.(map[string]any); ok {
				// Drop empty/null fields to match getSignContent's inner pass.
				cleaned := map[string]any{}
				for kk, vv := range obj {
					if vv == nil {
						continue
					}
					if s, isStr := vv.(string); isStr && s == "" {
						continue
					}
					cleaned[kk] = vv
				}
				enc, _ := xsyMarshal(cleaned)
				envelope[k] = string(enc)
			} else {
				envelope[k] = jsonText(top, k)
			}
		default:
			envelope[k] = jsonText(top, k)
		}
	}
	if !rsaVerify(p.pub, crypto.SHA1, xsySignContent(envelope), sign) {
		return PaymentEvent{}, ErrAuthentication
	}
	resp, _ := top["respData"].(map[string]any)
	if resp == nil {
		return PaymentEvent{}, ErrInvalid
	}
	if st := jsonText(resp, "tranSts"); st != "" && st != "SUCCESS" {
		return PaymentEvent{}, ErrInvalid
	}
	if jsonText(resp, "orderNo") != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	if expected.Amount <= 0 {
		return PaymentEvent{}, ErrAuthentication
	}
	if amtStr := jsonText(resp, "amt"); amtStr != "" {
		if amt, ae := parseFen(amtStr); ae != nil || amt != expected.Amount {
			return PaymentEvent{}, ErrAuthentication
		}
	}
	return paidEvent(expected.TradeNo, jsonText(resp, "outOrderNo"), jsonText(resp, "buyerId"), expected.Amount), nil
}

// NotificationAck answers with the JSON the original echoes.
func (p *xsyPlugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return http.StatusOK, "application/json; charset=utf-8", `{"code":"fail"}`
	}
	return http.StatusOK, "application/json; charset=utf-8", `{"code":"success"}`
}

// Refund posts /trade/refund; code 0000/0001 is accepted.
func (p *xsyPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	reqData, _ := xsyMarshal(&xsyRefundReq{
		MerchantNo: p.mchNo, OrderNo: r.RefundNo, OrigOrderNo: r.TradeNo, Amt: r.Amount,
	})
	resp, _, e := p.xsyRequest(ctx, "xsy.refund", "/trade/refund", reqData)
	if e != nil {
		return RefundResult{}, e
	}
	amount := r.Amount
	if f, fe := parseFen(jsonText(resp, "amt")); fe == nil {
		amount = f
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(resp, "orderNo"), State: "SUCCESS", Amount: amount}, nil
}

type xsyRefundReq struct {
	MerchantNo  string `json:"merchantNo"`
	OrderNo     string `json:"orderNo"`
	OrigOrderNo string `json:"origOrderNo"`
	Amt         int64  `json:"amt"`
}

type xsyReplyError string

func (e xsyReplyError) Error() string { return string(e) }

var errXsyReply = xsyReplyError("xsy: unreadable upstream reply")

var _ Provider = (*xsyPlugin)(nil)
var _ NotificationVerifier = (*xsyPlugin)(nil)
var _ CreateValidator = (*xsyPlugin)(nil)
