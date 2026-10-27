package server

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
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
	"strings"
	"sync/atomic"
	"testing"
	"time"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/channelconfig"
	"goepay/internal/compat"
)

const nativeBrowserOrigin202 = "https://cashier.example.invalid"
const nativeBrowserTicket202 = "synthetic-browser202-ticket"

type nativeBrowserFixture202 struct {
	f                    buyer104MerchantFixture
	tokens, tickets, bad atomic.Int64
	assertPaymentIO      func()
}

// Only the fixed official hostname's TCP dial is redirected. A private test CA
// and api.weixin.qq.com certificate still undergo ordinary TLS verification.
func nativeBrowserWechatClient202(t *testing.T, handler http.Handler) *http.Client {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	cert := &x509.Certificate{SerialNumber: big.NewInt(202), Subject: pkix.Name{CommonName: "Synthetic native202 official peer"}, DNSNames: []string{"api.weixin.qq.com"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
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
	return &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "api.weixin.qq.com:443" {
			return nil, errors.New("native202 official peer escaped its local dial")
		}
		return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, local.Host)
	}}}
}

func nativeBrowserWechat202(t *testing.T) *nativeBrowserFixture202 {
	t.Helper()
	setup, f := merchantScan192Ready(t)
	out := &nativeBrowserFixture202{f: f}
	out.assertPaymentIO = func() {
		if setup.creates.Load() != 0 || setup.queries.Load() != 0 || setup.refunds.Load() != 0 || setup.bad.Load() != 0 {
			t.Fatal("native browser reached a payment provider", setup.creates.Load(), setup.queries.Load(), setup.refunds.Load(), setup.bad.Load())
		}
	}
	client := nativeBrowserWechatClient202(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "api.weixin.qq.com" || r.TLS == nil || r.TLS.ServerName != "api.weixin.qq.com" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			out.bad.Add(1)
			http.Error(w, "invalid synthetic official request", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/cgi-bin/stable_token":
			out.tokens.Add(1)
			var fields map[string]string
			if r.Method != "POST" || r.URL.RawQuery != "" || json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&fields) != nil || len(fields) != 3 || fields["appid"] != "wx-browser202" || fields["secret"] != "synthetic-browser202-secret" || fields["grant_type"] != "client_credential" {
				out.bad.Add(1)
				http.Error(w, "invalid synthetic token request", 400)
				return
			}
			io.WriteString(w, `{"access_token":"synthetic-browser202-token","expires_in":7200}`)
		case "/cgi-bin/ticket/getticket":
			out.tickets.Add(1)
			if r.Method != "GET" || len(r.URL.Query()) != 2 || r.URL.Query().Get("access_token") != "synthetic-browser202-token" || r.URL.Query().Get("type") != "jsapi" {
				out.bad.Add(1)
				http.Error(w, "invalid synthetic ticket request", 400)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"errcode": 0, "ticket": nativeBrowserTicket202, "expires_in": 7200})
		default:
			out.bad.Add(1)
			http.NotFound(w, r)
		}
	}))
	var channel int64
	for _, id := range f.s.MerchantChannels[1] {
		if p := f.s.ProviderFlow.Providers[id]; p != nil && p.ID() == "wxpay" {
			channel = id
			break
		}
	}
	if channel == 0 {
		t.Fatal("native202 browser requires the actual registered WeChat scan grant")
	}
	imp := channelconfig.Import{Version: 1, Channels: []channelconfig.Channel{{ID: channel, Plugin: "wxpay", Config: map[string]string{"account_mode": "direct"}}}, IdentityProfiles: []channelconfig.IdentityProfileSpec{{ID: 202, Revision: 1, ChannelID: channel, Enabled: true, Provider: "wxpay", Kind: "web", AppID: "wx-browser202", AccountMode: "direct", MerchantIDs: channelconfig.IdentityMerchantIDs{1}, Config: map[string]string{"appsecret": "synthetic-browser202-secret"}}}}
	profiles, err := channelconfig.BuildScanJSProfiles202(imp)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := adapter.NewWechatJSSDK202(adapter.WechatJSSDKConfig202{AppID: "wx-browser202", AppSecret: "synthetic-browser202-secret", HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	profile := profiles[202]
	profile.Signer = signer
	profiles[202] = profile
	f.s.ScanJSProfiles202 = profiles
	f.s.ScanJSSiteOrigin202 = nativeBrowserOrigin202
	state := channel168Data(t, scanJSSettingsRequest202(f.channel168Fixture, "GET", ""))
	raw, _ := json.Marshal(map[string]any{"enabled": true, "profile_id": "202", "profile_revision": "1", "expected_fingerprint": state["fingerprint"]})
	if w := scanJSSettingsRequest202(f.channel168Fixture, "PATCH", string(raw)); w.Code != 200 {
		t.Fatal("native202 real browser authorization", w.Code, w.Body.String())
	}
	return out
}

// Preparation uses an actual Alipay RSA2 adapter and protected parent account.
// Any payment/query request is a test failure; the browser never presses apply.
func nativeBrowserAlipay202(t *testing.T) *nativeBrowserFixture202 {
	t.Helper()
	f := channel168Local(t)
	out := &nativeBrowserFixture202{}
	merchant, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	platform, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var providerIO atomic.Int64
	peer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerIO.Add(1)
		http.Error(w, "payment is forbidden in native202 browser preparation", 500)
	}))
	t.Cleanup(peer.Close)
	origin := channelconfig.Channel{ID: 8, Plugin: "alipay", AppTypes: []string{"3"}, Config: map[string]string{"appid": "alipay-browser202", "appmchid": "2088000000000202", "appsecret": parent180Private(merchant), "appkey": parent180Public(platform), "sign_type": "0", "enable_scan_pay": "false"}}
	factory, err := channelconfig.NewParentChannelFactory(origin, channelconfig.ParentBuildOptions{BuildOptions: channelconfig.BuildOptions{AllowLive: true}})
	if err != nil {
		t.Fatal(err)
	}
	local := &parent180TLSFactory{ParentChannelFactory: factory, endpoint: peer.URL, localProvider: func(c channelconfig.Channel) (adapter.Provider, error) {
		key, e := compat.ParsePrivateKey(c.Config["appsecret"])
		if e != nil {
			return nil, e
		}
		pub, e := compat.ParsePublicKey(c.Config["appkey"])
		if e != nil {
			return nil, e
		}
		methods := []string{"native"}
		if c.Config["enable_scan_pay"] == "true" {
			methods = append(methods, "scan")
		}
		client := peer.Client()
		client.Timeout = 2 * time.Second
		return adapter.NewAlipay(adapter.AlipayConfig{AppID: c.Config["appid"], SellerID: c.Config["appmchid"], PrivateKey: key, PublicKey: pub, Methods: methods, GatewayURL: peer.URL, HTTPClient: client})
	}}
	bootstrap, _, err := local.BootstrapSnapshot(8)
	if err != nil {
		t.Fatal(err)
	}
	f.s.ProviderFlow.Providers[8] = bootstrap.Payment
	f.s.ProviderFlow.MerchantChannels = f.s.MerchantChannels
	f.s.ProviderFlow.ParentFactories = map[int64]adapter.ParentAccountFactory{8: local}
	if err = f.s.ProviderFlow.InstallParentAccountFactories(f.s.ProviderFlow.ParentFactories); err != nil {
		t.Fatal(err)
	}
	templates := channel168Data(t, parent180HTTP(f, "GET", "/templates", ""))["items"].([]any)
	config := map[string]any{}
	for k, v := range origin.Config {
		config[k] = map[string]string{"operation": "replace", "value": v}
	}
	config["enable_scan_pay"] = map[string]string{"operation": "replace", "value": "true"}
	row := parent180Post(t, f, map[string]any{"source_channel_id": "8", "expected_source_fingerprint": templates[0].(map[string]any)["fingerprint"], "request_id": "20220220220220220220220220220220", "name": "Synthetic native202 Alipay", "rate": "99.50", "mode": 0, "account": map[string]any{"app_types": origin.AppTypes, "config": config, "certificates": map[string]any{}}})
	raw, _ := json.Marshal(map[string]any{"expected_fingerprint": row["fingerprint"], "enabled": true})
	channel168Data(t, parent180HTTP(f, "PATCH", "/"+row["id"].(string), string(raw)))
	out.f = buyer190MerchantOn(t, f)
	out.assertPaymentIO = func() {
		if providerIO.Load() != 0 {
			t.Fatal("native202 browser sent an Alipay payment or query", providerIO.Load())
		}
	}
	return out
}

