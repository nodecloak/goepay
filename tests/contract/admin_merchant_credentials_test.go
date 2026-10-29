package contract_test

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/compat"
	"goepay/internal/identity"
	"goepay/internal/server"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type adminCredentials162 struct {
	s           *server.Server
	pool        *pgxpool.Pool
	token, csrf string
	old         *rsa.PrivateKey
}

func adminCredentialsFixture162(t *testing.T) adminCredentials162 {
	t.Helper()
	s, p, old := fixture(t)
	s.Sessions = identity.NewPersistentSessions(p, false)
	var version int64
	if _, e := p.Exec(context.Background(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) SELECT 'backup162','admin',162,'Synthetic Backup Admin',password_hash,'super' FROM console_users WHERE username='admin'`); e != nil {
		t.Fatal(e)
	}
	if e := p.QueryRow(context.Background(), "SELECT auth_version FROM console_users WHERE username='admin'").Scan(&version); e != nil {
		t.Fatal(e)
	}
	token, csrf, e := s.Sessions.Create(identity.User{ID: 1, Role: "admin", Username: "admin", AdminClass: "super", AuthVersion: version})
	if e != nil {
		t.Fatal(e)
	}
	return adminCredentials162{s, p, token, csrf, old}
}
func (c adminCredentials162) request(method, path, body, csrf string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/console/v1/admin/merchants/"+path, strings.NewReader(body))
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: c.token})
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", csrf)
	w := httptest.NewRecorder()
	c.s.Handler().ServeHTTP(w, r)
	return w
}
func credentialData162(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil || out.Code != 0 || out.Data == nil {
		t.Fatalf("credential response was not successful JSON, HTTP %d", w.Code)
	}
	return out.Data
}

func TestOriginalAdminMerchantCredentialsMD5ConcurrentCASAndNoSecretReadback(t *testing.T) {
	c := adminCredentialsFixture162(t)
	ctx := context.Background()
	var before string
	if e := c.pool.QueryRow(ctx, "SELECT md5(jsonb_build_array((SELECT jsonb_agg(to_jsonb(m)) FROM merchants m),(SELECT jsonb_agg(to_jsonb(a)) FROM backoffice_audit a))::text)").Scan(&before); e != nil {
		t.Fatal(e)
	}
	w := c.request("GET", "1/credentials", "", c.csrf)
	if w.Code != 200 {
		t.Fatalf("original admin credential panel unavailable: %d", w.Code)
	}
	data := credentialData162(t, w)
	fp, ok := data["fingerprint"].(string)
	if !ok || len(fp) != 64 || strings.Contains(w.Body.String(), "contract-secret") {
		t.Fatal("state missing exact fingerprint or exposed an old secret")
	}
	var after string
	if e := c.pool.QueryRow(ctx, "SELECT md5(jsonb_build_array((SELECT jsonb_agg(to_jsonb(m)) FROM merchants m),(SELECT jsonb_agg(to_jsonb(a)) FROM backoffice_audit a))::text)").Scan(&after); e != nil || after != before {
		t.Fatal("state read changed business rows", e)
	}
	body := fmt.Sprintf(`{"expected_fingerprint":%q}`, fp)
	if got := c.request("POST", "1/credentials/reset-md5", body, "wrong"); got.Code != 403 {
		t.Fatal("missing CSRF was accepted", got.Code)
	}
	results := make(chan *httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- c.request("POST", "1/credentials/reset-md5", body, c.csrf) }()
	}
	wg.Wait()
	close(results)
	wins := 0
	var next string
	for got := range results {
		if got.Code == 200 {
			wins++
			data := credentialData162(t, got)
			next, _ = data["api_key"].(string)
			if data["merchant_id"] != "1" || data["revision"] != "2" {
				t.Fatal("credential ID/revision were not exact decimal strings")
			}
		} else if got.Code != 409 {
			t.Fatal("unexpected concurrent reset status", got.Code)
		}
	}
	if wins != 1 || len(next) != 32 {
		t.Fatal("same fingerprint did not have exactly one new key")
	}
	if _, e := hex.DecodeString(next); e != nil {
		t.Fatal("new MD5 key is not 128-bit hexadecimal")
	}
	merchant, e := c.s.Store.Merchant(ctx, 1)
	if e != nil || merchant.APIKey != next || merchant.SignatureAlgorithm != "MD5" || merchant.ConfigRevision != 2 {
		t.Fatal("reset changed another credential field or did not persist once", e)
	}
	for _, tc := range []struct {
		key  string
		want float64
	}{{"contract-secret", -3}, {next, 1}} {
		_, out := call(t, c.s.Handler(), "GET", "/api.php", url.Values{"act": {"query"}, "pid": {"1"}, "key": {tc.key}})
		if out["code"] != tc.want {
			t.Fatal("legacy API did not replace only the original MD5 credential")
		}
	}
	var cipher []byte
	var audit string
	var count int
	if e = c.pool.QueryRow(ctx, "SELECT api_key,(SELECT count(*) FROM backoffice_audit WHERE resource='merchants' AND operation='reset-md5'),(SELECT COALESCE(jsonb_agg(changes)::text,'[]') FROM backoffice_audit) FROM merchants WHERE id=1").Scan(&cipher, &count, &audit); e != nil || bytes.Contains(cipher, []byte(next)) || strings.Contains(audit, next) || count != 1 {
		t.Fatal("secret persisted unencrypted or leaked to audit", count, e)
	}
	if got := c.request("POST", "1/credentials/reset-md5", body, c.csrf); got.Code != 409 {
		t.Fatal("lost-response replay silently rotated a second key", got.Code)
	}
	got := c.request("GET", "1/credentials", "", c.csrf)
	if got.Code != 200 || strings.Contains(got.Body.String(), next) {
		t.Fatal("new secret became recoverable from state read", got.Code)
	}
}

func TestOriginalAdminMerchantCredentialsRSAPairActuallySignsAndPreservesMD5(t *testing.T) {
	c := adminCredentialsFixture162(t)
	ctx := context.Background()
	w := c.request("GET", "2/credentials", "", c.csrf)
	if w.Code != 200 {
		t.Fatal("RSA credentials panel unavailable", w.Code)
	}
	fp, ok := credentialData162(t, w)["fingerprint"].(string)
	if !ok || len(fp) != 64 {
		t.Fatal("RSA panel has no current credential fingerprint")
	}
	body := fmt.Sprintf(`{"expected_fingerprint":%q}`, fp)
	for _, bad := range []string{`null`, `{}`, strings.TrimSuffix(body, "}") + `,"bits":1024}`, strings.TrimSuffix(body, "}") + `,"expected_fingerprint":"duplicate"}`} {
		if got := c.request("POST", "2/credentials/generate-rsa", bad, c.csrf); got.Code != 400 || strings.Contains(got.Body.String(), "PRIVATE KEY") {
			t.Fatal("malformed generation request accepted", got.Code)
		}
	}
	w = c.request("POST", "2/credentials/generate-rsa", body, c.csrf)
	if w.Code != 200 {
		t.Fatal("RSA generation unavailable", w.Code)
	}
	data := credentialData162(t, w)
	public, _ := data["public_key"].(string)
	private, _ := data["private_key"].(string)
	key, e := compat.ParsePrivateKey(private)
	if e != nil || key.N.BitLen() != 2048 {
		t.Fatal("generated RSA private key not usable", e)
	}
	pub, e := compat.ParsePublicKey(public)
	if e != nil {
		t.Fatal(e)
	}
	params := compat.Params{"pid": "2", "sign_type": "RSA", "timestamp": fmt.Sprint(time.Now().Unix())}
	signature, e := compat.SignRSAWithProfile(params, key, compat.ProfileSDKV2)
	if e != nil || !compat.VerifyRSAWithProfile(params, signature, pub, compat.ProfileSDKV2) {
		t.Fatal("generated keypair did not agree", e)
	}
	for _, tc := range []struct {
		key  *rsa.PrivateKey
		want float64
	}{{c.old, -1}, {key, 0}} {
		params["sign"], e = compat.SignRSAWithProfile(params, tc.key, compat.ProfileSDKV2)
		if e != nil {
			t.Fatal(e)
		}
		values := url.Values{}
		for k, v := range params {
			values.Set(k, v)
		}
		_, out := call(t, c.s.Handler(), "POST", "/api/merchant/info", values)
		if out["code"] != tc.want {
			t.Fatal("actual signed payment API did not replace only the original public key")
		}
	}
	merchant, e := c.s.Store.Merchant(ctx, 2)
	if e != nil || merchant.APIKey != "contract-rsa-secret" || merchant.RSAPublicKey != public || merchant.SignatureAlgorithm != "RSA" {
		t.Fatal("RSA generation changed MD5 or algorithm", e)
	}
	var stored string
	if e = c.pool.QueryRow(ctx, "SELECT jsonb_build_array((SELECT jsonb_agg(to_jsonb(m)) FROM merchants m),(SELECT jsonb_agg(to_jsonb(a)) FROM backoffice_audit a))::text").Scan(&stored); e != nil || strings.Contains(stored, private) || strings.Contains(stored, "PRIVATE KEY") {
		t.Fatal("private key entered persistence", e)
	}
	if got := c.request("POST", "2/credentials/generate-rsa", body, c.csrf); got.Code != 409 || strings.Contains(got.Body.String(), "PRIVATE KEY") {
		t.Fatal("old RSA generation fingerprint was reused", got.Code)
	}
	if got := c.request("GET", "2/credentials", "", c.csrf); got.Code != 200 || strings.Contains(got.Body.String(), "PRIVATE KEY") {
		t.Fatal("private key returned by subsequent read", got.Code)
	}
}

func TestOriginalAdminMerchantCredentialsRejectOtherRolesAndAmbiguousTargets(t *testing.T) {
	c := adminCredentialsFixture162(t)
	ctx := context.Background()
	fp := credentialData162(t, c.request("GET", "1/credentials", "", c.csrf))["fingerprint"].(string)
	body := fmt.Sprintf(`{"expected_fingerprint":%q}`, fp)
	for _, tc := range []struct {
		method, path, body string
		want               int
	}{
		{"GET", "01/credentials", "", 400}, {"GET", "1/credentials?merchant_id=2", "", 400}, {"GET", "1/credentials?x=", "", 400},
		{"POST", "1/credentials/reset-md5", strings.TrimSuffix(body, "}") + `,"merchant_id":"2"}`, 400},
		{"POST", "1/credentials/reset-md5", strings.TrimSuffix(body, "}") + `,"expected_fingerprint":"duplicate"}`, 400},
	} {
		if got := c.request(tc.method, tc.path, tc.body, c.csrf); got.Code != tc.want {
			t.Fatal("ambiguous credential request was not rejected", tc.path, got.Code)
		}
	}
	for _, class := range []string{"operator"} {
		if _, e := c.pool.Exec(ctx, "UPDATE console_users SET admin_class=$1 WHERE username='admin'", class); e != nil {
			t.Fatal(e)
		}
		var version int64
		if e := c.pool.QueryRow(ctx, "SELECT auth_version FROM console_users WHERE username='admin'").Scan(&version); e != nil {
			t.Fatal(e)
		}
		token, csrf, e := c.s.Sessions.Create(identity.User{ID: 1, Role: "admin", Username: "admin", AdminClass: "operator", AuthVersion: version})
		if e != nil {
			t.Fatal(e)
		}
		c.token, c.csrf = token, csrf
		if got := c.request("GET", "1/credentials", "", c.csrf); got.Code != 403 {
			t.Fatal("non-super read credential state", class, got.Code)
		}
		if got := c.request("POST", "1/credentials/reset-md5", body, c.csrf); got.Code != 403 {
			t.Fatal("non-super rotated credential", class, got.Code)
		}
	}
	var v int64
	if e := c.pool.QueryRow(ctx, "SELECT auth_version FROM console_users WHERE username='merchant'").Scan(&v); e != nil {
		t.Fatal(e)
	}
	token, csrf, e := c.s.Sessions.Create(identity.User{ID: 1, Role: "merchant", Username: "merchant", AuthVersion: v})
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("POST", "/console/v1/merchant/merchants/1/credentials/reset-md5", strings.NewReader(body))
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
	r.Header.Set("X-CSRF-Token", csrf)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c.s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("merchant used administrator credential action", w.Code)
	}
	m, e := c.s.Store.Merchant(ctx, 1)
	if e != nil || m.APIKey != "contract-secret" || m.ConfigRevision != 1 {
		t.Fatal("denied requests changed original key", e)
	}
}

func TestOriginalAdminMerchantCredentialsAuditFailureRollsBackAndNeverReturnsSecret(t *testing.T) {
	for _, kind := range []string{"reset-md5", "generate-rsa"} {
		t.Run(kind, func(t *testing.T) {
			c := adminCredentialsFixture162(t)
			ctx := context.Background()
			fp := credentialData162(t, c.request("GET", "1/credentials", "", c.csrf))["fingerprint"].(string)
			before, e := c.s.Store.Merchant(ctx, 1)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = c.pool.Exec(ctx, `CREATE FUNCTION credential_audit_failure162() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.operation IN('reset-md5','generate-rsa') THEN RAISE EXCEPTION 'synthetic credential audit failure';END IF;RETURN NEW;END$$;CREATE TRIGGER credential_audit_failure162 BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION credential_audit_failure162()`); e != nil {
				t.Fatal(e)
			}
			got := c.request("POST", "1/credentials/"+kind, fmt.Sprintf(`{"expected_fingerprint":%q}`, fp), c.csrf)
			if got.Code != 503 || strings.Contains(got.Body.String(), "PRIVATE KEY") || strings.Contains(got.Body.String(), "api_key") {
				t.Fatal("uncommitted credential was returned", got.Code)
			}
			after, e := c.s.Store.Merchant(ctx, 1)
			if e != nil || after.APIKey != before.APIKey || after.RSAPublicKey != before.RSAPublicKey || after.ConfigRevision != before.ConfigRevision {
				t.Fatal("audit failure did not roll back credential", e)
			}
		})
	}
}

func TestOriginalAdminMerchantCredentialsActorChangeDuringLockWaitRejectsOriginalSession(t *testing.T) {
	for _, change := range []string{"demote", "revoke"} {
		t.Run(change, func(t *testing.T) {
			c := adminCredentialsFixture162(t)
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			fp := credentialData162(t, c.request("GET", "1/credentials", "", c.csrf))["fingerprint"].(string)
			block, e := c.pool.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer block.Rollback(context.Background())
			if _, e = block.Exec(ctx, "SELECT id FROM merchants WHERE id=1 FOR UPDATE"); e != nil {
				t.Fatal(e)
			}
			pid := int64(block.Conn().PgConn().PID())
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				done <- c.request("POST", "1/credentials/reset-md5", fmt.Sprintf(`{"expected_fingerprint":%q}`, fp), c.csrf)
			}()
			for {
				var waiting bool
				if e = c.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1::integer=ANY(pg_blocking_pids(pid)))", pid).Scan(&waiting); e != nil {
					t.Fatal(e)
				}
				if waiting {
					break
				}
				select {
				case early := <-done:
					t.Fatal("credential action never waited on original merchant", early.Code)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-time.After(10 * time.Millisecond):
				}
			}
			if change == "demote" {
				_, e = c.pool.Exec(ctx, "UPDATE console_users SET admin_class='operator' WHERE username='admin'")
			} else {
				_, e = c.pool.Exec(ctx, "DELETE FROM console_sessions WHERE username='admin'")
			}
			if e != nil {
				t.Fatal(e)
			}
			if e = block.Rollback(ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case got := <-done:
				if got.Code != 403 || strings.Contains(got.Body.String(), "api_key") {
					t.Fatal("revoked or demoted original actor changed credentials", got.Code)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			m, e := c.s.Store.Merchant(ctx, 1)
			if e != nil || m.APIKey != "contract-secret" || m.ConfigRevision != 1 {
				t.Fatal("invalid actor changed key after wait", e)
			}
		})
	}
}

func TestOriginalAdminMerchantCredentialsExpiryDuringAuditCannotCommit(t *testing.T) {
	c := adminCredentialsFixture162(t)
	ctx := context.Background()
	fp := credentialData162(t, c.request("GET", "1/credentials", "", c.csrf))["fingerprint"].(string)
	if _, e := c.pool.Exec(ctx, `CREATE SEQUENCE credential_audit_entered162;CREATE FUNCTION credential_audit_wait162() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.operation='reset-md5' THEN PERFORM nextval('credential_audit_entered162');PERFORM pg_sleep(1.3);END IF;RETURN NEW;END$$;CREATE TRIGGER credential_audit_wait162 BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION credential_audit_wait162();UPDATE console_sessions SET expires_at=clock_timestamp()+interval '1 second' WHERE username='admin'`); e != nil {
		t.Fatal(e)
	}
	got := c.request("POST", "1/credentials/reset-md5", fmt.Sprintf(`{"expected_fingerprint":%q}`, fp), c.csrf)
	if got.Code != 403 {
		t.Fatal("expired actor committed credential after audit wait", got.Code)
	}
	var entered bool
	if e := c.pool.QueryRow(ctx, "SELECT is_called FROM credential_audit_entered162").Scan(&entered); e != nil || !entered {
		t.Fatal("fixture did not reach actual final audit wait", e)
	}
	m, e := c.s.Store.Merchant(ctx, 1)
	if e != nil || m.APIKey != "contract-secret" || m.ConfigRevision != 1 {
		t.Fatal("expired audit wait changed original credential", e)
	}
	var count int
	if e = c.pool.QueryRow(ctx, "SELECT count(*) FROM backoffice_audit WHERE operation='reset-md5'").Scan(&count); e != nil || count != 0 {
		t.Fatal("expired audit wait retained success audit", count, e)
	}
}
