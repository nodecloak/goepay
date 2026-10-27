package payment

import (
	"bytes"
	"encoding/hex"
	"testing"
	"time"
)

func TestSubchannel168BNilChildPreservesFrozen166QuoteProof(t *testing.T) {
	s, e := NewWithKey(nil, bytes.Repeat([]byte{2}, 32))
	if e != nil {
		t.Fatal(e)
	}
	f := routeFeeFacts{Version: 1, MerchantRevision: 2, GroupUpdatedAt: time.Date(2026, 10, 25, 1, 2, 3, 4, time.UTC), MerchantID: 9007199254740993, ChannelID: 7, Amount: 100, GroupID: 3, ParentRevision: 1, ParentRetained: 9900, OutTradeNo: "synthetic-168b", Name: "合成商品", PayType: "alipay", Method: "native", GroupSnapshot: []byte(`{"payment_info":{"alipay":{"type":"channel","channel":"7"}},"group_id":"3"}`), GroupHash: bytes.Repeat([]byte{1}, 32), ParentIdentity: bytes.Repeat([]byte{2}, 32), AccountHash: bytes.Repeat([]byte{3}, 32), RouteHash: bytes.Repeat([]byte{4}, 32), ParentConfigured: true, ParentProduct: "alipay.payment.direct.rsa", QuotedAt: time.Date(2026, 10, 25, 1, 3, 4, 5, time.UTC), FeePolicy: "merchant_deduct", RefundPayer: "platform", Source: "parent", FeeRate: 100, FeeGroupID: 3, Net: 99, Fee: 1}
	proof, e := s.routeFeeProof(f)
	if e != nil {
		t.Fatal(e)
	}
	if hex.EncodeToString(proof) != "ff4cf1624a1fa452caf20f058ce39d1cea1e725002e6a899599f48d6f5b64208" {
		t.Fatal("nil-child v1 quote changed frozen166 authenticated bytes")
	}
}
