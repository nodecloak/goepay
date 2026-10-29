package payment

// Port of epay_release/plugins/huifu (汇付斗拱平台). Requests POST JSON
// {sys_id, product_id, data:{...}, sign} to https://api.huifu.com<path>; the
// sign is base64(RSA-SHA256) over json_encode(ksort(array_filter(data)))
// with JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE, using the merchant
// private key. The reply is verified with the 汇付 public key over the same
// canonical form of the returned data object. Collection products:
//   alipay   A_NATIVE (qr) / A_JSAPI (jsapi, needs buyer id)
//   wxpay    T_NATIVE (qr)  / T_JSAPI (jsapi, needs sub_appid+openid)
//   bank     U_NATIVE (qr)  / U_JSAPI (jump, needs user id)
//   ecny     D_NATIVE (qr)
//   douyinpay Y_H5 (jump)
// all through /v3/trade/payment/jspay. Refund (/v3/trade/payment/scanpay/refund),
// Query (/v3/trade/payment/scanpay/query) and Close
// (/v2/trade/payment/scanpay/close) are implemented. Callbacks POST
// resp_data + sign; the ack is "RECV_ORD_ID_<req_seq_id>". Hosting/combined
// orders (H5/PC jump, mini-program, quickpay, banking, app), profit sharing
// and complaint callbacks are OUT OF SCOPE.

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"math/big"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

const huifuGateway = "https://api.huifu.com"

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "huifu",
		Title:  "汇付斗拱平台",
		Author: "汇付天下",
		Link:   "https://paas.huifu.com/",
		Types:  []string{"alipay", "wxpay", "bank", "ecny", "douyinpay"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "汇付系统号", Note: "当主体为渠道商时填写渠道商ID，主体为直连商户时填写商户ID", Required: true},
			{Key: "appurl", Label: "汇付产品号", Note: "product_id", Required: true},
			{Key: "appsecret", Label: "商户私钥", Secret: true, Textarea: true, Required: true},
			{Key: "appkey", Label: "汇付公钥", Textarea: true, Required: true},
			{Key: "appmchid", Label: "汇付子商户号", Note: "当主体为渠道商时需要填写，主体为直连商户时不需要填写"},
		},
		Build: newHuifuPlugin,
	})
}

type huifuPlugin struct {
	pluginBase
	sysID, productID, huifuID string
	gateway                   string
	privKey                   *rsa.PrivateKey
	pubKey                    *rsa.PublicKey
}

func newHuifuPlugin(c PluginChannel) (Provider, error) {
	sysID := strings.TrimSpace(c.Config["appid"])
	productID := strings.TrimSpace(c.Config["appurl"])
	if sysID == "" || len(sysID) > 64 || productID == "" || len(productID) > 64 {
		return nil, ErrInvalid
	}
	priv, e := pluginPrivateKey(c.Config["appsecret"])
	if e != nil {
		return nil, e
	}
	pub, e := pluginPublicKey(c.Config["appkey"])
	if e != nil {
		return nil, e
	}
	huifuID := strings.TrimSpace(c.Config["appmchid"])
	if huifuID == "" {
		huifuID = sysID
	}
	if len(huifuID) > 64 {
		return nil, ErrInvalid
	}
	base, e := newPluginBase(c, pluginAccount("https://api.huifu.com", sysID, productID, huifuID))
	if e != nil {
		return nil, e
	}
	return &huifuPlugin{pluginBase: base, sysID: sysID, productID: productID, huifuID: huifuID, gateway: huifuGateway, privKey: priv, pubKey: pub}, nil
}

// tradeType maps the GOEPAY (payType, method) to the 斗拱 trade_type.
func (p *huifuPlugin) tradeType(method string) string {
	switch p.payType {
	case "alipay":
		if method == "jsapi" {
			return "A_JSAPI"
		}
		return "A_NATIVE"
	case "wxpay":
		if method == "jsapi" {
			return "T_JSAPI"
		}
		return "T_NATIVE"
	case "bank":
		if method == "jsapi" {
			return "U_JSAPI"
		}
		return "U_NATIVE"
	case "ecny":
		return "D_NATIVE"
	case "douyinpay":
		return "Y_H5"
	}
	return ""
}

func (p *huifuPlugin) methods() []string {
	switch p.payType {
	case "alipay", "wxpay", "bank":
		return []string{"native", "jsapi"}
	case "ecny":
		return []string{"native"}
	case "douyinpay":
		return []string{"h5"}
	}
	return nil
}

func (p *huifuPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Query: true, Close: true, Refund: true}
}

// sign signs the canonical JSON of the data object (base64 RSA-SHA256).
func (p *huifuPlugin) sign(data map[string]string) (string, error) {
	return rsaSign(p.privKey, crypto.SHA256, huifuCanonical(data))
}

