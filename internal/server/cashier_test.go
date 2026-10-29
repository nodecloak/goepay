package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"goepay/internal/compat"
)

// cashierSignedSubmit posts a signed page submit (submit.php) with the given
// params and returns the recorder. An empty type drives the cashier flow.
func cashierSignedSubmit(f channel168Fixture, extra compat.Params) *httptest.ResponseRecorder {
	p := compat.Params{"pid": "1", "out_trade_no": "cashier-1", "name": "测试商品", "money": "1.00", "notify_url": "https://merchant.example.invalid/notify", "return_url": "https://merchant.example.invalid/return", "sign_type": "MD5"}
	for k, v := range extra {
		if v == "" {
			delete(p, k)
		} else {
			p[k] = v
		}
	}
	p["sign"] = compat.SignMD5(p, "test-secret")
	q := url.Values{}
	for k, v := range p {
		q.Set(k, v)
	}
	r := httptest.NewRequest("POST", "/submit.php", strings.NewReader(q.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.RemoteAddr = "192.0.2.1:555"
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func cashierGet(f channel168Fixture, target string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", target, nil)
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func cashierChoose(f channel168Fixture, target, payType string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", target, strings.NewReader(url.Values{"type": {payType}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.RemoteAddr = "192.0.2.1:555"
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

// An empty-type page submit runs the normal checks, then redirects to the
// cashier; the cashier lists the usable pay types; choosing one creates exactly
// one order and continues to the checkout page; a replay resumes the same order.
func TestCashierEmptyTypeSubmitPicksTypeCreatesOneOrderAndReplays(t *testing.T) {
	f := channel168Local(t)
	ctx := context.Background()

	w := cashierSignedSubmit(f, compat.Params{"type": ""})
	if w.Code != http.StatusFound {
		t.Fatalf("empty-type submit should redirect to cashier: %d %s", w.Code, w.Body)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "/cashier?") {
		t.Fatalf("cashier redirect target: %s", loc)
	}
	// No order exists yet (the cashier intent is not an order).
	if _, e := f.s.Store.Order(ctx, 1, "cashier-1"); e == nil {
		t.Fatal("empty-type submit must not create an order before the payer chooses")
	}

	rel := loc[strings.Index(loc, "/cashier?"):]
	page := cashierGet(f, rel)
	if page.Code != 200 || !strings.Contains(page.Body.String(), `value="alipay"`) {
		t.Fatalf("cashier page should offer alipay: %d %s", page.Code, page.Body)
	}
	if strings.Contains(page.Body.String(), `value="qqpay"`) {
		t.Fatal("cashier offered a closed/unserved type")
	}

	chosen := cashierChoose(f, rel, "alipay")
	if chosen.Code != http.StatusFound || !strings.Contains(chosen.Header().Get("Location"), "/checkout/") {
		t.Fatalf("choosing a type should create the order and go to checkout: %d %s", chosen.Code, chosen.Body)
	}
	o, e := f.s.Store.Order(ctx, 1, "cashier-1")
	if e != nil {
		t.Fatalf("order not created after choice: %v", e)
	}

	// Replay: a repeat click resumes the same order, not a second one.
	again := cashierChoose(f, rel, "alipay")
	if again.Code != http.StatusFound {
		t.Fatalf("replay should resume to checkout: %d %s", again.Code, again.Body)
	}
	o2, e := f.s.Store.Order(ctx, 1, "cashier-1")
	if e != nil || o2.ID != o.ID {
		t.Fatalf("replay created a different order: %v %d/%d", e, o.ID, o2.ID)
	}
	var orders int
	if e := f.p.QueryRow(ctx, `SELECT count(*) FROM orders WHERE merchant_id=1 AND out_trade_no='cashier-1'`).Scan(&orders); e != nil || orders != 1 {
		t.Fatalf("expected exactly one order, got %d (%v)", orders, e)
	}
}

func TestCashierRejectsTamperedExpiredAndClosedType(t *testing.T) {
	f := channel168Local(t)
	w := cashierSignedSubmit(f, compat.Params{"type": ""})
	rel := w.Header().Get("Location")
	rel = rel[strings.Index(rel, "/cashier?"):]

	// Tampered token.
	tampered := strings.Replace(rel, "t=", "t=AAAA", 1)
	if cashierGet(f, tampered).Code != http.StatusBadRequest {
		t.Fatal("tampered cashier token should be rejected")
	}
	// Tampered payload (different amount) with the original token.
	u, _ := url.Parse(rel)
	q := u.Query()
	q.Set("i", q.Get("i")+"x")
	if cashierGet(f, u.Path+"?"+q.Encode()).Code != http.StatusBadRequest {
		t.Fatal("tampered cashier payload should be rejected")
	}
	// A closed type the page never offered is refused on choice.
	if cashierChoose(f, rel, "qqpay").Code != http.StatusBadRequest {
		t.Fatal("a closed/unserved type must be refused on choice")
	}
}

// A merchant listed in pay_force_cashier_pids is sent to the cashier even with
// an explicit type.
func TestCashierForcedMerchantGoesToCashierWithExplicitType(t *testing.T) {
	f := channel168Local(t)
	if _, e := f.p.Exec(context.Background(), `INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key','pay_force_cashier_pids','value','1'))`); e != nil {
		t.Fatal(e)
	}
	w := cashierSignedSubmit(f, compat.Params{"type": "alipay", "method": "native"})
	if w.Code != http.StatusFound || !strings.Contains(w.Header().Get("Location"), "/cashier?") {
		t.Fatalf("forced-cashier merchant should go to cashier even with a type: %d %s", w.Code, w.Body)
	}
}

// mapi.php (/api/pay/create) with an empty type keeps the original error, and
// a forced-cashier merchant on the v2 create gets a jump to the cashier.
func TestCashierMapiEmptyTypeErrorAndV2Jump(t *testing.T) {
	f := channel168Local(t)
	mapi := func(extra compat.Params) map[string]any {
		p := compat.Params{"pid": "1", "out_trade_no": "mapi-1", "name": "测试商品", "money": "1.00", "notify_url": "https://merchant.example.invalid/notify", "return_url": "https://merchant.example.invalid/return", "sign_type": "MD5"}
		for k, v := range extra {
			if v == "" {
				delete(p, k)
			} else {
				p[k] = v
			}
		}
		p["sign"] = compat.SignMD5(p, "test-secret")
		q := url.Values{}
		for k, v := range p {
			q.Set(k, v)
		}
		r := httptest.NewRequest("POST", "/mapi.php", strings.NewReader(q.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.RemoteAddr = "192.0.2.1:555"
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	out := mapi(compat.Params{"type": ""})
	if out["msg"] != "支付方式(type)不能为空" {
		t.Fatalf("mapi empty-type should keep the original error: %v", out)
	}
	if _, e := f.p.Exec(context.Background(), `INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key','pay_force_cashier_pids','value','1'))`); e != nil {
		t.Fatal(e)
	}
	jump := mapi(compat.Params{"type": "alipay", "method": "native"})
	if jump["pay_type"] != "jump" {
		t.Fatalf("forced-cashier v2 create should jump to cashier: %v", jump)
	}
	if url, _ := jump["url"].(string); !strings.Contains(url, "/cashier?") {
		t.Fatalf("v2 jump url should point at the cashier: %v", jump["url"])
	}
}
