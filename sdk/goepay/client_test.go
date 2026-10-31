package goepay

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var merchantKey, platformKey, wrongKey *rsa.PrivateKey
var testNow = time.Unix(1793400000, 0)

type forbiddenGlobalTransport struct{}

func (forbiddenGlobalTransport) RoundTrip(*http.Request) (*http.Response, error) {
	panic("SDK must not call a global custom transport")
}

func TestNewUsesIndependentTransportWithAnyGlobalDefault(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	for _, global := range []http.RoundTripper{forbiddenGlobalTransport{}, (*http.Transport)(nil), nil} {
		http.DefaultTransport = global
		c, err := New(config("https://pay.example.com"))
		if err != nil {
			t.Fatalf("valid config rejected with custom global transport: %v", err)
		}
		defer c.CloseIdleConnections()
		transport, ok := c.http.Transport.(*http.Transport)
		if !ok || transport == nil || transport.Proxy != nil || transport.TLSClientConfig == nil || transport.TLSClientConfig.InsecureSkipVerify {
			t.Fatal("SDK did not create its own protected transport")
		}
	}
}

func TestSDKDoesNotInheritGlobalDialer(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	var intercepted atomic.Int64
	http.DefaultTransport = &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		intercepted.Add(1)
		return nil, errors.New("global dialer must not be used")
	}}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(signedJSON(map[string]string{"code": "0", "trade_no": "trade-217"}))
	}))
	defer s.Close()
	c := client(t, s.URL)
	defer c.CloseIdleConnections()
	if _, err := c.Query(context.Background(), nil); err != nil {
		t.Fatalf("SDK inherited global dialer behavior: %v", err)
	}
	if intercepted.Load() != 0 {
		t.Fatal("global dialer intercepted signed request")
	}
}

func TestMain(m *testing.M) {
	var err error
	for _, dst := range []**rsa.PrivateKey{&merchantKey, &platformKey, &wrongKey} {
		*dst, err = rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}
	}
	os.Exit(m.Run())
}

func config(base string) Config {
	der, err := x509.MarshalPKIXPublicKey(&platformKey.PublicKey)
	if err != nil {
		panic(err)
	}
	return Config{BaseURL: base, MerchantID: 1001, AllowLoopbackHTTP: true,
		MerchantPrivateKeyPEM: pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(merchantKey)}),
		PlatformPublicKeyPEM:  pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})}
}

func client(t *testing.T, base string) *Client {
	t.Helper()
	c, err := New(config(base))
	if err != nil {
		t.Fatal(err)
	}
	c.now = func() time.Time { return testNow }
	return c
}

// Independent stdlib fixture signer: no SDK canonical/sign helpers are used.
func fixtureSignature(fields map[string]string, key *rsa.PrivateKey, trim bool) string {
	keys := make([]string, 0, len(fields))
	for k, v := range fields {
		if k == "sign" || k == "sign_type" || v == "" || (trim && strings.Trim(v, " \t\r\n\x00\x0b") == "") {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+fields[k])
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, "&")))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		panic(err)
	}
	return base64.StdEncoding.EncodeToString(sig)
}

func signedJSON(fields map[string]string) []byte {
	if fields == nil {
		fields = map[string]string{}
	}
	fields["timestamp"], fields["sign_type"] = strconv.FormatInt(testNow.Unix(), 10), "RSA"
	fields["sign"] = fixtureSignature(fields, platformKey, true)
	raw := map[string]any{}
	for k, v := range fields {
		raw[k] = v
	}
	code, err := strconv.Atoi(fields["code"])
	if err != nil {
		panic(err)
	}
	raw["code"] = code
	b, err := json.Marshal(raw)
	if err != nil {
		panic(err)
	}
	return b
}

