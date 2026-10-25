package contract_test

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
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

func TestTransferSearchUXListsCountPaginationCSVAndOwnerUseSameFilters(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	if _, e := pool.Exec(ctx, `UPDATE merchants SET profile='{"transfer_api_enabled":true,"transfer_rate":"0"}' WHERE id IN(1,2); INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"transfer_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	provider := &transferHTTPProvider{}
	flow := payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: provider}, AllowLive: true}
	var records []payment.Transfer
	for _, mid := range []int64{1, 2} {
		o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: mid, OutTradeNo: fmt.Sprintf("search151-fund-%d", mid), Amount: 100000})
		if e != nil {
			t.Fatal(e)
		}
		if e = s.Store.MarkPaid(ctx, o.TradeNo, "search151:"+o.TradeNo); e != nil {
			t.Fatal(e)
		}
		for i := 0; i < 2; i++ {
			v, e := flow.Transfer(ctx, mid, 7, fmt.Sprintf("merchant151-%d-%d", mid, i), adapter.TransferRequest{Type: "alipay", Account: "local151@example.invalid", Amount: int64(103 + i)})
			if e != nil {
				t.Fatal(e)
			}
			records = append(records, v)
		}
	}
	admin, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	merchant, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant"})
	if e != nil {
		t.Fatal(e)
	}
	request := func(role, token string, q url.Values) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", "/console/v1/"+role+"/transfers?"+q.Encode(), nil)
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	list := func(role, token string, q url.Values) ([]map[string]any, int) {
		t.Helper()
		w := request(role, token, q)
		var out struct {
			Data struct {
				Items []map[string]any
				Total int
			}
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("search %d %s", w.Code, w.Body.String())
		}
		return out.Data.Items, out.Data.Total
	}
	var created time.Time
	if e = pool.QueryRow(ctx, "SELECT created_at FROM transfers WHERE id=$1", records[0].ID).Scan(&created); e != nil {
		t.Fatal(e)
	}
	day := created.In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02")
	for _, scope := range []struct{ role, token string }{{"admin", admin}, {"merchant", merchant}} {
		for _, v := range []struct {
			field, value string
			count        int
		}{{"biz_no", records[0].BizNo, 1}, {"out_biz_no", records[0].TransferNo, 1}, {"pay_order_no", "ali-transfer-1", 2}, {"money", "1.03", 1}, {"biz_no", "' OR 1=1--", 0}, {"money", "1.05", 0}} {
			q := url.Values{"merchant_id": {"1"}, "search_field": {v.field}, "q": {v.value}, "channel_id": {"7"}, "status": {"pending"}, "from": {day}, "to": {day}, "limit": {"1"}, "page": {"1"}}
			items, total := list(scope.role, scope.token, q)
			if total != v.count || len(items) != min(v.count, 1) {
				t.Fatalf("wrong filtered quantity %s got %d/%d", v.field, total, len(items))
			}
			for _, r := range items {
				if r["merchant_id"] != "1" {
					t.Fatal("owner predicate replaced")
				}
			}
			q.Set("page", "2")
			q.Set("format", "csv")
			w := request(scope.role, scope.token, q)
			if w.Code != 200 {
				t.Fatalf("CSV %d %s", w.Code, w.Body.String())
			}
			csvRows, e := csv.NewReader(strings.NewReader(strings.TrimPrefix(w.Body.String(), "\ufeff"))).ReadAll()
			if e != nil || len(csvRows) != v.count+1 {
				t.Fatalf("CSV scope differs from quantity %v rows %d", e, len(csvRows))
			}
			for _, r := range csvRows[1:] {
				if r[1] != "1" {
					t.Fatal("CSV crossed merchant")
				}
			}
		}
		for _, q := range []url.Values{{"search_field": {"account"}, "q": {"private"}}, {"search_field": {"money"}, "q": {"1.001"}}, {"search_field": {"biz_no", "money"}, "q": {"1"}}, {"search_field": {"biz_no"}, "q": {"a\x00b"}}, {"unknown": {"x"}}, {"merchant_id": {"01"}}, {"channel_id": {"07"}}, {"page": {"1", "2"}}, {"limit": {"01"}}} {
			if w := request(scope.role, scope.token, q); w.Code != 400 {
				t.Fatal("invalid original filter accepted", w.Code)
			}
		}
	}
	if w := request("merchant", merchant, url.Values{"merchant_id": {"2"}, "search_field": {"money"}, "q": {"1.03"}}); w.Code != 403 {
		t.Fatal("cross merchant filter accepted", w.Code)
	}
	if provider.writes.Load() != 4 {
		t.Fatal("list or CSV called transfer provider")
	}
}
