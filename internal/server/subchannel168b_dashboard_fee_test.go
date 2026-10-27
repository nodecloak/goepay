package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func child168BDashboard(t *testing.T, f channel168Fixture, token, csrf string) map[string]any {
	t.Helper()
	w := child168BMerchantRequest(f, token, csrf, "GET", "dashboard", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var out struct{ Data map[string]any }
	if json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatal("dashboard JSON")
	}
	return out.Data
}
func TestSubchannel168BDashboardUsesEachEffectiveParentFeeWithoutRepricingPaidOrders(t *testing.T) {
	f, factory, _ := subchannel168BProductLocal(t)
	f.s.MerchantChannels[1] = []int64{7, 7}
	f.configure(t, 7, "99.50")
	ctx := context.Background()
	if _, e := f.p.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"rate_bps":0,"info":{"alipay":{"channel":"-2","type":"channel","rate":"0"}}}'::jsonb WHERE resource='groups' AND id=0`); e != nil {
		t.Fatal(e)
	}
	token, csrf := child168BMerchantSession(t, f, 1)
	fixtures := map[string]any{"synthetic_only": true}
	setFees := func(v bool) {
		t.Helper()
		tag, e := f.p.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{value}',to_jsonb($1::text)) WHERE resource='settings' AND body->>'key'='payment_fee_enabled'`, fmt.Sprint(v))
		if e != nil {
			t.Fatal(e)
		}
		if tag.RowsAffected() == 0 {
			if _, e = f.p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key','payment_fee_enabled','value',$1::text))`, fmt.Sprint(v)); e != nil {
				t.Fatal(e)
			}
		}
	}

	check := func(stage string, want *int64) {
		t.Helper()
		data := child168BDashboard(t, f, token, csrf)
		fixtures[stage] = data
		rows := data["payment_channels"].([]any)
		if len(rows) != 1 {
			t.Fatal("own channel dedup/scope", rows)
		}
		row := rows[0].(map[string]any)
		if row["channel_id"] != "7" || row["type"] != "alipay" {
			t.Fatal(row)
		}
		actual, ok := row["fee_rate_bps"]
		if !ok || (want == nil && actual != nil) || (want != nil && actual != float64(*want)) {
			t.Errorf("%s: actual parent fee absent or fabricated: got=%v want=%v", stage, actual, want)
		}
		raw, _ := json.Marshal(data)
		for _, secret := range []string{"synthetic-only-secret", "products-v1", "identity_hash", "account_scope"} {
			if strings.Contains(string(raw), secret) {
				t.Fatal("dashboard leaked private runtime identity", secret)
			}
		}
	}
	zero, fifty, thousand, hundred := int64(0), int64(50), int64(1000), int64(100)
	setFees(false)
	check("off", &zero)
	setFees(true)
	check("parent50", &fifty)
	if out := f.pay(t, "dashboard168b-paid100", "100.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	o, e := f.s.Store.Order(ctx, 1, "dashboard168b-paid100")
	if e != nil {
		t.Fatal(e)
	}
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/callbacks/providers/7/"+o.TradeNo, strings.NewReader("notify:products-v1")))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	f.configure(t, 7, "90.00")
	check("parent1000", &thousand)
	old, e := f.s.Store.Order(ctx, 1, o.OutTradeNo)
	if e != nil || old.PlatformFee != 50 || old.Status != "paid" {
		t.Fatal("current fee projection repriced prior paid fee", old.PlatformFee, old.Status, e)
	}
	if _, e = f.p.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{info,alipay,rate}','"99"'::jsonb) WHERE resource='groups' AND id=0`); e != nil {
		t.Fatal(e)
	}
	check("group100", &hundred)
	if _, e = f.p.Exec(ctx, `UPDATE runtime_channel_policies SET proof_hash=decode(repeat('00',32),'hex') WHERE channel_id=7`); e != nil {
		t.Fatal(e)
	}
	check("unknown", nil)
	if factory.providers["products-v1"].creates.Load() != 1 || factory.providers["products-v1"].queries.Load() != 0 || f.s.ProviderFlow.Providers[7].(*channel168Provider).sends.Load() != 0 {
		t.Fatal("dashboard GET called provider")
	}
	if target := os.Getenv("GOEPAY_SUBCHANNEL168B_DASHBOARD_FIXTURE"); target != "" {
		raw, _ := json.MarshalIndent(fixtures, "", "  ")
		if e = os.WriteFile(target, raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
}
func TestSubchannel168BDashboardFeesRemainOwnedAndBatched(t *testing.T) {
	f, _ := subchannel168BLocal(t)
	ctx := context.Background()
	if err := f.s.Store.CreateMerchant(ctx, payment.Merchant{ID: 2, Name: "Synthetic fee owner", APIKey: "synthetic-fee-owner"}); err != nil {
		t.Fatal(err)
	}
	token, csrf := child168BMerchantSession(t, f, 1)
	other, otherCSRF := child168BMerchantSession(t, f, 2)
	f.s.ProviderFlow.Providers[8] = &channel168Provider{app: "fee-other"}
	f.s.MerchantChannels[1] = []int64{7}
	f.s.MerchantChannels[2] = []int64{8}
	for _, tc := range []struct{ token, csrf, want string }{{token, csrf, "7"}, {other, otherCSRF, "8"}} {
		data := child168BDashboard(t, f, tc.token, tc.csrf)
		rows := data["payment_channels"].([]any)
		if len(rows) != 1 || rows[0].(map[string]any)["channel_id"] != tc.want {
			t.Fatal("dashboard crossed registry allowlist", rows)
		}
	}
	r := httptest.NewRequest("GET", "/console/v1/admin/dashboard", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "payment_channels") {
		t.Fatal("administrator overview borrowed a merchant channel catalog")
	}
	tag, err := f.p.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{value}','"true"'::jsonb) WHERE resource='settings' AND body->>'key'='payment_fee_enabled'`)
	if err != nil {
		t.Fatal(err)
	}
	if tag.RowsAffected() == 0 {
		if _, err = f.p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"payment_fee_enabled","value":"true"}'::jsonb)`); err != nil {
			t.Fatal(err)
		}
	}
	f.configure(t, 7, "99.50")
	f.configure(t, 8, "90.00")
	if _, e := f.p.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{info}', '{"alipay":{"channel":"-2","type":"channel","rate":"0"}}'::jsonb) WHERE resource='groups' AND id=0`); e != nil {
		t.Fatal(e)
	}
	f.s.MerchantChannels[1] = []int64{7, 8, 7}
	mixed := child168BDashboard(t, f, token, csrf)
	mixedRows := mixed["payment_channels"].([]any)
	if len(mixedRows) != 2 || mixedRows[0].(map[string]any)["fee_rate_bps"] != float64(50) || mixedRows[1].(map[string]any)["fee_rate_bps"] != float64(1000) || mixed["fee_schedule"].(map[string]any)["rate_bps"] != nil {
		t.Fatal("different parent policies flattened into one rate", mixedRows)
	}
	f.s.MerchantChannels[1] = []int64{7}
	counter := &subchannel168BQueryCounter{}
	cfg := f.p.Config()
	cfg.ConnConfig.Tracer = counter
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	store, e := payment.NewWithKey(pool, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	f.s.Pool = pool
	f.s.Store = store
	f.s.ProviderFlow.Store = store
	counter.n.Store(0)
	child168BDashboard(t, f, token, csrf)
	one := counter.n.Load()
	for i := int64(8); i <= 26; i++ {
		f.s.ProviderFlow.Providers[i] = &channel168Provider{app: fmt.Sprintf("fee-parent-%d", i)}
		f.s.MerchantChannels[1] = append(f.s.MerchantChannels[1], i)
	}
	counter.n.Store(0)
	data := child168BDashboard(t, f, token, csrf)
	many := counter.n.Load()
	if len(data["payment_channels"].([]any)) != 20 || one != many {
		t.Fatal("current fee projection issued per-parent SQL", one, many)
	}
	t.Logf("dashboard 1 vs 20 real parent bindings: %d vs %d queries", one, many)
}
