package main

import (
	"strings"
	"testing"
)

func TestWorkerRequiresIndependentNodesAndExplicitWriteGate(t *testing.T) {
	for _, raw := range []string{`{}`, `{"networks":{"tron":{"enabled":true,"primary_rpc":"https://a.example","witness_rpc":"https://a.example","start_height":1}}}`, `{"enable_broadcast":true,"networks":{}}`, `{"private_key":"secret"}`} {
		if _, e := parseConfig(strings.NewReader(raw)); e == nil {
			t.Fatalf("unsafe config accepted %s", raw)
		}
	}
	_, e := parseConfig(strings.NewReader(`{"networks":{"tron":{"enabled":true,"primary_rpc":"https://primary.example","witness_rpc":"https://witness.example","start_height":100,"fee_bps":100}}}`))
	if e != nil {
		t.Fatal(e)
	}
}
