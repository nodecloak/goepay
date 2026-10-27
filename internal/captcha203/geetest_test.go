package captcha203

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type privateGeetestTransport203 struct {
	target *url.URL
	base   http.RoundTripper
	calls  *atomic.Int64
}

func (t privateGeetestTransport203) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "https" || r.URL.Host != "api.geetest.com" && r.URL.Host != "gcaptcha4.geetest.com" {
		return nil, errors.New("unexpected CAPTCHA destination")
	}
	t.calls.Add(1)
	clone := r.Clone(r.Context())
	u := *r.URL
	u.Host = t.target.Host
	clone.URL = &u
	clone.Host = ""
	return t.base.RoundTrip(clone)
}
func privateGeetest203(t *testing.T, handler http.HandlerFunc) (Verifier, *atomic.Int64) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	target, _ := url.Parse(server.URL)
	counter := new(atomic.Int64)
	client := server.Client()
	client.Transport = privateGeetestTransport203{target: target, base: client.Transport, calls: counter}
	return NewGeetestVerifier(client), counter
}
func md5Test203(s string) string { sum := md5.Sum([]byte(s)); return hex.EncodeToString(sum[:]) }

func TestGeetest203V3PrivateTLSRequiresBoundChallengeKeyAndRemoteSeccode(t *testing.T) {
	config := Config{Version: 0, ID: strings.Repeat("a", 32), Key: strings.Repeat("b", 32)}
	remote := strings.Repeat("c", 32)
	expected := md5Test203(remote + config.Key)
	code := "opaque-v3-seccode203"
	verifier, calls := privateGeetest203(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/register.php" {
			if r.Method != "GET" || r.URL.Query().Get("gt") != config.ID || r.URL.Query().Get("digestmod") != "md5" {
				t.Error("V3 initialization changed the original protocol")
			}
			json.NewEncoder(w).Encode(map[string]string{"challenge": remote})
			return
		}
		if r.URL.Path != "/validate.php" || r.Method != "POST" {
			t.Error("unexpected V3 operation")
		}
		r.ParseForm()
		if r.PostForm.Get("challenge") != expected || r.PostForm.Get("captchaid") != config.ID || r.PostForm.Get("seccode") != code || r.PostForm.Get("ip_address") != "192.0.2.203" {
			t.Error("V3 validation lost its bound inputs")
		}
		json.NewEncoder(w).Encode(map[string]string{"seccode": md5Test203(code)})
	})
	challenge, err := verifier.Init(t.Context(), config, "testpay", "192.0.2.203")
	if err != nil || challenge.Challenge != expected || challenge.Success != 1 || challenge.Version != 0 {
		t.Fatal("controlled V3 register failed", err)
	}
	proof := Proof{GeetestChallenge: expected, GeetestValidate: md5Test203(config.Key + "geetest" + expected), GeetestSeccode: code}
	if err = verifier.Verify(t.Context(), config, challenge, proof, "192.0.2.203"); err != nil || calls.Load() != 2 {
		t.Fatal("V3 did not require the controlled remote acknowledgment", err)
	}
	proof.GeetestChallenge = strings.Repeat("d", 32)
	if verifier.Verify(t.Context(), config, challenge, proof, "192.0.2.203") == nil || calls.Load() != 2 {
		t.Fatal("another issued challenge reached V3 validation")
	}
}
func TestGeetest203V3UnavailableHasNoOfflineOrDemoSuccessFallback(t *testing.T) {
	verifier, calls := privateGeetest203(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "synthetic private failure", 503) })
	challenge, err := verifier.Init(t.Context(), Config{Version: 0, ID: strings.Repeat("a", 32), Key: strings.Repeat("b", 32)}, "testpay", "192.0.2.203")
	if err == nil || challenge.Success != 0 || calls.Load() != 1 {
		t.Fatal("unavailable V3 register fabricated a successful challenge")
	}
	if _, err = verifier.Init(t.Context(), Config{Version: 0, ID: "", Key: ""}, "testpay", "192.0.2.203"); err == nil || calls.Load() != 1 {
		t.Fatal("unconfigured CAPTCHA reached demo/fallback services")
	}
}
func TestGeetest203V4PrivateTLSBindsIDLotHMACAndBothRemoteSuccessFields(t *testing.T) {
	config := Config{Version: 1, ID: strings.Repeat("a", 32), Key: strings.Repeat("b", 32)}
	lot := strings.Repeat("c", 32)
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	var validReply atomic.Bool
	validReply.Store(true)
	verifier, calls := privateGeetest203(t, func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		mac := hmac.New(sha256.New, []byte(config.Key))
		mac.Write([]byte(lot))
		if r.URL.Path != "/validate" || r.URL.Query().Get("captcha_id") != config.ID || r.PostForm.Get("lot_number") != lot || r.PostForm.Get("gen_time") != stamp || r.PostForm.Get("sign_token") != hex.EncodeToString(mac.Sum(nil)) || r.PostForm.Get("captcha_output") != "controlled-output203" {
			t.Error("V4 proof or HMAC was not bound to its configured ID/lot")
		}
		if validReply.Load() {
			json.NewEncoder(w).Encode(map[string]any{"status": "success", "result": "success", "captcha_args": map[string]string{"captcha_id": config.ID, "lot_number": lot}})
		} else {
			fmt.Fprint(w, `{"status":"success","result":"fail"}`)
		}
	})
	challenge, err := verifier.Init(t.Context(), config, "testpay", "192.0.2.203")
	if err != nil || calls.Load() != 0 {
		t.Fatal("V4 initialization unexpectedly used another network protocol")
	}
	proof := Proof{CaptchaID: config.ID, LotNumber: lot, PassToken: "controlled-pass203", GenTime: stamp, CaptchaOutput: "controlled-output203"}
	if err = verifier.Verify(t.Context(), config, challenge, proof, "192.0.2.203"); err != nil || calls.Load() != 1 {
		t.Fatal("controlled V4 verification failed", err)
	}
	validReply.Store(false)
	if verifier.Verify(t.Context(), config, challenge, proof, "192.0.2.203") == nil {
		t.Fatal("partial remote success was accepted")
	}
	proof.CaptchaID = strings.Repeat("d", 32)
	if verifier.Verify(t.Context(), config, challenge, proof, "192.0.2.203") == nil || calls.Load() != 2 {
		t.Fatal("another captcha ID reached the V4 remote verifier")
	}
}
func TestGeetest203V4RejectsRedirectDuplicateOrMismatchedRemoteBinding(t *testing.T) {
	config := Config{Version: 1, ID: strings.Repeat("a", 32), Key: strings.Repeat("b", 32)}
	proof := Proof{CaptchaID: config.ID, LotNumber: strings.Repeat("c", 32), PassToken: "pass203", GenTime: strconv.FormatInt(time.Now().Unix(), 10), CaptchaOutput: "output203"}
	for _, mode := range []string{"redirect", "duplicate", "mismatch", "missing-status"} {
		t.Run(mode, func(t *testing.T) {
			verifier, calls := privateGeetest203(t, func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "redirect":
					w.Header().Set("Location", "https://unexpected.invalid/collect")
					w.WriteHeader(302)
				case "duplicate":
					fmt.Fprint(w, `{"status":"success","status":"success","result":"success"}`)
				case "mismatch":
					fmt.Fprintf(w, `{"status":"success","result":"success","captcha_args":{"captcha_id":%q,"lot_number":"another203"}}`, config.ID)
				case "missing-status":
					fmt.Fprint(w, `{"result":"success"}`)
				}
			})
			challenge, err := verifier.Init(context.Background(), config, "testpay", "192.0.2.203")
			if err != nil {
				t.Fatal(err)
			}
			if verifier.Verify(t.Context(), config, challenge, proof, "192.0.2.203") == nil || calls.Load() != 1 {
				t.Fatal("untrusted V4 response was accepted or followed a redirect")
			}
		})
	}
}
