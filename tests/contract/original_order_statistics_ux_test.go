package contract_test

import (
	"context"
	"encoding/csv"
	"encoding/json"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestOriginalOrderStatisticsUXSevenColumnsFiltersRefundsAndUnknownCost(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	now := time.Now().In(zone)
	businessDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 1, 0, 0, zone).AddDate(0, 0, -1)
	day := businessDate.Format("2006-01-02")
	var refundable string
	for _, in := range []struct {
		mid, amount, channel int64
		kind, no             string
		paid                 bool
	}{
		{1, 1234, 7, "alipay", "original-stat-paid", true},
		{1, 100, 7, "alipay", "original-stat-pending", false},
		{2, 5678, 8, "wxpay", "original-stat-other", true},
		{1, 9, 8, "wxpay", "original-stat-small", true},
	} {
		// Original-day reports cover imported historical births. Current104
		// births authenticate created_at and cannot be backdated for a fixture.
		var trade string
		err := pool.QueryRow(ctx, `INSERT INTO orders(trade_no,merchant_id,out_trade_no,name,type,amount,merchant_amount,platform_fee,fee_policy,refund_fee_payer,created_at)VALUES(nextval('trade_no_seq')::text,$1,$2,'原统计本地合成',$3,$4,$4,0,'none','platform',$5)RETURNING trade_no`, in.mid, in.no, in.kind, in.amount, businessDate).Scan(&trade)
		if err != nil {
			t.Fatal(err)
		}
		o, err := s.Store.OrderByTradeNo(ctx, trade)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO provider_attempts(order_id,channel_id,method,state) VALUES($1,$2,'native','pending')`, o.ID, in.channel); err != nil {
			t.Fatal(err)
		}
		if in.paid {
			if err = s.Store.MarkPaid(ctx, o.TradeNo, "statistics:"+o.TradeNo); err != nil {
				t.Fatal(err)
			}
		}
		if in.no == "original-stat-paid" {
			refundable = o.TradeNo
		}
	}
	for _, in := range []struct {
		no     string
		amount int64
	}{{"statistics-part-one", 123}, {"statistics-part-two", 77}} {
		if _, err := s.Store.ManualRefund(ctx, 1, refundable, in.no, in.amount, payment.IncomeHoldActor{ID: 1, Name: "admin", Class: "super"}, "local statistics fixture", true); err != nil {
			t.Fatal(err)
		}
	}
	admin, _, err := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super"})
	if err != nil {
		t.Fatal(err)
	}
	merchant, _, err := s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant"})
	if err != nil {
		t.Fatal(err)
	}
	get := func(role, token, name string, q url.Values) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", "/console/v1/"+role+"/reports/"+name+"?"+q.Encode(), nil)
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	fingerprint := func() string {
		t.Helper()
		var hash string
		if e := pool.QueryRow(ctx, `SELECT md5((SELECT string_agg(row_to_json(t)::text,'' ORDER BY id) FROM orders t)||(SELECT string_agg(row_to_json(t)::text,'' ORDER BY id) FROM refunds t)||(SELECT string_agg(row_to_json(t)::text,'' ORDER BY id) FROM backoffice_audit t))`).Scan(&hash); e != nil {
			t.Fatal(e)
		}
		return hash
	}
	before := fingerprint()
	var out struct {
		Data struct {
			Items    []map[string]any
			Columns  []struct{ Key, Label string }
			Summary  map[string]any
			From, To string
		}
	}
	read := func(name string, q url.Values) {
		t.Helper()
		w := get("admin", admin, name, q)
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("original %s statistics missing: %d %s", name, w.Code, w.Body.String())
		}
	}
	q := url.Values{"view": {"original"}, "type": {"alipay"}, "channel": {"7"}, "uid": {"1"}}
	read("daily", q)
	if len(out.Data.Items) != 7 || len(out.Data.Columns) != 7 {
		t.Fatalf("original seven days/columns missing: %+v", out.Data)
	}
	found := false
	for _, row := range out.Data.Items {
		if row["day"] == day {
			found = true
			if row["realmoney"] != "12.34" || row["refundmoney"] != "2.00" || row["profitmoney"] != nil || row["orderCount"] != "2" || row["successCount"] != "1" || row["successRate"] != "50.00" || row["missing_costs"] != "1" {
				t.Fatalf("cohort/refund fan-out/filter/unknown profit: %+v", row)
			}
		} else if row["orderCount"] != "0" || row["profitmoney"] != "0.00" {
			t.Fatalf("empty day fabricated or missing: %+v", row)
		}
	}
	if !found {
		t.Fatalf("Beijing creation business day missing: %+v", out.Data.Items)
	}
	q = url.Values{"view": {"original"}, "from": {day}, "to": {day}}
	read("channels", q)
	if len(out.Data.Items) != 2 || out.Data.Summary["channelName"] != "总计" || out.Data.Summary["realmoney"] != "69.21" || out.Data.Summary["refundmoney"] != "2.00" || out.Data.Summary["orderCount"] != "4" || out.Data.Summary["successCount"] != "3" || out.Data.Summary["successRate"] != "75.00" || out.Data.Summary["profitmoney"] != nil {
		t.Fatalf("channel totals/refund fan-out: %+v", out.Data)
	}
	q.Set("uid", "1")
	q.Set("type", "wxpay")
	read("channels", q)
	if len(out.Data.Items) != 1 || out.Data.Items[0]["channel"] != "8" || out.Data.Items[0]["realmoney"] != "0.09" {
		t.Fatalf("original channel filters ignored: %+v", out.Data)
	}
	for _, bad := range []url.Values{{"view": {"original"}, "uid": {"01"}}, {"view": {"original"}, "uid": {"1", "2"}}, {"view": {"original"}, "type": {"not-a-payment"}}, {"view": {"original"}, "channel": {"9223372036854775808"}}, {"view": {"original"}, "from": {"2026-02-30"}}, {"view": {"original"}, "unknown": {"1"}}} {
		if w := get("admin", admin, "channels", bad); w.Code != 400 {
			t.Fatalf("invalid original filters accepted %s: %d", bad.Encode(), w.Code)
		}
	}
	if w := get("merchant", merchant, "daily", url.Values{"view": {"original"}, "uid": {"2"}}); w.Code != 403 {
		t.Fatalf("merchant cross-merchant original report: %d", w.Code)
	}
	if fingerprint() != before {
		t.Fatal("report reads altered existing financial facts or audits")
	}
	q.Set("format", "csv")
	w := get("admin", admin, "channels", q)
	records, e := csv.NewReader(strings.NewReader(strings.TrimPrefix(w.Body.String(), "\ufeff"))).ReadAll()
	if w.Code != 200 || e != nil || len(records) != 3 || records[0][0] != "支付通道" || records[0][3] != "手续费利润" || records[1][0] != "总计" || records[1][1] != "0.09" || records[1][3] != "未知" {
		t.Fatalf("original same-scope full CSV %d %v %+v", w.Code, e, records)
	}
}

type statisticsCostProvider135 struct {
	adapter.Provider
	estimate adapter.PaymentCostEstimate
}

func (p statisticsCostProvider135) ID() string                      { return "wxpay" }
func (p statisticsCostProvider135) ApplicationID() string           { return "statistics135-app" }
func (p statisticsCostProvider135) TransferAccountIdentity() string { return "statistics135-mch" }
func (p statisticsCostProvider135) PaymentCostEstimate() (adapter.PaymentCostEstimate, bool) {
	return p.estimate, true
}

func TestOriginalOrderStatisticsUXExactLargeAmountsAndRoundedSuccessRate(t *testing.T) {
	s, _, _ := fixture(t)
	ctx := context.Background()
	for i, amount := range []int64{9007199254740993, 1, 1} {
		o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: []string{"exact-large", "exact-small", "exact-unpaid"}[i], Name: "exact local statistics", Type: "wxpay", Amount: amount})
		if e != nil {
			t.Fatal(e)
		}
		if i < 2 {
			if e = s.Store.MarkPaid(ctx, o.TradeNo, "exact-statistics:"+o.TradeNo); e != nil {
				t.Fatal(e)
			}
		}
	}
	token, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("GET", "/console/v1/admin/reports/channels?view=original", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	var out struct {
		Data struct{ Summary map[string]any }
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatalf("exact report %d %s", w.Code, w.Body.String())
	}
	if out.Data.Summary["realmoney"] != "90071992547409.94" || out.Data.Summary["orderCount"] != "3" || out.Data.Summary["successRate"] != "66.67" {
		t.Fatalf("large decimal or rounded success ratio lost: %+v", out.Data.Summary)
	}
}

func TestOriginalOrderStatisticsUXUnrecordedConfiguredCostRemainsUnknown(t *testing.T) {
	s, _, _ := fixture(t)
	ctx := context.Background()
	p := statisticsCostProvider135{}
	cost, e := adapter.NewPaymentCostEstimate(p, "0.6")
	if e != nil {
		t.Fatal(e)
	}
	p.estimate = cost
	flow := payment.ProviderFlow{Providers: map[int64]adapter.Provider{7: p}}
	quote, e := flow.PaymentCostQuote(7, "native")
	if e != nil {
		t.Fatal(e)
	}
	o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "statistics-known-cost", Name: "known original estimate", Type: "wxpay", Amount: 1000, PaymentCost: quote})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.MarkPaid(ctx, o.TradeNo, "statistics-known-paid"); e != nil {
		t.Fatal(e)
	}
	token, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("GET", "/console/v1/admin/reports/channels?view=original", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	var out struct {
		Data struct{ Summary map[string]any }
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatalf("known estimate report %d %s", w.Code, w.Body.String())
	}
	// A configured quote is not an immutable birth snapshot. This order has no
	// original profit policy/relation, so the quote was intentionally not stored.
	if out.Data.Summary["profitmoney"] != nil || out.Data.Summary["missing_costs"] != "1" {
		t.Fatalf("unrecorded configured estimate must not reprice historical orders: %+v", out.Data.Summary)
	}
}
