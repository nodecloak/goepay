package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const publicTestPay203Prefix = "/console/v1/public/testpay"

type publicTestPay203Init struct {
	CSRF        string `json:"csrf_token"`
	TradeNo     string `json:"trade_no"`
	Money       string `json:"money"`
	SiteName    string `json:"sitename"`
	CaptchaOpen int    `json:"captcha_open"`
	PayTypes    []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		ShowName string `json:"showname"`
	} `json:"paytype"`
}

func publicTestPay203Request(f channel168Fixture, method, path, body string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = "192.0.2.203:5203"
	r.Header.Set("Origin", "https://pay.example.invalid")
	if method != http.MethodGet {
		r.Header.Set("Content-Type", "application/json")
	}
	for _, cookie := range cookies {
		r.AddCookie(cookie)
	}
	out := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(out, r)
	return out
}

func publicTestPay203Ready(t *testing.T) channel168Fixture {
	t.Helper()
	f := channel168Local(t)
	f.s.Sandbox = true
	f.s.ProviderFlow.LocalSimulation = true
	for key, value := range map[string]string{"test_open": "1", "test_pay_uid": "1", "captcha_open_test": "0", "sitename": "公开测试203"} {
		site191Save(t, f, key, value)
	}
	var group int64
	if err := f.p.QueryRow(t.Context(), `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"enabled":true,"info":{"alipay":{"channel":"7","type":"channel"}}}') RETURNING id`).Scan(&group); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(t.Context(), `UPDATE merchants SET base_group_id=$1 WHERE id=1`, group); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(t.Context(), `INSERT INTO backoffice_records(id,resource,body) VALUES(700203,'pay-types','{"name":"支付宝203","type":"alipay","enabled":true}')`); err != nil {
		t.Fatal(err)
	}
	return f
}

func publicTestPay203ReadInit(t *testing.T, f channel168Fixture) (publicTestPay203Init, []*http.Cookie) {
	t.Helper()
	out := publicTestPay203Request(f, "GET", publicTestPay203Prefix+"/init", "", nil)
	if out.Code != http.StatusOK || out.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("public TestPay must initialize without a console session: HTTP=%d", out.Code)
	}
	var envelope struct {
		Code int                  `json:"code"`
		Data publicTestPay203Init `json:"data"`
	}
	if json.Unmarshal(out.Body.Bytes(), &envelope) != nil || envelope.Code != 0 || envelope.Data.CSRF == "" || envelope.Data.TradeNo == "" {
		t.Fatal("public TestPay omitted the original bound csrf_token/trade_no fields")
	}
	return envelope.Data, out.Result().Cookies()
}

