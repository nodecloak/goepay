package server

import (
	"encoding/json"
	"goepay/internal/identity"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func weixinApplicationHTTP(f channel168Fixture, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/console/v1/admin/weixin-applications"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func TestWeixinApplicationVaultActualOperatorReadsPublicDataButCannotMutate(t *testing.T) {
	f := channel168Local(t)
	body := `{"request_id":"` + strings.Repeat("9", 64) + `","kind":"official_account","name":"Operator readable","app_id":"wxoperator","appsecret":"synthetic-operator-secret","enabled":false,"source_channel_id":"0","merchant_ids":[],"path":"","expected_source_fingerprint":""}`
	if result := weixinApplicationHTTP(f, "POST", "", body); result.Code != 201 {
		t.Fatal("super setup failed", result.Code)
	}
	if _, e := f.p.Exec(t.Context(), `UPDATE console_users SET admin_class='operator' WHERE username='admin168'`); e != nil {
		t.Fatal(e)
	}
	var version int64
	if e := f.p.QueryRow(t.Context(), `SELECT auth_version FROM console_users WHERE username='admin168'`).Scan(&version); e != nil {
		t.Fatal(e)
	}
	var e error
	f.token, f.csrf, e = f.s.Sessions.Create(identity.User{ID: 168, Role: "admin", Username: "admin168", AdminClass: "operator", AuthVersion: version})
	if e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{"", "/capabilities"} {
		result := weixinApplicationHTTP(f, "GET", path, "")
		var out struct{ Data map[string]any }
		if result.Code != 200 || json.Unmarshal(result.Body.Bytes(), &out) != nil || out.Data["can_write"] != false || strings.Contains(result.Body.String(), "synthetic-operator-secret") {
			t.Fatal("original operator cannot read redacted application directory", path, result.Code)
		}
		if path == "/capabilities" && len(out.Data["sources"].([]any)) != 0 {
			t.Fatal("read-only operator received private write scope enumeration")
		}
	}
	if result := weixinApplicationHTTP(f, "POST", "", strings.Replace(body, strings.Repeat("9", 64), strings.Repeat("8", 64), 1)); result.Code != 403 {
		t.Fatal("read-only operator mutated application credentials", result.Code)
	}
	var count int
	if e = f.p.QueryRow(t.Context(), `SELECT count(*) FROM runtime_weixin_app_heads`).Scan(&count); e != nil || count != 1 {
		t.Fatal("rejected operator left a credential record", count, e)
	}
}

func TestWeixinApplicationVaultActualHTTPDraftEncryptsSecretAndRecoversReceipt(t *testing.T) {
	f := channel168Local(t)
	const secret = "synthetic-weixin-vault-secret-never-public"
	const requestID = "c4a358ac809d458c895b8af1fefaf223c4a358ac809d458c895b8af1fefaf223"
	body := `{"request_id":"` + requestID + `","kind":"official_account","name":"Synthetic Weixin","app_id":"wxvaultsynthetic","appsecret":"` + secret + `","enabled":false,"source_channel_id":"0","merchant_ids":[],"path":"","expected_source_fingerprint":""}`
	w := weixinApplicationHTTP(f, "POST", "", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("managed application draft cannot be created: HTTP=%d", w.Code)
	}
	if strings.Contains(w.Body.String(), secret) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("application response disclosed/cached credentials")
	}
	var out struct{ Data map[string]any }
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil || out.Data["draft"] != true || out.Data["secret_configured"] != true || out.Data["execution_ready"] != false {
		t.Fatal("unscoped application was presented as executable", out.Data, e)
	}
	recovered := weixinApplicationHTTP(f, "GET", "/requests/"+requestID, "")
	if recovered.Code != http.StatusOK || strings.Contains(recovered.Body.String(), secret) {
		t.Fatal("unknown result receipt is unavailable or exposes secret", recovered.Code)
	}
	var copies int
	if e := f.p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM backoffice_records WHERE body::text LIKE '%'||$1||'%')+(SELECT count(*) FROM backoffice_audit WHERE changes::text LIKE '%'||$1||'%')`, secret).Scan(&copies); e != nil || copies != 0 {
		t.Fatal("secret persisted in generic records or audit", copies, e)
	}
	var ciphertext string
	if e := f.p.QueryRow(t.Context(), `SELECT encode(ciphertext,'escape') FROM runtime_weixin_app_versions LIMIT 1`).Scan(&ciphertext); e != nil || strings.Contains(ciphertext, secret) {
		t.Fatal("application credential was not encrypted", e)
	}
	if duplicate := weixinApplicationHTTP(f, "POST", "", body); duplicate.Code != http.StatusCreated {
		t.Fatal("same confirmed request was not idempotent", duplicate.Code)
	}
	body = strings.Replace(body, secret, "synthetic-weixin-vault-different", 1)
	if conflict := weixinApplicationHTTP(f, "POST", "", body); conflict.Code != http.StatusConflict {
		t.Fatal("request id reused for different credentials", conflict.Code)
	}
}

func TestWeixinApplicationVaultCASRotationKeepsOriginalReceiptAndRejectsWrongCredentialBinding(t *testing.T) {
	f := channel168Local(t)
	const secret = "synthetic-vault-cas-secret"
	create := `{"request_id":"` + strings.Repeat("a", 64) + `","kind":"official_account","name":"Synthetic rotation","app_id":"wxvaultcas","appsecret":"` + secret + `","enabled":false,"source_channel_id":"0","merchant_ids":[],"path":"","expected_source_fingerprint":""}`
	w := weixinApplicationHTTP(f, "POST", "", create)
	if w.Code != http.StatusCreated {
		t.Fatal("managed app creation missing", w.Code)
	}
	var initial struct{ Data map[string]any }
	if e := json.Unmarshal(w.Body.Bytes(), &initial); e != nil {
		t.Fatal(e)
	}
	id, _ := initial.Data["id"].(string)
	fingerprint, _ := initial.Data["fingerprint"].(string)
	patch := `{"request_id":"` + strings.Repeat("b", 64) + `","expected_revision":"1","expected_fingerprint":"` + fingerprint + `","kind":"official_account","name":"Renamed without secret","app_id":"wxvaultcas","appsecret":"","enabled":false,"source_channel_id":"0","merchant_ids":[],"path":"","expected_source_fingerprint":""}`
	if out := weixinApplicationHTTP(f, "PATCH", "/"+id, patch); out.Code != http.StatusOK {
		t.Fatal("blank edit secret did not preserve existing credential", out.Code)
	}
	if out := weixinApplicationHTTP(f, "PATCH", "/"+id, strings.Replace(patch, strings.Repeat("b", 64), strings.Repeat("c", 64), 1)); out.Code != http.StatusConflict {
		t.Fatal("stale CAS overwrote a newer revision", out.Code)
	}
	receipt := weixinApplicationHTTP(f, "GET", "/requests/"+strings.Repeat("a", 64), "")
	var old struct{ Data map[string]any }
	if receipt.Code != 200 || json.Unmarshal(receipt.Body.Bytes(), &old) != nil || old.Data["revision"] != "1" || old.Data["name"] != "Synthetic rotation" {
		t.Fatal("old receipt was replaced with current head", receipt.Code, old.Data)
	}
	current := weixinApplicationHTTP(f, "GET", "/"+id, "")
	var latest struct{ Data map[string]any }
	if current.Code != 200 || json.Unmarshal(current.Body.Bytes(), &latest) != nil {
		t.Fatal("current app unavailable")
	}
	invalid := strings.Replace(patch, `"expected_revision":"1"`, `"expected_revision":"2"`, 1)
	invalid = strings.Replace(invalid, fingerprint, latest.Data["fingerprint"].(string), 1)
	invalid = strings.Replace(invalid, strings.Repeat("b", 64), strings.Repeat("d", 64), 1)
	invalid = strings.Replace(invalid, "wxvaultcas", "wxchangedwithoutsecret", 1)
	if out := weixinApplicationHTTP(f, "PATCH", "/"+id, invalid); out.Code != http.StatusBadRequest && out.Code != http.StatusUnprocessableEntity {
		t.Fatal("old secret silently attached to a different app", out.Code)
	}
	var versions int
	if e := f.p.QueryRow(t.Context(), `SELECT count(*) FROM runtime_weixin_app_versions`).Scan(&versions); e != nil || versions != 2 {
		t.Fatal("rejected mutation retained a version", versions, e)
	}
}

func TestWeixinApplicationVaultFinalSessionExpiryRollsBackEveryPrivateWrite(t *testing.T) {
	f := channel168Local(t)
	if _, e := f.p.Exec(t.Context(), `CREATE FUNCTION app_expiry_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='weixin-applications' THEN PERFORM pg_sleep(GREATEST(0,(SELECT extract(epoch FROM expires_at-clock_timestamp()) FROM console_sessions WHERE role='admin' ORDER BY expires_at DESC LIMIT 1))::double precision+0.05); END IF; RETURN NEW; END $$; CREATE TRIGGER app_expiry_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION app_expiry_audit(); UPDATE console_sessions SET expires_at=clock_timestamp()+interval '1 second' WHERE role='admin'`); e != nil {
		t.Fatal(e)
	}
	body := `{"request_id":"` + strings.Repeat("e", 64) + `","kind":"official_account","name":"Expiry draft","app_id":"wxexpiry","appsecret":"synthetic-expiry-only","enabled":false,"source_channel_id":"0","merchant_ids":[],"path":"","expected_source_fingerprint":""}`
	w := weixinApplicationHTTP(f, "POST", "", body)
	if w.Code != http.StatusForbidden && w.Code != http.StatusUnauthorized {
		t.Fatal("expired administrator committed private app configuration", w.Code)
	}
	var retained int
	if e := f.p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM runtime_weixin_app_heads)+(SELECT count(*) FROM runtime_weixin_app_versions)+(SELECT count(*) FROM runtime_weixin_app_requests)+(SELECT count(*) FROM backoffice_audit WHERE resource='weixin-applications')`).Scan(&retained); e != nil || retained != 0 {
		t.Fatal("expired write left private state or receipt", retained, e)
	}
}
