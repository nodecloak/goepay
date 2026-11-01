package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"goepay/internal/identity"
	"goepay/internal/payment"
)

func TestTransferStatisticsUsesOriginalListScopeAndExactAmounts(t *testing.T) {
	s, p := groupRoutingFixture152(t)
	ctx := t.Context()
	for _, id := range []int64{751, 752} {
		if e := s.Store.CreateMerchant(ctx, payment.Merchant{ID: id, Name: "Synthetic transfer statistics", APIKey: "synthetic-transfer-stats-only"}); e != nil {
			t.Fatal(e)
		}
	}
	for _, v := range []struct {
		mid, amount, fee int64
		status           string
	}{{751, 9007199254740993, 7, "succeeded"}, {751, 100, 3, "pending"}, {752, 200, 5, "failed"}} {
		if _, e := p.Exec(ctx, "INSERT INTO transfers(merchant_id,transfer_no,amount,cost_amount,fee_amount,status) VALUES($1,$2,$3,$3::bigint+$4::bigint,$4,$5)", v.mid, v.status, v.amount, v.fee, v.status); e != nil {
			t.Fatal(e)
		}
	}
	for _, v := range []struct {
		role                              string
		mid                               int64
		query                             string
		count, amount, fee, cost, success string
		status                            int
	}{
		{"admin", 0, "", "3", "9007199254741293", "15", "9007199254741308", "1", 200},
		{"admin", 0, "?merchant_id=751&status=succeeded", "1", "9007199254740993", "7", "9007199254741000", "1", 200},
		{"merchant", 752, "", "1", "200", "5", "205", "0", 200},
		{"merchant", 752, "?merchant_id=751", "", "", "", "", "", 403},
		{"admin", 0, "?page=2", "", "", "", "", "", 400},
		{"admin", 0, "?merchant_id=", "", "", "", "", "", 400},
		{"admin", 0, "?format=csv", "", "", "", "", "", 400},
		{"admin", 0, "?merchant_id=751&merchant_id=752", "", "", "", "", "", 400},
	} {
		r := httptest.NewRequest("GET", "/console/v1/"+v.role+"/transfers/statistics"+v.query, nil)
		w := httptest.NewRecorder()
		s.listMode(w, r, v.role, v.mid, "transfers", identity.Session{}, true)
		if w.Code != v.status {
			t.Fatalf("%s %s status=%d body=%s", v.role, v.query, w.Code, w.Body.String())
		}
		if v.status != 200 {
			continue
		}
		var body struct {
			Data map[string]string `json:"data"`
		}
		if e := json.Unmarshal(w.Body.Bytes(), &body); e != nil {
			t.Fatal(e)
		}
		for key, want := range map[string]string{"count": v.count, "amount": v.amount, "fee_amount": v.fee, "cost_amount": v.cost, "succeeded_count": v.success} {
			if body.Data[key] != want {
				t.Fatalf("%s %s %s=%s want %s", v.role, v.query, key, body.Data[key], want)
			}
		}
	}
}

func TestTransferStatisticsRouteIsReadOnly(t *testing.T) {
	for _, method := range []string{"POST", "DELETE", "PATCH"} {
		w := httptest.NewRecorder()
		s := &Server{}
		if !s.transferConsole(w, httptest.NewRequest(method, "/console/v1/admin/transfers/statistics", nil), "admin", identity.Session{}, []string{"admin", "transfers", "statistics"}) || w.Code != 405 || w.Header().Get("Allow") != "GET" {
			t.Fatalf("%s route did not refuse write: %d", method, w.Code)
		}
	}
}
