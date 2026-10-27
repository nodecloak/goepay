package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"goepay/internal/captcha203"
)

func captchaAdmin203(f channel168Fixture, method, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "/console/v1/admin/settings/captcha", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", f.csrf)
	request.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	out := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(out, request)
	return out
}
func captchaKeyView203(t *testing.T, out *httptest.ResponseRecorder) captcha203.KeyView {
	t.Helper()
	var envelope struct {
		Code int                `json:"code"`
		Data captcha203.KeyView `json:"data"`
	}
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &envelope) != nil || envelope.Code != 0 || len(envelope.Data.Fingerprint) != 64 {
		t.Fatal("CAPTCHA key metadata unavailable", out.Code)
	}
	return envelope.Data
}
func captchaSetKey203(t *testing.T, f channel168Fixture, key string) captcha203.KeyView {
	t.Helper()
	view := captchaKeyView203(t, captchaAdmin203(f, "GET", ""))
	body, _ := json.Marshal(captcha203.KeyUpdate{Action: "replace", Key: key, Expected: view.Fingerprint})
	return captchaKeyView203(t, captchaAdmin203(f, "PATCH", string(body)))
}

type controlledCaptchaVerifier203 struct{ calls atomic.Int64 }

func (v *controlledCaptchaVerifier203) Init(_ context.Context, c captcha203.Config, purpose, source string) (captcha203.Challenge, error) {
	if purpose != "testpay" || source != "192.0.2.203" || c.ID != strings.Repeat("a", 32) || c.Key != strings.Repeat("b", 32) {
		return captcha203.Challenge{}, captcha203.ErrUnavailable
	}
	out := captcha203.Challenge{Version: c.Version, GT: c.ID, Success: 1}
	if c.Version == 0 {
		out.Challenge = strings.Repeat("c", 32)
		out.NewCaptcha = true
	}
	return out, nil
}
func (v *controlledCaptchaVerifier203) Verify(_ context.Context, c captcha203.Config, issued captcha203.Challenge, p captcha203.Proof, source string) error {
	v.calls.Add(1)
	if source != "192.0.2.203" || issued.GT != c.ID || issued.Version != c.Version || c.Key != strings.Repeat("b", 32) {
		return captcha203.ErrInvalid
	}
	if c.Version == 0 {
		if p.GeetestChallenge != issued.Challenge || p.GeetestValidate != strings.Repeat("d", 32) || p.GeetestSeccode != "controlled-seccode203" {
			return captcha203.ErrInvalid
		}
	} else if p.CaptchaID != c.ID || p.LotNumber != strings.Repeat("c", 32) || p.PassToken != "controlled-pass203" || p.CaptchaOutput != "controlled-output203" {
		return captcha203.ErrInvalid
	}
	return nil
}
func captchaReady203(t *testing.T, version int) (channel168Fixture, *controlledCaptchaVerifier203) {
	t.Helper()
	f := publicTestPay203Ready(t)
	site191Save(t, f, "captcha_open_test", "1")
	site191Save(t, f, "captcha_id", strings.Repeat("a", 32))
	site191Save(t, f, "captcha_version", strconv.Itoa(version))
	captchaSetKey203(t, f, strings.Repeat("b", 32))
	verifier := new(controlledCaptchaVerifier203)
	service, e := NewPublicTestPayService203(f.s, verifier)
	if e != nil {
		t.Fatal(e)
	}
	f.s.PublicTestPay203 = service
	return f, verifier
}
func captchaChallenge203(t *testing.T, f channel168Fixture, cookies []*http.Cookie) captcha203.Challenge {
	t.Helper()
	out := publicTestPay203Request(f, "GET", "/console/v1/public/captcha?purpose=testpay", "", cookies)
	var envelope struct {
		Data captcha203.Challenge `json:"data"`
	}
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &envelope) != nil || len(envelope.Data.CaptchaToken) != 43 || envelope.Data.Success != 1 {
		t.Fatal("controlled bound CAPTCHA challenge unavailable", out.Code)
	}
	return envelope.Data
}
func captchaProofBody203(init publicTestPay203Init, challenge captcha203.Challenge) string {
	body := map[string]string{"money": "1.23", "typeid": "700203", "csrf_token": init.CSRF, "captcha_token": challenge.CaptchaToken}
	if challenge.Version == 0 {
		body["geetest_challenge"] = challenge.Challenge
		body["geetest_validate"] = strings.Repeat("d", 32)
		body["geetest_seccode"] = "controlled-seccode203"
	} else {
		body["captcha_id"] = challenge.GT
		body["lot_number"] = strings.Repeat("c", 32)
		body["pass_token"] = "controlled-pass203"
		body["gen_time"] = strconv.FormatInt(time.Now().Unix(), 10)
		body["captcha_output"] = "controlled-output203"
	}
	raw, _ := json.Marshal(body)
	return string(raw)
}

