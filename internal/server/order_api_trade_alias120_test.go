package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

// RED against the existing real HTTP dispatcher, durable sessions, migrated PG
// schema and ordinary provider orchestration. No future production symbol is
// imported. Migration 120 is reserved for this independent display metadata;
// these tests create no migration or production table themselves.
//
// Original Pro edits api_trade_no via order/editApiTradeNo. The agreed safer
// contract instead GETs six explicit fields and PATCHes ONLY
// {api_trade_no_alias, expected_fingerprint} at /orders/:trade/api-trade-alias.
// Neither the original reporting.api_trade_no nor a verified receipt is edited.
// The agreed side table is order_api_trade_aliases120, with order_id,
// merchant_id, api_trade_no_alias and proof_hash. A fresh empty alias must be
// distinguishable from a deleted/corrupted previously sealed alias.

type orderAlias120Provider struct {
	*channel168Provider
	mu      sync.Mutex
	creates map[string]adapter.CreateRequest
	queries []adapter.QueryRequest
	refunds []adapter.RefundRequest
}

func (p *orderAlias120Provider) Capabilities() adapter.Capabilities {
	return adapter.Capabilities{Methods: []string{"native"}, Query: true, Refund: true, RefundQuery: true}
}
func (p *orderAlias120Provider) Create(ctx context.Context, r adapter.CreateRequest) (adapter.CreateResult, error) {
	p.mu.Lock()
	p.creates[r.TradeNo] = r
	p.mu.Unlock()
	return p.channel168Provider.Create(ctx, r)
}
func (p *orderAlias120Provider) Query(_ context.Context, r adapter.QueryRequest) (adapter.QueryResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.queries = append(p.queries, r)
	created, ok := p.creates[r.TradeNo]
	if !ok {
		return adapter.QueryResult{}, adapter.ErrInvalid
	}
	return adapter.QueryResult{TradeNo: r.TradeNo, ProviderTradeNo: "receipt120:" + r.TradeNo, State: "SUCCESS", Currency: "CNY", Amount: created.Amount, BuyerID: "synthetic-buyer120"}, nil
}
func (p *orderAlias120Provider) Refund(_ context.Context, r adapter.RefundRequest) (adapter.RefundResult, error) {
	p.mu.Lock()
	p.refunds = append(p.refunds, r)
	p.mu.Unlock()
	// A metadata edit must never reach this method. No real refund or SDK call
	// is possible even if a broken handler accidentally attempts one.
	return adapter.RefundResult{}, adapter.ErrUnsupported
}
func (p *orderAlias120Provider) QueryRefund(_ context.Context, r adapter.RefundRequest) (adapter.RefundResult, error) {
	p.mu.Lock()
	p.refunds = append(p.refunds, r)
	p.mu.Unlock()
	return adapter.RefundResult{}, adapter.ErrUnsupported
}
func (p *orderAlias120Provider) counts() (int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.queries), len(p.refunds)
}

type orderAlias120Fixture struct {
	channel168Fixture
	ctx                                                        context.Context
	provider                                                   *orderAlias120Provider
	order                                                      payment.Order
	merchantToken, merchantCSRF, otherToken, otherCSRF         string
	operatorToken, operatorCSRF, delegatedToken, delegatedCSRF string
}

