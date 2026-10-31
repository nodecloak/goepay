package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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
	"goepay/internal/compat"
)

type checkoutBrowserProvider struct {
	*channel168CConfirmedProvider
	kind    string
	queries atomic.Int64
}

func (p *checkoutBrowserProvider) ID() string { return p.kind }
func (p *checkoutBrowserProvider) Query(ctx context.Context, r adapter.QueryRequest) (adapter.QueryResult, error) {
	p.queries.Add(1)
	return p.channel168CConfirmedProvider.Query(ctx, r)
}

func TestCheckoutPollRealBrowserLocalProvider(t *testing.T) {
	if os.Getenv("GOEPAY_CHECKOUT_BROWSER") != "1" {
		t.Skip("GOEPAY_CHECKOUT_BROWSER=1 requires local Chromium and synthetic PostgreSQL")
	}
	if os.Getenv("GOEPAY_TEST_DATABASE_URL") == "" {
		t.Skip("synthetic PostgreSQL URL required")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("local Node.js required")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "web", "node_modules", "@playwright", "test")); err != nil {
		t.Skip("local Playwright package required")
	}
	for _, scenario := range []struct {
		name, kind string
		expired    bool
	}{{"wxpay", "wxpay", false}, {"alipay", "alipay", false}, {"expired", "wxpay", true}} {
		t.Run(scenario.name, func(t *testing.T) {
			kind := scenario.kind
			f := channel168CConfirmedLocal(t)
			provider := &checkoutBrowserProvider{channel168CConfirmedProvider: f.s.ProviderFlow.Providers[7].(*channel168CConfirmedProvider), kind: kind}
			f.s.ProviderFlow.Providers[7] = provider
			key := make([]byte, 32)
			if _, err := rand.Read(key); err != nil {
				t.Fatal(err)
			}
			secret := hex.EncodeToString(key)
			var trade string
			var confirms atomic.Int64
			var merchantHits atomic.Int64
			merchant := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Host != "merchant.example.invalid" || r.URL.Path != "/return" || r.URL.Query().Get("trade_no") != trade {
					http.Error(w, "unexpected merchant target", 400)
					return
				}
				merchantHits.Add(1)
				w.Header().Set("Content-Type", "text/html")
				_, _ = w.Write([]byte("<title>Synthetic merchant return</title>"))
			}))
			defer merchant.Close()
			_, merchantPort, err := net.SplitHostPort(merchant.Listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			app := f.s.Handler()
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if scenario.expired && r.Method == "GET" && r.URL.Path == "/checkout/"+trade+"/status" {
					http.Error(w, "expired", http.StatusForbidden)
					return
				}
				if r.URL.Path == "/__checkout_browser/confirm" {
					if r.Method != "POST" || r.Header.Get("X-Local-Checkout-Test") != secret || !confirms.CompareAndSwap(0, 1) {
						http.Error(w, "forbidden", http.StatusForbidden)
						return
					}
					if _, err := f.s.ProviderFlow.Query(r.Context(), 1, trade); err != nil {
						http.Error(w, "confirmation failed", http.StatusInternalServerError)
						return
					}
					w.WriteHeader(http.StatusNoContent)
					return
				}
				app.ServeHTTP(w, r)
			}))
			defer api.Close()
			params := compat.Params{"pid": "1", "out_trade_no": "browser-poll-" + kind, "type": kind, "name": "synthetic browser item", "money": "1.00", "channel_id": "7", "method": "native", "notify_url": "https://merchant.example.invalid/notify", "return_url": "https://merchant.example.invalid/return", "sign_type": "MD5"}
			params["sign"] = compat.SignMD5(params, "test-secret")
			form := url.Values{}
			for k, v := range params {
				form.Set(k, v)
			}
			r := httptest.NewRequest("POST", "/mapi.php", strings.NewReader(form.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			f.s.Handler().ServeHTTP(w, r)
			var result struct {
				Code int `json:"code"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Code != 1 {
				t.Fatalf("synthetic create: %d %s %v", w.Code, w.Body.String(), err)
			}
			o, err := f.s.Store.Order(t.Context(), 1, params["out_trade_no"])
			if err != nil {
				t.Fatal(err)
			}
			trade = o.TradeNo
			// The provider accepts only the original HTTPS callback origin; once
			// the synthetic attempt exists, serve checkout/return on loopback.
			f.s.ProviderFlow.CallbackBaseURL = api.URL
			ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "web", "e2e", "checkout-poll-local-browser.mjs"))
			cmd.Dir = filepath.Join(root, "web")
			cmd.Env = append(os.Environ(), "CHECKOUT_BROWSER_API="+api.URL, "CHECKOUT_BROWSER_URL="+f.s.checkoutURL(trade, "checkout"), "CHECKOUT_BROWSER_CONTROL="+secret, "CHECKOUT_BROWSER_TRADE="+trade, "CHECKOUT_BROWSER_MERCHANT_PORT="+merchantPort)
			if scenario.expired {
				cmd.Env = append(cmd.Env, "CHECKOUT_BROWSER_EXPIRED=1")
			}
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("local browser: %v\n%s", err, output)
			}
			var orders, attempts, receipts int
			if err = f.p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM orders),(SELECT count(*) FROM provider_attempts),(SELECT count(*) FROM verified_payment_receipts)`).Scan(&orders, &attempts, &receipts); err != nil {
				t.Fatal(err)
			}
			paid, err := f.s.Store.Order(t.Context(), 1, params["out_trade_no"])
			if scenario.expired {
				if err != nil || paid.Status == "paid" || orders != 1 || attempts != 1 || receipts != 0 || provider.sends.Load() != 1 || provider.queries.Load() != 0 || confirms.Load() != 0 || merchantHits.Load() != 0 {
					t.Fatalf("expired checkout changed payment: status=%s orders=%d attempts=%d receipts=%d sends=%d queries=%d confirmations=%d merchant_hits=%d err=%v", paid.Status, orders, attempts, receipts, provider.sends.Load(), provider.queries.Load(), confirms.Load(), merchantHits.Load(), err)
				}
				return
			}
			if err != nil || paid.Status != "paid" || orders != 1 || attempts != 1 || receipts != 1 || provider.sends.Load() != 1 || provider.queries.Load() != 1 || confirms.Load() != 1 || merchantHits.Load() != 1 {
				t.Fatalf("payment boundaries: status=%s orders=%d attempts=%d receipts=%d sends=%d queries=%d confirmations=%d merchant_hits=%d err=%v", paid.Status, orders, attempts, receipts, provider.sends.Load(), provider.queries.Load(), confirms.Load(), merchantHits.Load(), err)
			}
		})
	}
}