func TestPublicCaptcha203ActualHTTPVaultUsesSeparateMasterWithoutEnablingPaymentProvider(t *testing.T) {
	f := publicTestPay203Ready(t)
	f.s.ProviderFlow = nil
	f.s.CheckoutKey = nil
	service, e := NewPublicTestPayService203ForMaster(f.s, nil, bytes.Repeat([]byte{7}, 32), "")
	if e != nil {
		t.Fatal(e)
	}
	f.s.PublicTestPay203 = service
	view := captchaSetKey203(t, f, strings.Repeat("b", 32))
	if !view.Configured {
		t.Fatal("separate CAPTCHA master did not enable key storage")
	}
	if len(f.s.CheckoutKey) != 0 || f.s.ProviderFlow != nil {
		t.Fatal("CAPTCHA configuration enabled provider or checkout authority")
	}
	if out := publicTestPay203Request(f, "GET", publicTestPay203Prefix+"/init", "", nil); out.Code != 403 {
		t.Fatal("missing public origin/provider did not keep TestPay closed", out.Code)
	}
}
func TestPublicCaptcha203ActualHTTPVaultEncryptsKeyIgnoresLegacyAndRejectsStaleCAS(t *testing.T) {
	f := publicTestPay203Ready(t)
	site191Save(t, f, "captcha_key", "legacy-plaintext203")
	old := captchaKeyView203(t, captchaAdmin203(f, "GET", ""))
	if old.Configured {
		t.Fatal("legacy plaintext key became a sealed credential")
	}
	secret := strings.Repeat("b", 32)
	current := captchaSetKey203(t, f, secret)
	if !current.Configured || current.Fingerprint == old.Fingerprint {
		t.Fatal("sealed CAPTCHA key did not change metadata")
	}
	var encrypted, proof []byte
	if e := f.p.QueryRow(t.Context(), "SELECT ciphertext,proof_hash FROM captcha_credentials203 WHERE id=true").Scan(&encrypted, &proof); e != nil || len(encrypted) < 32 || len(proof) != 32 || bytes.Contains(encrypted, []byte(secret)) {
		t.Fatal("CAPTCHA key was not encrypted and integrity sealed")
	}
	out := captchaAdmin203(f, "GET", "")
	if strings.Contains(out.Body.String(), secret) || strings.Contains(out.Body.String(), "legacy-plaintext203") {
		t.Fatal("key metadata exposed secret material")
	}
	raw, _ := json.Marshal(captcha203.KeyUpdate{Action: "clear", Expected: old.Fingerprint})
	if out = captchaAdmin203(f, "PATCH", string(raw)); out.Code != 409 {
		t.Fatal("stale CAPTCHA operation overwrote current secret", out.Code)
	}
	raw, _ = json.Marshal(captcha203.KeyUpdate{Action: "clear", Expected: current.Fingerprint})
	if view := captchaKeyView203(t, captchaAdmin203(f, "PATCH", string(raw))); view.Configured {
		t.Fatal("explicit credential clear left an active key")
	}
	var audit string
	if e := f.p.QueryRow(t.Context(), "SELECT COALESCE(jsonb_agg(changes),'[]'::jsonb)::text FROM backoffice_audit WHERE resource='captcha-credentials'").Scan(&audit); e != nil || strings.Contains(audit, secret) {
		t.Fatal("CAPTCHA secret reached audit logs")
	}
}
func TestPublicCaptcha203ActualHTTPControlledV3AndV4ProofIsBoundConsumedOnceAndCannotRearmPayment(t *testing.T) {
	for _, version := range []int{0, 1} {
		t.Run(strconv.Itoa(version), func(t *testing.T) {
			f, verifier := captchaReady203(t, version)
			init, cookies := publicTestPay203ReadInit(t, f)
			challenge := captchaChallenge203(t, f, cookies)
			body := captchaProofBody203(init, challenge)
			out := publicTestPay203Request(f, "POST", publicTestPay203Prefix+"/submit", body, cookies)
			if out.Code != 200 || verifier.calls.Load() != 1 || f.s.ProviderFlow.Providers[7].(*channel168Provider).sends.Load() != 1 {
				t.Fatal("controlled proof did not authorize exactly one ordinary payment", out.Code)
			}
			if replay := publicTestPay203Request(f, "POST", publicTestPay203Prefix+"/submit", body, cookies); replay.Code != 200 || verifier.calls.Load() != 1 || f.s.ProviderFlow.Providers[7].(*channel168Provider).sends.Load() != 1 {
				t.Fatal("public replay consumed CAPTCHA again or sent another payment", replay.Code)
			}
			other, otherCookies := publicTestPay203ReadInit(t, f)
			otherChallenge := captchaChallenge203(t, f, otherCookies)
			otherBody := captchaProofBody203(other, otherChallenge)
			if replay := publicTestPay203Request(f, "POST", publicTestPay203Prefix+"/submit", otherBody, otherCookies); replay.Code != 422 || f.s.ProviderFlow.Providers[7].(*channel168Provider).sends.Load() != 1 {
				t.Fatal("same native proof was accepted for a different browser intent", replay.Code)
			}
			var consumed int
			if e := f.p.QueryRow(t.Context(), "SELECT count(*) FROM captcha_used_proofs203").Scan(&consumed); e != nil || consumed != 1 {
				t.Fatal("single-use proof consumption was not durable")
			}
		})
	}
}
func TestPublicCaptcha203ActualHTTPCrossBrowserKeyRotationAndOtherPurposesNeverAuthorizeTestPayment(t *testing.T) {
	f, verifier := captchaReady203(t, 1)
	first, firstCookies := publicTestPay203ReadInit(t, f)
	issued := captchaChallenge203(t, f, firstCookies)
	other, otherCookies := publicTestPay203ReadInit(t, f)
	body := captchaProofBody203(other, issued)
	if out := publicTestPay203Request(f, "POST", publicTestPay203Prefix+"/submit", body, otherCookies); out.Code != 422 || verifier.calls.Load() != 0 {
		t.Fatal("another browser/intent used the challenge", out.Code)
	}
	for _, purpose := range []string{"login", "payment-defend"} {
		if out := publicTestPay203Request(f, "GET", "/console/v1/public/captcha?purpose="+purpose, "", firstCookies); out.Code != 503 {
			t.Fatal("unimplemented CAPTCHA consumer issued a reusable proof", purpose, out.Code)
		}
	}
	captchaSetKey203(t, f, strings.Repeat("e", 32))
	body = captchaProofBody203(first, issued)
	if out := publicTestPay203Request(f, "POST", publicTestPay203Prefix+"/submit", body, firstCookies); out.Code != 409 || verifier.calls.Load() != 0 {
		t.Fatal("old key/config CAPTCHA reached verification or payment", out.Code)
	}
	if f.s.ProviderFlow.Providers[7].(*channel168Provider).sends.Load() != 0 {
		t.Fatal("rejected purpose or binding crossed the provider boundary")
	}
}
