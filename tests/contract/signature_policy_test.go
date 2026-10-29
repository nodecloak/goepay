package contract_test

import (
	"context"
	"crypto/rsa"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/compat"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"goepay/internal/server"
)

type signatureFixture163 struct {
	s           *server.Server
	p           *pgxpool.Pool
	key         *rsa.PrivateKey
	token, csrf string
}

func signatureFixture(t *testing.T, mid int64) signatureFixture163 {
	t.Helper()
	s, p, key := fixture(t)
	ctx := context.Background()
	if _, e := p.Exec(ctx, "UPDATE console_users SET subject_id=$1 WHERE username='merchant'", mid); e != nil {
		t.Fatal(e)
	}
	s.Sessions = identity.NewPersistentSessions(p, false)
	var version int64
	if e := p.QueryRow(ctx, "SELECT auth_version FROM console_users WHERE username='merchant'").Scan(&version); e != nil {
		t.Fatal(e)
	}
	token, csrf, e := s.Sessions.Create(identity.User{ID: mid, Role: "merchant", Username: "merchant", AuthVersion: version})
	if e != nil {
		t.Fatal(e)
	}
	return signatureFixture163{s, p, key, token, csrf}
}
func (f signatureFixture163) request(method, path, body, csrf string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/console/v1/merchant/account/"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}
func signatureState163(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("signature policy HTTP=%d want 200", w.Code)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("signature policy can be cached")
	}
	v := credentialData162(t, w)
	for _, secret := range []string{"contract-secret", "contract-rsa-secret", "PRIVATE KEY", "api_key", "public_key"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatalf("policy response exposed %s", secret)
		}
	}
	if len(v) != 6 || v["can_update"] != true {
		t.Fatal("policy DTO mismatch", v)
	}
	return v
}
func signaturePatch163(fp string, keytype int) string {
	return fmt.Sprintf(`{"keytype":%d,"expected_fingerprint":%q}`, keytype, fp)
}

func TestSignature163OwnPolicyCASAndNoSecret(t *testing.T) {
	f := signatureFixture(t, 2)
	ctx := context.Background()
	before, e := f.s.Store.Merchant(ctx, 2)
	if e != nil {
		t.Fatal(e)
	}
	state := signatureState163(t, f.request("GET", "signature-policy", "", f.csrf))
	if state["merchant_id"] != "2" || state["keytype"] != nil || state["algorithm"] != "RSA" || state["rsa_configured"] != true {
		t.Fatal("unexpected initial strict state", state)
	}
	fp := state["fingerprint"].(string)
	body := signaturePatch163(fp, 0)
	if w := f.request("PATCH", "signature-policy", body, "wrong"); w.Code != 403 {
		t.Fatal("CSRF missing", w.Code)
	}
	results := make(chan *httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- f.request("PATCH", "signature-policy", body, f.csrf) }()
	}
	wg.Wait()
	close(results)
	wins := 0
	for w := range results {
		if w.Code == 200 {
			wins++
			v := signatureState163(t, w)
			if v["keytype"] != float64(0) || v["fingerprint"] == fp {
				t.Fatal("CAS state did not advance", v)
			}
		} else if w.Code != 409 {
			t.Fatal("concurrent policy status", w.Code)
		}
	}
	if wins != 1 {
		t.Fatal("CAS winners", wins)
	}
	if w := f.request("PATCH", "signature-policy", body, f.csrf); w.Code != 409 {
		t.Fatal("old confirmation replay", w.Code)
	}
	after, e := f.s.Store.Merchant(ctx, 2)
	if e != nil || after.ConfigRevision != before.ConfigRevision+1 || after.SignatureAlgorithm != before.SignatureAlgorithm || after.APIKey != before.APIKey || after.RSAPublicKey != before.RSAPublicKey {
		t.Fatal("policy switch changed keys/algorithm", e)
	}
	var count int
	var audit string
	if e = f.p.QueryRow(ctx, `SELECT count(*),COALESCE(jsonb_agg(changes)::text,'[]') FROM backoffice_audit WHERE operation='signature-policy-update'`).Scan(&count, &audit); e != nil || count != 1 || strings.Contains(audit, "contract-") {
		t.Fatal("audit not atomic or contains secret", count, e)
	}
}

