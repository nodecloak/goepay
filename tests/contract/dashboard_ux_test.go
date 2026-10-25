package contract_test

import (
	"context"
	"encoding/json"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDashboardUXOriginalOverviewUsesScopedRealFactsAndSevenDays(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	for _, entry := range []struct {
		mid, amount int64
		typ, no     string
	}{{1, 123, "alipay", "dashboard-own"}, {2, 900, "wxpay", "dashboard-other"}} {
		o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: entry.mid, OutTradeNo: entry.no, Name: "本地仪表盘", Type: entry.typ, Amount: entry.amount})
		if e != nil {
			t.Fatal(e)
		}
		if e = s.Store.MarkPaid(ctx, o.TradeNo, entry.no+"-paid"); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "dashboard-unpaid", Name: "本地未支付", Type: "wxpay", Amount: 500}); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.Exec(ctx, `INSERT INTO backoffice_records(resource,owner_id,body) VALUES('announcements',NULL,'{"title":"本地公告","content":"纯文本公告","audience":"merchant","enabled":true}'),('announcements',NULL,'{"title":"管理公告","content":"管理员内容","audience":"admin","enabled":true}'),('complaints',1,'{"status":"open","amount":"123","pay_type":"alipay"}')`); e != nil {
		t.Fatal(e)
	}
	get := func(role, query string) *httptest.ResponseRecorder {
		token, _, e := s.Sessions.Create(identity.User{ID: 1, Username: role, Role: role, AdminClass: "super"})
		if e != nil {
			t.Fatal(e)
		}
		r := httptest.NewRequest("GET", "/console/v1/"+role+"/dashboard"+query, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	w := get("merchant", "")
	if w.Code != 200 {
		t.Fatalf("merchant dashboard %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Data struct {
			Orders        int64            `json:"orders"`
			Paid          int64            `json:"paid_orders"`
			Summary       map[string]any   `json:"summary"`
			Trend         []map[string]any `json:"trend"`
			Mix           []map[string]any `json:"payment_mix"`
			Announcements []map[string]any `json:"announcements"`
			Timezone      string           `json:"timezone"`
			Source        string           `json:"source"`
			Todos         map[string]any   `json:"todos"`
		}
	}
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	if out.Data.Summary["today_money"] != "1.23" || out.Data.Summary["today_orders"] != "2" || out.Data.Summary["today_paid_orders"] != "1" || out.Data.Orders != 2 || out.Data.Paid != 1 {
		t.Fatalf("scope or paid facts %s", w.Body.String())
	}
	if len(out.Data.Trend) != 7 || out.Data.Timezone != "Asia/Shanghai" || out.Data.Source != "local_ledger_snapshot" {
		t.Fatalf("original trend missing %s", w.Body.String())
	}
	if len(out.Data.Mix) != 1 || out.Data.Mix[0]["type"] != "alipay" || out.Data.Mix[0]["amount"] != "1.23" {
		t.Fatalf("mix includes another merchant or unpaid order %v", out.Data.Mix)
	}
	if len(out.Data.Announcements) != 1 || out.Data.Announcements[0]["title"] != "本地公告" || out.Data.Todos != nil {
		t.Fatalf("admin data leaked %s", w.Body.String())
	}
	w = get("admin", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	if out.Data.Summary["today_money"] != "10.23" || out.Data.Todos["complaints"] != "1" || out.Data.Summary["total_users"] != "2" {
		t.Fatalf("admin original summary %s", w.Body.String())
	}
	var full map[string]any
	if e := json.Unmarshal(w.Body.Bytes(), &full); e != nil {
		t.Fatal(e)
	}
	data := full["data"].(map[string]any)
	fees, ok := data["fee_trend"].([]any)
	if !ok || len(fees) != 2 {
		t.Fatalf("missing fee trend %v", data["fee_trend"])
	}
	for _, f := range fees {
		row := f.(map[string]any)
		if row["estimated_profit"] != nil || row["missing_cost_orders"] != "1" {
			t.Fatalf("invented missing upstream cost %v", row)
		}
	}
	w = get("merchant", "")
	full = map[string]any{}
	if e := json.Unmarshal(w.Body.Bytes(), &full); e != nil {
		t.Fatal(e)
	}
	merchantData := full["data"].(map[string]any)
	schedule := merchantData["fee_schedule"].(map[string]any)
	if schedule["state"] != "disabled" || schedule["rate_bps"] != "0" {
		t.Fatalf("invented fee without enabled policy %v", schedule)
	}
	if _, exists := merchantData["api_key"]; exists {
		t.Fatal("dashboard leaked merchant credential")
	}
	var orders, audits int
	if e := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM orders),(SELECT count(*) FROM backoffice_audit)`).Scan(&orders, &audits); e != nil || orders != 3 || audits != 0 {
		t.Fatalf("dashboard wrote business data %d %d %v", orders, audits, e)
	}
	if w = get("merchant", "?merchant_id=2"); w.Code != 400 {
		t.Fatalf("dashboard accepted arbitrary tenant query %d", w.Code)
	}
}

func TestDashboardUXApprovedSettlementIsNotSuccessfulPayout(t *testing.T) {
	s, _, _ := fixture(t)
	ctx := context.Background()
	o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "dashboard-settlement-cash", Name: "本地测试", Type: "alipay", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.MarkPaid(ctx, o.TradeNo, "dashboard-settlement-paid"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Store.SetSettlementRecipient(ctx, 1, payment.SettlementRecipientInput{Type: "alipay", Account: "dashboard-payee@example.invalid", Name: "Local Payee"}, nil, payment.SettlementRecipientActor{Role: "merchant", ID: 1, Name: "merchant"}); e != nil {
		t.Fatal(e)
	}
	request, e := s.Store.RequestFunds(ctx, "settlements", 1, 0, "dashboard-settlement-request", 100, "merchant:1")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Store.ReviewFundsRequest(ctx, request.ID, true, "admin:1"); e != nil {
		t.Fatal(e)
	}
	token, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant"})
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("GET", "/console/v1/merchant/dashboard", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var out struct {
		Data struct {
			Summary     map[string]string `json:"summary"`
			Settlements []map[string]any  `json:"recent_settlements"`
			Available   string            `json:"available_money"`
			Frozen      string            `json:"frozen_money"`
		}
	}
	if e = json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	if out.Data.Summary["settled_money"] != "0.00" || out.Data.Summary["today_settled_money"] != "0.00" || len(out.Data.Settlements) != 1 || out.Data.Settlements[0]["status"] != "approved" || out.Data.Settlements[0]["payout_status"] != "" || out.Data.Available != "9.00" || out.Data.Frozen != "1.00" {
		t.Fatalf("approval reported as successful cash %s", w.Body.String())
	}
}

func TestDashboardUXFrozenOrderIncomeRemainsVisibleInFrozenMoney(t *testing.T){
	s,_,_:=fixture(t);ctx:=context.Background()
	o,e:=s.Store.CreateOrder(ctx,payment.CreateOrderInput{MerchantID:1,OutTradeNo:"dashboard-order-income",Name:"本地冻结",Type:"alipay",Amount:123});if e!=nil{t.Fatal(e)};if e=s.Store.MarkPaid(ctx,o.TradeNo,"dashboard-order-income-paid");e!=nil{t.Fatal(e)}
	if _,e=s.Store.FreezeOrderIncome(ctx,1,o.TradeNo,"dashboard-income-freeze",payment.IncomeHoldActor{ID:1,Name:"admin",Class:"super"});e!=nil{t.Fatal(e)}
	token,_,e:=s.Sessions.Create(identity.User{ID:1,Username:"merchant",Role:"merchant"});if e!=nil{t.Fatal(e)}
	r:=httptest.NewRequest("GET","/console/v1/merchant/dashboard",nil);r.AddCookie(&http.Cookie{Name:"goepay_merchant",Value:token});w:=httptest.NewRecorder();s.Handler().ServeHTTP(w,r)
	if w.Code!=200{t.Fatal(w.Code,w.Body.String())}
	var out struct{Data struct{Available string `json:"available_money"`;Frozen string `json:"frozen_money"`}}
	if e=json.Unmarshal(w.Body.Bytes(),&out);e!=nil{t.Fatal(e)};if out.Data.Available!="0.00"||out.Data.Frozen!="1.23"{t.Fatalf("held order income disappeared %s",w.Body.String())}
}