func orderAlias120Local(t *testing.T) *orderAlias120Fixture {
	t.Helper()
	f := &orderAlias120Fixture{channel168Fixture: channel168Local(t), ctx: t.Context()}
	f.provider = &orderAlias120Provider{channel168Provider: &channel168Provider{app: "synthetic-alias120"}, creates: map[string]adapter.CreateRequest{}}
	f.s.ProviderFlow.Providers[7] = f.provider
	created := f.pay(t, "original-alias120", "1.23", 7)
	trade, ok := created["trade_no"].(string)
	if !ok || trade == "" || created["code"] != float64(1) {
		t.Fatal("ordinary synthetic payment fixture did not create a real order", created)
	}
	w := f.request("admin", f.token, f.csrf, "POST", trade+"/query", `{}`)
	if w.Code != http.StatusOK {
		t.Fatalf("ordinary in-process query fixture failed: HTTP=%d body=%s", w.Code, w.Body.String())
	}
	var err error
	f.order, err = f.s.Store.OrderByTradeNo(t.Context(), trade)
	if err != nil || f.order.Status != "paid" || f.order.Amount != 123 {
		t.Fatal("fixture lacks the original paid order and requested amount", err)
	}
	var receipt string
	if err = f.p.QueryRow(t.Context(), `SELECT provider_trade_no FROM verified_payment_receipts WHERE order_id=$1`, f.order.ID).Scan(&receipt); err != nil || receipt != f.receipt() {
		t.Fatal("fixture did not authenticate its original provider receipt", err)
	}
	if err = f.s.Store.CreateMerchant(t.Context(), payment.Merchant{ID: 2, Name: "Other alias120", APIKey: "synthetic-other-alias120"}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.p.Exec(t.Context(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) VALUES
 ('alias120-owner','merchant',1,'Alias120 owner','unused-synthetic120','operator'),
 ('alias120-other','merchant',2,'Alias120 other','unused-synthetic120','operator'),
 ('alias120-operator','admin',170,'Alias120 operator','unused-synthetic120','operator')`); err != nil {
		t.Fatal(err)
	}
	for _, caller := range []struct {
		user        identity.User
		token, csrf *string
	}{
		{identity.User{ID: 1, Role: "merchant", Username: "alias120-owner", AuthVersion: 1}, &f.merchantToken, &f.merchantCSRF},
		{identity.User{ID: 2, Role: "merchant", Username: "alias120-other", AuthVersion: 1}, &f.otherToken, &f.otherCSRF},
		{identity.User{ID: 170, Role: "admin", Username: "alias120-operator", AdminClass: "operator", AuthVersion: 1}, &f.operatorToken, &f.operatorCSRF},
	} {
		*caller.token, *caller.csrf, err = f.s.Sessions.Create(caller.user)
		if err != nil {
			t.Fatal(err)
		}
	}
	tx, err := f.p.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	f.delegatedToken, f.delegatedCSRF, _, err = f.s.Sessions.CreateDelegated(t.Context(), tx,
		identity.User{ID: 1, Role: "merchant", Username: "alias120-owner", AuthVersion: 1},
		identity.User{ID: 168, Role: "admin", Username: "admin168", AdminClass: "super", AuthVersion: 1}, f.token)
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *orderAlias120Fixture) receipt() string { return "receipt120:" + f.order.TradeNo }
func (f *orderAlias120Fixture) path() string    { return f.order.TradeNo + "/api-trade-alias" }
func (f *orderAlias120Fixture) req(ctx context.Context, role, token, csrf, method, path, body string) *http.Request {
	r := httptest.NewRequest(method, "/console/v1/"+role+"/orders/"+path, strings.NewReader(body)).WithContext(ctx)
	r.RemoteAddr = "192.0.2.120:5120"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", csrf)
	if token != "" {
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
	}
	return r
}
func (f *orderAlias120Fixture) request(role, token, csrf, method, path, body string) *httptest.ResponseRecorder {
	ctx, cancel := context.WithTimeout(f.ctx, 12*time.Second)
	defer cancel()
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, f.req(ctx, role, token, csrf, method, path, body))
	return w
}

type orderAlias120View struct {
	TradeNo            string `json:"trade_no"`
	MerchantID         string `json:"merchant_id"`
	Alias              string `json:"api_trade_no_alias"`
	VerifiedAPITradeNo string `json:"verified_api_trade_no"`
	Fingerprint        string `json:"fingerprint"`
	CanUpdate          bool   `json:"can_update"`
}

func orderAlias120Decode(t *testing.T, f *orderAlias120Fixture, w *httptest.ResponseRecorder) orderAlias120View {
	t.Helper()
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("independent alias endpoint must return a private successful response: HTTP=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		Code int                        `json:"code"`
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.Code != 0 || len(out.Data) != 6 {
		t.Fatal("alias response must have the exact six-field frontend DTO", err)
	}
	for _, field := range []string{"trade_no", "merchant_id", "api_trade_no_alias", "verified_api_trade_no", "fingerprint"} {
		value := strings.TrimSpace(string(out.Data[field]))
		if len(value) < 2 || value[0] != '"' {
			t.Fatalf("alias DTO field %s must be an explicit string", field)
		}
	}
	if value := strings.TrimSpace(string(out.Data["can_update"])); value != "true" && value != "false" {
		t.Fatal("alias update authority is not an explicit boolean")
	}
	raw, err := json.Marshal(out.Data)
	var view orderAlias120View
	wantReceipt := ""
	if f.order.Status == "paid" {
		wantReceipt = f.receipt()
	}
	if err != nil || json.Unmarshal(raw, &view) != nil || view.TradeNo != f.order.TradeNo || view.MerchantID != "1" || view.VerifiedAPITradeNo != wantReceipt || len(view.Fingerprint) != 64 || strings.Trim(view.Fingerprint, "0123456789abcdef") != "" || !view.CanUpdate {
		t.Fatal("alias response lost original order/merchant/receipt binding or CAS authority", err)
	}
	for _, secret := range []string{"proof_hash", "birth_xid", "private_key", "test-secret", f.token, f.csrf} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("alias DTO leaked private source or session material")
		}
	}
	return view
}
func (f *orderAlias120Fixture) view(t *testing.T) orderAlias120View {
	t.Helper()
	return orderAlias120Decode(t, f, f.request("admin", f.token, "", "GET", f.path(), ""))
}
func orderAlias120Body(alias, fingerprint string) string {
	raw, _ := json.Marshal(map[string]string{"api_trade_no_alias": alias, "expected_fingerprint": fingerprint})
	return string(raw)
}
func (f *orderAlias120Fixture) save(t *testing.T, alias, fingerprint string) orderAlias120View {
	t.Helper()
	return orderAlias120Decode(t, f, f.request("admin", f.token, f.csrf, "PATCH", f.path(), orderAlias120Body(alias, fingerprint)))
}

func (f *orderAlias120Fixture) business(t *testing.T) string {
	t.Helper()
	var state string
	err := f.p.QueryRow(t.Context(), `SELECT jsonb_build_object(
 'orders',(SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY x.id),'[]'::jsonb) FROM orders x),
 'receipts',(SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY x.order_id),'[]'::jsonb) FROM verified_payment_receipts x),
 'attempts',(SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY x.id),'[]'::jsonb) FROM provider_attempts x),
 'operations',(SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY x.id),'[]'::jsonb) FROM provider_operations x),
 'refunds',(SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY x.id),'[]'::jsonb) FROM refunds x),
 'accounts',(SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY x.id),'[]'::jsonb) FROM accounts x),
 'journals',(SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY x.id),'[]'::jsonb) FROM journals x),
 'entries',(SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY x.id),'[]'::jsonb) FROM ledger_entries x),
 'jobs',(SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY x.id),'[]'::jsonb) FROM jobs x),
 'outbox',(SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY x.id),'[]'::jsonb) FROM outbox x),
 'charges',(SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY x.order_id),'[]'::jsonb) FROM order_customer_charges104 x)
 )::text`).Scan(&state)
	if err != nil {
		t.Fatal(err)
	}
	return state
}
func (f *orderAlias120Fixture) audits(t *testing.T) int {
	t.Helper()
	var count int
	if err := f.p.QueryRow(t.Context(), `SELECT count(*) FROM backoffice_audit WHERE resource='orders' AND resource_id=$1 AND operation='api-trade-alias-update'`, f.order.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
func (f *orderAlias120Fixture) unchanged(t *testing.T, before string, queries, refunds int) {
	t.Helper()
	q, r := f.provider.counts()
	if f.business(t) != before || q != queries || r != refunds || f.provider.sends.Load() != 1 {
		t.Fatal("display-alias work changed original money/receipt/refund/provider/notification facts or sent a financial request")
	}
}
func orderAlias120Closed(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusConflict && w.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfirmed original/alias source must fail closed: HTTP=%d body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), `"can_update":true`) {
		t.Fatal("unconfirmed source returned alias write authority")
	}
}
func (f *orderAlias120Fixture) publicOrder(t *testing.T) map[string]any {
	t.Helper()
	values := url.Values{"act": {"order"}, "pid": {"1"}, "key": {"test-secret"}, "trade_no": {f.order.TradeNo}}
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api.php?"+values.Encode(), nil))
	var out map[string]any
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out["code"] != float64(1) || out["api_trade_no"] != f.receipt() || out["money"] != "1.23" {
		t.Fatal("original authenticated legacy query lost its verified receipt or amount", w.Code, out)
	}
	return out
}

func TestOrderAPITradeAlias120ActualHTTPDefaultReadAndDetailCapability(t *testing.T) {
	f := orderAlias120Local(t)
	before := f.business(t)
	q, r := f.provider.counts()
	first := f.view(t)
	if first.Alias != "" || f.view(t) != first {
		t.Fatal("initial alias is not an explicit, stable authenticated empty value")
	}
	w := f.request("admin", f.token, "", "GET", f.order.TradeNo, "")
	var detail struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &detail) != nil || string(detail.Data["can_edit_api_trade_alias"]) != "true" {
		t.Fatalf("the real admin OrderDetails must expose the independent capability: HTTP=%d body=%s", w.Code, w.Body.String())
	}
	var reporting map[string]string
	if json.Unmarshal(detail.Data["reporting"], &reporting) != nil || reporting["api_trade_no"] != f.receipt() {
		t.Fatal("detail capability replaced the immutable reporting receipt")
	}
	f.unchanged(t, before, q, r)
}

func TestOrderAPITradeAlias120ActualHTTPSaveClearAndQueriesPreserveOriginalBusinessFacts(t *testing.T) {
	f := orderAlias120Local(t)
	initial := f.view(t)
	queryBefore := f.publicOrder(t)
	before := f.business(t)
	q, r := f.provider.counts()
	saved := f.save(t, "独立显示别名/<script>120</script>", initial.Fingerprint)
	if saved.Alias != "独立显示别名/<script>120</script>" || saved.Fingerprint == initial.Fingerprint || f.view(t) != saved {
		t.Fatal("alias did not round-trip independently with a new durable fingerprint")
	}
	if !reflect.DeepEqual(queryBefore, f.publicOrder(t)) {
		t.Fatal("display alias changed the original merchant payment query DTO")
	}
	cleared := f.save(t, "", saved.Fingerprint)
	if cleared.Alias != "" || cleared.Fingerprint == saved.Fingerprint || cleared.Fingerprint == initial.Fingerprint || f.view(t) != cleared {
		t.Fatal("clearing the alias erased its history or restored an old CAS fingerprint")
	}
	w := f.request("admin", f.token, f.csrf, "PATCH", f.path(), orderAlias120Body("STALE-INITIAL-CAS", initial.Fingerprint))
	if w.Code != 409 || f.view(t) != cleared {
		t.Fatal("clear permitted an ABA replay using the original empty fingerprint", w.Code)
	}
	if f.audits(t) != 2 {
		t.Fatal("save and clear did not each produce one attributable metadata audit")
	}
	var actorRole, actorName string
	var actorID int64
	if err := f.p.QueryRow(t.Context(), `SELECT actor_role,actor_id,actor_name FROM backoffice_audit WHERE resource='orders' AND resource_id=$1 AND operation='api-trade-alias-update' ORDER BY id DESC LIMIT 1`, f.order.ID).Scan(&actorRole, &actorID, &actorName); err != nil || actorRole != "admin" || actorID != 168 || actorName != "admin168" {
		t.Fatal("alias audit lost the real original administrator", err)
	}
	f.unchanged(t, before, q, r)
}

func TestOrderAPITradeAlias120ActualHTTPOriginalAdminRolesAndMerchantBoundaries(t *testing.T) {
	f := orderAlias120Local(t)
	initial := f.view(t)
	before := f.business(t)
	q, r := f.provider.counts()
	operator := orderAlias120Decode(t, f, f.request("admin", f.operatorToken, "", "GET", f.path(), ""))
	updated := orderAlias120Decode(t, f, f.request("admin", f.operatorToken, f.operatorCSRF, "PATCH", f.path(), orderAlias120Body("OPERATOR-DISPLAY-ALIAS", operator.Fingerprint)))
	if updated.Alias != "OPERATOR-DISPLAY-ALIAS" {
		t.Fatal("original R_ADMIN business-management permission was lost")
	}
	for _, tc := range []struct {
		name, role, token, csrf, method string
		want                            int
	}{
		{"anonymous read", "admin", "", "", "GET", 401},
		{"anonymous write", "admin", "", "", "PATCH", 401},
		{"missing CSRF", "admin", f.token, "", "PATCH", 403},
		{"foreign CSRF", "admin", f.token, f.operatorCSRF, "PATCH", 403},
		{"owner merchant read", "merchant", f.merchantToken, "", "GET", 404},
		{"owner merchant write", "merchant", f.merchantToken, f.merchantCSRF, "PATCH", 404},
		{"foreign merchant read", "merchant", f.otherToken, "", "GET", 404},
		{"foreign merchant write", "merchant", f.otherToken, f.otherCSRF, "PATCH", 404},
		{"delegated merchant read", "merchant", f.delegatedToken, "", "GET", 404},
		{"delegated merchant write", "merchant", f.delegatedToken, f.delegatedCSRF, "PATCH", 404},
		{"delegated token as administrator", "admin", f.delegatedToken, f.delegatedCSRF, "PATCH", 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := f.request(tc.role, tc.token, tc.csrf, tc.method, f.path(), orderAlias120Body("DENIED-ALIAS", initial.Fingerprint))
			if w.Code != tc.want {
				t.Fatalf("role/session boundary: HTTP=%d want%d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
	if f.view(t) != updated || f.audits(t) != 1 {
		t.Fatal("denied caller changed alias state or its audit")
	}
	f.unchanged(t, before, q, r)
}

func TestOrderAPITradeAlias120ActualHTTPPersistentSessionsAndRevocation(t *testing.T) {
	for _, change := range []string{"new-session-repository", "revoked", "expired", "auth-version", "disabled", "memory-session"} {
		t.Run(change, func(t *testing.T) {
			f := orderAlias120Local(t)
			prior := f.view(t)
			before := f.business(t)
			q, r := f.provider.counts()
			want := 401
			switch change {
			case "new-session-repository":
				f.s.Sessions = identity.NewPersistentSessions(f.p, false)
				if f.view(t) != prior {
					t.Fatal("durable alias authorization depended on process-local session state")
				}
				return
			case "revoked":
				if _, err := f.p.Exec(t.Context(), `DELETE FROM console_sessions WHERE username='admin168' AND role='admin'`); err != nil {
					t.Fatal(err)
				}
			case "expired":
				// Well past expiry on both clocks: the test PostgreSQL container's
				// clock can run seconds ahead of the host's.
				if _, err := f.p.Exec(t.Context(), `UPDATE console_sessions SET expires_at=clock_timestamp()-interval '10 minutes' WHERE username='admin168' AND role='admin'`); err != nil {
					t.Fatal(err)
				}
			case "auth-version":
				if _, err := f.p.Exec(t.Context(), `UPDATE console_users SET password_hash='changed-synthetic120' WHERE username='admin168' AND role='admin'`); err != nil {
					t.Fatal(err)
				}
			case "disabled":
				if _, err := f.p.Exec(t.Context(), `UPDATE console_users SET enabled=false WHERE username='admin168' AND role='admin'`); err != nil {
					t.Fatal(err)
				}
			case "memory-session":
				f.s.Sessions = identity.NewSessions(false)
				var err error
				f.token, f.csrf, err = f.s.Sessions.Create(identity.User{ID: 168, Role: "admin", Username: "admin168", AdminClass: "super", AuthVersion: 1})
				if err != nil {
					t.Fatal(err)
				}
				want = 503
			}
			for _, method := range []string{"GET", "PATCH"} {
				body := ""
				if method == "PATCH" {
					body = orderAlias120Body("REVOKED-ALIAS", prior.Fingerprint)
				}
				w := f.request("admin", f.token, f.csrf, method, f.path(), body)
				if w.Code != want {
					t.Fatalf("%s %s must verify original persistent session: HTTP=%d want%d", change, method, w.Code, want)
				}
			}
			if f.audits(t) != 0 {
				t.Fatal("unverified persistent session wrote an alias audit")
			}
			f.unchanged(t, before, q, r)
		})
	}
}

func TestOrderAPITradeAlias120ActualHTTPConcurrentCASHasOneWinner(t *testing.T) {
	f := orderAlias120Local(t)
	prior := f.view(t)
	before := f.business(t)
	q, r := f.provider.counts()
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
	defer cancel()
	start := make(chan struct{})
	done := make(chan *httptest.ResponseRecorder, 2)
	handler := f.s.Handler()
	for _, alias := range []string{"CAS-LEFT-120", "CAS-RIGHT-120"} {
		request := f.req(ctx, "admin", f.token, f.csrf, "PATCH", f.path(), orderAlias120Body(alias, prior.Fingerprint))
		go func(r *http.Request) { <-start; w := httptest.NewRecorder(); handler.ServeHTTP(w, r); done <- w }(request)
	}
	close(start)
	counts := map[int]int{}
	var winner orderAlias120View
	for range 2 {
		select {
		case w := <-done:
			counts[w.Code]++
			if w.Code == 200 {
				winner = orderAlias120Decode(t, f, w)
			}
		case <-ctx.Done():
			t.Fatal("concurrent alias CAS did not settle", ctx.Err())
		}
	}
	if counts[200] != 1 || counts[409] != 1 || f.view(t) != winner || f.audits(t) != 1 {
		t.Fatal("same-fingerprint writers did not produce exactly one committed alias and one conflict", counts)
	}
	f.unchanged(t, before, q, r)
}

func TestOrderAPITradeAlias120ActualHTTPUnknownOriginalSourceCannotBecomeEmptyAlias(t *testing.T) {
	f := orderAlias120Local(t)
	var id int64
	if err := f.p.QueryRow(t.Context(), `INSERT INTO orders(trade_no,merchant_id,out_trade_no,name,type,amount,status) VALUES('unknown-origin120',1,'unknown-origin120','Unsealed original120','alipay',123,'paid') RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	// An arbitrary SQL receipt-shaped row cannot substitute for a trusted birth.
	if _, err := f.p.Exec(t.Context(), `INSERT INTO verified_payment_receipts(order_id,channel_id,provider_trade_no) VALUES($1,7,'unsealed-receipt120')`, id); err != nil {
		t.Fatal(err)
	}
	before := f.business(t)
	q, r := f.provider.counts()
	for _, method := range []string{"GET", "PATCH"} {
		body := ""
		if method == "PATCH" {
			body = orderAlias120Body("UNSAFE-REINITIALIZATION", strings.Repeat("a", 64))
		}
		orderAlias120Closed(t, f.request("admin", f.token, f.csrf, method, "unknown-origin120/api-trade-alias", body))
	}
	w := f.request("admin", f.token, "", "GET", "unknown-origin120", "")
	if w.Code == 200 && strings.Contains(w.Body.String(), `"can_edit_api_trade_alias":true`) {
		t.Fatal("unsealed order exposed the alias edit capability")
	}
	f.unchanged(t, before, q, r)
}

