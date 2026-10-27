package server

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/captcha203"
)

const publicBrowserOrigin203 = "https://pay.example.invalid"
const publicBrowserCaptchaID203 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const publicBrowserCaptchaKey203 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

// This is a real verifier/HTTPS exchange with a local CA for the original
// official host names. It is not an implementation of the vendor browser SDK.
type publicBrowserCaptcha203 struct {
	registers atomic.Int64
	verifies  atomic.Int64
	rejected  atomic.Int64
	bad       atomic.Int64
	queries   atomic.Int64
	mu        sync.Mutex
	issued    map[string]string
}

func publicBrowserMD5203(value string) string {
	digest := md5.Sum([]byte(value))
	return hex.EncodeToString(digest[:])
}

func publicBrowserCaptchaClient203(t *testing.T, handler http.Handler) *http.Client {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	cert := &x509.Certificate{SerialNumber: big.NewInt(203), Subject: pkix.Name{CommonName: "Synthetic public203 official peer"}, DNSNames: []string{"api.geetest.com", "gcaptcha4.geetest.com"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	if err != nil {
		t.Fatal(err)
	}
	peer := httptest.NewUnstartedServer(handler)
	peer.Config.ErrorLog = log.New(io.Discard, "", 0)
	peer.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}}
	peer.StartTLS()
	t.Cleanup(peer.Close)
	local, err := url.Parse(peer.URL)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "api.geetest.com:443" && address != "gcaptcha4.geetest.com:443" {
			return nil, errors.New("public203 official peer escaped its local dial")
		}
		return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, local.Host)
	}}
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Timeout: 3 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (v *publicBrowserCaptcha203) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.TLS == nil || r.TLS.ServerName != r.Host || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
		v.bad.Add(1)
		http.Error(w, "invalid synthetic official request", 400)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if r.Host == "api.geetest.com" && r.URL.Path == "/register.php" && r.Method == "GET" {
		q := r.URL.Query()
		if len(q) != 6 || q.Get("gt") != publicBrowserCaptchaID203 || q.Get("digestmod") != "md5" || q.Get("sdk") != "php_3.0.0" || q.Get("json_format") != "1" || q.Get("client_type") != "web" || q.Get("ip_address") != "127.0.0.1" {
			v.bad.Add(1)
			http.Error(w, "invalid synthetic register", 400)
			return
		}
		raw := fmt.Sprintf("%032x", v.registers.Add(1))
		challenge := publicBrowserMD5203(raw + publicBrowserCaptchaKey203)
		v.mu.Lock()
		v.issued[challenge] = publicBrowserMD5203(publicBrowserCaptchaKey203 + "geetest" + challenge)
		v.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]string{"challenge": raw})
		return
	}
	if r.Method != "POST" || r.ParseForm() != nil {
		v.bad.Add(1)
		http.Error(w, "invalid synthetic validation", 400)
		return
	}
	if r.Host == "api.geetest.com" && r.URL.Path == "/validate.php" {
		v.mu.Lock()
		_, issued := v.issued[r.PostForm.Get("challenge")]
		v.mu.Unlock()
		code := r.PostForm.Get("seccode")
		if !issued || r.URL.RawQuery != "" || len(r.PostForm) != 6 || r.PostForm.Get("captchaid") != publicBrowserCaptchaID203 || r.PostForm.Get("sdk") != "php_3.0.0" || r.PostForm.Get("json_format") != "1" || r.PostForm.Get("ip_address") != "127.0.0.1" || code != "controlled-seccode203" && code != "rejected-seccode203" {
			v.bad.Add(1)
			http.Error(w, "invalid synthetic V3 binding", 400)
			return
		}
		v.verifies.Add(1)
		if code == "rejected-seccode203" {
			v.rejected.Add(1)
			code = "remote-rejected203"
		}
		json.NewEncoder(w).Encode(map[string]string{"seccode": publicBrowserMD5203(code)})
		return
	}
	if r.Host == "gcaptcha4.geetest.com" && r.URL.Path == "/validate" {
		lot := r.PostForm.Get("lot_number")
		mac := hmac.New(sha256.New, []byte(publicBrowserCaptchaKey203))
		mac.Write([]byte(lot))
		stamp, err := strconv.ParseInt(r.PostForm.Get("gen_time"), 10, 64)
		output := r.PostForm.Get("captcha_output")
		if len(r.URL.Query()) != 1 || r.URL.Query().Get("captcha_id") != publicBrowserCaptchaID203 || len(r.PostForm) != 5 || len(lot) != 32 || r.PostForm.Get("sign_token") != hex.EncodeToString(mac.Sum(nil)) || r.PostForm.Get("pass_token") != "controlled-pass203" || err != nil || stamp < time.Now().Add(-time.Minute).Unix() || stamp > time.Now().Add(time.Minute).Unix() || output != "controlled-output203" && output != "rejected-output203" {
			v.bad.Add(1)
			http.Error(w, "invalid synthetic V4 binding", 400)
			return
		}
		v.verifies.Add(1)
		result := "success"
		if output == "rejected-output203" {
			v.rejected.Add(1)
			result = "fail"
		}
		json.NewEncoder(w).Encode(map[string]any{"status": "success", "result": result, "captcha_args": map[string]string{"captcha_id": publicBrowserCaptchaID203, "lot_number": lot}})
		return
	}
	v.bad.Add(1)
	http.NotFound(w, r)
}

