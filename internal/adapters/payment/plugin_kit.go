package payment

import (
	"bytes"
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// ---- optional provider interfaces used by the callback endpoint ----

// NotificationAcker answers an upstream callback in the format that upstream
// expects ("success", "SUCCESS", JSON, XML ...). A zero status keeps the
// endpoint's built-in reply.
type NotificationAcker interface {
	NotificationAck(err error) (status int, contentType, body string)
}

// NotificationResponder answers from the callback itself, for upstreams whose
// original reply echoes data from their own request (an order id, a signed
// head). err is the verification result; the reply must not leak secrets. A
// zero status falls back to NotificationAck, then to the built-in reply.
type NotificationResponder interface {
	NotificationResponse(header http.Header, raw []byte, err error) (status int, contentType, body string)
}

// NotificationMethodsProvider lists the HTTP methods an upstream may use for
// callbacks. Without it only POST is accepted. For GET the notification bytes
// handed to VerifyNotification are the raw URL query string.
type NotificationMethodsProvider interface{ NotificationMethods() []string }

// PluginIdentity names the ported plugin behind a provider.
type PluginIdentity interface{ PluginName() string }

// ---- base type embedded by every ported plugin ----

// pluginBase carries the parts every ported plugin shares. Plugins embed it
// and implement Capabilities, Create and VerifyNotification themselves, plus
// whichever of Query/Close/Refund/QueryRefund the original supports.
type pluginBase struct {
	plugin, payType string
	// account is the public upstream account identity (never a credential):
	// e.g. gateway host + merchant number. Orders are bound to it.
	account string
	client  *http.Client
}

func newPluginBase(c PluginChannel, account string) (pluginBase, error) {
	account = strings.TrimSpace(account)
	if account == "" || len(account) > 400 || strings.ContainsAny(account, "\x00\r\n") {
		return pluginBase{}, ErrInvalid
	}
	return pluginBase{plugin: c.Plugin, payType: c.PayType, account: account, client: boundedClient(c.HTTPClient)}, nil
}

func (b *pluginBase) ID() string         { return b.payType }
func (b *pluginBase) PluginName() string { return b.plugin }

// ApplicationID names the plugin product; it is not a WeChat/Alipay appid, so
// a caller-supplied sub_appid can never match it.
func (b *pluginBase) ApplicationID() string { return "plugin:" + b.plugin }
func (b *pluginBase) TransferAccountIdentity() string {
	return "plugin:" + b.plugin + ":" + b.account
}
func (b *pluginBase) PaymentProductIdentity() string { return "plugin." + b.plugin + ".payment" }
func (b *pluginBase) Query(context.Context, QueryRequest) (QueryResult, error) {
	return QueryResult{}, ErrUnsupported
}
func (b *pluginBase) Close(context.Context, QueryRequest) error { return ErrUnsupported }
func (b *pluginBase) Refund(context.Context, RefundRequest) (RefundResult, error) {
	return RefundResult{}, ErrUnsupported
}
func (b *pluginBase) QueryRefund(context.Context, RefundRequest) (RefundResult, error) {
	return RefundResult{}, ErrUnsupported
}

// Default callback contract of the original plugins: POST, plain "success".
func (b *pluginBase) NotificationMethods() []string { return []string{http.MethodPost} }
func (b *pluginBase) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return http.StatusBadRequest, "text/plain; charset=utf-8", "fail"
	}
	return http.StatusOK, "text/plain; charset=utf-8", "success"
}

// ---- HTTP ----

// pluginCall sends one request. The outcome rules match the native adapters:
//   - transport error, timeout, cancellation or HTTP 5xx: *UnknownOutcome
//     (the request may have reached the upstream; query before retrying);
//   - other non-2xx: *ProviderError (a definitive rejection);
//   - 2xx: the body (at most 2 MiB).
func (b *pluginBase) pluginCall(ctx context.Context, operation, method, target string, body []byte, contentType string, headers map[string]string) ([]byte, error) {
	if !validPaymentURL(target) {
		return nil, ErrInvalid
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, e := http.NewRequestWithContext(ctx, method, target, reader)
	if e != nil {
		return nil, ErrInvalid
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "*/*")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, e := b.client.Do(req)
	if e != nil {
		return nil, &UnknownOutcome{Operation: operation, Cause: e}
	}
	defer resp.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(resp.Body, 2<<20+1))
	if e != nil || len(raw) > 2<<20 {
		return nil, &UnknownOutcome{Operation: operation, Cause: errors.New("unreadable upstream response")}
	}
	if resp.StatusCode >= 500 {
		return nil, &UnknownOutcome{Operation: operation, Cause: fmt.Errorf("upstream HTTP %d", resp.StatusCode)}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &ProviderError{Code: fmt.Sprintf("HTTP_%d", resp.StatusCode)}
	}
	return raw, nil
}

