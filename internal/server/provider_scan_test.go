package server

import (
	"goepay/internal/compat"
	"testing"
)

func TestScanTypeNormalizationDoesNotMutateSignedParameters(t *testing.T) {
	p := compat.Params{"pid": "1", "method": "scan", "auth_code": "120000000000000001", "sign_type": "MD5"}
	p["sign"] = compat.SignMD5(p, "test-secret")
	before := compat.Canonical(p)
	derived, e := normalizeScanParams(p)
	if e != nil || derived["type"] != "wxpay" || p["type"] != "" || compat.Canonical(p) != before {
		t.Fatal("signature input mutated", e)
	}
	derived["auth_code"] = "changed"
	if p["auth_code"] != "120000000000000001" {
		t.Fatal("derived parameters alias signature input")
	}
}