func TestNativeScan202ActualBrowserHTTPPreparesWithoutAnyCharge(t *testing.T) {
	if os.Getenv("GOEPAY_NATIVE_SCAN202_BROWSER") != "1" {
		t.Skip("GOEPAY_NATIVE_SCAN202_BROWSER=1 uses only synthetic Go/PG/Chromium peers")
	}
	for _, kind := range []string{"wechat", "alipay"} {
		t.Run(kind, func(t *testing.T) {
			var fixture *nativeBrowserFixture202
			if kind == "wechat" {
				fixture = nativeBrowserWechat202(t)
			} else {
				fixture = nativeBrowserAlipay202(t)
			}
			f := fixture.f
			var forbidden, jsconfigs, prepares atomic.Int64
			handler := f.s.Handler()
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/console/v1/merchant/scanpay") {
					cookie, e := r.Cookie("goepay_merchant")
					if e != nil || cookie.Value != f.merchantToken {
						fixture.bad.Add(1)
						http.Error(w, "real persistent merchant session required", 403)
						return
					}
					if r.Method == "POST" && r.Header.Get("X-CSRF-Token") != f.merchantCSRF {
						fixture.bad.Add(1)
						http.Error(w, "real CSRF required", 403)
						return
					}
					if strings.HasSuffix(r.URL.Path, "/apply") || strings.HasSuffix(r.URL.Path, "/query") {
						forbidden.Add(1)
						http.Error(w, "native202 browser never confirms a charge", 403)
						return
					}
					if r.URL.Path == "/console/v1/merchant/scanpay/jsconfig" {
						jsconfigs.Add(1)
					}
					if r.URL.Path == "/console/v1/merchant/scanpay/prepare" {
						prepares.Add(1)
					}
				}
				handler.ServeHTTP(w, r)
			}))
			t.Cleanup(api.Close)
			root, err := filepath.Abs(filepath.Join("..", ".."))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "web", "e2e", "native-scan202-http-browser.mjs"))
			cmd.Dir = filepath.Join(root, "web")
			cmd.Env = append(os.Environ(), "NATIVE202_BROWSER_API="+api.URL, "NATIVE202_BROWSER_COOKIE="+f.merchantToken, "NATIVE202_BROWSER_KIND="+kind, "NATIVE202_BROWSER_ORIGIN="+nativeBrowserOrigin202, "NATIVE202_BROWSER_ARTIFACTS="+filepath.Join(root, ".tools", "native202-actual-http-browser", kind))
			output, err := cmd.CombinedOutput()
			if err != nil {
				if len(output) > 5000 {
					output = output[len(output)-5000:]
				}
				t.Fatalf("native202 actual browser %s: %v\n%s", kind, err, output)
			}
			if prepares.Load() != 2 || forbidden.Load() != 0 || fixture.bad.Load() != 0 {
				t.Fatal("browser bypassed prepare-only or persistent session boundary", prepares.Load(), forbidden.Load(), fixture.bad.Load())
			}
			if kind == "wechat" && (jsconfigs.Load() != 4 || fixture.tokens.Load() != 1 || fixture.tickets.Load() != 1) {
				t.Fatal("real native202 configuration did not use the fixed TLS peer and shared credential cache", jsconfigs.Load(), fixture.tokens.Load(), fixture.tickets.Load())
			}
			if kind == "alipay" && jsconfigs.Load() != 0 {
				t.Fatal("Alipay scan requested a WeChat configuration", jsconfigs.Load())
			}
			var orders, sources, intents, effects int
			if err = f.p.QueryRow(t.Context(), `SELECT(SELECT count(*)FROM orders),(SELECT count(*)FROM merchant_scan_order_sources106),(SELECT count(*)FROM merchant_scan_intents106),(SELECT count(*)FROM provider_attempts)+(SELECT count(*)FROM journals)+(SELECT count(*)FROM jobs)+(SELECT count(*)FROM provider_operations)`).Scan(&orders, &sources, &intents, &effects); err != nil || orders != 2 || sources != 2 || intents != 2 || effects != 0 {
				t.Fatal("native browser did not retain exactly two unsent real preparations", orders, sources, intents, effects, err)
			}
			var paid bool
			if err = f.p.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM orders WHERE status='paid')`).Scan(&paid); err != nil || paid {
				t.Fatal("native browser falsely confirmed a payment", paid, err)
			}
			fixture.assertPaymentIO()
		})
	}
}
