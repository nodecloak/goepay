package server

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"goepay/internal/compat"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

type notificationReturn200Fixture struct {
	channel168Fixture
	merchantToken, merchantCSRF, otherToken, otherCSRF string
}

func notificationReturn200Local(t *testing.T) *notificationReturn200Fixture {
	t.Helper()
	f := &notificationReturn200Fixture{channel168Fixture: channel168Local(t)}
	if err := f.s.Store.CreateMerchant(t.Context(), payment.Merchant{ID: 2, Name: "Other200", APIKey: "synthetic-other200-key"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(t.Context(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES('merchant200','merchant',1,'Merchant200','unused-synthetic-200'),('other200','merchant',2,'Other200','unused-synthetic-200')`); err != nil {
		t.Fatal(err)
	}
	var err error
	f.merchantToken, f.merchantCSRF, err = f.s.Sessions.Create(identity.User{ID: 1, Role: "merchant", Username: "merchant200", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	f.otherToken, f.otherCSRF, err = f.s.Sessions.Create(identity.User{ID: 2, Role: "merchant", Username: "other200", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *notificationReturn200Fixture) order(t *testing.T, name, version, target string, paid bool) payment.Order {
	t.Helper()
	o, err := f.s.Store.CreateOrder(t.Context(), payment.CreateOrderInput{MerchantID: 1, OutTradeNo: name, Version: version, Name: "Original item200", Type: "alipay", Amount: 123, NotifyURL: "https://notify.example.invalid/original200", ReturnURL: target, Param: "original200"})
	if err != nil {
		t.Fatal(err)
	}
	if paid {
		if err = f.s.Store.MarkPaid(t.Context(), o.TradeNo, "synthetic200:"+name); err != nil {
			t.Fatal(err)
		}
	}
	return o
}

func (f *notificationReturn200Fixture) request(role, token, csrf, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/console/v1/"+role+"/orders/"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func notificationReturn200URL(t *testing.T, w *httptest.ResponseRecorder) *url.URL {
	t.Helper()
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("synchronous notification HTTP=%d; a private successful response is required", w.Code)
	}
	var out struct {
		Code int                        `json:"code"`
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.Code != 0 || len(out.Data) != 1 {
		t.Fatal("invalid synchronous notification envelope")
	}
	var raw string
	if json.Unmarshal(out.Data["url"], &raw) != nil || raw == "" {
		t.Fatal("signed return address missing")
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal("invalid signed return address")
	}
	return u
}

func (f *notificationReturn200Fixture) effects(t *testing.T) string {
	t.Helper()
	var out string
	if err := f.p.QueryRow(t.Context(), `SELECT jsonb_build_object('jobs',COALESCE((SELECT jsonb_agg(to_jsonb(j) ORDER BY j.id) FROM jobs j),'[]'::jsonb),'journals',(SELECT count(*) FROM journals),'refunds',(SELECT count(*) FROM refunds),'provider_operations',(SELECT count(*) FROM provider_operations),'notification_controls',(SELECT count(*) FROM notification_send_controls),'resend_requests',(SELECT count(*) FROM notification_resend_requests))::text`).Scan(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestNotificationReturn200ActualHTTPUsesOriginalMD5ReturnWithoutChangingAsyncOrFunds(t *testing.T) {
	f := notificationReturn200Local(t)
	o := f.order(t, "return200-v1", "v1", "https://merchant.example.invalid/return200?tenant=original&money=99.00&sign=old", true)
	before := f.effects(t)
	w := f.request("merchant", f.merchantToken, f.merchantCSRF, "POST", o.TradeNo+"/return-notification", `{}`)
	u := notificationReturn200URL(t, w)
	p, err := compat.Parse(u.Query())
	if err != nil || u.Scheme != "https" || u.Host != "merchant.example.invalid" || u.Path != "/return200" || p["tenant"] != "original" || p["pid"] != "1" || p["trade_no"] != o.TradeNo || p["out_trade_no"] != "return200-v1" || p["money"] != "1.23" || p["name"] != "Original item200" || p["param"] != "original200" || p["trade_status"] != "TRADE_SUCCESS" || p["sign_type"] != "MD5" || !compat.VerifyMD5(p, "test-secret", p["sign"]) {
		t.Fatal("synchronous return changed the original destination, order facts, or MD5 signature")
	}
	if strings.Contains(w.Body.String(), "test-secret") || f.effects(t) != before {
		t.Fatal("synchronous generation leaked key material or changed async delivery/funds")
	}
	var audit string
	if err = f.p.QueryRow(t.Context(), `SELECT COALESCE(jsonb_agg(to_jsonb(a)),'[]'::jsonb)::text FROM backoffice_audit a WHERE resource='orders' AND resource_id=$1 AND operation='return-notification'`, o.ID).Scan(&audit); err != nil || audit == "[]" {
		t.Fatal("synchronous return did not record its actor audit")
	}
	if strings.Contains(audit, p["sign"]) || strings.Contains(audit, "merchant.example.invalid") || strings.Contains(audit, "test-secret") {
		t.Fatal("audit stored a signed bearer address or signing material")
	}
	// Synchronous generation must neither complete nor consume the pending
	// automatic notification. The existing async path still coalesces it.
	w = f.request("merchant", f.merchantToken, f.merchantCSRF, "POST", o.TradeNo+"/resend-notification", `{"external_no":"after-return200"}`)
	if w.Code != 202 {
		t.Fatalf("existing asynchronous resend changed: HTTP=%d", w.Code)
	}
}

func TestNotificationReturn200ActualHTTPRSAUsesStoredV2OrderAndCurrentPlatformKey(t *testing.T) {
	f := notificationReturn200Local(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f.s.PlatformKey = key
	o := f.order(t, "return200-v2", "v2", "https://merchant.example.invalid/v2?tenant=200", true)
	before := f.effects(t)
	u := notificationReturn200URL(t, f.request("admin", f.token, f.csrf, "POST", o.TradeNo+"/return-notification", `{}`))
	p, err := compat.Parse(u.Query())
	if err != nil || p["sign_type"] != "RSA" || p["timestamp"] == "" || p["trade_no"] != o.TradeNo || !compat.VerifyRSAWithProfile(p, p["sign"], &key.PublicKey, compat.ProfileSDKV2) {
		t.Fatal("V2 synchronous return is not compatible with the existing signed notification format")
	}
	if f.effects(t) != before {
		t.Fatal("RSA return generation changed asynchronous tasks or funds")
	}
}

func TestNotificationReturn200ActualHTTPRejectsWrongAuthorityBodyAndDestination(t *testing.T) {
	f := notificationReturn200Local(t)
	paid := f.order(t, "return200-guards", "v1", "https://merchant.example.invalid/return200", true)
	unpaid := f.order(t, "return200-unpaid", "v1", "https://merchant.example.invalid/return200", false)
	missing := f.order(t, "return200-missing", "v1", "", true)
	before := f.effects(t)
	for _, tc := range []struct {
		name, role, token, csrf, method, path, body string
		want                                        int
	}{
		{"foreign merchant", "merchant", f.otherToken, f.otherCSRF, "POST", paid.TradeNo + "/return-notification", `{}`, 404},
		{"missing CSRF", "merchant", f.merchantToken, "", "POST", paid.TradeNo + "/return-notification", `{}`, 403},
		{"unpaid", "merchant", f.merchantToken, f.merchantCSRF, "POST", unpaid.TradeNo + "/return-notification", `{}`, 409},
		{"missing return", "merchant", f.merchantToken, f.merchantCSRF, "POST", missing.TradeNo + "/return-notification", `{}`, 409},
		{"read method", "merchant", f.merchantToken, f.merchantCSRF, "GET", paid.TradeNo + "/return-notification", ``, 405},
		{"caller destination", "merchant", f.merchantToken, f.merchantCSRF, "POST", paid.TradeNo + "/return-notification", `{"url":"https://changed.example.invalid/"}`, 400},
		{"caller order", "merchant", f.merchantToken, f.merchantCSRF, "POST", paid.TradeNo + "/return-notification", `{"trade_no":"another-order"}`, 400},
		{"duplicate JSON", "merchant", f.merchantToken, f.merchantCSRF, "POST", paid.TradeNo + "/return-notification", `{"url":"one","url":"two"}`, 400},
		{"JSON null", "merchant", f.merchantToken, f.merchantCSRF, "POST", paid.TradeNo + "/return-notification", `null`, 400},
		{"trailing JSON", "merchant", f.merchantToken, f.merchantCSRF, "POST", paid.TradeNo + "/return-notification", `{} {}`, 400},
		{"query destination", "merchant", f.merchantToken, f.merchantCSRF, "POST", paid.TradeNo + "/return-notification?url=https%3A%2F%2Fchanged.example.invalid", `{}`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := f.request(tc.role, tc.token, tc.csrf, tc.method, tc.path, tc.body)
			if w.Code != tc.want || strings.Contains(w.Body.String(), `"url"`) || strings.Contains(w.Body.String(), "sign=") {
				t.Fatalf("rejected synchronous return HTTP=%d want=%d or leaked signed material", w.Code, tc.want)
			}
		})
	}
	for _, target := range []string{"http://127.0.0.1/private200", "http://169.254.169.254/metadata200", "https://user:password@example.com/return200", "javascript:alert(200)"} {
		// Simulate a historical imported destination, bypassing create validation.
		if _, err := f.p.Exec(t.Context(), "UPDATE orders SET return_url=$1 WHERE id=$2", target, paid.ID); err != nil {
			t.Fatal(err)
		}
		w := f.request("admin", f.token, f.csrf, "POST", paid.TradeNo+"/return-notification", `{}`)
		if w.Code != 409 || strings.Contains(w.Body.String(), `"url"`) {
			t.Fatalf("unsafe stored return destination was exposed: HTTP=%d", w.Code)
		}
	}
	if f.effects(t) != before {
		t.Fatal("rejected synchronous return changed delivery or funds")
	}
}

func TestNotificationReturn200ActualHTTPDetailReportsActualIndependentCapability(t *testing.T) {
	f := notificationReturn200Local(t)
	paid := f.order(t, "return200-capable", "v1", "https://merchant.example.invalid/return200", true)
	if _, err := f.p.Exec(t.Context(), "UPDATE orders SET notify_url='' WHERE id=$1", paid.ID); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, version, target string
		paid, allowed         bool
	}{
		{"capable", "v1", "https://merchant.example.invalid/return200", true, true},
		{"unpaid-cap", "v1", "https://merchant.example.invalid/return200", false, false},
		{"missing-cap", "v1", "", true, false},
		{"missing-rsa-cap", "v2", "https://merchant.example.invalid/return200", true, false},
	} {
		o := paid
		if tc.name != "capable" {
			o = f.order(t, "return200-"+tc.name, tc.version, tc.target, tc.paid)
		}
		w := f.request("merchant", f.merchantToken, f.merchantCSRF, "GET", o.TradeNo, "")
		var out struct {
			Data map[string]json.RawMessage `json:"data"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("order detail unavailable: HTTP=%d", w.Code)
		}
		var allowed bool
		value, exists := out.Data["can_return_notification"]
		if !exists || json.Unmarshal(value, &allowed) != nil || allowed != tc.allowed {
			t.Errorf("%s detail synchronous capability missing or inaccurate", tc.name)
		}
		if tc.name == "capable" && string(out.Data["can_resend_notification"]) != "false" {
			t.Fatal("synchronous capability was incorrectly coupled to async notification URL")
		}
	}
}

func TestNotificationReturn200ActualHTTPPreservesOperatorAccessAndRejectsRevokedPersistentActor(t *testing.T) {
	f := notificationReturn200Local(t)
	o := f.order(t, "return200-session", "v1", "https://merchant.example.invalid/return200", true)
	if _, err := f.p.Exec(t.Context(), "UPDATE console_users SET admin_class='operator' WHERE username='admin168'"); err != nil {
		t.Fatal(err)
	}
	// Class changes revoke the prior authority epoch. Reauthenticate the
	// operator instead of weakening the production original-session check.
	stale := f.request("admin", f.token, f.csrf, "POST", o.TradeNo+"/return-notification", `{}`)
	if stale.Code != 401 {
		t.Fatalf("old super session survived its class/epoch change: HTTP=%d", stale.Code)
	}
	var version int64
	if err := f.p.QueryRow(t.Context(), "SELECT auth_version FROM console_users WHERE username='admin168' AND role='admin'").Scan(&version); err != nil {
		t.Fatal(err)
	}
	var err error
	f.token, f.csrf, err = f.s.Sessions.Create(identity.User{ID: 168, Role: "admin", Username: "admin168", AdminClass: "operator", AuthVersion: version})
	if err != nil {
		t.Fatal(err)
	}
	notificationReturn200URL(t, f.request("admin", f.token, f.csrf, "POST", o.TradeNo+"/return-notification", `{}`))
	if _, err := f.p.Exec(t.Context(), "UPDATE console_sessions SET expires_at=$1 WHERE username='merchant200'", time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	w := f.request("merchant", f.merchantToken, f.merchantCSRF, "POST", o.TradeNo+"/return-notification", `{}`)
	if w.Code != 401 || strings.Contains(w.Body.String(), `"url"`) {
		t.Fatalf("expired persistent session received a signed return: HTTP=%d", w.Code)
	}
}

func TestNotificationReturn200ActualHTTPAuditFailureCannotReleaseSignedURL(t *testing.T) {
	f := notificationReturn200Local(t)
	o := f.order(t, "return200-audit", "v1", "https://merchant.example.invalid/return200", true)
	before := f.effects(t)
	if _, err := f.p.Exec(t.Context(), `CREATE FUNCTION reject_return200_audit() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.operation='return-notification' THEN RAISE EXCEPTION 'synthetic200 audit rejection'; END IF; RETURN NEW; END$$;CREATE TRIGGER reject_return200_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION reject_return200_audit()`); err != nil {
		t.Fatal(err)
	}
	w := f.request("admin", f.token, f.csrf, "POST", o.TradeNo+"/return-notification", `{}`)
	if w.Code < 500 || strings.Contains(w.Body.String(), `"url"`) || strings.Contains(w.Body.String(), "sign=") || f.effects(t) != before {
		t.Fatalf("failed audit exposed a return URL or changed delivery/funds: HTTP=%d", w.Code)
	}
}

func TestNotificationReturn200ActualHTTPFinalDeadlineIsCheckedAfterAuditWait(t *testing.T) {
	f := notificationReturn200Local(t)
	o := f.order(t, "return200-late-expiry", "v1", "https://merchant.example.invalid/return200", true)
	before := f.effects(t)
	if _, err := f.p.Exec(t.Context(), `CREATE SEQUENCE return200_audit_entered;CREATE FUNCTION wait_return200_audit() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.operation='return-notification' THEN PERFORM nextval('return200_audit_entered'); PERFORM pg_sleep(1.2); END IF; RETURN NEW; END$$;CREATE TRIGGER wait_return200_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION wait_return200_audit();UPDATE console_sessions SET expires_at=clock_timestamp()+interval '1 second' WHERE username='merchant200'`); err != nil {
		t.Fatal(err)
	}
	w := f.request("merchant", f.merchantToken, f.merchantCSRF, "POST", o.TradeNo+"/return-notification", `{}`)
	if w.Code != 403 || strings.Contains(w.Body.String(), `"url"`) || f.effects(t) != before {
		t.Fatalf("expired session received a signed URL after audit wait: HTTP=%d", w.Code)
	}
	var entered bool
	if err := f.p.QueryRow(t.Context(), "SELECT is_called FROM return200_audit_entered").Scan(&entered); err != nil || !entered {
		t.Fatal("final deadline test did not reach the waiting audit")
	}
	var audits int
	if err := f.p.QueryRow(t.Context(), "SELECT count(*) FROM backoffice_audit WHERE operation='return-notification'").Scan(&audits); err != nil || audits != 0 {
		t.Fatal("expired session committed a successful return audit")
	}
}

func TestNotificationReturn200ActualHTTPDelegatedOrMemoryOnlySessionCannotGenerateReturn(t *testing.T) {
	f := notificationReturn200Local(t)
	o := f.order(t, "return200-original-session", "v1", "https://merchant.example.invalid/return200", true)
	tx, err := f.p.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	delegated, csrf, _, err := f.s.Sessions.CreateDelegated(t.Context(), tx,
		identity.User{ID: 1, Role: "merchant", Username: "merchant200", AuthVersion: 1},
		identity.User{ID: 168, Role: "admin", Username: "admin168", AdminClass: "super", AuthVersion: 1}, f.token)
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	w := f.request("merchant", delegated, csrf, "POST", o.TradeNo+"/return-notification", `{}`)
	if w.Code != 403 || strings.Contains(w.Body.String(), `"url"`) {
		t.Fatalf("delegated actor generated an original-session return: HTTP=%d", w.Code)
	}
	f.s.Sessions = identity.NewSessions(false)
	token, csrf, err := f.s.Sessions.Create(identity.User{ID: 1, Role: "merchant", Username: "merchant200", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	w = f.request("merchant", token, csrf, "POST", o.TradeNo+"/return-notification", `{}`)
	if w.Code != 503 || strings.Contains(w.Body.String(), `"url"`) {
		t.Fatalf("memory-only session generated a persistent-session return: HTTP=%d", w.Code)
	}
}
