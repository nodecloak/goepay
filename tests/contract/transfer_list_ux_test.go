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
	"time"
)

func TestTransferListUXOriginalFieldsMaskedAndCSVBound(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	if _, e := pool.Exec(ctx, `UPDATE merchants SET profile='{"transfer_api_enabled":true,"transfer_rate":"3"}' WHERE id IN(1,2); INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"transfer_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	p := &transferHTTPProvider{}
	flow := payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true}
	var transfers []payment.Transfer
	for _, mid := range []int64{1, 2} {
		o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: mid, OutTradeNo: fmt.Sprintf("transfer150-fund-%d", mid), Amount: 10000})
		if e != nil {
			t.Fatal(e)
		}
		if e = s.Store.MarkPaid(ctx, o.TradeNo, "transfer150-paid:"+o.TradeNo); e != nil {
			t.Fatal(e)
		}
		v, e := flow.Transfer(ctx, mid, 7, fmt.Sprintf("transfer150-%d", mid), adapter.TransferRequest{Type: "alipay", Account: fmt.Sprintf("private150-owner%d@example.invalid", mid), Name: "测试私人姓名", Title: "测试标题", Amount: 103})
		if e != nil {
			t.Fatal(e)
		}
		transfers = append(transfers, v)
	}
	admin, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	merchant, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant"})
	if e != nil {
		t.Fatal(e)
	}
	invoke := func(role, token, q string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", "/console/v1/"+role+"/transfers?"+q, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	var before string
	if e = pool.QueryRow(ctx, `SELECT md5((SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM transfers t)||(SELECT jsonb_agg(to_jsonb(a) ORDER BY transfer_id)::text FROM provider_transfer_attempts a)||(SELECT jsonb_agg(to_jsonb(j) ORDER BY id)::text FROM journals j))`).Scan(&before); e != nil {
		t.Fatal(e)
	}
	for _, v := range []struct{ role, token, q string }{{"admin", admin, "merchant_id=1"}, {"merchant", merchant, ""}} {
		w := invoke(v.role, v.token, v.q)
		if w.Code != 200 {
			t.Fatalf("list %d %s", w.Code, w.Body.String())
		}
		var out struct {
			Data struct {
				Items []map[string]any
				Total int
			}
		}
		if e = json.Unmarshal(w.Body.Bytes(), &out); e != nil {
			t.Fatal(e)
		}
		if len(out.Data.Items) != 1 || out.Data.Total != 1 {
			t.Fatalf("owner scope %+v", out.Data)
		}
		row := out.Data.Items[0]
		if row["type"] != "alipay" || row["account_masked"] != "***alid" || row["name_masked"] != "***" || row["amount"] != "103" || row["provider_transfer_no"] != "ali-transfer-1" || row["pay_date"] != "" {
			t.Fatalf("original fields %+v", row)
		}
		if _, e = time.Parse(time.RFC3339Nano, fmt.Sprint(row["created_at"])); e != nil {
			t.Fatalf("real creation time missing %+v", row)
		}
		for _, secret := range []string{"private150-owner", "测试私人姓名", "recipient_cipher", "private_recipient"} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatalf("private projection leaked %s", secret)
			}
		}
		csvResponse := invoke(v.role, v.token, v.q+"&format=csv")
		if csvResponse.Code != 200 {
			t.Fatalf("CSV %d %s", csvResponse.Code, csvResponse.Body.String())
		}
		records, e := csv.NewReader(strings.NewReader(strings.TrimPrefix(csvResponse.Body.String(), "\ufeff"))).ReadAll()
		if e != nil || len(records) != 2 {
			t.Fatalf("CSV inventory %v", e)
		}
		data := map[string]string{}
		for i, key := range records[0] {
			data[key] = records[1][i]
		}
		if data["merchant_id"] != "1" || data["type"] != "alipay" || data["account_masked"] != "***alid" || data["name_masked"] != "***" {
			t.Fatalf("CSV not same masked owner %+v", data)
		}
		for _, secret := range []string{"private150-owner", "测试私人姓名", "recipient_cipher", "private_recipient"} {
			if strings.Contains(csvResponse.Body.String(), secret) {
				t.Fatalf("private CSV leaked %s", secret)
			}
		}
	}
	var after string
	if e = pool.QueryRow(ctx, `SELECT md5((SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM transfers t)||(SELECT jsonb_agg(to_jsonb(a) ORDER BY transfer_id)::text FROM provider_transfer_attempts a)||(SELECT jsonb_agg(to_jsonb(j) ORDER BY id)::text FROM journals j))`).Scan(&after); e != nil {
		t.Fatal(e)
	}
	if before != after || p.writes.Load() != 2 {
		t.Fatal("list changed financial state or invoked provider")
	}
	if w := invoke("merchant", merchant, "merchant_id=2"); w.Code != 403 {
		t.Fatal("cross merchant list broadened", w.Code)
	}
	// Fault injection is confined to this disposable test schema. A corrupt
	// authenticated original must fail both reads before any partial CSV escapes.
	tx, e := pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `ALTER TABLE provider_transfer_attempts DISABLE TRIGGER provider_transfer_attempt_immutable`); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `UPDATE provider_transfer_attempts SET recipient_cipher=set_byte(recipient_cipher,20,get_byte(recipient_cipher,20)#1) WHERE transfer_id=$1`, transfers[0].ID); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `ALTER TABLE provider_transfer_attempts ENABLE TRIGGER provider_transfer_attempt_immutable`); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	for _, q := range []string{"merchant_id=1", "merchant_id=1&format=csv"} {
		w := invoke("admin", admin, q)
		if w.Code < 400 || strings.Contains(w.Header().Get("Content-Type"), "text/csv") || w.Header().Get("Content-Disposition") != "" || strings.Contains(w.Body.String(), "private150-owner") {
			t.Fatalf("unverified or partial list escaped: %d", w.Code)
		}
	}
}