func publicTestPay203Effects(t *testing.T, f channel168Fixture) string {
	t.Helper()
	var result string
	if err := f.p.QueryRow(t.Context(), `SELECT jsonb_build_object('orders',(SELECT count(*) FROM orders),'attempts',(SELECT count(*) FROM provider_attempts),'jobs',(SELECT count(*) FROM jobs),'journals',(SELECT count(*) FROM journals),'refunds',(SELECT count(*) FROM refunds))::text`).Scan(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestPublicTestPay203ActualHTTPDefaultIsClosedWithoutCreatingPaymentWork(t *testing.T) {
	f := channel168Local(t)
	before := publicTestPay203Effects(t, f)
	out := publicTestPay203Request(f, "GET", publicTestPay203Prefix+"/init", "", nil)
	if out.Code != http.StatusForbidden {
		t.Fatalf("unconfigured public TestPay must be explicitly closed: HTTP=%d, want403", out.Code)
	}
	if publicTestPay203Effects(t, f) != before {
		t.Fatal("closed TestPay initialization created payment work")
	}
}

func TestPublicTestPay203ActualHTTPPublishesOriginalFieldsWithoutConsoleCredentialsOrPrivateConfig(t *testing.T) {
	f := publicTestPay203Ready(t)
	before := publicTestPay203Effects(t, f)
	data, cookies := publicTestPay203ReadInit(t, f)
	if data.Money != "1.00" || data.SiteName != "公开测试203" || data.CaptchaOpen != 0 {
		t.Fatal("public TestPay lost original amount/site/captcha fields")
	}
	found := false
	for _, method := range data.PayTypes {
		if method.Name == "alipay" && method.ShowName == "支付宝203" && method.ID == "700203" {
			found = true
		}
	}
	if !found {
		t.Fatal("public TestPay did not expose the configured permitted payment type")
	}
	if len(cookies) == 0 {
		t.Fatal("public TestPay did not bind its original intent to a browser")
	}
	for _, cookie := range cookies {
		if cookie.Name == "goepay_admin" || cookie.Name == "goepay_merchant" || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
			t.Fatal("public TestPay reused a console identity or issued an unprotected browser cookie")
		}
	}
	out := publicTestPay203Request(f, "GET", publicTestPay203Prefix+"/init", "", nil)
	for _, private := range []string{"test_pay_uid", "captcha_key", "api_key", "app168-", f.token, f.csrf} {
		if strings.Contains(out.Body.String(), private) {
			t.Fatal("public TestPay exposed private configuration or console authority")
		}
	}
	if publicTestPay203Effects(t, f) != before {
		t.Fatal("reading public TestPay initialized a payment or provider operation")
	}
}

func TestPublicTestPay203ActualHTTPUsesFixedMerchantOriginalIntentAndOrdinarySimulationOnce(t *testing.T) {
	f := publicTestPay203Ready(t)
	init, cookies := publicTestPay203ReadInit(t, f)
	body, _ := json.Marshal(map[string]string{"money": "1.23", "typeid": "700203", "csrf_token": init.CSRF})
	out := publicTestPay203Request(f, "POST", publicTestPay203Prefix+"/submit", string(body), cookies)
	if out.Code != http.StatusOK {
		t.Fatalf("public TestPay did not reach controlled ordinary payment orchestration: HTTP=%d", out.Code)
	}
	var result struct {
		Data struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if json.Unmarshal(out.Body.Bytes(), &result) != nil {
		t.Fatal("public submit response was not JSON")
	}
	checkout, err := url.Parse(result.Data.URL)
	if err != nil || checkout.IsAbs() || !strings.HasPrefix(checkout.Path, "/checkout/") || checkout.Query().Get("token") == "" || checkout.Query().Get("expires") == "" {
		t.Fatal("public submit did not return a relative protected original checkout")
	}
	var merchant, amount int64
	var trade, original, name string
	if err = f.p.QueryRow(t.Context(), `SELECT merchant_id,amount,trade_no,out_trade_no,name FROM orders`).Scan(&merchant, &amount, &trade, &original, &name); err != nil || merchant != 1 || amount != 123 || original != init.TradeNo || name != "支付测试" {
		t.Fatal("public test order changed the fixed merchant, original intent or exact amount")
	}
	if checkout.Path != "/checkout/"+trade || f.s.ProviderFlow.Providers[7].(*channel168Provider).sends.Load() != 1 {
		t.Fatal("public test did not use the original controlled payment route")
	}
	if replay := publicTestPay203Request(f, "POST", publicTestPay203Prefix+"/submit", string(body), cookies); replay.Code != http.StatusOK || f.s.ProviderFlow.Providers[7].(*channel168Provider).sends.Load() != 1 {
		t.Fatal("same public intent rearmed a provider send")
	}
	var orders int
	if err = f.p.QueryRow(t.Context(), "SELECT count(*) FROM orders").Scan(&orders); err != nil || orders != 1 {
		t.Fatal("same public intent created a second order")
	}
	pending := publicTestPay203Request(f, "GET", publicTestPay203Prefix+"/success?trade_no="+url.QueryEscape(trade), "", cookies)
	if pending.Code != http.StatusConflict {
		t.Fatal("pending public test was reported as paid", pending.Code)
	}
	if err = f.s.Store.MarkPaid(t.Context(), trade, "synthetic-public-testpay203"); err != nil {
		t.Fatal(err)
	}
	paid := publicTestPay203Request(f, "GET", publicTestPay203Prefix+"/success?trade_no="+url.QueryEscape(trade), "", cookies)
	var success struct {
		Data struct {
			Money    string `json:"money"`
			SiteName string `json:"sitename"`
		} `json:"data"`
	}
	if paid.Code != http.StatusOK || json.Unmarshal(paid.Body.Bytes(), &success) != nil || success.Data.Money != "1.23" || success.Data.SiteName != "公开测试203" {
		t.Fatal("public success did not read the verified original paid order")
	}
}

func TestPublicTestPay203ActualHTTPRejectsOverridesAndCrossBrowserOrMissingCSRFBeforeAnyPayment(t *testing.T) {
	f := publicTestPay203Ready(t)
	init, cookies := publicTestPay203ReadInit(t, f)
	before := publicTestPay203Effects(t, f)
	base := map[string]string{"money": "1.23", "typeid": "700203", "csrf_token": init.CSRF}
	for _, field := range []string{"merchant_id", "pid", "trade_no", "out_trade_no", "name", "channel_id", "notify_url", "return_url", "provider_url"} {
		t.Run(field, func(t *testing.T) {
			input := map[string]string{}
			for key, value := range base {
				input[key] = value
			}
			input[field] = "caller-controlled203"
			body, _ := json.Marshal(input)
			out := publicTestPay203Request(f, "POST", publicTestPay203Prefix+"/submit", string(body), cookies)
			if out.Code != http.StatusBadRequest {
				t.Error("caller override accepted", field, out.Code)
			}
		})
	}
	body, _ := json.Marshal(base)
	if out := publicTestPay203Request(f, "POST", publicTestPay203Prefix+"/submit", string(body), nil); out.Code != http.StatusForbidden {
		t.Error("public TestPay CSRF worked in another browser", out.Code)
	}
	base["csrf_token"] = ""
	body, _ = json.Marshal(base)
	if out := publicTestPay203Request(f, "POST", publicTestPay203Prefix+"/submit", string(body), cookies); out.Code != http.StatusForbidden {
		t.Error("public TestPay accepted missing CSRF", out.Code)
	}
	if publicTestPay203Effects(t, f) != before {
		t.Fatal("rejected public input created payment work")
	}
}

func TestPublicTestPay203ActualHTTPUsesExactMethodsAndQueriesForTestPayAndCaptchaPurpose(t *testing.T) {
	f := publicTestPay203Ready(t)
	for _, tc := range []struct {
		method, path, body string
		want               int
	}{
		{"GET", publicTestPay203Prefix + "/init?merchant_id=2", "", 400},
		{"POST", publicTestPay203Prefix + "/init", `{}`, 405},
		{"GET", publicTestPay203Prefix + "/submit", "", 405},
		{"POST", publicTestPay203Prefix + "/success", `{}`, 405},
		{"GET", publicTestPay203Prefix + "/success?trade_no=outside203&ok=1", "", 400},
		{"POST", "/console/v1/public/captcha?purpose=testpay", `{}`, 405},
		{"GET", "/console/v1/public/captcha?purpose=unrecognized203", "", 400},
		{"GET", "/console/v1/public/captcha?purpose=testpay&purpose=login", "", 400},
		{"GET", "/console/v1/public/captcha?purpose=testpay&captcha_key=caller203", "", 400},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			if out := publicTestPay203Request(f, tc.method, tc.path, tc.body, nil); out.Code != tc.want {
				t.Errorf("public TestPay/CAPTCHA boundary HTTP=%d, want%d", out.Code, tc.want)
			}
		})
	}
}