func TestOrderAPITradeAlias120ActualHTTPCorruptionAndCreatedRowLossFailClosed(t *testing.T) {
	for _, change := range []string{"alias", "proof", "deleted", "verified-receipt", "witness-proof", "charge-proof-deleted"} {
		t.Run(change, func(t *testing.T) {
			f := orderAlias120Local(t)
			saved := f.save(t, "SEALED-ORIGINAL-ALIAS", f.view(t).Fingerprint)
			var count int
			if err := f.p.QueryRow(t.Context(), `SELECT count(*) FROM order_api_trade_aliases120 WHERE order_id=$1 AND merchant_id=1`, f.order.ID).Scan(&count); err != nil || count != 1 {
				t.Fatal("successful PATCH did not create one independent bound alias source", count, err)
			}
			tx, err := f.p.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(t.Context())
			table := "order_api_trade_aliases120"
			sql := `UPDATE order_api_trade_aliases120 SET api_trade_no_alias='UNSEALED-TAMPERED-ALIAS' WHERE order_id=$1`
			switch change {
			case "proof":
				sql = `UPDATE order_api_trade_aliases120 SET proof_hash=decode(repeat('00',32),'hex') WHERE order_id=$1`
			case "deleted":
				sql = `DELETE FROM order_api_trade_aliases120 WHERE order_id=$1`
			case "verified-receipt":
				table = "verified_payment_receipts"
				sql = `UPDATE verified_payment_receipts SET provider_trade_no='TAMPERED-RECEIPT120' WHERE order_id=$1`
			case "witness-proof":
				table = "order_api_trade_alias_commits120"
				sql = `UPDATE order_api_trade_alias_commits120 SET proof_hash=decode(repeat('00',32),'hex') WHERE order_id=$1`
			case "charge-proof-deleted":
				table = "order_customer_charges104"
				sql = `DELETE FROM order_customer_charges104 WHERE order_id=$1`
			}
			// Deliberately simulate storage corruption only inside the fixture's
			// private schema. Re-enable every guard in the same transaction.
			if _, err = tx.Exec(t.Context(), "ALTER TABLE "+table+" DISABLE TRIGGER USER"); err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec(t.Context(), sql, f.order.ID); err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec(t.Context(), "ALTER TABLE "+table+" ENABLE TRIGGER USER"); err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(t.Context()); err != nil {
				t.Fatal(err)
			}
			before := f.business(t)
			q, r := f.provider.counts()
			orderAlias120Closed(t, f.request("admin", f.token, "", "GET", f.path(), ""))
			orderAlias120Closed(t, f.request("admin", f.token, f.csrf, "PATCH", f.path(), orderAlias120Body("MUST-NOT-REPAIR-AS-EMPTY", saved.Fingerprint)))
			w := f.request("admin", f.token, "", "GET", f.order.TradeNo, "")
			if w.Code == 200 && strings.Contains(w.Body.String(), `"can_edit_api_trade_alias":true`) {
				t.Fatal("corrupted alias source still exposed an editable detail capability")
			}
			if f.audits(t) != 1 {
				t.Fatal("corruption silently repaired an alias or added a successful write audit")
			}
			f.unchanged(t, before, q, r)
		})
	}
}

