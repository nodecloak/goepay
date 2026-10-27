package contract_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/db"
	"goepay/internal/compat"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"goepay/internal/server"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) (*server.Server, *pgxpool.Pool, *rsa.PrivateKey) {
	t.Helper()
	return fixtureWithMigration(t, db.Migrate)
}

func fixtureWithMigration(t *testing.T, migrate func(context.Context, *pgxpool.Pool) error) (*server.Server, *pgxpool.Pool, *rsa.PrivateKey) {
	t.Helper()
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	var scoped *pgxpool.Pool
	var schema string
	created := false
	t.Cleanup(func() {
		if scoped != nil {
			scoped.Close()
		}
		if created {
			if _, err := pool.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
				t.Error(err)
			}
		}
		pool.Close()
	})
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	schema = "contract_" + hex.EncodeToString(b)
	if _, err := pool.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	created = true
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	scoped, err = pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrate(ctx, scoped); err != nil {
		t.Fatal(err)
	}
	store, err := payment.NewWithKey(scoped, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []payment.Merchant{{ID: 1, Name: "legacy", APIKey: "contract-secret", SignatureAlgorithm: "MD5"}, {ID: 2, Name: "rsa", APIKey: "contract-rsa-secret", SignatureAlgorithm: "RSA", RSAPublicKey: base64.StdEncoding.EncodeToString(der)}} {
		if err := store.CreateMerchant(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	hash, err := identity.HashPassword("contract-password-123")
	if err != nil {
		t.Fatal(err)
	}
	_, err = scoped.Exec(ctx, "INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) VALUES('admin','admin',1,'Admin',$1,'super'),('merchant','merchant',1,'Merchant',$1,'operator')", hash)
	if err != nil {
		t.Fatal(err)
	}
	app := &server.Server{Pool: scoped, Store: store, Sessions: identity.NewSessions(false), PlatformKey: key, Sandbox: true}
	// Match the API bootstrap's independent login protection key without
	// configuring checkout credentials or enabling a payment provider.
	app.LoginCaptcha205, err = server.NewLoginCaptchaService205ForMaster(app, nil, make([]byte, 32), "")
	if err != nil {
		t.Fatal(err)
	}
	return app, scoped, key
}
func call(t *testing.T, h http.Handler, method, path string, p url.Values) (int, map[string]any) {
	t.Helper()
	var body *strings.Reader
	if method == "GET" {
		path += "?" + p.Encode()
		body = strings.NewReader("")
	} else {
		body = strings.NewReader(p.Encode())
	}
	r := httptest.NewRequest(method, path, body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s status=%d body=%s", path, w.Code, w.Body.String())
	}
	return w.Code, out
}
func signed(p compat.Params, key string) url.Values {
	p["sign_type"] = "MD5"
	p["sign"] = compat.SignMD5(p, key)
	v := url.Values{}
	for k, x := range p {
		v.Set(k, x)
	}
	return v
}
func TestLegacyPaymentContractAndOwnership(t *testing.T) {
	s, pool, _ := fixture(t)
	h := s.Handler()
	base := compat.Params{"pid": "1", "out_trade_no": "same-order", "money": "1.00", "name": "中文😀", "type": "sandbox", "extension": "signed extension"}
	_, first := call(t, h, "POST", "/mapi.php", signed(base, "contract-secret"))
	if first["code"] != float64(1) || first["payurl"] == nil || first["pay_info"] != nil || first["pay_type"] != nil {
		t.Fatalf("legacy material %v", first)
	}
	_, again := call(t, h, "POST", "/mapi.php", signed(base, "contract-secret"))
	if first["trade_no"] != again["trade_no"] {
		t.Fatal("duplicate created another order")
	}
	base["money"] = "2.00"
	_, conflict := call(t, h, "POST", "/mapi.php", signed(base, "contract-secret"))
	if conflict["code"] != float64(-1) {
		t.Fatal("different amount idempotency accepted")
	}
	_, query := call(t, h, "GET", "/api.php", url.Values{"act": {"order"}, "pid": {"1"}, "key": {"contract-secret"}, "trade_no": {first["trade_no"].(string)}})
	if query["code"] != float64(1) || query["money"] != "1.00" {
		t.Fatalf("legacy query %v", query)
	}
	_, wrong := call(t, h, "GET", "/api.php", url.Values{"act": {"order"}, "pid": {"2"}, "key": {"contract-rsa-secret"}, "trade_no": {first["trade_no"].(string)}})
	if wrong["code"] != float64(-1) {
		t.Fatal("RSA merchant allowed legacy key auth")
	}
	var count int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM orders").Scan(&count); err != nil || count != 1 {
		t.Fatalf("order count=%d err=%v", count, err)
	}
}
func TestRSAContractRejectsDowngradeAndSignsSuccess(t *testing.T) {
	s, _, key := fixture(t)
	h := s.Handler()
	p := compat.Params{"pid": "2", "out_trade_no": "rsa-order", "money": "1.00", "name": "test", "type": "sandbox", "sign_type": "RSA", "timestamp": fmt.Sprint(time.Now().Unix())}
	signature, err := compat.SignRSA(p, key)
	if err != nil {
		t.Fatal(err)
	}
	p["sign"] = signature
	v := url.Values{}
	for k, x := range p {
		v.Set(k, x)
	}
	_, result := call(t, h, "POST", "/api/pay/create", v)
	if result["code"] != float64(0) || result["pay_info"] == nil || result["payurl"] != nil {
		t.Fatalf("V2 material %v", result)
	}
	response := compat.Params{}
	for k, x := range result {
		if b, ok := x.(bool); ok {
			if b {
				response[k] = "1"
			} else {
				response[k] = ""
			}
		} else {
			response[k] = fmt.Sprint(x)
		}
	}
	if err := compat.Verify(response, compat.Credentials{Algorithm: "RSA", PublicKey: &key.PublicKey}, time.Now()); err != nil {
		t.Fatalf("response not verifiable: %v", err)
	}
	_, downgrade := call(t, h, "POST", "/mapi.php", signed(p, "contract-rsa-secret"))
	if downgrade["code"] != float64(-1) {
		t.Fatal("RSA-only merchant downgraded")
	}
}
func TestConsoleRoleCSRFAndLogin(t *testing.T) {
	s, _, _ := fixture(t)
	h := s.Handler()
	login := httptest.NewRequest("POST", "/console/v1/admin/login", bytes.NewBufferString(`{"username":"admin","password":"contract-password-123"}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, login)
	if w.Code != 200 {
		t.Fatalf("login=%d %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("cookie security")
	}
	var result struct {
		Data struct {
			CSRF string `json:"csrf_token"`
		} `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &result)
	req := httptest.NewRequest("POST", "/console/v1/admin/logout", nil)
	req.AddCookie(cookies[0])
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatal("mutation without CSRF allowed")
	}
	cross := httptest.NewRequest("GET", "/console/v1/merchant/session", nil)
	cross.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: cookies[0].Value})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, cross)
	if rec.Code != 401 {
		t.Fatal("cross role cookie accepted")
	}
	req = httptest.NewRequest("POST", "/console/v1/admin/logout", nil)
	req.AddCookie(cookies[0])
	req.Header.Set("X-CSRF-Token", result.Data.CSRF)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatal("valid logout failed")
	}
}
func TestConsolePaginationFilterAndTenantIsolation(t *testing.T) {
	s, _, _ := fixture(t)
	ctx := context.Background()
	for mid := int64(1); mid <= 2; mid++ {
		for n := 0; n < 25; n++ {
			_, err := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: mid, OutTradeNo: fmt.Sprintf("alpha-%d-%02d", mid, n), Name: "Alpha matching", Type: "sandbox", Amount: 100})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	token, _, err := s.Sessions.Create(identity.User{Username: "merchant", ID: 1, Name: "merchant", Role: "merchant"})
	if err != nil {
		t.Fatal(err)
	}
	request := func(path string) map[string]any {
		t.Helper()
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 {
			t.Fatalf("list status=%d %s", w.Code, w.Body.String())
		}
		return out["data"].(map[string]any)
	}
	data := request("/console/v1/merchant/orders?page=2&limit=10&q=ALPHA")
	if data["total"] != float64(25) || len(data["items"].([]any)) != 10 || data["page"] != float64(2) {
		t.Fatalf("pagination %v", data)
	}
	for _, item := range data["items"].([]any) {
		if item.(map[string]any)["merchant_id"] != "1" {
			t.Fatal("cross merchant order leaked")
		}
	}
	data = request("/console/v1/merchant/orders?page=1&limit=10&q=alpha-2")
	if data["total"] != float64(0) || len(data["items"].([]any)) != 0 {
		t.Fatal("search revealed another merchant")
	}
	data = request("/console/v1/merchant/orders?q=%27%20OR%201=1--")
	if data["total"] != float64(0) {
		t.Fatal("query treated as SQL")
	}
}
func TestReadinessRequiresSchema(t *testing.T) {
	s, pool, _ := fixture(t)
	h := s.Handler()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
	if w.Code != 200 {
		t.Fatal("ready schema failed")
	}
	if _, err := pool.Exec(context.Background(), "DROP TABLE console_users CASCADE"); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
	if w.Code != 503 {
		t.Fatal("readiness passed despite missing schema")
	}
}
func TestOtherConsoleListsRespectScopeAndTotals(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	for mid := int64(1); mid <= 2; mid++ {
		for n := 0; n < 12; n++ {
			o, err := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: mid, OutTradeNo: fmt.Sprintf("scope-%d-%02d", mid, n), Name: "Scoped", Type: "sandbox", Amount: 100})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Store.MarkPaid(ctx, o.TradeNo, "evt:"+o.TradeNo); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Store.ReserveRefund(ctx, mid, o.OutTradeNo, fmt.Sprintf("search-refund-%d-%02d", mid, n), 1); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, "INSERT INTO transfers(merchant_id,transfer_no,amount) VALUES($1,$2,1)", mid, fmt.Sprintf("search-transfer-%d-%02d", mid, n)); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, kind := range []string{"refunds", "transfers", "ledger", "merchants"} {
		role := "merchant"
		id := int64(1)
		want := float64(12)
		if kind == "ledger" {
			// Each paid order contributes one merchant credit entry; every
			// authenticated refund adds an available debit and reserved credit.
			want = 36
		}
		if kind == "merchants" {
			role = "admin"
			want = 2
		}
		token, _, err := s.Sessions.Create(identity.User{Username: role, ID: id, Name: "test", Role: role})
		if err != nil {
			t.Fatal(err)
		}
		path := "/console/v1/" + role + "/" + kind + "?page=2&limit=5"
		if kind == "merchants" {
			path = "/console/v1/admin/merchants?page=1&limit=1&q=LEGACY"
			want = 1
		}
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		var out struct {
			Data struct {
				Items []map[string]any `json:"items"`
				Total float64          `json:"total"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || out.Data.Total != want {
			t.Fatalf("%s total=%v status=%d body=%s", kind, out.Data.Total, w.Code, w.Body.String())
		}
		wantLen := 5
		if kind == "merchants" {
			wantLen = 1
		}
		if len(out.Data.Items) != wantLen {
			t.Fatalf("%s page size=%d", kind, len(out.Data.Items))
		}
		for _, item := range out.Data.Items {
			if mid, ok := item["merchant_id"]; ok && mid != "1" {
				t.Fatal("scope leak")
			}
		}
	}
}
func TestDisabledAccountsRevokeExistingSessions(t *testing.T) {
	for _, target := range []string{"console", "merchant"} {
		t.Run(target, func(t *testing.T) {
			s, pool, _ := fixture(t)
			s.Sessions = identity.NewPersistentSessions(pool, false)
			r := httptest.NewRequest("POST", "/console/v1/merchant/login", bytes.NewBufferString(`{"username":"merchant","password":"contract-password-123"}`))
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			if w.Code != 200 {
				t.Fatalf("login failed: status=%d body=%s", w.Code, w.Body.String())
			}
			cookie := w.Result().Cookies()[0]
			q := "UPDATE console_users SET enabled=false WHERE username='merchant'"
			if target == "merchant" {
				q = "UPDATE merchants SET enabled=false WHERE id=1"
			}
			if _, err := pool.Exec(context.Background(), q); err != nil {
				t.Fatal(err)
			}
			r = httptest.NewRequest("GET", "/console/v1/merchant/session", nil)
			r.AddCookie(cookie)
			w = httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			if w.Code != 401 {
				t.Fatalf("disabled account session status=%d", w.Code)
			}
			if _, ok := s.Sessions.Get(cookie.Value, "merchant"); ok {
				t.Fatal("disabled session not invalidated")
			}
		})
	}
}
