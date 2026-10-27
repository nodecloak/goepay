package contract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestOriginalPrivateSettlementSearchUsesEncryptedOriginalsAndNeverWritesOnGET(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	for _, mid := range []int64{1, 2} {
		o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: mid, OutTradeNo: fmt.Sprintf("private158-income-%d", mid), Amount: 10000})
		if e != nil {
			t.Fatal(e)
		}
		if e = s.Store.MarkPaid(ctx, o.TradeNo, "private158-paid:"+o.TradeNo); e != nil {
			t.Fatal(e)
		}
	}
	for i, v := range []struct {
		mid           int64
		account, name string
	}{{1, "Alpha.Private158@Example.invalid", "本地张三"}, {1, "Beta.Private158@example.invalid", "本地李四"}, {2, "Alpha.Private158@Example.invalid", "本地张三"}} {
		if _, e := s.Store.SetSettlementRecipient(ctx, v.mid, payment.SettlementRecipientInput{Type: "alipay", Account: v.account, Name: v.name}, nil, payment.SettlementRecipientActor{Role: "merchant", ID: v.mid, Name: "local"}); e != nil {
			t.Fatal(e)
		}
		if _, e := s.Store.RequestFunds(ctx, "settlements", v.mid, 0, fmt.Sprintf("private158-request-%d", i), 100, "merchant"); e != nil {
			t.Fatal(e)
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
	fingerprint := func() string {
		var out string
		if e := pool.QueryRow(ctx, `SELECT md5(jsonb_build_array((SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM accounts t),(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM ledger_requests t),(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM settlement_recipient_configs t),(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM backoffice_audit t))::text)`).Scan(&out); e != nil {
			t.Fatal(e)
		}
		return out
	}
	before := fingerprint()
	call := func(role, path, q string) map[string]any {
		t.Helper()
		r := httptest.NewRequest("GET", "/console/v1/"+role+"/workflows/settlements"+path+"?"+q, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: tokens[role]})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		var out struct{ Data map[string]any }
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("private original search %s %q: %d %s", path, q, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "Alpha.Private158") || strings.Contains(w.Body.String(), "本地张三") {
			t.Fatal("search returned private unmasked original")
		}
		return out.Data
	}
	for _, v := range []struct {
		role, value, scope string
		count              int
	}{{"merchant", "ALPHA.PRIVATE158", "", 1}, {"merchant", "张三", "", 1}, {"merchant", "李四", "", 1}, {"admin", "alpha.private158", "", 2}, {"admin", "张三", "merchant_id=1&", 1}, {"merchant", "not-present158", "", 0}, {"merchant", "%", "", 0}} {
		q := v.scope + "value=" + url.QueryEscape(v.value)
		data := call(v.role, "", q+"&limit=1")
		if data["total"] != float64(v.count) {
			t.Fatal("wrong original or tenant search", v.value, data)
		}
		stats := call(v.role, "/statistics", q)
		if stats["total_count"] != fmt.Sprint(v.count) {
			t.Fatal("statistics private filter differs", stats)
		}
	}
	if fingerprint() != before {
		t.Fatal("GET private search mutated original records or audit")
	}
}
