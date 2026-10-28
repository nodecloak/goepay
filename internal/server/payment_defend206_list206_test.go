package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func defendList206Configure(t *testing.T, f channel168Fixture, mode, kind, uids string) {
	t.Helper()
	view := paymentDefend206Read(t, f)
	view.Settings["pay_verify"], view.Settings["pay_verify_type"], view.Settings["pay_verify_check_uid"] = mode, kind, uids
	body, err := json.Marshal(map[string]any{"settings": view.Settings, "expected_fingerprint": view.Fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	defendAdmission206Configured(t, paymentDefend206Request(f, http.MethodPatch, "", string(body), f.token, f.csrf))
}

// Slice 3: pay_verify=2 triggers only for listed merchants; an unlisted
// merchant pays normally on every route, including mapi/create.
func TestPaymentDefend206MerchantListTriggersOnlyListedMerchants(t *testing.T) {
	t.Run("listed-browser-gets-jump", func(t *testing.T) {
		f, key := paymentDefend206WireFixture(t, true)
		defendList206Configure(t, f, "2", "0", "7|1")
		before := paymentDefend206Effects(t, f)
		w := defendSigned206Call(t, f, http.MethodPost, "/api/pay/submit", defendSigned206Form(t, key, "list206-listed", nil))
		if w.Code != http.StatusOK || !defendJump206Token.MatchString(w.Body.String()) || paymentDefend206Effects(t, f) != before {
			t.Fatalf("listed merchant was not sent to verification: HTTP=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("listed-api-blocked", func(t *testing.T) {
		f, key := paymentDefend206WireFixture(t, true)
		defendList206Configure(t, f, "2", "0", "1")
		before := paymentDefend206Effects(t, f)
		defendSigned206Reject(t, f, before, 0, defendSigned206Call(t, f, http.MethodPost, "/api/pay/create", defendSigned206Form(t, key, "list206-api", nil)))
	})
	for _, path := range []string{"/api/pay/submit", "/api/pay/create"} {
		t.Run("unlisted-pays"+strings.ReplaceAll(path, "/", "-"), func(t *testing.T) {
			f, key := paymentDefend206WireFixture(t, true)
			defendList206Configure(t, f, "2", "3", "2|3")
			sends := defendJump206Sends(f)
			w := defendSigned206Call(t, f, http.MethodPost, path, defendSigned206Form(t, key, "list206-unlisted", nil))
			ok := w.Code == http.StatusFound
			if path == "/api/pay/create" {
				var out map[string]any
				ok = w.Code == http.StatusOK && json.Unmarshal(w.Body.Bytes(), &out) == nil && out["code"] == float64(0)
			}
			if !ok || defendJump206Sends(f) != sends+1 {
				t.Fatalf("unlisted merchant was blocked by a list it is not on: HTTP=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
	t.Run("mode1-unlisted-skips-rules", func(t *testing.T) {
		f, key := paymentDefend206WireFixture(t, true)
		defendList206Configure(t, f, "1", "3", "5")
		sends := defendJump206Sends(f)
		w := defendSigned206Call(t, f, http.MethodPost, "/api/pay/submit", defendSigned206Form(t, key, "list206-mode1", nil))
		if w.Code != http.StatusFound || defendJump206Sends(f) != sends+1 {
			t.Fatalf("mode 1 applied rules to a merchant outside its uid filter: HTTP=%d body=%s", w.Code, w.Body.String())
		}
	})
}
