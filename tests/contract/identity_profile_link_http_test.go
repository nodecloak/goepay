package contract_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/channelconfig"
	"goepay/internal/identity"
)

// Only the official hostname's TCP destination is mapped locally. TLS chain
// and hostname verification remain enabled for both real HTTP requests.
func identityLinkTLSClient(t *testing.T, handler http.Handler) *http.Client {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	cert := &x509.Certificate{SerialNumber: big.NewInt(67), DNSNames: []string{"api.weixin.qq.com"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	up := httptest.NewUnstartedServer(handler)
	up.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	up.StartTLS()
	t.Cleanup(up.Close)
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	target, _ := url.Parse(up.URL)
	return &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "api.weixin.qq.com:443" {
			t.Errorf("nonofficial address: %s", address)
			return nil, adapter.ErrInvalid
		}
		return (&net.Dialer{}).DialContext(ctx, network, target.Host)
	}}}
}

func TestIdentityProfileLinkHTTPCommittedClaimAndNoResend(t *testing.T) {
	for _, mode := range []string{"ready", "rejected", "unknown", "revoked", "concurrent"} {
		t.Run(mode, func(t *testing.T) {
			s, p, _ := fixture(t)
			s.Sandbox = false
			s.IdentityProfileEnabled = true
			s.IdentityProfileCallbackBaseURL = "https://pay.example.invalid"
			s.Sessions = identity.NewPersistentSessions(p, true)
			token, csrf, err := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1, AuthVersion: 1})
			if err != nil {
				t.Fatal(err)
			}
			var tokens, links atomic.Int32
			entered, release := make(chan struct{}), make(chan struct{})
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
			}()
			client := identityLinkTLSClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var claimed bool
				if e := p.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM profile_identity_links WHERE status='creating') AND (SELECT count(*) FROM profile_identity_link_events WHERE operation IN ('created','claimed'))=2`).Scan(&claimed); e != nil || !claimed {
					t.Error("I/O before durable original claim", e)
					w.WriteHeader(500)
					return
				}
				if r.URL.Path == "/cgi-bin/stable_token" {
					tokens.Add(1)
					w.Write([]byte(`{"access_token":"synthetic-only-access","expires_in":7200}`))
					return
				}
				if r.URL.Path != "/wxa/generate_urllink" {
					t.Error("wrong API")
					w.WriteHeader(500)
					return
				}
				links.Add(1)
				if mode == "concurrent" {
					close(entered)
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
				}
				if mode == "unknown" {
					w.WriteHeader(503)
					w.Write([]byte("provider-private-error"))
					return
				}
				if mode == "rejected" {
					w.Write([]byte(`{"errcode":85401,"errmsg":"private-rejection"}`))
					return
				}
				if mode == "revoked" {
					if _, e := p.Exec(context.Background(), `UPDATE merchants SET enabled=false WHERE id=1`); e != nil {
						t.Error(e)
					}
				}
				json.NewEncoder(w).Encode(map[string]any{"errcode": 0, "errmsg": "ok", "url_link": "https://wxaurl.cn/synthetic-identity-ticket"})
			}))
			mini, err := adapter.NewWechatMiniOAuth(adapter.WechatOAuthConfig{AppID: "independent-app", AppSecret: "synthetic-secret"})
			if err != nil {
				t.Fatal(err)
			}
			// The Windows test process and Docker PostgreSQL can have different
			// wall clocks. Keep this synthetic provider on the same clock as the
			// original database-issued intent, without relaxing production expiry
			// or the adapter's future-birth validation.
			providerClock := func() time.Time {
				clockContext, cancelClock := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancelClock()
				var now time.Time
				if err := p.QueryRow(clockContext, "SELECT pg_catalog.clock_timestamp()").Scan(&now); err != nil {
					t.Errorf("synthetic provider clock unavailable: %v", err)
					return time.Time{}
				}
				return now
			}
			link, err := adapter.NewWechatMiniLink(adapter.WechatMiniLinkConfig{AppID: "independent-app", AppSecret: "synthetic-secret", IdentityPath: "pages/identity/index", HTTPClient: client, Now: providerClock})
			if err != nil {
				t.Fatal(err)
			}
			profile := channelconfig.IdentityProfile{ID: 11, Revision: 1, ChannelID: 7, Provider: "wxpay", Kind: "mini", AppID: "independent-app", Path: "pages/identity/index", MerchantIDs: []int64{1}, Fingerprint: strings.Repeat("f", 64), Mini: mini, MiniLink: link, URLLinkAvailable: true}
			s.IdentityProfiles = map[int64]channelconfig.IdentityProfile{11: profile}
			s.MerchantChannels = map[int64][]int64{1: {7}}
			h := s.Handler()
			base := "/console/v1/merchant/identity-profile-lookups"
			cap := lookupHTTP(t, h, "GET", base+"/capabilities", "", token, "merchant", csrf)
			if cap.Code != 200 || lookupData(t, cap)["url_link_available"] != true {
				t.Fatal("URL Link capability missing", cap.Code, cap.Body.String())
			}
			created := lookupHTTP(t, h, "POST", base, `{"profile_id":"11","profile_revision":"1"}`, token, "merchant", csrf)
			if created.Code != 201 {
				t.Fatal(created.Code, created.Body.String())
			}
			data := lookupData(t, created)
			id := data["request"].(map[string]any)["request_id"].(string)
			state := data["launch"].(map[string]any)["body"].(map[string]any)["state"].(string)
			path := base + "/" + id + "/url-link"
			body := `{"state":"` + state + `"}`
			for _, invalid := range []string{`{}`, `{"state":null}`, `{"state":"` + state + `","state":"` + state + `"}`, `{"state":"` + state + `","redirect":"https://evil.invalid"}`} {
				w := lookupHTTP(t, h, "POST", path, invalid, token, "merchant", csrf)
				if w.Code != 400 && w.Code != 409 {
					t.Fatal("invalid body", w.Code, w.Body.String())
				}
			}
			if w := lookupHTTP(t, h, "POST", path, body, token, "merchant", ""); w.Code != 403 {
				t.Fatal("CSRF", w.Code)
			}
			other, otherCSRF, e := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1, AuthVersion: 1})
			if e != nil {
				t.Fatal(e)
			}
			if w := lookupHTTP(t, h, "POST", path, body, other, "merchant", otherCSRF); w.Code != 403 {
				t.Fatal("other original session", w.Code)
			}
			if tokens.Load() != 0 || links.Load() != 0 {
				t.Fatal("invalid request performed I/O")
			}
			var generated *httptest.ResponseRecorder
			if mode == "concurrent" {
				done := make(chan *httptest.ResponseRecorder, 1)
				go func() {
					r := httptest.NewRequest("POST", path, strings.NewReader(body))
					r.Header.Set("Content-Type", "application/json")
					r.Header.Set("X-CSRF-Token", csrf)
					r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
					w := httptest.NewRecorder()
					h.ServeHTTP(w, r)
					done <- w
				}()
				select {
				case <-entered:
				case <-time.After(5 * time.Second):
					t.Fatal("original link POST did not reach local TLS endpoint")
				}
				duplicate := lookupHTTP(t, h, "POST", path, body, token, "merchant", csrf)
				if duplicate.Code != 200 || lookupData(t, duplicate)["status"] != "creating" || links.Load() != 1 || tokens.Load() != 1 {
					t.Fatal("concurrent generation was not blocked", duplicate.Code, duplicate.Body.String())
				}
				observed := lookupHTTP(t, h, "GET", path, "", token, "merchant", csrf)
				if observed.Code != 200 || lookupData(t, observed)["status"] != "creating" {
					t.Fatal("local creating status unavailable", observed.Code)
				}
				close(release)
				select {
				case generated = <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("original generation did not complete")
				}
			} else {
				generated = lookupHTTP(t, h, "POST", path, body, token, "merchant", csrf)
			}
			if mode == "revoked" {
				if generated.Code == 200 || strings.Contains(generated.Body.String(), "synthetic-identity-ticket") {
					t.Fatal("revoked result disclosed", generated.Code, generated.Body.String())
				}
			} else {
				if generated.Code != 200 {
					t.Fatal(generated.Code, generated.Body.String())
				}
				result := lookupData(t, generated)
				expected := mode
				if mode == "rejected" {
					expected = "failed"
				}
				if mode == "concurrent" {
					expected = "ready"
				}
				if result["status"] != expected || result["purpose"] != "wechat_mini_identity_url_link" || result["request_id"] != id {
					t.Fatal("wrong link result", result)
				}
				if (mode == "ready" || mode == "concurrent") != (result["url_link"] == "https://wxaurl.cn/synthetic-identity-ticket") {
					t.Fatal("link disclosure", result)
				}
			}
			if strings.Contains(generated.Body.String(), "private-") || strings.Contains(generated.Body.String(), "synthetic-only-access") || strings.Contains(generated.Body.String(), state) {
				t.Fatal("secret/provider error leak")
			}
			for i := 0; i < 3; i++ {
				lookupHTTP(t, h, "POST", path, body, token, "merchant", csrf)
				lookupHTTP(t, h, "GET", path, "", token, "merchant", csrf)
			}
			if tokens.Load() != 1 || links.Load() != 1 {
				t.Fatal("link generation replayed", tokens.Load(), links.Load())
			}
			for _, table := range []string{"orders", "provider_attempts", "ledger_entries", "jobs"} {
				var n int
				if e := p.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); e != nil || n != 0 {
					t.Fatal("identity link changed financial state", table, n, e)
				}
			}
		})
	}
}
