package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"goepay/internal/captcha"
	"goepay/internal/identity"
)

type loginVerifier205 struct {
	calls       atomic.Int64
	unavailable bool
}

func (v *loginVerifier205) Init(_ context.Context, c captcha.Config, purpose, source string) (captcha.Challenge, error) {
	if v.unavailable || purpose != "login" && purpose != "testpay" || source != "192.0.2.205" && source != "192.0.2.203" || c.ID != strings.Repeat("a", 32) || c.Key != strings.Repeat("b", 32) {
		return captcha.Challenge{}, captcha.ErrUnavailable
	}
	p := captcha.Challenge{Version: c.Version, GT: c.ID, Success: 1}
	if c.Version == 0 {
		p.Challenge = strings.Repeat("c", 32)
		p.NewCaptcha = true
	}
	return p, nil
}
func (v *loginVerifier205) Verify(_ context.Context, c captcha.Config, issued captcha.Challenge, p captcha.Proof, source string) error {
	v.calls.Add(1)
	if v.unavailable {
		return captcha.ErrUnavailable
	}
	if c.Key != strings.Repeat("b", 32) || c.ID != issued.GT || c.Version != issued.Version || source != "192.0.2.205" && source != "192.0.2.203" {
		return captcha.ErrInvalid
	}
	if c.Version == 0 {
		if p.GeetestChallenge != issued.Challenge || p.GeetestValidate != strings.Repeat("d", 32) || p.GeetestSeccode != "controlled-seccode203" {
			return captcha.ErrInvalid
		}
	} else if p.CaptchaID != c.ID || p.LotNumber != strings.Repeat("c", 32) || p.PassToken != "controlled-pass203" || p.CaptchaOutput != "controlled-output203" {
		return captcha.ErrInvalid
	}
	return nil
}
func loginReady205(t *testing.T, version int) (channel168Fixture, *loginVerifier205) {
	t.Helper()
	f := loginCaptcha205Local(t, "1")
	site191Save(t, f, "captcha_version", strconv.Itoa(version))
	v := new(loginVerifier205)
	s, e := NewLoginCaptchaService205ForMaster(f.s, v, bytes.Repeat([]byte{205}, 32), "https://pay.example.invalid")
	if e != nil {
		t.Fatal(e)
	}
	f.s.LoginCaptcha205 = s
	return f, v
}
func loginInit205(t *testing.T, f channel168Fixture) (captcha.Challenge, []*http.Cookie) {
	t.Helper()
	out := loginCaptcha205Request(f, "POST", "/console/v1/public/login-captcha/init", map[string]string{"username": "captcha205"})
	var envelope struct {
		Code int               `json:"code"`
		Data captcha.Challenge `json:"data"`
	}
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &envelope) != nil || envelope.Code != 0 || len(envelope.Data.CaptchaToken) != 43 || envelope.Data.Success != 1 {
		t.Fatal("bound login CAPTCHA init unavailable", out.Code)
	}
	cookies := out.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "goepay_login205" || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("login CAPTCHA did not issue an independent protected browser cookie")
	}
	return envelope.Data, cookies
}
func loginBody205(c captcha.Challenge) map[string]string {
	in := map[string]string{"username": "captcha205", "password": loginCaptcha205Password, "code": "", "captcha_token": c.CaptchaToken}
	if c.Version == 0 {
		in["geetest_challenge"] = c.Challenge
		in["geetest_validate"] = strings.Repeat("d", 32)
		in["geetest_seccode"] = "controlled-seccode203"
	} else {
		in["captcha_id"] = c.GT
		in["lot_number"] = strings.Repeat("c", 32)
		in["pass_token"] = "controlled-pass203"
		in["gen_time"] = strconv.FormatInt(time.Now().Unix(), 10)
		in["captcha_output"] = "controlled-output203"
	}
	return in
}
func TestLoginCaptcha205ActualHTTPControlledProofAllowsOneSessionAndPasswordFailureBurnsProof(t *testing.T) {
	for _, version := range []int{0, 1} {
		for _, wrong := range []bool{false, true} {
			t.Run(strconv.Itoa(version)+"/wrong-password="+strconv.FormatBool(wrong), func(t *testing.T) {
				f, v := loginReady205(t, version)
				c, cookies := loginInit205(t, f)
				body := loginBody205(c)
				if wrong {
					body["password"] = "controlled-wrong-password205"
				}
				raw, _ := json.Marshal(body)
				out := publicTestPay203Request(f, "POST", "/console/v1/merchant/login", string(raw), cookies)
				if wrong {
					if out.Code != 401 {
						t.Fatal("CAPTCHA replaced password authentication", out.Code)
					}
				} else {
					loginCaptcha205OriginalSession(t, f, out, "merchant", "captcha205")
				}
				body["password"] = loginCaptcha205Password
				raw, _ = json.Marshal(body)
				if replay := publicTestPay203Request(f, "POST", "/console/v1/merchant/login", string(raw), cookies); replay.Code != 422 {
					t.Fatal("spent proof authorized another credential attempt", replay.Code)
				}
				want := 1
				if wrong {
					want = 0
				}
				if loginCaptcha205Sessions(t, f, "merchant", "captcha205") != want || v.calls.Load() != 1 {
					t.Fatal("proof consumption did not bound authentication to one attempt")
				}
				var used, old int
				if e := f.p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM captcha_native_claims205),(SELECT count(*) FROM captcha_used_proofs203)`).Scan(&used, &old); e != nil || used != 1 || old != 0 {
					t.Fatal("login claim was not independent of TestPay intent/legacy claim")
				}
			})
		}
	}
}
func TestLoginCaptcha205ActualHTTPRejectsCrossBrowserAccountRotationAndVerifierFailure(t *testing.T) {
	for _, mutation := range []string{"browser", "username", "configuration", "key", "verifier"} {
		t.Run(mutation, func(t *testing.T) {
			f, v := loginReady205(t, 1)
			c, cookies := loginInit205(t, f)
			body := loginBody205(c)
			want := 422
			switch mutation {
			case "browser":
				_, cookies = loginInit205(t, f)
			case "username":
				body["username"] = "another-account205"
			case "configuration":
				site191Save(t, f, "captcha_open_login", "0")
				want = 409
			case "key":
				captchaSetKey203(t, f, strings.Repeat("e", 32))
				want = 409
			case "verifier":
				v.unavailable = true
				want = 503
			}
			raw, _ := json.Marshal(body)
			out := publicTestPay203Request(f, "POST", "/console/v1/merchant/login", string(raw), cookies)
			if out.Code != want || loginCaptcha205Sessions(t, f, "merchant", "captcha205") != 0 {
				t.Fatal("wrong bound login CAPTCHA context created a session", mutation, out.Code)
			}
			var used int
			if e := f.p.QueryRow(t.Context(), `SELECT count(*) FROM captcha_native_claims205`).Scan(&used); e != nil || used != 0 {
				t.Fatal("rejected CAPTCHA claimed a usable native proof")
			}
		})
	}
}
func TestLoginCaptcha205ActualHTTPCrossPurposeV4LotCannotChangeWrapperToReplay(t *testing.T) {
	for _, first := range []string{"testpay", "login"} {
		t.Run(first, func(t *testing.T) {
			f, _ := captchaReady203(t, 1)
			site191Save(t, f, "captcha_open_login", "1")
			hash, e := identity.HashPassword(loginCaptcha205Password)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = f.p.Exec(t.Context(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash)VALUES('captcha205','merchant',1,'Synthetic CAPTCHA205',$1)`, hash); e != nil {
				t.Fatal(e)
			}
			v := new(loginVerifier205)
			public, e := NewPublicTestPayService203(f.s, v)
			if e != nil {
				t.Fatal(e)
			}
			f.s.PublicTestPay203 = public
			login, e := NewLoginCaptchaService205ForMaster(f.s, v, f.s.CheckoutKey, "https://pay.example.invalid")
			if e != nil {
				t.Fatal(e)
			}
			f.s.LoginCaptcha205 = login
			init, testCookies := publicTestPay203ReadInit(t, f)
			testChallenge := captchaChallenge203(t, f, testCookies)
			loginChallenge, loginCookies := loginInit205(t, f)
			raw, _ := json.Marshal(loginBody205(loginChallenge))
			loginSend := func() int {
				return publicTestPay203Request(f, "POST", "/console/v1/merchant/login", string(raw), loginCookies).Code
			}
			testSend := func() int {
				return publicTestPay203Request(f, "POST", publicTestPay203Prefix+"/submit", captchaProofBody203(init, testChallenge), testCookies).Code
			}
			initial, replay := testSend, loginSend
			if first == "login" {
				initial, replay = loginSend, testSend
			}
			if status := initial(); status != 200 {
				t.Fatal("first controlled purpose did not admit", first, status)
			}
			if status := replay(); status != 422 {
				t.Fatal("same V4 lot changed wrapper/purpose and was accepted", first, status)
			}
			var used int
			if e = f.p.QueryRow(t.Context(), `SELECT count(*) FROM captcha_native_claims205`).Scan(&used); e != nil || used != 1 {
				t.Fatal("cross-purpose native proof identity was not unique")
			}
		})
	}
}
func TestLoginCaptcha205ActualHTTPInitRejectsAuthorityOverridesAndKeepsMetadataIndependent(t *testing.T) {
	f, _ := loginReady205(t, 1)
	for _, body := range []map[string]string{{"username": "captcha205", "role": "admin"}, {"username": "captcha205", "mode": "key"}, {"username": "captcha205", "purpose": "testpay"}, {"username": "captcha205", "captcha_key": "not-authority"}} {
		if out := loginCaptcha205Request(f, "POST", "/console/v1/public/login-captcha/init", body); out.Code != 400 {
			t.Fatal("login CAPTCHA init accepted authority fields", out.Code)
		}
	}
	for _, path := range []string{"/console/v1/public/login-captcha?role=admin&mode=password", "/console/v1/public/login-captcha?role=merchant&mode=key", "/console/v1/public/login-captcha?role=merchant&mode=password&purpose=testpay"} {
		if out := loginCaptcha205Request(f, "GET", path, nil); out.Code != 400 {
			t.Fatal("metadata accepted a different consumer", out.Code)
		}
	}
	loginCaptcha205NoPublicPaymentIntent(t, f)
}

