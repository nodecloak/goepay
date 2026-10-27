package contract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"goepay/internal/identity"
	"goepay/internal/payment"
)

func personal172State(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("personal settings HTTP=%d want200", w.Code)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("personal settings cacheable")
	}
	v := credentialData162(t, w)
	if len(v) != 7 || v["merchant_id"] != "2" || v["can_update"] != true {
		t.Fatal("personal DTO identity/shape", v)
	}
	for _, key := range []string{"key", "api_key", "rsa_public_key", "profile", "email", "phone"} {
		if _, ok := v[key]; ok {
			t.Fatal("unrelated/private projection", key)
		}
	}
	if fp, ok := v["fingerprint"].(string); !ok || len(fp) != 64 {
		t.Fatal("personal fingerprint missing")
	}
	return v
}
func personal172Patch(fp, qq, min, max string) string {
	b, _ := json.Marshal(map[string]string{"qq": qq, "pay_minmoney": min, "pay_maxmoney": max, "expected_fingerprint": fp})
	return string(b)
}
func TestPersonal172OwnFieldsCASPreservesOtherProfileAndCredentials(t *testing.T) {
	f := signatureFixture(t, 2)
	ctx := context.Background()
	if _, e := f.p.Exec(ctx, `UPDATE merchants SET profile=profile||'{"qq":"123456","pay_minmoney":"1","pay_maxmoney":"2.00","unrelated":{"proof":"keep172"}}'::jsonb WHERE id=2`); e != nil {
		t.Fatal(e)
	}
	before, e := f.s.Store.Merchant(ctx, 2)
	if e != nil {
		t.Fatal(e)
	}
	var original string
	var revision int64
	if e = f.p.QueryRow(ctx, `SELECT (profile-'qq'-'pay_minmoney'-'pay_maxmoney')::text,config_revision FROM merchants WHERE id=2`).Scan(&original, &revision); e != nil {
		t.Fatal(e)
	}
	state := personal172State(t, f.request("GET", "personal-settings", "", f.csrf))
	if state["qq"] != "123456" || state["pay_minmoney"] != "1.00" || state["pay_maxmoney"] != "2.00" || state["bounds_valid"] != true {
		t.Fatal("original personal fields", state)
	}
	body := personal172Patch(state["fingerprint"].(string), "7654321", "1.01", "")
	updated := personal172State(t, f.request("PATCH", "personal-settings", body, f.csrf))
	if updated["qq"] != "7654321" || updated["pay_minmoney"] != "1.01" || updated["pay_maxmoney"] != "" || updated["fingerprint"] == state["fingerprint"] {
		t.Fatal("personal fields not saved", updated)
	}
	if w := f.request("PATCH", "personal-settings", body, f.csrf); w.Code != 409 {
		t.Fatal("stale CAS accepted", w.Code)
	}
	var after string
	var afterRevision int64
	if e = f.p.QueryRow(ctx, `SELECT (profile-'qq'-'pay_minmoney'-'pay_maxmoney')::text,config_revision FROM merchants WHERE id=2`).Scan(&after, &afterRevision); e != nil || after != original || afterRevision != revision+1 {
		t.Fatal("unrelated original profile/revision changed", e)
	}
	merchant, e := f.s.Store.Merchant(ctx, 2)
	if e != nil || merchant.APIKey != before.APIKey || merchant.RSAPublicKey != before.RSAPublicKey || merchant.SignatureAlgorithm != before.SignatureAlgorithm {
		t.Fatal("personal edit changed credentials", e)
	}
}
func TestPersonal172StrictFieldsBoundsAndCSRF(t *testing.T) {
	f := signatureFixture(t, 2)
	state := personal172State(t, f.request("GET", "personal-settings", "", f.csrf))
	fp := state["fingerprint"].(string)
	valid := personal172Patch(fp, "123456", "1.00", "2.00")
	if w := f.request("PATCH", "personal-settings", valid, "wrong"); w.Code != 403 {
		t.Fatal("invalid CSRF accepted", w.Code)
	}
	for _, path := range []string{"personal-settings?merchant_id=1", "personal-settings?", "personal-settings/2"} {
		w := f.request("GET", path, "", f.csrf)
		if w.Code != 400 && w.Code != 404 {
			t.Fatal("target override accepted", path, w.Code)
		}
	}
	for _, body := range []string{strings.TrimSuffix(valid, "}") + `,"qq":"999999"}`, strings.TrimSuffix(valid, "}") + `,"merchant_id":"1"}`, `{"qq":123456,"pay_minmoney":"","pay_maxmoney":"","expected_fingerprint":"` + fp + `"}`, `{"qq":"123456","pay_minmoney":"","pay_maxmoney":"","Expected_fingerprint":"` + fp + `"}`} {
		if w := f.request("PATCH", "personal-settings", body, f.csrf); w.Code != 400 {
			t.Fatal("non-exact JSON accepted", w.Code)
		}
	}
	for _, v := range [][3]string{{"1234", "", ""}, {"1e5", "", ""}, {"123456", "-1", ""}, {"123456", "1e2", ""}, {"123456", "1.001", ""}, {"123456", "92233720368547758.08", ""}, {"123456", "3.00", "2.00"}} {
		if w := f.request("PATCH", "personal-settings", personal172Patch(fp, v[0], v[1], v[2]), f.csrf); w.Code != 422 {
			t.Fatal("invalid personal value accepted", v, w.Code)
		}
	}
	personal172State(t, f.request("PATCH", "personal-settings", personal172Patch(fp, "", "0.00", ""), f.csrf))
}
func TestPersonal172SameFingerprintHasOneWinner(t *testing.T) {
	f := signatureFixture(t, 2)
	state := personal172State(t, f.request("GET", "personal-settings", "", f.csrf))
	body := personal172Patch(state["fingerprint"].(string), "123456", "1", "")
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); codes <- f.request("PATCH", "personal-settings", body, f.csrf).Code }()
	}
	wg.Wait()
	close(codes)
	success, conflict := 0, 0
	for code := range codes {
		if code == 200 {
			success++
		} else if code == 409 {
			conflict++
		} else {
			t.Fatal("concurrent policy unexpected status", code)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("personal CAS lost uniqueness", success, conflict)
	}
}
func TestPersonal172SavedBoundsAreConsumedAndOriginalOrderReplayRemains(t *testing.T) {
	f := signatureFixture(t, 2)
	ctx := context.Background()
	in := payment.CreateOrderInput{MerchantID: 2, OutTradeNo: "personal172-original", Name: "original172", Type: "alipay", Amount: 250}
	old, e := f.s.Store.CreateOrder(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	state := personal172State(t, f.request("GET", "personal-settings", "", f.csrf))
	personal172State(t, f.request("PATCH", "personal-settings", personal172Patch(state["fingerprint"].(string), "", "1.01", "2.00"), f.csrf))
	for _, amount := range []int64{100, 201} {
		in.OutTradeNo = fmt.Sprintf("personal172-rejected-%d", amount)
		in.Amount = amount
		if _, e = f.s.Store.CreateOrder(ctx, in); e == nil {
			t.Fatal("saved personal limit not consumed", amount)
		}
	}
	for _, amount := range []int64{101, 200} {
		in.OutTradeNo = fmt.Sprintf("personal172-allowed-%d", amount)
		in.Amount = amount
		if _, e = f.s.Store.CreateOrder(ctx, in); e != nil {
			t.Fatal("inclusive personal limit", amount, e)
		}
	}
	in.OutTradeNo = old.OutTradeNo
	in.Amount = 250
	replay, e := f.s.Store.CreateOrder(ctx, in)
	if e != nil || replay.ID != old.ID || replay.PlatformFee != old.PlatformFee || replay.MerchantAmount != old.MerchantAmount {
		t.Fatal("personal edit repriced original order", e)
	}
	if _, e = f.p.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"pay_minmoney":"1.50","pay_maxmoney":"1.75"}'::jsonb WHERE resource='groups' AND id=0`); e != nil {
		t.Fatal(e)
	}
	for _, amount := range []int64{149, 176} {
		in.OutTradeNo = fmt.Sprintf("personal172-group-rejected-%d", amount)
		in.Amount = amount
		if _, e = f.s.Store.CreateOrder(ctx, in); e == nil {
			t.Fatal("wider personal bounds bypassed effective group", amount)
		}
	}
	for _, amount := range []int64{150, 175} {
		in.OutTradeNo = fmt.Sprintf("personal172-group-allowed-%d", amount)
		in.Amount = amount
		if _, e = f.s.Store.CreateOrder(ctx, in); e != nil {
			t.Fatal("effective group boundary changed", amount, e)
		}
	}
	in.OutTradeNo = old.OutTradeNo
	in.Amount = 250
	replay, e = f.s.Store.CreateOrder(ctx, in)
	if e != nil || replay.ID != old.ID || replay.PlatformFee != old.PlatformFee || replay.MerchantAmount != old.MerchantAmount {
		t.Fatal("later group change repriced original order", e)
	}
}