func TestSubmitURLPreservesSignedValuesAndDoesNotSend(t *testing.T) {
	c := client(t, "https://pay.example.com/")
	params := url.Values{"money": {"1.00"}, "name": {" 中文😀 +% "}, "out_trade_no": {"order-217"}, "type": {"alipay"}, "space": {" \t"}, "zero": {"0"}, "empty": {""}}
	target, err := c.SubmitURL(params)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "https" || u.Host != "pay.example.com" || u.Path != "/api/pay/submit" {
		t.Fatal("wrong submission target")
	}
	q := u.Query()
	if q.Get("name") != params.Get("name") || q.Get("money") != "1.00" || q.Get("space") != " \t" {
		t.Fatal("wire values changed")
	}
	canonical := "money=1.00&name= 中文😀 +% &out_trade_no=order-217&pid=1001&space= \t&timestamp=1793400000&type=alipay&zero=0"
	digest := sha256.Sum256([]byte(canonical))
	sig, err := base64.StdEncoding.Strict().DecodeString(q.Get("sign"))
	if err != nil || rsa.VerifyPKCS1v15(&merchantKey.PublicKey, crypto.SHA256, digest[:], sig) != nil {
		t.Fatal("signature does not match original decoded values")
	}
	if params.Get("pid") != "" || params.Get("sign") != "" {
		t.Fatal("caller parameters mutated")
	}
}

func TestRequestRejectsAmbiguousAndReservedParameters(t *testing.T) {
	c := client(t, "https://pay.example.com")
	for _, params := range []url.Values{{"name": {"a", "b"}}, {"": {"x"}}, {"a.b": {"x"}}, {"a b": {"x"}}, {"a[]": {"x"}}, {"a\x00": {"x"}}, {"a\n": {"x"}}, {"pid": {"1001"}}, {"timestamp": {"1"}}, {"sign": {"x"}}, {"sign_type": {"RSA"}}} {
		if _, err := c.SubmitURL(params); err == nil {
			t.Errorf("accepted ambiguous or reserved input keys: %q", params)
		}
	}
}

func TestFiveWrappersUseSameOriginPOSTAndVerifyResponses(t *testing.T) {
	received := make(chan string, 5)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.URL.Path
		if r.Method != "POST" || r.URL.RawQuery != "" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Error("unexpected request transport")
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		p := map[string]string{}
		for k, v := range r.PostForm {
			if len(v) != 1 {
				t.Fatal("duplicate output")
			}
			p[k] = v[0]
		}
		if p["pid"] != "1001" || p["sign_type"] != "RSA" || p["sign"] != fixtureSignature(p, merchantKey, false) {
			t.Error("request signature mismatch")
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write(signedJSON(map[string]string{"code": "0", "trade_no": "trade-217", "money": "1.00"}))
	}))
	defer s.Close()
	c := client(t, s.URL)
	methods := []struct {
		path string
		call func(context.Context, url.Values) (*Response, error)
	}{
		{"/api/pay/create", c.Create}, {"/api/pay/query", c.Query}, {"/api/pay/refund", c.Refund}, {"/api/pay/refundquery", c.RefundQuery}, {"/api/pay/close", c.Close},
	}
	for _, m := range methods {
		r, err := m.call(context.Background(), url.Values{"out_trade_no": {"order-217"}})
		if err != nil || r == nil || !r.Authenticated || r.Code != 0 || r.Fields["money"] != "1.00" {
			t.Fatalf("%s: invalid verified reply: %v", m.path, err)
		}
		if <-received != m.path {
			t.Error("wrong endpoint")
		}
	}
	if len(received) != 0 {
		t.Fatal("unexpected retry")
	}
}

func TestSignedAndUnsignedBusinessErrorsAreNeverSuccess(t *testing.T) {
	for _, signed := range []bool{false, true} {
		t.Run(fmt.Sprint(signed), func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if signed {
					w.Write(signedJSON(map[string]string{"code": "-1", "msg": "refund processing", "out_refund_no": "refund-217"}))
				} else {
					w.Write([]byte(`{"code":-1,"msg":"platform signing key unavailable"}`))
				}
			}))
			defer s.Close()
			r, err := client(t, s.URL).RefundQuery(context.Background(), url.Values{"out_refund_no": {"refund-217"}})
			var api *APIError
			if !errors.As(err, &api) || r == nil || r.Code != -1 || api.Authenticated != signed || r.Authenticated != signed {
				t.Fatalf("wrong error trust flag: %v", err)
			}
			if signed && r.Fields["out_refund_no"] != "refund-217" {
				t.Error("lost original refund identity")
			}
		})
	}
}

