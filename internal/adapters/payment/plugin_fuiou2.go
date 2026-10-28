package payment

// Port of epay_release/plugins/fuiou2 (富友支付(合作方)): the 富友 "scan-code"
// gateway (https://spay-mc.fuioupay.com). Requests are a GBK XML document sent
// as POST body "req=" + double-urlencode(xml); the reply is double-urldecoded
// then parsed as flat XML. Both are RSA/MD5 signed over the ksort'ed
// "k=v&..." of the non-empty, non-"reserved", non-"sign" fields: the merchant
// private key signs the request, the 富友 public key verifies the reply and
// the callback. Amounts are integer cents (order_amt). QR products call
// /preCreate (ALIPAY/WECHAT/UNIONPAY -> qr_code); JSAPI calls /wxPreCreate.
// Query is /commonQuery, refund /commonRefund. Callbacks are POST with the XML
// in req=, acked with "1" (ok) / "0" (fail).
//
// Out of scope for this wave: the micropay (被扫/付款码) flow, order revoke,
// and the EntryService 进件/投诉 (complaint/apply) callbacks.

import (
	"context"
	"crypto"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "fuiou2",
		Title:  "富友支付(合作方)",
		Author: "富友",
		Link:   "https://www.fuiou.com/",
		Types:  []string{"alipay", "wxpay", "bank"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "机构号", Required: true},
			{Key: "appmchid", Label: "商户号", Required: true},
			{Key: "appsecret", Label: "商户私钥", Textarea: true, Secret: true, Required: true},
			{Key: "appkey", Label: "富友公钥", Textarea: true, Required: true},
			{Key: "appurl", Label: "订单号前缀"},
			{Key: "appswitch", Label: "环境选择", Options: []PluginOption{{Value: "0", Label: "生产环境"}, {Value: "1", Label: "测试环境"}}},
		},
		// select_alipay / select_wxpay: 1=扫码支付(native), 2=JS支付(jsapi).
		AppTypes: []PluginOption{{Value: "1", Label: "扫码支付"}, {Value: "2", Label: "JS支付/公众号支付"}},
		Build:    newFuiou2Plugin,
	})
}

const (
	fuiou2ProdHost = "https://spay-mc.fuioupay.com"
	fuiou2TestHost = "https://fundwx.fuiou.com"
)

type fuiou2Plugin struct {
	pluginBase
	insCd, mchntCd, prefix string
	host                   string
	priv                   *rsa.PrivateKey
	pub                    *rsa.PublicKey
	appTypes               []string
}

func newFuiou2Plugin(c PluginChannel) (Provider, error) {
	insCd := strings.TrimSpace(c.Config["appid"])
	mchntCd := strings.TrimSpace(c.Config["appmchid"])
	if insCd == "" || len(insCd) > 32 || mchntCd == "" || len(mchntCd) > 32 {
		return nil, ErrInvalid
	}
	prefix := strings.TrimSpace(c.Config["appurl"])
	if len(prefix) > 32 || strings.ContainsAny(prefix, "&=?# \x00") {
		return nil, ErrInvalid
	}
	priv, e := pluginPrivateKey(c.Config["appsecret"])
	if e != nil {
		return nil, ErrInvalid
	}
	pub, e := pluginPublicKey(c.Config["appkey"])
	if e != nil {
		return nil, ErrInvalid
	}
	host := fuiou2ProdHost
	if c.Config["appswitch"] == "1" {
		host = fuiou2TestHost
	}
	base, e := newPluginBase(c, pluginAccount(host, insCd, mchntCd))
	if e != nil {
		return nil, e
	}
	return &fuiou2Plugin{pluginBase: base, insCd: insCd, mchntCd: mchntCd, prefix: prefix, host: host, priv: priv, pub: pub, appTypes: append([]string(nil), c.AppTypes...)}, nil
}

