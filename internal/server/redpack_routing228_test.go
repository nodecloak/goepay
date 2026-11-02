package server

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"goepay/internal/payment"
)

func TestTransferRouting228RedpackCreateRetryRetainsOriginalChannelAndRejectsUnrestorableOAuth(t *testing.T) {
	f, p, _ := redpackFixture(t)
	body := `{"merchant_id":"1","type":"alipay","out_biz_no":"2026110200000000281","amount":"1.00","remark":"synthetic routing228"}`
	w := redpackConsoleCall(f, "admin", [2]string{f.token, f.csrf}, "POST", "", body)
	var first struct{ Data payment.Redpack }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &first) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	setRouting228(t, f, "alipay", "manual")
	w = redpackConsoleCall(f, "admin", [2]string{f.token, f.csrf}, "POST", "", body)
	var again struct{ Data payment.Redpack }
	json.Unmarshal(w.Body.Bytes(), &again)
	if w.Code != 200 || again.Data.ID != first.Data.ID || again.Data.ChannelID != 7 || p.sent.Load() != 0 {
		t.Fatal("creation retry changed packet", w.Code, w.Body.String())
	}
	setup := parent180Outgoing(t)
	setRouting228(t, setup.f, "wxpay", "channel:"+setup.id)
	w = redpackConsoleCall(setup.f, "admin", [2]string{setup.f.token, setup.f.csrf}, "POST", "", strings.ReplaceAll(strings.ReplaceAll(body, "alipay", "wxpay"), "00000281", "00000282"))
	if w.Code != 422 || !strings.Contains(w.Body.String(), "领取授权") {
		t.Fatal("unrestorable OAuth packet created", w.Code, w.Body.String())
	}
}

func TestTransferRouting228RedpackInterruptedClaimHasHonestRecoveryAndOnlyOriginalClaimer(t *testing.T) {
	f, p, _ := redpackFixture(t)
	ctx := t.Context()
	r, err := f.s.Store.CreateRedpack(ctx, payment.RedpackInput{MerchantID: 1, ChannelID: 7, ActorID: 168, OutBizNo: "2026110200000000283", Type: "alipay", Amount: 100, Remark: "synthetic recovery228", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	f.s.ProviderFlow.AllowLive = false
	got, transfer, err := f.s.ProviderFlow.ClaimRedpack(ctx, r.ID, "2088000000000207")
	if err == nil || transfer.ID != 0 || got.Status != "claiming" {
		t.Fatal("interruption fixture", err)
	}
	f.s.ProviderFlow.AllowLive = true
	setRouting228(t, f, "alipay", "manual")
	link, _ := url.Parse(f.s.redpackClaimURL(r))
	landing := redpackPublicCall(f, link.RequestURI())
	if landing.Code != 200 || !strings.Contains(landing.Body.String(), "原领取人继续核对") {
		t.Fatal("no recovery entrance", landing.Code)
	}
	callback := func(subject string) (int, string) {
		f.s.OAuthProviders[7] = &redpackOAuth{subject: subject}
		auth := redpackPublicCall(f, "/pay/red/authorize?"+link.RawQuery)
		location, _ := url.Parse(auth.Header().Get("Location"))
		redirect, _ := url.Parse(location.Query().Get("redirect_uri"))
		uri := redirect.Path + "?" + url.Values{"auth_code": {"auth-code-ok"}, "state": {location.Query().Get("state")}}.Encode()
		result := redpackPublicCall(f, uri, auth.Result().Cookies()...)
		return result.Code, result.Body.String()
	}
	if code, body := callback("2088000000000207"); code != 503 || strings.Contains(body, "领取成功") {
		t.Fatal("unsent falsely paid", code, body)
	}
	if code, _ := callback("2088000000000999"); code != 409 {
		t.Fatal("another claimer resumed", code)
	}
	setRouting228(t, f, "alipay", "inherit")
	if code, body := callback("2088000000000207"); code != 200 || !strings.Contains(body, "已存入支付宝余额") {
		t.Fatal("owner cannot recover", code, body)
	}
	if p.sent.Load() != 1 {
		t.Fatal("recovery send count", strconv.Itoa(int(p.sent.Load())))
	}
}

func TestTransferRouting228RedpackUnknownResumesByReadOnlyQueryWhenNewWritesAreOff(t *testing.T) {
	f, p, _ := redpackFixture(t)
	p.unknown = true
	r, err := f.s.Store.CreateRedpack(t.Context(), payment.RedpackInput{MerchantID: 1, ChannelID: 7, ActorID: 168, OutBizNo: "2026110200000000284", Type: "alipay", Amount: 100, Remark: "synthetic unknown228", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	got, tfr, err := f.s.ProviderFlow.ClaimRedpack(t.Context(), r.ID, "2088000000000207")
	if err == nil || tfr.ID == 0 || got.Status != "claiming" || p.sent.Load() != 1 {
		t.Fatal("unknown fixture", err)
	}
	f.s.ProviderFlow.AllowLive = false
	link, _ := url.Parse(f.s.redpackClaimURL(r))
	auth := redpackPublicCall(f, "/pay/red/authorize?"+link.RawQuery)
	location, _ := url.Parse(auth.Header().Get("Location"))
	redirect, _ := url.Parse(location.Query().Get("redirect_uri"))
	uri := redirect.Path + "?" + url.Values{"auth_code": {"auth-code-ok"}, "state": {location.Query().Get("state")}}.Encode()
	result := redpackPublicCall(f, uri, auth.Result().Cookies()...)
	if result.Code != 200 || !strings.Contains(result.Body.String(), "已存入支付宝余额") || p.sent.Load() != 1 {
		t.Fatal("read-only recovery required new sends", result.Code, result.Body.String())
	}
}
