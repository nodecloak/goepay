package contract_test

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"goepay/internal/compat"
	"goepay/internal/identity"
)

func TestMerchantRSARotationHTTP(t *testing.T) {
	s, pool, old := fixture(t)
	ctx := context.Background()
	if _, e := pool.Exec(ctx, `UPDATE console_users SET subject_id=2 WHERE username='merchant'`); e != nil {
		t.Fatal(e)
	}
	s.Sessions = identity.NewPersistentSessions(pool, false)
	var version int64
	if e := pool.QueryRow(ctx, `SELECT auth_version FROM console_users WHERE username='merchant'`).Scan(&version); e != nil {
		t.Fatal(e)
	}
	token, csrf, e := s.Sessions.Create(identity.User{Username: "merchant", ID: 2, Role: "merchant", AuthVersion: version})
	if e != nil {
		t.Fatal(e)
	}
	h := s.Handler()
	request := func(method, path, body, csrfValue string) (int, map[string]any, string) {
		r := httptest.NewRequest(method, "/console/v1/merchant/account/"+path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		r.Header.Set("X-CSRF-Token", csrfValue)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		var v map[string]any
		if json.Unmarshal(w.Body.Bytes(), &v) != nil {
			t.Fatal("invalid JSON", w.Code)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Error("secret response cache policy")
		}
		return w.Code, v, w.Body.String()
	}
	code, out, _ := request("GET", "api-info", "", csrf)
	if code != 200 {
		t.Fatal(code)
	}
	data := out["data"].(map[string]any)
	if data["can_rotate_rsa"] != true {
		t.Fatal("own original merchant cannot generate RSA", data["can_rotate_rsa"])
	}
	fp, ok := data["rsa_fingerprint"].(string)
	if !ok || len(fp) != 64 {
		t.Fatal("missing CAS fingerprint")
	}
	body := fmt.Sprintf(`{"current_password":"contract-password-123","expected_fingerprint":%q}`, fp)
	for _, tc := range []struct {
		body, csrf string
		want       int
	}{{body, "wrong", 403}, {strings.Replace(body, "contract-password-123", "wrong", 1), csrf, 401}, {strings.TrimSuffix(body, "}") + `,"bits":"1024"}`, csrf, 400}, {strings.TrimSuffix(body, "}") + `,"expected_fingerprint":"x"}`, csrf, 400}} {
		c, _, raw := request("POST", "rsa-key/rotate", tc.body, tc.csrf)
		if c != tc.want || strings.Contains(raw, "PRIVATE KEY") {
			t.Fatalf("invalid request got %d want %d", c, tc.want)
		}
	}
	// Concurrent HTTP requests from one original key have exactly one winner.
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	var result map[string]any
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, v, raw := request("POST", "rsa-key/rotate", body, csrf)
			mu.Lock()
			defer mu.Unlock()
			if c == 200 {
				wins++
				result = v["data"].(map[string]any)
			} else if c != 409 || strings.Contains(raw, "PRIVATE KEY") {
				t.Errorf("loser=%d", c)
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("CAS wins=%d", wins)
	}
	pub := result["public_key"].(string)
	priv := result["private_key"].(string)
	block, rest := pem.Decode([]byte(priv))
	if block == nil || block.Type != "PRIVATE KEY" || len(rest) != 0 {
		t.Fatal("private must be PKCS8 PEM")
	}
	parsed, e := x509.ParsePKCS8PrivateKey(block.Bytes)
	if e != nil {
		t.Fatal(e)
	}
	key := parsed.(*rsa.PrivateKey)
	if key.N.BitLen() < 2048 {
		t.Fatal("weak key")
	}
	block, _ = pem.Decode([]byte(pub))
	if block == nil || block.Type != "PUBLIC KEY" {
		t.Fatal("public must be PKIX PEM")
	}
	if _, e = x509.ParsePKIXPublicKey(block.Bytes); e != nil {
		t.Fatal(e)
	}
	// Real API verification, using SDK-v2 canonicalization and actual signatures.
	params := compat.Params{"pid": "2", "sign_type": "RSA", "timestamp": fmt.Sprint(time.Now().Unix())}
	for _, tc := range []struct {
		key  *rsa.PrivateKey
		want float64
	}{{old, -1}, {key, 0}} {
		params["sign"], e = compat.SignRSAWithProfile(params, tc.key, compat.ProfileSDKV2)
		if e != nil {
			t.Fatal(e)
		}
		values := url.Values{}
		for k, v := range params {
			values.Set(k, v)
		}
		_, v := call(t, h, "POST", "/api/merchant/info", values)
		if v["code"] != tc.want {
			t.Fatalf("RSA real signature result=%v want%v", v["code"], tc.want)
		}
	}
	m, e := s.Store.Merchant(ctx, 2)
	if e != nil || m.SignatureAlgorithm != "RSA" || m.RSAPublicKey != pub || m.APIKey != "contract-rsa-secret" {
		t.Fatal("rotation overwrote unrelated fields")
	}
	_, _, raw := request("GET", "api-info", "", csrf)
	if strings.Contains(raw, priv) || strings.Contains(raw, "PRIVATE KEY") {
		t.Fatal("private recoverable")
	}
	var persisted string
	if e = pool.QueryRow(ctx, `SELECT row_to_json(m)::text FROM merchants m WHERE id=2`).Scan(&persisted); e != nil {
		t.Fatal(e)
	}
	var audit string
	if e = pool.QueryRow(ctx, `SELECT coalesce(json_agg(a)::text,'') FROM backoffice_audit a`).Scan(&audit); e != nil {
		t.Fatal(e)
	}
	if bytes.Contains([]byte(persisted+audit), []byte(priv)) || strings.Contains(persisted+audit, "PRIVATE KEY") {
		t.Fatal("private persisted")
	}
	// Auditing failure must roll back a fresh rotation and never reveal a key.
	var current map[string]any
	_, current, _ = request("GET", "api-info", "", csrf)
	nextFP := current["data"].(map[string]any)["rsa_fingerprint"].(string)
	if _, e = pool.Exec(ctx, `CREATE FUNCTION reject_rsa_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'audit unavailable'; END $$; CREATE TRIGGER reject_rsa_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION reject_rsa_audit()`); e != nil {
		t.Fatal(e)
	}
	c, _, raw := request("POST", "rsa-key/rotate", fmt.Sprintf(`{"current_password":"contract-password-123","expected_fingerprint":%q}`, nextFP), csrf)
	if c == 200 || strings.Contains(raw, "PRIVATE KEY") {
		t.Fatal("audit failure exposed success")
	}
	m, _ = s.Store.Merchant(ctx, 2)
	if m.RSAPublicKey != pub {
		t.Fatal("audit failure committed public key")
	}
	s.Sessions.Delete(token)
	c, _, raw = request("POST", "rsa-key/rotate", body, csrf)
	if c != 401 || strings.Contains(raw, "PRIVATE KEY") {
		t.Fatal("revoked session rotated")
	}
}