func TestLoginCaptcha205ActualHTTPLegacy203SpentNativeProofCannotEnterLogin(t *testing.T) {
	f, _ := captchaReady203(t, 1)
	site191Save(t, f, "captcha_open_login", "1")
	hash, e := identity.HashPassword(loginCaptcha205Password)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.p.Exec(t.Context(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash)VALUES('captcha205','merchant',1,'Synthetic CAPTCHA205',$1)`, hash); e != nil {
		t.Fatal(e)
	}
	_, cookies := publicTestPay203ReadInit(t, f)
	c := captchaChallenge203(t, f, cookies)
	derive := hmac.New(sha256.New, f.s.CheckoutKey)
	derive.Write([]byte("goepay/public-testpay203/integrity"))
	key := derive.Sum(nil)
	tag := func(purpose, raw string) []byte {
		mac := hmac.New(sha256.New, key)
		mac.Write([]byte(purpose + "\n" + raw))
		return mac.Sum(nil)
	}
	// An authentic original 203 claim is deliberately left as-is: there is no
	// global 205 claim to backfill and no plaintext native identity to recover.
	if _, e = f.p.Exec(t.Context(), `INSERT INTO captcha_used_proofs203(proof_hash,challenge_hash,purpose)VALUES($1,$2,'testpay')`, tag("native-proof", strings.Repeat("a", 32)+"\n"+strings.Repeat("c", 32)), tag("captcha-token", c.CaptchaToken)); e != nil {
		t.Fatal(e)
	}
	v := new(loginVerifier205)
	login, e := NewLoginCaptchaService205ForMaster(f.s, v, f.s.CheckoutKey, "https://pay.example.invalid")
	if e != nil {
		t.Fatal(e)
	}
	f.s.LoginCaptcha205 = login
	challenge, loginCookies := loginInit205(t, f)
	body, _ := json.Marshal(loginBody205(challenge))
	if out := publicTestPay203Request(f, "POST", "/console/v1/merchant/login", string(body), loginCookies); out.Code != 422 || loginCaptcha205Sessions(t, f, "merchant", "captcha205") != 0 {
		t.Fatal("legacy 203 native claim was ignored by new login consumer", out.Code)
	}
	var old, newClaims int
	if e = f.p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM captcha_used_proofs203),(SELECT count(*) FROM captcha_native_claims205)`).Scan(&old, &newClaims); e != nil || old != 1 || newClaims != 0 {
		t.Fatal("original claim was rewritten or failed login claimed another proof")
	}
}
