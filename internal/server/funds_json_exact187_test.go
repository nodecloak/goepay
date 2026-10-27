package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"goepay/internal/payment"
)

func TestFunds187ExactBatchJSONRejectsCaseAliases(t *testing.T) {
	for _, raw := range []string{
		`{"action":"notify","Action":"delete","external_no":"same","items":[{"trade_no":"T1"}]}`,
		`{"Action":"notify","external_no":"same","items":[{"trade_no":"T1"}]}`,
		`{"action":"notify","external_no":"same","Items":[{"trade_no":"T1"}]}`,
		`{"action":"notify","external_no":"same","items":[{"Trade_no":"T1"}]}`,
	} {
		t.Run(raw, func(t *testing.T) {
			var in payment.OrderBusinessBatchInput
			r := httptest.NewRequest("POST", "/console/v1/admin/orders/business-batches", strings.NewReader(raw))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			if decodeFundsJSON(w, r, &in) || w.Code != 400 {
				t.Fatalf("noncanonical batch fields accepted: status=%d action=%q", w.Code, in.Action)
			}
		})
	}
	var in payment.OrderBusinessBatchInput
	if err := strictFundsJSON([]byte(`{"action":"notify","external_no":"same","items":[{"trade_no":"T1"}]}`), &in); err != nil || in.Action != "notify" {
		t.Fatal("original exact DTO rejected", err)
	}
}

func TestFunds187EmbeddedFieldDominanceUsesExactNames(t *testing.T) {
	type Embedded struct {
		Value string `json:"value"`
	}
	var in struct {
		Embedded
		Value    string  `json:"value"`
		Optional *string `json:"optional,omitempty"`
	}
	if err := strictFundsJSON([]byte(`{"value":"direct","optional":null}`), &in); err != nil || in.Value != "direct" || in.Embedded.Value != "" {
		t.Fatal("canonical embedded DTO changed", err, in)
	}
	if err := strictFundsJSON([]byte(`{"Value":"case-alias"}`), &in); err == nil {
		t.Fatal("embedded field accepted alias")
	}
}

func TestFunds187TransferPreviewRejectsNestedCaseAliases(t *testing.T) {
	target, ok := fundsTarget("/console/v1/admin/transfer-batches")
	if !ok {
		t.Fatal("original funds target missing")
	}
	for _, member := range []string{`"amount":"1.00","Amount":"999.00"`, `"Amount":"1.00"`, `"channel_id":"7","Channel_id":"8","amount":"1.00"`} {
		raw := []byte(`{"merchant_id":"1","external_no":"exact-items","items":[{"type":"alipay","account":"recipient","name":"name",` + member + `}]}`)
		if _, _, err := normalizeFundsBody(raw, target); err == nil {
			t.Fatal("nested case alias accepted in original authorization preview", member)
		}
	}
	raw := []byte(`{"merchant_id":"1","external_no":"exact-items","items":[{"type":"alipay","account":"recipient","name":"name","amount":"1.00"}]}`)
	if _, _, err := normalizeFundsBody(raw, target); err != nil {
		t.Fatal("canonical transfer item rejected", err)
	}
}

func TestFunds187ExactFieldsRespectOpaqueBodiesAndMapKeys(t *testing.T) {
	var in struct {
		Body   json.RawMessage `json:"body"`
		Labels map[string]struct {
			Value string `json:"value"`
		} `json:"labels"`
	}
	if err := strictFundsJSON([]byte(`{"body":{"ProviderKey":"unchanged"},"labels":{"CustomerID":{"value":"kept"}}}`), &in); err != nil {
		t.Fatal(err)
	}
	if err := strictFundsJSON([]byte(`{"body":{},"labels":{"CustomerID":{"Value":"alias"}}}`), &in); err == nil {
		t.Fatal("nested typed field accepted a case alias")
	}
}
