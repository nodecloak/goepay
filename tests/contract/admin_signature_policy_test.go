package contract_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

func adminResource163(c adminCredentials162, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/console/v1/admin/"+path, strings.NewReader(body))
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: c.token})
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", c.csrf)
	w := httptest.NewRecorder()
	c.s.Handler().ServeHTTP(w, r)
	return w
}
func policyRowDigest163(t *testing.T, c adminCredentials162, mid int64) string {
	t.Helper()
	var digest string
	if e := c.pool.QueryRow(context.Background(), `SELECT md5(jsonb_build_array(to_jsonb(m),(SELECT COALESCE(jsonb_agg(to_jsonb(a)),'[]') FROM backoffice_audit a))::text) FROM merchants m WHERE id=$1`, mid).Scan(&digest); e != nil {
		t.Fatal(e)
	}
	return digest
}

func TestSignature163AdminGenericCannotReportIneffectiveAlgorithmChange(t *testing.T) {
	c := adminCredentialsFixture162(t)
	ctx := context.Background()
	if _, e := c.pool.Exec(ctx, `UPDATE merchants SET keytype=0,rsa_public_key=(SELECT rsa_public_key FROM merchants WHERE id=2) WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	before := policyRowDigest163(t, c, 1)
	w := adminResource163(c, "PATCH", "resources/merchants/1", `{"signature_algorithm":"RSA"}`)
	if w.Code != 422 {
		t.Fatalf("generic algorithm change under explicit compatibility must fail, got %d", w.Code)
	}
	if policyRowDigest163(t, c, 1) != before {
		t.Fatal("rejected ineffective algorithm change mutated row/audit")
	}
	got := credentialData162(t, adminResource163(c, "GET", "resources/merchants/1", ""))
	if got["keytype"] != float64(0) {
		t.Fatal("admin projection hides actual compatibility policy", got["keytype"])
	}
	schema := credentialData162(t, adminResource163(c, "GET", "resources/merchants/schema", ""))
	var found bool
	for _, raw := range schema["fields"].([]any) {
		f := raw.(map[string]any)
		if f["name"] == "keytype" {
			found = f["read_only"] == true
		}
	}
	if !found {
		t.Fatal("schema has no readonly keytype")
	}
	// Keeping the historical value while editing ordinary contacts is permitted.
	w = adminResource163(c, "PATCH", "resources/merchants/1", `{"name":"policy-aware ordinary save","signature_algorithm":"MD5"}`)
	if w.Code != 200 {
		t.Fatal("ordinary profile save refused", w.Code)
	}
	var policyInProfile bool
	if e := c.pool.QueryRow(ctx, "SELECT profile ? 'keytype' FROM merchants WHERE id=1").Scan(&policyInProfile); e != nil || policyInProfile {
		t.Fatal("derived policy persisted in profile", e)
	}
}

func TestSignature163AdminGenericCannotRemoveOnlyRSAKey(t *testing.T) {
	for _, public := range []string{"", "invalid-RSA-public-key"} {
		t.Run(fmt.Sprint(len(public)), func(t *testing.T) {
			c := adminCredentialsFixture162(t)
			ctx := context.Background()
			if _, e := c.pool.Exec(ctx, `UPDATE merchants SET keytype=1,rsa_public_key=(SELECT rsa_public_key FROM merchants WHERE id=2) WHERE id=1`); e != nil {
				t.Fatal(e)
			}
			before := policyRowDigest163(t, c, 1)
			body, _ := json.Marshal(map[string]any{"rsa_public_key": public})
			w := adminResource163(c, "PATCH", "resources/merchants/1", string(body))
			if w.Code != 422 {
				t.Fatalf("RSA-only lost its sole valid key through generic PATCH, got %d", w.Code)
			}
			if policyRowDigest163(t, c, 1) != before {
				t.Fatal("invalid RSA generic write changed row/audit")
			}
		})
	}
}

func TestSignature163StoreEnforcesStoredPolicyWhenCallerOmitsIt(t *testing.T) {
	c := adminCredentialsFixture162(t)
	ctx := context.Background()
	if _, e := c.pool.Exec(ctx, `UPDATE merchants SET keytype=1,rsa_public_key=(SELECT rsa_public_key FROM merchants WHERE id=2) WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	m, e := c.s.Store.Merchant(ctx, 1)
	if e != nil {
		t.Fatal(e)
	}
	m.Keytype = nil
	m.RSAPublicKey = ""
	before := policyRowDigest163(t, c, 1)
	called := false
	e = c.s.Store.ReplaceMerchantAtomic(ctx, m, []byte(`{}`), func(context.Context, pgx.Tx) error { called = true; return nil })
	if !errors.Is(e, payment.ErrInvalid) || called {
		t.Fatal("Store trusted caller Keytype/allowed invalid key", e, called)
	}
	if policyRowDigest163(t, c, 1) != before {
		t.Fatal("direct rejected Store write mutated row/audit")
	}
}

func TestSignature163StoreStrictRSACannotWriteInvalidPublicKey(t *testing.T) {
	for _, public := range []string{"", "invalid-public-key"} {
		t.Run(fmt.Sprint(len(public)), func(t *testing.T) {
			c := adminCredentialsFixture162(t)
			ctx := context.Background()
			m, e := c.s.Store.Merchant(ctx, 2)
			if e != nil {
				t.Fatal(e)
			}
			m.RSAPublicKey = public
			before := policyRowDigest163(t, c, 2)
			called := false
			e = c.s.Store.ReplaceMerchantAtomic(ctx, m, []byte(`{}`), func(context.Context, pgx.Tx) error { called = true; return nil })
			if !errors.Is(e, payment.ErrInvalid) || called {
				t.Fatal("NULL strict RSA accepted invalid direct Store public key", e, called)
			}
			if policyRowDigest163(t, c, 2) != before {
				t.Fatal("strict RSA rejection changed merchant/audit")
			}
		})
	}
}

func TestSignature163AdminPolicyOriginalSuperAndMerchantShareCAS(t *testing.T) {
	c := adminCredentialsFixture162(t)
	ctx := context.Background()
	// The administrator DTO uses the same state and fingerprint as the owner.
	state := signatureState163(t, c.request("GET", "2/signature-policy", "", c.csrf))
	if _, e := c.pool.Exec(ctx, "UPDATE console_users SET subject_id=2 WHERE username='merchant'"); e != nil {
		t.Fatal(e)
	}
	var v int64
	if e := c.pool.QueryRow(ctx, "SELECT auth_version FROM console_users WHERE username='merchant'").Scan(&v); e != nil {
		t.Fatal(e)
	}
	token, csrf, e := c.s.Sessions.Create(identity.User{ID: 2, Role: "merchant", Username: "merchant", AuthVersion: v})
	if e != nil {
		t.Fatal(e)
	}
	owner := signatureFixture163{c.s, c.pool, c.old, token, csrf}
	own := signatureState163(t, owner.request("GET", "signature-policy", "", csrf))
	if own["fingerprint"] != state["fingerprint"] {
		t.Fatal("admin/merchant policies use different CAS facts")
	}
	body := signaturePatch163(state["fingerprint"].(string), 0)
	done := make(chan *httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); done <- c.request("PATCH", "2/signature-policy", body, c.csrf) }()
	go func() { defer wg.Done(); done <- owner.request("PATCH", "signature-policy", body, csrf) }()
	wg.Wait()
	close(done)
	wins := 0
	for w := range done {
		if w.Code == 200 {
			wins++
			signatureState163(t, w)
		} else if w.Code != 409 {
			t.Fatal("shared CAS loser status", w.Code)
		}
	}
	if wins != 1 {
		t.Fatal("shared CAS winner count", wins)
	}
	if w := c.request("PATCH", "2/signature-policy", body, c.csrf); w.Code != 409 {
		t.Fatal("admin old fingerprint replay", w.Code)
	}
	var revision int64
	var count int
	if e = c.pool.QueryRow(ctx, `SELECT config_revision,(SELECT count(*) FROM backoffice_audit WHERE operation='signature-policy-update') FROM merchants WHERE id=2`).Scan(&revision, &count); e != nil || revision != 2 || count != 1 {
		t.Fatal("cross actor CAS mutated more than once", revision, count, e)
	}
}