// verify checks a base64 RSA-SHA256 signature over the canonical JSON of data.
func (p *huifuPlugin) verify(dataJSON, signature string) bool {
	return rsaVerify(p.pubKey, crypto.SHA256, dataJSON, signature)
}

// huifuCanonical reproduces json_encode(ksort(array_filter($data))) with
// JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE: drop empty values, sort keys,
// emit a compact object. Values are plain strings (nested JSON is pre-encoded).
func huifuCanonical(data map[string]string) string {
	keys := make([]string, 0, len(data))
	for k, v := range data {
		if v == "" { // array_filter drops null; empty string is treated as absent.
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(huifuJSONString(k))
		b.WriteByte(':')
		b.WriteString(huifuJSONString(data[k]))
	}
	b.WriteByte('}')
	return b.String()
}

func huifuJSONString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				const hexdigits = "0123456789abcdef"
				b.WriteString(`\u00`)
				b.WriteByte(hexdigits[r>>4])
				b.WriteByte(hexdigits[r&0xF])
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func (p *huifuPlugin) ValidateCreate(r CreateRequest) error {
	if e := validatePluginCreate(r, p.methods()); e != nil {
		return e
	}
	if p.tradeType(r.Method) == "" {
		return ErrInvalid
	}
	if r.Method == "jsapi" && (r.OpenID == "" || len(r.OpenID) > 128) {
		return ErrInvalid
	}
	return nil
}

func (p *huifuPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	tradeType := p.tradeType(r.Method)
	reqDate := huifuReqDate(r.TradeNo)
	data := map[string]string{
		"req_date":        reqDate,
		"req_seq_id":      r.TradeNo,
		"huifu_id":        p.huifuID,
		"trade_type":      tradeType,
		"trans_amt":       yuan(r.Amount),
		"goods_desc":      r.Subject,
		"notify_url":      r.NotifyURL,
		"risk_check_data": `{"ip_addr":"` + huifuJSONEscape(r.ClientIP) + `"}`,
	}
	switch tradeType {
	case "T_JSAPI":
		data["wx_data"] = `{"sub_openid":"` + huifuJSONEscape(r.OpenID) + `","device_info":"4","spbill_create_ip":"` + huifuJSONEscape(r.ClientIP) + `"}`
	case "A_JSAPI":
		data["alipay_data"] = `{"subject":"` + huifuJSONEscape(r.Subject) + `","buyer_id":"` + huifuJSONEscape(r.OpenID) + `"}`
	case "A_NATIVE":
		data["alipay_data"] = `{"subject":"` + huifuJSONEscape(r.Subject) + `"}`
	case "T_NATIVE":
		data["wx_data"] = `{"product_id":"01001","spbill_create_ip":"` + huifuJSONEscape(r.ClientIP) + `"}`
	case "U_JSAPI":
		data["unionpay_data"] = `{"qr_code":"` + huifuJSONEscape(r.NotifyURL) + `","customer_ip":"` + huifuJSONEscape(r.ClientIP) + `","user_id":"` + huifuJSONEscape(r.OpenID) + `"}`
	case "Y_H5":
		data["dy_data"] = `{"order_ip":"` + huifuJSONEscape(r.ClientIP) + `"}`
	}
	out, e := p.request(ctx, "huifu.jspay", "/v3/trade/payment/jspay", data)
	if e != nil {
		return CreateResult{}, e
	}
	// The create path treats resp_code 00000100 (processing) as success.
	if code := jsonText(out, "resp_code"); code != "00000000" && code != "00000100" {
		return CreateResult{}, rejected("HUIFU_" + firstNonEmpty(code, "ERR"))
	}
	material := firstNonEmpty(jsonText(out, "pay_info"), jsonText(out, "qr_code"))
	if material == "" {
		return CreateResult{}, rejected("HUIFU_NO_PAY_INFO")
	}
	provider := jsonText(out, "hf_seq_id")
	switch r.Method {
	case "jsapi":
		if p.payType == "bank" { // U_JSAPI returns a redirect URL.
			if !validPaymentURL(material) {
				return CreateResult{}, rejected("HUIFU_BAD_URL")
			}
			return CreateResult{MaterialType: "jump", Material: material, ProviderTradeNo: provider}, nil
		}
		if len(material) > 4096 || strings.ContainsRune(material, 0) {
			return CreateResult{}, rejected("HUIFU_BAD_JSAPI")
		}
		return CreateResult{MaterialType: "jsapi", Material: material, ProviderTradeNo: provider}, nil
	case "h5": // douyinpay Y_H5 pay_info is a redirect URL.
		if !validPaymentURL(material) {
			return CreateResult{}, rejected("HUIFU_BAD_URL")
		}
		return CreateResult{MaterialType: "jump", Material: material, ProviderTradeNo: provider}, nil
	default: // native
		if len(material) > 4096 || strings.ContainsAny(material, "\x00\r\n") {
			return CreateResult{}, rejected("HUIFU_BAD_QR")
		}
		return CreateResult{MaterialType: "qrcode", Material: material, ProviderTradeNo: provider}, nil
	}
}

// request builds the signed envelope, posts it, and verifies the reply sign
// over the returned data object before returning it. The reply is verified by
// re-canonicalizing the returned data the way the original's checkResponseSign
// does (ksort + json_encode with unescaped slashes/unicode).
func (p *huifuPlugin) request(ctx context.Context, operation, path string, data map[string]string) (map[string]any, error) {
	sign, e := p.sign(data)
	if e != nil {
		return nil, e
	}
	envelope := map[string]any{
		"sys_id":     p.sysID,
		"product_id": p.productID,
		"data":       json.RawMessage(huifuCanonical(data)),
		"sign":       sign,
	}
	raw, e := p.postJSON(ctx, operation, p.gateway+path, envelope, nil)
	if e != nil {
		return nil, e
	}
	var top struct {
		Data json.RawMessage `json:"data"`
		Sign string          `json:"sign"`
	}
	if json.Unmarshal(raw, &top) != nil || len(top.Data) == 0 || top.Sign == "" {
		return nil, &UnknownOutcome{Operation: operation, Cause: ErrInvalid}
	}
	canon, e := huifuCanonicalRaw(top.Data)
	if e != nil {
		return nil, &UnknownOutcome{Operation: operation, Cause: e}
	}
	if !p.verify(canon, top.Sign) {
		return nil, ErrAuthentication
	}
	data2, e := callbackJSON(top.Data)
	if e != nil {
		return nil, &UnknownOutcome{Operation: operation, Cause: e}
	}
	return data2, nil
}

func (p *huifuPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	form, e := callbackParams(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	respData, sign := form["resp_data"], form["sign"]
	if respData == "" || sign == "" {
		return PaymentEvent{}, ErrInvalid
	}
	if !p.verify(respData, sign) {
		return PaymentEvent{}, ErrAuthentication
	}
	data, e := callbackJSON([]byte(respData))
	if e != nil {
		return PaymentEvent{}, ErrInvalid
	}
	if jsonText(data, "trans_stat") != "S" {
		return PaymentEvent{}, ErrInvalid
	}
	amount, e := parseYuan(jsonText(data, "trans_amt"))
	if e != nil || expected.Amount <= 0 || amount != expected.Amount || jsonText(data, "req_seq_id") != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	buyer := huifuBuyer(data)
	return paidEvent(expected.TradeNo, jsonText(data, "hf_seq_id"), buyer, amount), nil
}

func huifuBuyer(data map[string]any) string {
	if ali, ok := data["alipay_response"].(map[string]any); ok {
		return jsonText(ali, "buyer_id")
	}
	if wx, ok := data["wx_response"].(map[string]any); ok {
		return jsonText(wx, "sub_openid")
	}
	return ""
}

// NotificationResponse answers like the original notify(): a verified paid
// callback gets "RECV_ORD_ID_<order>" (huifu stops retrying on it); anything
// else keeps the plain failure reply. Only a plain order number is echoed.
func (p *huifuPlugin) NotificationResponse(_ http.Header, raw []byte, verifyErr error) (int, string, string) {
	if verifyErr != nil {
		return 0, "", ""
	}
	form, e := callbackParams(raw)
	if e != nil {
		return 0, "", ""
	}
	data, e := callbackJSON([]byte(form["resp_data"]))
	if e != nil {
		return 0, "", ""
	}
	no := jsonText(data, "req_seq_id")
	if no == "" || len(no) > 64 {
		return 0, "", ""
	}
	for _, c := range no {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c == '-') {
			return 0, "", ""
		}
	}
	return 200, "text/plain; charset=utf-8", "RECV_ORD_ID_" + no
}

func (p *huifuPlugin) Query(ctx context.Context, r QueryRequest) (QueryResult, error) {
	if r.TradeNo == "" {
		return QueryResult{}, ErrInvalid
	}
	data := map[string]string{
		"huifu_id":       p.huifuID,
		"org_req_date":   huifuReqDate(r.TradeNo),
		"org_req_seq_id": r.TradeNo,
	}
	out, e := p.request(ctx, "huifu.query", "/v3/trade/payment/scanpay/query", data)
	if e != nil {
		return QueryResult{}, e
	}
	if code := jsonText(out, "resp_code"); code != "00000000" && code != "00000100" {
		return QueryResult{}, rejected("HUIFU_" + firstNonEmpty(code, "ERR"))
	}
	if jsonText(out, "req_seq_id") != "" && jsonText(out, "req_seq_id") != r.TradeNo {
		return QueryResult{}, ErrAuthentication
	}
	amount, _ := parseYuan(jsonText(out, "trans_amt"))
	state := "NOTPAY"
	switch jsonText(out, "trans_stat") {
	case "S":
		state = "SUCCESS"
	case "P":
		state = "NOTPAY"
	case "F":
		state = "CLOSED"
	}
	return QueryResult{TradeNo: r.TradeNo, ProviderTradeNo: jsonText(out, "hf_seq_id"), State: state, Currency: "CNY", BuyerID: huifuBuyer(out), Amount: amount}, nil
}

func (p *huifuPlugin) Close(ctx context.Context, r QueryRequest) error {
	if r.TradeNo == "" {
		return ErrInvalid
	}
	data := map[string]string{
		"req_date":       huifuNowDate(),
		"req_seq_id":     huifuCloseSeq(r.TradeNo),
		"huifu_id":       p.huifuID,
		"org_req_date":   huifuReqDate(r.TradeNo),
		"org_req_seq_id": r.TradeNo,
	}
	out, e := p.request(ctx, "huifu.close", "/v2/trade/payment/scanpay/close", data)
	if e != nil {
		return e
	}
	if code := jsonText(out, "resp_code"); code != "00000000" && code != "00000100" {
		return rejected("HUIFU_" + firstNonEmpty(code, "ERR"))
	}
	return nil
}

func (p *huifuPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	data := map[string]string{
		"req_date":       huifuNowDate(),
		"req_seq_id":     r.RefundNo,
		"huifu_id":       p.huifuID,
		"ord_amt":        yuan(r.Amount),
		"org_req_date":   huifuReqDate(r.TradeNo),
		"org_req_seq_id": r.TradeNo,
	}
	out, e := p.request(ctx, "huifu.refund", "/v3/trade/payment/scanpay/refund", data)
	if e != nil {
		return RefundResult{}, e
	}
	if code := jsonText(out, "resp_code"); code != "00000000" && code != "00000100" {
		return RefundResult{}, rejected("HUIFU_" + firstNonEmpty(code, "ERR"))
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(out, "hf_seq_id"), State: "SUCCESS", Amount: r.Amount}, nil
}

var _ Provider = (*huifuPlugin)(nil)
var _ NotificationVerifier = (*huifuPlugin)(nil)
var _ CreateValidator = (*huifuPlugin)(nil)

// huifuReqDate derives req_date (YYYYMMDD) from the first 8 chars of the trade
// number (the original does substr(TRADE_NO,0,8)); it falls back to today when
// the trade number is shorter.
func huifuReqDate(tradeNo string) string {
	if len(tradeNo) >= 8 && huifuAllDigits(tradeNo[:8]) {
		return tradeNo[:8]
	}
	return time.Now().Format("20060102")
}

func huifuNowDate() string { return time.Now().Format("20060102") }

func huifuAllDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}

