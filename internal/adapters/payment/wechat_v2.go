package payment

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type WechatV2Config struct {
	EnableChannelSharing               bool
	EnableScanCancellation             bool
	MiniProgram                        *MiniProgramConfig
	BankPublicKey                      *rsa.PublicKey
	AppID, MerchantID, APIKey, BaseURL string
	H5SiteURL, H5SiteName              string
	Methods                            []string
	HTTPClient, RefundClient           *http.Client
}
type WechatV2 struct{ config WechatV2Config }

func NewWechatV2(c WechatV2Config) (*WechatV2, error) {
	if c.AppID == "" || c.MerchantID == "" || len(c.APIKey) < 16 {
		return nil, ErrInvalid
	}
	if c.BankPublicKey != nil && (c.BankPublicKey.N == nil || c.BankPublicKey.N.BitLen() < 2048 || c.BankPublicKey.E < 3 || c.BankPublicKey.E%2 == 0) {
		return nil, ErrInvalid
	}
	if c.BaseURL == "" {
		c.BaseURL = "https://api.mch.weixin.qq.com"
	}
	u, e := url.Parse(c.BaseURL)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
		return nil, ErrInvalid
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	if c.Methods == nil {
		c.Methods = []string{"native", "jsapi"}
	}
	c.MiniProgram, c.Methods, e = configureMiniProgram(c.MiniProgram, c.AppID, "wxpay", c.Methods)
	if e != nil {
		return nil, e
	}
	for _, method := range c.Methods {
		if method != "native" && method != "jsapi" && method != "h5" && method != "app" && method != "scan" && method != "applet" {
			return nil, ErrUnsupported
		}
		if method == "h5" {
			site, e := url.Parse(c.H5SiteURL)
			if e != nil || site.Scheme != "https" || site.Host == "" || site.User != nil || site.RawQuery != "" || site.Fragment != "" || strings.TrimSpace(c.H5SiteName) == "" || len(c.H5SiteName) > 128 {
				return nil, ErrInvalid
			}
		}
	}
	c.Methods = append([]string{}, c.Methods...)
	c.HTTPClient = boundedClient(c.HTTPClient)
	if c.RefundClient != nil {
		c.RefundClient = boundedClient(c.RefundClient)
	}
	return &WechatV2{c}, nil
}
func (w *WechatV2) ID() string            { return "wxpay" }
func (w *WechatV2) ApplicationID() string { return w.config.AppID }
func (w *WechatV2) Capabilities() Capabilities {
	return Capabilities{Methods: append([]string{}, w.config.Methods...), Query: true, Close: true, Refund: w.config.RefundClient != nil, RefundQuery: true}
}
func nonce() (string, error) {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return hex.EncodeToString(b), nil
}
func (w *WechatV2) call(ctx context.Context, path string, p map[string]string, write, cert bool) (map[string]string, error) {
	p["appid"] = w.config.AppID
	p["mch_id"] = w.config.MerchantID
	return w.exchange(ctx, path, p, write, cert, true, map[string]string{"appid": w.config.AppID, "mch_id": w.config.MerchantID})
}
func (w *WechatV2) exchange(ctx context.Context, path string, p map[string]string, write, cert, requireSign bool, expected map[string]string) (map[string]string, error) {
	client := w.config.HTTPClient
	if cert {
		client = w.config.RefundClient
		if client == nil {
			return nil, ErrUnsupported
		}
	}
	n, e := nonce()
	if e != nil {
		return nil, e
	}
	p["nonce_str"] = n
	p["sign_type"] = "MD5"
	p["sign"], e = SignWechatV2(p, w.config.APIKey, "MD5")
	if e != nil {
		return nil, e
	}
	body := encodeXML(p)
	req, e := http.NewRequestWithContext(ctx, "POST", strings.TrimSuffix(w.config.BaseURL, "/")+path, bytes.NewReader(body))
	if e != nil {
		return nil, e
	}
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	res, e := client.Do(req)
	if e != nil {
		if write {
			return nil, &UnknownOutcome{path, e}
		}
		return nil, e
	}
	defer res.Body.Close()
	if path == wechatScanReversePath && !wechatVerifiedMutualTLS(req, res, client) {
		return nil, &UnknownOutcome{Operation: "scan cancellation", Cause: ErrAuthentication}
	}
	if path == "/pay/orderquery" && scanObservationRequested(ctx) && !authenticatedTLSExchange(req, res) {
		return nil, ErrAuthentication
	}
	raw, e := io.ReadAll(io.LimitReader(res.Body, 1<<20+1))
	if e != nil || len(raw) > 1<<20 || res.StatusCode != 200 {
		if write {
			return nil, &UnknownOutcome{path, errors.New("invalid upstream HTTP response")}
		}
		return nil, errors.New("invalid upstream HTTP response")
	}
	out, e := decodeXML(raw)
	if e != nil {
		if write {
			return nil, &UnknownOutcome{path, e}
		}
		return nil, e
	}
	if path == "/pay/orderquery" && scanObservationRequested(ctx) && (!scanCancellationNumber.MatchString(out["nonce_str"]) || len(out["nonce_str"]) > 32) {
		return nil, ErrAuthentication
	}
	if out["return_code"] != "SUCCESS" {
		return nil, &ProviderError{Code: "RETURN_FAIL"}
	}
	if (requireSign || out["sign"] != "") && !VerifyWechatV2(out, w.config.APIKey, "MD5", out["sign"]) {
		if write {
			return nil, &UnknownOutcome{path, ErrAuthentication}
		}
		return nil, ErrAuthentication
	}
	if requireSign {
		for field, value := range expected {
			if out[field] != value {
				if write {
					return nil, &UnknownOutcome{path, ErrAuthentication}
				}
				return nil, ErrAuthentication
			}
		}
	}
	if out["result_code"] != "SUCCESS" {
		if authentication := wechatFailureAuthentication(req, res, client, cert, out, expected); authentication != "" {
			rejected := authenticatedProviderError("wxpay", path, out["err_code"], authentication, raw)
			rejected.rejection.verifiedTLS = authenticatedTLSExchange(req, res)
			if path == "/pay/micropay" && authentication == "wechat_v2_md5" && authenticatedTLSExchange(req, res) && scanCancellationNumber.MatchString(out["nonce_str"]) && len(out["nonce_str"]) <= 32 {
				trusted := true
				for _, field := range []string{"out_trade_no", "total_fee"} {
					if got, present := out[field]; present && got != p[field] {
						trusted = false
					}
				}
				rejected.rejection.scanPendingTransport = trusted
			}
			return nil, rejected
		}
		return nil, &ProviderError{Code: out["err_code"]}
	}
	for field, value := range expected {
		if out[field] != value {
			if write {
				return nil, &UnknownOutcome{path, ErrAuthentication}
			}
			return nil, ErrAuthentication
		}
	}
	return out, nil
}
func (w *WechatV2) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := w.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	tradeType := ""
	switch r.Method {
	case "native":
		tradeType = "NATIVE"
	case "jsapi":
		tradeType = "JSAPI"
	case "h5":
		tradeType = "MWEB"
	case "app":
		tradeType = "APP"
	default:
		return CreateResult{}, ErrUnsupported
	}
	p := map[string]string{"body": r.Subject, "out_trade_no": r.TradeNo, "total_fee": strconv.FormatInt(r.Amount, 10), "spbill_create_ip": r.ClientIP, "notify_url": r.NotifyURL, "trade_type": tradeType}
	if r.ProfitSharing {
		p["profit_sharing"] = "Y"
	}
	if r.Method == "native" {
		p["product_id"] = r.TradeNo
	}
	if r.Method == "h5" {
		scene, _ := json.Marshal(map[string]any{"h5_info": map[string]string{"type": "Wap", "wap_url": w.config.H5SiteURL, "wap_name": w.config.H5SiteName}})
		p["scene_info"] = string(scene)
	}
	if r.Method == "jsapi" && r.OpenID != "" {
		p["openid"] = r.OpenID
	}
	if r.ExpiresAt != nil {
		p["time_expire"] = r.ExpiresAt.In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("20060102150405")
	} else {
		p["time_expire"] = time.Now().Add(2 * time.Hour).In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("20060102150405")
	}
	out, e := w.call(ctx, "/pay/unifiedorder", p, true, false)
	if e != nil {
		return CreateResult{}, e
	}
	if out["out_trade_no"] != "" && out["out_trade_no"] != r.TradeNo {
		return CreateResult{}, &UnknownOutcome{"/pay/unifiedorder", ErrAuthentication}
	}
	switch r.Method {
	case "native":
		if out["code_url"] == "" {
			return CreateResult{}, &UnknownOutcome{"/pay/unifiedorder", ErrAuthentication}
		}
		return CreateResult{MaterialType: "qrcode", Material: out["code_url"]}, nil
	case "h5":
		mweb, e := url.Parse(out["mweb_url"])
		if e != nil || mweb.Scheme != "https" || mweb.Host == "" || mweb.User != nil || mweb.Fragment != "" {
			return CreateResult{}, &UnknownOutcome{"/pay/unifiedorder", ErrAuthentication}
		}
		if r.ReturnURL != "" {
			q := mweb.Query()
			q.Set("redirect_url", r.ReturnURL)
			mweb.RawQuery = q.Encode()
		}
		return CreateResult{MaterialType: "jump", Material: mweb.String()}, nil
	default:
		if out["prepay_id"] == "" {
			return CreateResult{}, &UnknownOutcome{"/pay/unifiedorder", ErrAuthentication}
		}
		if r.Method == "jsapi" {
			p, e := w.JSAPIParams(out["prepay_id"], time.Now())
			if e != nil {
				return CreateResult{}, e
			}
			b, e := json.Marshal(p)
			if e != nil {
				return CreateResult{}, e
			}
			return CreateResult{MaterialType: "jsapi", Material: string(b)}, nil
		}
		params, e := w.APPParams(out["prepay_id"], time.Now())
		if e != nil {
			return CreateResult{}, e
		}
		body, e := json.Marshal(params)
		if e != nil {
			return CreateResult{}, e
		}
		return CreateResult{MaterialType: "app", Material: string(body)}, nil
	}
}

