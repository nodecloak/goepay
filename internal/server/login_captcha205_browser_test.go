package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/captcha"
)

// Deliberately separate from the provider's callback origin and the loopback
// HTTP Host. Production Main supplies this value from configured consoleOrigin.
const loginBrowserOrigin205 = "https://console.example.invalid"

func TestLoginCaptcha205ActualBrowserHTTPWithPrivateTLSAndPersistentSessions(t *testing.T) {
	if os.Getenv("GOEPAY_LOGIN_CAPTCHA205_BROWSER") != "1" {
		t.Skip("GOEPAY_LOGIN_CAPTCHA205_BROWSER=1 uses only synthetic Go/PG/Chromium peers")
	}
	for _, version := range []int{0, 1} {
		for _, width := range []int{375, 1440} {
			t.Run("v"+strconv.Itoa(version+3)+"/"+strconv.Itoa(width), func(t *testing.T) {
				// Keep each real account within the original per-source login budget.
				// This reuses the existing scoped fixture on the runner's same PG.
				f := loginCaptcha205Local(t, "1")
				site191Save(t, f, "captcha_version", strconv.Itoa(version))
				v := &publicBrowserCaptcha203{issued: map[string]string{}}
				verifier := captcha.NewGeetestVerifier(publicBrowserCaptchaClient203(t, v))
				service, err := NewLoginCaptchaService205ForMaster(f.s, verifier, bytes.Repeat([]byte{205}, 32), loginBrowserOrigin205)
				if err != nil {
					t.Fatal(err)
				}
				f.s.LoginCaptcha205 = service
				f.s.Sessions.Secure = true
				// Preserve the independent administrator password path with a real
				// Argon2 hash, after the vault's authenticated setup is complete.
				if _, err = f.p.Exec(t.Context(), `UPDATE console_users SET password_hash=(SELECT password_hash FROM console_users WHERE username='captcha205' AND role='merchant') WHERE username='admin168' AND role='admin'`); err != nil {
					t.Fatal(err)
				}
				grant, err := f.s.Store.PrepareMerchantKeyLogin(t.Context(), 1, "test-secret")
				if err != nil || grant.User().Username != "captcha205" {
					t.Fatal("existing original merchant key-login fixture unavailable", err)
				}
				provider := f.s.ProviderFlow.Providers[7].(*channel168Provider)
				provider.subchannelTestQuery = func(context.Context, adapter.QueryRequest) (adapter.QueryResult, error) {
					v.queries.Add(1)
					return adapter.QueryResult{}, adapter.ErrUnsupported
				}
				initialMerchant := loginCaptcha205Sessions(t, f, "merchant", "captcha205")
				initialAdmin := loginCaptcha205Sessions(t, f, "admin", "admin168")
				controlBytes := make([]byte, 32)
				if _, err = rand.Read(controlBytes); err != nil {
					t.Fatal(err)
				}
				controlKey := hex.EncodeToString(controlBytes)
				var unrelatedWrites, publicPaymentRequests atomic.Int64
				handler := f.s.Handler()
				api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.HasPrefix(r.URL.Path, "/__login205/") {
						if r.Method != "POST" || r.Header.Get("X-Synthetic-Login205-Control") != controlKey {
							http.Error(w, "synthetic fixture control required", 403)
							return
						}
						var in struct{ Challenge string }
						if json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&in) != nil {
							http.Error(w, "synthetic control input", 400)
							return
						}
						w.Header().Set("Content-Type", "application/json")
						switch r.URL.Path {
						case "/__login205/proof":
							v.mu.Lock()
							validate, exists := v.issued[in.Challenge]
							v.mu.Unlock()
							if version != 0 || !exists {
								http.Error(w, "actual issued V3 challenge required", 400)
								return
							}
							json.NewEncoder(w).Encode(map[string]string{"geetest_challenge": in.Challenge, "geetest_validate": validate, "geetest_seccode": "controlled-seccode203"})
						case "/__login205/metrics":
							var effects string
							var merchant, admin, claims, consumed, issued int
							if err := f.p.QueryRow(r.Context(), `SELECT jsonb_build_object('orders',(SELECT count(*)FROM orders),'attempts',(SELECT count(*)FROM provider_attempts),'operations',(SELECT count(*)FROM provider_operations),'jobs',(SELECT count(*)FROM jobs),'journals',(SELECT count(*)FROM journals),'refunds',(SELECT count(*)FROM refunds),'public_intents',(SELECT count(*)FROM public_testpay203_intents),'public_challenges',(SELECT count(*)FROM captcha_challenges203),'old_proofs',(SELECT count(*)FROM captcha_used_proofs203))::text,(SELECT count(*)FROM console_sessions WHERE role='merchant' AND username='captcha205'),(SELECT count(*)FROM console_sessions WHERE role='admin' AND username='admin168'),(SELECT count(*)FROM captcha_native_claims205),(SELECT count(*)FROM login_captcha205_challenges WHERE consumed_at IS NOT NULL),(SELECT count(*)FROM login_captcha205_challenges WHERE session_at IS NOT NULL)`).Scan(&effects, &merchant, &admin, &claims, &consumed, &issued); err != nil {
								http.Error(w, "synthetic metrics unavailable", 500)
								return
							}
							json.NewEncoder(w).Encode(map[string]any{"effects": json.RawMessage(effects), "merchant_sessions": merchant, "admin_sessions": admin, "claims": claims, "consumed": consumed, "issued": issued, "verifies": v.verifies.Load(), "rejected": v.rejected.Load(), "registers": v.registers.Load(), "bad": v.bad.Load(), "sends": provider.sends.Load(), "queries": v.queries.Load(), "unrelated_writes": unrelatedWrites.Load(), "public_payment_requests": publicPaymentRequests.Load()})
						default:
							http.NotFound(w, r)
						}
						return
					}
					if strings.HasPrefix(r.URL.Path, "/console/v1/public/testpay/") || r.URL.Path == "/console/v1/public/captcha" || strings.HasPrefix(r.URL.Path, "/checkout/") {
						publicPaymentRequests.Add(1)
					}
					if r.Method != "GET" && r.Method != "HEAD" {
						allowed := r.Method == "POST" && (r.URL.Path == "/console/v1/public/login-captcha/init" || r.URL.Path == "/console/v1/merchant/login" || r.URL.Path == "/console/v1/merchant/key-login" || r.URL.Path == "/console/v1/merchant/logout" || r.URL.Path == "/console/v1/admin/login" || r.URL.Path == "/console/v1/admin/logout")
						if !allowed {
							unrelatedWrites.Add(1)
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
				cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "web", "e2e", "login-captcha205-http-browser.mjs"))
				cmd.Dir = filepath.Join(root, "web")
				cmd.Env = append(os.Environ(), "LOGIN205_BROWSER_API="+api.URL, "LOGIN205_BROWSER_CONTROL="+controlKey, "LOGIN205_BROWSER_VERSION="+strconv.Itoa(version), "LOGIN205_BROWSER_WIDTH="+strconv.Itoa(width), "LOGIN205_BROWSER_ORIGIN="+loginBrowserOrigin205, "LOGIN205_BROWSER_ARTIFACTS="+filepath.Join(root, ".tools", "login205-actual-http-browser", "v"+strconv.Itoa(version+3), strconv.Itoa(width)))
				output, err := cmd.CombinedOutput()
				if err != nil {
					if len(output) > 7000 {
						output = output[len(output)-7000:]
					}
					t.Fatalf("login205 actual browser v%d/%d: %v\n%s", version+3, width, err, output)
				}
				var merchant, admin, claims, consumed, issued, effects int
				if err = f.p.QueryRow(t.Context(), `SELECT (SELECT count(*)FROM console_sessions WHERE role='merchant' AND username='captcha205'),(SELECT count(*)FROM console_sessions WHERE role='admin' AND username='admin168'),(SELECT count(*)FROM captcha_native_claims205),(SELECT count(*)FROM login_captcha205_challenges WHERE consumed_at IS NOT NULL),(SELECT count(*)FROM login_captcha205_challenges WHERE session_at IS NOT NULL),(SELECT count(*)FROM orders)+(SELECT count(*)FROM provider_attempts)+(SELECT count(*)FROM provider_operations)+(SELECT count(*)FROM jobs)+(SELECT count(*)FROM journals)+(SELECT count(*)FROM refunds)+(SELECT count(*)FROM public_testpay203_intents)+(SELECT count(*)FROM captcha_challenges203)+(SELECT count(*)FROM captcha_used_proofs203)`).Scan(&merchant, &admin, &claims, &consumed, &issued, &effects); err != nil || merchant != initialMerchant+1 || admin != initialAdmin+1 || claims != 2 || consumed != 2 || issued != 1 || effects != 0 {
					t.Fatal("login browser changed its persistent/session/once-only boundaries", merchant, admin, claims, consumed, issued, effects, err)
				}
				if v.bad.Load() != 0 || v.verifies.Load() != 3 || v.rejected.Load() != 1 || provider.sends.Load() != 0 || v.queries.Load() != 0 || unrelatedWrites.Load() != 0 || publicPaymentRequests.Load() != 0 {
					t.Fatal("login browser reached an unrelated payment or external operation", v.bad.Load(), v.verifies.Load(), v.rejected.Load(), provider.sends.Load(), v.queries.Load(), unrelatedWrites.Load(), publicPaymentRequests.Load())
				}
			})
		}
	}
}
