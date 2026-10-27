package server

import (
	"encoding/json"
	"testing"
)

func TestBuyer104Independent187OpaqueNumbers(t *testing.T) {
	var out struct {
		Body json.RawMessage `json:"body"`
	}
	raw := []byte(`{"body":{"ProviderBigNumber":1e1000,"CustomerKey":"preserved"}}`)
	if err := strictFundsJSON(raw, &out); err != nil {
		t.Fatal("opaque valid JSON number rejected before endpoint-owned decoder", err)
	}
	if string(out.Body) != `{"ProviderBigNumber":1e1000,"CustomerKey":"preserved"}` {
		t.Fatal("opaque raw content rewritten")
	}
}
func TestBuyer104Independent187EmbeddedTaggedDominance(t *testing.T) {
	type One struct{ Value string }
	type Two struct {
		Other string `json:"Value"`
	}
	var out struct {
		One
		Two
	}
	if err := strictFundsJSON([]byte(`{"Value":"tagged"}`), &out); err != nil || out.Other != "tagged" || out.One.Value != "" {
		t.Fatal("tagged equal-depth dominance mismatch", out, err)
	}
	type Three struct{ Value string }
	var ambiguous struct {
		One
		Three
	}
	if err := strictFundsJSON([]byte(`{"Value":"ambiguous"}`), &ambiguous); err == nil {
		t.Fatal("ambiguous embedded name accepted")
	}
}
