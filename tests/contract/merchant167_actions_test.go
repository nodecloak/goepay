package contract_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"goepay/internal/simulation"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

type merchant167Provider struct {
	adapter.Provider
	queries atomic.Int32
}

func (p *merchant167Provider) Query(ctx context.Context, r adapter.QueryRequest) (adapter.QueryResult, error) {
	p.queries.Add(1)
	return p.Provider.Query(ctx, r)
}
func (p *merchant167Provider) VerifyNotification(ctx context.Context, h http.Header, b []byte, e adapter.ExpectedPayment) (adapter.PaymentEvent, error) {
	return p.Provider.(adapter.NotificationVerifier).VerifyNotification(ctx, h, b, e)
}

func TestMerchant167ActualListCapabilitiesAndQueryKeepOwnerAndDoNotCallProviderOnGET(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	env, e := simulation.New(bytes.Repeat([]byte{167}, 32), "http://127.0.0.1:8167")
	if e != nil {
		t.Fatal(e)
	}
	providers := env.Providers()
	p := &merchant167Provider{Provider: providers[2]}
	providers[2] = p
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: providers, AllowLive: true, LocalSimulation: true, CallbackBaseURL: "http://127.0.0.1:8167"}
	env.Notify = func(ctx context.Context, id int64, trade string, raw []byte) error {
		return s.ProviderFlow.HandleNotification(ctx, id, trade, nil, raw)
	}
	var orders []payment.Order
	for i := 0; i < 3; i++ {
		o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: fmt.Sprintf("merchant167-%d", i), Name: "synthetic167", Type: "wxpay", Amount: 10000, NotifyURL: "https://synthetic.example.invalid/notify"})
		if e != nil {
			t.Fatal(e)
		}
		orders = append(orders, o)
		if i == 2 {
			continue
		}
		attempt, e := s.ProviderFlow.Submit(ctx, o.TradeNo, 2, "native", "127.0.0.1", "")
		if e != nil {
			t.Fatal(e)
		}
		if i == 1 {
			u, _ := url.Parse(attempt.Material)
			w := httptest.NewRecorder()
			env.ServeHTTP(w, httptest.NewRequest("POST", u.RequestURI(), nil))
			if w.Code != 200 {
				t.Fatal("synthetic payment", w.Code, w.Body.String())
			}
		}
	}
	token, csrf, _ := s.Sessions.Create(identity.User{ID: 1, Role: "merchant", Username: "merchant"})
	invoke := func(method, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/console/v1/merchant/"+path, strings.NewReader(`{}`))
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	digest := `SELECT md5((SELECT jsonb_agg(to_jsonb(o) ORDER BY id)::text FROM orders o)||(SELECT jsonb_agg(to_jsonb(a) ORDER BY order_id)::text FROM provider_attempts a)||(SELECT jsonb_agg(to_jsonb(j) ORDER BY id)::text FROM journals j))`
	var before string
	if e = pool.QueryRow(ctx, digest).Scan(&before); e != nil {
		t.Fatal(e)
	}
	w := invoke("GET", "orders")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result struct {
		Data struct{ Items []map[string]any }
	}
	if e = json.Unmarshal(w.Body.Bytes(), &result); e != nil {
		t.Fatal(e)
	}
	rows := map[string]map[string]any{}
	for _, r := range result.Data.Items {
		rows[fmt.Sprint(r["trade_no"])] = r
	}
	caps, ok := rows[orders[0].TradeNo]["merchant_actions"].(map[string]any)
	if !ok {
		t.Fatal("trusted merchant action capability projection missing")
	}
	if caps["query"] != true || caps["refund"] != false || caps["notify"] != false {
		t.Fatal("pending capability", caps)
	}
	paid := rows[orders[1].TradeNo]["merchant_actions"].(map[string]any)
	if paid["notify"] != true || paid["refund"] != true {
		t.Fatal("verified paid capability", paid)
	}
	unknown := rows[orders[2].TradeNo]["merchant_actions"].(map[string]any)
	if unknown["query"] != false || unknown["refund"] != false {
		t.Fatal("unknown provider capabilities fabricated", unknown)
	}
	if p.queries.Load() != 0 {
		t.Fatal("GET queried provider")
	}
	var after string
	if e = pool.QueryRow(ctx, digest).Scan(&after); e != nil || after != before {
		t.Fatal("GET mutated original payment", e)
	}
	for _, secret := range []string{"proof_hash", "private_key", "appsecret", "origin_proof"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("private capability source escaped", secret)
		}
	}
	readCaps := func() map[string]any {
		t.Helper()
		w := invoke("GET", "orders/"+orders[1].TradeNo)
		if w.Code != 200 {
			t.Fatal("fresh permission", w.Code, w.Body.String())
		}
		var fresh struct {
			Data struct {
				MerchantActions map[string]any `json:"merchant_actions"`
			}
		}
		if e := json.Unmarshal(w.Body.Bytes(), &fresh); e != nil {
			t.Fatal(e)
		}
		return fresh.Data.MerchantActions
	}
	if readCaps()["refund"] != true {
		t.Fatal("fresh paid refund missing")
	}
	if _, e = s.Store.FreezeOrderIncome(ctx, 1, orders[1].TradeNo, "merchant167-hold", payment.IncomeHoldActor{ID: 1, Name: "admin", Class: "super"}); e != nil {
		t.Fatal(e)
	}
	if readCaps()["refund"] != true {
		t.Fatal("original funded held refund unreachable")
	}
	partial, e := s.Store.ReserveRefund(ctx, 1, orders[1].OutTradeNo, "merchant167-partial", 100)
	if e != nil {
		t.Fatal(e)
	}
	if readCaps()["refund"] != false {
		t.Fatal("pending refund duplicate action exposed")
	}
	if e = s.Store.ResolveRefund(ctx, partial.ID, "succeeded"); e != nil {
		t.Fatal(e)
	}
	if readCaps()["refund"] != true {
		t.Fatal("partial held refund unreachable")
	}
	if p.queries.Load() != 0 {
		t.Fatal("fresh GET queried provider")
	}
	if q := invoke("POST", "orders/"+orders[0].TradeNo+"/query"); q.Code != 200 || p.queries.Load() != 1 {
		t.Fatal("original query endpoint not single provider query", q.Code, p.queries.Load())
	}
	for _, action := range []string{"mark-paid", "manual-refund", "freeze-income"} {
		if q := invoke("POST", "orders/"+orders[0].TradeNo+"/"+action); q.Code < 400 {
			t.Fatal("merchant obtained privileged action", action, q.Code)
		}
	}
	foreign, _ := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 2, OutTradeNo: "merchant167-foreign", Type: "wxpay", Name: "foreign", Amount: 10000})
	for _, suffix := range []string{"", "/query", "/refund", "/receipt"} {
		method := "GET"
		if suffix == "/query" || suffix == "/refund" {
			method = "POST"
		}
		if q := invoke(method, "orders/"+foreign.TradeNo+suffix); q.Code < 400 {
			t.Fatal("other merchant order action", suffix, q.Code)
		}
	}
	if p.queries.Load() != 1 {
		t.Fatal("cross merchant queried provider")
	}
}

