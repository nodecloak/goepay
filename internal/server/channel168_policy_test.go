package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

type channel168Provider struct {
	subchannelTestQuery func(context.Context, adapter.QueryRequest) (adapter.QueryResult, error)
	app                 string
	cost                string
	sends               atomic.Int64
	subchannelTestCaps  *adapter.Capabilities
}

func (p *channel168Provider) PaymentCostEstimate() (adapter.PaymentCostEstimate, bool) {
	if p.cost == "" {
		return adapter.PaymentCostEstimate{}, false
	}
	v, e := adapter.NewPaymentCostEstimate(p, p.cost)
	return v, e == nil
}

func (p *channel168Provider) ID() string                      { return "alipay" }
func (p *channel168Provider) ApplicationID() string           { return p.app }
func (p *channel168Provider) TransferAccountIdentity() string { return "seller:" + p.app }
func (p *channel168Provider) Capabilities() adapter.Capabilities {
	if p.subchannelTestCaps != nil {
		return *p.subchannelTestCaps
	}
	return adapter.Capabilities{Methods: []string{"native"}, Query: true}
}
func (p *channel168Provider) Create(context.Context, adapter.CreateRequest) (adapter.CreateResult, error) {
	p.sends.Add(1)
	return adapter.CreateResult{MaterialType: "qrcode", Material: "https://local.example.invalid/channel168"}, nil
}
func (p *channel168Provider) Query(ctx context.Context, r adapter.QueryRequest) (adapter.QueryResult, error) {
	if p.subchannelTestQuery != nil {
		return p.subchannelTestQuery(ctx, r)
	}
	return adapter.QueryResult{}, adapter.ErrUnsupported
}
func (*channel168Provider) Close(context.Context, adapter.QueryRequest) error {
	return adapter.ErrUnsupported
}
func (*channel168Provider) Refund(context.Context, adapter.RefundRequest) (adapter.RefundResult, error) {
	return adapter.RefundResult{}, adapter.ErrUnsupported
}
func (*channel168Provider) QueryRefund(context.Context, adapter.RefundRequest) (adapter.RefundResult, error) {
	return adapter.RefundResult{}, adapter.ErrUnsupported
}

type channel168Fixture struct {
	s           *Server
	p           *pgxpool.Pool
	token, csrf string
}

