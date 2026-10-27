package contract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"goepay/internal/identity"
)

func TestGlobalAccess179AdminAuthorizedRefundRemainsAllowed(t *testing.T) {
	f, p, o := merchant174Runtime(t)
	c := runtimeAdmin179(t, f)
	ctx := context.Background()
	globalSetting179(t, f, "user_refund", "0")
	merchant174Set(t, f, 1, 0, 0)
	user := identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super", AuthVersion: 1}
	auth := identity.NewFundsAuthorization(f.p, nil)
	if e := auth.SetPassword(ctx, identity.FundsActor{User: user, SessionToken: c.token, ClientIP: "192.0.2.1"}, "contract-password-123", "", "local-authorization179", ""); e != nil {
		t.Fatal(e)
	}
	globalSetting179(t, f, "admin_funds_authorization_enabled", "true")
	f.s.FundsActionResolver = f.s.ResolveFundsAction
	target := "/console/v1/admin/orders/" + o.TradeNo + "/refund"
	body := `{"amount":"1.00","external_no":"authorized179"}`
	request := func(path, raw, grant string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", path, strings.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", c.csrf)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: c.token})
		if grant != "" {
			r.Header.Set("X-Payment-Authorization", grant)
		}
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		return w
	}
	if w := request(target, body, ""); w.Code != 428 || p.refundWrites.Load() != 0 {
		t.Fatal("admin authorization was bypassed", w.Code, p.refundWrites.Load())
	}
	preview, _ := json.Marshal(map[string]any{"target": target, "body": json.RawMessage(body)})
	w := request("/console/v1/admin/security/funds-authorizations/challenges", string(preview), "")
	if w.Code != 201 {
		t.Fatal("original admin challenge failed", w.Code, w.Body.String())
	}
	var challenge struct{ Data identity.FundsChallenge }
	if e := json.Unmarshal(w.Body.Bytes(), &challenge); e != nil {
		t.Fatal(e)
	}
	w = request("/console/v1/admin/security/funds-authorizations/confirm", fmt.Sprintf(`{"challenge_id":%q,"payment_password":"local-authorization179"}`, challenge.Data.ChallengeID), "")
	if w.Code != 200 {
		t.Fatal("original authorization confirmation failed", w.Code, w.Body.String())
	}
	var grant struct{ Data identity.FundsGrant }
	if e := json.Unmarshal(w.Body.Bytes(), &grant); e != nil {
		t.Fatal(e)
	}
	w = request(target, body, grant.Data.Authorization)
	if w.Code != 200 && w.Code != 202 || p.refundWrites.Load() != 1 {
		t.Fatal("platform merchant gate blocked separately authorized administrator", w.Code, p.refundWrites.Load(), w.Body.String())
	}
}

func TestGlobalAccess179FirstAndExistingConfigurationUseOriginalSettingsLock(t *testing.T) {
	for _, configured := range []bool{false, true} {
		t.Run(fmt.Sprint(configured), func(t *testing.T) {
			c := adminCredentialsFixture162(t)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			state := platformState179(t, platformAdmin179(c, "GET", "settings/merchant-access", "", c.csrf))
			if configured {
				state = platformState179(t, platformAdmin179(c, "PATCH", "settings/merchant-access", platformPatch179(state["fingerprint"].(string), "0", "1"), c.csrf))
			}
			conn, e := c.pool.Acquire(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer conn.Release()
			tx, e := conn.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer tx.Rollback(context.Background())
			if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7170140001);INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"site_name","value":"unrelated settings commit179"}')`); e != nil {
				t.Fatal(e)
			}
			result := make(chan int, 1)
			go func() {
				result <- platformAdmin179(c, "PATCH", "settings/merchant-access", platformPatch179(state["fingerprint"].(string), "1", "0"), c.csrf).Code
			}()
			for {
				var waiting bool
				if e = c.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1::int=ANY(pg_blocking_pids(pid)))`, int(conn.Conn().PgConn().PID())).Scan(&waiting); e != nil {
					t.Fatal(e)
				}
				if waiting {
					break
				}
				select {
				case code := <-result:
					t.Fatal("configuration bypassed original settings lock", code)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-time.After(10 * time.Millisecond):
				}
			}
			if e = tx.Commit(ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case code := <-result:
				if code != 200 {
					t.Fatal("serialized original settings update falsely conflicted or deadlocked", code)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			after := platformState179(t, platformAdmin179(c, "GET", "settings/merchant-access", "", c.csrf))
			if after["close_keylogin"] != "1" || after["user_refund"] != "0" {
				t.Fatal(after)
			}
		})
	}
}

func TestGlobalAccess179DurableSuperDowngradeDisableAndForeignSessionReject(t *testing.T) {
	for _, change := range []string{"operator", "disabled", "merchant-cookie"} {
		t.Run(change, func(t *testing.T) {
			c := adminCredentialsFixture162(t)
			state := platformState179(t, platformAdmin179(c, "GET", "settings/merchant-access", "", c.csrf))
			ctx := context.Background()
			switch change {
			case "operator":
				if _, e := c.pool.Exec(ctx, `UPDATE console_users SET admin_class='operator' WHERE username='admin' AND role='admin'`); e != nil {
					t.Fatal(e)
				}
			case "disabled":
				if _, e := c.pool.Exec(ctx, `UPDATE console_users SET enabled=false WHERE username='admin' AND role='admin'`); e != nil {
					t.Fatal(e)
				}
			case "merchant-cookie":
				var e error
				c.token, c.csrf, e = c.s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant", AuthVersion: 1})
				if e != nil {
					t.Fatal(e)
				}
			}
			w := platformAdmin179(c, "PATCH", "settings/merchant-access", platformPatch179(state["fingerprint"].(string), "1", "0"), c.csrf)
			if w.Code != 401 && w.Code != 403 {
				t.Fatal("non-super original actor wrote global policy", change, w.Code)
			}
			var n int
			if e := c.pool.QueryRow(ctx, `SELECT count(*) FROM backoffice_records WHERE resource='settings' AND body->>'key' IN('close_keylogin','user_refund','__platform_merchant_access179')`).Scan(&n); e != nil || n != 0 {
				t.Fatal("rejected actor left state", n, e)
			}
		})
	}
}