// fuiou2OrderType maps the channel pay type to 富友's order_type.
func (p *fuiou2Plugin) fuiou2OrderType() string {
	switch p.payType {
	case "alipay":
		return "ALIPAY"
	case "wxpay":
		return "WECHAT"
	default:
		return "UNIONPAY"
	}
}

func (p *fuiou2Plugin) fuiou2HasApp(v string) bool {
	if len(p.appTypes) == 0 {
		return true // original default: all products available
	}
	for _, a := range p.appTypes {
		if a == v {
			return true
		}
	}
	return false
}

func (p *fuiou2Plugin) methods() []string {
	var m []string
	if p.fuiou2HasApp("1") {
		m = append(m, "native")
	}
	// JS支付: alipay "jsapi" needs a buyer uid; wxpay needs sub_openid+sub_appid.
	if p.fuiou2HasApp("2") {
		m = append(m, "jsapi")
	}
	if len(m) == 0 {
		m = []string{"native"}
	}
	return m
}

func (p *fuiou2Plugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Query: true, Refund: true}
}

// fuiou2SignContent builds the ksort'ed "k=v&..." base, skipping sign, empties
// and any key beginning with "reserved" (the original getSignContent).
func fuiou2SignContent(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if k == "sign" || v == "" || strings.HasPrefix(k, "reserved") {
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
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(params[k])
	}
	return sb.String()
}

func (p *fuiou2Plugin) fuiou2Sign(params map[string]string) (string, error) {
	return fuiou2RSASignMD5(p.priv, fuiou2SignContent(params))
}

func (p *fuiou2Plugin) fuiou2Verify(params map[string]string) bool {
	return fuiou2RSAVerifyMD5(p.pub, fuiou2SignContent(params), params["sign"])
}

// fuiou2ToGBK re-encodes a UTF-8 string to GBK as the original does per value
// before signing and sending. On encode failure the original leaves bytes;
// we keep the UTF-8 bytes rather than drop the field.
func fuiou2ToGBK(s string) string {
	if s == "" {
		return s
	}
	out, e := simplifiedchinese.GBK.NewEncoder().String(s)
	if e != nil {
		return s
	}
	return out
}

