package contract_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/db"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

// Stage only frozen schemas1..62, then use db.Migrate for the real upgrade.
// No current trigger is disabled to manufacture a historical source.
func incomeAdoptionHTTPMigrate62(ctx context.Context, p *pgxpool.Pool) error {
	if _, err := p.Exec(ctx, `CREATE TABLE schema_migrations(name text PRIMARY KEY,sha256 text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	files, err := filepath.Glob("../../db/migrations/*.up.sql")
	if err != nil {
		return err
	}
	for _, file := range files {
		if filepath.Base(file) >= "000063_" {
			continue
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		if _, err = p.Exec(ctx, string(raw)); err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		if _, err = p.Exec(ctx, `INSERT INTO schema_migrations(name,sha256) VALUES($1,$2)`, filepath.Base(file), fmt.Sprintf("%x", sha256.Sum256(raw))); err != nil {
			return err
		}
	}
	return nil
}

func incomeAdoptionHTTPSeedHold(t *testing.T, p *pgxpool.Pool, no string) (payment.Order, int64) {
	t.Helper()
	ctx := context.Background()
	tx, err := p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	// Reproduce the original schema62 writer with all historical guards active.
	// Today's Store requires current migrations and is used only after upgrade.
	var latest string
	if err = tx.QueryRow(ctx, `SELECT max(name) FROM schema_migrations`).Scan(&latest); err != nil || !strings.HasPrefix(latest, "000062_") {
		t.Fatal("historical fixture requires exactly schema62", latest, err)
	}
	o := payment.Order{MerchantID: 1, OutTradeNo: no, Name: "local pre63 income", Amount: 100, MerchantAmount: 100, Version: "v1", FeePolicy: "none", RefundFeePayer: "platform", Status: "created"}
	if err = tx.QueryRow(ctx, `INSERT INTO orders(trade_no,merchant_id,out_trade_no,name,type,amount,merchant_amount)VALUES(nextval('trade_no_seq')::text,1,$1,$2,'',100,100)RETURNING id,trade_no,created_at`, no, o.Name).Scan(&o.ID, &o.TradeNo, &o.CreatedAt); err != nil {
		t.Fatal(err)
	}
	const refundPolicy = "any_successful_refund_voids_reward"
	raw, err := json.Marshal([]any{o.ID, o.TradeNo, o.MerchantID, o.OutTradeNo, o.Amount, o.MerchantAmount, o.PlatformFee, o.FeeRateBPS, o.FeeGroupID, o.FeePolicy, o.RefundFeePayer, int64(0), int64(0), "payment", false, "no_relation", int64(0), int64(0), int64(0), refundPolicy, [5]int64{}})
	if err != nil {
		t.Fatal(err)
	}
	// The shared HTTP fixture uses the explicit all-zero synthetic master key.
	index := hmac.New(sha256.New, make([]byte, 32))
	index.Write([]byte("goepay/transfer-policy-index/v1"))
	seal := hmac.New(sha256.New, index.Sum(nil))
	seal.Write([]byte("goepay/referral/order-terms/v1\x00"))
	seal.Write(raw)
	if _, err = tx.Exec(ctx, `INSERT INTO referral_order_terms(order_id,merchant_id,purpose,enabled,reason,rate_bps,platform_fee,reward_amount,refund_policy,policy_versions,proof_hash)VALUES($1,1,'payment',false,'no_relation',0,0,0,$2,'[0,0,0,0,0]',$3)`, o.ID, refundPolicy, seal.Sum(nil)); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO inbox(provider,event_id,order_id)VALUES('payment',$1,$2)`, "local-paid-"+no, o.ID); err != nil {
		t.Fatal(err)
	}
	var paymentFrom, paymentTo, paymentJournal int64
	if err = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE owner_id=0 AND kind='clearing' AND scope_id=0`).Scan(&paymentFrom); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE owner_id=1 AND kind='available' AND scope_id=0`).Scan(&paymentTo); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM accounts WHERE id=ANY($1::bigint[]) ORDER BY id FOR UPDATE`, []int64{paymentFrom, paymentTo}); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `INSERT INTO journals(event_key,description)VALUES($1,'payment net credit')RETURNING id`, "payment:"+o.TradeNo).Scan(&paymentJournal); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE accounts SET balance=balance+CASE WHEN id=$1 THEN -100 WHEN id=$2 THEN 100 ELSE 0 END WHERE id=ANY($3::bigint[])`, paymentFrom, paymentTo, []int64{paymentFrom, paymentTo}); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO ledger_entries(journal_id,account_id,amount)VALUES($1,$2,-100),($1,$3,100)`, paymentJournal, paymentFrom, paymentTo); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE orders SET status='paid',paid_at=statement_timestamp(),provider_event_id=$2 WHERE id=$1`, o.ID, "local-paid-"+no); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO outbox(event_key,kind,payload)VALUES($1,'payment.paid',jsonb_build_object('trade_no',$2::text,'merchant_id',1))`, "payment:"+o.TradeNo, o.TradeNo); err != nil {
		t.Fatal(err)
	}
	var hid, from, to, jid int64
	if _, err = tx.Exec(ctx, `INSERT INTO accounts(owner_id,kind) VALUES(1,'order_income_hold') ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `INSERT INTO order_income_holds(order_id,merchant_id,request_no,merchant_net,refund_merchant_amount,amount) VALUES($1,1,$2,100,0,100) RETURNING id`, o.ID, "legacy-"+no).Scan(&hid); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE owner_id=1 AND kind='available' AND scope_id=0`).Scan(&from); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE owner_id=1 AND kind='order_income_hold' AND scope_id=0`).Scan(&to); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `INSERT INTO journals(event_key,description) VALUES($1,'genuine local pre63 freeze') RETURNING id`, fmt.Sprintf("order-income:%d:freeze", hid)).Scan(&jid); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE accounts SET balance=balance-100 WHERE id=$1`, from); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE accounts SET balance=balance+100 WHERE id=$1`, to); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO ledger_entries(journal_id,account_id,amount) VALUES($1,$2,-100),($1,$3,100)`, jid, from, to); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO order_income_hold_events(hold_id,action,actor_id,actor_name,actor_class) VALUES($1,'freeze',1,'admin','super')`, hid); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',1,'admin','orders',$1,'freeze-income',jsonb_build_object('hold_id',$2::bigint::text,'amount','100'))`, o.ID, hid); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return o, hid
}

