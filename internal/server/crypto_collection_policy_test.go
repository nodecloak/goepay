package server

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"goepay/internal/compat"
	"goepay/internal/paymentdefend"
)

func cryptoPolicyEnableVerification(t *testing.T, f *email197Fixture) {
	t.Helper()
	var err error
	f.s.PaymentDefend206, err = paymentdefend.NewStore(f.p, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	defendSigned206Configure(t, f.channel168Fixture, "0")
}

func cryptoPolicyCashierRequest(f *email197Fixture, endpoint string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", endpoint, strings.NewReader("type=usdt"))
	r.RemoteAddr = "192.0.2.206:5206"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func cryptoPolicyNoAllocation(t *testing.T, f *email197Fixture) {
	t.Helper()
	var invoices, used int
	if err := f.p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM crypto_invoices),(SELECT count(*) FROM crypto_addresses WHERE used)`).Scan(&invoices, &used); err != nil || invoices != 0 || used != 0 {
		t.Fatalf("rejected payment allocated funds/address: %d %d %v", invoices, used, err)
	}
}

func TestCryptoCashierRechecksBuyerBlacklistAtCollection(t *testing.T) {
	f := cryptoAdmissionFixture(t)
	cryptoAdmissionChannels(t, f)
	intent := cashierIntent{Version: "v1", MerchantID: 1, OutTradeNo: "buyer-blocked", Name: "Buyer policy", Money: "7.20", Buyer: "blocked-wallet-buyer", ClientIP: "192.0.2.206", Expires: time.Now().Add(time.Minute).Unix()}
	endpoint := f.s.cashierURL(intent)
	if _, err := f.p.Exec(t.Context(), `INSERT INTO backoffice_records(resource,body) VALUES('blacklist','{"kind":"buyer","value":"blocked-wallet-buyer","enabled":true}')`); err != nil {
		t.Fatal(err)
	}
	w := cryptoPolicyCashierRequest(f, endpoint)
	if w.Code < 400 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") || !strings.Contains(w.Body.String(), "暂时无法发起支付") {
		t.Fatalf("cashier bypassed buyer policy or returned raw data: %d %s", w.Code, w.Body.String())
	}
	cryptoPolicyNoAllocation(t, f)
}

func TestCryptoConsoleAndCashierCannotSkipRequiredVerification(t *testing.T) {
	for _, entry := range []string{"console", "cashier"} {
		t.Run(entry, func(t *testing.T) {
			f := cryptoAdmissionFixture(t)
			channel, _ := cryptoAdmissionChannels(t, f)
			intent := cashierIntent{Version: "v1", MerchantID: 1, OutTradeNo: "required-verification", Name: "Verify policy", Money: "7.20", ClientIP: "192.0.2.206", Expires: time.Now().Add(time.Minute).Unix()}
			endpoint := f.s.cashierURL(intent)
			cryptoPolicyEnableVerification(t, f)
			var w *httptest.ResponseRecorder
			if entry == "console" {
				w = cryptoAdmissionConsole(f, "POST", "/invoices", `{"channel_id":"`+strconv.FormatInt(channel.ID, 10)+`","out_trade_no":"required-verification","amount":"1"}`)
			} else {
				w = cryptoPolicyCashierRequest(f, endpoint)
			}
			if w.Code < 400 || !strings.Contains(w.Body.String(), "请通过标准支付提交入口完成支付安全验证") {
				t.Fatalf("verification bypass or unclear result: %d %s", w.Code, w.Body.String())
			}
			if entry == "cashier" && !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
				t.Fatal("cashier returned raw JSON", w.Body.String())
			}
			cryptoPolicyNoAllocation(t, f)
		})
	}
}

func TestCryptoVerifiedStandardSubmitKeepsOriginalProof(t *testing.T) {
	f := cryptoAdmissionFixture(t)
	channel, _ := cryptoAdmissionChannels(t, f)
	cryptoPolicyEnableVerification(t, f)
	p := compat.Params{"pid": "1", "type": "usdt", "channel_id": strconv.FormatInt(channel.ID, 10), "out_trade_no": "verified-standard-usdt", "name": "Verified USDT", "money": "7.20", "sign_type": "MD5"}
	p["sign"] = compat.SignMD5(p, "test-secret")
	form := url.Values{}
	for k, v := range p {
		form.Set(k, v)
	}
	w := defendSigned206Call(t, f.channel168Fixture, "POST", "/submit.php", form)
	match := defendJump206Token.FindStringSubmatch(w.Body.String())
	if w.Code != 200 || len(match) != 2 {
		t.Fatalf("missing standard verification challenge: %d %s", w.Code, w.Body.String())
	}
	token := match[1]
	cryptoPolicyNoAllocation(t, f)
	w = defendSigned206Call(t, f.channel168Fixture, "POST", "/submit.php", defendJump206With(form, token))
	if w.Code != 302 || !strings.Contains(w.Header().Get("Location"), "/crypto/checkout/") {
		t.Fatalf("verified API proof was replaced: %d %s", w.Code, w.Body.String())
	}
	var count int
	if err := f.p.QueryRow(t.Context(), "SELECT count(*) FROM crypto_invoices").Scan(&count); err != nil || count != 1 {
		t.Fatalf("verified allocation %d %v", count, err)
	}
}

func TestCryptoExistingNativeInvoiceReplaysAfterVerificationPolicyChange(t *testing.T) {
	f := cryptoAdmissionFixture(t)
	channel, _ := cryptoAdmissionChannels(t, f)
	body := `{"channel_id":"` + strconv.FormatInt(channel.ID, 10) + `","out_trade_no":"native-existing","amount":"1"}`
	w := cryptoAdmissionConsole(f, "POST", "/invoices", body)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var first struct {
		Data struct {
			ID string `json:"id"`
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	cryptoPolicyEnableVerification(t, f)
	w = cryptoAdmissionConsole(f, "POST", "/invoices", body)
	var repeated struct {
		Data struct {
			ID string `json:"id"`
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &repeated); err != nil || w.Code != 200 || repeated.Data.ID != first.Data.ID {
		t.Fatalf("original replay changed: %d %s %v", w.Code, w.Body.String(), err)
	}
	w = cryptoAdmissionConsole(f, "POST", "/invoices", strings.Replace(body, `"amount":"1"`, `"amount":"2"`, 1))
	if w.Code != 409 {
		t.Fatal("changed original amount accepted", w.Code, w.Body.String())
	}
}
