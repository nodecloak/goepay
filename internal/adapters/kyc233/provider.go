// Package kyc233 implements fixed-endpoint identity providers. It never owns
// merchant state: callers must durably claim each operation before invoking it.
package kyc233

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/hex"
	"encoding/json"
	payment "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	domain "goepay/internal/kyc233"
	"goepay/internal/platform/httpclient"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Adapter struct{ client *http.Client }

// New performs no network I/O. A supplied client is an application-owned
// transport dependency, never a browser-selected endpoint or TLS option.
func New(client *http.Client) *Adapter {
	if client == nil {
		client = httpclient.New()
	}
	c := *client
	if c.Timeout <= 0 || c.Timeout > 10*time.Second {
		c.Timeout = 10 * time.Second
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c.Jar = nil
	return &Adapter{client: &c}
}

func (p *Adapter) Initialize(ctx context.Context, s domain.Snapshot, in domain.Submission, a domain.Attempt) (domain.InitResult, error) {
	if s.Mode < 1 || s.Mode > 5 {
		return domain.InitResult{}, domain.ErrUnavailable
	}
	if !value(in.Name, 128) || !value(in.Number, 128) || !value(a.ID, 64) || !callback(a.CallbackURL) || in.CertType < 0 || in.CertType > 1 || in.CardType < 0 || in.CardType > 5 || s.Mode != 1 && in.CardType != 0 {
		return domain.InitResult{}, domain.ErrInvalid
	}
	if in.CertType == 1 {
		if !s.Corporate || !value(in.CompanyName, 256) || !value(in.CompanyNumber, 128) {
			return domain.InitResult{}, domain.ErrInvalid
		}
		passed, e := p.company(ctx, s.Credentials, in)
		if e != nil {
			return domain.InitResult{}, e
		}
		if !passed {
			return domain.InitResult{Status: "rejected"}, nil
		}
	}
	switch s.Mode {
	case 1, 3:
		ali, e := p.alipay(s)
		if e != nil {
			return domain.InitResult{}, e
		}
		var token, link string
		if s.Mode == 1 {
			token, link, e = ali.KYC233Initialize(ctx, a.ID, in.Name, in.Number, in.CardType, a.CallbackURL)
		} else {
			token, link, e = ali.KYC233Preconsult(ctx, in.Name, in.Number, a.State, a.CallbackURL)
		}
		if e != nil {
			return domain.InitResult{}, domain.ErrUnknown
		}
		if !providerURL(s.Mode, link) {
			return domain.InitResult{}, domain.ErrUnknown
		}
		return domain.InitResult{Token: token, URL: link, Scan: "alipay", Status: "pending"}, nil
	case 2:
		if !value(in.Phone, 32) {
			return domain.InitResult{}, domain.ErrInvalid
		}
		passed, e := p.phone(ctx, s.Credentials, in)
		if e != nil {
			return domain.InitResult{}, e
		}
		status := "rejected"
		if passed {
			status = "passed"
		}
		return domain.InitResult{Status: status, Scan: "phone"}, nil
	case 4:
		return p.tencentInit(ctx, s.Credentials, in, a)
	case 5:
		return p.aliyunInit(ctx, s.Credentials, in, a)
	}
	return domain.InitResult{}, domain.ErrUnavailable
}

func (p *Adapter) Query(ctx context.Context, s domain.Snapshot, a domain.Attempt) (domain.QueryResult, error) {
	if !value(a.Token, 4096) {
		return domain.QueryResult{}, domain.ErrInvalid
	}
	switch s.Mode {
	case 1, 3:
		if s.Mode == 3 && a.AuthCode == "" {
			return domain.QueryResult{Status: "pending"}, nil
		}
		ali, e := p.alipay(s)
		if e != nil {
			return domain.QueryResult{}, e
		}
		var status string
		if s.Mode == 1 {
			status, e = ali.KYC233Query(ctx, a.Token)
		} else {
			status, e = ali.KYC233Consult(ctx, a.Token, a.AuthCode)
		}
		if e != nil {
			return domain.QueryResult{}, domain.ErrUnknown
		}
		return domain.QueryResult{Status: status}, nil
	case 4:
		return p.tencentQuery(ctx, s.Credentials, a)
	case 5:
		return p.aliyunQuery(ctx, s.Credentials, a)
	default:
		return domain.QueryResult{}, domain.ErrUnavailable
	}
}

func (p *Adapter) alipay(s domain.Snapshot) (*payment.Alipay, error) {
	c := s.Alipay
	if c == nil || c.Plugin != "alipay" || c.ID != s.ChannelID {
		return nil, domain.ErrUnavailable
	}
	private, e := compat.ParsePrivateKey(c.Config["appsecret"])
	if e != nil {
		return nil, domain.ErrUnavailable
	}
	var public *rsa.PublicKey
	var certs *payment.AlipayCertificateConfig
	switch c.Config["sign_type"] {
	case "", "0":
		public, e = compat.ParsePublicKey(c.Config["appkey"])
	case "1":
		certs = &payment.AlipayCertificateConfig{ApplicationPEM: c.Certificates["app_cert_path"], PlatformPEM: c.Certificates["alipay_cert_path"], RootPEM: c.Certificates["root_cert_path"]}
	default:
		return nil, domain.ErrUnavailable
	}
	if e != nil {
		return nil, domain.ErrUnavailable
	}
	out, e := payment.NewAlipay(payment.AlipayConfig{AppID: c.Config["appid"], PrivateKey: private, PublicKey: public, CertificateMode: certs, HTTPClient: p.client})
	if e != nil {
		return nil, domain.ErrUnavailable
	}
	return out, nil
}

func value(s string, n int) bool {
	return s != "" && len(s) <= n && strings.TrimSpace(s) == s && !strings.ContainsAny(s, "\x00\r\n")
}
func callback(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Opaque == "" && u.Fragment == "" && len(raw) <= 2048 && !strings.ContainsAny(raw, "\r\n\x00\\")
}
func providerURL(mode int, raw string) bool {
	if !value(raw, 8192) {
		return false
	}
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Opaque != "" || strings.ContainsAny(raw, "\\\r\n\x00") {
		return false
	}
	switch mode {
	case 1:
		return u.Host == "openapi.alipay.com" && u.Path == "/gateway.do" && u.Fragment == ""
	case 3:
		return u.Host == "openauth.alipay.com" && u.Path == "/oauth2/publicAppAuthorize.htm" && u.Fragment == ""
	case 4:
		if u.Host == "faceid.qq.com" {
			return (u.Path == "" || u.Path == "/") && u.Fragment == ""
		}
		return u.Host == "open.weixin.qq.com" && u.Path == "/connect/oauth2/authorize" && (u.Fragment == "" || u.Fragment == "wechat_redirect")
	case 5:
		return u.Host == "picker.antcloudauth.aliyuncs.com" && u.Path == "/gateway.do" && u.Fragment == ""
	}
	return false
}

func nonce() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}
func (p *Adapter) request(ctx context.Context, method, endpoint string, body []byte, headers map[string]string) (map[string]json.RawMessage, error) {
	req, e := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if e != nil {
		return nil, domain.ErrUnavailable
	}
	// No automatic retries of billable GETs or POSTs on reused connections.
	req.GetBody = nil
	req.Close = true
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, e := p.client.Do(req)
	if e != nil {
		return nil, domain.ErrUnknown
	}
	defer res.Body.Close()
	if res.TLS == nil || len(res.TLS.VerifiedChains) == 0 || res.StatusCode != 200 {
		return nil, domain.ErrUnknown
	}
	raw, e := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if e != nil || len(raw) > 1<<20 {
		return nil, domain.ErrUnknown
	}
	m, e := object(raw)
	if e != nil {
		return nil, domain.ErrUnknown
	}
	return m, nil
}

