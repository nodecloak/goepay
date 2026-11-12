package contract_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"goepay/internal/identity"
	"goepay/internal/payment"
)

func TestAdminFundsRechecksAuthorityAfterBusinessLockWait(t *testing.T) {
	for _, mode := range []string{"permissions-grant", "permissions-off", "logout-off", "disabled-off", "demoted-off", "unchanged-off", "memory-logout-off"} {
		t.Run(mode, func(t *testing.T) {
			s, pool, _ := fixture(t)
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			class, permissions := "operator", `["InternalTransfer"]`
			if mode == "demoted-off" {
				class, permissions = "super", `null`
			}
			if _, err := pool.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class,permissions) SELECT 'funds-authority','admin',8,'Authority',password_hash,$1,NULLIF($2::jsonb,'null'::jsonb) FROM console_users WHERE username='admin'`, class, permissions); err != nil {
				t.Fatal(err)
			}
			s.Sessions = identity.NewPersistentSessions(pool, false)
			if mode == "memory-logout-off" {
				s.Sessions = identity.NewSessions(false)
			}
			user := identity.User{Username: "funds-authority", Role: "admin", ID: 8, AuthVersion: 1, AdminClass: class}
			token, csrf, err := s.Sessions.Create(user)
			if err != nil {
				t.Fatal(err)
			}
			s.FundsActionResolver = s.ResolveFundsAction
			order, err := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "authority-funding", Name: "local", Type: "sandbox", Amount: 1000})
			if err != nil {
				t.Fatal(err)
			}
			if err = s.Store.MarkPaid(ctx, order.TradeNo, "authority-funding"); err != nil {
				t.Fatal(err)
			}
			const target = "/console/v1/admin/workflows/internal-transfers"
			const body = `{"merchant_id":"1","recipient_id":"2","amount":"1.25","external_no":"authority-wait"}`
			grant := ""
			if mode == "permissions-grant" {
				if _, err = pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"admin_funds_authorization_enabled","value":"true"}')`); err != nil {
					t.Fatal(err)
				}
				funds := identity.NewFundsAuthorization(pool, nil)
				actor := identity.FundsActor{User: user, SessionToken: token, ClientIP: "192.0.2.1"}
				if err = funds.SetPassword(ctx, actor, "contract-password-123", "", "authority-payment-password", ""); err != nil {
					t.Fatal(err)
				}
				action, _, err := s.ResolveFundsAction(ctx, pool, target, []byte(body))
				if err != nil {
					t.Fatal(err)
				}
				challenge, err := funds.Prepare(ctx, actor, action)
				if err != nil {
					t.Fatal(err)
				}
				authorized, err := funds.Authorize(ctx, actor, challenge.ChallengeID, "authority-payment-password")
				if err != nil {
					t.Fatal(err)
				}
				grant = authorized.Authorization
			}
			block, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer block.Rollback(context.Background())
			if _, err = block.Exec(ctx, "SELECT id FROM merchants WHERE id=1 FOR UPDATE"); err != nil {
				t.Fatal(err)
			}
			pid := int64(block.Conn().PgConn().PID())
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				r := httptest.NewRequest("POST", target, strings.NewReader(body)).WithContext(ctx)
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("X-CSRF-Token", csrf)
				r.Header.Set("X-Payment-Authorization", grant)
				r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
				w := httptest.NewRecorder()
				s.Handler().ServeHTTP(w, r)
				done <- w
			}()
			for {
				var waiting bool
				if err = pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1::integer=ANY(pg_blocking_pids(pid)))", pid).Scan(&waiting); err != nil {
					t.Fatal(err)
				}
				if waiting {
					break
				}
				select {
				case w := <-done:
					t.Fatalf("request did not wait for merchant lock: %d %s", w.Code, w.Body.String())
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-time.After(10 * time.Millisecond):
				}
			}
			switch mode {
			case "permissions-grant", "permissions-off":
				_, err = pool.Exec(ctx, `UPDATE console_users SET permissions='[]' WHERE username='funds-authority'`)
			case "logout-off", "memory-logout-off":
				s.Sessions.Delete(token)
			case "disabled-off":
				_, err = pool.Exec(ctx, `UPDATE console_users SET enabled=false WHERE username='funds-authority'`)
			case "demoted-off":
				_, err = pool.Exec(ctx, `UPDATE console_users SET admin_class='operator',permissions='[]' WHERE username='funds-authority'`)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = block.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			var result *httptest.ResponseRecorder
			select {
			case result = <-done:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			wantHTTP, wantCount, wantReserved := 403, 0, int64(0)
			if mode == "unchanged-off" {
				wantHTTP, wantCount, wantReserved = 201, 1, 125
			}
			if result.Code != wantHTTP {
				t.Errorf("authority after lock wait: HTTP %d want %d: %s", result.Code, wantHTTP, result.Body.String())
			}
			var count, consumed int
			if err = pool.QueryRow(ctx, "SELECT count(*) FROM ledger_requests").Scan(&count); err != nil || count != wantCount {
				t.Errorf("new funds intents=%d want %d err=%v", count, wantCount, err)
			}
			balance, err := s.Store.Balance(ctx, 1)
			if err != nil || balance.Reserved != wantReserved || balance.Available != 1000-wantReserved {
				t.Errorf("revoked request changed balance: %+v err=%v", balance, err)
			}
			if err = pool.QueryRow(ctx, "SELECT count(*) FROM admin_funds_grants WHERE consumed_at IS NOT NULL").Scan(&consumed); err != nil || consumed != 0 {
				t.Errorf("rejected request consumed grant: %d err=%v", consumed, err)
			}
		})
	}
}

