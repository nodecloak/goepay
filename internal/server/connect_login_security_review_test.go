package server

import (
	"net/http/httptest"
	"sync"
	"testing"

	"goepay/internal/payment"
)

func TestConnectLoginUsesCurrentAuthVersion(t *testing.T) {
	f := email197Local(t)
	if _, err := f.p.Exec(t.Context(), `UPDATE merchants SET profile=profile||'{"qq_uid":"bound-test"}'::jsonb WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(t.Context(), `UPDATE console_users SET auth_version=auth_version+1 WHERE username='email197' AND role='merchant'`); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	f.s.connectLoginOpenID(w, httptest.NewRequest("GET", "/", nil), "qq", "bound-test", "browser")
	if w.Code != 302 || connectCookie(w, "goepay_merchant") == "" {
		t.Fatalf("bound identity after security update: code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestConnectBindingConcurrentIdentityHasOneOwner(t *testing.T) {
	f := email197Local(t)
	if err := f.s.Store.CreateMerchant(t.Context(), payment.Merchant{ID: 2, Name: "another", APIKey: "another-secret"}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make(chan int, 2)
	for _, mid := range []int64{1, 2} {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			<-start
			w := httptest.NewRecorder()
			f.s.connectBindOpenID(w, httptest.NewRequest("GET", "/", nil), "qq", id, "same-openid")
			results <- w.Code
		}(mid)
	}
	close(start)
	wg.Wait()
	close(results)
	counts := map[int]int{}
	for code := range results {
		counts[code]++
	}
	if counts[302] != 1 || counts[409] != 1 {
		t.Fatalf("concurrent bind results: %v", counts)
	}
}

func TestConnectIdentityCannotBindTwoMerchants(t *testing.T) {
	f := email197Local(t)
	if err := f.s.Store.CreateMerchant(t.Context(), payment.Merchant{ID: 2, Name: "another", APIKey: "another-secret"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(t.Context(), `UPDATE merchants SET profile=profile||'{"qq_uid":"same-openid"}'::jsonb WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(t.Context(), `UPDATE merchants SET profile=profile||'{"qq_uid":"same-openid"}'::jsonb WHERE id=2`); err == nil {
		t.Fatal("same third-party identity may not belong to multiple merchants")
	}
}

func TestConnectLoginDoesNotBypassMFA(t *testing.T) {
	f := email197Local(t)
	if _, err := f.p.Exec(t.Context(), `UPDATE merchants SET profile=profile||'{"qq_uid":"bound-test"}'::jsonb WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(t.Context(), `INSERT INTO console_mfa(username,role,secret,last_counter) VALUES('email197','merchant',$1,-1)`, []byte("synthetic secret")); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	f.s.connectLoginOpenID(w, httptest.NewRequest("GET", "/", nil), "qq", "bound-test", "browser")
	if connectCookie(w, "goepay_merchant") != "" {
		t.Fatal("MFA-enabled account logged in without its second factor")
	}
}
