package contract_test

import (
	"bytes"
	"context"
	"encoding/xml"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/notification"
	"goepay/internal/ordersubject"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

func TestProviderSubjectTLSOriginalMerchantContractAndImmutableReplay(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	if _, e := pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('order-subject-policies','{"scope":"global","scope_id":"0","template":"upstream-[outorder]","enabled":true}')`); e != nil {
		t.Fatal(e)
	}
	var sends atomic.Int32
	key := "subject-test-only-wx-key-123456789"
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		var request struct {
			Body   string `xml:"body"`
			Amount string `xml:"total_fee"`
			Trade  string `xml:"out_trade_no"`
		}
		if e := xml.NewDecoder(r.Body).Decode(&request); e != nil {
			t.Error(e)
		}
		if request.Body != "upstream-title-order" || request.Amount != "123" {
			t.Errorf("wrong upstream title/amount: %#v", request)
		}
		var persisted string
		if e := pool.QueryRow(ctx, "SELECT subject FROM provider_subject_snapshots x JOIN orders o ON o.id=x.order_id WHERE o.trade_no=$1", request.Trade).Scan(&persisted); e != nil || persisted != request.Body {
			t.Error("external send preceded snapshot", e)
		}
		p := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "subject-app", "mch_id": "subject-mch", "code_url": "weixin://temporary-subject-test"}
		p["sign"], _ = adapter.SignWechatV2(p, key, "MD5")
		_, _ = w.Write(providerXML(p))
	}))
	defer upstream.Close()
	wx, e := adapter.NewWechatV2(adapter.WechatV2Config{AppID: "subject-app", MerchantID: "subject-mch", APIKey: key, BaseURL: upstream.URL, HTTPClient: upstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: wx}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid", SubjectResolver: ordersubject.New(pool).Resolve}
	s.MerchantChannels = map[int64][]int64{1: {7}}
	s.CheckoutKey = bytes.Repeat([]byte{7}, 32)
	p := compat.Params{"pid": "1", "out_trade_no": "title-order", "type": "wxpay", "method": "native", "channel_id": "7", "money": "1.23", "name": "original 商品名称", "notify_url": "https://merchant.example.invalid/notify"}
	_, out := call(t, s.Handler(), "POST", "/mapi.php", signed(p, "contract-secret"))
	if out["code"] != float64(1) {
		t.Fatal(out)
	}
	trade := out["trade_no"].(string)
	o, e := s.Store.OrderByTradeNo(ctx, trade)
	if e != nil || o.Name != p["name"] {
		t.Fatal("original merchant name mutated", e)
	}
	if _, e = pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{template}','"changed-after-send"') WHERE resource='order-subject-policies'`); e != nil {
		t.Fatal(e)
	}
	_, again := call(t, s.Handler(), "POST", "/mapi.php", signed(p, "contract-secret"))
	if again["trade_no"] != trade || sends.Load() != 1 {
		t.Fatal("retry changed title or sent again", again, sends.Load())
	}
	m, e := s.Store.Merchant(ctx, 1)
	if e != nil {
		t.Fatal(e)
	}
	target, e := notification.URL(o.NotifyURL, o, m, nil, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	u, e := url.Parse(target)
	if e != nil || u.Query().Get("name") != p["name"] {
		t.Fatal("notification did not keep signed original name", e)
	}
	changed := compat.Params{}
	for k, v := range p {
		changed[k] = v
	}
	changed["name"] = "different-original"
	_, out = call(t, s.Handler(), "POST", "/mapi.php", signed(changed, "contract-secret"))
	if out["code"] != float64(-1) || sends.Load() != 1 {
		t.Fatal("changed original name bypassed idempotency", out)
	}
}