func TestSignature163PolicyValidationAndExactOwnTarget(t *testing.T) {
	f := signatureFixture(t, 1)
	state := signatureState163(t, f.request("GET", "signature-policy", "", f.csrf))
	fp := state["fingerprint"].(string)
	if state["rsa_configured"] != false {
		t.Fatal("unconfigured RSA")
	}
	for _, tc := range []struct {
		path, body string
		want       int
	}{
		{"signature-policy", signaturePatch163(fp, 1), 422},
		{"signature-policy?merchant_id=2", signaturePatch163(fp, 0), 400},
		{"signature-policy", `{"keytype":"0","expected_fingerprint":"` + fp + `"}`, 400},
		{"signature-policy", `{"keytype":null,"expected_fingerprint":"` + fp + `"}`, 400},
		{"signature-policy", signaturePatch163(fp, 2), 400},
		{"signature-policy", strings.TrimSuffix(signaturePatch163(fp, 0), "}") + `,"merchant_id":"2"}`, 400},
		{"signature-policy", strings.TrimSuffix(signaturePatch163(fp, 0), "}") + `,"keytype":1}`, 400},
		{"signature-policy", `{"keytype":0,"expected_fingerprint":"` + fp + `","expected_fingerprint":"` + fp + `"}`, 400},
		{"signature-policy", `null`, 400}, {"signature-policy", `{}`, 400},
	} {
		if w := f.request("PATCH", tc.path, tc.body, f.csrf); w.Code != tc.want {
			t.Fatalf("policy malformed request got %d want %d", w.Code, tc.want)
		}
	}
	next := signatureState163(t, f.request("PATCH", "signature-policy", signaturePatch163(fp, 0), f.csrf))
	if next["keytype"] != float64(0) || next["rsa_configured"] != false {
		t.Fatal("compatibility without RSA should remain usable for MD5")
	}
	var other *int
	if e := f.p.QueryRow(context.Background(), "SELECT keytype FROM merchants WHERE id=2").Scan(&other); e != nil || other != nil {
		t.Fatal("policy changed another merchant", e)
	}
}

func signatureCall163(t *testing.T, f signatureFixture163, path, algorithm, stamp string, extra compat.Params) map[string]any {
	t.Helper()
	p := compat.Params{"pid": "2", "sign_type": algorithm}
	for k, v := range extra {
		p[k] = v
	}
	if stamp != "" {
		p["timestamp"] = stamp
	}
	var e error
	if algorithm == "RSA" {
		p["sign"], e = compat.SignRSA(p, f.key)
	} else {
		secret := "contract-rsa-secret"
		if p["pid"] == "1" {
			secret = "contract-secret"
		}
		p["sign"] = compat.SignMD5(p, secret)
	}
	if e != nil {
		t.Fatal(e)
	}
	v := url.Values{}
	for k, x := range p {
		v.Set(k, x)
	}
	_, out := call(t, f.s.Handler(), "POST", path, v)
	return out
}

