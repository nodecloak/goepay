package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/db"
)

// /agreement.html is answered before the payment API catch-all; /agreement
// is the original short address.
func TestAgreementRoutesBeforePaymentAPI(t *testing.T) {
	h := (&Server{}).Handler()
	for _, c := range []struct {
		method, path string
		code         int
		location     string
	}{
		{"GET", "/agreement", http.StatusFound, "/agreement.html"},
		{"HEAD", "/agreement", http.StatusFound, "/agreement.html"},
		{"POST", "/agreement.html", http.StatusMethodNotAllowed, ""},
		{"GET", "/agreement.html", http.StatusServiceUnavailable, ""},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(c.method, c.path, nil))
		if w.Code != c.code || w.Header().Get("Location") != c.location || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s %s: %d %q %q", c.method, c.path, w.Code, w.Header().Get("Location"), w.Header().Get("Cache-Control"))
		}
		if strings.Contains(w.Body.String(), "merchant") {
			t.Fatalf("%s %s reached the payment API: %s", c.method, c.path, w.Body.String())
		}
	}
	if (&Server{}).agreementPublic(httptest.NewRecorder(), httptest.NewRequest("GET", "/agreement.htm", nil)) {
		t.Fatal("other paths must pass through")
	}
}

// The agreement uses the selected home template, the site name and the
// contact details from the website settings, with the home page's CSP.
func TestAgreementPageUsesSelectedTemplateAndSiteSettings(t *testing.T) {
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	nonce := make([]byte, 8)
	rand.Read(nonce)
	schema := "agreement_" + hex.EncodeToString(nonce)
	if _, e = pool.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	config, _ := pgxpool.ParseConfig(dsn)
	config.ConnConfig.RuntimeParams["search_path"] = schema
	scoped, e := pgxpool.NewWithConfig(ctx, config)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { scoped.Close(); pool.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE") }()
	if e = db.Migrate(ctx, scoped); e != nil {
		t.Fatal(e)
	}
	h := (&Server{Pool: scoped, CheckoutKey: bytes.Repeat([]byte{7}, 32)}).Handler()
	get := func() *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/agreement.html", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("/agreement.html: %d %s", w.Code, w.Body.String())
		}
		return w
	}
	bare := get()
	if !strings.Contains(bare.Body.String(), `data-home-style="default"`) || !strings.Contains(bare.Body.String(), "<title>GOEPAY服务协议</title>") || strings.Contains(bare.Body.String(), `id="contact"`) {
		t.Fatal("fresh install must show the default template and no contact section")
	}
	if csp := bare.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") || strings.Contains(csp, "script-src") {
		t.Fatalf("agreement CSP: %q", csp)
	}
	for key, value := range map[string]string{"sitename": "协议测试站", "orgname": "示例科技有限公司", "email": "support@example.com", "kfqq": "10001", "qqqun": "https://qm.qq.com/q/abc", "homepage": "2", "homepage_url": "https://www.example.com/"} {
		if _, e = scoped.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key',$1::text,'value',$2::text))`, key, value); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = scoped.Exec(ctx, `UPDATE page_template_heads SET selected_name='index7' WHERE kind='home'`); e != nil {
		t.Fatal(e)
	}
	// homepage=2 sends "/" elsewhere; the agreement is still served.
	page := get().Body.String()
	for _, want := range []string{`data-home-style="index7"`, "<title>协议测试站服务协议</title>", "本协议由协议测试站系统和你共同签订", `id="contact"`, "示例科技有限公司", `href="mailto:support@example.com"`, "<dd>10001</dd>", `href="https://qm.qq.com/q/abc"`} {
		if !strings.Contains(page, want) {
			t.Fatalf("agreement page missing %q", want)
		}
	}
}