// Reject duplicate keys at every depth before json.Unmarshal can overwrite a
// proof field. No raw upstream values are ever included in errors.
func object(raw []byte) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if e := unique(d); e != nil {
		return nil, e
	}
	if _, e := d.Token(); e != io.EOF {
		return nil, domain.ErrUnknown
	}
	var out map[string]json.RawMessage
	if json.Unmarshal(raw, &out) != nil || out == nil {
		return nil, domain.ErrUnknown
	}
	return out, nil
}
func unique(d *json.Decoder) error {
	t, e := d.Token()
	if e != nil {
		return domain.ErrUnknown
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			k, e := d.Token()
			s, ok := k.(string)
			if e != nil || !ok || seen[s] {
				return domain.ErrUnknown
			}
			seen[s] = true
			if e = unique(d); e != nil {
				return e
			}
		}
	case '[':
		for d.More() {
			if e = unique(d); e != nil {
				return e
			}
		}
	default:
		return domain.ErrUnknown
	}
	_, e = d.Token()
	if e != nil {
		return domain.ErrUnknown
	}
	return nil
}
func str(m map[string]json.RawMessage, k string) string {
	var s string
	json.Unmarshal(m[k], &s)
	return s
}
func number(raw json.RawMessage) (int64, bool) {
	var n int64
	if json.Unmarshal(raw, &n) == nil {
		return n, true
	}
	var s string
	if json.Unmarshal(raw, &s) != nil || s == "" {
		return 0, false
	}
	n, e := strconv.ParseInt(s, 10, 64)
	return n, e == nil
}
func child(m map[string]json.RawMessage, k string) (map[string]json.RawMessage, error) {
	return object(m[k])
}
func alias(m map[string]json.RawMessage, a, b string) json.RawMessage {
	first, ok := m[a]
	second, other := m[b]
	if ok && other {
		return nil
	}
	if ok {
		return first
	}
	return second
}

var _ domain.Provider = (*Adapter)(nil)
