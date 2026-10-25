package contract_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"goepay/internal/identity"
)

func TestAccountLoginLogUXOnlyVerifiedOwnerRowsAndReadOnlyPagination(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO console_login_audit(username,role,ip,success,subject_id,auth_version,source_transaction) VALUES
 ('merchant','merchant','192.0.2.1',true,1,1,pg_current_xact_id()),
 ('merchant','merchant','192.0.2.2',true,1,1,pg_current_xact_id()),
 ('merchant','merchant','192.0.2.3',true,2,1,pg_current_xact_id()),
 ('merchant','merchant','192.0.2.4',true,NULL,NULL,NULL),
 ('merchant','merchant','192.0.2.5',false,NULL,NULL,NULL),
 ('merchant','admin','192.0.2.6',true,NULL,NULL,NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO console_login_audit(username,role,ip,success,subject_id,auth_version,source_transaction,created_at) VALUES('merchant','merchant','192.0.2.7',true,1,1,pg_current_xact_id(),clock_timestamp()-interval '1 day')`); err != nil {
		t.Fatal(err)
	}
	token, _, err := s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant"})
	if err != nil {
		t.Fatal(err)
	}
	get := func(query string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/console/v1/merchant/account/login-log"+query, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	for _, tc := range []struct{ query, ip string }{{"?page=1&limit=1", "192.0.2.2"}, {"?page=2&limit=1", "192.0.2.1"}, {"?page=3&limit=1", "192.0.2.7"}} {
		w := get(tc.query)
		var out struct {
			Data struct {
				Items []struct {
					ID   string `json:"id"`
					IP   string `json:"ip"`
					Type string `json:"type"`
					City string `json:"city"`
				}
				Total int `json:"total"`
			}
		}
		if err = json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 || out.Data.Total != 3 || len(out.Data.Items) != 1 || out.Data.Items[0].IP != tc.ip || out.Data.Items[0].ID == "" || out.Data.Items[0].Type != "" || out.Data.Items[0].City != "" {
			t.Fatalf("original own log %s: %d %s %v", tc.query, w.Code, w.Body.String(), err)
		}
	}
	for _, q := range []string{"?merchant_id=2", "?uid=2", "?page=01", "?page=0", "?limit=101", "?page=1&page=2", "?limit=1&limit=2", "?page=100001", "?q=%ZZ"} {
		if w := get(q); w.Code != 400 {
			t.Fatalf("invalid owner-log query accepted %s: %d", q, w.Code)
		}
	}
	r := httptest.NewRequest("GET", "/console/v1/merchant/account/login-log", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("login log without session", w.Code)
	}
	var audits, journals int
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM backoffice_audit),(SELECT count(*) FROM journals)`).Scan(&audits, &journals); err != nil || audits+journals != 0 {
		t.Fatal("read created business data", audits, journals, err)
	}
}
