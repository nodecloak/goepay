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

// The /doc/ pages read the site name, the configured merchant API origin and
// the open 支付方式 the same way payment routing decides them.
func TestDevDocPagesUseSiteSettingsAndOpenPayTypes(t *testing.T) {
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
	schema := "devdoc_" + hex.EncodeToString(nonce)
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
	insert := func(resource, body string) {
		t.Helper()
		if _, e := scoped.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES($1,$2::jsonb)`, resource, body); e != nil {
			t.Fatal(e)
		}
	}
	insert("settings", `{"key":"sitename","value":"文档测试站"}`)
	s := &Server{Pool: scoped, CheckoutKey: bytes.Repeat([]byte{7}, 32), DocsAPIOrigin: "https://pay.example.com"}
	h := s.Handler()
	get := func(path string) string {
		t.Helper()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	index := get("/doc/index.html")
	if !strings.Contains(index, "<title>接口说明 - 开发文档 - 文档测试站</title>") || !strings.Contains(index, `<code class="doc-url">https://pay.example.com/</code>`) {
		t.Fatal("index page does not show the site name and API origin")
	}
	if !strings.Contains(get("/doc/pay_submit.html"), "https://pay.example.com/api/pay/submit") {
		t.Fatal("request address does not use the API origin")
	}
	rows := func() string {
		t.Helper()
		page := get("/doc/paytype.html")
		_, table, _ := strings.Cut(page, "<tbody>")
		table, _, _ = strings.Cut(table, "</tbody>")
		return strings.Join(strings.Fields(table), "")
	}
	// A fresh install: only the original defaults (alipay, wxpay) are open.
	if got := rows(); got != "<tr><td><code>alipay</code></td><td>支付宝</td></tr><tr><td><code>wxpay</code></td><td>微信支付</td></tr>" {
		t.Fatalf("default pay types: %s", got)
	}
	insert("pay-types", `{"name":"QQ钱包","type":"qqpay","enabled":true}`)
	insert("pay-types", `{"name":"支付宝","type":"alipay","enabled":false}`)
	insert("pay-types", `{"name":"测试","type":"sandbox","enabled":true}`)
	insert("pay-types", `{"name":"数字人民币","type":"ecny","enabled":true}`)
	insert("pay-types", `{"name":"微信","type":"wxpay","enabled":true}`)
	if got := rows(); got != "<tr><td><code>wxpay</code></td><td>微信</td></tr><tr><td><code>qqpay</code></td><td>QQ钱包</td></tr><tr><td><code>ecny</code></td><td>数字人民币</td></tr>" {
		t.Fatalf("pay types after records: %s", got)
	}
}
