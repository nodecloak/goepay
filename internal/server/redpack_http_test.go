package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

// redpackProvider is an in-process transfer double on channel 7 (alipay).
type redpackProvider struct {
	*channel168Provider
	unknown  bool
	sent     atomic.Int32
	accounts []string
}

func (p *redpackProvider) TransferTypes() []string { return []string{"alipay"} }
func (p *redpackProvider) Transfer(_ context.Context, r adapter.TransferRequest) (adapter.TransferResult, error) {
	p.sent.Add(1)
	p.accounts = append(p.accounts, r.Account)
	if p.unknown {
		return adapter.TransferResult{}, errors.New("synthetic unknown result")
	}
	return adapter.TransferResult{TransferNo: r.TransferNo, ProviderTransferNo: "ALI_" + r.TransferNo, State: "SUCCESS", Amount: r.Amount, PayDate: "2026-10-28 10:00:00"}, nil
}
func (p *redpackProvider) QueryTransfer(_ context.Context, r adapter.TransferRequest) (adapter.TransferResult, error) {
	return adapter.TransferResult{TransferNo: r.TransferNo, ProviderTransferNo: "ALI_" + r.TransferNo, State: "SUCCESS", Amount: r.Amount}, nil
}

// redpackOAuth is the claimer's platform OAuth double.
type redpackOAuth struct{ subject string }

func (o *redpackOAuth) ID() string            { return "alipay" }
func (o *redpackOAuth) ApplicationID() string { return "app168-seven" }
func (o *redpackOAuth) OAuthAuthorizationURL(state, redirect string) (string, error) {
	return "https://openauth.alipay.com/oauth2/publicAppAuthorize.htm?" + url.Values{"app_id": {"app168-seven"}, "scope": {"auth_base"}, "state": {state}, "redirect_uri": {redirect}}.Encode(), nil
}
func (o *redpackOAuth) ExchangeOAuth(_ context.Context, code string) (adapter.OAuthIdentity, error) {
	if code != "auth-code-ok" {
		return adapter.OAuthIdentity{}, adapter.ErrOAuthExchange
	}
	return adapter.OAuthIdentity{Provider: "alipay", AppID: "app168-seven", SubjectID: o.subject}, nil
}

