package server

import (
	"net/http/httptest"
	"strings"
	"testing"

	"goepay/internal/identity"
)

func TestSettlementPlanConsoleRequiresOriginalOwnerAndSuper(t *testing.T) {
	for _, tc := range []struct {
		role, path, class string
		issuer            bool
	}{
		{"admin", "settlement-plans", "operator", false},
		{"merchant", "settlement-plans", "", false},
		{"admin", "settlement-plan-runs/1", "super", true},
		{"admin", "account/settlement-schedule", "super", false},
		{"merchant", "account/settlement-schedule", "", true},
	} {
		session := identity.Session{User: identity.User{ID: 1, Username: "local", Role: tc.role, AdminClass: tc.class}, CSRF: "csrf"}
		if tc.issuer {
			session.Issuer = &identity.SessionIssuer{ID: 1}
		}
		r := httptest.NewRequest("GET", "/console/v1/"+tc.role+"/"+tc.path, nil)
		w := httptest.NewRecorder()
		if !(&Server{}).settlementPlanConsole(w, r, tc.role, session, append([]string{tc.role}, strings.Split(tc.path, "/")...)) || w.Code != 403 {
			t.Fatalf("ownership bypass %s/%s status=%d", tc.role, tc.path, w.Code)
		}
	}
}

func TestSettlementPlanConsoleWritesRequireCSRF(t *testing.T) {
	session := identity.Session{User: identity.User{ID: 1, Username: "super", Role: "admin", AdminClass: "super"}, CSRF: "csrf"}
	r := httptest.NewRequest("POST", "/console/v1/admin/settlement-plans", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	if !(&Server{}).settlementPlanConsole(w, r, "admin", session, []string{"admin", "settlement-plans"}) || w.Code != 403 {
		t.Fatalf("plan mutation accepted without csrf: %d", w.Code)
	}
}

func TestSettlementPlanDefinitionRejectsNullScheduleAndAmbiguousQuery(t *testing.T) {
	const valid = `{"name":"local","mode":"generate_only","merchant_ids":["1"],"schedule":{"frequency":"hour","interval":"1","anchor_at":"2026-10-23T00:00:00Z","execute_hour":0,"execute_minute":0,"execute_second":0}}`
	for _, field := range []string{"execute_hour", "execute_minute", "execute_second"} {
		body := strings.Replace(valid, `"`+field+`":0`, `"`+field+`":null`, 1)
		r := httptest.NewRequest("POST", "/console/v1/admin/settlement-plans", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		if _, _, ok := decodeSettlementPlanDefinition(w, r, false); ok {
			t.Fatalf("null schedule field accepted as explicit zero: %s", field)
		}
	}
	for _, query := range []string{"limit=1;owner=2", "limit=%ZZ", "after_id=0&limit=20&limit=30"} {
		r := httptest.NewRequest("GET", "/console/v1/admin/settlement-plans?"+query, nil)
		w := httptest.NewRecorder()
		if _, _, ok := settlementPlanPaging(w, r); ok {
			t.Fatalf("ambiguous query accepted: %s", query)
		}
	}
}
