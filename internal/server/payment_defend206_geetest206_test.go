package server

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"goepay/internal/captcha203"
)

// controlledGeetest206 stands in for the remote gt4 validate endpoint.
type controlledGeetest206 struct {
	calls atomic.Int64
	down  atomic.Bool // remote validate unknown/unavailable
}

func (v *controlledGeetest206) Init(context.Context, captcha203.Config, string, string) (captcha203.Challenge, error) {
	return captcha203.Challenge{}, captcha203.ErrUnavailable
}
func (v *controlledGeetest206) Verify(_ context.Context, c captcha203.Config, issued captcha203.Challenge, p captcha203.Proof, _ string) error {
	v.calls.Add(1)
	if v.down.Load() {
		return captcha203.ErrUnavailable
	}
	if c.Version != 1 || issued.GT != c.ID || c.Key != strings.Repeat("b", 32) || p.CaptchaID != c.ID || p.PassToken != "controlled-pass206" || p.CaptchaOutput != "controlled-output206" {
		return captcha203.ErrInvalid
	}
	return nil
}

var geetest206Lot = 0

func geetest206Ready(t *testing.T, kind string) (channel168Fixture, *controlledGeetest206, func(string) url.Values) {
	t.Helper()
	f, key := paymentDefend206WireFixture(t, true)
	site191Save(t, f, "captcha_id", strings.Repeat("a", 32))
	site191Save(t, f, "captcha_version", "1")
	captchaSetKey203(t, f, strings.Repeat("b", 32))
	v := new(controlledGeetest206)
	f.s.PaymentDefendVerifier206 = v
	defendSigned206Configure(t, f, kind)
	return f, v, func(no string) url.Values { return defendSigned206Form(t, key, no, nil) }
}

func geetest206Proof(form url.Values, token, lot string) url.Values {
	out := defendJump206With(form, token)
	out.Set("__defend_captcha_id", strings.Repeat("a", 32))
	out.Set("__defend_lot_number", lot)
	out.Set("__defend_pass_token", "controlled-pass206")
	out.Set("__defend_gen_time", strconv.FormatInt(time.Now().Unix(), 10))
	out.Set("__defend_captcha_output", "controlled-output206")
	return out
}

var geetest206Page = regexp.MustCompile(`initGeetest4\(\{captchaId:"([0-9a-f]{32})",product:"bind",protocol:"https://",riskType:"(ai|slide)"`)

// Slice 5: Geetest invisible (1) / slide (2) pages use the configured site
// captcha, never a demo id, and create no payment work before verification.
func TestPaymentDefend206GeetestPageUsesConfiguredCaptchaWithoutSideEffects(t *testing.T) {
	for kind, risk := range map[string]string{"1": "ai", "2": "slide"} {
		t.Run(risk, func(t *testing.T) {
			f, _, form := geetest206Ready(t, kind)
			before := paymentDefend206Effects(t, f)
			w := defendSigned206Call(t, f, http.MethodPost, "/api/pay/submit", form("gt206-page-"+risk))
			m := geetest206Page.FindStringSubmatch(w.Body.String())
			if w.Code != 200 || m == nil || m[1] != strings.Repeat("a", 32) || m[2] != risk || !defendJump206Token.MatchString(w.Body.String()) {
				t.Fatalf("Geetest page missing configured captcha or token: HTTP=%d body=%s", w.Code, w.Body.String())
			}
			for _, demo := range []string{"99b142aaece96330d0f3ffb565ffb3ef", "54088bb07d2df3c46b79f80300b0abbe", "demov4"} {
				if strings.Contains(w.Body.String(), demo) {
					t.Fatal("Geetest page used the original demo captcha", demo)
				}
			}
			if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "'nonce-") || !strings.Contains(csp, "form-action 'self'") {
				t.Fatal("Geetest page CSP not nonce/self bound", csp)
			}
			if paymentDefend206Effects(t, f) != before {
				t.Fatal("Geetest page created payment side effects")
			}
		})
	}
}

func TestPaymentDefend206GeetestProofAdmitsOnceAndFailsClosed(t *testing.T) {
	f, v, form := geetest206Ready(t, "1")
	base := form("gt206-ok")
	token := defendJump206Mint(t, f, "/api/pay/submit", base)
	sends := defendJump206Sends(f)
	w := defendSigned206Call(t, f, http.MethodPost, "/api/pay/submit", geetest206Proof(base, token, strings.Repeat("1", 32)))
	if w.Code != http.StatusFound || defendJump206Sends(f) != sends+1 || v.calls.Load() != 1 {
		t.Fatalf("verified Geetest proof did not admit once: HTTP=%d body=%s calls=%d", w.Code, w.Body.String(), v.calls.Load())
	}
	sends = defendJump206Sends(f)
	before := paymentDefend206Effects(t, f)
	t.Run("token-replay", func(t *testing.T) {
		defendSigned206Reject(t, f, before, sends, defendSigned206Call(t, f, http.MethodPost, "/api/pay/submit", geetest206Proof(base, token, strings.Repeat("2", 32))))
	})
	t.Run("lot-replay-new-token", func(t *testing.T) {
		other := form("gt206-lot")
		tok := defendJump206Mint(t, f, "/api/pay/submit", other)
		defendSigned206Reject(t, f, before, sends, defendSigned206Call(t, f, http.MethodPost, "/api/pay/submit", geetest206Proof(other, tok, strings.Repeat("1", 32))))
	})
	t.Run("remote-rejects", func(t *testing.T) {
		other := form("gt206-remote")
		tok := defendJump206Mint(t, f, "/api/pay/submit", other)
		proof := geetest206Proof(other, tok, strings.Repeat("3", 32))
		proof.Set("__defend_captcha_output", "forged")
		defendSigned206Reject(t, f, before, sends, defendSigned206Call(t, f, http.MethodPost, "/api/pay/submit", proof))
	})
	t.Run("remote-unknown", func(t *testing.T) {
		v.down.Store(true)
		defer v.down.Store(false)
		other := form("gt206-unknown")
		tok := defendJump206Mint(t, f, "/api/pay/submit", other)
		defendSigned206Reject(t, f, before, sends, defendSigned206Call(t, f, http.MethodPost, "/api/pay/submit", geetest206Proof(other, tok, strings.Repeat("4", 32))))
	})
	t.Run("token-without-proof", func(t *testing.T) {
		other := form("gt206-noproof")
		tok := defendJump206Mint(t, f, "/api/pay/submit", other)
		defendSigned206Reject(t, f, before, sends, defendSigned206Call(t, f, http.MethodPost, "/api/pay/submit", defendJump206With(other, tok)))
	})
	t.Run("proof-on-api", func(t *testing.T) {
		defendSigned206Reject(t, f, before, sends, defendSigned206Call(t, f, http.MethodPost, "/api/pay/create", geetest206Proof(form("gt206-api"), strings.Repeat("ab", 32), strings.Repeat("5", 32))))
	})
}

func TestPaymentDefend206GeetestUnconfiguredFailsClosed(t *testing.T) {
	f, key := paymentDefend206WireFixture(t, true)
	defendSigned206Configure(t, f, "2") // no captcha id/key/verifier
	before := paymentDefend206Effects(t, f)
	defendSigned206Reject(t, f, before, 0, defendSigned206Call(t, f, http.MethodPost, "/api/pay/submit", defendSigned206Form(t, key, "gt206-unconfigured", nil)))
}