func TestPublicTestPay203ActualHTTPCaptchaUnavailableFailsClosedWithoutExposingLegacyPlaintextKey(t *testing.T) {
	f := publicTestPay203Ready(t)
	site191Save(t, f, "captcha_open_test", "1")
	site191Save(t, f, "captcha_id", strings.Repeat("a", 32))
	site191Save(t, f, "captcha_version", "1")
	site191Save(t, f, "captcha_key", "legacy-plaintext-must-not-be-adopted203")
	before := publicTestPay203Effects(t, f)
	out := publicTestPay203Request(f, "GET", "/console/v1/public/captcha?purpose=testpay", "", nil)
	if out.Code != http.StatusServiceUnavailable || strings.Contains(out.Body.String(), "legacy-plaintext-must-not-be-adopted203") || strings.Contains(out.Body.String(), "captcha_key") {
		t.Fatal("unavailable CAPTCHA adopted or exposed an unsealed key", out.Code)
	}
	out = publicTestPay203Request(f, "POST", publicTestPay203Prefix+"/submit", `{"money":"1.23","typeid":"700203","csrf_token":"invented203","captcha_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","lot_number":"fabricated203","pass_token":"fabricated203","gen_time":"1","captcha_output":"fabricated203"}`, nil)
	if out.Code < 400 || strings.Contains(out.Body.String(), `"url"`) {
		t.Fatal("fabricated CAPTCHA output reached payment")
	}
	if publicTestPay203Effects(t, f) != before {
		t.Fatal("unavailable CAPTCHA created payment work")
	}
}
