package server

import (
	"strings"
	"testing"

	"goepay/internal/compat"
)

func TestGroupPaymentDomainRequestIntentMatchesWireNormalization(t *testing.T) {
	name := strings.Repeat("界", 60)
	p := compat.Params{"money": "1.00", "name": name, "type": "sandbox", "out_trade_no": "sandbox-normalization"}
	if got := paymentPolicyRequestIntent(1, true, p); got == nil || got.Name != name || got.Version != "v1" {
		t.Fatal("sandbox replay changed the actual wire order name", got)
	}
	p["type"] = "wxpay"
	if got := paymentPolicyRequestIntent(1, false, p); got == nil || got.Name != truncateUTF8Bytes(name, 127) || got.Version != "v2" {
		t.Fatal("provider replay differs from actual UTF8 normalization", got)
	}
}

func TestGroupPaymentDomainActualCreateOriginalReplayAndGroupZero(t *testing.T) {
	f := channel168Local(t)
	ctx := t.Context()
	if out := f.pay(t, "domain-http-original", "1.00", 7); out["code"] != float64(1) {
		t.Fatal("synthetic original creation failed", out["msg"])
	}
	if _, e := f.p.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"pay_domain_forbid":"1"}'::jsonb WHERE resource='groups' AND id=0`); e != nil {
		t.Fatal(e)
	}
	if out := f.pay(t, "domain-http-closed", "1.00", 7); out["code"] == float64(1) {
		t.Error("HTTP create bypassed group domain gate")
	}
	if out := f.pay(t, "domain-http-original", "1.00", 7); out["code"] != float64(1) {
		t.Error("current group rejected exact authenticated original replay", out["msg"])
	}
	if _, e := f.p.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"pay_domain_forbid":"0"}'::jsonb WHERE resource='groups' AND id=0; INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"enforce_domains","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	if out := f.pay(t, "domain-http-zero", "1.00", 7); out["code"] != float64(1) {
		t.Error("explicit group zero ignored by HTTP create", out["msg"])
	}
}