func TestSignature163AdminPolicyPermissionStrictJSONAndFullID(t *testing.T) {
	c := adminCredentialsFixture162(t)
	ctx := context.Background()
	state := signatureState163(t, c.request("GET", "2/signature-policy", "", c.csrf))
	body := signaturePatch163(state["fingerprint"].(string), 1)
	for _, tc := range []struct {
		method, path, body, csrf string
		want                     int
	}{
		{"PATCH", "2/signature-policy", body, "bad", 403}, {"GET", "02/signature-policy", "", c.csrf, 400}, {"GET", "2/signature-policy?merchant_id=1", "", c.csrf, 400},
		{"PATCH", "2/signature-policy", strings.TrimSuffix(body, "}") + `,"keytype":0}`, c.csrf, 400},
		{"PATCH", "2/signature-policy", strings.TrimSuffix(body, "}") + `,"merchant_id":"1"}`, c.csrf, 400},
		{"PATCH", "2/signature-policy", `{"keytype":"1","expected_fingerprint":"` + state["fingerprint"].(string) + `"}`, c.csrf, 400},
	} {
		w := c.request(tc.method, tc.path, tc.body, tc.csrf)
		if w.Code != tc.want || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("admin policy invalid request", w.Code, tc.want)
		}
	}
	one := signatureState163(t, c.request("GET", "1/signature-policy", "", c.csrf))
	if w := c.request("PATCH", "1/signature-policy", signaturePatch163(one["fingerprint"].(string), 1), c.csrf); w.Code != 422 {
		t.Fatal("RSA-only enabled without public key", w.Code)
	}
	const mid int64 = 9007199254740993
	if e := c.s.Store.CreateMerchant(ctx, payment.Merchant{ID: mid, Name: "exact admin target", APIKey: "synthetic-admin163-exact-key", SignatureAlgorithm: "MD5"}); e != nil {
		t.Fatal(e)
	}
	high := signatureState163(t, c.request("GET", fmt.Sprint(mid)+"/signature-policy", "", c.csrf))
	high = signatureState163(t, c.request("PATCH", fmt.Sprint(mid)+"/signature-policy", signaturePatch163(high["fingerprint"].(string), 0), c.csrf))
	if high["merchant_id"] != "9007199254740993" {
		t.Fatal("admin policy ID rounded")
	}
	if _, e := c.pool.Exec(ctx, "UPDATE console_users SET admin_class='operator' WHERE username='admin'"); e != nil {
		t.Fatal(e)
	}
	if w := c.request("GET", "2/signature-policy", "", c.csrf); w.Code != 401 {
		t.Fatal("demotion left original session active", w.Code)
	}
	var version int64
	if e := c.pool.QueryRow(ctx, "SELECT auth_version FROM console_users WHERE username='admin'").Scan(&version); e != nil {
		t.Fatal(e)
	}
	token, csrf, createErr := c.s.Sessions.Create(identity.User{ID: 1, Role: "admin", Username: "admin", AdminClass: "operator", AuthVersion: version})
	if createErr != nil {
		t.Fatal(createErr)
	}
	c.token, c.csrf = token, csrf
	if w := c.request("GET", "2/signature-policy", "", c.csrf); w.Code != 403 {
		t.Fatal("fresh operator policy capability", w.Code)
	}
}