func (w *WechatV2) ValidateCreate(r CreateRequest) error {
	if r.ProfitSharing {
		if e := w.validateSharingV2Transport(false); e != nil {
			return e
		}
	}
	if r.Method == "scan" || r.Method == "applet" {
		return ErrUnsupported
	}
	if !validCreate(r) {
		return ErrInvalid
	}
	if !enabled(r.Method, w.config.Methods) {
		return ErrUnsupported
	}
	if !validPaymentURL(r.NotifyURL) || net.ParseIP(r.ClientIP) == nil {
		return ErrInvalid
	}
	if r.Method == "jsapi" && (r.OpenID == "" || len(r.OpenID) > 128 || strings.ContainsAny(r.OpenID, "\r\n\x00")) {
		return ErrInvalid
	}
	if r.Method == "h5" && r.ReturnURL != "" && !validPaymentURL(r.ReturnURL) {
		return ErrInvalid
	}
	return nil
}
func (w *WechatV2) Query(ctx context.Context, r QueryRequest) (QueryResult, error) {
	if r.TradeNo == "" {
		return QueryResult{}, ErrInvalid
	}
	out, e := w.call(ctx, "/pay/orderquery", map[string]string{"out_trade_no": r.TradeNo}, false, false)
	if e != nil {
		return QueryResult{}, e
	}
	n, e := strconv.ParseInt(out["total_fee"], 10, 64)
	if e != nil || n <= 0 || out["out_trade_no"] != r.TradeNo || !trustedBuyer(out["openid"]) || out["fee_type"] != "" && out["fee_type"] != "CNY" {
		return QueryResult{}, ErrAuthentication
	}
	return QueryResult{TradeNo: r.TradeNo, ProviderTradeNo: out["transaction_id"], State: out["trade_state"], Currency: "CNY", Amount: n, BuyerID: out["openid"]}, nil
}
func (w *WechatV2) Close(ctx context.Context, r QueryRequest) error {
	if r.TradeNo == "" {
		return ErrInvalid
	}
	_, e := w.call(ctx, "/pay/closeorder", map[string]string{"out_trade_no": r.TradeNo}, true, false)
	return e
}
func (w *WechatV2) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	out, e := w.call(ctx, "/secapi/pay/refund", map[string]string{"out_trade_no": r.TradeNo, "out_refund_no": r.RefundNo, "total_fee": strconv.FormatInt(r.TotalAmount, 10), "refund_fee": strconv.FormatInt(r.Amount, 10)}, true, true)
	if e != nil {
		return RefundResult{}, e
	}
	if out["out_refund_no"] != r.RefundNo || out["out_trade_no"] != r.TradeNo || out["refund_fee"] != strconv.FormatInt(r.Amount, 10) {
		return RefundResult{}, &UnknownOutcome{"/secapi/pay/refund", ErrAuthentication}
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: out["refund_id"], State: "PROCESSING", Amount: r.Amount}, nil
}
func (w *WechatV2) QueryRefund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.RefundNo == "" || r.TradeNo == "" || r.TotalAmount <= 0 {
		return RefundResult{}, ErrInvalid
	}
	out, e := w.call(ctx, "/pay/refundquery", map[string]string{"out_refund_no": r.RefundNo}, false, false)
	if e != nil {
		return RefundResult{}, e
	}
	if out["out_trade_no"] != r.TradeNo || out["total_fee"] != strconv.FormatInt(r.TotalAmount, 10) {
		return RefundResult{}, ErrAuthentication
	}
	for i := 0; i < 100; i++ {
		suffix := "_" + strconv.Itoa(i)
		if out["out_refund_no"+suffix] == r.RefundNo {
			n, e := strconv.ParseInt(out["refund_fee"+suffix], 10, 64)
			if e != nil {
				return RefundResult{}, ErrAuthentication
			}
			return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: out["refund_id"+suffix], State: out["refund_status"+suffix], Amount: n}, nil
		}
	}
	return RefundResult{}, ErrAuthentication
}
func (w *WechatV2) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	out, e := decodeXML(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	if out["return_code"] != "SUCCESS" || out["result_code"] != "SUCCESS" || !VerifyWechatV2(out, w.config.APIKey, "MD5", out["sign"]) {
		return PaymentEvent{}, ErrAuthentication
	}
	n, e := strconv.ParseInt(out["total_fee"], 10, 64)
	if e != nil || n != expected.Amount || expected.Amount <= 0 || out["out_trade_no"] != expected.TradeNo || out["appid"] != w.config.AppID || out["mch_id"] != w.config.MerchantID || expected.AppID != "" && out["appid"] != expected.AppID || expected.MerchantID != "" && out["mch_id"] != expected.MerchantID || out["transaction_id"] == "" || out["fee_type"] != "" && out["fee_type"] != "CNY" {
		return PaymentEvent{}, ErrAuthentication
	}
	return PaymentEvent{EventID: out["transaction_id"], TradeNo: out["out_trade_no"], ProviderTradeNo: out["transaction_id"], BuyerID: out["openid"], State: "SUCCESS", Currency: "CNY", Amount: n}, nil
}
func encodeXML(p map[string]string) []byte {
	var b bytes.Buffer
	b.WriteString("<xml>")
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteString("<" + k + ">")
		xml.EscapeText(&b, []byte(p[k]))
		b.WriteString("</" + k + ">")
	}
	b.WriteString("</xml>")
	return b.Bytes()
}
func decodeXML(raw []byte) (map[string]string, error) {
	if len(raw) > 1<<20 {
		return nil, ErrInvalid
	}
	d := xml.NewDecoder(bytes.NewReader(raw))
	out := map[string]string{}
	root := false
	closed := false
	for {
		token, e := d.Token()
		if e == io.EOF {
			if !closed {
				return nil, ErrInvalid
			}
			return out, nil
		}
		if e != nil {
			return nil, ErrInvalid
		}
		switch t := token.(type) {
		case xml.StartElement:
			if closed {
				return nil, ErrInvalid
			}
			if !root {
				if t.Name.Local != "xml" || t.Name.Space != "" || len(t.Attr) > 0 {
					return nil, ErrInvalid
				}
				root = true
				continue
			}
			if t.Name.Space != "" || len(t.Attr) > 0 {
				return nil, ErrInvalid
			}
			if _, exists := out[t.Name.Local]; exists {
				return nil, ErrInvalid
			}
			var text strings.Builder
			for {
				token, e := d.Token()
				if e != nil {
					return nil, ErrInvalid
				}
				if end, ok := token.(xml.EndElement); ok {
					if end.Name != t.Name {
						return nil, ErrInvalid
					}
					break
				}
				chars, ok := token.(xml.CharData)
				if !ok {
					return nil, ErrInvalid
				}
				text.Write([]byte(chars))
			}
			out[t.Name.Local] = text.String()
		case xml.EndElement:
			if t.Name.Local == "xml" {
				closed = true
			}
		case xml.Directive:
			return nil, ErrInvalid
		case xml.CharData:
			if strings.TrimSpace(string(t)) != "" {
				return nil, ErrInvalid
			}
		}
	}
}

