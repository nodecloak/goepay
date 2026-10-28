package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
)

var defendJump206Token = regexp.MustCompile(`name="__defend_continue" value="([0-9a-f]{64})"`)

func defendSigned206Recorder(f channel168Fixture, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func defendJump206Sends(f channel168Fixture) int64 {
	return f.s.ProviderFlow.Providers[7].(*channel168Provider).sends.Load()
}

// defendJump206Mint renders the jump page for form and returns its token.
func defendJump206Mint(t *testing.T, f channel168Fixture, path string, form url.Values) string {
	t.Helper()
	w := defendSigned206Call(t, f, http.MethodPost, path, form)
	m := defendJump206Token.FindStringSubmatch(w.Body.String())
	if w.Code != http.StatusOK || m == nil {
		t.Fatalf("JS-jump page did not carry a continue token: HTTP=%d body=%s", w.Code, w.Body.String())
	}
	return m[1]
}

func defendJump206With(form url.Values, token string) url.Values {
	out := url.Values{}
	for k, v := range form {
		out[k] = append([]string(nil), v...)
	}
	out.Set("__defend_continue", token)
	return out
}

// Slice 2 (pay_verify_type=0 JS jump): a triggered but not-yet-verified browser
// submit must render the jump page carrying a one-time continue token and must
// NOT create an order, attempt, or provider send. This replaces the interim
// blanket block for the JS-jump mode without reusing the original weak,
// timeless md5 defend key.
func TestPaymentDefend206JSJumpRendersContinuePageWithoutPaymentSideEffects(t *testing.T) {
	for _, route := range defendSigned206BrowserRoutes() {
		t.Run(route.name, func(t *testing.T) {
			f, key := paymentDefend206WireFixture(t, route.rsaMode)
			defendSigned206Configure(t, f, "0") // pay_verify=3, pay_verify_type=0 (JS jump)
			before := paymentDefend206Effects(t, f)
			sends := defendJump206Sends(f)
			w := defendSigned206Call(t, f, http.MethodPost, route.path, defendSigned206Form(t, key, "jump206-"+route.name, nil))
			if w.Code != http.StatusOK {
				t.Fatalf("JS-jump mode did not render a continue page: HTTP=%d body=%s", w.Code, w.Body.String())
			}
			if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
				t.Fatalf("JS-jump continue page was not HTML: %q", ct)
			}
			if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'nonce-") || !strings.Contains(csp, "form-action 'self'") {
				t.Fatalf("JS-jump page CSP is not nonce/self bound: %q", csp)
			}
			body := w.Body.String()
			if !strings.Contains(body, "<form") || !strings.Contains(body, "__defend") {
				t.Fatalf("JS-jump page missing the continue-token resubmission form: %s", body)
			}
			if after := paymentDefend206Effects(t, f); after != before || defendJump206Sends(f) != sends {
				t.Fatal("JS-jump continue page created payment side effects before verification", before, after)
			}
		})
	}
}

// A valid token admits exactly one first send; replay, forgery, a token bound
// to another order, another browser, or another route fail closed.
func TestPaymentDefend206JSJumpTokenAdmitsOnceAndFailsClosed(t *testing.T) {
	for _, route := range defendSigned206BrowserRoutes() {
		t.Run(route.name, func(t *testing.T) {
			f, key := paymentDefend206WireFixture(t, route.rsaMode)
			defendSigned206Configure(t, f, "0")
			signed := defendSigned206MerchantField
			form := defendSigned206Form(t, key, "jump206-ok-"+route.name, &signed)
			token := defendJump206Mint(t, f, route.path, form)
			sends := defendJump206Sends(f)
			w := defendSigned206Call(t, f, http.MethodPost, route.path, defendJump206With(form, token))
			checkoutPrefix := strings.TrimRight(f.s.ProviderFlow.CallbackBaseURL, "/") + "/checkout/"
			if w.Code != http.StatusFound || !strings.HasPrefix(w.Header().Get("Location"), checkoutPrefix) || defendJump206Sends(f) != sends+1 {
				t.Fatalf("valid continue token did not admit exactly one send: HTTP=%d body=%s", w.Code, w.Body.String())
			}
			if _, err := f.s.Store.Order(t.Context(), 1, "jump206-ok-"+route.name); err != nil {
				t.Fatal("admitted submit omitted its order", err)
			}
			sends = defendJump206Sends(f)
			before := paymentDefend206Effects(t, f)
			t.Run("replay", func(t *testing.T) {
				defendSigned206Reject(t, f, before, sends, defendSigned206Call(t, f, http.MethodPost, route.path, defendJump206With(form, token)))
			})
			t.Run("forged", func(t *testing.T) {
				other := defendSigned206Form(t, key, "jump206-forged-"+route.name, nil)
				defendSigned206Reject(t, f, before, sends, defendSigned206Call(t, f, http.MethodPost, route.path, defendJump206With(other, strings.Repeat("ab", 32))))
			})
			t.Run("other-order", func(t *testing.T) {
				a := defendSigned206Form(t, key, "jump206-a-"+route.name, nil)
				b := defendSigned206Form(t, key, "jump206-b-"+route.name, nil)
				tok := defendJump206Mint(t, f, route.path, a)
				defendSigned206Reject(t, f, before, sends, defendSigned206Call(t, f, http.MethodPost, route.path, defendJump206With(b, tok)))
			})
			t.Run("other-browser", func(t *testing.T) {
				a := defendSigned206Form(t, key, "jump206-ua-"+route.name, nil)
				tok := defendJump206Mint(t, f, route.path, a)
				body := defendJump206With(a, tok).Encode()
				r, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, route.path, strings.NewReader(body))
				r.RemoteAddr = "192.0.2.206:5206"
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				r.Header.Set("User-Agent", "different-browser206")
				rec := defendSigned206Recorder(f, r)
				defendSigned206Reject(t, f, before, sends, rec)
			})
			t.Run("api-create-cannot-carry", func(t *testing.T) {
				a := defendSigned206Form(t, key, "jump206-api-"+route.name, nil)
				tok := defendJump206Mint(t, f, route.path, a)
				defendSigned206Reject(t, f, before, sends, defendSigned206Call(t, f, http.MethodPost, "/mapi.php", defendJump206With(a, tok)))
			})
			t.Run("policy-changed", func(t *testing.T) {
				a := defendSigned206Form(t, key, "jump206-pol-"+route.name, nil)
				tok := defendJump206Mint(t, f, route.path, a)
				defendSigned206Configure(t, f, "3")
				defendSigned206Reject(t, f, before, sends, defendSigned206Call(t, f, http.MethodPost, route.path, defendJump206With(a, tok)))
				defendSigned206Configure(t, f, "0")
			})
		})
	}
}

func TestPaymentDefend206JSJumpTokenConcurrentConsumeHasOneWinner(t *testing.T) {
	f, key := paymentDefend206WireFixture(t, true)
	defendSigned206Configure(t, f, "0")
	form := defendSigned206Form(t, key, "jump206-race", nil)
	token := defendJump206Mint(t, f, "/api/pay/submit", form)
	sends := defendJump206Sends(f)
	const n = 4
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = defendSigned206Call(t, f, http.MethodPost, "/api/pay/submit", defendJump206With(form, token)).Code
		}(i)
	}
	wg.Wait()
	wins := 0
	for _, c := range codes {
		if c == http.StatusFound {
			wins++
		}
	}
	if wins != 1 || defendJump206Sends(f) != sends+1 {
		t.Fatalf("concurrent consume of one token: wins=%d codes=%v sends=%d->%d", wins, codes, sends, defendJump206Sends(f))
	}
}
