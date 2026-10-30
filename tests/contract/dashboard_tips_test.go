package contract_test

import (
	"context"
	"encoding/json"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"goepay/internal/server"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// tipsResponse is the GET /console/v1/merchant/dashboard/tips payload.
type tipsResponse struct {
	Data struct {
		Tips []struct {
			Type     string `json:"type"`
			Title    string `json:"title"`
			Link     string `json:"link"`
			LinkText string `json:"link_text"`
		} `json:"tips"`
	}
}

func fetchMerchantTips(t *testing.T, s *server.Server, token string) (*httptest.ResponseRecorder, tipsResponse) {
	t.Helper()
	r := httptest.NewRequest("GET", "/console/v1/merchant/dashboard/tips", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	var out tipsResponse
	if w.Code == 200 {
		if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
			t.Fatalf("tips decode: %v %s", e, w.Body.String())
		}
	}
	return w, out
}

func tipByLink(resp tipsResponse, link, substr string) bool {
	for _, tip := range resp.Data.Tips {
		if tip.Link == link && strings.Contains(tip.Title, substr) {
			return true
		}
	}
	return false
}

// TestMerchantDashboardTipsConditionsOnAndOff exercises each tip firing when its
// condition holds and vanishing once resolved, all through s.Handler().
func TestMerchantDashboardTipsConditionsOnAndOff(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()

	merchantToken := func() string {
		token, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant"})
		if e != nil {
			t.Fatal(e)
		}
		return token
	}

	// Baseline: fixture merchant 1 has no phone, no email, no settlement
	// recipient and pay=1 (normal). Expect: settlement + phone + email tips,
	// no payment-status tip.
	w, resp := fetchMerchantTips(t, s, merchantToken())
	if w.Code != 200 {
		t.Fatalf("tips %d %s", w.Code, w.Body.String())
	}
	if !tipByLink(resp, "/account/settings", "结算账户") {
		t.Fatalf("missing settlement-account tip: %s", w.Body.String())
	}
	if !tipByLink(resp, "/account/settings", "手机号码") {
		t.Fatalf("missing phone-binding tip: %s", w.Body.String())
	}
	if !tipByLink(resp, "/account/settings", "邮箱") {
		t.Fatalf("missing email-binding tip: %s", w.Body.String())
	}
	for _, tip := range resp.Data.Tips {
		if strings.Contains(tip.Title, "审核") || strings.Contains(tip.Title, "限制") {
			t.Fatalf("unexpected payment-status tip at pay=1: %s", w.Body.String())
		}
		if tip.LinkText == "" {
			t.Fatalf("tip missing link_text: %+v", tip)
		}
		if !strings.HasPrefix(tip.Link, "/account/") {
			t.Fatalf("tip link not a merchant account route: %+v", tip)
		}
	}

	// Turn pay=2 (pending audit): payment-status warning appears.
	if _, e := pool.Exec(ctx, "UPDATE merchants SET pay=2 WHERE id=1"); e != nil {
		t.Fatal(e)
	}
	_, resp = fetchMerchantTips(t, s, merchantToken())
	if !tipByLink(resp, "/account/settings", "审核") {
		t.Fatalf("missing pending-audit tip at pay=2")
	}

	// pay=0 (restricted): error-tone restriction tip.
	if _, e := pool.Exec(ctx, "UPDATE merchants SET pay=0 WHERE id=1"); e != nil {
		t.Fatal(e)
	}
	_, resp = fetchMerchantTips(t, s, merchantToken())
	found := false
	for _, tip := range resp.Data.Tips {
		if strings.Contains(tip.Title, "限制") {
			if tip.Type != "error" {
				t.Fatalf("restricted tip not error tone: %+v", tip)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("missing restricted tip at pay=0")
	}
	if _, e := pool.Exec(ctx, "UPDATE merchants SET pay=1 WHERE id=1"); e != nil {
		t.Fatal(e)
	}

	// Resolve bindings and settlement account: those three tips vanish.
	if _, e := pool.Exec(ctx, `UPDATE merchants SET profile=jsonb_set(jsonb_set(COALESCE(profile,'{}'::jsonb),'{phone}','"13900000000"'),'{email}','"m@example.com"') WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Store.SetSettlementRecipient(ctx, 1, payment.SettlementRecipientInput{Type: "alipay", Account: "payee@example.invalid", Name: "Payee"}, nil, payment.SettlementRecipientActor{Role: "merchant", ID: 1, Name: "merchant"}); e != nil {
		t.Fatal(e)
	}
	w, resp = fetchMerchantTips(t, s, merchantToken())
	if w.Code != 200 {
		t.Fatalf("tips %d %s", w.Code, w.Body.String())
	}
	if len(resp.Data.Tips) != 0 {
		t.Fatalf("expected no tips once resolved, got %s", w.Body.String())
	}
}

// TestMerchantDashboardTipsAuthRefusal confirms the endpoint is merchant-only
// and read-only.
func TestMerchantDashboardTipsAuthRefusal(t *testing.T) {
	s, _, _ := fixture(t)

	// Anonymous: no cookie -> 401.
	r := httptest.NewRequest("GET", "/console/v1/merchant/dashboard/tips", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("anonymous expected 401, got %d %s", w.Code, w.Body.String())
	}

	// Admin session on the (admin) tips route -> refused, never 200.
	token, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	r = httptest.NewRequest("GET", "/console/v1/admin/dashboard/tips", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("admin expected 403 on tips endpoint: %d %s", w.Code, w.Body.String())
	}

	// POST is rejected (read-only endpoint); supply valid CSRF so the method
	// guard, not the CSRF guard, is what refuses it.
	mToken, csrf, e := s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant"})
	if e != nil {
		t.Fatal(e)
	}
	r = httptest.NewRequest("POST", "/console/v1/merchant/dashboard/tips", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: mToken})
	r.Header.Set("X-CSRF-Token", csrf)
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 405 {
		t.Fatalf("POST expected 405, got %d %s", w.Code, w.Body.String())
	}

	// Query parameters are rejected.
	r = httptest.NewRequest("GET", "/console/v1/merchant/dashboard/tips?x=1", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: mToken})
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatalf("query expected 400, got %d %s", w.Code, w.Body.String())
	}
}