func (b *pluginBase) postForm(ctx context.Context, operation, target string, form url.Values) ([]byte, error) {
	return b.pluginCall(ctx, operation, http.MethodPost, target, []byte(form.Encode()), "application/x-www-form-urlencoded; charset=utf-8", nil)
}

func (b *pluginBase) postJSON(ctx context.Context, operation, target string, payload any, headers map[string]string) ([]byte, error) {
	raw, e := json.Marshal(payload)
	if e != nil {
		return nil, ErrInvalid
	}
	return b.pluginCall(ctx, operation, http.MethodPost, target, raw, "application/json; charset=utf-8", headers)
}

func (b *pluginBase) getURL(ctx context.Context, operation, target string) ([]byte, error) {
	return b.pluginCall(ctx, operation, http.MethodGet, target, nil, "", nil)
}

// rejected turns an upstream's explicit business refusal into a definitive
// provider error carrying its code (never a free-text message).
func rejected(code string) error {
	code = strings.TrimSpace(code)
	if code == "" || len(code) > 64 {
		code = "REJECTED"
	}
	return &ProviderError{Code: code}
}

// ---- parameters and signatures ----

// epaySignString is the original sign base used by 彩虹易支付 and many
// aggregators: keys sorted (PHP ksort, byte order), "k=v" joined by "&",
// skipping the sign fields and empty values, values NOT url-encoded.
func epaySignString(p map[string]string, skip ...string) string {
	skipped := map[string]bool{"sign": true, "sign_type": true}
	for _, k := range skip {
		skipped[k] = true
	}
	keys := make([]string, 0, len(p))
	for k, v := range p {
		if skipped[k] || v == "" {
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
		sb.WriteString(p[k])
	}
	return sb.String()
}

func md5Hex(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}
func sha1Hex(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}
func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
func hmacSHA256Hex(key, s string) string {
	m := hmac.New(sha256.New, []byte(key))
	m.Write([]byte(s))
	return hex.EncodeToString(m.Sum(nil))
}

// equalFold compares signatures in constant time, case-insensitively when
// the original compares hex digests without regard to case.
func signEqual(want, got string, ignoreCase bool) bool {
	if ignoreCase {
		want, got = strings.ToLower(want), strings.ToLower(got)
	}
	return want != "" && subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}

// Upstream acquirers pick their own RSA keys; many still use 1024-bit keys
// with SHA1. Their keys are accepted from 1024 bits, merchant-facing GOEPAY
// keys keep their stricter rules elsewhere.
const pluginMinRSABits = 1024

// pluginKeyDER accepts one PEM block or bare base64 (the plugins' config
// fields hold the key text without headers).
func pluginKeyDER(text string) ([]byte, error) {
	text = strings.TrimSpace(text)
	if block, rest := pem.Decode([]byte(text)); block != nil {
		if strings.TrimSpace(string(rest)) != "" {
			return nil, ErrInvalid
		}
		return block.Bytes, nil
	}
	der, e := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(text), ""))
	if e != nil || len(der) == 0 {
		return nil, ErrInvalid
	}
	return der, nil
}

func pluginPrivateKey(text string) (*rsa.PrivateKey, error) {
	der, e := pluginKeyDER(text)
	if e != nil {
		return nil, ErrInvalid
	}
	var k *rsa.PrivateKey
	if parsed, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		k, _ = parsed.(*rsa.PrivateKey)
	} else if pk, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		k = pk
	}
	if k == nil || k.N.BitLen() < pluginMinRSABits || k.Validate() != nil {
		return nil, ErrInvalid
	}
	return k, nil
}

