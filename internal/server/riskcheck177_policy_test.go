package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goepay/internal/identity"
)

type risk177Fixture struct {
	s           *Server
	token, csrf string
}

func risk177Local(t *testing.T) risk177Fixture {
	t.Helper()
	s, p := groupRoutingFixture152(t)
	s.Sessions = identity.NewPersistentSessions(p, false)
	if _, e := p.Exec(context.Background(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class)VALUES('risk177','admin',177,'Synthetic Risk','unused','super')`); e != nil {
		t.Fatal(e)
	}
	token, csrf, e := s.Sessions.Create(identity.User{ID: 177, Role: "admin", Username: "risk177", AdminClass: "super", AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	return risk177Fixture{s, token, csrf}
}
func (f risk177Fixture) request(method, body, csrf string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/console/v1/admin/payment-risk", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}
func risk177Data(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("actual core risk HTTP=%d want200", w.Code)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("risk policy cached")
	}
	var out struct {
		Code int
		Data map[string]any
	}
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Code != 0 || out.Data == nil {
		t.Fatal("invalid risk envelope")
	}
	return out.Data
}
func TestRisk177ActualPolicyDefaultsDoNotPretendLegacySettingsAreExecuting(t *testing.T) {
	f := risk177Local(t)
	if _, e := f.s.Pool.Exec(context.Background(), `INSERT INTO backoffice_records(resource,body)VALUES('settings','{"key":"pay_iplimit","value":"1"}')`); e != nil {
		t.Fatal(e)
	}
	v := risk177Data(t, f.request("GET", "", f.csrf))
	if v["configured"] != false || v["revision"] != "0" {
		t.Fatal("old metadata was silently activated", v)
	}
	settings, ok := v["settings"].(map[string]any)
	if !ok || settings["pay_iplimit"] != "" || settings["pay_block_oversea"] != "0" || settings["auto_check_payip"] != "0" {
		t.Fatal("original default fields missing or enabled", settings)
	}
	for _, secret := range []string{"proof_hash", "api_key", "lease_token", "password", "actor_name"} {
		if strings.Contains(f.request("GET", "", f.csrf).Body.String(), secret) {
			t.Fatal("private risk field leaked", secret)
		}
	}
}

func risk177Body(t *testing.T, v map[string]any, change func(map[string]any)) string {
	t.Helper()
	settings := v["settings"].(map[string]any)
	if change != nil {
		change(settings)
	}
	raw, e := json.Marshal(map[string]any{"settings": settings, "expected_fingerprint": v["fingerprint"]})
	if e != nil {
		t.Fatal(e)
	}
	return string(raw)
}
func TestRisk177PolicyStrictOriginalSessionCASAndRollbackAnchor(t *testing.T) {
	f := risk177Local(t)
	ctx := context.Background()
	v := risk177Data(t, f.request("GET", "", f.csrf))
	body := risk177Body(t, v, func(s map[string]any) { s["pay_iplimit"] = "1" })
	if w := f.request("PATCH", body, ""); w.Code != 403 {
		t.Fatal("missing CSRF changed risk policy", w.Code)
	}
	first := risk177Data(t, f.request("PATCH", body, f.csrf))
	if first["configured"] != true || first["revision"] != "1" || first["settings"].(map[string]any)["pay_iplimit"] != "1" {
		t.Fatal("risk policy commit", first)
	}
	if w := f.request("PATCH", body, f.csrf); w.Code != 409 {
		t.Fatal("stale risk CAS accepted", w.Code)
	}
	var original []byte
	if e := f.s.Pool.QueryRow(ctx, `SELECT to_jsonb(p)FROM core_payment_risk_policy p WHERE id=1`).Scan(&original); e != nil {
		t.Fatal(e)
	}
	second := risk177Data(t, f.request("PATCH", risk177Body(t, first, nil), f.csrf))
	if second["revision"] != "2" {
		t.Fatal("same-value original commit did not advance revision")
	}
	if _, e := f.s.Pool.Exec(ctx, `UPDATE core_payment_risk_policy SET(revision,settings,saved_at,proof_hash)=(SELECT r.revision,r.settings,r.saved_at,r.proof_hash FROM jsonb_populate_record(NULL::core_payment_risk_policy,$1::jsonb)r)WHERE id=1`, string(original)); e != nil {
		t.Fatal(e)
	}
	if w := f.request("GET", "", f.csrf); w.Code != 409 {
		t.Fatal("whole old valid policy row restored original enabled risk state", w.Code)
	}
}
func TestRisk177PolicyUnknownDuplicateNullAndAuditFailureCannotCommit(t *testing.T) {
	f := risk177Local(t)
	ctx := context.Background()
	v := risk177Data(t, f.request("GET", "", f.csrf))
	body := risk177Body(t, v, nil)
	for _, invalid := range []string{`null`, `{"settings":{},"settings":{},"expected_fingerprint":"` + v["fingerprint"].(string) + `"}`, strings.Replace(body, `"pay_iplimit":""`, `"pay_iplimit":null`, 1), strings.TrimSuffix(body, "}") + `,"unknown":1}`} {
		if w := f.request("PATCH", invalid, f.csrf); w.Code != 400 {
			t.Fatal("loose risk object accepted", w.Code)
		}
	}
	if _, e := f.s.Pool.Exec(ctx, `CREATE FUNCTION risk177_audit_failure()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.resource='core-payment-risk' THEN RAISE EXCEPTION 'synthetic risk audit failure';END IF;RETURN NEW;END$$;CREATE TRIGGER risk177_audit_failure BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION risk177_audit_failure()`); e != nil {
		t.Fatal(e)
	}
	if w := f.request("PATCH", body, f.csrf); w.Code != 503 {
		t.Fatal("risk audit failure reported saved", w.Code)
	}
	var rows, audits int
	if e := f.s.Pool.QueryRow(ctx, `SELECT(SELECT count(*)FROM core_payment_risk_policy),(SELECT count(*)FROM backoffice_audit WHERE resource='core-payment-risk')`).Scan(&rows, &audits); e != nil || rows != 0 || audits != 0 {
		t.Fatal("risk write/audit not atomic", rows, audits, e)
	}
}