func TestPublicTestPay203ActualBrowserHTTPWithPrivateTLSAndNoConsoleIdentity(t *testing.T) {
	if os.Getenv("GOEPAY_PUBLIC_TESTPAY203_BROWSER") != "1" {
		t.Skip("GOEPAY_PUBLIC_TESTPAY203_BROWSER=1 uses only synthetic Go/PG/Chromium peers")
	}
	for _, version := range []int{0, 1} {
		t.Run("v"+strconv.Itoa(version+3), func(t *testing.T) {
			f := publicTestPay203Ready(t)
			if !f.s.Sandbox || !f.s.ProviderFlow.LocalSimulation {
				t.Fatal("public203 browser fixture must remain local simulation")
			}
			v := &publicBrowserCaptcha203{issued: map[string]string{}}
			provider := f.s.ProviderFlow.Providers[7].(*channel168Provider)
			provider.subchannelTestQuery = func(context.Context, adapter.QueryRequest) (adapter.QueryResult, error) {
				v.queries.Add(1)
				return adapter.QueryResult{}, adapter.ErrUnsupported
			}
			// The real engine and its existing controlled provider are retained.
			// There is no network payment implementation in this fixture.
			site191Save(t, f, "captcha_open_test", "1")
			site191Save(t, f, "captcha_id", publicBrowserCaptchaID203)
			site191Save(t, f, "captcha_version", strconv.Itoa(version))
			service, err := NewPublicTestPayService203ForMaster(f.s, captcha203.NewGeetestVerifier(publicBrowserCaptchaClient203(t, v)), f.s.CheckoutKey, publicBrowserOrigin203)
			if err != nil {
				t.Fatal(err)
			}
			f.s.PublicTestPay203 = service
			captchaSetKey203(t, f, publicBrowserCaptchaKey203)
			controlBytes := make([]byte, 32)
			if _, err = rand.Read(controlBytes); err != nil {
				t.Fatal(err)
			}
			control := hex.EncodeToString(controlBytes)
			var consoleReads, invalidCookies atomic.Int64
			handler := f.s.Handler()
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/__public203/") {
					if r.Header.Get("X-Synthetic-Public203-Control") != control || r.Method != "POST" {
						http.Error(w, "synthetic fixture control required", 403)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					var in struct{ Trade, Challenge string }
					if json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&in) != nil {
						http.Error(w, "synthetic control input", 400)
						return
					}
					switch r.URL.Path {
					case "/__public203/metrics":
						var financial string
						if err := f.p.QueryRow(r.Context(), `SELECT jsonb_build_object('orders',(SELECT count(*)FROM orders),'paid',(SELECT count(*)FROM orders WHERE status='paid'),'attempts',(SELECT count(*)FROM provider_attempts),'jobs',(SELECT count(*)FROM jobs),'journals',(SELECT count(*)FROM journals),'refunds',(SELECT count(*)FROM refunds),'operations',(SELECT count(*)FROM provider_operations))::text`).Scan(&financial); err != nil {
							http.Error(w, "synthetic metrics unavailable", 500)
							return
						}
						json.NewEncoder(w).Encode(map[string]any{"financial": json.RawMessage(financial), "sends": provider.sends.Load(), "registers": v.registers.Load(), "verifies": v.verifies.Load(), "rejected": v.rejected.Load(), "queries": v.queries.Load(), "bad": v.bad.Load(), "console_reads": consoleReads.Load(), "console_cookies": invalidCookies.Load()})
					case "/__public203/proof":
						v.mu.Lock()
						validate, exists := v.issued[in.Challenge]
						v.mu.Unlock()
						if version != 0 || !exists {
							http.Error(w, "actual issued challenge required", 400)
							return
						}
						json.NewEncoder(w).Encode(map[string]string{"geetest_challenge": in.Challenge, "geetest_validate": validate, "geetest_seccode": "controlled-seccode203"})
					case "/__public203/paid":
						var trade string
						if err := f.p.QueryRow(r.Context(), `SELECT trade_no FROM orders WHERE merchant_id=1 AND out_trade_no=$1 AND name='支付测试' AND amount=123 AND status='pending' AND param LIKE 'public-testpay203:%'`, in.Trade).Scan(&trade); err != nil || !f.s.Sandbox || !f.s.ProviderFlow.LocalSimulation || f.s.Store.MarkPaid(r.Context(), trade, "synthetic-public-browser203:"+in.Trade) != nil {
							http.Error(w, "current synthetic pending order required", 409)
							return
						}
						json.NewEncoder(w).Encode(map[string]bool{"paid": true})
					default:
						http.NotFound(w, r)
					}
					return
				}
				if strings.HasPrefix(r.URL.Path, "/console/v1/admin/") || strings.HasPrefix(r.URL.Path, "/console/v1/merchant/") {
					consoleReads.Add(1)
				}
				for _, name := range []string{"goepay_admin", "goepay_merchant"} {
					if _, err := r.Cookie(name); err == nil {
						invalidCookies.Add(1)
					}
				}
				handler.ServeHTTP(w, r)
			}))
			t.Cleanup(api.Close)
			root, err := filepath.Abs(filepath.Join("..", ".."))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "web", "e2e", "public-testpay203-http-browser.mjs"))
			cmd.Dir = filepath.Join(root, "web")
			cmd.Env = append(os.Environ(), "PUBLIC203_BROWSER_API="+api.URL, "PUBLIC203_BROWSER_CONTROL="+control, "PUBLIC203_BROWSER_VERSION="+strconv.Itoa(version), "PUBLIC203_BROWSER_ORIGIN="+publicBrowserOrigin203, "PUBLIC203_BROWSER_ARTIFACTS="+filepath.Join(root, ".tools", "public203-actual-http-browser", "v"+strconv.Itoa(version+3)))
			output, err := cmd.CombinedOutput()
			if err != nil {
				if len(output) > 7000 {
					output = output[len(output)-7000:]
				}
				t.Fatalf("public203 actual browser v%d: %v\n%s", version+3, err, output)
			}
			var orders, paid, attempts, consumed, used, refunds int
			if err = f.p.QueryRow(t.Context(), `SELECT (SELECT count(*)FROM orders),(SELECT count(*)FROM orders WHERE status='paid'),(SELECT count(*)FROM provider_attempts),(SELECT count(*)FROM captcha_challenges203 WHERE consumed_at IS NOT NULL),(SELECT count(*)FROM captcha_used_proofs203),(SELECT count(*)FROM refunds)`).Scan(&orders, &paid, &attempts, &consumed, &used, &refunds); err != nil || orders != 2 || paid != 2 || attempts != 2 || consumed != 2 || used != 2 || refunds != 0 {
				t.Fatal("public browser did not retain exactly two once-only synthetic payments", orders, paid, attempts, consumed, used, refunds, err)
			}
			if provider.sends.Load() != 2 || v.verifies.Load() != 4 || v.rejected.Load() != 2 || v.queries.Load() != 0 || v.bad.Load() != 0 || consoleReads.Load() != 0 || invalidCookies.Load() != 0 {
				t.Fatal("public browser crossed payment/captcha/console boundaries", provider.sends.Load(), v.verifies.Load(), v.rejected.Load(), v.queries.Load(), v.bad.Load(), consoleReads.Load(), invalidCookies.Load())
			}
		})
	}
}
