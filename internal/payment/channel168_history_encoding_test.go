package payment

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestChannel168HistoricalBirthMACBytesRemainExact(t *testing.T) {
	// Fixed vectors use the frozen pre96 array encodings and a synthetic key.
	// A false marker must not append an empty object or alter those old bytes.
	s := &Store{indexKey: bytes.Repeat([]byte{1}, 32)}
	o := Order{ID: 11, TradeNo: "t", MerchantID: 1, OutTradeNo: "old", Amount: 125, MerchantAmount: 125, FeePolicy: "none", RefundFeePayer: "platform"}
	terms := referralOrderTerms{OrderID: 11, MerchantID: 1, Purpose: "payment", Reason: "no_relation", RefundPolicy: referralRefundPolicy}
	old, e := s.referralTermsProof(o, terms)
	if e != nil || hex.EncodeToString(old) != "d87741dfe668000dde93a54088ffb946c8508d4a2af44b8ed3d57eae0efd618f" {
		t.Fatal("old terms MAC encoding changed", e)
	}
	source, e := s.referralOrderSourceProof(o, old, nil)
	if e != nil || hex.EncodeToString(source) != "df830ef427c29e2d6d411aa8e7cf6ade38e509cd2a197a2c57d9e57fda660eb2" {
		t.Fatal("old source MAC encoding changed", e)
	}
	o.routeFeeRequired = true
	fresh, e := s.referralTermsProof(o, terms)
	if e != nil || bytes.Equal(fresh, old) {
		t.Fatal("new birth marker omitted from independent terms commitment", e)
	}
	next, e := s.referralOrderSourceProof(o, old, nil)
	if e != nil || bytes.Equal(next, source) {
		t.Fatal("new birth marker omitted from independent source commitment", e)
	}
}
