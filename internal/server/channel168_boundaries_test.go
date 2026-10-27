package server

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"goepay/internal/compat"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

func TestChannel168PolicyDurableSuperAndAtomicAudit(t *testing.T) {
	for _, change := range []string{"demote", "revoke", "expiry", "audit-failure"} {
		t.Run(change, func(t *testing.T) {
			f := channel168Local(t)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			state := channel168Data(t, f.admin("GET", "/7", "", f.csrf))
			body := fmt.Sprintf(`{"rate":"99.50","mode":0,"enabled":true,"expected_fingerprint":%q}`, state["fingerprint"])
			if change == "expiry" || change == "audit-failure" {
				action := "PERFORM pg_sleep(1.3);"
				if change == "audit-failure" {
					action = "RAISE EXCEPTION 'synthetic channel168 audit failure';"
				}
				if _, e := f.p.Exec(ctx, `CREATE SEQUENCE channel168_audit_entered;CREATE FUNCTION channel168_audit_gate() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.operation='runtime-channel-policy-update' THEN PERFORM nextval('channel168_audit_entered');`+action+`END IF;RETURN NEW;END$$;CREATE TRIGGER channel168_audit_gate BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION channel168_audit_gate()`); e != nil {
					t.Fatal(e)
				}
				if change == "expiry" {
					if _, e := f.p.Exec(ctx, `UPDATE console_sessions SET expires_at=clock_timestamp()+interval '1 second' WHERE username='admin168'`); e != nil {
						t.Fatal(e)
					}
				}
				w := f.admin("PATCH", "/7", body, f.csrf)
				want := 503
				if change == "expiry" {
					want = 403
				}
				if w.Code != want {
					t.Fatal("audit/deadline status", w.Code, want)
				}
				var entered bool
				if e := f.p.QueryRow(ctx, "SELECT is_called FROM channel168_audit_entered").Scan(&entered); e != nil || !entered {
					t.Fatal("audit branch not reached", e)
				}
			} else {
				block, e := f.p.Begin(ctx)
				if e != nil {
					t.Fatal(e)
				}
				defer block.Rollback(context.Background())
				if _, e = block.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('goepay/route/channel/7',0))"); e != nil {
					t.Fatal(e)
				}
				done := make(chan *httptest.ResponseRecorder, 1)
				go func() { done <- f.admin("PATCH", "/7", body, f.csrf) }()
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
						t.Fatal("no lock wait", early.Code)
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					case <-time.After(10 * time.Millisecond):
					}
				}
				sql := "DELETE FROM console_sessions WHERE username='admin168'"
				if change == "demote" {
					sql = "UPDATE console_users SET admin_class='operator' WHERE username='admin168'"
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
						t.Fatal("invalid durable actor wrote policy", w.Code)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			var policies, audits int
			if e := f.p.QueryRow(ctx, "SELECT (SELECT count(*) FROM runtime_channel_policies),(SELECT count(*) FROM backoffice_audit WHERE operation='runtime-channel-policy-update')").Scan(&policies, &audits); e != nil || policies != 0 || audits != 0 {
				t.Fatal("policy/audit failed to roll back", policies, audits, e)
			}
		})
	}
}

func TestChannel168RateExtremesRoundingAndDisabledFees(t *testing.T) {
	f := channel168Local(t)
	ctx := context.Background()
	f.group(t, pointer168("0"))
	if _, e := f.p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"payment_fee_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	for i, tc := range []struct {
		rate, money string
		net, fee    int64
	}{{"100.00", "100.00", 10000, 0}, {"", "0.01", 1, 0}, {"0", "0.01", 0, 1}, {"50.00", "0.01", 0, 1}, {"50.00", "0.03", 1, 2}, {"99.50", "100.00", 9950, 50}} {
		f.configure(t, 7, tc.rate)
		no := fmt.Sprintf("bound168-%d", i)
		out := f.pay(t, no, tc.money, 7)
		if out["code"] != float64(1) {
			t.Fatal("boundary HTTP payment", i, out)
		}
		o, e := f.s.Store.Order(ctx, 1, no)
		if e != nil || o.MerchantAmount != tc.net || o.PlatformFee != tc.fee {
			t.Fatal("new104 birth must round the Pro nominal fee first", i, o.MerchantAmount, o.PlatformFee, e)
		}
	}
	if _, e := f.p.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{value}','"false"') WHERE resource='settings' AND body->>'key'='payment_fee_enabled'`); e != nil {
		t.Fatal(e)
	}
	f.configure(t, 7, "0")
	if out := f.pay(t, "disabledfee168", "100.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	o, e := f.s.Store.Order(ctx, 1, "disabledfee168")
	if e != nil || o.MerchantAmount != 10000 || o.PlatformFee != 0 || o.FeePolicy != "none" {
		t.Fatal("disabled fee switch ignored", o.PlatformFee, e)
	}
}

