package contract_test

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMerchantMatrixUXOriginalModesTotalsPaginationAndExport(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	for _, v := range []struct {
		mid, amount int64
		kind, no    string
	}{{1, 1234, "alipay", "matrix-a"}, {1, 9, "wxpay", "matrix-b"}, {2, 987, "wxpay", "matrix-c"}, {1, 99999, "alipay", "matrix-pending"}} {
		order, err := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: v.mid, OutTradeNo: v.no, Name: "本地统计", Type: v.kind, Amount: v.amount})
		if err != nil {
			t.Fatal(err)
		}
		if v.no != "matrix-pending" {
			if err = s.Store.MarkPaid(ctx, order.TradeNo, v.no+"-paid"); err != nil {
				t.Fatal(err)
			}
		}
	}
	token, _, err := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super"})
	if err != nil {
		t.Fatal(err)
	}
	merchant, _, err := s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant"})
	if err != nil {
		t.Fatal(err)
	}
	today := time.Now().In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02")
	get := func(name, query, role, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/console/v1/"+role+"/reports/"+name+"?from="+today+"&to="+today+"&"+query, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	var out struct {
		Data struct {
			Columns                []struct{ Key, Label string }
			Items                  []map[string]any
			Summary                map[string]any
			Total                  int
			MissingCosts           string `json:"missing_costs"`
			Method, Metric, Source string
		}
	}
	parse := func(q string) {
		t.Helper()
		w := get("merchants", q, "admin", token)
		if w.Code != 200 {
			t.Fatalf("matrix %s status%d: %s", q, w.Code, w.Body.String())
		}
		if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
			t.Fatal(e)
		}
	}
	parse("view=matrix&method=type&type=0&limit=1")
	if out.Data.Total != 2 || len(out.Data.Items) != 1 || out.Data.Items[0]["uid"] != "1" || out.Data.Items[0]["total"] != "12.43" || out.Data.Items[0]["type_alipay"] != "12.34" || out.Data.Items[0]["type_wxpay"] != "0.09" || out.Data.Summary["total"] != "22.30" {
		t.Fatalf("wrong success-only matrix: %+v", out.Data)
	}
	parse("view=matrix&method=type&type=0&limit=1&page=2")
	if out.Data.Items[0]["uid"] != "2" || out.Data.Items[0]["type_alipay"] != "0.00" || out.Data.Items[0]["type_wxpay"] != "9.87" {
		t.Fatalf("wrong second matrix page: %+v", out.Data)
	}
	parse("view=matrix&method=channel&type=1")
	if out.Data.Summary["channel_unassigned"] != "22.30" || out.Data.Method != "channel" || out.Data.Metric != "1" {
		t.Fatalf("wrong channel matrix: %+v", out.Data)
	}
	parse("view=matrix&method=type&type=2")
	if out.Data.Summary["total"] != "22.30" {
		t.Fatalf("wrong original retained amounts: %+v", out.Data)
	}
	parse("view=matrix&method=type&type=3")
	if out.Data.Summary["total"] != nil || out.Data.MissingCosts != "3" {
		t.Fatalf("fabricated missing cost profit: %+v", out.Data)
	}
	parse("view=matrix&method=type&type=0&sort=uid&direction=desc&limit=1")
	if out.Data.Items[0]["uid"] != "2" {
		t.Fatalf("sorting only the visible page: %+v", out.Data)
	}
	parse("view=matrix&method=type&type=0&sort=type_wxpay&direction=desc&limit=1")
	if out.Data.Items[0]["uid"] != "2" {
		t.Fatalf("dimension sort ignored: %+v", out.Data)
	}
	w := get("merchants", "view=matrix&method=type&type=0&format=csv&limit=1&page=2", "admin", token)
	records, e := csv.NewReader(strings.NewReader(strings.TrimPrefix(w.Body.String(), "\ufeff"))).ReadAll()
	if w.Code != 200 || e != nil || len(records) != 4 || records[0][0] != "商户ID" || records[1][0] != "总计" || records[1][1] != "22.30" || records[2][0] != "1" || records[2][1] != "12.43" {
		t.Fatalf("matrix full CSV %d %v %#v", w.Code, e, records)
	}
	for _, q := range []string{"view=matrix&method=other", "view=matrix&method=type&method=channel", "view=matrix&type=4", "view=matrix&type=03", "view=matrix&uid=2", "view=matrix&view=matrix", "view=matrix&page=01"} {
		if w = get("merchants", q, "admin", token); w.Code != 400 {
			t.Fatalf("invalid matrix %s accepted%d", q, w.Code)
		}
	}
	if w = get("merchants", "view=matrix&method=type", "merchant", merchant); w.Code != 403 {
		t.Fatalf("merchant read admin matrix%d", w.Code)
	}
	if w = get("merchants", "", "admin", token); w.Code != 200 {
		t.Fatalf("old report broken %d", w.Code)
	}
	var audits int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit`).Scan(&audits); e != nil || audits != 1 {
		t.Fatalf("matrix reads wrote business state: %d %v", audits, e)
	}
}
