package backoffice

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/db"
	"goepay/internal/payment"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
)

func setup(t *testing.T) *Service {
	t.Helper()
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	p, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	n := make([]byte, 8)
	rand.Read(n)
	schema := "bo_" + hex.EncodeToString(n)
	if _, e = p.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	cfg, _ := pgxpool.ParseConfig(dsn)
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	scoped, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { scoped.Close(); p.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); p.Close() })
	if e = db.Migrate(ctx, scoped); e != nil {
		t.Fatal(e)
	}
	store, e := payment.NewWithKey(scoped, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	for _, id := range []int64{101, 102} {
		if e = store.CreateMerchant(ctx, payment.Merchant{ID: id, Name: "test", APIKey: "test-secret-long"}); e != nil {
			t.Fatal(e)
		}
	}
	return New(scoped, store)
}
func input(raw string) map[string]json.RawMessage {
	var v map[string]json.RawMessage
	json.Unmarshal([]byte(raw), &v)
	return v
}

var admin = Principal{Role: "admin", SubjectID: 1, Username: "admin"}

func TestTenantCRUDAndAudit(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	merchant := Principal{Role: "merchant", SubjectID: 101, Username: "m1"}
	other := Principal{Role: "merchant", SubjectID: 102, Username: "m2"}
	item, e := s.Create(ctx, merchant, "domains", input(`{"domain":"shop.example.com"}`))
	if e != nil {
		t.Fatal(e)
	}
	id := item.ID
	if item.Fields["merchant_id"] != "101" || item.Fields["approved"] != false {
		t.Fatalf("tenant fields %+v", item)
	}
	if _, e = s.Update(ctx, merchant, "domains", id, input(`{"approved":true}`)); !errors.Is(e, ErrForbidden) {
		t.Fatalf("merchant approved domain: %v", e)
	}
	if _, e = s.Get(ctx, other, "domains", id); !errors.Is(e, ErrNotFound) {
		t.Fatalf("other tenant read %v", e)
	}
	if e = s.Delete(ctx, other, "domains", id); !errors.Is(e, ErrNotFound) {
		t.Fatalf("other tenant delete %v", e)
	}
	updated, e := s.Update(ctx, admin, "domains", id, input(`{"approved":true}`))
	if e != nil || updated.Fields["approved"] != true {
		t.Fatalf("approval %+v %v", updated, e)
	}
	page, e := s.List(ctx, merchant, "domains", Query{Page: 1, Limit: 10, Search: "shop"})
	if e != nil || page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("list %+v %v", page, e)
	}
	if e = s.Delete(ctx, merchant, "domains", id); e != nil {
		t.Fatal(e)
	}
	var count int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM backoffice_audit WHERE resource='domains'").Scan(&count); e != nil || count != 3 {
		t.Fatalf("audit count=%d %v", count, e)
	}
	if _, e = s.pool.Exec(ctx, "DELETE FROM backoffice_audit"); e == nil {
		t.Fatal("audit mutable")
	}
}
func TestResourceValidationAndRestrictions(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	for _, tc := range []struct{ r, body string }{{"settings", `{"key":"database_password","value":"oops"}`}, {"groups", `{"name":"g","balance":"999"}`}, {"groups", `{"name":"g","rate_bps":"10001"}`}, {"blacklist", `{"kind":"ip","value":"bad-ip"}`}, {"collection-styles", `{"name":"x","background_color":"<script>"}`}} {
		if _, e := s.Create(ctx, admin, tc.r, input(tc.body)); e == nil {
			t.Errorf("invalid %s accepted", tc.r)
		}
	}
	if _, e := s.Create(ctx, Principal{Role: "merchant", SubjectID: 101}, "groups", input(`{"name":"x"}`)); !errors.Is(e, ErrForbidden) {
		t.Errorf("merchant admin write %v", e)
	}
	if _, e := s.List(ctx, admin, "orders; DROP TABLE merchants", Query{}); !errors.Is(e, ErrNotFound) {
		t.Errorf("arbitraryresource %v", e)
	}
}
func TestMerchantAndAdministratorCRUDSecrets(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	merchant, e := s.Create(ctx, admin, "merchants", input(`{"id":"103","name":"new merchant","api_key":"test-created-merchant-api-key","signature_algorithm":"MD5","email":"shop@example.com"}`))
	if e != nil {
		t.Fatal(e)
	}
	if merchant.Fields["api_key"] != nil {
		t.Fatal("API key exposed")
	}
	m, e := s.store.Merchant(ctx, 103)
	if e != nil || m.APIKey != "test-created-merchant-api-key" {
		t.Fatalf("secret persistence %v", e)
	}
	if _, e = s.Update(ctx, admin, "merchants", 103, input(`{"enabled":false}`)); e != nil {
		t.Fatal(e)
	}
	m, e = s.store.Merchant(ctx, 103)
	if e != nil || m.Enabled {
		t.Fatalf("disabled %+v %v", m, e)
	}
	u, e := s.Create(ctx, admin, "admin-users", input(`{"username":"operator","display_name":"Operator","password":"sufficient-test-password"}`))
	if e != nil {
		t.Fatal(e)
	}
	if u.Fields["password"] != nil {
		t.Fatal("password exposed")
	}
	var hash string
	if e = s.pool.QueryRow(ctx, "SELECT password_hash FROM console_users WHERE username='operator'").Scan(&hash); e != nil || hash == "sufficient-test-password" {
		t.Fatalf("password not hashed %v", e)
	}
	if _, e = s.Update(ctx, Principal{Role: "admin", SubjectID: 10000, Username: "operator"}, "admin-users", u.ID, input(`{"enabled":false}`)); !errors.Is(e, ErrForbidden) {
		t.Fatalf("self disable %v", e)
	}
	var leaked bool
	if e = s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE changes::text LIKE '%sufficient-test-password%' OR changes::text LIKE '%test-created-merchant-api-key%')").Scan(&leaked); e != nil || leaked {
		t.Fatalf("audit leaked secrets %v", e)
	}
}
func TestRiskPoliciesAndComplaintOwnership(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	merchant := Principal{Role: "merchant", SubjectID: 101, Username: "m1"}
	if e := s.CheckPayment(ctx, PaymentContext{MerchantID: 101, ClientIP: "192.0.2.1", NotifyURL: "https://shop.example.com/hook"}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Create(ctx, admin, "blacklist", input(`{"kind":"ip","value":"192.0.2.0/24","reason":"test","enabled":true}`)); e != nil {
		t.Fatal(e)
	}
	if e := s.CheckPayment(ctx, PaymentContext{MerchantID: 101, ClientIP: "192.0.2.1"}); !errors.Is(e, ErrForbidden) {
		t.Fatalf("blacklist ineffective %v", e)
	}
	domain, e := s.Create(ctx, merchant, "domains", input(`{"domain":"shop.example.com"}`))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Update(ctx, admin, "domains", domain.ID, input(`{"approved":true}`)); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Create(ctx, admin, "settings", input(`{"key":"enforce_domains","value":"true"}`)); e != nil {
		t.Fatal(e)
	}
	if e = s.CheckPayment(ctx, PaymentContext{MerchantID: 101, ClientIP: "198.51.100.1", NotifyURL: "https://shop.example.com/hook"}); e != nil {
		t.Fatal(e)
	}
	if e = s.CheckPayment(ctx, PaymentContext{MerchantID: 101, NotifyURL: "https://shop.example.com.attacker.invalid/hook"}); !errors.Is(e, ErrForbidden) {
		t.Fatalf("domain suffix bypass %v", e)
	}
	other, e := s.store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 102, OutTradeNo: "other", Name: "goods", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	payload := fmt.Sprintf(`{"trade_no":%q,"subject":"support","content":"details","pay_type":"sandbox"}`, other.TradeNo)
	if _, e = s.Create(ctx, merchant, "complaints", input(payload)); !errors.Is(e, ErrInvalid) {
		t.Fatalf("cross tenant complaint %v", e)
	}
	if _, e = s.Create(ctx, merchant, "merchant-risk", input(`{"reason":"unblock me"}`)); !errors.Is(e, ErrForbidden) {
		t.Fatalf("merchant modified risk %v", e)
	}
}
func TestProfitRulesBudgetAndOrderAllocation(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	merchant := Principal{Role: "merchant", SubjectID: 101, Username: "m1"}
	rule, e := s.Create(ctx, merchant, "profitsharing-rules", input(`{"name":"share","recipient_id":"102","rate_bps":"6000"}`))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Create(ctx, merchant, "profitsharing-rules", input(`{"name":"over","recipient_id":"102","rate_bps":"6000"}`)); !errors.Is(e, ErrInvalid) {
		t.Fatalf("rule budget exceeded %v", e)
	}
	o, e := s.store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 101, OutTradeNo: "share", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.store.MarkPaid(ctx, o.TradeNo, "share-fund"); e != nil {
		t.Fatal(e)
	}
	v, e := s.store.ExecuteProfitRule(ctx, rule.ID, o.TradeNo, "merchant:101")
	if e != nil || v.Amount != 600 {
		t.Fatalf("share %+v %v", v, e)
	}
	same, e := s.store.ExecuteProfitRule(ctx, rule.ID, o.TradeNo, "merchant:101")
	if e != nil || same.ID != v.ID {
		t.Fatalf("share duplicate %+v %v", same, e)
	}
	if _, e = s.store.ReviewFundsRequest(ctx, v.ID, true, "admin:1"); e != nil {
		t.Fatal(e)
	}
	b, _ := s.store.Balance(ctx, 102)
	if b.Available != 600 {
		t.Fatalf("recipient %+v", b)
	}
	if _, e = s.Update(ctx, merchant, "profitsharing-rules", rule.ID, input(`{"enabled":false}`)); e != nil {
		t.Fatal(e)
	}
	second, e := s.Create(ctx, merchant, "profitsharing-rules", input(`{"name":"changed policy","recipient_id":"102","rate_bps":"6000"}`))
	if e != nil {
		t.Fatal(e)
	}
	// A different paid order makes the wallet more than sufficient for this
	// request. It must not enlarge the original order's retained source budget.
	independent, e := s.store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 101, OutTradeNo: "independent-income", Name: "other goods", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.store.MarkPaid(ctx, independent.TradeNo, "independent-income-paid"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.store.ExecuteProfitRule(ctx, second.ID, o.TradeNo, "merchant:101"); !errors.Is(e, payment.ErrInsufficient) {
		t.Fatalf("changed policy exceeded the original retained income: %v", e)
	}
	owner, e := s.store.Balance(ctx, 101)
	if e != nil || owner.Available != 1400 || owner.Reserved != 0 {
		t.Fatalf("rejected allocation changed owner funds: %+v %v", owner, e)
	}
	recipient, e := s.store.Balance(ctx, 102)
	if e != nil || recipient.Available != 600 || recipient.Reserved != 0 {
		t.Fatalf("rejected allocation changed recipient funds: %+v %v", recipient, e)
	}
	var requests, allocations, journals, entries int64
	if e = s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM ledger_requests),(SELECT count(*) FROM order_profit_allocations),(SELECT count(*) FROM journals),(SELECT count(*) FROM ledger_entries)").Scan(&requests, &allocations, &journals, &entries); e != nil || requests != 1 || allocations != 1 || journals != 4 || entries != 8 {
		t.Fatalf("rejected allocation persisted financial side effects: requests=%d allocations=%d journals=%d entries=%d error=%v", requests, allocations, journals, entries, e)
	}
	for trade, want := range map[string]int64{o.TradeNo: 400, independent.TradeNo: 1000} {
		amount, err := s.store.OrderIncomeFreezable(ctx, 101, trade)
		if err != nil || amount != want {
			t.Fatalf("original order source changed: amount=%d want=%d error=%v", amount, want, err)
		}
	}
	report, e := s.store.CheckLedger(ctx)
	if e != nil || report.InvalidAccounts != 0 || report.InvalidJournals != 0 {
		t.Fatalf("allocation ledger is invalid: %+v %v", report, e)
	}
}
func TestCaseCommentsImmutableAndTenantSafe(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	merchant := Principal{Role: "merchant", SubjectID: 101, Username: "m1"}
	o, e := s.store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 101, OutTradeNo: "complaint", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	item, e := s.Create(ctx, merchant, "complaints", input(fmt.Sprintf(`{"trade_no":%q,"subject":"support","content":"details","pay_type":"sandbox"}`, o.TradeNo)))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.appendCaseComment(ctx, merchant, "complaints", item.ID, "first reply"); e != nil {
		t.Fatal(e)
	}
	if e = s.appendCaseComment(ctx, merchant, "complaints", item.ID, "second reply"); e != nil {
		t.Fatal(e)
	}
	if e = s.appendCaseComment(ctx, Principal{Role: "merchant", SubjectID: 102}, "complaints", item.ID, "cross tenant"); !errors.Is(e, ErrNotFound) {
		t.Fatalf("cross tenant reply %v", e)
	}
	var count int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM case_comments WHERE record_id=$1", item.ID).Scan(&count); e != nil || count != 2 {
		t.Fatalf("history count%d %v", count, e)
	}
	if _, e = s.pool.Exec(ctx, "UPDATE case_comments SET content='rewrite'"); e == nil {
		t.Fatal("case history mutable")
	}
	if e = s.caseState(ctx, merchant, "complaints", item.ID, "closed"); !errors.Is(e, ErrForbidden) {
		t.Fatalf("merchant closed case %v", e)
	}
}
func TestBusinessHTTPCreatesRechargeWithoutCreditUntilPaid(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	merchant := Principal{Role: "merchant", SubjectID: 101, Username: "m1"}
	calls := 0
	s.RechargeStart = func(_ context.Context, mid int64, trade string, params map[string]string) (string, error) {
		calls++
		if mid != 101 || params["type"] != "wxpay" {
			t.Error("wrong recharge context")
		}
		return "/test-checkout/" + trade, nil
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/workflows/recharges", strings.NewReader(`{"amount":"1.23","external_no":"r1","type":"wxpay"}`))
	s.ServeWorkflows(w, r, merchant, []string{"recharges"})
	if w.Code != 201 {
		t.Fatalf("recharge HTTP %d %s", w.Code, w.Body)
	}
	var out struct {
		Data map[string]any `json:"data"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	trade, _ := out.Data["trade_no"].(string)
	if trade == "" || calls != 1 {
		t.Fatalf("recharge data %+v", out)
	}
	b, _ := s.store.Balance(ctx, 101)
	if b.Available != 0 {
		t.Fatal("unpaid recharge credited")
	}
	if e := s.store.MarkPaid(ctx, trade, "recharge-verified"); e != nil {
		t.Fatal(e)
	}
	b, _ = s.store.Balance(ctx, 101)
	if b.Available != 123 {
		t.Fatalf("paid recharge %+v", b)
	}
	w = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/workflows/recharges", nil)
	s.ServeWorkflows(w, r, Principal{Role: "merchant", SubjectID: 102}, []string{"recharges"})
	if !strings.Contains(w.Body.String(), `"total":0`) {
		t.Fatalf("recharge cross tenant %s", w.Body)
	}
}
func TestOnboardingLocalLifecycleAndPrivateCaseHistory(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	merchant := Principal{Role: "merchant", SubjectID: 101, Username: "m1"}
	v, e := s.Create(ctx, merchant, "onboarding-applications", input(`{"business_name":"shop","contact_name":"person","contact_phone":"1234","channel_id":"1"}`))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.caseState(ctx, merchant, "onboarding-applications", v.ID, "under_review"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Update(ctx, merchant, "onboarding-applications", v.ID, input(`{"business_name":"replace while reviewing"}`)); !errors.Is(e, ErrForbidden) {
		t.Fatalf("submitted draft editable %v", e)
	}
	if e = s.caseStateWithComment(ctx, admin, "onboarding-applications", v.ID, "action_required", "please complete fields"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Update(ctx, merchant, "onboarding-applications", v.ID, input(`{"business_name":"completed"}`)); e != nil {
		t.Fatal(e)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/history", nil)
	s.Serve(w, r, Principal{Role: "merchant", SubjectID: 102}, []string{"onboarding-applications", strconv.FormatInt(v.ID, 10), "history"})
	if w.Code != 404 {
		t.Fatalf("case history leak HTTP%d", w.Code)
	}
}
