package merchantnotice_test

import (
	"context"
	"goepay/internal/identity"
	notice "goepay/internal/merchantnotice"
	"goepay/internal/server"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMerchantNotice80HTTPMerchantUsesRoleWithoutLegacyAdminClassification(t *testing.T) {
	s, pool := profileStore80(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES('notice-owner80','merchant',1000,'Local','synthetic-only80')`); err != nil {
		t.Fatal(err)
	}
	var class string
	if err := pool.QueryRow(ctx, `SELECT admin_class FROM console_users WHERE username='notice-owner80'`).Scan(&class); err != nil || class != "operator" {
		t.Fatal("original schema default changed", class, err)
	}
	sessions := identity.NewSessions(false)
	token, csrf, err := sessions.Create(identity.User{ID: 1000, Username: "notice-owner80", Role: "merchant", AdminClass: class})
	if err != nil {
		t.Fatal(err)
	}
	app := (&server.Server{Pool: pool, MerchantNotice: s, Sessions: sessions}).Handler()
	for _, tc := range []struct{ method, path, body string }{{"GET", "account/notice", ""}, {"PUT", "account/notice", `{"expected_profile_id":"0","msgconfig":{"order":0,"settle":0,"login":0,"complain":0,"mchrisk":0,"balance":0,"group":0,"domain_audit":0,"daily_report":0,"order_money":"0.00","balance_money":"0.00"},"voice_order":false,"targets":{},"clear_fields":[]}`}, {"GET", "account/notice/events?limit=20", ""}} {
		r := httptest.NewRequest(tc.method, "/console/v1/merchant/"+tc.path, strings.NewReader(tc.body))
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		out := httptest.NewRecorder()
		app.ServeHTTP(out, r)
		if out.Code != 200 {
			t.Fatalf("original merchant incorrectly rejected by unused admin class: %s %d %s", tc.path, out.Code, out.Body.String())
		}
	}
}

func TestMerchantNotice80LoginSourceRequiresFreshSuccessfulAuditAndDoesNotBackfill(t *testing.T) {
	s, pool := profileStore80(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES('notice-owner80','merchant',1000,'Local','synthetic-only80')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveSettings(ctx, 1000, 0, notice.SettingsWrite{Preferences: notice.Preferences{Login: 2}}, owner80); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var id int64
	if err = tx.QueryRow(ctx, `INSERT INTO console_login_audit(username,role,ip,success,subject_id,auth_version,source_transaction) VALUES('notice-owner80','merchant','192.0.2.80',true,1000,1,pg_current_xact_id()) RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err = s.CaptureLoginAudit(ctx, tx, id); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	for _, metadata := range []string{"NULL,1,pg_current_xact_id()", "1000,NULL,pg_current_xact_id()", "1000,1,NULL", "1000,0,pg_current_xact_id()"} {
		if _, err = pool.Exec(ctx, `INSERT INTO console_login_audit(username,role,ip,success,subject_id,auth_version,source_transaction) VALUES('notice-owner80','merchant','192.0.2.80',true,`+metadata+`)`); err == nil {
			t.Fatal("partial login origin accepted", metadata)
		}
	}
	var events, queued int
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM merchant_notice_events WHERE scene='login' AND login_audit_id=$1 AND order_id IS NULL),(SELECT count(*) FROM jobs WHERE kind='merchant.notice')`, id).Scan(&events, &queued); err != nil || events != 1 || queued != 1 {
		t.Fatal("login event missing or misrouted", events, queued, err)
	}
	later, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer later.Rollback(ctx)
	if err = s.CaptureLoginAudit(ctx, later, id); err == nil {
		t.Fatal("historical login adopted as a fresh source")
	}
}

func TestMerchantNotice80SuccessfulHTTPLoginCreatesNoticeAfterAuthenticationOnly(t *testing.T) {
	s, pool := profileStore80(t)
	ctx := context.Background()
	hash, err := identity.HashPassword("local-notice-password80")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES('notice-owner80','merchant',1000,'Local',$1)`, hash); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveSettings(ctx, 1000, 0, notice.SettingsWrite{Preferences: notice.Preferences{Login: 2}}, owner80); err != nil {
		t.Fatal(err)
	}
	app := (&server.Server{Pool: pool, MerchantNotice: s, Sessions: identity.NewSessions(false)}).Handler()
	login := func(password string) int {
		r := httptest.NewRequest("POST", "/console/v1/merchant/login", strings.NewReader(`{"username":"notice-owner80","password":"`+password+`","code":""}`))
		r.RemoteAddr = "192.0.2.80:1234"
		r.Header.Set("Content-Type", "application/json")
		out := httptest.NewRecorder()
		app.ServeHTTP(out, r)
		return out.Code
	}
	if status := login("wrong-local-password80"); status != 401 {
		t.Fatal("failed credentials accepted", status)
	}
	var events int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM merchant_notice_events`).Scan(&events); err != nil || events != 0 {
		t.Fatal("failed login generated notification", events, err)
	}
	if status := login("local-notice-password80"); status != 200 {
		t.Fatal("valid local login rejected", status)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM merchant_notice_events e JOIN console_login_audit a ON a.id=e.login_audit_id WHERE e.scene='login' AND e.merchant_id=1000 AND a.success AND a.subject_id=1000 AND a.ip='192.0.2.80' AND a.source_transaction=e.source_transaction`).Scan(&events); err != nil || events != 1 {
		t.Fatal("successful login lacks original protected source", events, err)
	}
	// An enrolled account whose MFA key is unavailable must still fail closed;
	// knowing the password cannot create another successful-login notice.
	if _, err = pool.Exec(ctx, `INSERT INTO console_mfa(username,role,secret) VALUES('notice-owner80','merchant',decode('0080','hex'))`); err != nil {
		t.Fatal(err)
	}
	if status := login("local-notice-password80"); status != 401 {
		t.Fatal("MFA failure accepted", status)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM merchant_notice_events WHERE scene='login'`).Scan(&events); err != nil || events != 1 {
		t.Fatal("MFA failure generated notice", events, err)
	}
}
