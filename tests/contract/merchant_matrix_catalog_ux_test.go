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
	"strings"
	"testing"
)

// All I/O methods are deliberately absent (embedded nil interface). A report
// must only inspect these local capability declarations, never call a provider.
type matrixCatalogProvider struct {
	adapter.Provider
	kind string
}

func (p matrixCatalogProvider) ID() string { return p.kind }
func (p matrixCatalogProvider) Capabilities() adapter.Capabilities {
	return adapter.Capabilities{Methods: []string{"native"}}
}

type matrixCatalogTransfer struct{ matrixCatalogProvider }

func (p matrixCatalogTransfer) TransferTypes() []string { return []string{"alipay", "bank"} }
func (p matrixCatalogTransfer) Transfer(context.Context, adapter.TransferRequest) (adapter.TransferResult, error) {
	panic("report called transfer")
}
func (p matrixCatalogTransfer) QueryTransfer(context.Context, adapter.TransferRequest) (adapter.TransferResult, error) {
	panic("report queried transfer")
}

func TestMerchantMatrixUXConfiguredZeroDimensionsRemainInJSONSortAndCSV(t *testing.T) {
	s, pool, _ := fixture(t)
	const large int64 = 9223372036854775807
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{large: matrixCatalogProvider{kind: "wxpay"}, 7: matrixCatalogTransfer{matrixCatalogProvider{kind: "alipay"}}, 8: nil, -1: matrixCatalogProvider{kind: "alipay"}}}
	token, _, err := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super"})
	if err != nil {
		t.Fatal(err)
	}
	get := func(name, query string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", "/console/v1/admin/reports/"+name+"?view=matrix&"+query, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	var out struct {
		Data struct {
			Columns []struct{ Key, Label string }
			Summary map[string]any
			Items   []map[string]any
			Total   int
		}
	}
	parse := func(name, query string) {
		t.Helper()
		out.Data.Summary = nil
		out.Data.Items = nil
		out.Data.Columns = nil
		w := get(name, query)
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("catalog matrix %d %s", w.Code, w.Body.String())
		}
	}
	parse("merchants", "method=channel&sort=channel_9223372036854775807")
	if out.Data.Summary["channel_9223372036854775807"] != "0.00" || out.Data.Summary["channel_7"] != "0.00" || out.Data.Total != 0 || len(out.Data.Items) != 0 {
		t.Fatalf("configured zero channels missing or merchant rows fabricated: %+v", out.Data)
	}
	parse("merchants", "method=type&type=3")
	if out.Data.Summary["type_wxpay"] != "0.00" || out.Data.Summary["type_alipay"] != "0.00" {
		t.Fatalf("empty cost dimensions must be zero, not missing cost: %+v", out.Data)
	}
	parse("transfer-merchants", "method=channel&sort=channel_7")
	if out.Data.Summary["channel_7"] != "0.00" {
		t.Fatal("transfer-capable zero channel missing")
	}
	if _, ok := out.Data.Summary["channel_9223372036854775807"]; ok {
		t.Fatal("payment-only channel shown as a payout channel")
	}
	parse("transfer-merchants", "method=type")
	if out.Data.Summary["type_bank"] != "0.00" || out.Data.Summary["type_alipay"] != "0.00" {
		t.Fatal("declared transfer types missing")
	}
	w := get("merchants", "method=channel&format=csv")
	rows, e := csv.NewReader(strings.NewReader(strings.TrimPrefix(w.Body.String(), "\ufeff"))).ReadAll()
	if w.Code != 200 || e != nil || len(rows) != 2 || len(rows[0]) != 4 || !strings.Contains(rows[0][3], "9223372036854775807") || rows[1][3] != "0.00" {
		t.Fatalf("zero dimension CSV %d %v %#v", w.Code, e, rows)
	}
	var count int
	if e = pool.QueryRow(context.Background(), "SELECT count(*) FROM orders").Scan(&count); e != nil || count != 0 {
		t.Fatalf("report wrote order %d %v", count, e)
	}
	order, e := s.Store.CreateOrder(context.Background(), payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "matrix-catalog-history", Name: "历史统计", Type: "alipay", Amount: 123})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.MarkPaid(context.Background(), order.TradeNo, "matrix-catalog-history-paid"); e != nil {
		t.Fatal(e)
	}
	// A removed channel/type does not erase history, and a new zero dimension
	// must not turn a genuinely unknown historical cost into known zero profit.
	s.ProviderFlow.Providers = map[int64]adapter.Provider{large: matrixCatalogProvider{kind: "wxpay"}, 9: matrixCatalogProvider{kind: "untrusted-private-label"}}
	parse("merchants", "method=type&type=3")
	if out.Data.Summary["total"] != nil || out.Data.Summary["type_alipay"] != nil || out.Data.Summary["type_wxpay"] != "0.00" {
		t.Fatalf("zero catalog rewrote unknown historical costs: %+v", out.Data)
	}
	if _, ok := out.Data.Summary["type_untrusted-private-label"]; ok {
		t.Fatal("unrecognized provider label exported")
	}
	parse("merchants", "method=channel")
	if out.Data.Summary["channel_unassigned"] != "1.23" || out.Data.Summary["channel_9223372036854775807"] != "0.00" {
		t.Fatal("configured catalog dropped immutable legacy facts")
	}
	s.ProviderFlow.Providers = map[int64]adapter.Provider{}
	for id := int64(1); id <= 129; id++ {
		s.ProviderFlow.Providers[id] = matrixCatalogProvider{kind: "alipay"}
	}
	if w = get("merchants", "method=channel"); w.Code != 422 {
		t.Fatalf("oversize catalog silently truncated: %d %s", w.Code, w.Body.String())
	}
}