func channel168Local(t *testing.T) channel168Fixture {
	t.Helper()
	s, p := groupRoutingFixture152(t)
	s.ProviderFlow.Providers = map[int64]adapter.Provider{7: &channel168Provider{app: "app168-seven"}, 8: &channel168Provider{app: "app168-eight"}}
	s.Sessions = identity.NewPersistentSessions(p, false)
	if _, e := p.Exec(context.Background(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) VALUES('admin168','admin',168,'Synthetic Admin','unused-synthetic-hash','super'),('backup168','admin',169,'Synthetic Backup','unused-synthetic-hash','super')`); e != nil {
		t.Fatal(e)
	}
	token, csrf, e := s.Sessions.Create(identity.User{ID: 168, Role: "admin", Username: "admin168", AdminClass: "super", AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	return channel168Fixture{s, p, token, csrf}
}
func (f channel168Fixture) admin(method, path, body, csrf string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/console/v1/admin/channel-policies"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}
func channel168Data(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("channel policy HTTP=%d want200", w.Code)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("policy response cached")
	}
	var out struct {
		Code int
		Data map[string]any
	}
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil || out.Code != 0 || out.Data == nil {
		t.Fatal("invalid policy envelope", e)
	}
	for _, secret := range []string{"app168-", "seller:", "password", "private_key", "account_scope"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("policy leaked runtime private identity", secret)
		}
	}
	return out.Data
}
func (f channel168Fixture) configure(t *testing.T, id int64, rate string) map[string]any {
	t.Helper()
	v := channel168Data(t, f.admin("GET", "/"+fmt.Sprint(id), "", f.csrf))
	body, _ := json.Marshal(map[string]any{"rate": rate, "mode": 0, "enabled": true, "expected_fingerprint": v["fingerprint"]})
	return channel168Data(t, f.admin("PATCH", "/"+fmt.Sprint(id), string(body), f.csrf))
}
func (f channel168Fixture) group(t *testing.T, rate *string) {
	t.Helper()
	info := map[string]any{}
	if rate != nil {
		info["alipay"] = map[string]string{"rate": *rate}
	}
	raw, _ := json.Marshal(info)
	if _, e := f.p.Exec(context.Background(), `UPDATE backoffice_records SET body=body||jsonb_build_object('rate_bps',200,'info',$1::jsonb) WHERE resource='groups' AND id=0`, string(raw)); e != nil {
		t.Fatal(e)
	}
}
func (f channel168Fixture) pay(t *testing.T, no, money string, channel int64) map[string]any {
	t.Helper()
	p := compat.Params{"pid": "1", "out_trade_no": no, "type": "alipay", "name": "synthetic item168", "money": money, "channel_id": fmt.Sprint(channel), "method": "native", "sign_type": "MD5"}
	p["sign"] = compat.SignMD5(p, "test-secret")
	v := url.Values{}
	for k, x := range p {
		v.Set(k, x)
	}
	r := httptest.NewRequest("POST", "/mapi.php", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	var out map[string]any
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatal("nonJSON payment response", w.Code, e)
	}
	return out
}

func TestChannel168RuntimePolicyStrictCASAndRegistryBinding(t *testing.T) {
	f := channel168Local(t)
	ctx := context.Background()
	list := channel168Data(t, f.admin("GET", "", "", f.csrf))
	if len(list["items"].([]any)) != 2 {
		t.Fatal("catalog is not actual loaded registry")
	}
	before := channel168Data(t, f.admin("GET", "/7", "", f.csrf))
	if before["configured"] != false || before["rate"] != nil || before["revision"] != "0" {
		t.Fatal("unconfigured runtime changed legacy fee semantics", before)
	}
	body := fmt.Sprintf(`{"rate":"99.50","mode":0,"enabled":true,"expected_fingerprint":%q}`, before["fingerprint"])
	for _, tc := range []struct {
		path, body, csrf string
		want             int
	}{
		{"/7", body, "bad", 403}, {"/07", body, f.csrf, 400}, {"/7?channel_id=8", body, f.csrf, 400}, {"/999", body, f.csrf, 404},
		{"/7", strings.Replace(body, `"mode":0`, `"mode":1`, 1), f.csrf, 422},
		{"/7", strings.TrimSuffix(body, "}") + `,"rate":"1"}`, f.csrf, 400},
		{"/7", strings.TrimSuffix(body, "}") + `,"provider":"wxpay"}`, f.csrf, 400},
	} {
		if w := f.admin("PATCH", tc.path, tc.body, tc.csrf); w.Code != tc.want {
			t.Fatal("policy validation", tc.path, w.Code, tc.want)
		}
	}
	for _, bad := range []string{"-1", "100.01", "1.001", "1e2", "NaN", "01.00"} {
		if w := f.admin("PATCH", "/7", strings.Replace(body, "99.50", bad, 1), f.csrf); w.Code != 422 {
			t.Fatal("unsafe rate accepted", bad, w.Code)
		}
	}
	done := make(chan *httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); done <- f.admin("PATCH", "/7", body, f.csrf) }()
	}
	wg.Wait()
	close(done)
	wins := 0
	for w := range done {
		if w.Code == 200 {
			wins++
			v := channel168Data(t, w)
			if v["rate"] != "99.50" || v["fee_rate_bps"] != float64(50) {
				t.Fatal("retained/fee confusion", v)
			}
		} else if w.Code != 409 {
			t.Fatal("concurrent policy status", w.Code)
		}
	}
	if wins != 1 {
		t.Fatal("CAS winner count", wins)
	}
	var count int
	if e := f.p.QueryRow(ctx, "SELECT count(*) FROM backoffice_audit WHERE operation='runtime-channel-policy-update'").Scan(&count); e != nil || count != 1 {
		t.Fatal("policy audit atomicity", count, e)
	}
	// Same-name generic metadata must not configure or execute another runtime.
	if _, e := f.p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('channels','{"name":"app168-seven","provider":"alipay","enabled":true,"rate":"1"}')`); e != nil {
		t.Fatal(e)
	}
	after := channel168Data(t, f.admin("GET", "/8", "", f.csrf))
	if after["configured"] != false {
		t.Fatal("arbitrary directory metadata bound runtime policy")
	}
}

