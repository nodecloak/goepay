package backoffice

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// a3 F4 / W3: the admin 后台加款/后台扣款 HTTP action end to end — credit, debit,
// debit-below-zero refused, idempotent replay, and merchant-forbidden — through
// ServeWorkflows → store.AdminAdjustBalance → immutable ledger. The funds-password
// guard itself is a server-layer concern (protectAdminFunds matches guard
// "admin-adjust"); here it is a no-op, so this covers the money path.
func TestAdminBalanceAdjustmentHTTP(t *testing.T) {
	s := setup(t)
	ctx := context.Background()

	post := func(p Principal, body string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/workflows/balance-adjustments", strings.NewReader(body))
		s.ServeWorkflows(w, r, p, []string{"balance-adjustments"})
		return w
	}
	bal := func() int64 { b, _ := s.store.Balance(ctx, 101); return b.Available }

	// A merchant principal can never reach the admin money action.
	if w := post(Principal{Role: "merchant", SubjectID: 101, Username: "m"},
		`{"merchant_id":"101","direction":"credit","amount":"5.00","reason":"x","external_no":"a"}`); w.Code != 403 {
		t.Fatalf("merchant adjust not forbidden: %d %s", w.Code, w.Body)
	}

	// 后台加款 5.00 → available 500 cents.
	if w := post(admin, `{"merchant_id":"101","direction":"credit","amount":"5.00","reason":"补偿","external_no":"cr-1"}`); w.Code != 201 {
		t.Fatalf("credit HTTP %d %s", w.Code, w.Body)
	}
	if bal() != 500 {
		t.Fatalf("after credit balance=%d", bal())
	}

	// Idempotent replay posts nothing new.
	if w := post(admin, `{"merchant_id":"101","direction":"credit","amount":"5.00","reason":"补偿","external_no":"cr-1"}`); w.Code != 201 {
		t.Fatalf("replay HTTP %d %s", w.Code, w.Body)
	}
	if bal() != 500 {
		t.Fatalf("replay double-posted balance=%d", bal())
	}

	// 后台扣款 3.00 → 200 cents.
	if w := post(admin, `{"merchant_id":"101","direction":"debit","amount":"3.00","reason":"回收","external_no":"db-1"}`); w.Code != 201 {
		t.Fatalf("debit HTTP %d %s", w.Code, w.Body)
	}
	if bal() != 200 {
		t.Fatalf("after debit balance=%d", bal())
	}

	// A debit beyond the balance is a 409 conflict/insufficient; balance intact.
	if w := post(admin, `{"merchant_id":"101","direction":"debit","amount":"2.01","reason":"超额","external_no":"db-over"}`); w.Code != 409 {
		t.Fatalf("over-debit status %d %s", w.Code, w.Body)
	}
	if bal() != 200 {
		t.Fatalf("over-debit moved balance=%d", bal())
	}

	// Conflicting reuse of an external_no (different amount) is 409.
	if w := post(admin, `{"merchant_id":"101","direction":"credit","amount":"9.00","reason":"x","external_no":"cr-1"}`); w.Code != 409 {
		t.Fatalf("conflict status %d %s", w.Code, w.Body)
	}

	// Bad direction is rejected (422).
	if w := post(admin, `{"merchant_id":"101","direction":"gift","amount":"1.00","reason":"x","external_no":"z"}`); w.Code != 422 {
		t.Fatalf("bad direction status %d %s", w.Code, w.Body)
	}

	// An unknown field is rejected (422), proving the allow-list.
	if w := post(admin, `{"merchant_id":"101","direction":"credit","amount":"1.00","reason":"x","external_no":"z2","evil":"1"}`); w.Code != 422 {
		t.Fatalf("unknown field status %d %s", w.Code, w.Body)
	}

	// GET lists the recorded adjustments for admin.
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/workflows/balance-adjustments", nil)
	s.ServeWorkflows(w, r, admin, []string{"balance-adjustments"})
	if w.Code != 200 {
		t.Fatalf("list HTTP %d %s", w.Code, w.Body)
	}
	var out struct {
		Data struct {
			Total int64            `json:"total"`
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	// cr-1 and db-1 posted; the over-debit and conflict left no row.
	if out.Data.Total != 2 {
		t.Fatalf("list total=%d want 2 (%s)", out.Data.Total, w.Body)
	}
}
