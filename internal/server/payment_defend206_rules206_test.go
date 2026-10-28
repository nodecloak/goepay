package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

func defendRules206Configure(t *testing.T, f channel168Fixture, values map[string]string) {
	t.Helper()
	view := paymentDefend206Read(t, f)
	for k, v := range values {
		view.Settings[k] = v
	}
	body, err := json.Marshal(map[string]any{"settings": view.Settings, "expected_fingerprint": view.Fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	defendAdmission206Configured(t, paymentDefend206Request(f, http.MethodPatch, "", string(body), f.token, f.csrf))
}

func defendRules206Pays(t *testing.T, f channel168Fixture, w interface{ Result() *http.Response }, body []byte, sendsBefore int64) bool {
	var out map[string]any
	return json.Unmarshal(body, &out) == nil && out["code"] == float64(0) && defendJump206Sends(f) == sendsBefore+1
}

// Slice 4: pay_verify=1 rate rule over the merchant's own recent orders.
func TestPaymentDefend206ConditionalRateRuleFiresAtCountWithLowSuccess(t *testing.T) {
	f, key := paymentDefend206WireFixture(t, true)
	defendRules206Configure(t, f, map[string]string{"pay_verify": "1", "pay_verify_type": "3", "pay_verify_check_second": "3600", "pay_verify_check_count": "2", "pay_verify_check_rate": "50"})
	for i := 0; i < 2; i++ {
		sends := defendJump206Sends(f)
		w := defendSigned206Call(t, f, http.MethodPost, "/api/pay/create", defendSigned206Form(t, key, "rules206-rate-"+strconv.Itoa(i), nil))
		if !defendRules206Pays(t, f, w, w.Body.Bytes(), sends) {
			t.Fatalf("order %d below the count threshold was blocked: HTTP=%d body=%s", i, w.Code, w.Body.String())
		}
	}
	before := paymentDefend206Effects(t, f)
	sends := defendJump206Sends(f)
	defendSigned206Reject(t, f, before, sends, defendSigned206Call(t, f, http.MethodPost, "/api/pay/create", defendSigned206Form(t, key, "rules206-rate-3", nil)))
}

// With every order paid the success rate is 100 and the rule does not fire.
func TestPaymentDefend206ConditionalRateRuleSkipsHighSuccess(t *testing.T) {
	f, key := paymentDefend206WireFixture(t, true)
	defendRules206Configure(t, f, map[string]string{"pay_verify": "1", "pay_verify_type": "3", "pay_verify_check_second": "3600", "pay_verify_check_count": "1", "pay_verify_check_rate": "50"})
	sends := defendJump206Sends(f)
	// total 0 < count 1: first order passes.
	w := defendSigned206Call(t, f, http.MethodPost, "/api/pay/create", defendSigned206Form(t, key, "rules206-paid-0", nil))
	if !defendRules206Pays(t, f, w, w.Body.Bytes(), sends) {
		t.Fatalf("first order blocked: %s", w.Body.String())
	}
	if _, err := f.p.Exec(t.Context(), `ALTER TABLE orders DISABLE TRIGGER USER; UPDATE orders SET status='paid',paid_at=now() WHERE out_trade_no='rules206-paid-0'; ALTER TABLE orders ENABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	sends = defendJump206Sends(f)
	w = defendSigned206Call(t, f, http.MethodPost, "/api/pay/create", defendSigned206Form(t, key, "rules206-paid-1", nil))
	if !defendRules206Pays(t, f, w, w.Body.Bytes(), sends) {
		t.Fatalf("100%% success rate still triggered verification: %s", w.Body.String())
	}
}

// PHP 7: a zero total with count unset divides by zero -> rate 0 < rate.
func TestPaymentDefend206ConditionalRateRuleZeroTotalFollowsOriginal(t *testing.T) {
	f, key := paymentDefend206WireFixture(t, true)
	defendRules206Configure(t, f, map[string]string{"pay_verify": "1", "pay_verify_type": "3", "pay_verify_check_second": "60", "pay_verify_check_rate": "10"})
	before := paymentDefend206Effects(t, f)
	defendSigned206Reject(t, f, before, 0, defendSigned206Call(t, f, http.MethodPost, "/api/pay/create", defendSigned206Form(t, key, "rules206-zero", nil)))
}

// IP rule: the last N orders of this client IP within an hour are unpaid.
func TestPaymentDefend206ConditionalIPRuleFiresAfterUnpaidRun(t *testing.T) {
	f, key := paymentDefend206WireFixture(t, true)
	defendRules206Configure(t, f, map[string]string{"pay_verify": "1", "pay_verify_type": "3", "pay_verify_check_ip": "2"})
	for i := 0; i < 2; i++ {
		sends := defendJump206Sends(f)
		w := defendSigned206Call(t, f, http.MethodPost, "/api/pay/create", defendSigned206Form(t, key, "rules206-ip-"+strconv.Itoa(i), nil))
		if !defendRules206Pays(t, f, w, w.Body.Bytes(), sends) {
			t.Fatalf("IP rule fired before N unpaid orders: %s", w.Body.String())
		}
	}
	before := paymentDefend206Effects(t, f)
	sends := defendJump206Sends(f)
	defendSigned206Reject(t, f, before, sends, defendSigned206Call(t, f, http.MethodPost, "/api/pay/create", defendSigned206Form(t, key, "rules206-ip-2", nil)))
}

// A rule trigger on the browser route with jump type renders the jump page,
// and its token admits once even though admitting adds another unpaid order.
func TestPaymentDefend206ConditionalRuleUsesJumpOnBrowserSubmit(t *testing.T) {
	f, key := paymentDefend206WireFixture(t, true)
	defendRules206Configure(t, f, map[string]string{"pay_verify": "1", "pay_verify_type": "0", "pay_verify_check_second": "60", "pay_verify_check_rate": "10"})
	form := defendSigned206Form(t, key, "rules206-jump", nil)
	token := defendJump206Mint(t, f, "/api/pay/submit", form)
	sends := defendJump206Sends(f)
	if w := defendSigned206Call(t, f, http.MethodPost, "/api/pay/submit", defendJump206With(form, token)); w.Code != http.StatusFound || defendJump206Sends(f) != sends+1 {
		t.Fatalf("rule-triggered jump token did not admit once: HTTP=%d body=%s", w.Code, w.Body.String())
	}
}
