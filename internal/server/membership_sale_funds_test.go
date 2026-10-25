package server

import "testing"

func TestMembershipSaleUXFundsQuantityIsCanonicalAndBounded(t *testing.T) {
	d, ok := fundsTarget("/console/v1/admin/workflows/memberships")
	if !ok {
		t.Fatal("missing membership target")
	}
	if _, _, e := normalizeFundsBody([]byte(`{"merchant_id":"1","group_id":"2","external_no":"sale","quantity":2}`), d); e != nil {
		t.Fatalf("quantity not supported: %v", e)
	}
	for _, q := range []string{`0`, `301`, `-1`, `1.5`, `"2"`, `null`, `true`} {
		if _, _, e := normalizeFundsBody([]byte(`{"merchant_id":"1","group_id":"2","external_no":"sale","quantity":`+q+`}`), d); e == nil {
			t.Fatalf("invalid quantity accepted: %s", q)
		}
	}
}
