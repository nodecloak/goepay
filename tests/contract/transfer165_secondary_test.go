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
	"strings"
	"testing"
)

func TestTransfer165OriginalRemarkCostPaymentTimeAndOwnerIsolation(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	if _, e := pool.Exec(ctx, `UPDATE merchants SET profile='{"transfer_api_enabled":true,"transfer_rate":"2","remark":"CURRENT-PROFILE-MUST-NOT-APPEAR"}' WHERE id IN(1,2); INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"transfer_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	p := &transferHTTPProvider{}
	flow := payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true}
	for _, mid := range []int64{1, 2} {
		o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: mid, OutTradeNo: fmt.Sprintf("transfer165-fund-%d", mid), Amount: 100000})
		if e != nil {
			t.Fatal(e)
		}
		if e = s.Store.MarkPaid(ctx, o.TradeNo, "transfer165-paid:"+o.TradeNo); e != nil {
			t.Fatal(e)
		}
	}
	var own []payment.Transfer
	for i, remark := range []string{"原业务备注165", "=SYNTHETIC(165)", "", "含\t制表符", "foreign165-only"} {
		mid := int64(1)
		if i == 4 {
			mid = 2
		}
		v, e := flow.Transfer(ctx, mid, 7, fmt.Sprintf("transfer165-original-%d", i), adapter.TransferRequest{Type: "alipay", Account: "synthetic-private-account@example.invalid", Name: "Synthetic Private Name", Title: "TITLE-MUST-NOT-BECOME-REMARK", Remark: remark, Amount: 10000})
		if e != nil {
			t.Fatal(e)
		}
		if i == 0 {
			v, e = flow.QueryTransfer(ctx, mid, v.TransferNo)
			if e != nil {
				t.Fatal(e)
			}
		}
		if mid == 1 {
			own = append(own, v)
		}
	}
	tokens := map[string]string{}
	for _, role := range []string{"admin", "merchant"} {
		token, _, e := s.Sessions.Create(identity.User{ID: 1, Username: role, Role: role, AdminClass: "super"})
		if e != nil {
			t.Fatal(e)
		}
		tokens[role] = token
	}
	invoke := func(role, q string, authenticated bool) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", "/console/v1/"+role+"/transfers?"+q, nil)
		if authenticated {
			r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: tokens[role]})
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	var before string
	digest := `SELECT md5((SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM transfers t)||(SELECT jsonb_agg(to_jsonb(a) ORDER BY transfer_id)::text FROM provider_transfer_attempts a)||(SELECT jsonb_agg(to_jsonb(j) ORDER BY id)::text FROM journals j))`
	if e := pool.QueryRow(ctx, digest).Scan(&before); e != nil {
		t.Fatal(e)
	}
	for _, role := range []string{"admin", "merchant"} {
		q := ""
		if role == "admin" {
			q = "merchant_id=1"
		}
		w := invoke(role, q, true)
		if w.Code != 200 {
			t.Fatalf("list %d %s", w.Code, w.Body.String())
		}
		var result struct {
			Data struct {
				Items []map[string]any
				Total int
			}
		}
		if e := json.Unmarshal(w.Body.Bytes(), &result); e != nil {
			t.Fatal(e)
		}
		if result.Data.Total != 4 || len(result.Data.Items) != 4 {
			t.Fatal("owner scope", result.Data)
		}
		rows := map[string]map[string]any{}
		for _, r := range result.Data.Items {
			rows[fmt.Sprint(r["id"])] = r
		}
		success := rows[fmt.Sprint(own[0].ID)]
		pending := rows[fmt.Sprint(own[1].ID)]
		if success["remark"] != "原业务备注165" {
			t.Fatalf("original remark absent or borrowed: %+v", success)
		}
		if success["amount"] != "10000" || success["fee_amount"] != "200" || success["cost_amount"] != "10200" || success["pay_date"] != "2026-10-23 10:00:00" || success["status"] != "succeeded" {
			t.Fatal("original money/time changed", success)
		}
		if pending["remark"] != "=SYNTHETIC(165)" || pending["pay_date"] != "" || pending["status"] != "pending" {
			t.Fatal("invented date or status", pending)
		}
		if empty := rows[fmt.Sprint(own[2].ID)]; empty["remark"] != "" || empty["pay_date"] != "" {
			t.Fatal("empty original remark borrowed current profile or separate title", empty)
		}
		if tab := rows[fmt.Sprint(own[3].ID)]; tab["remark"] != "含\t制表符" {
			t.Fatal("valid original tab remark not preserved", tab)
		}
		csvResponse := invoke(role, q+"&format=csv", true)
		if csvResponse.Code != 200 {
			t.Fatal("CSV failed", csvResponse.Code)
		}
		records, e := csv.NewReader(strings.NewReader(strings.TrimPrefix(csvResponse.Body.String(), "\ufeff"))).ReadAll()
		if e != nil || len(records) != 5 {
			t.Fatal("filtered CSV", e)
		}
		hasRemark := false
		for _, key := range records[0] {
			hasRemark = hasRemark || key == "remark"
		}
		if !hasRemark {
			t.Fatal("CSV lacks same original projection")
		}
		if !strings.Contains(csvResponse.Body.String(), "含\t制表符") {
			t.Fatal("CSV lost valid original tab remark")
		}
		if !strings.Contains(csvResponse.Body.String(), "'=SYNTHETIC(165)") {
			t.Fatal("CSV formula not escaped")
		}
		for _, r := range []*httptest.ResponseRecorder{w, csvResponse} {
			for _, secret := range []string{"foreign165-only", "CURRENT-PROFILE-MUST-NOT-APPEAR", "TITLE-MUST-NOT-BECOME-REMARK", "Synthetic Private Name", "synthetic-private-account", "recipient_cipher", "private_recipient"} {
				if strings.Contains(r.Body.String(), secret) {
					t.Fatal("unrelated private field escaped", secret)
				}
			}
		}
		if got := invoke(role, q, false); got.Code != 401 {
			t.Fatal("unauthenticated display", got.Code)
		}
	}
	if w := invoke("merchant", "merchant_id=2", true); w.Code != 403 {
		t.Fatal("cross-merchant JSON", w.Code)
	}
	if w := invoke("merchant", "merchant_id=2&format=csv", true); w.Code != 403 {
		t.Fatal("cross-merchant CSV", w.Code)
	}
	var after string
	if e := pool.QueryRow(ctx, digest).Scan(&after); e != nil {
		t.Fatal(e)
	}
	if before != after || p.writes.Load() != 5 {
		t.Fatal("read changed original intent or called provider")
	}
}