func redpackFixture(t *testing.T) (channel168Fixture, *redpackProvider, [2]string) {
	t.Helper()
	f := channel168Local(t)
	p := &redpackProvider{channel168Provider: &channel168Provider{app: "app168-seven"}}
	f.s.ProviderFlow.Providers[7] = p
	f.s.ProviderFlow.AllowLive = true
	f.s.ProviderFlow.CallbackBaseURL = "https://pay.example.invalid"
	f.s.OAuthProviders = map[int64]adapter.WebOAuthProvider{7: &redpackOAuth{subject: "2088000000000207"}}
	ctx := t.Context()
	if _, err := f.p.Exec(ctx, `UPDATE merchants SET profile=profile||'{"transfer_api_enabled":true,"transfer_rate":"0"}'::jsonb WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"transfer_enabled","value":"true"}')`); err != nil {
		t.Fatal(err)
	}
	o, err := f.s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "redpack-fund", Name: "fund", Type: "alipay", Amount: 10000})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.Store.MarkPaid(ctx, o.TradeNo, "redpack-fund"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.p.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) VALUES('redpackmerchant','merchant',1,'Synthetic Merchant','unused-synthetic-only','operator')`); err != nil {
		t.Fatal(err)
	}
	merchant, merchantCSRF, err := f.s.Sessions.Create(identity.User{ID: 1, Role: "merchant", Username: "redpackmerchant", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	return f, p, [2]string{merchant, merchantCSRF}
}

func redpackConsoleCall(f channel168Fixture, role string, auth [2]string, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/console/v1/"+role+"/redpacks"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", auth[1])
	if role == "admin" {
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: auth[0]})
	} else {
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: auth[0]})
	}
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func redpackPublicCall(f channel168Fixture, target string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, target, nil)
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func TestRedpackHTTPCreateClaimThroughSignedQRAndOAuth(t *testing.T) {
	f, p, _ := redpackFixture(t)
	body := `{"merchant_id":"1","channel_id":"7","type":"alipay","out_biz_no":"2026102811000000001","amount":"1.00","remark":"恭喜发财"}`
	w := redpackConsoleCall(f, "admin", [2]string{f.token, f.csrf}, http.MethodPost, "", body)
	var created struct {
		Code int
		Data struct {
			ID       string `json:"id"`
			Status   string `json:"status"`
			ClaimURL string `json:"claim_url"`
		}
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &created) != nil || created.Data.Status != "claimable" || !strings.HasPrefix(created.Data.ClaimURL, "https://pay.example.invalid/pay/red?") {
		t.Fatalf("admin create failed: HTTP=%d body=%s", w.Code, w.Body.String())
	}
	link, _ := url.Parse(created.Data.ClaimURL)
	landing := redpackPublicCall(f, link.RequestURI())
	if landing.Code != 200 || !strings.Contains(landing.Body.String(), "￥1.00") || !strings.Contains(landing.Body.String(), "/pay/red/authorize?") {
		t.Fatalf("landing page wrong: HTTP=%d body=%s", landing.Code, landing.Body.String())
	}
	tampered := link.Query()
	tampered.Set("s", strings.Repeat("0", 64))
	if w := redpackPublicCall(f, "/pay/red?"+tampered.Encode()); w.Code != 400 {
		t.Fatal("tampered signature accepted", w.Code)
	}
	auth := redpackPublicCall(f, "/pay/red/authorize?"+link.RawQuery)
	location, _ := url.Parse(auth.Header().Get("Location"))
	if auth.Code != http.StatusFound || location.Host != "openauth.alipay.com" {
		t.Fatalf("authorize did not start OAuth: HTTP=%d %s", auth.Code, auth.Body.String())
	}
	cookies := auth.Result().Cookies()
	redirect, _ := url.Parse(location.Query().Get("redirect_uri"))
	state := location.Query().Get("state")
	callback := redirect.Path + "?" + url.Values{"auth_code": {"auth-code-ok"}, "state": {state}}.Encode()
	if w := redpackPublicCall(f, callback); w.Code != 400 || p.sent.Load() != 0 {
		t.Fatal("callback without the browser cookie claimed", w.Code)
	}
	claimed := redpackPublicCall(f, callback, cookies...)
	if claimed.Code != 200 || !strings.Contains(claimed.Body.String(), "已存入支付宝余额") || p.sent.Load() != 1 || p.accounts[0] != "2088000000000207" {
		t.Fatalf("claim failed: HTTP=%d body=%s sent=%d", claimed.Code, claimed.Body.String(), p.sent.Load())
	}
	if again := redpackPublicCall(f, link.RequestURI()); again.Code != 409 || !strings.Contains(again.Body.String(), "已被领取") {
		t.Fatal("claimed packet still claimable", again.Code)
	}
	if replay := redpackPublicCall(f, callback, cookies...); p.sent.Load() != 1 {
		t.Fatal("callback replay resent the transfer", replay.Code)
	}
	list := redpackConsoleCall(f, "admin", [2]string{f.token, f.csrf}, http.MethodGet, "", "")
	if list.Code != 200 || !strings.Contains(list.Body.String(), `"status":"succeeded"`) {
		t.Fatalf("admin list wrong: %s", list.Body.String())
	}
}

func TestRedpackHTTPMerchantRequiresOriginalSwitch(t *testing.T) {
	f, _, merchant := redpackFixture(t)
	body := `{"channel_id":"7","type":"alipay","out_biz_no":"2026102811000000002","amount":"1.00","remark":"x"}`
	if w := redpackConsoleCall(f, "merchant", merchant, http.MethodPost, "", body); w.Code != 403 || !strings.Contains(w.Body.String(), "未开启创建红包功能") {
		t.Fatalf("merchant created a red packet with user_transfer_red off: HTTP=%d %s", w.Code, w.Body.String())
	}
	if _, err := f.p.Exec(t.Context(), `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"user_transfer_red","value":"1"}')`); err != nil {
		t.Fatal(err)
	}
	if w := redpackConsoleCall(f, "merchant", merchant, http.MethodPost, "", body); w.Code != 200 {
		t.Fatalf("merchant create with switch on failed: HTTP=%d %s", w.Code, w.Body.String())
	}
	if w := redpackConsoleCall(f, "merchant", merchant, http.MethodPost, "", `{"channel_id":"7","type":"alipay","out_biz_no":"2026102811000000002","amount":"2.00","remark":"x"}`); w.Code != 409 || !strings.Contains(w.Body.String(), "该交易号已存在") {
		t.Fatalf("changed duplicate 交易号 accepted: HTTP=%d %s", w.Code, w.Body.String())
	}
}
