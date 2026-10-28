package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	adapter "goepay/internal/adapters/payment"
)

// wxtransOAuth is the claimer's WeChat official-account OAuth double.
type wxtransOAuth struct{}

func (*wxtransOAuth) ID() string            { return "wxpay" }
func (*wxtransOAuth) ApplicationID() string { return "app168-seven" }
func (*wxtransOAuth) OAuthAuthorizationURL(state, redirect string) (string, error) {
	return "https://open.weixin.qq.com/connect/oauth2/authorize?" + url.Values{"appid": {"app168-seven"}, "scope": {"snsapi_base"}, "state": {state}, "redirect_uri": {redirect}}.Encode(), nil
}
func (*wxtransOAuth) ExchangeOAuth(_ context.Context, code string) (adapter.OAuthIdentity, error) {
	if code != "wx-code-ok" {
		return adapter.OAuthIdentity{}, adapter.ErrOAuthExchange
	}
	return adapter.OAuthIdentity{Provider: "wxpay", AppID: "app168-seven", SubjectID: "claimer-openid"}, nil
}

func TestWxtransRedpackClaimContinuesToPayeeConfirmation(t *testing.T) {
	f, _, _, _ := wxtransFixture(t)
	f.s.OAuthProviders = map[int64]adapter.WebOAuthProvider{7: &wxtransOAuth{}}
	body := `{"merchant_id":"1","channel_id":"7","type":"wxpay","out_biz_no":"2026102812000000001","amount":"1.00","remark":"恭喜发财"}`
	w := redpackConsoleCall(f, "admin", [2]string{f.token, f.csrf}, http.MethodPost, "", body)
	var created struct {
		Data struct {
			ClaimURL string `json:"claim_url"`
		}
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &created) != nil || created.Data.ClaimURL == "" {
		t.Fatalf("wxpay red packet: HTTP=%d %s", w.Code, w.Body.String())
	}
	link, _ := url.Parse(created.Data.ClaimURL)
	auth := redpackPublicCall(f, "/pay/red/authorize?"+link.RawQuery)
	location, _ := url.Parse(auth.Header().Get("Location"))
	if auth.Code != http.StatusFound || location.Host != "open.weixin.qq.com" {
		t.Fatalf("authorize: HTTP=%d %s", auth.Code, auth.Body.String())
	}
	redirect, _ := url.Parse(location.Query().Get("redirect_uri"))
	callback := redirect.Path + "?" + url.Values{"code": {"wx-code-ok"}, "state": {location.Query().Get("state")}}.Encode()
	claim := redpackPublicCall(f, callback, auth.Result().Cookies()...)
	next := claim.Header().Get("Location")
	if claim.Code != http.StatusFound || !strings.HasPrefix(next, "https://pay.example.invalid/pay/wxtrans?") {
		t.Fatalf("claim did not continue to 确认收款: HTTP=%d location=%q %s", claim.Code, next, claim.Body.String())
	}
	page := wxtransCall(f, next, wxtransUA)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "待你收款") || !strings.Contains(page.Body.String(), "1.00") {
		t.Fatalf("claimer confirm page: HTTP=%d %s", page.Code, page.Body.String())
	}
}
