package server

import (
	"context"
	"crypto"
	"crypto/md5" // Original MD5 wire fixture, independent of production canonicalization.
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// No value in this file impersonates an issued platform credential. The
// continuation endpoint/token contract is deliberately not invented here.
const defendSigned206MerchantField = "merchant-signed +/&?中文206"
const defendSigned206UnissuedField = "synthetic-unissued-platform-transport206"

type defendSigned206Route struct {
	name, path string
	rsaMode    bool
}

func defendSigned206BrowserRoutes() []defendSigned206Route {
	return []defendSigned206Route{{"md5-legacy-submit", "/submit.php", false}, {"rsa-legacy-submit", "/submit.php", true}, {"rsa-submit", "/api/pay/submit", true}}
}

// Sign a fixed, explicit original wire preimage rather than asking production
// to sign and verify its own canonicalization. In particular, __defend remains
// in this merchant preimage when supplied before signing.
func defendSigned206Form(t *testing.T, key *rsa.PrivateKey, no string, field *string) url.Values {
	t.Helper()
	form := url.Values{
		"pid": {"1"}, "out_trade_no": {no}, "type": {"alipay"},
		"name": {"Synthetic signed defend206"}, "money": {"1.23"},
		"channel_id": {"7"}, "method": {"native"}, "clientip": {"192.0.2.206"},
		"notify_url": {"https://merchant.example.invalid/notify"},
		"return_url": {"https://merchant.example.invalid/return"},
		"sign_type":  {"MD5"},
	}
	preimage := ""
	if field != nil {
		form.Set("__defend", *field)
		if *field != "" {
			preimage = "__defend=" + *field + "&"
		}
	}
	preimage += "channel_id=7&clientip=192.0.2.206&method=native&money=1.23&name=Synthetic signed defend206&notify_url=https://merchant.example.invalid/notify&out_trade_no=" + no + "&pid=1&return_url=https://merchant.example.invalid/return"
	if key == nil {
		digest := md5.Sum([]byte(preimage + "&type=alipay" + "test-secret"))
		form.Set("sign", hex.EncodeToString(digest[:]))
		return form
	}
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	form.Set("sign_type", "RSA")
	form.Set("timestamp", stamp)
	digest := sha256.Sum256([]byte(preimage + "&timestamp=" + stamp + "&type=alipay"))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	form.Set("sign", base64.StdEncoding.EncodeToString(signature))
	return form
}

func defendSigned206Call(t *testing.T, f channel168Fixture, method, target string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	body := form.Encode()
	if method == http.MethodGet {
		separator := "?"
		if strings.Contains(target, "?") {
			separator = "&"
		}
		target += separator + body
		body = ""
	}
	r := httptest.NewRequest(method, target, strings.NewReader(body)).WithContext(ctx)
	r.RemoteAddr = "192.0.2.206:5206"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func defendSigned206Reject(t *testing.T, f channel168Fixture, before string, sends int64, w *httptest.ResponseRecorder) {
	t.Helper()
	defendAdmission206Blocked(t, w)
	if after := paymentDefend206Effects(t, f); after != before || f.s.ProviderFlow.Providers[7].(*channel168Provider).sends.Load() != sends {
		t.Fatal("unverified or ambiguous __defend reached payment work before rejection", before, after)
	}
}

func defendSigned206Configure(t *testing.T, f channel168Fixture, kind string) {
	t.Helper()
	view := paymentDefend206Read(t, f)
	view.Settings["pay_verify"], view.Settings["pay_verify_type"] = "3", kind
	body, err := json.Marshal(map[string]any{"settings": view.Settings, "expected_fingerprint": view.Fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	w := paymentDefend206Request(f, http.MethodPatch, "", string(body), f.token, f.csrf)
	defendAdmission206Configured(t, w)
	current := paymentDefend206Read(t, f)
	if current.Settings["pay_verify"] != "3" || current.Settings["pay_verify_type"] != kind {
		t.Fatal("policy fixture did not configure the requested actual HTTP verification mode", current)
	}
}

func TestPaymentDefend206OriginalSignedDefendKeepsMD5AndRSABrowserSubmitCompatibility(t *testing.T) {
	for _, route := range defendSigned206BrowserRoutes() {
		t.Run(route.name, func(t *testing.T) {
			f, key := paymentDefend206WireFixture(t, route.rsaMode)
			for _, method := range []string{http.MethodPost, http.MethodGet} {
				for _, signedField := range []bool{false, true} {
					name := method + "-without-extension"
					var field *string
					if signedField {
						name = method + "-with-signed-extension"
						value := defendSigned206MerchantField
						field = &value
					}
					t.Run(name, func(t *testing.T) {
						no := "signed-defend206-" + name
						before := f.s.ProviderFlow.Providers[7].(*channel168Provider).sends.Load()
						w := defendSigned206Call(t, f, method, route.path, defendSigned206Form(t, key, no, field))
						checkoutPrefix := strings.TrimRight(f.s.ProviderFlow.CallbackBaseURL, "/") + "/checkout/"
						if w.Code != http.StatusFound || !strings.HasPrefix(w.Header().Get("Location"), checkoutPrefix) {
							t.Fatalf("independently signed original browser submit regressed: HTTP=%d body=%s", w.Code, w.Body.String())
						}
						o, err := f.s.Store.Order(t.Context(), 1, no)
						if err != nil {
							t.Fatal("successful wire response omitted its original order", err)
						}
						a, err := f.s.Store.ProviderAttempt(t.Context(), o.TradeNo)
						if err != nil || a.ID <= 0 || a.State != "pending" || f.s.ProviderFlow.Providers[7].(*channel168Provider).sends.Load() != before+1 {
							t.Fatal("signed __defend compatibility baseline did not really send once through the controlled provider", a, err)
						}
					})
				}
			}
		})
	}
}

func TestPaymentDefend206ForgedUnsignedTransportAndTamperingCannotAuthorizeBrowserSubmit(t *testing.T) {
	for _, route := range defendSigned206BrowserRoutes() {
		t.Run(route.name, func(t *testing.T) {
			f, key := paymentDefend206WireFixture(t, route.rsaMode)
			before := paymentDefend206Effects(t, f)
			for _, mutation := range []string{"unsigned-transport", "unsigned-legacy-shaped-transport", "changed-signed-defend", "removed-signed-defend", "changed-money", "changed-merchant-order", "changed-notify-url", "extra-unsigned-field", "array-defend", "duplicate-same-defend", "duplicate-empty-defend", "query-body-duplicate"} {
				t.Run(mutation, func(t *testing.T) {
					field := defendSigned206MerchantField
					var signedField *string = &field
					if strings.HasPrefix(mutation, "unsigned-") {
						signedField = nil
					}
					form := defendSigned206Form(t, key, "signed-defend206-invalid-"+mutation, signedField)
					target := route.path
					switch mutation {
					case "unsigned-transport":
						form.Set("__defend", defendSigned206UnissuedField)
					case "unsigned-legacy-shaped-transport":
						form.Set("__defend", strconv.FormatInt(time.Now().Unix(), 10)+strings.Repeat("a", 32)+"123456")
					case "changed-signed-defend":
						form.Set("__defend", field+"changed")
					case "removed-signed-defend":
						form.Del("__defend")
					case "changed-money":
						form.Set("money", "1.24")
					case "changed-merchant-order":
						form.Set("out_trade_no", "different-original206")
					case "changed-notify-url":
						form.Set("notify_url", "https://other.example.invalid/notify")
					case "extra-unsigned-field":
						form.Set("merchant_extension", "unsigned")
					case "array-defend":
						form.Set("__defend[]", defendSigned206UnissuedField)
					case "duplicate-same-defend":
						form.Add("__defend", field)
					case "duplicate-empty-defend":
						form.Add("__defend", "")
					case "query-body-duplicate":
						target += "?" + url.Values{"__defend": {field}}.Encode()
					}
					w := defendSigned206Call(t, f, http.MethodPost, target, form)
					defendSigned206Reject(t, f, before, 0, w)
				})
			}
		})
	}
}

func TestPaymentDefend206TypeThreeCannotBeBypassedByAnyDefendFieldOnBrowserSubmit(t *testing.T) {
	for _, route := range defendSigned206BrowserRoutes() {
		t.Run(route.name, func(t *testing.T) {
			f, key := paymentDefend206WireFixture(t, route.rsaMode)
			defendSigned206Configure(t, f, "3")
			before := paymentDefend206Effects(t, f)
			for _, method := range []string{http.MethodPost, http.MethodGet} {
				for _, variant := range []string{"no-defend", "merchant-signed", "signed-legacy-shaped", "unsigned-transport"} {
					t.Run(method+"-"+variant, func(t *testing.T) {
						var field *string
						value := defendSigned206MerchantField
						if variant == "signed-legacy-shaped" {
							value = strconv.FormatInt(time.Now().Unix(), 10) + strings.Repeat("b", 32) + "123456"
						}
						if variant == "merchant-signed" || variant == "signed-legacy-shaped" {
							field = &value
						}
						form := defendSigned206Form(t, key, "signed-defend206-forbidden-"+method+"-"+variant, field)
						if variant == "unsigned-transport" {
							form.Set("__defend", defendSigned206UnissuedField)
						}
						defendSigned206Reject(t, f, before, 0, defendSigned206Call(t, f, method, route.path, form))
					})
				}
			}
		})
	}
}

func TestPaymentDefend206MapiAndCreateCannotBorrowBrowserDefendAuthority(t *testing.T) {
	for _, route := range []defendSigned206Route{{"md5-mapi", "/mapi.php", false}, {"rsa-mapi", "/mapi.php", true}, {"rsa-create", "/api/pay/create", true}} {
		t.Run(route.name, func(t *testing.T) {
			f, key := paymentDefend206WireFixture(t, route.rsaMode)
			// Positive off baseline: the signature and real ordinary fake-provider
			// route must work before any security rejection can count as evidence.
			field := defendSigned206MerchantField
			baseline := defendSigned206Call(t, f, http.MethodPost, route.path, defendSigned206Form(t, key, "signed-defend206-api-off", &field))
			var body map[string]any
			want := float64(1)
			if route.path == "/api/pay/create" {
				want = 0
			}
			if baseline.Code != http.StatusOK || json.Unmarshal(baseline.Body.Bytes(), &body) != nil || body["code"] != want || f.s.ProviderFlow.Providers[7].(*channel168Provider).sends.Load() != 1 {
				t.Fatal("API off baseline did not accept its independently signed original request", baseline.Code, baseline.Body.String())
			}
			before := paymentDefend206Effects(t, f)
			for _, kind := range []string{"0", "1", "2", "3"} {
				defendSigned206Configure(t, f, kind)
				for _, variant := range []string{"no-defend", "merchant-signed", "signed-legacy-shaped", "unsigned-transport"} {
					t.Run("type-"+kind+"-"+variant, func(t *testing.T) {
						var signedField *string
						value := defendSigned206UnissuedField
						if variant == "signed-legacy-shaped" {
							value = strconv.FormatInt(time.Now().Unix(), 10) + strings.Repeat("c", 32) + "123456"
						}
						if variant == "merchant-signed" || variant == "signed-legacy-shaped" {
							signedField = &value
						}
						form := defendSigned206Form(t, key, "signed-defend206-api-on-"+kind+"-"+variant, signedField)
						if variant == "unsigned-transport" {
							form.Set("__defend", defendSigned206UnissuedField)
						}
						defendSigned206Reject(t, f, before, 1, defendSigned206Call(t, f, http.MethodPost, route.path, form))
					})
				}
			}
		})
	}
}
