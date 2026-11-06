package contract_test

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/payment"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func providerXML(p map[string]string) []byte {
	var b bytes.Buffer
	b.WriteString("<xml>")
	for k, v := range p {
		b.WriteString("<" + k + ">")
		xml.EscapeText(&b, []byte(v))
		b.WriteString("</" + k + ">")
	}
	b.WriteString("</xml>")
	return b.Bytes()
}
func TestProviderHTTPCheckoutCallbackCommitAndToken(t *testing.T) {
	s, pool, _ := fixture(t)
	key := "test-only-wx-key-12345678901234567"
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		raw, _ := io.ReadAll(r.Body)
		if !bytes.Contains(raw, []byte("<trade_type>NATIVE</trade_type>")) {
			t.Error("wrong method")
		}
		p := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "testapp", "mch_id": "testmch", "code_url": "weixin://test-qrcode"}
		p["sign"], _ = adapter.SignWechatV2(p, key, "MD5")
		w.Write(providerXML(p))
	}))
	defer upstream.Close()
	wx, e := adapter.NewWechatV2(adapter.WechatV2Config{AppID: "testapp", MerchantID: "testmch", APIKey: key, BaseURL: upstream.URL, HTTPClient: upstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: wx}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	s.CheckoutKey = bytes.Repeat([]byte{7}, 32)
	s.MerchantChannels = map[int64][]int64{1: {7}}
	h := s.Handler()
	p := compat.Params{"pid": "1", "out_trade_no": "provider-http", "money": "1.01", "name": "test", "type": "wxpay", "method": "web", "channel_id": "7", "notify_url": "https://merchant.example.invalid/notify", "return_url": "https://merchant.example.invalid/return"}
	_, out := call(t, h, "POST", "/mapi.php", signed(p, "contract-secret"))
	if out["code"] != float64(1) {
		t.Fatalf("create %v", out)
	}
	trade := out["trade_no"].(string)
	link := out["payurl"].(string)
	_, again := call(t, h, "POST", "/mapi.php", signed(p, "contract-secret"))
	if again["trade_no"] != trade || requests.Load() != 1 {
		t.Fatal("duplicate resubmitted upstream")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/checkout/"+trade, nil))
	if w.Code != 403 {
		t.Fatal("unguarded material")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", link, nil))
	// The checkout shows the provider's native code as a QR image rendered by
	// its own signed qr.png endpoint from the stored payment material.
	page := w.Body.String()
	start := strings.Index(page, "/checkout/"+trade+"/qr.png?")
	if w.Code != 200 || start < 0 {
		t.Fatalf("checkout %d %s", w.Code, page)
	}
	qr := html.UnescapeString(page[start : start+strings.Index(page[start:], `"`)])
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", qr, nil))
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("checkout QR %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	var material string
	if e := pool.QueryRow(context.Background(), "SELECT a.material FROM provider_attempts a JOIN orders o ON o.id=a.order_id WHERE o.trade_no=$1", trade).Scan(&material); e != nil || material != "weixin://test-qrcode" {
		t.Fatal("checkout material", material, e)
	}
	u, _ := url.Parse(link)
	u.Path += "/return"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", u.String(), nil))
	if w.Code != 403 {
		t.Fatal("checkout token reused as return token")
	}
	balance, _ := s.Store.Balance(context.Background(), 1)
	if balance.Available != 0 {
		t.Fatal("page request credited payment")
	}
	callback := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "testapp", "mch_id": "testmch", "out_trade_no": trade, "transaction_id": "wx-test-1", "total_fee": "101", "openid": "synthetic-buyer"}
	callback["sign"], _ = adapter.SignWechatV2(callback, key, "MD5")
	if _, e := pool.Exec(context.Background(), `CREATE FUNCTION reject_callback_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected callback failure'; END $$; CREATE TRIGGER reject_callback_test BEFORE INSERT ON ledger_entries FOR EACH ROW EXECUTE FUNCTION reject_callback_test()`); e != nil {
		t.Fatal(e)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/callbacks/providers/7/"+trade, bytes.NewReader(providerXML(callback))))
	if w.Code == 200 {
		t.Fatal("callback ACK emitted before successful ledger commit")
	}
	var buyers int
	if e := pool.QueryRow(context.Background(), "SELECT count(*) FROM order_reporting x JOIN orders o ON o.id=x.order_id WHERE o.trade_no=$1 AND x.buyer<>''", trade).Scan(&buyers); e != nil || buyers != 0 {
		t.Fatal("buyer committed before ledger", buyers, e)
	}
	if _, e := pool.Exec(context.Background(), "DROP TRIGGER reject_callback_test ON ledger_entries"); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		w = httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/callbacks/providers/7/"+trade, bytes.NewReader(providerXML(callback)))
		h.ServeHTTP(w, r)
		if w.Code != 200 || w.Body.String() != `<xml><return_code><![CDATA[SUCCESS]]></return_code><return_msg><![CDATA[OK]]></return_msg></xml>` {
			t.Fatalf("ack %d %s", w.Code, w.Body.String())
		}
	}
	balance, _ = s.Store.Balance(context.Background(), 1)
	if balance.Available != 101 {
		t.Fatalf("balance=%d", balance.Available)
	}
	var buyer string
	if e := pool.QueryRow(context.Background(), "SELECT x.buyer FROM order_reporting x JOIN orders o ON o.id=x.order_id WHERE o.trade_no=$1", trade).Scan(&buyer); e != nil || buyer != "synthetic-buyer" {
		t.Fatal("verified buyer not persisted", buyer, e)
	}
	callback["openid"] = "different-buyer"
	callback["sign"], _ = adapter.SignWechatV2(callback, key, "MD5")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/callbacks/providers/7/"+trade, bytes.NewReader(providerXML(callback))))
	if w.Code == 200 {
		t.Fatal("conflicting buyer metadata acknowledged")
	}
	callback["openid"] = "synthetic-buyer"
	callback["total_fee"] = "102"
	callback["sign"], _ = adapter.SignWechatV2(callback, key, "MD5")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/callbacks/providers/7/"+trade, bytes.NewReader(providerXML(callback))))
	if w.Code == 200 {
		t.Fatal("wrong amount acknowledged")
	}
}
func TestV2AlipayProviderHTTPAndForbiddenChannel(t *testing.T) {
	s, _, merchant := fixture(t)
	platform, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	ali, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "testapp", SellerID: "testseller", PrivateKey: merchant, PublicKey: &platform.PublicKey, GatewayURL: "https://simulated.example.invalid/gateway.do"})
	if e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{9: ali}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	s.CheckoutKey = bytes.Repeat([]byte{8}, 32)
	s.MerchantChannels = map[int64][]int64{2: {9}}
	p := compat.Params{"pid": "2", "out_trade_no": "ali-v2", "money": "1.00", "name": "test", "type": "alipay", "channel_id": "9", "method": "jump", "timestamp": strconv.FormatInt(time.Now().Unix(), 10), "sign_type": "RSA"}
	p["sign"], _ = compat.SignRSA(p, merchant)
	v := url.Values{}
	for k, x := range p {
		v.Set(k, x)
	}
	_, out := call(t, s.Handler(), "POST", "/api/pay/create", v)
	if out["code"] != float64(0) {
		t.Fatalf("create=%v", out)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", out["pay_info"].(string), nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "simulated.example.invalid") {
		t.Fatal("protected signed form missing")
	}
	trade := out["trade_no"].(string)
	callback := map[string]string{"app_id": "testapp", "seller_id": "testseller", "out_trade_no": trade, "trade_no": "ali-test-1", "total_amount": "1.00", "trade_status": "TRADE_SUCCESS", "sign_type": "RSA2"}
	digest := sha256.Sum256([]byte(adapter.AlipayNotificationCanonical(callback)))
	signature, _ := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, digest[:])
	callback["sign"] = base64.StdEncoding.EncodeToString(signature)
	form := url.Values{}
	for k, x := range callback {
		form.Set(k, x)
	}
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("POST", fmt.Sprintf("/callbacks/providers/9/%s", trade), strings.NewReader(form.Encode())))
	if w.Code != 200 || w.Body.String() != "success" {
		t.Fatalf("ack %d %s", w.Code, w.Body.String())
	}
	p["pid"] = "1"
	p["out_trade_no"] = "forbidden"
	_, blocked := call(t, s.Handler(), "POST", "/mapi.php", signed(p, "contract-secret"))
	if blocked["code"] != float64(-1) {
		t.Fatal("forbidden merchant used channel")
	}
}
func TestProviderHTTPQueryRefundAndClose(t *testing.T) {
	s, pool, merchantKey := fixture(t)
	key := "test-only-wx-key-12345678901234567"
	var closes atomic.Int32
	var refundUnknown, refundRejected atomic.Bool
	var refundTrade atomic.Value
	refundTrade.Store("")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Trade  string `xml:"out_trade_no"`
			Refund string `xml:"out_refund_no"`
			Fee    string `xml:"refund_fee"`
		}
		xml.NewDecoder(r.Body).Decode(&in)
		out := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "app", "mch_id": "mch", "out_trade_no": in.Trade}
		switch r.URL.Path {
		case "/pay/unifiedorder":
			out["code_url"] = "weixin://simulation"
		case "/pay/orderquery":
			out["transaction_id"] = "wx-" + in.Trade
			out["total_fee"] = "101"
			out["trade_state"] = "SUCCESS"
		case "/secapi/pay/refund":
			if refundUnknown.Load() {
				w.WriteHeader(500)
				return
			}
			if refundRejected.Load() {
				// A definitive business refusal: nothing was refunded upstream.
				out["result_code"] = "FAIL"
				out["err_code"] = "NOTENOUGH"
				break
			}
			refundTrade.Store(in.Trade)
			out["out_refund_no"] = in.Refund
			out["refund_fee"] = in.Fee
			out["refund_id"] = "wx-refund"
		case "/pay/refundquery":
			out["out_trade_no"] = refundTrade.Load().(string)
			out["total_fee"] = "101"
			out["out_refund_no_0"] = in.Refund
			out["refund_fee_0"] = "1"
			out["refund_id_0"] = "wx-refund"
			out["refund_status_0"] = "SUCCESS"
		case "/pay/closeorder":
			closes.Add(1)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
		out["sign"], _ = adapter.SignWechatV2(out, key, "MD5")
		w.Write(providerXML(out))
	}))
	defer upstream.Close()
	wx, e := adapter.NewWechatV2(adapter.WechatV2Config{AppID: "app", MerchantID: "mch", APIKey: key, BaseURL: upstream.URL, HTTPClient: upstream.Client(), RefundClient: upstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: wx}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	s.CheckoutKey = bytes.Repeat([]byte{3}, 32)
	s.MerchantChannels = map[int64][]int64{2: {7}}
	invoke := func(path string, p compat.Params) map[string]any {
		t.Helper()
		p["pid"] = "2"
		p["sign_type"] = "RSA"
		p["timestamp"] = strconv.FormatInt(time.Now().Unix(), 10)
		p["sign"], _ = compat.SignRSA(p, merchantKey)
		v := url.Values{}
		for k, x := range p {
			v.Set(k, x)
		}
		_, out := call(t, s.Handler(), "POST", path, v)
		return out
	}
	create := func(external string) string {
		t.Helper()
		out := invoke("/api/pay/create", compat.Params{"out_trade_no": external, "name": "test", "money": "1.01", "type": "wxpay", "channel_id": "7"})
		if out["code"] != float64(0) {
			t.Fatal(out)
		}
		return out["trade_no"].(string)
	}
	trade := create("ops-paid")
	prevalidation := invoke("/api/pay/refund", compat.Params{"trade_no": trade, "out_refund_no": "unpaid-refund", "money": "0.01"})
	if prevalidation["code"] != float64(-1) {
		t.Fatal("unpaid refund falsely accepted")
	}
	var rejectedRows int
	if e := pool.QueryRow(context.Background(), "SELECT count(*) FROM refunds WHERE refund_no='unpaid-refund'").Scan(&rejectedRows); e != nil || rejectedRows != 0 {
		t.Fatal("prevalidation reserved a refund")
	}
	out := invoke("/api/pay/query", compat.Params{"trade_no": trade})
	if out["code"] != float64(0) || out["status"] != float64(1) {
		t.Fatal("verified query not credited", out)
	}
	out = invoke("/api/pay/refund", compat.Params{"trade_no": trade, "out_refund_no": "stable-refund", "money": "0.01"})
	if out["code"] != float64(0) {
		t.Fatal(out)
	}
	out = invoke("/api/pay/refundquery", compat.Params{"out_refund_no": "stable-refund"})
	if out["code"] != float64(0) || out["status"] != float64(1) {
		t.Fatal("refund not reconciled", out)
	}
	settled := invoke("/api/pay/refund", compat.Params{"trade_no": trade, "out_refund_no": "stable-refund", "money": "0.01"})
	if settled["code"] != float64(0) || settled["status"] != float64(1) || settled["refund_no"] != out["refund_no"] || settled["refund_state"] != "succeeded" || out["refund_state"] != "succeeded" {
		t.Fatal("replayed succeeded refund lost its final state", settled)
	}
	closesBefore := closes.Load()
	paidClose := invoke("/api/pay/close", compat.Params{"trade_no": trade})
	if paidClose["code"] != float64(-1) || paidClose["msg"] != "order already paid; close is not a refund" || closes.Load() != closesBefore {
		t.Fatal("closing a paid order was not refused locally", paidClose)
	}
	balance, _ := s.Store.Balance(context.Background(), 2)
	if balance.Available != 100 || balance.Reserved != 0 {
		t.Fatal(balance)
	}
	hold, e := s.Store.ReserveTransfer(context.Background(), 2, "test-hold", 100)
	if e != nil {
		t.Fatal(e)
	}
	insufficient := invoke("/api/pay/refund", compat.Params{"trade_no": trade, "out_refund_no": "insufficient-refund", "money": "0.01"})
	if insufficient["code"] != float64(-1) {
		t.Fatal("insufficient refund falsely accepted")
	}
	if e := pool.QueryRow(context.Background(), "SELECT count(*) FROM refunds WHERE refund_no='insufficient-refund'").Scan(&rejectedRows); e != nil || rejectedRows != 0 {
		t.Fatal("insufficient refund row committed")
	}
	if e := s.Store.ResolveTransfer(context.Background(), hold.ID, "failed"); e != nil {
		t.Fatal(e)
	}
	refundUnknown.Store(true)
	unknown := invoke("/api/pay/refund", compat.Params{"trade_no": trade, "out_refund_no": "unknown-refund", "money": "0.01"})
	if unknown["code"] != float64(0) || unknown["refund_no"] == nil {
		t.Fatal("persisted unknown refund not reported accepted", unknown)
	}
	refundUnknown.Store(false)
	refundRejected.Store(true)
	rejected := invoke("/api/pay/refund", compat.Params{"trade_no": trade, "out_refund_no": "rejected-refund", "money": "0.01"})
	if rejected["code"] != float64(-1) || rejected["msg"] != "refund failed" || rejected["status"] != float64(0) || rejected["refund_state"] != "failed" || rejected["refund_no"] == nil {
		t.Fatal("definitive refund refusal not reported as a final failure", rejected)
	}
	// The merchant retries with the same out_refund_no (the documented recovery
	// path). The stored failure must be repeated, never the success shape.
	replayed := invoke("/api/pay/refund", compat.Params{"trade_no": trade, "out_refund_no": "rejected-refund", "money": "0.01"})
	if replayed["code"] != float64(-1) || replayed["msg"] != "refund failed" || replayed["status"] != float64(0) || replayed["refund_no"] != rejected["refund_no"] {
		t.Fatal("replayed failed refund reported as accepted", replayed)
	}
	refundRejected.Store(false)
	refunds := func() (n int) {
		t.Helper()
		if e := pool.QueryRow(context.Background(), "SELECT count(*) FROM refunds").Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	// Without out_refund_no, a retry after a timeout must not refund twice.
	before := refunds()
	first := invoke("/api/pay/refund", compat.Params{"trade_no": trade, "money": "0.01"})
	retry := invoke("/api/pay/refund", compat.Params{"trade_no": trade, "money": "0.01"})
	if first["code"] != float64(0) || first["out_refund_no"] == nil || retry["out_refund_no"] != first["out_refund_no"] || retry["refund_no"] != first["refund_no"] || refunds() != before+1 {
		t.Fatal("unnumbered refund retry created a second refund", first, retry)
	}
	// After a definitive refusal, an unnumbered retry is a new refund attempt.
	refundRejected.Store(true)
	refused := invoke("/api/pay/refund", compat.Params{"trade_no": trade, "money": "0.02"})
	refundRejected.Store(false)
	again := invoke("/api/pay/refund", compat.Params{"trade_no": trade, "money": "0.02"})
	if refused["status"] != float64(0) || again["code"] != float64(0) || again["out_refund_no"] == refused["out_refund_no"] || refunds() != before+3 {
		t.Fatal("unnumbered retry after a final failure was not a new refund", refused, again)
	}
	closing := create("ops-close")
	for i := 0; i < 2; i++ {
		out = invoke("/api/pay/close", compat.Params{"trade_no": closing})
		if out["code"] != float64(0) {
			t.Fatal(out)
		}
	}
	if closes.Load() != 1 {
		t.Fatal("duplicate close upstream")
	}
}
func TestMerchantReturnUsesVerifiedPaymentAndStoredSignedURL(t *testing.T) {
	for _, version := range []string{"v1", "v2"} {
		t.Run(version, func(t *testing.T) {
			s, _, rsaKey := fixture(t)
			key := "test-only-wx-key-12345678901234567"
			var paid atomic.Bool
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var in struct {
					Trade string `xml:"out_trade_no"`
				}
				xml.NewDecoder(r.Body).Decode(&in)
				out := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "app", "mch_id": "mch", "out_trade_no": in.Trade}
				if r.URL.Path == "/pay/orderquery" {
					out["total_fee"] = "101"
					out["trade_state"] = "NOTPAY"
					if paid.Load() {
						out["trade_state"] = "SUCCESS"
						out["transaction_id"] = "verified-return-payment"
					}
				} else {
					out["code_url"] = "weixin://simulation"
				}
				out["sign"], _ = adapter.SignWechatV2(out, key, "MD5")
				w.Write(providerXML(out))
			}))
			defer upstream.Close()
			wx, _ := adapter.NewWechatV2(adapter.WechatV2Config{AppID: "app", MerchantID: "mch", APIKey: key, BaseURL: upstream.URL, HTTPClient: upstream.Client()})
			s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: wx}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
			s.CheckoutKey = bytes.Repeat([]byte{9}, 32)
			s.ProviderFlow.ReturnURLBuilder = s.ProviderReturnURL
			pid := int64(1)
			if version == "v2" {
				pid = 2
			}
			s.MerchantChannels = map[int64][]int64{pid: {7}}
			p := compat.Params{"pid": fmt.Sprint(pid), "out_trade_no": "return-check", "name": "test", "money": "1.01", "type": "wxpay", "return_url": "https://merchant.example.invalid/after?client=1", "notify_url": "https://merchant.example.invalid/notify"}
			path := "/mapi.php"
			form := url.Values{}
			if version == "v1" {
				form = signed(p, "contract-secret")
			} else {
				path = "/api/pay/create"
				p["sign_type"] = "RSA"
				p["timestamp"] = fmt.Sprint(time.Now().Unix())
				p["sign"], _ = compat.SignRSA(p, rsaKey)
				for k, x := range p {
					form.Set(k, x)
				}
			}
			_, out := call(t, s.Handler(), "POST", path, form)
			trade := out["trade_no"].(string)
			returnURL := s.ProviderReturnURL(trade)
			u, _ := url.Parse(returnURL)
			q := u.Query()
			q.Set("trade_status", "TRADE_SUCCESS")
			q.Set("return_url", "https://evil.invalid/")
			u.RawQuery = q.Encode()
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, httptest.NewRequest("GET", u.String(), nil))
			if w.Code != 200 || w.Header().Get("Location") != "" {
				t.Fatal("unpaid return redirected")
			}
			balance, _ := s.Store.Balance(context.Background(), pid)
			if balance.Available != 0 {
				t.Fatal("unpaid return credited funds")
			}
			paid.Store(true)
			w = httptest.NewRecorder()
			s.Handler().ServeHTTP(w, httptest.NewRequest("GET", u.String(), nil))
			if w.Code != http.StatusFound {
				t.Fatal("paid return did not reach merchant", w.Code)
			}
			target, e := url.Parse(w.Header().Get("Location"))
			if e != nil || target.Host != "merchant.example.invalid" || target.Path != "/after" || target.Query().Get("client") != "1" {
				t.Fatal("return target changed")
			}
			params, e := compat.Parse(target.Query())
			if e != nil {
				t.Fatal(e)
			}
			if version == "v1" {
				if !compat.VerifyMD5(params, "contract-secret", params["sign"]) {
					t.Fatal("merchant MD5 return invalid")
				}
			} else if e := compat.Verify(params, compat.Credentials{Algorithm: "RSA", PublicKey: &rsaKey.PublicKey, Profile: compat.ProfileSDKV2}, time.Now()); e != nil {
				t.Fatal("merchant RSA return invalid", e)
			}
		})
	}
}