// pluginPublicKey accepts PKIX, PKCS#1 or an X.509 certificate (many
// acquirers hand out their verification key as a .cer), PEM or bare base64.
func pluginPublicKey(text string) (*rsa.PublicKey, error) {
	der, e := pluginKeyDER(text)
	if e != nil {
		return nil, ErrInvalid
	}
	var k *rsa.PublicKey
	if parsed, err := x509.ParsePKIXPublicKey(der); err == nil {
		k, _ = parsed.(*rsa.PublicKey)
	} else if pk, err := x509.ParsePKCS1PublicKey(der); err == nil {
		k = pk
	} else if c, err := x509.ParseCertificate(der); err == nil {
		k, _ = c.PublicKey.(*rsa.PublicKey)
	}
	if k == nil || k.N.BitLen() < pluginMinRSABits {
		return nil, ErrInvalid
	}
	return k, nil
}

// rsaSign signs with PKCS#1 v1.5 and returns standard base64.
func rsaSign(key *rsa.PrivateKey, hash crypto.Hash, message string) (string, error) {
	if key == nil || (hash != crypto.SHA1 && hash != crypto.SHA256) {
		return "", ErrAuthentication
	}
	h := hash.New()
	h.Write([]byte(message))
	sig, e := rsa.SignPKCS1v15(rand.Reader, key, hash, h.Sum(nil))
	if e != nil {
		return "", e
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

// rsaVerify accepts standard or URL-safe base64 signatures (PKCS#1 v1.5).
func rsaVerify(key *rsa.PublicKey, hash crypto.Hash, message, signature string) bool {
	if key == nil || signature == "" || (hash != crypto.SHA1 && hash != crypto.SHA256) {
		return false
	}
	sig, e := base64.StdEncoding.DecodeString(signature)
	if e != nil {
		sig, e = base64.URLEncoding.DecodeString(signature)
		if e != nil {
			return false
		}
	}
	h := hash.New()
	h.Write([]byte(message))
	return rsa.VerifyPKCS1v15(key, hash, h.Sum(nil), sig) == nil
}

// ---- amounts ----

// yuan renders cents as the original's two-decimal amount ("12.30").
func yuan(cents int64) string { return amountString(cents) }

// parseYuan reads an upstream amount in yuan ("12.3", "12.30", "12").
func parseYuan(s string) (int64, error) { return parseAmount(strings.TrimSpace(s)) }

// parseFen reads an upstream amount in cents ("1230").
func parseFen(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 13 {
		return 0, ErrInvalid
	}
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, ErrInvalid
		}
		n = n*10 + int64(c-'0')
	}
	if n <= 0 || n > 999999999999 {
		return 0, ErrInvalid
	}
	return n, nil
}

// ---- callbacks ----

// callbackParams parses a form or query-string callback into single values.
// Repeated keys, PHP array keys and NUL bytes are refused.
func callbackParams(raw []byte) (map[string]string, error) {
	if len(raw) == 0 || len(raw) > 1<<20 {
		return nil, ErrInvalid
	}
	values, e := url.ParseQuery(string(raw))
	if e != nil {
		return nil, ErrInvalid
	}
	out := map[string]string{}
	for k, vs := range values {
		if len(vs) != 1 || k == "" || strings.ContainsAny(k, "[]\x00") || strings.ContainsRune(vs[0], 0) {
			return nil, ErrInvalid
		}
		out[k] = vs[0]
	}
	return out, nil
}