// huifuCloseSeq mirrors date("YmdHis").rand(1000,9999) for a fresh close seq id.
func huifuCloseSeq(string) string {
	n, _ := rand.Int(rand.Reader, bigInt9000)
	return time.Now().Format("20060102150405") + strconv.FormatInt(1000+n.Int64(), 10)
}

func huifuJSONEscape(s string) string {
	// Reuse the string encoder and strip the surrounding quotes.
	q := huifuJSONString(s)
	return q[1 : len(q)-1]
}

// huifuCanonicalRaw re-encodes a received JSON object the way the original's
// checkResponseSign does: ksort the top-level keys and json_encode compactly
// with unescaped slashes/unicode. Nested values keep their received bytes
// (compacted), which equals PHP's recursive re-encode for normalized JSON.
func huifuCanonicalRaw(obj json.RawMessage) (string, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(obj, &fields) != nil {
		return "", ErrInvalid
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(huifuJSONString(k))
		b.WriteByte(':')
		b.Write(huifuCompactUnescaped(fields[k]))
	}
	b.WriteByte('}')
	return b.String(), nil
}

// huifuCompactUnescaped compacts a JSON value and, for strings, re-emits them
// with unescaped slashes and unicode so they match json_encode's flags.
func huifuCompactUnescaped(v json.RawMessage) []byte {
	trimmed := strings.TrimSpace(string(v))
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var s string
		if json.Unmarshal(v, &s) == nil {
			return []byte(huifuJSONString(s))
		}
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(trimmed)); err != nil {
		return []byte(trimmed)
	}
	return buf.Bytes()
}

var bigInt9000 = big.NewInt(9000)
