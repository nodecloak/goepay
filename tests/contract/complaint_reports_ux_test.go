package contract_test

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"goepay/internal/identity"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestComplaintReportUXOverview(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	// The business date is fixed by PostgreSQL at UTC+08; all records are local fixtures.
	_, e := pool.Exec(ctx, `INSERT INTO backoffice_records(resource,owner_id,body,created_at) VALUES
 ('complaints',1,'{"status":"open","pay_type":"alipay","amount":12345}',date_trunc('day',now() AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai'),
 ('complaints',1,'{"status":"resolved","pay_type":"wxpay","amount":5}',date_trunc('day',now() AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai'),
 ('complaints',1,'{"status":"closed","pay_type":"alipay","amount":987}',(date_trunc('day',now() AT TIME ZONE 'Asia/Shanghai')-interval '1 day') AT TIME ZONE 'Asia/Shanghai'),
 ('complaints',2,'{"status":"open","pay_type":"wxpay","amount":99999}',now()),
 ('complaints',1,'{"status":"open","pay_type":"alipay","amount":999999}',now()-interval '31 days')`)
	if e != nil {
		t.Fatal(e)
	}
	admin, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	merchant, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant"})
	if e != nil {
		t.Fatal(e)
	}
	get := func(path, role, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/console/v1/"+role+"/reports/complaints?view=overview"+path, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	w := get("", "admin", admin)
	if w.Code != 200 {
		t.Fatalf("overview %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Data struct {
			Columns []struct {
				Key string `json:"key"`
			}
			Items   []map[string]any
			Summary map[string]any
			Source  string
		}
	}
	if e = json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	if len(out.Data.Items) != 2 {
		t.Fatalf("merchant ranking not returned: %s", w.Body.String())
	}
	row := out.Data.Items[0]
	if row["dimension"] != "1" || row["today_count"] != "2" || row["today_money"] != "123.50" || row["yesterday_money"] != "9.87" || row["today_alipay"] != "1" || row["today_wxpay"] != "1" || row["complaint_rate"] != nil {
		t.Fatalf("wrong complaint facts: %v", row)
	}
	for _, c := range out.Data.Columns {
		if strings.Contains(c.Key, "paid_") || c.Key == "fee_money" || c.Key == "net_money" {
			t.Fatalf("payment columns leaked: %+v", out.Data.Columns)
		}
	}
	if out.Data.Source != "local_registered_complaints" || out.Data.Summary["today_count"] != "3" || out.Data.Summary["today_money"] != "1123.49" {
		t.Fatalf("missing source or summary: %+v", out.Data)
	}
	w = get("&format=csv", "admin", admin)
	rows, e := csv.NewReader(strings.NewReader(strings.TrimPrefix(w.Body.String(), "\ufeff"))).ReadAll()
	if e != nil || w.Code != 200 || len(rows) != 3 || rows[0][1] != "today_count" || rows[1][2] != "123.50" {
		t.Fatalf("wrong export %d %v %v", w.Code, rows, e)
	}
	if w = get("", "merchant", merchant); w.Code != 403 {
		t.Fatalf("merchant read admin report: %d", w.Code)
	}
	if w = get("&view=overview", "admin", admin); w.Code != 400 {
		t.Fatalf("accepted ambiguous view: %d", w.Code)
	}
}