func TestChannel168SelectedParentRateInheritedAndRealPaidEntries(t *testing.T) {
	f := channel168Local(t)
	ctx := context.Background()
	if _, e := f.p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"payment_fee_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	f.configure(t, 7, "99.50")
	f.configure(t, 8, "98.75")
	for i, rate := range []*string{nil, pointer168(""), pointer168("0"), pointer168("0.00"), pointer168("99.00")} {
		f.group(t, rate)
		for _, id := range []int64{7, 8} {
			no := fmt.Sprintf("rate168-%d-%d", i, id)
			out := f.pay(t, no, "100.00", id)
			if out["code"] != float64(1) {
				t.Fatal("local HTTP payment failed", out)
			}
			o, e := f.s.Store.Order(ctx, 1, no)
			want := int64(50)
			if id == 8 {
				want = 125
			}
			if i == 4 {
				want = 100
			}
			if e != nil || o.PlatformFee != want || o.MerchantAmount != 10000-want {
				t.Fatal("fee did not inherit selected parent", i, id, o.PlatformFee, o.MerchantAmount, e)
			}
			if e = f.s.Store.MarkPaid(ctx, o.TradeNo, "paid168:"+o.TradeNo); e != nil {
				t.Fatal(e)
			}
			if e = f.s.Store.MarkPaid(ctx, o.TradeNo, "paid168:"+o.TradeNo); e != nil {
				t.Fatal("duplicate paid callback not idempotent", e)
			}
		}
	}
	var available, fees int64
	if e := f.p.QueryRow(ctx, "SELECT COALESCE(sum(balance) FILTER(WHERE owner_id=1 AND kind='available'),0),COALESCE(sum(balance) FILTER(WHERE owner_id=0 AND kind='fees'),0) FROM accounts").Scan(&available, &fees); e != nil || available != 99100 || fees != 900 {
		t.Fatal("actual success entries not exact or cost charged twice", available, fees, e)
	}
}
func pointer168(v string) *string { return &v }
func create168(p compat.Params, amount int64) payment.CreateOrderInput {
	return payment.CreateOrderInput{MerchantID: 1, OutTradeNo: p["out_trade_no"], Name: p["name"], Type: p["type"], Amount: amount, NotifyURL: p["notify_url"], ReturnURL: p["return_url"], Param: p["param"]}
}

