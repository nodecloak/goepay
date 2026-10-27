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

func TestMerchantDirectoryUXOriginalFiltersAndSortAreServerEnforced(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	if _, e := pool.Exec(ctx, `UPDATE merchants SET pay=2 WHERE id=1; UPDATE merchants SET enabled=false WHERE id=2`); e != nil {
		t.Fatal(e)
	}
	token, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	get := func(query string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/console/v1/admin/resources/merchants?"+query, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	only := func(query, id string) {
		t.Helper()
		w := get(query)
		if w.Code != 200 {
			t.Fatalf("filter %s %d %s", query, w.Code, w.Body.String())
		}
		var out struct {
			Data struct {
				Total int
				Items []struct {
					ID string `json:"id"`
				}
			}
		}
		if e = json.Unmarshal(w.Body.Bytes(), &out); e != nil || out.Data.Total != 1 || len(out.Data.Items) != 1 || out.Data.Items[0].ID != id {
			t.Fatalf("ignored original filter %s: %s %v", query, w.Body.String(), e)
		}
	}
	only("dstatus=pay_2", "1")
	only("pay=2", "1")
	only("uid=1", "1")
	only("dstatus=status_0", "2")
	only("column=uid&q=2", "2")
	for _, q := range []string{"dstatus=pay_2&dstatus=pay_1", "pay=2&dstatus=pay_0", "uid=01", "order=money_desc%3BDELETE", "gid=-1", "from=2026-02-30", "from=2026-10-25&to=2026-01-01"} {
		if w := get(q); w.Code != 400 {
			t.Fatalf("invalid directory query %q status %d", q, w.Code)
		}
	}
	if _, e = pool.Exec(ctx, `UPDATE merchants SET enabled=true,pay=1 WHERE id=2; UPDATE merchants SET pay=1 WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	for _, v := range []struct {
		mid, amount int64
		no          string
	}{{1, 100, "directory-low"}, {2, 900, "directory-high"}} {
		o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: v.mid, OutTradeNo: v.no, Name: "本地目录余额", Type: "alipay", Amount: v.amount})
		if e != nil {
			t.Fatal(e)
		}
		if e = s.Store.MarkPaid(ctx, o.TradeNo, v.no+"-paid"); e != nil {
			t.Fatal(e)
		}
	}
	var auditsBefore int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit`).Scan(&auditsBefore); e != nil {
		t.Fatal(e)
	}
	for _, v := range []struct{ sort, want string }{{"money_desc", "2"}, {"money_asc", "1"}} {
		w := get("order=" + v.sort + "&limit=1")
		var out struct {
			Data struct {
				Items []struct {
					ID string `json:"id"`
				}
			}
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out.Data.Items) != 1 || out.Data.Items[0].ID != v.want {
			t.Fatalf("ignored balance sort %s %d %s", v.sort, w.Code, w.Body.String())
		}
	}
	var audits int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit`).Scan(&audits); e != nil || audits != auditsBefore {
		t.Fatalf("filter caused business write %d %v", audits, e)
	}
}
