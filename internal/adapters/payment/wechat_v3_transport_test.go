package payment

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

type v3TLSTransport struct {
	upstream *httptest.Server
	t        *testing.T
}

func (x v3TLSTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "https" || r.URL.Host != "api.mch.weixin.qq.com" {
		x.t.Error("unexpected V3 origin")
		return nil, ErrAuthentication
	}
	copy := r.Clone(r.Context())
	target, _ := url.Parse(x.upstream.URL)
	copy.URL.Scheme = target.Scheme
	copy.URL.Host = target.Host
	return x.upstream.Client().Transport.RoundTrip(copy)
}
func v3SignedResponse(t *testing.T, w http.ResponseWriter, key *rsa.PrivateKey, status int, body, serial string) {
	t.Helper()
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	n := "synthetic-response-nonce"
	sum := sha256.Sum256([]byte(stamp + "\n" + n + "\n" + body + "\n"))
	sig, e := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if e != nil {
		t.Fatal(e)
	}
	w.Header().Set("Wechatpay-Timestamp", stamp)
	w.Header().Set("Wechatpay-Nonce", n)
	w.Header().Set("Wechatpay-Serial", serial)
	w.Header().Set("Wechatpay-Signature", base64.StdEncoding.EncodeToString(sig))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	io.WriteString(w, body)
}
func TestWechatV3TransportAuthenticatesExactRequestAndResponseWithoutRetry(t *testing.T) {
	merchant, _ := rsa.GenerateKey(rand.Reader, 2048)
	platform, _ := rsa.GenerateKey(rand.Reader, 2048)
	mode, calls := "ok", 0
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		raw, _ := io.ReadAll(r.Body)
		fields := map[string]string{}
		for _, m := range regexp.MustCompile(`([a-z_]+)="([^"]+)"`).FindAllStringSubmatch(r.Header.Get("Authorization"), -1) {
			fields[m[1]] = m[2]
		}
		canonical := r.Method + "\n" + r.URL.RequestURI() + "\n" + fields["timestamp"] + "\n" + fields["nonce_str"] + "\n" + string(raw) + "\n"
		hash := sha256.Sum256([]byte(canonical))
		sig, _ := base64.StdEncoding.DecodeString(fields["signature"])
		if !strings.HasPrefix(r.Header.Get("Authorization"), "WECHATPAY2-SHA256-RSA2048 ") || fields["mchid"] != "local-mch" || fields["serial_no"] != "MERCHANT_SERIAL" || rsa.VerifyPKCS1v15(&merchant.PublicKey, crypto.SHA256, hash[:], sig) != nil {
			t.Error("incorrect V3 canonical request")
		}
		switch mode {
		case "ok":
			v3SignedResponse(t, w, platform, 200, `{"prepay_id":"prepay-local"}`, "PUB_KEY_ID_LOCAL")
		case "unknown-key":
			v3SignedResponse(t, w, platform, 200, `{"prepay_id":"prepay-local"}`, "PUB_KEY_ID_FOREIGN")
		case "duplicate":
			v3SignedResponse(t, w, platform, 200, `{"amount":{"total":1,"total":2}}`, "PUB_KEY_ID_LOCAL")
		case "unsigned-error":
			w.WriteHeader(400)
			io.WriteString(w, `{"code":"PARAM_ERROR"}`)
		case "signed-error":
			v3SignedResponse(t, w, platform, 400, `{"code":"PARAM_ERROR"}`, "PUB_KEY_ID_LOCAL")
		case "server-error":
			v3SignedResponse(t, w, platform, 500, `{"code":"SYSTEM_ERROR"}`, "PUB_KEY_ID_LOCAL")
		case "redirect":
			http.Redirect(w, r, "https://foreign.example.invalid/", 307)
		case "unsigned-204":
			w.WriteHeader(204)
		case "signed-204":
			v3SignedResponse(t, w, platform, 204, "", "PUB_KEY_ID_LOCAL")
		case "duplicate-header":
			recorder := httptest.NewRecorder()
			v3SignedResponse(t, recorder, platform, 200, `{"ok":true}`, "PUB_KEY_ID_LOCAL")
			for name, values := range recorder.Header() {
				w.Header()[name] = values
			}
			w.Header().Add("Wechatpay-Nonce", "second")
			w.Write(recorder.Body.Bytes())
		case "tampered-body":
			recorder := httptest.NewRecorder()
			v3SignedResponse(t, recorder, platform, 200, `{"ok":true}`, "PUB_KEY_ID_LOCAL")
			for name, values := range recorder.Header() {
				w.Header()[name] = values
			}
			w.Write([]byte(`{"ok":false}`))
		case "oversized":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, strings.Repeat("x", (1<<20)+1))
		}
	}))
	defer up.Close()
	c, e := newWechatV3Client(WechatV3Config{AppID: "local-app", MerchantID: "local-mch", MerchantSerial: "MERCHANT_SERIAL", PrivateKey: merchant, APIv3Key: []byte("0123456789abcdef0123456789abcdef"), VerificationKeys: map[string]*rsa.PublicKey{"PUB_KEY_ID_LOCAL": &platform.PublicKey}, HTTPClient: &http.Client{Transport: v3TLSTransport{up, t}}})
	if e != nil {
		t.Fatal(e)
	}
	for _, test := range []struct {
		mode           string
		unknown, known bool
	}{{"ok", false, false}, {"unknown-key", true, false}, {"duplicate", true, false}, {"unsigned-error", true, false}, {"signed-error", false, true}, {"server-error", true, false}, {"redirect", true, false}, {"unsigned-204", true, false}, {"signed-204", false, false}, {"duplicate-header", true, false}, {"tampered-body", true, false}, {"oversized", true, false}} {
		t.Run(test.mode, func(t *testing.T) {
			mode = test.mode
			before := calls
			_, e := c.call(context.Background(), "POST", "/v3/test?mchid=local-mch", map[string]any{"amount": 101})
			var unknown *UnknownOutcome
			var known *ProviderError
			if errors.As(e, &unknown) != test.unknown || errors.As(e, &known) != test.known || test.mode == "ok" && e != nil {
				t.Fatalf("unexpected result %T %v", e, e)
			}
			if calls != before+1 {
				t.Fatal("financial request was retried")
			}
		})
	}
}
