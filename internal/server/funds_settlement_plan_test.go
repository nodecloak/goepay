package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/identity"
)

func TestSettlementPlanChallengeRejectsNonSuperBeforeSnapshot(t *testing.T) {
	called := false
	s := &Server{Pool: &pgxpool.Pool{}}
	s.FundsActionResolver = func(context.Context, identity.FundsQuerier, string, json.RawMessage) (identity.FundsAction, map[string]string, error) {
		called = true
		return identity.FundsAction{}, nil, identity.ErrFundsInvalid
	}
	r := httptest.NewRequest("POST", "/console/v1/admin/security/funds-authorizations/challenges", strings.NewReader(`{"target":"/console/v1/admin/settlement-plans/1/authorizations","body":`+validSettlementPlanAuthorizationBody+`}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", "csrf")
	w := httptest.NewRecorder()
	ok := s.fundsSecurity(w, r, "admin", identity.Session{User: identity.User{Role: "admin", ID: 1, Username: "operator", AdminClass: "operator"}, CSRF: "csrf"}, []string{"admin", "security", "funds-authorizations", "challenges"}, "session")
	if !ok || w.Code != 403 || called {
		t.Fatalf("non-super reached authority snapshot: status=%d called=%v", w.Code, called)
	}
}

const validSettlementPlanAuthorizationBody = `{"expected_revision":"1","expected_authorization_id":"0","valid_from":"2026-10-23T00:00:00Z","expires_at":"2026-10-24T00:00:00Z","max_item_gross":"10000","max_run_gross":"100000","max_day_gross":"200000","max_day_items":"10"}`

func TestSettlementPlanFundsExactTarget(t *testing.T) {
	for _, path := range []string{"/console/v1/admin/settlement-plans/1/authorizations", "/console/v1/admin/settlement-plans/9223372036854775807/authorizations"} {
		d, ok := fundsTarget(path)
		if !ok || d.guard != "settlement-plan-authorize" || d.category != "settlement-plan" {
			t.Fatalf("missing protected plan authorization: %s %+v", path, d)
		}
	}
	for _, path := range []string{"/console/v1/merchant/settlement-plans/1/authorizations", "/console/v1/admin/settlement-plans/01/authorizations", "/console/v1/admin/settlement-plans/+1/authorizations", "/console/v1/admin/settlement-plans/0/authorizations", "/console/v1/admin/settlement-plans/1/authorizations?x=1", "/console/v1/admin/settlement-plans/1/runs", "/console/v1/admin/settlement-plan-authorizations/1/revoke", "/console/v1/admin/settlement-plans"} {
		if _, ok := fundsTarget(path); ok {
			t.Fatalf("unexpected funds target: %s", path)
		}
	}
}

func TestSettlementPlanAuthorizationBodyIsBoundedAndCanonical(t *testing.T) {
	d := fundsTargetDefinition{category: "settlement-plan"}
	fields, canonical, err := normalizeSettlementPlanFundsBody([]byte(validSettlementPlanAuthorizationBody), d)
	if err != nil || fundsText(fields, "expected_revision") != "1" || !json.Valid(canonical) {
		t.Fatalf("valid body rejected: %s %v", canonical, err)
	}
	for _, raw := range []string{
		strings.Replace(validSettlementPlanAuthorizationBody, `"expected_revision":"1"`, `"expected_revision":"01"`, 1),
		strings.Replace(validSettlementPlanAuthorizationBody, `"expected_authorization_id":"0"`, `"expected_authorization_id":0`, 1),
		strings.Replace(validSettlementPlanAuthorizationBody, `"max_item_gross":"10000"`, `"max_item_gross":"0"`, 1),
		strings.Replace(validSettlementPlanAuthorizationBody, `"max_item_gross":"10000"`, `"max_item_gross":"100001"`, 1),
		strings.Replace(validSettlementPlanAuthorizationBody, `"max_day_items":"10"`, `"max_day_items":"1.0"`, 1),
		strings.Replace(validSettlementPlanAuthorizationBody, `"expires_at":"2026-10-24T00:00:00Z"`, `"expires_at":"2026-12-24T00:00:00Z"`, 1),
		strings.Replace(validSettlementPlanAuthorizationBody, `"valid_from":"2026-10-23T00:00:00Z"`, `"valid_from":"2026-10-23"`, 1),
		strings.Replace(validSettlementPlanAuthorizationBody, `"max_day_items":"10"`, `"max_day_items":"10","max_day_items":"20"`, 1),
		strings.Replace(validSettlementPlanAuthorizationBody, `"max_day_items":"10"`, `"max_day_items":"10","mode":"auto_payout"`, 1),
		`{}`, `null`,
	} {
		if _, _, err := normalizeSettlementPlanFundsBody([]byte(raw), d); err == nil {
			t.Fatalf("ambiguous or unbounded body accepted: %s", raw)
		}
	}
}