func TestChannel168HistoricalAssignmentFailsClosedAndOriginalOrderIsNotRepriced(t *testing.T) {
	f := channel168Local(t)
	ctx := context.Background()
	old, e := f.s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "old168", Name: "old", Type: "alipay", Amount: 10000})
	if e != nil {
		t.Fatal(e)
	}
	f.configure(t, 7, "90.00")
	if _, e = f.p.Exec(ctx, `INSERT INTO provider_route_assignments(merchant_id,out_trade_no,channel_id,method,pay_type,amount,request_hash,rule_snapshot) VALUES(1,'pending168',7,'native','alipay',100,decode(repeat('00',32),'hex'),'{}')`); e != nil {
		t.Fatal(e)
	}
	p := compat.Params{"type": "alipay", "method": "native", "name": "pending", "money": "1.00", "clientip": "192.0.2.1", "out_trade_no": "pending168", "channel_id": "7"}
	if _, _, e = f.s.providerRoutedRequest(ctx, 1, p); !errors.Is(e, payment.ErrState) {
		t.Fatal("pending historical route accepted latest pricing", e)
	}
	if _, e = f.s.Store.CreateOrder(ctx, create168(p, 100)); !errors.Is(e, payment.ErrState) {
		t.Fatal("historical assignment born without quote", e)
	}
	replay, e := f.s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "old168", Name: "old", Type: "alipay", Amount: 10000})
	if e != nil || old.ID != replay.ID || old.PlatformFee != replay.PlatformFee || old.MerchantAmount != replay.MerchantAmount {
		t.Fatal("old order repriced", e)
	}
	var quotes, terms int
	if e = f.p.QueryRow(ctx, "SELECT (SELECT count(*) FROM provider_route_fee_quotes),(SELECT count(*) FROM order_route_fee_terms)").Scan(&quotes, &terms); e != nil || quotes != 0 || terms != 0 {
		t.Fatal("invented historical pricing facts", quotes, terms, e)
	}
}

func TestChannel168PolicyPermissionAndRuntimeIdentityDrift(t *testing.T) {
	f := channel168Local(t)
	ctx := context.Background()
	f.configure(t, 7, "99.50")
	if _, e := f.p.Exec(ctx, `UPDATE console_users SET admin_class='operator' WHERE username='admin168'`); e != nil {
		t.Fatal(e)
	}
	if w := f.admin("GET", "/7", "", f.csrf); w.Code != 401 {
		t.Fatal("operator read private policy state", w.Code)
	}
	if _, e := f.p.Exec(ctx, `UPDATE console_users SET admin_class='super' WHERE username='admin168'`); e != nil {
		t.Fatal(e)
	}
	var version int64
	if e := f.p.QueryRow(ctx, "SELECT auth_version FROM console_users WHERE username='admin168'").Scan(&version); e != nil {
		t.Fatal(e)
	}
	var e error
	f.token, f.csrf, e = f.s.Sessions.Create(identity.User{ID: 168, Role: "admin", Username: "admin168", AdminClass: "super", AuthVersion: version})
	if e != nil {
		t.Fatal(e)
	}
	f.s.ProviderFlow.Providers[7] = &channel168Provider{app: "different-account168"}
	if w := f.admin("GET", "/7", "", f.csrf); w.Code != 409 {
		t.Fatal("same runtime ID silently rebound receiving account", w.Code)
	}
	if out := f.pay(t, "identitydrift168", "1.00", 7); out["code"] == float64(1) {
		t.Fatal("new receiving account consumed old policy")
	}
	var count int
	if e := f.p.QueryRow(ctx, "SELECT count(*) FROM provider_route_assignments").Scan(&count); e != nil || count != 0 {
		t.Fatal("identity drift left route", count, e)
	}
	// A syntactically super value in memory still cannot impersonate an absent durable actor.
	b, e := f.s.ProviderFlow.RuntimeChannel(8)
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.s.Store.RuntimeChannelPolicies(ctx, []payment.RuntimeChannel{b}, payment.MerchantCredentialActor{User: identity.User{ID: 168, Username: "absent", Role: "admin", AdminClass: "super", AuthVersion: 1}, SessionToken: f.token})
	if !errors.Is(e, identity.ErrAdminSession) {
		t.Fatal("trusted in-memory super without durable user", e)
	}
}

func TestChannel168PolicyFullIDStrictJSONAndTargetBoundCAS(t *testing.T) {
	f := channel168Local(t)
	const id int64 = 9007199254740993
	f.s.ProviderFlow.Providers[id] = &channel168Provider{app: "precise168"}
	state := channel168Data(t, f.admin("GET", "/9007199254740993", "", f.csrf))
	if state["channel_id"] != "9007199254740993" {
		t.Fatal("runtime ID rounded", state["channel_id"])
	}
	other := channel168Data(t, f.admin("GET", "/7", "", f.csrf))
	body := fmt.Sprintf(`{"rate":"99.50","mode":0,"enabled":true,"expected_fingerprint":%q}`, state["fingerprint"])
	cases := []string{`{}`, `null`, `[]`, body + ` {}`, body[:len(body)-1] + `,"enabled":false}`, body[:len(body)-1] + `,"channel_id":"7"}`, `{"rate":99.5,"mode":0,"enabled":true,"expected_fingerprint":"` + state["fingerprint"].(string) + `"}`, `{"rate":null,"mode":0,"enabled":true,"expected_fingerprint":"` + state["fingerprint"].(string) + `"}`}
	for _, raw := range cases {
		if w := f.admin("PATCH", "/9007199254740993", raw, f.csrf); w.Code != 400 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("ambiguous JSON or cacheable error", w.Code)
		}
	}
	foreign := fmt.Sprintf(`{"rate":"99.50","mode":0,"enabled":true,"expected_fingerprint":%q}`, other["fingerprint"])
	if w := f.admin("PATCH", "/9007199254740993", foreign, f.csrf); w.Code != 409 {
		t.Fatal("CAS copied from other channel", w.Code)
	}
	saved := channel168Data(t, f.admin("PATCH", "/9007199254740993", body, f.csrf))
	if saved["channel_id"] != "9007199254740993" || saved["revision"] != "1" {
		t.Fatal("large ID saved on another target", saved)
	}
	unchanged := channel168Data(t, f.admin("GET", "/7", "", f.csrf))
	if unchanged["configured"] != false {
		t.Fatal("large ID changed channel7")
	}
}
