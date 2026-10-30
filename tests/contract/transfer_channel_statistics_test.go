package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"testing"
)

// transferChannelStatProvider immediately succeeds so seeded payouts land in a
// terminal 'succeeded' state for the 通道数据统计 aggregate without any external
// call (mirrors transferAccountStatProvider).
type transferChannelStatProvider struct {
	adapter.Provider
	id string
}

func (p *transferChannelStatProvider) ID() string              { return p.id }
func (p *transferChannelStatProvider) TransferTypes() []string { return []string{p.id} }
func (p *transferChannelStatProvider) Transfer(_ context.Context, r adapter.TransferRequest) (adapter.TransferResult, error) {
	return adapter.TransferResult{TransferNo: r.TransferNo, Amount: r.Amount, State: "SUCCESS", ProviderTransferNo: p.id + "-tn"}, nil
}
func (p *transferChannelStatProvider) QueryTransfer(_ context.Context, r adapter.TransferRequest) (adapter.TransferResult, error) {
	return adapter.TransferResult{TransferNo: r.TransferNo, Amount: r.Amount, State: "SUCCESS", ProviderTransferNo: p.id + "-tn"}, nil
}

// TestTransferChannelStatisticsFiltersByMethodAndMerchant is B04 #12: the
// 通道数据统计 (transfer-channels) report must honour epay_pro's channelStat
// 付款方式 (type) and 商户号 (uid) search fields. It verifies the per-channel
// pivot cells, that the filtered 成功金额 equals the sum over the underlying
// transfer records, and admin-only access.
func TestTransferChannelStatisticsFiltersByMethodAndMerchant(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	// Fund merchants 1 and 2 and enable transfers for both.
	if _, e := pool.Exec(ctx, `UPDATE merchants SET profile='{"transfer_api_enabled":true,"transfer_rate":"0"}', enabled=true WHERE id IN (1,2); INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"transfer_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	for i, mid := range []int64{1, 2} {
		o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: mid, OutTradeNo: "chanstat-fund-" + string(rune('a'+i)), Amount: 1000000})
		if e != nil {
			t.Fatal(e)
		}
		if e = s.Store.MarkPaid(ctx, o.TradeNo, "chanstat-fund-"+string(rune('a'+i))); e != nil {
			t.Fatal(e)
		}
	}
	alipay := &transferChannelStatProvider{id: "alipay"}
	wxpay := &transferChannelStatProvider{id: "wxpay"}
	// Provider IDs double as channel IDs: 7=alipay, 8=wxpay.
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: alipay, 8: wxpay}, AllowLive: true}
	s.MerchantChannels = map[int64][]int64{1: {7, 8}, 2: {7, 8}}

	// The fixture only seeds a console_user for merchant 1; add one for merchant 2
	// so it can open a session and create its own payout.
	if _, e := pool.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class,enabled) VALUES('merchant2','merchant',2,'Merchant2',(SELECT password_hash FROM console_users WHERE username='merchant' LIMIT 1),'operator',true)`); e != nil {
		t.Fatal(e)
	}

	create := func(mid int64, channel, typ, account, amount, no string) {
		t.Helper()
		username := "merchant"
		if mid == 2 {
			username = "merchant2"
		}
		token, csrf, e := s.Sessions.Create(identity.User{Username: username, Role: "merchant", ID: mid})
		if e != nil {
			t.Fatal(e)
		}
		body := `{"channel_id":"` + channel + `","type":"` + typ + `","account":"` + account + `","name":"Payee","amount":"` + amount + `","remark":"本地统计","external_no":"` + no + `"}`
		r := httptest.NewRequest("POST", "/console/v1/merchant/transfers", bytes.NewBufferString(body))
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 200 && w.Code != 202 {
			t.Fatalf("create %s failed: %d %s", no, w.Code, w.Body.String())
		}
	}
	// Merchant 1: alipay 10.00 + 5.00 (channel 7), wxpay 7.00 (channel 8).
	// Merchant 2: alipay 3.00 (channel 7).
	create(1, "7", "alipay", "m1-a1", "10.00", "c-a1")
	create(1, "7", "alipay", "m1-a2", "5.00", "c-a2")
	create(1, "8", "wxpay", "m1-w1", "7.00", "c-w1")
	create(2, "7", "alipay", "m2-a1", "3.00", "c-b1")

	adminToken, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	get := func(path, cookie, tok string) (int, map[string]any) {
		t.Helper()
		r := httptest.NewRequest("GET", path, nil)
		if tok != "" {
			r.AddCookie(&http.Cookie{Name: cookie, Value: tok})
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	// The report defaults to "today"; seeded payouts resolve today, so the
	// default window covers them.
	rows := func(out map[string]any) map[string]map[string]any {
		data := out["data"].(map[string]any)
		byChannel := map[string]map[string]any{}
		for _, raw := range data["items"].([]any) {
			item := raw.(map[string]any)
			byChannel[item["dimension"].(string)] = item
		}
		return byChannel
	}

	// Unfiltered: channel 7 = 10+5+3 = 18.00 (3 笔), channel 8 = 7.00 (1 笔).
	code, out := get("/console/v1/admin/reports/transfer-channels", "goepay_admin", adminToken)
	if code != 200 {
		t.Fatalf("status=%d body=%v", code, out)
	}
	all := rows(out)
	if all["7"]["paid_money"] != "18.00" || all["7"]["paid_orders"] != "3" {
		t.Fatalf("channel 7 aggregate wrong: %v", all["7"])
	}
	if all["8"]["paid_money"] != "7.00" || all["8"]["paid_orders"] != "1" {
		t.Fatalf("channel 8 aggregate wrong: %v", all["8"])
	}

	// 付款方式=wxpay: only channel 8 remains, 7.00.
	code, out = get("/console/v1/admin/reports/transfer-channels?type=wxpay", "goepay_admin", adminToken)
	if code != 200 {
		t.Fatalf("wxpay filter status=%d body=%v", code, out)
	}
	byMethod := rows(out)
	if _, present := byMethod["7"]; present {
		t.Fatalf("wxpay filter leaked an alipay channel: %v", byMethod)
	}
	if byMethod["8"]["paid_money"] != "7.00" {
		t.Fatalf("wxpay filter channel 8 money wrong: %v", byMethod["8"])
	}

	// 商户号=1: channel 7 = 10+5 = 15.00 (2 笔), channel 8 = 7.00 (1 笔);
	// merchant 2's 3.00 is excluded.
	code, out = get("/console/v1/admin/reports/transfer-channels?uid=1", "goepay_admin", adminToken)
	if code != 200 {
		t.Fatalf("uid filter status=%d body=%v", code, out)
	}
	byMerchant := rows(out)
	if byMerchant["7"]["paid_money"] != "15.00" || byMerchant["7"]["paid_orders"] != "2" {
		t.Fatalf("uid=1 channel 7 wrong (merchant 2 should be excluded): %v", byMerchant["7"])
	}

	// Combined 付款方式=alipay & 商户号=1: channel 7 only, 15.00.
	code, out = get("/console/v1/admin/reports/transfer-channels?type=alipay&uid=1", "goepay_admin", adminToken)
	if code != 200 {
		t.Fatalf("combined filter status=%d body=%v", code, out)
	}
	combined := rows(out)
	if len(combined) != 1 || combined["7"]["paid_money"] != "15.00" {
		t.Fatalf("combined alipay+uid=1 wrong: %v", combined)
	}

	// Invalid method and invalid merchant are rejected (400), never silently
	// dropped into an unfiltered report.
	if code, _ = get("/console/v1/admin/reports/transfer-channels?type=nope", "goepay_admin", adminToken); code != 400 {
		t.Fatalf("invalid method accepted: %d", code)
	}
	if code, _ = get("/console/v1/admin/reports/transfer-channels?uid=0", "goepay_admin", adminToken); code != 400 {
		t.Fatalf("invalid merchant accepted: %d", code)
	}
	if code, _ = get("/console/v1/admin/reports/transfer-channels?uid=abc", "goepay_admin", adminToken); code != 400 {
		t.Fatalf("non-numeric merchant accepted: %d", code)
	}

	// CSV export honours the filters too (reuses the same aggregate + args).
	r := httptest.NewRequest("GET", "/console/v1/admin/reports/transfer-channels?type=wxpay&format=csv", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: adminToken})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 || w.Header().Get("Content-Type") != "text/csv; charset=utf-8" {
		t.Fatalf("csv export failed: %d %s", w.Code, w.Header().Get("Content-Type"))
	}

	// Admin-only: a merchant session is refused by the shared reports gate.
	tok, _, e := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1})
	if e != nil {
		t.Fatal(e)
	}
	if code, _ = get("/console/v1/merchant/reports/transfer-channels", "goepay_merchant", tok); code != 403 {
		t.Fatalf("merchant reached admin transfer-channels report: %d", code)
	}
}
