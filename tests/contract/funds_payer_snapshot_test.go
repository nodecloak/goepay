package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/payment"
	"net/http"
	"strings"
	"testing"
)

func TestFundsActionSnapshotBindsWechatMerchantAndAllowsKeyRotation(t *testing.T) {
	s, pool, _ := fixture(t)
	makeProvider := func(mch, key string) adapter.Provider {
		t.Helper()
		p, e := adapter.NewWechatV2(adapter.WechatV2Config{AppID: "same-local-app", MerchantID: mch, APIKey: key, RefundClient: http.DefaultClient})
		if e != nil {
			t.Fatal(e)
		}
		return p
	}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: makeProvider("original", strings.Repeat("a", 32))}, AllowLive: true}
	s.MerchantChannels = map[int64][]int64{1: {7}}
	target := "/console/v1/admin/transfers"
	body := json.RawMessage(`{"merchant_id":"1","channel_id":"7","type":"wxpay","account":"synthetic-openid","name":"测试姓名","amount":"1.25","remark":"local-only","external_no":"payer-snapshot"}`)
	resolve := func() []byte {
		t.Helper()
		a, _, e := s.ResolveFundsAction(context.Background(), pool, target, body)
		if e != nil {
			t.Fatal(e)
		}
		return a.Digest()
	}
	original := resolve()
	s.ProviderFlow.Providers[7] = makeProvider("original", strings.Repeat("b", 32))
	if !bytes.Equal(original, resolve()) {
		t.Fatal("key rotation rebound payer")
	}
	s.ProviderFlow.Providers[7] = makeProvider("changed", strings.Repeat("b", 32))
	if bytes.Equal(original, resolve()) {
		t.Fatal("same AppID different mchid inherited financial confirmation")
	}
}
