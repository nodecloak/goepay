package backoffice

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"goepay/internal/payment"
)

func TestGroupPaymentDomainPolicyUsesEffectiveGroupAndBothApprovedURLs(t *testing.T) {
	s := setup(t)
	ctx := t.Context()
	set := func(value any) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"pay_domain_forbid": value})
		if _, e := s.pool.Exec(ctx, `UPDATE backoffice_records SET body=body||$1::jsonb WHERE resource='groups' AND id=0`, string(raw)); e != nil {
			t.Fatal(e)
		}
	}
	pc := PaymentContext{MerchantID: 101, NotifyURL: "https://shop.example.com/notify", ReturnURL: "https://return.example.net/return"}
	set("1")
	if e := s.CheckPayment(ctx, pc); !errors.Is(e, ErrForbidden) {
		t.Error("group-on did not reject unapproved callback domains", e)
	}
	if _, e := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,owner_id,body) VALUES ('domains',101,'{"domain":"*.example.com","approved":true,"enabled":true}'),('domains',102,'{"domain":"return.example.net","approved":true,"enabled":true}')`); e != nil {
		t.Fatal(e)
	}
	if e := s.CheckPayment(ctx, pc); !errors.Is(e, ErrForbidden) {
		t.Error("return URL borrowed another merchant's approval", e)
	}
	pc.ReturnURL = "https://return.example.com/return"
	if e := s.CheckPayment(ctx, pc); e != nil {
		t.Error("both owned approved URLs should pass", e)
	}
	seedSetting(t, s, "enforce_domains", "true")
	pc.NotifyURL = "https://unapproved.example.org/notify"
	set("0")
	if e := s.CheckPayment(ctx, pc); e != nil {
		t.Error("explicit group zero did not override global true", e)
	}
	for _, value := range []any{"", "_", nil} {
		set(value)
		if e := s.CheckPayment(ctx, pc); !errors.Is(e, ErrForbidden) {
			t.Error("inherited global domain rule was lost", value, e)
		}
	}
	set("corrupt")
	if e := s.CheckPayment(ctx, pc); e == nil {
		t.Error("corrupt group domain policy failed open")
	}
}

func TestGroupPaymentDomainExactReplayRetainsOtherRiskGuards(t *testing.T) {
	s := setup(t)
	ctx := t.Context()
	in := payment.CreateOrderInput{MerchantID: 101, OutTradeNo: "domain-policy-original", Name: "synthetic goods", Amount: 100, Type: "wxpay", NotifyURL: "https://original.example.com/notify", ReturnURL: "https://original.example.com/done", Param: "original-param"}
	if _, e := s.store.CreateOrder(ctx, in); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"pay_domain_forbid":"1"}'::jsonb WHERE resource='groups' AND id=0`); e != nil {
		t.Fatal(e)
	}
	pc := PaymentContext{MerchantID: 101, ClientIP: "192.0.2.77", NotifyURL: in.NotifyURL, ReturnURL: in.ReturnURL, Name: in.Name, OrderIntent: &in}
	if e := s.CheckPayment(ctx, pc); e != nil {
		t.Fatal("exact immutable replay rejected by new domain configuration", e)
	}
	changed := in
	changed.Amount++
	pc.OrderIntent = &changed
	if e := s.CheckPayment(ctx, pc); !errors.Is(e, ErrForbidden) {
		t.Error("an original number alone bypassed new intent policy", e)
	}
	pc.OrderIntent = &in
	pc.ReturnURL = "https://changed.example.com/done"
	if e := s.CheckPayment(ctx, pc); !errors.Is(e, ErrForbidden) {
		t.Error("mismatched context borrowed original callback approval", e)
	}
	pc.ReturnURL = in.ReturnURL
	if _, e := s.Create(ctx, admin, "blacklist", input(`{"kind":"ip","value":"192.0.2.77","reason":"synthetic replay risk","enabled":true}`)); e != nil {
		t.Fatal(e)
	}
	if e := s.CheckPayment(ctx, pc); !errors.Is(e, ErrForbidden) {
		t.Error("domain replay exemption bypassed live payer risk", e)
	}
}

func TestGroupPaymentSettingsSchemaAndTemplateValidation(t *testing.T) {
	s := setup(t)
	schema, e := s.Schema(admin, "groups")
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"pay_domain_forbid", "ordername"} {
		found := false
		for _, f := range schema.Fields {
			if f.Name == name && !f.Required {
				found = true
			}
		}
		if !found {
			t.Errorf("missing optional group payment field %s", name)
		}
	}
	for i, value := range []string{"", "_", "0", "1"} {
		item, e := s.Create(t.Context(), admin, "groups", input(fmt.Sprintf(`{"name":"payment-%d","pay_domain_forbid":%q,"ordername":"Go-[name]-[order]-[outorder]-[qq]-[phone]-[time]"}`, i, value)))
		if e != nil || item.Fields["pay_domain_forbid"] != value {
			t.Fatal("group payment field did not persist", value, e)
		}
	}
	for _, raw := range []string{`"pay_domain_forbid":"true"`, `"pay_domain_forbid":0`, `"pay_domain_forbid":null`, `"ordername":"x|"`, `"ordername":"x\n"`, `"ordername":0`, `"ordername":null`} {
		if _, e := s.Create(t.Context(), admin, "groups", input(`{"name":"invalid-payment",`+raw+`}`)); !errors.Is(e, ErrInvalid) {
			t.Error("invalid group payment setting persisted", raw, e)
		}
	}
}