func TestChannel168RateSnapshotCannotSpliceLaterGroupOrParent(t *testing.T) {
	f := channel168Local(t)
	ctx := context.Background()
	if _, e := f.p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"payment_fee_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	f.group(t, pointer168("0"))
	f.configure(t, 7, "99.50")
	p := compat.Params{"type": "alipay", "method": "native", "name": "frozen quote168", "money": "100.00", "clientip": "192.0.2.1", "out_trade_no": "snapshot168", "channel_id": "7"}
	if _, _, e := f.s.providerRoutedRequest(ctx, 1, p); e != nil {
		t.Fatal(e)
	}
	var merchantRevision int64
	if e := f.p.QueryRow(ctx, `SELECT (facts->>'MerchantRevision')::bigint FROM provider_route_fee_quotes WHERE out_trade_no='snapshot168'`).Scan(&merchantRevision); e != nil || merchantRevision <= 0 {
		t.Fatal("quote omitted original merchant configuration revision", merchantRevision, e)
	}
	f.configure(t, 7, "90.00")
	f.group(t, pointer168("80.00"))
	o, e := f.s.Store.CreateOrder(ctx, create168(p, 10000))
	if e != nil || o.PlatformFee != 50 || o.MerchantAmount != 9950 {
		t.Fatal("order spliced new configuration into old route", o.PlatformFee, e)
	}
	replay, e := f.s.Store.CreateOrder(ctx, create168(p, 10000))
	if e != nil || replay.ID != o.ID || replay.PlatformFee != 50 {
		t.Fatal("order replay repriced", e)
	}
	var quotes, terms int
	if e = f.p.QueryRow(ctx, "SELECT (SELECT count(*) FROM provider_route_fee_quotes),(SELECT count(*) FROM order_route_fee_terms)").Scan(&quotes, &terms); e != nil || quotes != 1 || terms != 1 {
		t.Fatal("new route/order fact not singular", quotes, terms, e)
	}
}

func TestChannel168MissingBirthProofCannotDowngradeToHistoricalOrder(t *testing.T) {
	f := channel168Local(t)
	ctx := context.Background()
	f.configure(t, 7, "99.50")
	p := compat.Params{"type": "alipay", "method": "native", "name": "sealed168", "money": "1.00", "clientip": "192.0.2.1", "out_trade_no": "missing-proof168", "channel_id": "7"}
	if _, _, e := f.s.providerRoutedRequest(ctx, 1, p); e != nil {
		t.Fatal(e)
	}
	o, e := f.s.Store.CreateOrder(ctx, create168(p, 100))
	if e != nil {
		t.Fatal(e)
	}
	// Simulate offline deletion while keeping the original immutable route quote.
	if _, e = f.p.Exec(ctx, `ALTER TABLE order_route_fee_terms DISABLE TRIGGER order_route_fee_terms_immutable;DELETE FROM order_route_fee_terms;ALTER TABLE order_route_fee_terms ENABLE TRIGGER order_route_fee_terms_immutable`); e != nil {
		t.Fatal(e)
	}
	if e = f.s.Store.MarkPaid(ctx, o.TradeNo, "corrupt168"); !errors.Is(e, payment.ErrConflict) {
		t.Fatal("new birth proof deletion was reinterpreted as historical", e)
	}
}

func TestChannel168FirstSendAuthenticatesCurrentPolicyProof(t *testing.T) {
	f := channel168Local(t)
	ctx := context.Background()
	f.configure(t, 7, "99.50")
	p := compat.Params{"type": "alipay", "method": "native", "name": "policy proof168", "money": "1.00", "clientip": "192.0.2.1", "out_trade_no": "bad-policy168", "channel_id": "7"}
	if _, _, e := f.s.providerRoutedRequest(ctx, 1, p); e != nil {
		t.Fatal(e)
	}
	o, e := f.s.Store.CreateOrder(ctx, create168(p, 100))
	if e != nil {
		t.Fatal(e)
	}
	state := channel168Data(t, f.admin("GET", "/7", "", f.csrf))
	body := fmt.Sprintf(`{"rate":"99.50","mode":0,"enabled":false,"expected_fingerprint":%q}`, state["fingerprint"])
	channel168Data(t, f.admin("PATCH", "/7", body, f.csrf))
	if _, e = f.p.Exec(ctx, "UPDATE runtime_channel_policies SET enabled=true WHERE channel_id=7"); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProviderFlow.Submit(ctx, o.TradeNo, 7, "native", "192.0.2.1", ""); !errors.Is(e, payment.ErrConflict) {
		t.Fatal("first send trusted unauthenticated policy fields", e)
	}
	if f.s.ProviderFlow.Providers[7].(*channel168Provider).sends.Load() != 0 {
		t.Fatal("corrupt policy reached provider")
	}
}
