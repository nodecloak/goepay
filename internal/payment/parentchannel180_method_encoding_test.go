package payment

import (
	"encoding/json"
	"testing"
)

func TestParentChannel180UnboundPaymentSourceKeepsOriginalMACInputBytes(t *testing.T) {
	source := parentPaymentSource{Version: 1, Merchant: 1, Channel: 9, AccountVersion: 2, Source: 8, Amount: 100, OutTradeNo: "original", Kind: "wxpay", Method: "native", AccountProof: []byte{1}, FeeIdentity: []byte{2}, FeeAccountHash: []byte{3}, QuoteProof: []byte{4}, RouteHash: []byte{5}, FeeProduct: "original-product", FeeMethods: []string{"native"}}
	raw, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	const original = `{"Version":1,"Merchant":1,"Channel":9,"AccountVersion":2,"Source":8,"Amount":100,"OutTradeNo":"original","Kind":"wxpay","Method":"native","AccountProof":"AQ==","FeeIdentity":"Ag==","FeeAccountHash":"Aw==","QuoteProof":"BA==","RouteHash":"BQ==","FeeProduct":"original-product","FeeMethods":["native"]}`
	if string(raw) != original {
		t.Fatal("optional execution variant changed original MAC bytes", string(raw))
	}
	var parsed parentPaymentSource
	if parentFactsJSON(raw, &parsed) != nil {
		t.Fatal("original facts no longer decode")
	}
}