func TestMerchant167LedgerRefundTradeFilterUsesRealReferencesAndSameCSVScope(t *testing.T) {
	s, _, _ := fixture(t)
	ctx := context.Background()
	var target, foreign payment.Order
	for i := 0; i < 3; i++ {
		mid := int64(1)
		if i == 2 {
			mid = 2
		}
		o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: mid, OutTradeNo: fmt.Sprintf("merchant167-ledger-%d", i), Amount: 10000})
		if e != nil {
			t.Fatal(e)
		}
		if e = s.Store.MarkPaid(ctx, o.TradeNo, "merchant167-paid:"+o.TradeNo); e != nil {
			t.Fatal(e)
		}
		if i == 0 {
			target = o
		} else if i == 2 {
			foreign = o
		}
	}
	refund, e := s.Store.ReserveRefund(ctx, 1, target.OutTradeNo, "merchant167-refund", 100)
	if e != nil {
		t.Fatal(e)
	}
	token, _, _ := s.Sessions.Create(identity.User{ID: 1, Role: "merchant", Username: "merchant"})
	invoke := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/console/v1/merchant/"+path, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	w := invoke("ledger?trade_no=" + target.TradeNo)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result struct {
		Data struct {
			Items []map[string]any
			Total int
		}
	}
	if e = json.Unmarshal(w.Body.Bytes(), &result); e != nil {
		t.Fatal(e)
	}
	if len(result.Data.Items) == 0 {
		t.Fatal("known order journals missing")
	}
	for _, r := range result.Data.Items {
		key := fmt.Sprint(r["event_key"])
		if key != "payment:"+target.TradeNo && !strings.HasPrefix(key, fmt.Sprintf("refund:%d:", refund.ID)) && !strings.HasPrefix(key, fmt.Sprintf("refund-funding:%d:", refund.ID)) {
			t.Fatal("trade_no ignored or linked unrelated journal", key)
		}
	}
	csvResponse := invoke("ledger?trade_no=" + target.TradeNo + "&format=csv")
	records, e := csv.NewReader(strings.NewReader(strings.TrimPrefix(csvResponse.Body.String(), "\ufeff"))).ReadAll()
	if csvResponse.Code != 200 || e != nil || len(records) != result.Data.Total+1 {
		t.Fatal("CSV differs from same order filter", csvResponse.Code, e, len(records), result.Data.Total)
	}
	w = invoke("refunds?trade_no=" + target.TradeNo)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if e = json.Unmarshal(w.Body.Bytes(), &result); e != nil {
		t.Fatal(e)
	}
	if result.Data.Total != 1 || result.Data.Items[0]["trade_no"] != target.TradeNo {
		t.Fatal("refund original trade filter", result.Data)
	}
	if w = invoke("ledger?trade_no=not-owned-no-fallback"); w.Code != 200 {
		t.Fatal(w.Code)
	} else {
		if e = json.Unmarshal(w.Body.Bytes(), &result); e != nil || result.Data.Total != 0 {
			t.Fatal("unknown reference broadened", e, result.Data)
		}
	}
	for _, kind := range []string{"ledger", "refunds"} {
		for _, suffix := range []string{"", "&format=csv"} {
			w = invoke(kind + "?trade_no=" + foreign.TradeNo + suffix)
			if w.Code != 200 {
				t.Fatal("foreign filter", w.Code, w.Body.String())
			}
			if suffix == "" {
				if e = json.Unmarshal(w.Body.Bytes(), &result); e != nil || result.Data.Total != 0 {
					t.Fatal("foreign row exposed", e, result.Data)
				}
			} else {
				records, e = csv.NewReader(strings.NewReader(strings.TrimPrefix(w.Body.String(), "\ufeff"))).ReadAll()
				if e != nil || len(records) != 1 {
					t.Fatal("foreign CSV exposed", e, len(records))
				}
			}
		}
		for _, query := range []string{"trade_no=" + target.TradeNo + "&trade_no=" + foreign.TradeNo, "trade_no=" + target.TradeNo + "&q=other", "trade_no=" + target.TradeNo + "&search_field=name"} {
			if w = invoke(kind + "?" + query); w.Code != 400 {
				t.Fatal("ambiguous filter accepted", kind, w.Code)
			}
		}
	}
	adminToken, _, e := s.Sessions.Create(identity.User{ID: 1, Role: "admin", Username: "admin", AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	for _, suffix := range []string{"", "&format=csv"} {
		r := httptest.NewRequest("GET", "/console/v1/admin/ledger?trade_no="+foreign.TradeNo+suffix, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: adminToken})
		w = httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal("admin filter", w.Code, w.Body.String())
		}
		if suffix == "" {
			if e = json.Unmarshal(w.Body.Bytes(), &result); e != nil || result.Data.Total == 0 {
				t.Fatal("admin exact filter", e, result.Data)
			}
			for _, row := range result.Data.Items {
				if row["event_key"] != "payment:"+foreign.TradeNo {
					t.Fatal("admin filter broadened", row)
				}
			}
		} else {
			records, e = csv.NewReader(strings.NewReader(strings.TrimPrefix(w.Body.String(), "\ufeff"))).ReadAll()
			if e != nil || len(records) != result.Data.Total+1 {
				t.Fatal("admin CSV mismatch", e, len(records), result.Data.Total)
			}
		}
	}
}