// callbackJSON decodes a JSON callback object; numbers stay as json.Number.
func callbackJSON(raw []byte) (map[string]any, error) {
	if len(raw) == 0 || len(raw) > 1<<20 {
		return nil, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var out map[string]any
	if d.Decode(&out) != nil || out == nil {
		return nil, ErrInvalid
	}
	return out, nil
}

// jsonText reads a JSON field as text (string or number), "" when absent.
func jsonText(m map[string]any, key string) string {
	switch v := m[key].(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case bool:
		if v {
			return "true"
		}
		return "false"
	case nil:
		return ""
	default:
		raw, _ := json.Marshal(v)
		return string(raw)
	}
}

// xmlFlat decodes a flat <xml><k>v</k>…</xml> document (WeChat-style).
func xmlFlat(raw []byte) (map[string]string, error) {
	if len(raw) == 0 || len(raw) > 1<<20 {
		return nil, ErrInvalid
	}
	d := xml.NewDecoder(bytes.NewReader(raw))
	out := map[string]string{}
	depth, key := 0, ""
	var text strings.Builder
	for {
		t, e := d.Token()
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			return nil, ErrInvalid
		}
		switch v := t.(type) {
		case xml.StartElement:
			depth++
			if depth == 2 {
				key = v.Name.Local
				text.Reset()
			} else if depth > 2 {
				return nil, ErrInvalid
			}
		case xml.CharData:
			if depth == 2 {
				text.Write(v)
			}
		case xml.EndElement:
			if depth == 2 {
				if _, dup := out[key]; dup {
					return nil, ErrInvalid
				}
				out[key] = text.String()
			}
			depth--
		case xml.Directive:
			return nil, ErrInvalid
		}
	}
	return out, nil
}

// paidEvent builds the event the payment core expects after the plugin has
// verified the signature, the trade number and the amount.
func paidEvent(tradeNo, providerTradeNo, buyer string, amount int64) PaymentEvent {
	id := providerTradeNo
	if id == "" {
		id = tradeNo
	}
	if len(buyer) > 256 || strings.ContainsAny(buyer, "\x00\r\n") {
		buyer = ""
	}
	return PaymentEvent{EventID: id, TradeNo: tradeNo, ProviderTradeNo: providerTradeNo, BuyerID: buyer, State: "SUCCESS", Currency: "CNY", Amount: amount}
}

// pluginAccount joins public account facts into a stable identity string.
func pluginAccount(parts ...string) string {
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	return strings.Join(parts, "|")
}

// gatewayHost returns scheme://host of a configured gateway URL for the
// account identity (paths and queries are not identity).
func gatewayHost(raw string) (string, bool) {
	u, e := url.Parse(strings.TrimSpace(raw))
	if e != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return "", false
	}
	return u.Scheme + "://" + strings.ToLower(u.Host), true
}

// ---- create helpers ----

// pluginForm renders a POST form to an upstream page (the original plugins'
// "html" result). Every name and value is HTML-escaped; no inline script, the
// checkout page submits it.
func pluginForm(action string, params map[string]string, button string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(`<form method="post" action="` + html.EscapeString(action) + `">`)
	for _, k := range keys {
		b.WriteString(`<input type="hidden" name="` + html.EscapeString(k) + `" value="` + html.EscapeString(params[k]) + `">`)
	}
	if button == "" {
		button = "立即支付"
	}
	b.WriteString(`<button type="submit">` + html.EscapeString(button) + `</button></form>`)
	return b.String()
}

// validatePluginCreate is the local, offline check every plugin's
// ValidateCreate runs before any attempt is recorded.
func validatePluginCreate(r CreateRequest, methods []string) error {
	if !validCreate(r) || !enabled(r.Method, methods) {
		return ErrInvalid
	}
	if r.NotifyURL == "" || !validPaymentURL(r.NotifyURL) || r.ReturnURL != "" && !validPaymentURL(r.ReturnURL) {
		return ErrInvalid
	}
	if len(r.Subject) > 256 || strings.ContainsRune(r.Subject, 0) || len(r.ClientIP) > 64 {
		return ErrInvalid
	}
	return nil
}

// materialFromLinks maps the common aggregator reply (pay URL, QR content,
// app scheme) to checkout material, preferring what the original preferred.
func materialFromLinks(payURL, qrcode, scheme string) (CreateResult, bool) {
	switch {
	case payURL != "" && validPaymentURL(payURL):
		return CreateResult{MaterialType: "jump", Material: payURL}, true
	case qrcode != "" && len(qrcode) <= 4096 && !strings.ContainsAny(qrcode, "\x00\r\n"):
		return CreateResult{MaterialType: "qrcode", Material: qrcode}, true
	case scheme != "" && len(scheme) <= 4096 && !strings.ContainsAny(scheme, "\x00\r\n") && strings.Contains(scheme, "://"):
		return CreateResult{MaterialType: "urlscheme", Material: scheme}, true
	}
	return CreateResult{}, false
}
