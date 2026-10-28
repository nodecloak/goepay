package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

type defendGroup206View struct {
	GroupID            string `json:"group_id"`
	GroupName          string `json:"group_name"`
	Configured         bool   `json:"configured"`
	Revision           string `json:"revision"`
	PayVerify          string `json:"pay_verify"`
	EffectivePayVerify string `json:"effective_pay_verify"`
	Fingerprint        string `json:"fingerprint"`
}

func defendGroup206Read(t *testing.T, f channel168Fixture, id string) defendGroup206View {
	t.Helper()
	w := paymentDefend206Request(f, http.MethodGet, "/groups/"+id, "", f.token, f.csrf)
	var out struct {
		Code int                `json:"code"`
		Data defendGroup206View `json:"data"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Code != 0 || out.Data.Fingerprint == "" {
		t.Fatalf("group override read failed: HTTP=%d body=%s", w.Code, w.Body.String())
	}
	return out.Data
}

func defendGroup206Save(t *testing.T, f channel168Fixture, id, mode string) defendGroup206View {
	t.Helper()
	v := defendGroup206Read(t, f, id)
	body, _ := json.Marshal(map[string]string{"pay_verify": mode, "expected_fingerprint": v.Fingerprint})
	w := paymentDefend206Request(f, http.MethodPatch, "/groups/"+id, string(body), f.token, f.csrf)
	var out struct {
		Code int                `json:"code"`
		Data defendGroup206View `json:"data"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Code != 0 || out.Data.PayVerify != mode {
		t.Fatalf("group override save failed: HTTP=%d body=%s", w.Code, w.Body.String())
	}
	return out.Data
}

// Slice 3: a group override replaces the global trigger for its merchants;
// "" inherits; a tampered override fails closed instead of inheriting.
func TestPaymentDefend206GroupOverrideInheritsOverridesAndFailsClosed(t *testing.T) {
	f, key := paymentDefend206WireFixture(t, true)
	defendSigned206Configure(t, f, "3") // global: forced trigger, forbid payment
	before := paymentDefend206Effects(t, f)
	defendSigned206Reject(t, f, before, 0, defendSigned206Call(t, f, http.MethodPost, "/api/pay/create", defendSigned206Form(t, key, "group206-global", nil)))

	v := defendGroup206Save(t, f, "0", "0") // default group: off
	if v.EffectivePayVerify != "0" || v.Revision != "1" {
		t.Fatal("group override view did not reflect the override", v)
	}
	sends := defendJump206Sends(f)
	w := defendSigned206Call(t, f, http.MethodPost, "/api/pay/submit", defendSigned206Form(t, key, "group206-off", nil))
	if w.Code != http.StatusFound || defendJump206Sends(f) != sends+1 {
		t.Fatalf("group override off did not release its merchant: HTTP=%d body=%s", w.Code, w.Body.String())
	}

	v = defendGroup206Save(t, f, "0", "") // inherit again
	if v.EffectivePayVerify != "3" || v.Revision != "2" {
		t.Fatal("inherit did not restore the global trigger", v)
	}
	before = paymentDefend206Effects(t, f)
	sends = defendJump206Sends(f)
	defendSigned206Reject(t, f, before, sends, defendSigned206Call(t, f, http.MethodPost, "/api/pay/submit", defendSigned206Form(t, key, "group206-inherit", nil)))

	t.Run("bad-value", func(t *testing.T) {
		cur := defendGroup206Read(t, f, "0")
		body, _ := json.Marshal(map[string]string{"pay_verify": "2", "expected_fingerprint": cur.Fingerprint})
		if w := paymentDefend206Request(f, http.MethodPatch, "/groups/0", string(body), f.token, f.csrf); w.Code != 422 {
			t.Fatalf("group override accepted a value the original group form lacks: HTTP=%d", w.Code)
		}
	})
	t.Run("stale-fingerprint", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"pay_verify": "0", "expected_fingerprint": v.Fingerprint + ""})
		defendGroup206Save(t, f, "0", "3")
		if w := paymentDefend206Request(f, http.MethodPatch, "/groups/0", string(body), f.token, f.csrf); w.Code != 409 {
			t.Fatalf("stale group override write was accepted: HTTP=%d", w.Code)
		}
	})
	t.Run("tampered-fails-closed", func(t *testing.T) {
		defendGroup206Save(t, f, "0", "0")
		if _, err := f.p.Exec(t.Context(), `ALTER TABLE payment_defend206_groups DISABLE TRIGGER payment_defend206_groups_original; UPDATE payment_defend206_groups SET pay_verify='' WHERE group_id=0; ALTER TABLE payment_defend206_groups ENABLE TRIGGER payment_defend206_groups_original`); err != nil {
			t.Fatal(err)
		}
		before := paymentDefend206Effects(t, f)
		sends := defendJump206Sends(f)
		defendSigned206Reject(t, f, before, sends, defendSigned206Call(t, f, http.MethodPost, "/api/pay/submit", defendSigned206Form(t, key, "group206-tamper", nil)))
	})
}

func TestPaymentDefend206GroupForcedOverrideTriggersJumpWhenGlobalOff(t *testing.T) {
	f, key := paymentDefend206WireFixture(t, true)
	defendList206Configure(t, f, "0", "0", "")
	defendGroup206Save(t, f, "0", "3")
	before := paymentDefend206Effects(t, f)
	w := defendSigned206Call(t, f, http.MethodPost, "/api/pay/submit", defendSigned206Form(t, key, "group206-forced", nil))
	if w.Code != http.StatusOK || !defendJump206Token.MatchString(w.Body.String()) || paymentDefend206Effects(t, f) != before {
		t.Fatalf("forced group override did not require verification: HTTP=%d body=%s", w.Code, w.Body.String())
	}
	form := defendSigned206Form(t, key, "group206-forced-ok", nil)
	token := defendJump206Mint(t, f, "/api/pay/submit", form)
	sends := defendJump206Sends(f)
	if w := defendSigned206Call(t, f, http.MethodPost, "/api/pay/submit", defendJump206With(form, token)); w.Code != http.StatusFound || defendJump206Sends(f) != sends+1 {
		t.Fatalf("forced group override token did not admit once: HTTP=%d body=%s", w.Code, w.Body.String())
	}
}