func TestOrderAPITradeAlias120ActualHTTPAuditFailureRollsBackAliasAndFingerprint(t *testing.T) {
	f := orderAlias120Local(t)
	prior := f.view(t)
	before := f.business(t)
	q, r := f.provider.counts()
	ddl := fmt.Sprintf(`CREATE SEQUENCE alias120_audit_entered;
 CREATE FUNCTION alias120_audit_fail() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.resource='orders' AND NEW.resource_id=%d THEN PERFORM nextval('alias120_audit_entered'); RAISE EXCEPTION 'synthetic alias120 audit failure'; END IF; RETURN NEW; END$$;
 CREATE TRIGGER alias120_audit_fail BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION alias120_audit_fail()`, f.order.ID)
	if _, err := f.p.Exec(t.Context(), ddl); err != nil {
		t.Fatal(err)
	}
	w := f.request("admin", f.token, f.csrf, "PATCH", f.path(), orderAlias120Body("AUDIT-MUST-ROLLBACK", prior.Fingerprint))
	if w.Code != 503 {
		t.Fatalf("alias audit failure must fail closed: HTTP=%d body=%s", w.Code, w.Body.String())
	}
	var entered bool
	if err := f.p.QueryRow(t.Context(), `SELECT is_called FROM alias120_audit_entered`).Scan(&entered); err != nil || !entered {
		t.Fatal("the real INSERT audit branch was not reached", entered, err)
	}
	if _, err := f.p.Exec(t.Context(), `DROP TRIGGER alias120_audit_fail ON backoffice_audit`); err != nil {
		t.Fatal(err)
	}
	if f.view(t) != prior || f.audits(t) != 0 {
		t.Fatal("failed audit left a saved alias, changed fingerprint, or successful audit")
	}
	f.unchanged(t, before, q, r)
}