func TestIncomeAdoptionHTTPPersistentSuperMandatoryGrantAndStrictOwnership(t *testing.T) {
	s, p, _ := fixtureWithMigration(t, incomeAdoptionHTTPMigrate62)
	ctx := context.Background()
	o, hid := incomeAdoptionHTTPSeedHold(t, p, "http-adopt-original")
	_, other := incomeAdoptionHTTPSeedHold(t, p, "http-adopt-other")
	if err := db.Migrate(ctx, p); err != nil {
		t.Fatal("formal pre63 upgrade", err)
	}
	s.Sessions = identity.NewPersistentSessions(p, false)
	s.FundsActionResolver = s.ResolveFundsAction
	u := identity.User{Username: "admin", Role: "admin", ID: 1, AuthVersion: 1, AdminClass: "super"}
	admin, csrf, err := s.Sessions.Create(u)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) VALUES('adopt-operator','admin',82,'Operator','unused','operator')`); err != nil {
		t.Fatal(err)
	}
	operator, operatorCSRF, err := s.Sessions.Create(identity.User{Username: "adopt-operator", Role: "admin", ID: 82, AuthVersion: 1, AdminClass: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	merchant, merchantCSRF, err := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1, AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	// Retain Issuer metadata in a real persisted super session to exercise the
	// original-admin check independently of the simpler merchant role rejection.
	issued, issuedCSRF, err := s.Sessions.Create(u)
	if err != nil {
		t.Fatal(err)
	}
	issuedHash, issuerHash := sha256.Sum256([]byte(issued)), sha256.Sum256([]byte(admin))
	if _, err = p.Exec(ctx, `INSERT INTO console_session_issuers(token_hash,issuer_session_hash,issuer_username,issuer_id,issuer_auth_version) VALUES($1,$2,'admin',1,1)`, issuedHash[:], issuerHash[:]); err != nil {
		t.Fatal(err)
	}
	if session, ok := s.Sessions.Get(issued, "admin"); !ok || session.Issuer == nil {
		t.Fatal("fixture did not preserve verified persistent Issuer")
	}
	call := func(role, token, csrfValue, path, body, grant string) (int, map[string]any) {
		t.Helper()
		r := httptest.NewRequest("POST", "/console/v1/"+role+path, strings.NewReader(body))
		r.RemoteAddr = "192.0.2.63:1234"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "http://localhost")
		r.Header.Set("X-CSRF-Token", csrfValue)
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
		if grant != "" {
			r.Header.Set("X-Payment-Authorization", grant)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		var v map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatal(w.Code, w.Body.String(), err)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("financial response cacheable")
		}
		return w.Code, v
	}
	target := "/orders/" + o.TradeNo + "/adopt-income-hold"
	body := fmt.Sprintf(`{"hold_id":"%d","external_no":"http-adopt"}`, hid)
	if enforced, err := identity.FundsEnforcedTx(ctx, p); err != nil || enforced {
		t.Fatal("test must keep optional global protection off", enforced, err)
	}
	for _, actor := range []struct{ name, role, token, csrf string }{{"operator", "admin", operator, operatorCSRF}, {"merchant", "merchant", merchant, merchantCSRF}, {"issuer", "admin", issued, issuedCSRF}, {"csrf", "admin", admin, ""}} {
		if code, v := call(actor.role, actor.token, actor.csrf, target, body, ""); code != 403 {
			t.Fatalf("%s reached adoption: %d %v", actor.name, code, v)
		}
	}
	for _, bad := range []string{`{}`, fmt.Sprintf(`{"hold_id":%d,"external_no":"http-adopt"}`, hid), fmt.Sprintf(`{"hold_id":"0%d","external_no":"http-adopt"}`, hid), fmt.Sprintf(`{"hold_id":"%d","hold_id":"%d","external_no":"http-adopt"}`, hid, hid), fmt.Sprintf(`{"hold_id":"%d","external_no":"http-adopt","amount":"1.00"}`, hid)} {
		if code, v := call("admin", admin, csrf, target, bad, ""); code != 400 && code != 422 {
			t.Fatalf("ambiguous body reached adoption: %d %v", code, v)
		}
	}
	if code, v := call("admin", admin, csrf, target, body, ""); code != 428 {
		t.Fatal("global-off adoption bypassed mandatory grant", code, v)
	}
	wrongOriginal := fmt.Sprintf(`{"hold_id":"%d","external_no":"http-adopt"}`, other)
	if code, v := call("admin", admin, csrf, target, wrongOriginal, ""); code != 404 {
		t.Fatal("hold from another original order reached funding adoption", code, v)
	}
	var count int
	if err = p.QueryRow(ctx, `SELECT count(*) FROM order_income_hold_fundings`).Scan(&count); err != nil || count != 0 {
		t.Fatal("unauthorized source was created", count, err)
	}
	funds := identity.NewFundsAuthorization(p, nil)
	if err = funds.SetPassword(ctx, identity.FundsActor{User: u, SessionToken: admin, ClientIP: "192.0.2.63"}, "contract-password-123", "", "local-adopt-funds-password-456", ""); err != nil {
		t.Fatal(err)
	}
	authorize := func() string {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"target": "/console/v1/admin" + target, "body": json.RawMessage(body)})
		code, v := call("admin", admin, csrf, "/security/funds-authorizations/challenges", string(raw), "")
		if code != 201 {
			t.Fatal("prepare real funds challenge", code, v)
		}
		data := v["data"].(map[string]any)
		summary := data["summary"].(map[string]any)
		if summary["income_hold_id"] != fmt.Sprint(hid) || summary["held_original"] != "1.00" || summary["income_hold_source_mode"] != "legacy_unprepared" {
			t.Fatal("authorization lacks concrete original funding", summary)
		}
		confirmation, _ := json.Marshal(map[string]any{"challenge_id": data["challenge_id"], "payment_password": "local-adopt-funds-password-456"})
		code, v = call("admin", admin, csrf, "/security/funds-authorizations/confirm", string(confirmation), "")
		if code != 200 {
			t.Fatal("authorize real funds grant", code, v)
		}
		return v["data"].(map[string]any)["authorization"].(string)
	}
	stale := authorize()
	if _, err = p.Exec(ctx, `UPDATE merchants SET profile=jsonb_set(profile,'{review_policy_revision}','"changed"') WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if code, v := call("admin", admin, csrf, target, body, stale); code != 428 {
		t.Fatal("stale confirmed snapshot adopted source", code, v)
	}
	grant := authorize()
	changed := strings.Replace(body, "http-adopt", "switched-adopt", 1)
	if code, v := call("admin", admin, csrf, target, changed, grant); code != 428 {
		t.Fatal("changed body adopted original grant", code, v)
	}
	if code, v := call("admin", admin, csrf, target, body, grant); code != 200 || v["data"].(map[string]any)["source_mode"] != "legacy_adopt" {
		t.Fatal("authorized adoption failed", code, v)
	}
	if code, v := call("admin", admin, csrf, target, body, ""); code != 200 {
		t.Fatal("lost response replay consumed new authority", code, v)
	}
	var consumed, journals, remaining, otherSources int
	if err = p.QueryRow(ctx, `SELECT (SELECT count(*) FROM admin_funds_grants WHERE action_kind='admin.income_hold_adopt' AND consumed_at IS NOT NULL),(SELECT count(*) FROM journals WHERE event_key=$1),(SELECT balance FROM accounts WHERE owner_id=1 AND kind='order_income_hold' AND scope_id=0),(SELECT count(*) FROM order_income_hold_fundings WHERE hold_id=$2)`, fmt.Sprintf("order-income:%d:adopt", hid), other).Scan(&consumed, &journals, &remaining, &otherSources); err != nil || consumed != 1 || journals != 1 || remaining != 100 || otherSources != 0 {
		t.Fatal("adoption duplicated authority or touched other original source", consumed, journals, remaining, otherSources, err)
	}
	report, err := s.Store.CheckLedger(ctx)
	if err != nil || report.InvalidOrderIncomeFundings != 0 || report.InvalidRefundFundings != 0 || report.InvalidReferralTerms != 0 || report.InvalidReferralRewards != 0 || report.InvalidReferralClawbacks != 0 || report.InvalidAccounts != 0 || report.InvalidJournals != 0 {
		t.Fatal(report, err)
	}
}