func personal172Digest(t *testing.T, f signatureFixture163) string {
	t.Helper()
	var digest string
	if e := f.p.QueryRow(context.Background(), `SELECT md5((SELECT to_jsonb(m)::text FROM merchants m WHERE id=2)||COALESCE((SELECT jsonb_agg(to_jsonb(a) ORDER BY id)::text FROM backoffice_audit a WHERE operation='personal-settings-update'),''))`).Scan(&digest); e != nil {
		t.Fatal(e)
	}
	return digest
}
func TestPersonal172FinalWriteExpiryAndAuditFailureRollback(t *testing.T) {
	for _, mode := range []string{"expiry", "audit-failure"} {
		t.Run(mode, func(t *testing.T) {
			f := signatureFixture(t, 2)
			ctx := context.Background()
			state := personal172State(t, f.request("GET", "personal-settings", "", f.csrf))
			before := personal172Digest(t, f)
			action := "PERFORM pg_sleep(1.3);"
			if mode == "audit-failure" {
				action = "RAISE EXCEPTION 'synthetic personal172 audit failure';"
			}
			if _, e := f.p.Exec(ctx, `CREATE SEQUENCE personal172_gate_entered;CREATE FUNCTION personal172_gate() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.operation='personal-settings-update' THEN PERFORM nextval('personal172_gate_entered');`+action+`END IF;RETURN NEW;END$$;CREATE TRIGGER personal172_gate BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION personal172_gate()`); e != nil {
				t.Fatal(e)
			}
			if mode == "expiry" {
				if _, e := f.p.Exec(ctx, "UPDATE console_sessions SET expires_at=clock_timestamp()+interval '1 second' WHERE username='merchant'"); e != nil {
					t.Fatal(e)
				}
			}
			w := f.request("PATCH", "personal-settings", personal172Patch(state["fingerprint"].(string), "123456", "1", "2"), f.csrf)
			want := 503
			if mode == "expiry" {
				want = 403
			}
			if w.Code != want {
				t.Fatal("last-write policy committed after authority/audit failure", mode, w.Code, want)
			}
			var entered bool
			if e := f.p.QueryRow(ctx, "SELECT is_called FROM personal172_gate_entered").Scan(&entered); e != nil || !entered {
				t.Fatal("last-write gate not reached", e)
			}
			if personal172Digest(t, f) != before {
				t.Fatal("rejected personal settings changed original row or audit")
			}
		})
	}
}
func TestPersonal172RevocationDuringMerchantLockWaitRejectsCommit(t *testing.T) {
	f := signatureFixture(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	state := personal172State(t, f.request("GET", "personal-settings", "", f.csrf))
	before := personal172Digest(t, f)
	block, e := f.p.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer block.Rollback(context.Background())
	if _, e = block.Exec(ctx, "SELECT id FROM merchants WHERE id=2 FOR UPDATE"); e != nil {
		t.Fatal(e)
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- f.request("PATCH", "personal-settings", personal172Patch(state["fingerprint"].(string), "123456", "1", "2"), f.csrf)
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
		case result := <-done:
			t.Fatal("request did not wait", result.Code)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	if _, e = f.p.Exec(ctx, "DELETE FROM console_sessions WHERE username='merchant'"); e != nil {
		t.Fatal(e)
	}
	if e = block.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case result := <-done:
		if result.Code != 403 {
			t.Fatal("revoked session committed personal policy", result.Code)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if personal172Digest(t, f) != before {
		t.Fatal("revoked session altered original personal facts")
	}
}
func TestPersonal172DelegationCannotReadOrChangeOwnerSettings(t *testing.T) {
	f := signatureFixture(t, 2)
	ctx := context.Background()
	var av, mv int64
	if e := f.p.QueryRow(ctx, "SELECT auth_version FROM console_users WHERE username='admin'").Scan(&av); e != nil {
		t.Fatal(e)
	}
	if e := f.p.QueryRow(ctx, "SELECT auth_version FROM console_users WHERE username='merchant'").Scan(&mv); e != nil {
		t.Fatal(e)
	}
	issuer := identity.User{ID: 1, Role: "admin", Username: "admin", AuthVersion: av, AdminClass: "super"}
	issuerToken, _, e := f.s.Sessions.Create(issuer)
	if e != nil {
		t.Fatal(e)
	}
	tx, e := f.p.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	token, csrf, _, e := f.s.Sessions.CreateDelegated(ctx, tx, identity.User{ID: 2, Role: "merchant", Username: "merchant", AuthVersion: mv}, issuer, issuerToken)
	if e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	f.token, f.csrf = token, csrf
	for _, method := range []string{"GET", "PATCH"} {
		if w := f.request(method, "personal-settings", personal172Patch(strings.Repeat("a", 64), "123456", "", ""), csrf); w.Code != 403 {
			t.Fatal("delegated owner capability exposed", method, w.Code)
		}
	}
}