func TestSignature163AdminPolicyActorWaitExpiryAuditRollback(t *testing.T) {
	for _, change := range []string{"demote", "revoke", "expiry", "audit-failure"} {
		t.Run(change, func(t *testing.T) {
			c := adminCredentialsFixture162(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			state := signatureState163(t, c.request("GET", "2/signature-policy", "", c.csrf))
			body := signaturePatch163(state["fingerprint"].(string), 0)
			before := policyRowDigest163(t, c, 2)
			if change == "expiry" || change == "audit-failure" {
				action := "PERFORM pg_sleep(1.3);"
				if change == "audit-failure" {
					action = "RAISE EXCEPTION 'synthetic admin policy audit failure';"
				}
				if _, e := c.pool.Exec(ctx, `CREATE SEQUENCE signature163_admin_entered;CREATE FUNCTION signature163_admin_gate() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.operation='signature-policy-update' THEN PERFORM nextval('signature163_admin_entered');`+action+`END IF;RETURN NEW;END$$;CREATE TRIGGER signature163_admin_gate BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION signature163_admin_gate()`); e != nil {
					t.Fatal(e)
				}
				if change == "expiry" {
					if _, e := c.pool.Exec(ctx, "UPDATE console_sessions SET expires_at=clock_timestamp()+interval '1 second' WHERE username='admin'"); e != nil {
						t.Fatal(e)
					}
				}
				w := c.request("PATCH", "2/signature-policy", body, c.csrf)
				want := 503
				if change == "expiry" {
					want = 403
				}
				if w.Code != want {
					t.Fatal("admin audit failure/expiry status", w.Code, want)
				}
				var entered bool
				if e := c.pool.QueryRow(ctx, "SELECT is_called FROM signature163_admin_entered").Scan(&entered); e != nil || !entered {
					t.Fatal("admin audit gate was not reached", e)
				}
			} else {
				block, e := c.pool.Begin(ctx)
				if e != nil {
					t.Fatal(e)
				}
				defer block.Rollback(context.Background())
				if _, e = block.Exec(ctx, "SELECT id FROM merchants WHERE id=2 FOR UPDATE"); e != nil {
					t.Fatal(e)
				}
				done := make(chan *httptest.ResponseRecorder, 1)
				go func() { done <- c.request("PATCH", "2/signature-policy", body, c.csrf) }()
				for {
					var waiting bool
					if e = c.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1::integer=ANY(pg_blocking_pids(pid)))", block.Conn().PgConn().PID()).Scan(&waiting); e != nil {
						t.Fatal(e)
					}
					if waiting {
						break
					}
					select {
					case early := <-done:
						t.Fatal("admin policy did not wait", early.Code)
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					case <-time.After(10 * time.Millisecond):
					}
				}
				sql := "DELETE FROM console_sessions WHERE username='admin'"
				if change == "demote" {
					sql = "UPDATE console_users SET admin_class='operator' WHERE username='admin'"
				}
				if _, e = c.pool.Exec(ctx, sql); e != nil {
					t.Fatal(e)
				}
				if e = block.Rollback(ctx); e != nil {
					t.Fatal(e)
				}
				select {
				case w := <-done:
					if w.Code != 403 {
						t.Fatal("invalid admin committed policy", w.Code)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			if policyRowDigest163(t, c, 2) != before {
				t.Fatal("invalid admin/audit failure changed merchant/audit")
			}
		})
	}
}

func TestSignature163AdminGenericAndPolicyRaceKeepAnAvailableCredential(t *testing.T) {
	c := adminCredentialsFixture162(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, e := c.pool.Exec(ctx, `UPDATE merchants SET rsa_public_key=(SELECT rsa_public_key FROM merchants WHERE id=2) WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	state := signatureState163(t, c.request("GET", "1/signature-policy", "", c.csrf))
	block, e := c.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer block.Rollback(context.Background())
	if _, e = block.Exec(ctx, "SELECT id FROM merchants WHERE id=1 FOR UPDATE"); e != nil {
		t.Fatal(e)
	}
	done := make(chan *httptest.ResponseRecorder, 2)
	go func() {
		done <- c.request("PATCH", "1/signature-policy", signaturePatch163(state["fingerprint"].(string), 1), c.csrf)
	}()
	go func() { done <- adminResource163(c, "PATCH", "resources/merchants/1", `{"rsa_public_key":""}`) }()
	for {
		var waiting int
		if e = c.pool.QueryRow(ctx, `WITH RECURSIVE waiters AS (
 SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND $1::integer=ANY(pg_blocking_pids(pid))
 UNION SELECT a.pid FROM pg_stat_activity a JOIN waiters w ON w.pid=ANY(pg_blocking_pids(a.pid))
) SELECT count(*) FROM waiters`, block.Conn().PgConn().PID()).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting >= 2 {
			break
		}
		select {
		case early := <-done:
			t.Fatal("both competing writes did not reach original lock", early.Code)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	if e = block.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	wins := 0
	for i := 0; i < 2; i++ {
		select {
		case w := <-done:
			if w.Code == 200 {
				wins++
			} else if w.Code != 409 {
				t.Fatal("competing config write status", w.Code)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if wins != 1 {
		t.Fatal("generic/policy race winner count", wins)
	}
	final := signatureState163(t, c.request("GET", "1/signature-policy", "", c.csrf))
	if final["keytype"] == float64(1) && final["rsa_configured"] != true {
		t.Fatal("race enabled RSA-only without RSA")
	}
	if final["keytype"] != nil && final["keytype"] != float64(1) {
		t.Fatal("race created unrelated mode")
	}
	var revision int64
	var count int
	if e = c.pool.QueryRow(ctx, "SELECT config_revision,(SELECT count(*) FROM backoffice_audit WHERE resource_id=1) FROM merchants WHERE id=1").Scan(&revision, &count); e != nil || revision != 2 || count != 1 {
		t.Fatal("race produced extra write/audit", revision, count, e)
	}
}

func TestSignature163AdminGenericCompatibilityCanLackRSAWhileNullStrictCannot(t *testing.T) {
	c := adminCredentialsFixture162(t)
	ctx := context.Background()
	before := policyRowDigest163(t, c, 2)
	if w := adminResource163(c, "PATCH", "resources/merchants/2", `{"rsa_public_key":""}`); w.Code != 422 {
		t.Fatal("NULL strict RSA accepted missing key", w.Code)
	}
	if policyRowDigest163(t, c, 2) != before {
		t.Fatal("NULL strict rejection changed row")
	}
	if _, e := c.pool.Exec(ctx, "UPDATE merchants SET keytype=0 WHERE id=2"); e != nil {
		t.Fatal(e)
	}
	if w := adminResource163(c, "PATCH", "resources/merchants/2", `{"rsa_public_key":""}`); w.Code != 200 {
		t.Fatal("compatibility cannot temporarily keep MD5 alone", w.Code)
	}
	state := signatureState163(t, c.request("GET", "2/signature-policy", "", c.csrf))
	if state["keytype"] != float64(0) || state["rsa_configured"] != false || state["algorithm"] != "RSA" {
		t.Fatal("compatibility clear silently changed policy/legacy algorithm", state)
	}
	if w := c.request("PATCH", "2/signature-policy", signaturePatch163(state["fingerprint"].(string), 1), c.csrf); w.Code != 422 {
		t.Fatal("RSA-only enabled after public key removed", w.Code)
	}
	if w := adminResource163(c, "PATCH", "resources/merchants/2", `{"keytype":1}`); w.Code != 403 {
		t.Fatal("read-only policy writable through generic profile", w.Code)
	}
}

func TestSignature163AdminPolicyChangesPreserveKeysAndCredentialGenerationPreservesMode(t *testing.T) {
	c := adminCredentialsFixture162(t)
	ctx := context.Background()
	before, e := c.s.Store.Merchant(ctx, 2)
	if e != nil {
		t.Fatal(e)
	}
	state := signatureState163(t, c.request("GET", "2/signature-policy", "", c.csrf))
	state = signatureState163(t, c.request("PATCH", "2/signature-policy", signaturePatch163(state["fingerprint"].(string), 0), c.csrf))
	state = signatureState163(t, c.request("PATCH", "2/signature-policy", signaturePatch163(state["fingerprint"].(string), 1), c.csrf))
	after, e := c.s.Store.Merchant(ctx, 2)
	if e != nil || after.APIKey != before.APIKey || after.RSAPublicKey != before.RSAPublicKey || after.SignatureAlgorithm != before.SignatureAlgorithm {
		t.Fatal("admin policy modified keys/algorithm", e)
	}
	for _, kind := range []string{"reset-md5", "generate-rsa"} {
		cred := credentialData162(t, c.request("GET", "2/credentials", "", c.csrf))
		w := c.request("POST", "2/credentials/"+kind, fmt.Sprintf(`{"expected_fingerprint":%q}`, cred["fingerprint"]), c.csrf)
		if w.Code != 200 {
			t.Fatal("credential generation failed", kind, w.Code)
		}
		next := signatureState163(t, c.request("GET", "2/signature-policy", "", c.csrf))
		if next["keytype"] != float64(1) || next["algorithm"] != "RSA" || next["fingerprint"] == state["fingerprint"] {
			t.Fatal("generation changed policy or ignored its CAS", next)
		}
		state = next
	}
}