func TestOrderAPITradeAlias120ActualHTTPStrictShapeAndUnsupportedMethods(t *testing.T) {
	f := orderAlias120Local(t)
	prior := f.view(t)
	before := f.business(t)
	q, r := f.provider.counts()
	valid := orderAlias120Body("VALID-ALIAS", prior.Fingerprint)
	for _, tc := range []struct {
		name, method, suffix, body string
		want                       int
	}{
		{"old original field", "PATCH", "", fmt.Sprintf(`{"api_trade_no":"OVERRIDE-RECEIPT","expected_fingerprint":%q}`, prior.Fingerprint), 400},
		{"extra provider identity", "PATCH", "", strings.TrimSuffix(valid, "}") + `,"provider_trade_no":"OVERRIDE"}`, 400},
		{"extra amount", "PATCH", "", strings.TrimSuffix(valid, "}") + `,"amount":"99.99"}`, 400},
		{"extra merchant", "PATCH", "", strings.TrimSuffix(valid, "}") + `,"merchant_id":"2"}`, 400},
		{"extra target order", "PATCH", "", strings.TrimSuffix(valid, "}") + `,"trade_no":"other"}`, 400},
		{"duplicate alias", "PATCH", "", strings.TrimSuffix(valid, "}") + `,"api_trade_no_alias":"duplicate"}`, 400},
		{"null alias", "PATCH", "", fmt.Sprintf(`{"api_trade_no_alias":null,"expected_fingerprint":%q}`, prior.Fingerprint), 400},
		{"missing alias", "PATCH", "", fmt.Sprintf(`{"expected_fingerprint":%q}`, prior.Fingerprint), 400},
		{"null body", "PATCH", "", `null`, 400},
		{"trailing JSON", "PATCH", "", valid + ` {}`, 400},
		{"GET with PATCH body", "GET", "", valid, 400},
		{"query override read", "GET", "?merchant_id=2", ``, 400},
		{"query override write", "PATCH", "?trade_no=other", valid, 400},
		{"old POST method", "POST", "", valid, 405},
		{"delete method", "DELETE", "", valid, 405},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := f.request("admin", f.token, f.csrf, tc.method, f.path()+tc.suffix, tc.body)
			if w.Code != tc.want {
				t.Fatalf("strict alias request shape: HTTP=%d want%d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
	if f.view(t) != prior || f.audits(t) != 0 {
		t.Fatal("invalid request changed an alias or wrote an accepted audit")
	}
	f.unchanged(t, before, q, r)
}

type orderAlias120LostWriter struct {
	header http.Header
	status int
	writes int
}

func (w *orderAlias120LostWriter) Header() http.Header    { return w.header }
func (w *orderAlias120LostWriter) WriteHeader(status int) { w.status = status }
func (w *orderAlias120LostWriter) Write(p []byte) (int, error) {
	w.writes++
	return 0, io.ErrClosedPipe
}

func TestOrderAPITradeAlias120ActualHTTPLostResponseReconcilesThroughReadWithoutReapplying(t *testing.T) {
	f := orderAlias120Local(t)
	prior := f.view(t)
	before := f.business(t)
	q, r := f.provider.counts()
	lost := &orderAlias120LostWriter{header: make(http.Header)}
	f.s.Handler().ServeHTTP(lost, f.req(t.Context(), "admin", f.token, f.csrf, "PATCH", f.path(), orderAlias120Body("COMMITTED-WITH-LOST-RESPONSE", prior.Fingerprint)))
	if lost.status != 200 || lost.writes == 0 {
		t.Fatal("successful real handler did not reach the deliberately broken response transport", lost.status, lost.writes)
	}
	current := f.view(t)
	if current.Alias != "COMMITTED-WITH-LOST-RESPONSE" || current.Fingerprint == prior.Fingerprint || f.audits(t) != 1 {
		t.Fatal("read could not reconcile a committed alias with a lost response")
	}
	w := f.request("admin", f.token, f.csrf, "PATCH", f.path(), orderAlias120Body("DUPLICATE-RETRY", prior.Fingerprint))
	if w.Code != 409 || f.view(t) != current || f.audits(t) != 1 {
		t.Fatal("lost-response retry applied a second write using a stale fingerprint", w.Code)
	}
	f.unchanged(t, before, q, r)
}

func TestOrderAPITradeAlias120ActualHTTPOriginalSessionExpiresAtLastWrite(t *testing.T) {
	f := orderAlias120Local(t)
	prior := f.view(t)
	before := f.business(t)
	q, r := f.provider.counts()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	ddl := fmt.Sprintf(`CREATE SEQUENCE alias120_expiry_entered;
 CREATE FUNCTION alias120_expiry_delay() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.resource='orders' AND NEW.resource_id=%d THEN PERFORM nextval('alias120_expiry_entered'); PERFORM pg_sleep(5.5); END IF; RETURN NEW; END$$;
 CREATE TRIGGER alias120_expiry_delay AFTER INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION alias120_expiry_delay();
 UPDATE console_sessions SET expires_at=clock_timestamp()+interval '5 seconds' WHERE username='admin168' AND role='admin'`, f.order.ID)
	if _, err := f.p.Exec(ctx, ddl); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, f.req(ctx, "admin", f.token, f.csrf, "PATCH", f.path(), orderAlias120Body("EXPIRED-AT-COMMIT", prior.Fingerprint)))
	if w.Code != 403 {
		t.Fatalf("expired original session committed metadata after its final write: HTTP=%d body=%s", w.Code, w.Body.String())
	}
	var entered bool
	if err := f.p.QueryRow(ctx, `SELECT is_called FROM alias120_expiry_entered`).Scan(&entered); err != nil || !entered {
		t.Fatal("expiry regression did not reach the real last audit INSERT", entered, err)
	}
	// Read through the independent still-live original R_ADMIN session.
	after := orderAlias120Decode(t, f, f.request("admin", f.operatorToken, "", "GET", f.path(), ""))
	if after != prior || f.audits(t) != 0 {
		t.Fatal("expiry failed to roll back alias state and audit atomically")
	}
	f.unchanged(t, before, q, r)
}

func TestOrderAPITradeAlias120ActualHTTPAuthenticatedPendingBirthKeepsAliasAcrossRealPaidObservation(t *testing.T) {
	f := orderAlias120Local(t)
	created := f.pay(t, "pending-original-alias120", "1.23", 7)
	trade, ok := created["trade_no"].(string)
	if !ok || trade == "" || created["code"] != float64(1) {
		t.Fatal("ordinary trusted pending fixture failed", created)
	}
	var err error
	f.order, err = f.s.Store.OrderByTradeNo(t.Context(), trade)
	if err != nil || f.order.Status == "paid" {
		t.Fatal("pending fixture was not a distinct unpaid original order", err)
	}
	initial := f.view(t)
	before := f.business(t)
	q, r := f.provider.counts()
	saved := f.save(t, "PENDING-DISPLAY-ALIAS", initial.Fingerprint)
	var receipts int
	if err = f.p.QueryRow(t.Context(), `SELECT count(*) FROM verified_payment_receipts WHERE order_id=$1`, f.order.ID).Scan(&receipts); err != nil || receipts != 0 || saved.VerifiedAPITradeNo != "" {
		t.Fatal("editing an unpaid display alias fabricated a verified receipt", receipts, err)
	}
	qAfter, rAfter := f.provider.counts()
	if before != f.business(t) || qAfter != q || rAfter != r {
		t.Fatal("unpaid alias edit queried a provider or changed the payment state")
	}
	w := f.request("admin", f.token, f.csrf, "POST", f.order.TradeNo+"/query", `{}`)
	if w.Code != 200 {
		t.Fatal("normal controlled payment completion was blocked by an alias", w.Code)
	}
	f.order, err = f.s.Store.OrderByTradeNo(t.Context(), trade)
	if err != nil || f.order.Status != "paid" {
		t.Fatal("ordinary query did not confirm the original payment", err)
	}
	paid := f.view(t)
	if paid.Alias != saved.Alias || paid.VerifiedAPITradeNo != f.receipt() || paid.Fingerprint == saved.Fingerprint {
		t.Fatal("trusted paid observation lost the independent alias or failed to invalidate pending CAS")
	}
	w = f.request("admin", f.token, f.csrf, "PATCH", f.path(), orderAlias120Body("STALE-PENDING-WRITE", saved.Fingerprint))
	if w.Code != 409 {
		t.Fatal("a pending fingerprint remained writable after a real receipt appeared", w.Code)
	}
	before = f.business(t)
	sealed := f.save(t, "PAID-DISPLAY-ALIAS", paid.Fingerprint)
	if sealed.VerifiedAPITradeNo != f.receipt() || f.business(t) != before {
		t.Fatal("explicit post-payment alias save replaced original payment facts")
	}
}