func TestSignature163StrictMD5IsNotGloballyDowngradedAndExplicitModesOverrideAlgorithm(t *testing.T) {
	f := signatureFixture(t, 1)
	ctx := context.Background()
	stamp := fmt.Sprint(time.Now().Unix())
	if _, e := f.p.Exec(ctx, `UPDATE merchants SET rsa_public_key=(SELECT rsa_public_key FROM merchants WHERE id=2) WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	extra := compat.Params{"pid": "1", "out_trade_no": "strict-md5-rsa", "money": "1.00", "name": "local", "type": "sandbox"}
	if out := signatureCall163(t, f, "/mapi.php", "RSA", stamp, extra); out["code"] != float64(-1) {
		t.Fatal("strict MD5 silently accepted RSA", out)
	}
	extra["out_trade_no"] = "strict-md5-only"
	if out := signatureCall163(t, f, "/mapi.php", "MD5", stamp, extra); out["code"] != float64(1) {
		t.Fatal("strict MD5 legacy changed", out)
	}
	if out := signatureCall163(t, f, "/api/merchant/info", "RSA", stamp, compat.Params{"pid": "1"}); out["code"] != float64(-1) {
		t.Fatal("strict MD5 was enabled for V2", out)
	}
	state := signatureState163(t, f.request("GET", "signature-policy", "", f.csrf))
	state = signatureState163(t, f.request("PATCH", "signature-policy", signaturePatch163(state["fingerprint"].(string), 0), f.csrf))
	for _, alg := range []string{"MD5", "RSA"} {
		extra["out_trade_no"] = "explicit-both-" + alg
		if out := signatureCall163(t, f, "/mapi.php", alg, stamp, extra); out["code"] != float64(1) {
			t.Fatal("compat with algorithm MD5 refused", alg, out)
		}
	}
	out := signatureCall163(t, f, "/api/merchant/info", "RSA", stamp, compat.Params{"pid": "1"})
	if out["code"] != float64(0) {
		t.Fatal("explicit compatibility MD5 merchant cannot use V2 RSA", out)
	}
	state = signatureState163(t, f.request("PATCH", "signature-policy", signaturePatch163(state["fingerprint"].(string), 1), f.csrf))
	if state["algorithm"] != "MD5" {
		t.Fatal("policy silently switched algorithm")
	}
	extra["out_trade_no"] = "rsa-only-on-md5"
	if out := signatureCall163(t, f, "/mapi.php", "RSA", stamp, extra); out["code"] != float64(1) {
		t.Fatal("RSA-only with preserved MD5 algorithm unusable", out)
	}
	extra["out_trade_no"] = "denied-md5-on-md5"
	if out := signatureCall163(t, f, "/mapi.php", "MD5", stamp, extra); out["code"] != float64(-1) {
		t.Fatal("RSA-only downgraded because old algorithm MD5", out)
	}
}

func TestSignature163FingerprintBindsEveryCredentialFactAndRevision(t *testing.T) {
	for _, change := range []string{"revision", "policy", "algorithm", "public", "cipher"} {
		t.Run(change, func(t *testing.T) {
			f := signatureFixture(t, 2)
			ctx := context.Background()
			state := signatureState163(t, f.request("GET", "signature-policy", "", f.csrf))
			sql := map[string]string{
				"revision":  "UPDATE merchants SET config_revision=config_revision+1 WHERE id=2",
				"policy":    "UPDATE merchants SET keytype=0 WHERE id=2",
				"algorithm": "UPDATE merchants SET signature_algorithm='MD5' WHERE id=2",
				"public":    "UPDATE merchants SET rsa_public_key=rsa_public_key||E'\\n' WHERE id=2",
				"cipher":    "UPDATE merchants SET api_key=set_byte(api_key,0,(get_byte(api_key,0)+1)%256) WHERE id=2",
			}[change]
			if _, e := f.p.Exec(ctx, sql); e != nil {
				t.Fatal(e)
			}
			if w := f.request("PATCH", "signature-policy", signaturePatch163(state["fingerprint"].(string), 0), f.csrf); w.Code != 409 {
				t.Fatal("old policy confirmation did not bind", change, w.Code)
			}
		})
	}
}

func TestSignature163EveryPolicyResponseIsNoStore(t *testing.T) {
	f := signatureFixture(t, 2)
	for _, tc := range []struct {
		method, path, body, csrf string
		want                     int
	}{
		{"PATCH", "signature-policy", `{}`, "wrong", 403},
		{"GET", "signature-policy?merchant_id=1", "", f.csrf, 400},
		{"PATCH", "signature-policy", `{}`, f.csrf, 400},
		{"POST", "signature-policy", `{}`, f.csrf, 405},
	} {
		w := f.request(tc.method, tc.path, tc.body, tc.csrf)
		if w.Code != tc.want || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("policy error cache/status", w.Code, w.Header().Get("Cache-Control"))
		}
	}
}

func TestSignature163CompatibilityAndRSAOnlyEverySignedEntry(t *testing.T) {
	f := signatureFixture(t, 2)
	ctx := context.Background()
	// Personal transfer defaults now intersect the existing administrator grant.
	// Keep this algorithm test authorized so it still reaches every verifier.
	if _, e := f.p.Exec(ctx, `UPDATE merchants SET profile=jsonb_set(profile,'{transfer_api_enabled}','true') WHERE id=2`); e != nil {
		t.Fatal(e)
	}
	stamp := fmt.Sprint(time.Now().Unix())
	// Null policy must retain strict RSA and reject MD5 even with a correct secret.
	if out := signatureCall163(t, f, "/api/merchant/info", "MD5", stamp, nil); out["code"] != float64(-1) {
		t.Fatal("strict RSA downgraded", out)
	}
	state := signatureState163(t, f.request("GET", "signature-policy", "", f.csrf))
	state = signatureState163(t, f.request("PATCH", "signature-policy", signaturePatch163(state["fingerprint"].(string), 0), f.csrf))
	// V1 signed form and legacy key authentication both accept explicit compatibility.
	_, out := call(t, f.s.Handler(), "GET", "/api.php", url.Values{"pid": {"2"}, "act": {"query"}, "key": {"contract-rsa-secret"}})
	if out["code"] != float64(1) {
		t.Fatal("compat MD5 legacy key refused", out)
	}
	for _, alg := range []string{"MD5", "RSA"} {
		out = signatureCall163(t, f, "/mapi.php", alg, stamp, compat.Params{"out_trade_no": "sig163-" + alg, "money": "1.00", "name": "local", "type": "sandbox"})
		if out["code"] != float64(1) {
			t.Fatal("compat V1 signed form refused", alg, out)
		}
	}
	for _, path := range []string{"/api/merchant/info", "/api/transfer/balance"} {
		out = signatureCall163(t, f, path, "RSA", stamp, nil)
		if out["code"] != float64(0) || out["sign_type"] != "RSA" {
			t.Fatal("V2 RSA unavailable", path, out)
		}
		out = signatureCall163(t, f, path, "MD5", stamp, nil)
		if out["code"] != float64(-1) {
			t.Fatal("compat must never allow V2 MD5", path, out)
		}
	}
	for _, bad := range []string{"", fmt.Sprint(time.Now().Add(-time.Hour).Unix()), "9223372036854775807"} {
		out = signatureCall163(t, f, "/mapi.php", "RSA", bad, compat.Params{"out_trade_no": "bad-time", "money": "1.00", "name": "local", "type": "sandbox"})
		if out["code"] != float64(-1) {
			t.Fatal("compat RSA accepted stale/missing timestamp", out)
		}
	}
	state = signatureState163(t, f.request("PATCH", "signature-policy", signaturePatch163(state["fingerprint"].(string), 1), f.csrf))
	for _, path := range []string{"/mapi.php", "/submit.php", "/api/pay/create", "/api/pay/submit", "/api/pay/query", "/api/pay/close", "/api/pay/refund", "/api/pay/refundquery", "/api/merchant/info", "/api/merchant/orders", "/api/transfer/balance", "/api/transfer/query", "/api/transfer/submit", "/api/transfer/proof"} {
		out = signatureCall163(t, f, path, "MD5", stamp, nil)
		if out["code"] != float64(-1) {
			t.Fatal("RSA-only accepted MD5", path, out)
		}
	}
	for _, act := range []string{"query", "orders", "order", "refund", "close"} {
		_, out = call(t, f.s.Handler(), "GET", "/api.php", url.Values{"pid": {"2"}, "act": {act}, "key": {"contract-rsa-secret"}})
		if out["code"] != float64(-3) {
			t.Fatal("RSA-only legacy key accepted", act, out)
		}
	}
	// Clearing/invalidating RSA must fail closed even under compatibility policy.
	if _, e := f.p.Exec(ctx, "UPDATE merchants SET keytype=0,rsa_public_key='' WHERE id=2"); e != nil {
		t.Fatal(e)
	}
	out = signatureCall163(t, f, "/api/merchant/info", "RSA", stamp, nil)
	if out["code"] != float64(-1) {
		t.Fatal("missing RSA key allowed RSA", out)
	}
}

func TestSignature163ActorWaitExpiryAndAuditRollback(t *testing.T) {
	for _, change := range []string{"revoke", "disable", "auth-version", "role", "expiry", "audit-failure"} {
		t.Run(change, func(t *testing.T) {
			f := signatureFixture(t, 2)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			state := signatureState163(t, f.request("GET", "signature-policy", "", f.csrf))
			body := signaturePatch163(state["fingerprint"].(string), 0)
			if change == "expiry" || change == "audit-failure" {
				action := "PERFORM pg_sleep(1.3);"
				if change == "audit-failure" {
					action = "RAISE EXCEPTION 'local signature audit failure';"
				}
				if _, e := f.p.Exec(ctx, `CREATE SEQUENCE signature163_audit_entered; CREATE FUNCTION signature163_audit_gate() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.operation='signature-policy-update' THEN PERFORM nextval('signature163_audit_entered'); `+action+` END IF; RETURN NEW; END$$; CREATE TRIGGER signature163_audit_gate BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION signature163_audit_gate()`); e != nil {
					t.Fatal(e)
				}
				if change == "expiry" {
					if _, e := f.p.Exec(ctx, "UPDATE console_sessions SET expires_at=clock_timestamp()+interval '1 second' WHERE username='merchant'"); e != nil {
						t.Fatal(e)
					}
				}
				w := f.request("PATCH", "signature-policy", body, f.csrf)
				want := 503
				if change == "expiry" {
					want = 403
				}
				if w.Code != want {
					t.Fatal("audit gate status", w.Code, want)
				}
				var entered bool
				if e := f.p.QueryRow(ctx, "SELECT is_called FROM signature163_audit_entered").Scan(&entered); e != nil || !entered {
					t.Fatal("audit gate did not execute", e)
				}
			} else {
				block, e := f.p.Begin(ctx)
				if e != nil {
					t.Fatal(e)
				}
				defer block.Rollback(context.Background())
				if _, e = block.Exec(ctx, "SELECT id FROM merchants WHERE id=2 FOR UPDATE"); e != nil {
					t.Fatal(e)
				}
				done := make(chan *httptest.ResponseRecorder, 1)
				go func() { done <- f.request("PATCH", "signature-policy", body, f.csrf) }()
				for {
					var waiting bool
					if e = f.p.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1::integer=ANY(pg_blocking_pids(pid)))", block.Conn().PgConn().PID()).Scan(&waiting); e != nil {
						t.Fatal(e)
					}
					if waiting {
						break
					}
					select {
					case early := <-done:
						t.Fatal("request did not wait", early.Code)
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					case <-time.After(10 * time.Millisecond):
					}
				}
				sql := "DELETE FROM console_sessions WHERE username='merchant'"
				if change == "disable" {
					sql = "UPDATE console_users SET enabled=false WHERE username='merchant'"
				}
				if change == "auth-version" {
					sql = "UPDATE console_users SET auth_version=auth_version+1 WHERE username='merchant'"
				}
				if change == "role" {
					// The durable session FK forbids changing a live account's role.
					// Model the valid transition: revoke its sessions, then change role.
					sql = "DELETE FROM console_sessions WHERE username='merchant'; UPDATE console_users SET role='admin' WHERE username='merchant'"
				}
				if _, e = f.p.Exec(ctx, sql); e != nil {
					t.Fatal(e)
				}
				if e = block.Rollback(ctx); e != nil {
					t.Fatal(e)
				}
				select {
				case w := <-done:
					if w.Code != 403 {
						t.Fatal("changed original actor committed", w.Code)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			var policy *int
			var revision int64
			var count int
			if e := f.p.QueryRow(ctx, "SELECT keytype,config_revision,(SELECT count(*) FROM backoffice_audit WHERE operation='signature-policy-update') FROM merchants WHERE id=2").Scan(&policy, &revision, &count); e != nil || policy != nil || revision != 1 || count != 0 {
				t.Fatal("denied/audit failure mutated policy", e, revision, count)
			}
		})
	}
}

func TestSignature163RejectAdminDelegatedAndFullID(t *testing.T) {
	f := signatureFixture(t, 2)
	ctx := context.Background()
	signatureState163(t, f.request("GET", "signature-policy", "", f.csrf))
	tx, e := f.p.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	var version int64
	if e = f.p.QueryRow(ctx, "SELECT auth_version FROM console_users WHERE username='admin'").Scan(&version); e != nil {
		t.Fatal(e)
	}
	issuer := identity.User{ID: 1, Role: "admin", Username: "admin", AuthVersion: version, AdminClass: "super"}
	issuerToken, _, e := f.s.Sessions.Create(issuer)
	if e != nil {
		t.Fatal(e)
	}
	var merchantVersion int64
	if e = f.p.QueryRow(ctx, "SELECT auth_version FROM console_users WHERE username='merchant'").Scan(&merchantVersion); e != nil {
		t.Fatal(e)
	}
	token, csrf, _, e := f.s.Sessions.CreateDelegated(ctx, tx, identity.User{ID: 2, Role: "merchant", Username: "merchant", AuthVersion: merchantVersion}, issuer, issuerToken)
	if e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	f.token, f.csrf = token, csrf
	if w := f.request("GET", "signature-policy", "", csrf); w.Code != 403 {
		t.Fatal("delegated policy capability", w.Code)
	}
	r := httptest.NewRequest("GET", "/console/v1/admin/account/signature-policy", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: issuerToken})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("admin used merchant policy", w.Code)
	}
	// Full IDs remain strings above JS safe integer and can only target the session owner.
	original := signatureFixture(t, 1)
	const mid int64 = 9007199254740993
	if _, e = original.p.Exec(ctx, "UPDATE console_users SET subject_id=$1 WHERE username='merchant';", mid); e != nil {
		t.Fatal(e)
	}
	if e = original.s.Store.CreateMerchant(ctx, payment.Merchant{ID: mid, Name: "large exact owner", APIKey: "synthetic-large-owner-secret163", SignatureAlgorithm: "MD5"}); e != nil {
		t.Fatal(e)
	}
	if e = original.p.QueryRow(ctx, "SELECT auth_version FROM console_users WHERE username='merchant'").Scan(&merchantVersion); e != nil {
		t.Fatal(e)
	}
	original.token, original.csrf, e = original.s.Sessions.Create(identity.User{ID: mid, Role: "merchant", Username: "merchant", AuthVersion: merchantVersion})
	if e != nil {
		t.Fatal(e)
	}
	large := signatureState163(t, original.request("GET", "signature-policy", "", original.csrf))
	if large["merchant_id"] != "9007199254740993" {
		t.Fatal("owner ID truncated")
	}
	large = signatureState163(t, original.request("PATCH", "signature-policy", signaturePatch163(large["fingerprint"].(string), 0), original.csrf))
	if large["merchant_id"] != "9007199254740993" || large["keytype"] != float64(0) {
		t.Fatal("full ID policy update misbound", large)
	}
	if w := original.request("GET", "signature-policy?merchant_id=1", "", original.csrf); w.Code != 400 {
		t.Fatal("query target override", w.Code)
	}
}

func TestSignature163CredentialRotationPreservesPolicyAndInvalidatesPolicyCAS(t *testing.T) {
	f := signatureFixture(t, 2)
	ctx := context.Background()
	state := signatureState163(t, f.request("GET", "signature-policy", "", f.csrf))
	state = signatureState163(t, f.request("PATCH", "signature-policy", signaturePatch163(state["fingerprint"].(string), 1), f.csrf))
	fp := state["fingerprint"].(string)
	var version int64
	if e := f.p.QueryRow(ctx, "SELECT auth_version FROM console_users WHERE username='admin'").Scan(&version); e != nil {
		t.Fatal(e)
	}
	token, csrf, e := f.s.Sessions.Create(identity.User{ID: 1, Role: "admin", Username: "admin", AdminClass: "super", AuthVersion: version})
	if e != nil {
		t.Fatal(e)
	}
	admin := adminCredentials162{f.s, f.p, token, csrf, f.key}
	for _, kind := range []string{"reset-md5", "generate-rsa"} {
		current := credentialData162(t, admin.request("GET", "2/credentials", "", csrf))
		w := admin.request("POST", "2/credentials/"+kind, fmt.Sprintf(`{"expected_fingerprint":%q}`, current["fingerprint"]), csrf)
		if w.Code != 200 {
			t.Fatal("admin credential generation failed", kind, w.Code)
		}
		var policy int
		var algorithm string
		if e = f.p.QueryRow(ctx, "SELECT keytype,signature_algorithm FROM merchants WHERE id=2").Scan(&policy, &algorithm); e != nil || policy != 1 || algorithm != "RSA" {
			t.Fatal("admin generation changed policy", kind, e)
		}
	}
	if w := f.request("PATCH", "signature-policy", signaturePatch163(fp, 0), f.csrf); w.Code != 409 {
		t.Fatal("policy fingerprint ignored credential rotation", w.Code)
	}
	before, e := f.s.Store.Merchant(ctx, 2)
	if e != nil {
		t.Fatal(e)
	}
	info := credentialData162(t, f.request("GET", "api-info", "", f.csrf))
	body := fmt.Sprintf(`{"current_password":"contract-password-123","expected_fingerprint":%q}`, info["rsa_fingerprint"])
	w := f.request("POST", "rsa-key/rotate", body, f.csrf)
	if w.Code != 200 {
		t.Fatal("own RSA rotation failed", w.Code)
	}
	after, e := f.s.Store.Merchant(ctx, 2)
	if e != nil || after.Keytype == nil || *after.Keytype != 1 || after.SignatureAlgorithm != before.SignatureAlgorithm || after.APIKey != before.APIKey || after.RSAPublicKey == before.RSAPublicKey {
		t.Fatal("merchant RSA rotation changed policy/MD5", e)
	}
}

func TestSignature163SessionExpiresWhileWaitingForSessionLock(t *testing.T) {
	f := signatureFixture(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	state := signatureState163(t, f.request("GET", "signature-policy", "", f.csrf))
	if _, e := f.p.Exec(ctx, "UPDATE console_sessions SET expires_at=clock_timestamp()+interval '1.2 seconds' WHERE username='merchant'"); e != nil {
		t.Fatal(e)
	}
	block, e := f.p.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer block.Rollback(context.Background())
	if _, e = block.Exec(ctx, "SELECT token_hash FROM console_sessions WHERE username='merchant' FOR UPDATE"); e != nil {
		t.Fatal(e)
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- f.request("PATCH", "signature-policy", signaturePatch163(state["fingerprint"].(string), 0), f.csrf)
	}()
	for {
		var waiting bool
		if e = f.p.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1::integer=ANY(pg_blocking_pids(pid)))", block.Conn().PgConn().PID()).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		select {
		case early := <-done:
			t.Fatal("session lock not reached", early.Code)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	// Observe the actual database clock instead of assuming a scheduler sleep.
	for {
		var expired bool
		if e = f.p.QueryRow(ctx, "SELECT expires_at<=clock_timestamp() FROM console_sessions WHERE username='merchant'").Scan(&expired); e != nil {
			t.Fatal(e)
		}
		if expired {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
	if e = block.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case w := <-done:
		if w.Code != 403 {
			t.Fatal("session clock was evaluated before lock wait", w.Code)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var policy *int
	if e = f.p.QueryRow(ctx, "SELECT keytype FROM merchants WHERE id=2").Scan(&policy); e != nil || policy != nil {
		t.Fatal("expired lock waiter changed policy", e)
	}
}
