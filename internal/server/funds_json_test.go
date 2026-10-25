package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFundsStrictJSONAcceptsBoundedHundredItemBatchAndRejectsAmbiguity(t *testing.T) {
	items := []map[string]string{}
	for range 100 {
		items = append(items, map[string]string{"channel_id": "7", "type": "alipay", "account": strings.Repeat("a", 128), "name": strings.Repeat("b", 128), "amount": "1.25", "remark": strings.Repeat("c", 32)})
	}
	body, _ := json.Marshal(map[string]any{"target": "/console/v1/admin/transfer-batches", "body": map[string]any{"merchant_id": "1", "external_no": "hundred-items", "items": items}})
	if len(body) <= 32768 {
		t.Fatal("fixture must exceed prior authorization size bound", len(body))
	}
	var out map[string]json.RawMessage
	if e := strictFundsJSON(body, &out); e != nil {
		t.Fatal("legitimate hundred-item batch rejected", len(body), e)
	}
	for _, raw := range []string{`{"body":{"amount":"1","\u0061mount":"999"}}`, `{"body":{"items":[{"amount":"1","amount":"2"}]}}`, `{"body":{}} {}`, `{"a":` + strings.Repeat("[", 30) + `0` + strings.Repeat("]", 30) + `}`} {
		if e := strictFundsJSON([]byte(raw), &out); e == nil {
			t.Fatal("ambiguous or unbounded JSON accepted")
		}
	}
}
