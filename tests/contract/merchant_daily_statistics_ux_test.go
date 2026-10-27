package contract_test

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestMerchantDailyStatisticsUXOriginalFieldsWeightedFooterAndPrivateScope(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	var gid int64
	if e := pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"daily137 fee","enabled":true,"rate_bps":"1000"}') RETURNING id`).Scan(&gid); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.Exec(ctx, `UPDATE merchants SET profile=jsonb_set(profile,'{group_id}',to_jsonb($1::bigint::text)) WHERE id=1;`, gid); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"payment_fee_enabled","value":"true"}'),('settings','{"key":"refund_fee_payer","value":"platform"}')`); e != nil {
		t.Fatal(e)
	}
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	now := time.Now().In(zone)
	at := time.Date(now.Year(), now.Month(), now.Day(), 0, 1, 0, 0, zone).AddDate(0, 0, -1)
	var refundable string
	for _, in := range []struct {
		mid, amount int64
		kind, no    string
		paid        bool
	}{{1, 10000, "alipay", "daily137-alipay", true}, {1, 50, "wxpay", "daily137-wxpay", true}, {1, 9999, "wxpay", "daily137-pending", false}, {2, 900000, "wxpay", "daily137-foreign", true}} {
		// This report explicitly covers imported historical orders. Create their
		// original timestamp and retained amount before settling/refunding them;
		// never mutate a new104 authenticated birth to manufacture history.
		net, rate, feePolicy := in.amount, int64(0), "none"
		if in.mid == 1 {
			net, rate, feePolicy = in.amount*9/10, 1000, "merchant_deduct"
		}
		var trade string
		e := pool.QueryRow(ctx, `INSERT INTO orders(trade_no,merchant_id,out_trade_no,name,type,amount,merchant_amount,platform_fee,fee_rate_bps,fee_policy,refund_fee_payer,created_at)VALUES(nextval('trade_no_seq')::text,$1,$2,'merchant daily original',$3,$4,$5,$4::bigint-$5::bigint,$6,$7,'platform',$8)RETURNING trade_no`, in.mid, in.no, in.kind, in.amount, net, rate, feePolicy, at).Scan(&trade)
		if e != nil {
			t.Fatal(e)
		}
		o, e := s.Store.OrderByTradeNo(ctx, trade)
		if e != nil {
			t.Fatal(e)
		}
		if in.paid {
			if e = s.Store.MarkPaid(ctx, o.TradeNo, "daily137:"+o.TradeNo); e != nil {
				t.Fatal(e)
			}
		}
		if in.no == "daily137-alipay" {
			refundable = o.TradeNo
		}
	}
	if _, e := s.Store.ManualRefund(ctx, 1, refundable, "daily137-partial", 1200, payment.IncomeHoldActor{ID: 1, Name: "admin", Class: "super"}, "local merchant daily fixture", true); e != nil {
		t.Fatal(e)
	}
	token, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant"})
	if e != nil {
		t.Fatal(e)
	}
	get := func(q url.Values) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/console/v1/merchant/reports/daily?"+q.Encode(), nil)
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	q := url.Values{"view": {"original"}}
	w := get(q)
	var out struct {
		Data struct {
			Items   []map[string]any
			Summary map[string]any
			Columns []struct{ Key, Label string }
		}
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatalf("original merchant daily missing: %d %s", w.Code, w.Body.String())
	}
	if len(out.Data.Items) != 7 || len(out.Data.Columns) != 7 || out.Data.Columns[1].Label != "订单金额" || out.Data.Columns[2].Key != "getmoney" || out.Data.Columns[2].Label != "分成金额" {
		t.Fatalf("wrong merchant seven columns: %+v", out.Data)
	}
	want := map[string]any{"day": "合计", "realmoney": "100.50", "getmoney": "90.45", "refundmoney": "12.00", "orderCount": "3", "successCount": "2", "successRate": "66.67"}
	for key, value := range want {
		if out.Data.Summary[key] != value {
			t.Fatalf("original immutable net/weighted footer %s=%v want%v: %+v", key, out.Data.Summary[key], value, out.Data.Summary)
		}
	}
	if strings.Contains(w.Body.String(), "profitmoney") || strings.Contains(w.Body.String(), "missing_costs") || strings.Contains(w.Body.String(), "cost_estimates") {
		t.Fatalf("merchant received private administrator cost metadata: %s", w.Body.String())
	}
	for _, key := range []string{"uid", "channel"} {
		for _, value := range []string{"", "1", "2"} {
			bad := url.Values{"view": {"original"}, key: {value}}
			if w = get(bad); w.Code != 403 {
				t.Fatalf("merchant accepted administrator %s=%q: %d", key, value, w.Code)
			}
		}
	}
	var audits int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE resource='reports/daily-original'`).Scan(&audits); e != nil || audits != 0 {
		t.Fatalf("normal report wrote audit: %d %v", audits, e)
	}
	q.Set("type", "alipay")
	q.Set("format", "csv")
	w = get(q)
	rows, e := csv.NewReader(strings.NewReader(strings.TrimPrefix(w.Body.String(), "\ufeff"))).ReadAll()
	if w.Code != 200 || e != nil || len(rows) != 9 || rows[0][2] != "分成金额" || rows[8][0] != "合计" || rows[8][1] != "100.00" || rows[8][2] != "90.00" || rows[8][3] != "12.00" {
		t.Fatalf("same scope seven days and footer CSV %d %v %+v", w.Code, e, rows)
	}
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE resource='reports/daily-original' AND operation='export-csv' AND actor_role='merchant' AND actor_id=1`).Scan(&audits); e != nil || audits != 1 {
		t.Fatalf("CSV audit not exact once: %d %v", audits, e)
	}
	if w = get(url.Values{}); w.Code != 200 || !strings.Contains(w.Body.String(), "net_money") {
		t.Fatalf("legacy default merchant report removed: %d %s", w.Code, w.Body.String())
	}
}

func TestMerchantDailyStatisticsUXDoesNotReadUpstreamCostTable(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "daily137-private-unavailable", Name: "local private table isolation", Type: "wxpay", Amount: 9007199254740993})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.MarkPaid(ctx, o.TradeNo, "daily137-private-paid"); e != nil {
		t.Fatal(e)
	}
	// Only this fixture's random schema is changed, after its valid births. A
	// merchant report must not query the unrelated private provider cost table.
	if _, e = pool.Exec(ctx, `ALTER TABLE referral_profit_costs RENAME TO daily137_private_cost_unavailable`); e != nil {
		t.Fatal(e)
	}
	token, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant"})
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("GET", "/console/v1/merchant/reports/daily?view=original", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	var out struct {
		Data struct{ Summary map[string]any }
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Data.Summary["getmoney"] != "90071992547409.93" {
		t.Fatalf("merchant query depended on private cost table or lost exact cents: %d %s", w.Code, w.Body.String())
	}
}
