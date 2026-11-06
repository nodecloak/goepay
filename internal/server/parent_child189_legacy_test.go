package server

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/channelconfig"
	"goepay/internal/compat"
	"goepay/internal/payment"
	"testing"
)

func TestParentChild189CompletePre103ChildSurvivesLaterParentAdoptionWithoutSourceBackfill(t *testing.T) {
	f, children := subchannel168BLocal(t)
	if w := f.childHTTP("POST", "", `{"merchant_id":"1","parent_id":"7","name":"original pre103 child189","enabled":true,"weight":1,"min_amount":"1","max_amount":"10000","values":{"app":"pre103-child189","secret":"synthetic-only-secret"}}`); w.Code != 200 {
		t.Fatal("old child creation rejected", w.Code)
	}
	if result := f.pay(t, "child189-before-parent-adopt", "1.00", 7); result["code"] != float64(1) {
		t.Fatal("original child never paid", result)
	}
	order, err := f.s.Store.Order(t.Context(), 1, "child189-before-parent-adopt")
	if err != nil {
		t.Fatal(err)
	}
	merchant, _ := rsa.GenerateKey(rand.Reader, 2048)
	platform, _ := rsa.GenerateKey(rand.Reader, 2048)
	base, err := channelconfig.NewParentChannelFactory(channelconfig.Channel{ID: 7, Plugin: "alipay", AppTypes: []string{"3"}, Config: map[string]string{"appid": "adopted189-parent", "appmchid": "adopted189-merchant", "appsecret": parent180Private(merchant), "appkey": parent180Public(platform), "sign_type": "0"}}, channelconfig.ParentBuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// This adopted provider is an in-memory negative control. If a recovery
	// accidentally selects it, its account identity differs and no network exists.
	f.s.ProviderFlow.ParentFactories = map[int64]adapter.ParentAccountFactory{7: &parent180TLSFactory{ParentChannelFactory: base, endpoint: "https://127.0.0.1:1", localProvider: func(channelconfig.Channel) (adapter.Provider, error) {
		return &channel168Provider{app: "adopted189-parent"}, nil
	}}}
	var templates struct {
		Data struct{ Items []map[string]any }
	}
	w := parent180HTTP(f, "GET", "/templates", "")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &templates) != nil {
		t.Fatal("adoption template unavailable", w.Code)
	}
	var fingerprint any
	for _, item := range templates.Data.Items {
		if item["source_channel_id"] == "7" {
			fingerprint = item["fingerprint"]
		}
	}
	if fingerprint == nil {
		t.Fatal("original parent template missing")
	}
	body, _ := json.Marshal(map[string]any{"expected_source_fingerprint": fingerprint})
	if w = parent180HTTP(f, "POST", "/7/adopt", string(body)); w.Code != 200 {
		t.Fatal("parent adoption failed", w.Code)
	}
	reads := 0
	children.providers["pre103-child189"].subchannelTestQuery = func(_ context.Context, req adapter.QueryRequest) (adapter.QueryResult, error) {
		reads++
		return adapter.QueryResult{TradeNo: req.TradeNo, ProviderTradeNo: "original189-transaction", State: "SUCCESS", Currency: "CNY", Amount: 100}, nil
	}
	if _, err = f.s.ProviderFlow.Query(t.Context(), 1, order.TradeNo); err != nil || reads != 1 {
		t.Fatal("complete historical child could not recover after adoption", err, reads)
	}
	var sources int
	if err = f.p.QueryRow(t.Context(), `SELECT count(*) FROM runtime_parent_payment_sources WHERE merchant_id=1 AND out_trade_no=$1`, order.OutTradeNo).Scan(&sources); err != nil || sources != 0 {
		t.Fatal("historical parent source was fabricated", sources, err)
	}
	if _, err = f.p.Exec(t.Context(), `ALTER TABLE payment_subchannel_accounts DISABLE TRIGGER ALL;DELETE FROM payment_subchannel_accounts;ALTER TABLE payment_subchannel_accounts ENABLE TRIGGER ALL`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ProviderFlow.Query(t.Context(), 1, order.TradeNo); err == nil || reads != 1 {
		t.Fatal("lost child source borrowed adopted parent", err, reads)
	}
}

func TestParentChild189PreparedOriginalChildRequiresLiveOperatorAfterWholeMapReplacement(t *testing.T) {
	f, children := subchannel168BLocal(t)
	if err := f.s.InstallParentRuntimeAuthorization(); err != nil {
		t.Fatal(err)
	}
	if w := f.childHTTP("POST", "", `{"merchant_id":"1","parent_id":"7","name":"original prepared child189","enabled":true,"weight":1,"min_amount":"1","max_amount":"10000","values":{"app":"original-prepared189","secret":"synthetic-only-secret"}}`); w.Code != 200 {
		t.Fatal("child creation", w.Code)
	}
	params := compat.Params{"out_trade_no": "original-prepared189", "type": "alipay", "name": "prepared original189", "money": "1.00", "channel_id": "7", "method": "native", "clientip": "192.0.2.189"}
	channel, method, err := f.s.providerRoutedRequest(t.Context(), 1, params)
	if err != nil {
		t.Fatal(err)
	}
	view, err := f.s.paymentIntentServer(t.Context(), 1, params["out_trade_no"], channel)
	if err != nil {
		t.Fatal(err)
	}
	cost, err := view.ProviderFlow.PaymentCostQuote(channel, method)
	if err != nil {
		t.Fatal(err)
	}
	order, err := f.s.Store.CreateOrder(t.Context(), payment.CreateOrderInput{MerchantID: 1, OutTradeNo: params["out_trade_no"], Name: params["name"], Type: "alipay", Amount: 100, PaymentCost: cost})
	if err != nil {
		t.Fatal(err)
	}
	f.s.MerchantChannels = map[int64][]int64{}
	if _, err = view.ProviderFlow.Submit(t.Context(), order.TradeNo, channel, method, "192.0.2.189", ""); err == nil || children.providers["original-prepared189"].sends.Load() != 0 {
		t.Fatal("original child bypassed current root operator", err)
	}
}
