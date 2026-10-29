package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"goepay/internal/compat"
)

// Pro's order list shows 网站域名 under 商户号 and 插件名称 under 支付方式. The list
// projection must expose both: domain = the host of the order's notify_url (else
// return_url), plugin = the configured name of the channel the attempt used.
func TestOrderListProjectsDomainAndPlugin(t *testing.T) {
	f, _, _ := subchannel168BProductLocal(t)
	if out := subchannel168BProductPay(t, f, "domaincol-1", "native", compat.Params{
		"notify_url": "https://shop.domaincol.example/pay/notify?x=1",
		"return_url": "https://return.domaincol.example/done",
	}); out["code"] != float64(1) {
		t.Fatal(out)
	}
	r := httptest.NewRequest("GET", "/console/v1/admin/orders", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	var v struct {
		Data struct{ Items []map[string]any }
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil || len(v.Data.Items) != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
	row := v.Data.Items[0]
	// domain = notify_url host, never the full URL or a guessed value
	if got, _ := row["domain"].(string); got != "shop.domaincol.example" {
		t.Fatalf("domain = %q, want shop.domaincol.example", row["domain"])
	}
	// plugin is always present (empty when the channel record has no name)
	if _, ok := row["plugin"].(string); !ok {
		t.Fatalf("plugin column missing: %#v", row["plugin"])
	}
}

// With no notify_url the domain falls back to the return_url host; with neither
// it is empty — never fabricated. The legacy /mapi.php wire now requires
// notify_url (like the original Pay.php), so these orders are imported
// directly, the way riskcheck177 imports legacy history.
func TestOrderListDomainFallsBackToReturnURL(t *testing.T) {
	f, _, _ := subchannel168BProductLocal(t)
	ctx := context.Background()
	var mid int64
	if e := f.s.Pool.QueryRow(ctx, `SELECT id FROM merchants ORDER BY id LIMIT 1`).Scan(&mid); e != nil {
		t.Fatal(e)
	}
	for _, o := range []struct{ no, notify, ret string }{
		{"domaincol-2", "", "https://only-return.example/back"},
		{"domaincol-3", "", ""},
	} {
		if _, e := f.s.Pool.Exec(ctx, `INSERT INTO orders(trade_no,merchant_id,out_trade_no,name,type,amount,merchant_amount,platform_fee,fee_policy,refund_fee_payer,notify_url,return_url) VALUES(nextval('trade_no_seq')::text,$1,$2,'domain fallback','alipay',100,100,0,'none','platform',$3,$4)`, mid, o.no, o.notify, o.ret); e != nil {
			t.Fatal(o.no, e)
		}
	}
	r := httptest.NewRequest("GET", "/console/v1/admin/orders", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	var v struct {
		Data struct{ Items []map[string]any }
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil || len(v.Data.Items) != 2 {
		t.Fatal(w.Code, w.Body.String())
	}
	got := map[string]any{}
	for _, row := range v.Data.Items {
		got[row["out_trade_no"].(string)] = row["domain"]
	}
	if got["domaincol-2"] != "only-return.example" {
		t.Fatalf("domain fallback = %#v, want only-return.example", got["domaincol-2"])
	}
	if got["domaincol-3"] != "" {
		t.Fatalf("domain without either URL = %#v, want empty", got["domaincol-3"])
	}
}

// 网站域名 is a selectable order search field (Pro's bill/domain search column):
// search_field=domain matches the same host the list projects, so an admin can
// find every order of one website. The match is a case-insensitive substring,
// like the original domain search.
func TestOrderListDomainSearchFiltersByHost(t *testing.T) {
	f, _, _ := subchannel168BProductLocal(t)
	ctx := context.Background()
	var mid int64
	if e := f.s.Pool.QueryRow(ctx, `SELECT id FROM merchants ORDER BY id LIMIT 1`).Scan(&mid); e != nil {
		t.Fatal(e)
	}
	for _, o := range []struct{ no, notify string }{
		{"dsearch-1", "https://shop.alpha.example/notify"},
		{"dsearch-2", "https://shop.beta.example/notify"},
	} {
		if _, e := f.s.Pool.Exec(ctx, `INSERT INTO orders(trade_no,merchant_id,out_trade_no,name,type,amount,merchant_amount,platform_fee,fee_policy,refund_fee_payer,notify_url,return_url) VALUES(nextval('trade_no_seq')::text,$1,$2,'domain search','alipay',100,100,0,'none','platform',$3,'')`, mid, o.no, o.notify); e != nil {
			t.Fatal(o.no, e)
		}
	}
	read := func(query string) map[string]any {
		t.Helper()
		r := httptest.NewRequest("GET", "/console/v1/admin/orders?"+query, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		var v struct {
			Data struct {
				Items []map[string]any
				Total float64
			}
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil {
			t.Fatal(w.Code, w.Body.String())
		}
		out := map[string]any{"total": v.Data.Total}
		for _, row := range v.Data.Items {
			out[row["out_trade_no"].(string)] = true
		}
		return out
	}
	// Exact host matches only its own order.
	one := read("search_field=domain&q=shop.alpha.example")
	if one["total"] != float64(1) || one["dsearch-1"] != true || one["dsearch-2"] == true {
		t.Fatalf("domain search for shop.alpha.example = %#v", one)
	}
	// Case-insensitive substring across the shared suffix matches both.
	both := read("search_field=domain&q=SHOP.")
	if both["total"] != float64(2) || both["dsearch-1"] != true || both["dsearch-2"] != true {
		t.Fatalf("domain substring search = %#v", both)
	}
}