func TestResponseRejectsMissingWrongTamperedExpiredAndNestedSignatures(t *testing.T) {
	cases := map[string][]byte{"unsigned-success": []byte(`{"code":0}`), "duplicate": []byte(`{"code":-1,"code":0}`), "nested": []byte(`{"code":0,"data":{"money":"1.00"}}`), "trailing": []byte(`{"code":-1} {"code":0}`)}
	for _, name := range []string{"wrong-key", "tampered", "expired", "future", "bad-type"} {
		body := signedJSON(map[string]string{"code": "0", "money": "1.00"})
		var raw map[string]any
		json.Unmarshal(body, &raw)
		switch name {
		case "wrong-key":
			fields := map[string]string{}
			for k, v := range raw {
				fields[k] = fmt.Sprint(v)
			}
			raw["sign"] = fixtureSignature(fields, wrongKey, true)
		case "tampered":
			raw["money"] = "999.00"
		case "expired", "future":
			stamp := testNow.Add(-301 * time.Second)
			if name == "future" {
				stamp = testNow.Add(301 * time.Second)
			}
			raw["timestamp"] = fmt.Sprint(stamp.Unix())
			fields := map[string]string{}
			for k, v := range raw {
				fields[k] = fmt.Sprint(v)
			}
			raw["sign"] = fixtureSignature(fields, platformKey, true)
		case "bad-type":
			raw["sign_type"] = "MD5"
		}
		cases[name], _ = json.Marshal(raw)
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write(body)
			}))
			defer s.Close()
			if r, err := client(t, s.URL).Query(context.Background(), nil); err == nil || (r != nil && r.Authenticated) {
				t.Fatal("untrusted response accepted")
			}
		})
	}
}

func TestResponseScalarProjectionMatchesWireProfile(t *testing.T) {
	fields := map[string]string{"code": "0", "enabled": "1", "disabled": "", "nil": "", "space": " \t\r\n\x00\x0b", "zero": "0", "count": "9007199254740993", "name": "中文 +%"}
	fields["timestamp"], fields["sign_type"] = fmt.Sprint(testNow.Unix()), "RSA"
	fields["sign"] = fixtureSignature(fields, platformKey, true)
	raw := map[string]any{}
	for k, v := range fields {
		raw[k] = v
	}
	raw["code"] = 0
	raw["enabled"] = true
	raw["disabled"] = false
	raw["nil"] = nil
	raw["count"] = json.Number("9007199254740993")
	body, _ := json.Marshal(raw)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer s.Close()
	r, err := client(t, s.URL).Query(context.Background(), nil)
	if err != nil || r.Fields["count"] != "9007199254740993" || r.Fields["enabled"] != "1" || r.Fields["disabled"] != "" {
		t.Fatalf("scalar response failed: %v", err)
	}
}

func TestNotificationVerifiesFreshnessMerchantAndAllQueryFields(t *testing.T) {
	c := client(t, "https://pay.example.com")
	base := map[string]string{"pid": "1001", "trade_no": "trade-217", "out_trade_no": "order-217", "money": "1.00", "trade_status": "TRADE_SUCCESS", "tenant": "original-query", "space": " \t", "timestamp": fmt.Sprint(testNow.Unix()), "sign_type": "RSA"}
	values := func(fields map[string]string) url.Values {
		q := url.Values{}
		for k, v := range fields {
			q.Set(k, v)
		}
		q.Set("sign", fixtureSignature(fields, platformKey, true))
		return q
	}
	q := values(base)
	if p, err := c.VerifyNotification(q); err != nil || p["tenant"] != "original-query" {
		t.Fatalf("valid notification rejected: %v", err)
	}
	q.Set("tenant", "changed")
	if _, err := c.VerifyNotification(q); err == nil {
		t.Error("unsigned base query change accepted")
	}
	q = values(base)
	q.Add("pid", "1001")
	if _, err := c.VerifyNotification(q); err == nil {
		t.Error("duplicate callback accepted")
	}
	base["pid"] = "1002"
	if _, err := c.VerifyNotification(values(base)); err == nil {
		t.Error("other merchant accepted")
	}
	base["pid"] = "1001"
	for _, delta := range []int64{-301, 301} {
		base["timestamp"] = fmt.Sprint(testNow.Unix() + delta)
		if _, err := c.VerifyNotification(values(base)); err == nil {
			t.Error("expired/future callback accepted")
		}
	}
	for _, delta := range []int64{-300, 300} {
		base["timestamp"] = fmt.Sprint(testNow.Unix() + delta)
		if _, err := c.VerifyNotification(values(base)); err != nil {
			t.Error("inclusive boundary rejected")
		}
	}
	base["timestamp"] = "9223372036854775807"
	if _, err := c.VerifyNotification(values(base)); err == nil {
		t.Error("extreme timestamp accepted")
	}
}

