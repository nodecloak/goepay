package contract_test

import (
	"context"
	"fmt"
	"goepay/internal/compat"
	"goepay/internal/server"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTrustedProxyFeedsPersistentThrottleAndAudit(t *testing.T) {
	s, pool, _ := fixture(t)
	wrap, e := server.NewTrustedProxy("192.0.2.7/32")
	if e != nil {
		t.Fatal(e)
	}
	handler := wrap(s.Handler())
	attempt := func(peer, claimed, user string) int {
		r := httptest.NewRequest("POST", "/console/v1/admin/login", strings.NewReader(fmt.Sprintf(`{"username":%q,"password":"invalid-password"}`, user)))
		r.RemoteAddr = peer
		r.Header.Set("X-Real-IP", claimed)
		r.Header.Set("X-Forwarded-For", "198.51.100.99")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code
	}
	for i := 0; i < 11; i++ {
		want := 401
		if i == 10 {
			want = 429
		}
		if got := attempt(fmt.Sprintf("192.0.2.7:%d", 1000+i), "203.0.113.5", "proxy-limit"); got != want {
			t.Fatalf("trusted client budget %d: %d", i, got)
		}
	}
	if got := attempt("192.0.2.7:9000", "203.0.113.6", "proxy-limit"); got != 401 {
		t.Fatal("separate proxy clients shared account budget", got)
	}
	for i := 0; i < 11; i++ {
		want := 401
		if i == 10 {
			want = 429
		}
		if got := attempt(fmt.Sprintf("192.0.2.8:%d", 2000+i), fmt.Sprintf("203.0.113.%d", 20+i), "direct-limit"); got != want {
			t.Fatalf("untrusted spoof bypassed budget %d: %d", i, got)
		}
	}
	var trusted, direct int
	e = pool.QueryRow(context.Background(), "SELECT count(*) FILTER(WHERE ip='203.0.113.5'),count(*) FILTER(WHERE ip='192.0.2.8') FROM console_login_audit").Scan(&trusted, &direct)
	if e != nil || trusted != 11 || direct != 11 {
		t.Fatalf("wrong audit source %d/%d %v", trusted, direct, e)
	}
}

func TestTrustedProxyClientCannotBypassPaymentBlacklist(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	if _, e := pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('blacklist','{"kind":"ip","value":"203.0.113.5","enabled":true}')`); e != nil {
		t.Fatal(e)
	}
	wrap, e := server.NewTrustedProxy("192.0.2.7/32")
	if e != nil {
		t.Fatal(e)
	}
	p := signed(compat.Params{"pid": "1", "type": "sandbox", "out_trade_no": "proxy-blocked", "name": "test", "money": "1.00", "clientip": "203.0.113.6"}, "contract-secret")
	r := httptest.NewRequest("POST", "/mapi.php", strings.NewReader(p.Encode()))
	r.RemoteAddr = "192.0.2.7:456"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Real-IP", "203.0.113.5")
	w := httptest.NewRecorder()
	wrap(s.Handler()).ServeHTTP(w, r)
	var count int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM orders").Scan(&count); e != nil || count != 0 || !strings.Contains(w.Body.String(), `"code":-1`) {
		t.Fatalf("proxy source blacklist bypass: %d %v", count, e)
	}
}