// fuiou2BuildXML signs the GBK-encoded params and renders the request XML.
func (p *fuiou2Plugin) fuiou2BuildXML(params map[string]string) (string, error) {
	gbk := make(map[string]string, len(params))
	for k, v := range params {
		gbk[k] = fuiou2ToGBK(v)
	}
	sign, e := p.fuiou2Sign(gbk)
	if e != nil {
		return "", e
	}
	gbk["sign"] = sign
	keys := make([]string, 0, len(gbk))
	for k := range gbk {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="GBK" standalone="yes"?><xml>`)
	for _, k := range keys {
		sb.WriteString("<" + k + ">" + fuiou2XMLEscape(gbk[k]) + "</" + k + ">")
	}
	sb.WriteString("</xml>")
	return sb.String(), nil
}

func fuiou2XMLEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

// fuiou2Post sends the XML as req=urlencode(urlencode(xml)); the reply is
// urldecode(body) then parsed as flat XML, with GBK values decoded to UTF-8.
func (p *fuiou2Plugin) fuiou2Post(ctx context.Context, operation, path string, params map[string]string) (map[string]string, error) {
	xmlStr, e := p.fuiou2BuildXML(params)
	if e != nil {
		return nil, e
	}
	body := "req=" + url.QueryEscape(url.QueryEscape(xmlStr))
	raw, e := p.pluginCall(ctx, operation, http.MethodPost, p.host+path, []byte(body), "application/x-www-form-urlencoded", nil)
	if e != nil {
		return nil, e
	}
	decoded, e := url.QueryUnescape(string(raw))
	if e != nil {
		decoded = string(raw)
	}
	flat, e := xmlFlat([]byte(decoded))
	if e != nil {
		// Upstream may have acted; the reply is unreadable.
		return nil, &UnknownOutcome{Operation: operation, Cause: e}
	}
	out := make(map[string]string, len(flat))
	for k, v := range flat {
		out[k] = fuiou2FromGBK(v)
	}
	return out, nil
}

func fuiou2FromGBK(s string) string {
	if s == "" {
		return s
	}
	out, e := simplifiedchinese.GBK.NewDecoder().String(s)
	if e != nil {
		return s
	}
	return out
}

// fuiou2CheckReply enforces the original's success contract (result_code
// 000000, or 030010 "processing"), verifies the reply sign, else maps to a
// definitive rejection / unknown outcome.
func (p *fuiou2Plugin) fuiou2CheckReply(out map[string]string, operation string) error {
	code := out["result_code"]
	if code == "000000" || code == "030010" {
		// The sign over GBK values: re-encode before verifying.
		gbk := make(map[string]string, len(out))
		for k, v := range out {
			if k == "sign" {
				gbk[k] = v
				continue
			}
			gbk[k] = fuiou2ToGBK(v)
		}
		if !p.fuiou2Verify(gbk) {
			return ErrAuthentication
		}
		return nil
	}
	if msg := out["result_code"]; msg != "" {
		return rejected("FUIOU2_" + msg)
	}
	return &UnknownOutcome{Operation: operation, Cause: errFuiou2UnexpectedReply}
}

func (p *fuiou2Plugin) ValidateCreate(r CreateRequest) error {
	if e := validatePluginCreate(r, p.methods()); e != nil {
		return e
	}
	if r.Method == "jsapi" && r.OpenID == "" {
		return ErrInvalid
	}
	return nil
}

func (p *fuiou2Plugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	common := map[string]string{
		"version":        "1.0",
		"ins_cd":         p.insCd,
		"mchnt_cd":       p.mchntCd,
		"term_id":        "88888888",
		"random_str":     fuiou2Nonce(r.TradeNo),
		"order_amt":      strconv.FormatInt(r.Amount, 10),
		"mchnt_order_no": p.prefix + r.TradeNo,
		"txn_begin_ts":   fuiou2Timestamp(r),
		"goods_des":      r.Subject,
		"term_ip":        r.ClientIP,
		"curr_type":      "CNY",
		"notify_url":     r.NotifyURL,
	}
	switch r.Method {
	case "native":
		common["order_type"] = p.fuiou2OrderType()
		out, e := p.fuiou2Post(ctx, "fuiou2.preCreate", "/preCreate", common)
		if e != nil {
			return CreateResult{}, e
		}
		if e := p.fuiou2CheckReply(out, "fuiou2.preCreate"); e != nil {
			return CreateResult{}, e
		}
		qr := out["qr_code"]
		if qr == "" || len(qr) > 4096 || strings.ContainsAny(qr, "\x00\r\n") {
			return CreateResult{}, rejected("FUIOU2_NO_QR")
		}
		return CreateResult{MaterialType: "qrcode", Material: qr, ProviderTradeNo: out["transaction_id"]}, nil
	case "jsapi":
		// wxPreCreate: alipay uses trade_type FWC with sub_openid=uid; wxpay
		// JSAPI needs sub_openid + sub_appid in r.OpenID ("appid:openid").
		tradeType := "JSAPI"
		subAppid, subOpenid := "", r.OpenID
		if p.payType == "alipay" {
			tradeType = "FWC"
		} else if i := strings.IndexByte(r.OpenID, ':'); i > 0 {
			subAppid, subOpenid = r.OpenID[:i], r.OpenID[i+1:]
		}
		common["trade_type"] = tradeType
		common["sub_openid"] = subOpenid
		if subAppid != "" {
			common["sub_appid"] = subAppid
		}
		out, e := p.fuiou2Post(ctx, "fuiou2.wxPreCreate", "/wxPreCreate", common)
		if e != nil {
			return CreateResult{}, e
		}
		if e := p.fuiou2CheckReply(out, "fuiou2.wxPreCreate"); e != nil {
			return CreateResult{}, e
		}
		if p.payType == "alipay" {
			tn := out["reserved_transaction_id"]
			if tn == "" {
				return CreateResult{}, rejected("FUIOU2_NO_TRADE_NO")
			}
			return CreateResult{MaterialType: "jsapi", Material: tn, ProviderTradeNo: out["transaction_id"]}, nil
		}
		js := fuiou2WxJSAPI(out)
		if js == "" {
			return CreateResult{}, rejected("FUIOU2_NO_JSAPI")
		}
		return CreateResult{MaterialType: "jsapi", Material: js, ProviderTradeNo: out["transaction_id"]}, nil
	}
	return CreateResult{}, ErrInvalid
}

// fuiou2WxJSAPI assembles the WeChat JSAPI parameter object the browser needs.
func fuiou2WxJSAPI(out map[string]string) string {
	fields := map[string]string{
		"appId":     out["sdk_appid"],
		"timeStamp": out["sdk_timestamp"],
		"nonceStr":  out["sdk_noncestr"],
		"package":   out["sdk_package"],
		"signType":  out["sdk_signtype"],
		"paySign":   out["sdk_paysign"],
	}
	if fields["package"] == "" || fields["paySign"] == "" {
		return ""
	}
	return fuiou2JSONObject(fields)
}

func (p *fuiou2Plugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	form, e := callbackParams(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	reqXML := form["req"]
	if reqXML == "" {
		return PaymentEvent{}, ErrInvalid
	}
	flat, e := xmlFlat([]byte(reqXML))
	if e != nil {
		return PaymentEvent{}, ErrInvalid
	}
	// Values arrive GBK-encoded; verify over GBK, read as UTF-8.
	utf := make(map[string]string, len(flat))
	for k, v := range flat {
		if k == "sign" {
			utf[k] = v
			continue
		}
		utf[k] = fuiou2FromGBK(v)
	}
	if !p.fuiou2Verify(flat) {
		return PaymentEvent{}, ErrAuthentication
	}
	if utf["result_code"] != "000000" {
		return PaymentEvent{}, ErrInvalid
	}
	mch := utf["mchnt_order_no"]
	if !strings.HasPrefix(mch, p.prefix) {
		return PaymentEvent{}, ErrAuthentication
	}
	ourTrade := mch[len(p.prefix):]
	amount, e := parseFen(utf["order_amt"])
	if e != nil || expected.Amount <= 0 || amount != expected.Amount || ourTrade != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, mch, utf["user_id"], amount), nil
}

// The original notify() echoes plain "1" for success and "0" for failure.
func (p *fuiou2Plugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return http.StatusOK, "text/plain; charset=utf-8", "0"
	}
	return http.StatusOK, "text/plain; charset=utf-8", "1"
}

func (p *fuiou2Plugin) Query(ctx context.Context, r QueryRequest) (QueryResult, error) {
	if r.TradeNo == "" {
		return QueryResult{}, ErrInvalid
	}
	params := map[string]string{
		"version":        "1.0",
		"ins_cd":         p.insCd,
		"mchnt_cd":       p.mchntCd,
		"term_id":        "88888888",
		"random_str":     fuiou2Nonce(r.TradeNo),
		"order_type":     p.fuiou2OrderType(),
		"mchnt_order_no": p.prefix + r.TradeNo,
	}
	out, e := p.fuiou2Post(ctx, "fuiou2.commonQuery", "/commonQuery", params)
	if e != nil {
		return QueryResult{}, e
	}
	if e := p.fuiou2CheckReply(out, "fuiou2.commonQuery"); e != nil {
		return QueryResult{}, e
	}
	mch := out["mchnt_order_no"]
	if !strings.HasPrefix(mch, p.prefix) || mch[len(p.prefix):] != r.TradeNo {
		return QueryResult{}, ErrAuthentication
	}
	amount, e := parseFen(out["order_amt"])
	if e != nil {
		return QueryResult{}, ErrAuthentication
	}
	state := "NOTPAY"
	switch out["trans_stat"] {
	case "SUCCESS":
		state = "SUCCESS"
	case "REFUND":
		state = "REFUND"
	case "CLOSED", "REVOKED":
		state = "CLOSED"
	}
	return QueryResult{TradeNo: r.TradeNo, ProviderTradeNo: out["transaction_id"], State: state, Currency: "CNY", BuyerID: out["user_id"], Amount: amount}, nil
}

func (p *fuiou2Plugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.ProviderTradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	params := map[string]string{
		"version":         "1.0",
		"ins_cd":          p.insCd,
		"mchnt_cd":        p.mchntCd,
		"term_id":         "88888888",
		"random_str":      fuiou2Nonce(r.RefundNo),
		"mchnt_order_no":  r.ProviderTradeNo,
		"refund_order_no": r.RefundNo,
		"order_type":      p.fuiou2OrderType(),
		"total_amt":       strconv.FormatInt(r.TotalAmount, 10),
		"refund_amt":      strconv.FormatInt(r.Amount, 10),
	}
	out, e := p.fuiou2Post(ctx, "fuiou2.commonRefund", "/commonRefund", params)
	if e != nil {
		return RefundResult{}, e
	}
	if e := p.fuiou2CheckReply(out, "fuiou2.commonRefund"); e != nil {
		return RefundResult{}, e
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: out["mchnt_order_no"], State: "SUCCESS", Amount: r.Amount}, nil
}

var _ Provider = (*fuiou2Plugin)(nil)
var _ NotificationVerifier = (*fuiou2Plugin)(nil)
var _ CreateValidator = (*fuiou2Plugin)(nil)
var _ NotificationAcker = (*fuiou2Plugin)(nil)

var errFuiou2UnexpectedReply = errors.New("fuiou2: unexpected upstream reply")

// fuiou2Nonce derives a deterministic 32-char lowercase-hex random_str from a
// stable business number (the original uses a per-request getSid()); a
// deterministic value keeps retries idempotent and avoids a global RNG.
func fuiou2Nonce(seed string) string {
	return md5Hex("fuiou2:" + seed)
}

// fuiou2Timestamp is the YmdHis order begin time; it is excluded from identity
// and only labels the request.
func fuiou2Timestamp(r CreateRequest) string {
	t := time.Now().UTC()
	if r.ExpiresAt != nil {
		t = r.ExpiresAt.Add(-30 * time.Minute)
	}
	return t.Format("20060102150405")
}

// fuiou2JSONObject marshals a flat string map to a compact JSON object.
func fuiou2JSONObject(fields map[string]string) string {
	b, e := json.Marshal(fields)
	if e != nil {
		return ""
	}
	return string(b)
}

// fuiou2RSASignMD5 / fuiou2RSAVerifyMD5 are PKCS#1 v1.5 with MD5 — the kit's
// rsaSign/rsaVerify deliberately refuse MD5, but 富友 (openssl_sign with
// OPENSSL_ALGO_MD5) requires it, so the primitive lives here.
func fuiou2RSASignMD5(key *rsa.PrivateKey, message string) (string, error) {
	if key == nil {
		return "", ErrAuthentication
	}
	h := md5.Sum([]byte(message))
	sig, e := rsa.SignPKCS1v15(rand.Reader, key, crypto.MD5, h[:])
	if e != nil {
		return "", e
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

func fuiou2RSAVerifyMD5(key *rsa.PublicKey, message, signature string) bool {
	if key == nil || signature == "" {
		return false
	}
	sig, e := base64.StdEncoding.DecodeString(signature)
	if e != nil {
		sig, e = base64.URLEncoding.DecodeString(signature)
		if e != nil {
			return false
		}
	}
	h := md5.Sum([]byte(message))
	return rsa.VerifyPKCS1v15(key, crypto.MD5, h[:], sig) == nil
}