func TestMenuPermissionChangesInvalidatePersistentFundsAuthority(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := t.Context()
	s.Sessions = identity.NewPersistentSessions(pool, false)
	if _, err := pool.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class,permissions) SELECT 'menu-epoch','admin',8,'Epoch',password_hash,'operator','["InternalTransfer"]'::jsonb FROM console_users WHERE username='admin'`); err != nil {
		t.Fatal(err)
	}
	user := identity.User{Username: "menu-epoch", Role: "admin", ID: 8, AuthVersion: 1, AdminClass: "operator"}
	token, _, err := s.Sessions.Create(user)
	if err != nil {
		t.Fatal(err)
	}
	actor := identity.FundsActor{User: user, SessionToken: token, ClientIP: "192.0.2.1"}
	funds := identity.NewFundsAuthorization(pool, nil)
	if err = funds.SetPassword(ctx, actor, "contract-password-123", "", "authority-payment-password", ""); err != nil {
		t.Fatal(err)
	}
	action, err := identity.NewFundsAction("admin.internal_transfer", "synthetic-transfer", map[string]string{"amount": "125"})
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := funds.Prepare(ctx, actor, action)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := funds.Authorize(ctx, actor, challenge.ChallengeID, "authority-payment-password")
	if err != nil {
		t.Fatal(err)
	}
	for i, permissions := range []string{`[]`, `["InternalTransfer"]`} {
		if _, err = pool.Exec(ctx, "UPDATE console_users SET permissions=$1::jsonb WHERE username='menu-epoch'", permissions); err != nil {
			t.Fatal(err)
		}
		var version int64
		if err = pool.QueryRow(ctx, "SELECT auth_version FROM console_users WHERE username='menu-epoch'").Scan(&version); err != nil || version != int64(i+2) {
			t.Errorf("permission change %d did not advance epoch: version=%d err=%v", i, version, err)
		}
		if _, valid := s.Sessions.Get(token, "admin"); valid {
			t.Error("menu permission change retained existing session")
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		err = funds.ConsumeRequiredTx(ctx, tx, actor, grant.Authorization, action, fmt.Sprintf("synthetic-operation-%d", i))
		if !errors.Is(err, identity.ErrFundsAuthentication) {
			t.Errorf("old funds grant survived permission change/restore: %v", err)
		}
		tx.Rollback(ctx)
	}
	if _, err = pool.Exec(ctx, `UPDATE console_users SET permissions='["InternalTransfer"]' WHERE username='menu-epoch'`); err != nil {
		t.Fatal(err)
	}
	var version int64
	if err = pool.QueryRow(ctx, "SELECT auth_version FROM console_users WHERE username='menu-epoch'").Scan(&version); err != nil || version != 3 {
		t.Errorf("identical menu save changed epoch: %d err=%v", version, err)
	}
}