var _ Provider = (*WechatV2)(nil)
var _ NotificationVerifier = (*WechatV2)(nil)

// JSAPIParams creates the second, browser-facing APIv2 signature. The returned
// JSON keys follow WeixinJSBridge and differ from server API field names.
func (w *WechatV2) JSAPIParams(prepayID string, now time.Time) (map[string]string, error) {
	if prepayID == "" || strings.ContainsAny(prepayID, "\r\n") {
		return nil, ErrInvalid
	}
	n, e := nonce()
	if e != nil {
		return nil, e
	}
	p := map[string]string{"appId": w.config.AppID, "timeStamp": strconv.FormatInt(now.Unix(), 10), "nonceStr": n, "package": "prepay_id=" + prepayID, "signType": "MD5"}
	signature, e := SignWechatV2(p, w.config.APIKey, "MD5")
	if e != nil {
		return nil, e
	}
	p["paySign"] = signature
	return p, nil
}

func (w *WechatV2) APPParams(prepayID string, now time.Time) (map[string]string, error) {
	if prepayID == "" || strings.ContainsAny(prepayID, "\r\n") {
		return nil, ErrInvalid
	}
	n, e := nonce()
	if e != nil {
		return nil, e
	}
	p := map[string]string{"appid": w.config.AppID, "partnerid": w.config.MerchantID, "prepayid": prepayID, "package": "Sign=WXPay", "noncestr": n, "timestamp": strconv.FormatInt(now.Unix(), 10)}
	p["sign"], e = SignWechatV2(p, w.config.APIKey, "MD5")
	return p, e
}
