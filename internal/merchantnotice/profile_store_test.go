package merchantnotice_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/identity"
	notice "goepay/internal/merchantnotice"
	"goepay/internal/payment"
	"goepay/internal/server"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func profileStore80(t *testing.T) (*notice.Store, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	var nonce [8]byte
	if _, e = rand.Read(nonce[:]); e != nil {
		t.Fatal(e)
	}
	schema := "notice80_test_" + hex.EncodeToString(nonce[:])
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+quoted); e != nil {
		t.Fatal(e)
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		pool.Close()
		admin.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE")
		admin.Close()
	})
	paths, e := filepath.Glob("../../db/migrations/*.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	for _, path := range paths {
		raw, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, string(raw)); e != nil {
			t.Fatal(filepath.Base(path), e)
		}
	}
	base, e := payment.NewWithKey(pool, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	for _, id := range []int64{1000, 1001} {
		if e = base.CreateMerchant(ctx, payment.Merchant{ID: id, Name: "notice-owner80", APIKey: "synthetic-only80"}); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = pool.Exec(ctx, `UPDATE merchants SET profile=profile||'{"email":"owner80@example.test"}'::jsonb WHERE id=1000`); e != nil {
		t.Fatal(e)
	}
	s, e := notice.NewStore(pool, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	return s, pool
}

func text80(value string) *string { return &value }

var owner80 = notice.Actor{Role: "merchant", ID: 1000, Name: "notice-owner80"}

func TestMerchantNotice80HTTPUsesOriginalOwnerAndProtectedVersion(t *testing.T) {
	s, pool := profileStore80(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES('notice-owner80','merchant',1000,'Local','synthetic-not-a-login'),('notice-other80','merchant',1001,'Other','synthetic-not-a-login')`); err != nil {
		t.Fatal(err)
	}
	sessions := identity.NewSessions(false)
	token, csrf, err := sessions.Create(identity.User{Username: "notice-owner80", Role: "merchant", ID: 1000})
	if err != nil {
		t.Fatal(err)
	}
	app := (&server.Server{Pool: pool, MerchantNotice: s, Sessions: sessions}).Handler()
	request := func(method, path, body, auth string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: auth})
		out := httptest.NewRecorder()
		app.ServeHTTP(out, r)
		return out
	}
	raw := `{"expected_profile_id":"0","msgconfig":{"order":2,"settle":0,"login":0,"complain":0,"mchrisk":0,"balance":0,"group":0,"domain_audit":0,"daily_report":4,"order_money":"10.00","balance_money":"20.00"},"voice_order":false,"targets":{"qqbot_appsecret":"local-synthetic-http-secret80"},"clear_fields":[]}`
	out := request("PUT", "/console/v1/merchant/account/notice", raw, token)
	if out.Code != 200 || strings.Contains(out.Body.String(), "local-synthetic-http-secret80") || !strings.Contains(out.Body.String(), `"order_money":"10.00"`) {
		t.Fatalf("original fields or redaction %d %s", out.Code, out.Body.String())
	}
	if request("PUT", "/console/v1/merchant/account/notice", raw, token).Code != 409 {
		t.Fatal("stale version overwritten")
	}
	if request("GET", "/console/v1/merchant/account/notice?merchant_id=1001", "", token).Code != 400 {
		t.Fatal("tenant override accepted")
	}
	var profiles, audits, jobs int
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM merchant_notice_profiles),(SELECT count(*) FROM backoffice_audit WHERE resource='merchant-notice-profiles'),(SELECT count(*) FROM jobs)`).Scan(&profiles, &audits, &jobs); err != nil || profiles != 1 || audits != 1 || jobs != 0 {
		t.Fatalf("save caused delivery or duplicate %d %d %d %v", profiles, audits, jobs, err)
	}
	out = request("GET", "/console/v1/merchant/account/notice/events?limit=1", "", token)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"items":[]`) || !strings.Contains(out.Body.String(), `"next_cursor":""`) {
		t.Fatal("empty HTTP notice page invalid", out.Code, out.Body.String())
	}
	for i := 0; i < 2; i++ {
		tx, e := pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		var loginID int64
		e = tx.QueryRow(ctx, `INSERT INTO console_login_audit(username,role,ip,success,subject_id,auth_version,source_transaction) VALUES('notice-owner80','merchant','192.0.2.80',true,1000,1,pg_current_xact_id()) RETURNING id`).Scan(&loginID)
		if e == nil {
			e = s.CaptureLoginAudit(ctx, tx, loginID)
		}
		if e == nil {
			e = tx.Commit(ctx)
		} else {
			tx.Rollback(ctx)
		}
		if e != nil {
			t.Fatal(e)
		}
	}
	out = request("GET", "/console/v1/merchant/account/notice/events?limit=1", "", token)
	var page struct {
		Data struct {
			Items      []notice.EventView `json:"items"`
			NextCursor string             `json:"next_cursor"`
		} `json:"data"`
	}
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &page) != nil || len(page.Data.Items) != 1 || page.Data.NextCursor == "" || strings.Contains(out.Body.String(), "192.0.2.80") {
		t.Fatal("first HTTP notice page invalid", out.Code, out.Body.String())
	}
	out = request("GET", "/console/v1/merchant/account/notice/events?limit=1&before_id="+page.Data.NextCursor, "", token)
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &page) != nil || len(page.Data.Items) != 1 || page.Data.Items[0].Scene != "login" {
		t.Fatal("second HTTP notice page invalid", out.Code, out.Body.String())
	}
	other, _, err := sessions.Create(identity.User{Username: "notice-other80", Role: "merchant", ID: 1001})
	if err != nil {
		t.Fatal(err)
	}
	out = request("GET", "/console/v1/merchant/account/notice", "", other)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"id":"0"`) || !strings.Contains(out.Body.String(), `"merchant_id":"1001"`) {
		t.Fatal("owner profiles mixed", out.Code, out.Body.String())
	}
	out = request("GET", "/console/v1/merchant/account/notice/events", "", other)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"items":[]`) {
		t.Fatal("another merchant's HTTP history was visible", out.Code, out.Body.String())
	}
}

func settings80() notice.SettingsWrite {
	return notice.SettingsWrite{Preferences: notice.Preferences{Order: 2, DailyReport: 4, OrderMoney: 1000, BalanceMoney: 2000}, Targets: notice.TargetPatch{MsgRobotURL: text80("https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic-robot-secret80"), QQBotAppID: text80("800001"), QQBotAppSecret: text80("synthetic-qq-secret80"), QQBotOpenID: text80("synthetic-qq-openid80"), TelegramUID: text80("-1000000000080")}}
}

func TestMerchantNotice80ProfileEncryptsOriginalFieldsAndNeverExposesRecipientOrCredential(t *testing.T) {
	s, pool := profileStore80(t)
	ctx := context.Background()
	empty, e := s.Settings(ctx, 1000, owner80)
	if e != nil || empty.ID != 0 {
		t.Fatal(empty, e)
	}
	input := settings80()
	view, e := s.SaveSettings(ctx, 1000, 0, input, owner80)
	if e != nil || view.ID <= 0 || view.Revision != 1 || view.MsgConfig.Order != 2 || view.MsgConfig.OrderMoney != "10.00" || view.MsgConfig.BalanceMoney != "20.00" {
		t.Fatal(view, e)
	}
	encoded, _ := json.Marshal(view)
	var cipher []byte
	var audit string
	if e = pool.QueryRow(ctx, "SELECT payload_cipher FROM merchant_notice_profiles WHERE id=$1", view.ID).Scan(&cipher); e != nil {
		t.Fatal(e)
	}
	if e = pool.QueryRow(ctx, "SELECT string_agg(changes::text,'') FROM backoffice_audit WHERE resource='merchant-notice-profiles'").Scan(&audit); e != nil {
		t.Fatal(e)
	}
	for _, value := range []string{"synthetic-robot-secret80", "synthetic-qq-secret80", "synthetic-qq-openid80", "-1000000000080", "owner80@example.test"} {
		if bytes.Contains(encoded, []byte(value)) || bytes.Contains(cipher, []byte(value)) || strings.Contains(audit, value) {
			t.Fatal("protected notification configuration leaked")
		}
	}
	read, e := s.Settings(ctx, 1000, owner80)
	if e != nil || read.ID != view.ID || !read.Configured["msgrobot_url"] || !read.Configured["qqbot_appsecret"] {
		t.Fatal(read, e)
	}
	unchanged, e := s.SaveSettings(ctx, 1000, view.ID, input, owner80)
	if e != nil || unchanged.ID != view.ID {
		t.Fatal("unchanged settings created a new secret revision", unchanged, e)
	}
	if _, e = s.Settings(ctx, 1001, owner80); !errors.Is(e, notice.ErrForbidden) {
		t.Fatal("merchant crossed configuration owner", e)
	}
	if _, e = s.SaveSettings(ctx, 1001, 0, input, owner80); !errors.Is(e, notice.ErrForbidden) {
		t.Fatal("merchant wrote another owner's targets", e)
	}
}

func TestMerchantNotice80ProfileConcurrentCASAndSecretPatchPreserveExplicitRevision(t *testing.T) {
	s, pool := profileStore80(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	var mu sync.Mutex
	var success []notice.SettingsView
	var failures []error
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := s.SaveSettings(ctx, 1000, 0, settings80(), owner80)
			mu.Lock()
			defer mu.Unlock()
			if e == nil {
				success = append(success, v)
			} else {
				failures = append(failures, e)
			}
		}()
	}
	wg.Wait()
	if len(success) != 1 || len(failures) != 7 {
		t.Fatal("concurrent settings were not one version", len(success), len(failures))
	}
	for _, e := range failures {
		if !errors.Is(e, notice.ErrConflict) {
			t.Fatal(e)
		}
	}
	first := success[0]
	patch := notice.SettingsWrite{Preferences: settings80().Preferences, Targets: notice.TargetPatch{QQBotAppSecret: text80("rotated-synthetic-secret80")}}
	second, e := s.SaveSettings(ctx, 1000, first.ID, patch, owner80)
	if e != nil || second.Revision != 2 || !second.Configured["msgrobot_url"] || !second.Configured["telegram_uid"] {
		t.Fatal("partial secret update erased old targets", second, e)
	}
	if _, e = s.SaveSettings(ctx, 1000, first.ID, settings80(), owner80); !errors.Is(e, notice.ErrConflict) {
		t.Fatal("stale owner form overwrote new credentials", e)
	}
	patch.ClearFields = []string{"msgrobot_url"}
	patch.Targets = notice.TargetPatch{}
	third, e := s.SaveSettings(ctx, 1000, second.ID, patch, owner80)
	if e != nil || third.Revision != 3 || third.Configured["msgrobot_url"] || !third.Configured["qqbot_appsecret"] {
		t.Fatal(third, e)
	}
	var profiles, audits int
	if e = pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM merchant_notice_profiles),(SELECT count(*) FROM backoffice_audit WHERE resource='merchant-notice-profiles')").Scan(&profiles, &audits); e != nil || profiles != 3 || audits != 3 {
		t.Fatal(profiles, audits, e)
	}
}

func TestMerchantNotice80ProfileAuditFailureRollsBackAndBadTargetsPerformNoWrites(t *testing.T) {
	s, pool := profileStore80(t)
	ctx := context.Background()
	for _, mutate := range []func(*notice.SettingsWrite){func(i *notice.SettingsWrite) { i.Preferences.Order = 7 }, func(i *notice.SettingsWrite) { i.Preferences.OrderMoney = -1 }, func(i *notice.SettingsWrite) {
		i.Targets.MsgRobotURL = text80("https://qyapi.weixin.qq.com.evil.invalid/cgi-bin/webhook/send?key=secret")
	}, func(i *notice.SettingsWrite) { i.Targets.TelegramUID = text80("@reassignable_alias80") }, func(i *notice.SettingsWrite) { i.Targets.QQBotAppSecret = text80("secret\r\n") }, func(i *notice.SettingsWrite) { i.ClearFields = []string{"unknown-secret"} }} {
		in := settings80()
		mutate(&in)
		if _, e := s.SaveSettings(ctx, 1000, 0, in, owner80); e == nil {
			t.Fatal("bad protected settings accepted")
		}
	}
	if _, e := pool.Exec(ctx, `CREATE FUNCTION fail_notice80_audit() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.resource='merchant-notice-profiles' THEN RAISE EXCEPTION 'injected notice profile audit failure'; END IF; RETURN NEW; END$$;CREATE TRIGGER fail_notice80_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION fail_notice80_audit()`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.SaveSettings(ctx, 1000, 0, settings80(), owner80); e == nil {
		t.Fatal("missing original audit committed a profile")
	}
	var profiles, heads int
	if e := pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM merchant_notice_profiles),(SELECT count(*) FROM merchant_notice_current)").Scan(&profiles, &heads); e != nil || profiles != 0 || heads != 0 {
		t.Fatal("partial credential configuration survived rollback", profiles, heads, e)
	}
}

func TestMerchantNotice80ProfileLostOrMutatedProofNeverBecomesAnEmptyDefault(t *testing.T) {
	for _, mode := range []string{"cipher", "proof", "audit", "head"} {
		t.Run(mode, func(t *testing.T) {
			s, pool := profileStore80(t)
			ctx := context.Background()
			view, e := s.SaveSettings(ctx, 1000, 0, settings80(), owner80)
			if e != nil {
				t.Fatal(e)
			}
			switch mode {
			case "cipher", "proof":
				if _, e = pool.Exec(ctx, "ALTER TABLE merchant_notice_profiles DISABLE TRIGGER USER"); e != nil {
					t.Fatal(e)
				}
				query := "UPDATE merchant_notice_profiles SET payload_cipher=set_byte(payload_cipher,0,get_byte(payload_cipher,0)#1) WHERE id=$1"
				if mode == "proof" {
					query = "UPDATE merchant_notice_profiles SET profile_hash=decode(repeat('00',32),'hex') WHERE id=$1"
				}
				if _, e = pool.Exec(ctx, query, view.ID); e != nil {
					t.Fatal(e)
				}
			case "audit":
				if _, e = pool.Exec(ctx, "ALTER TABLE backoffice_audit DISABLE TRIGGER USER;UPDATE backoffice_audit SET changes='{}' WHERE resource='merchant-notice-profiles'"); e != nil {
					t.Fatal(e)
				}
			case "head":
				if _, e = pool.Exec(ctx, "ALTER TABLE merchant_notice_current DISABLE TRIGGER USER;DELETE FROM merchant_notice_current"); e != nil {
					t.Fatal(e)
				}
			}
			if _, e = s.Settings(ctx, 1000, owner80); !errors.Is(e, notice.ErrConflict) {
				t.Fatal("corrupt protected source became default preferences", e)
			}
		})
	}
}