func TestClientRejectsRedirectsOversizeUnexpectedTypesAndCancelledContexts(t *testing.T) {
	var targetCalls atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls.Add(1) }))
	defer target.Close()
	for _, kind := range []string{"redirect", "oversize", "content-type"} {
		t.Run(kind, func(t *testing.T) {
			var calls atomic.Int64
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				switch kind {
				case "redirect":
					http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
				case "oversize":
					w.Header().Set("Content-Type", "application/json")
					w.Write([]byte(strings.Repeat("x", MaxResponseBytes+1)))
				case "content-type":
					w.Header().Set("Content-Type", "text/html")
					w.Write([]byte(`<html>not JSON</html>`))
				}
			}))
			defer s.Close()
			if _, err := client(t, s.URL).Query(context.Background(), nil); err == nil {
				t.Fatal("invalid transport response accepted")
			}
			if calls.Load() != 1 || targetCalls.Load() != 0 {
				t.Fatal("automatic retry/redirect sent credentials")
			}
		})
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("cancelled request reached network") }))
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client(t, s.URL).Query(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("context cancellation lost: %v", err)
	}
}

func TestPEMRejectsUnexpectedPrefix(t *testing.T) {
	cfg := config("https://pay.example.com")
	cfg.MerchantPrivateKeyPEM = append([]byte("unexpected prefix\n"), cfg.MerchantPrivateKeyPEM...)
	if _, err := New(cfg); err == nil {
		t.Fatal("non-PEM prefix accepted")
	}
}

func TestPKCS8PrivateKeyAndPKCS1PublicKey(t *testing.T) {
	cfg := config("https://pay.example.com")
	der, err := x509.MarshalPKCS8PrivateKey(merchantKey)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MerchantPrivateKeyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	cfg.PlatformPublicKeyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&platformKey.PublicKey)})
	if _, err := New(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidUTF8JSONFailsClosed(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(append([]byte(`{"code":-1,"msg":"`), append([]byte{255}, []byte(`"}`)...)...))
	}))
	defer s.Close()
	if _, err := client(t, s.URL).Query(context.Background(), nil); !errors.Is(err, ErrProtocol) {
		t.Fatal("invalid UTF-8 was not rejected as a protocol violation")
	}
}

func TestConfiguredTimeoutBoundsOneCall(t *testing.T) {
	var calls atomic.Int64
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		select {
		case <-r.Context().Done():
		case <-time.After(500 * time.Millisecond):
		}
	}))
	defer s.Close()
	cfg := config(s.URL)
	cfg.Timeout = 100 * time.Millisecond
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseIdleConnections()
	started := time.Now()
	if _, err := c.Query(context.Background(), nil); err == nil {
		t.Fatal("timeout ignored")
	}
	if time.Since(started) > 2*time.Second || calls.Load() != 1 {
		t.Fatal("timeout unbounded or automatic retry")
	}
}

func TestConfigRequiresHTTPSOriginAndValidRSAKeys(t *testing.T) {
	for _, base := range []string{"http://pay.example.com", "http://localhost:1234", "ftp://pay.example.com", "https://user:pass@pay.example.com", "https://pay.example.com/path", "https://pay.example.com?pid=1", "https://pay.example.com/#fragment", "https://pay.example.com/?", "https://"} {
		if _, err := New(config(base)); err == nil {
			t.Errorf("unsafe base accepted: %q", base)
		}
	}
	cfg := config("http://127.0.0.1:1234")
	cfg.AllowLoopbackHTTP = false
	if _, err := New(cfg); err == nil {
		t.Error("loopback HTTP allowed without explicit opt-in")
	}
	for _, mutate := range []func(*Config){func(c *Config) { c.MerchantID = 0 }, func(c *Config) { c.MerchantPrivateKeyPEM = []byte("not a PEM key") }, func(c *Config) { c.PlatformPublicKeyPEM = []byte("not a PEM key") }, func(c *Config) { c.MerchantPrivateKeyPEM = append(c.MerchantPrivateKeyPEM, []byte("trailing")...) }, func(c *Config) { c.Timeout = -time.Second }} {
		cfg := config("https://pay.example.com")
		mutate(&cfg)
		if _, err := New(cfg); err == nil {
			t.Error("invalid configuration accepted")
		}
	}
}
