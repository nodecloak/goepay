package contract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/identity"
)

// unqueryableRefunds sends refunds but, like most ported plugins, cannot query
// their outcome: an unfinished refund has no automatic path to a final state.
type unqueryableRefunds struct{ *merchant174Provider }

func (unqueryableRefunds) Capabilities() adapter.Capabilities {
	return adapter.Capabilities{Methods: []string{"native"}, Query: true, Refund: true}
}

func TestRefundAttestationClosesUnqueryableRefundsWithFundsGrant(t *testing.T) {
	f, p, o := merchant174Runtime(t)
	ctx := context.Background()
	s, pool := f.s, f.p
	s.ProviderFlow.Providers[7] = unqueryableRefunds{p}
	s.FundsActionResolver = s.ResolveFundsAction
	user := identity.User{Username: "admin", Role: "admin", ID: 1, AuthVersion: 1, AdminClass: "super"}
	token, csrf, e := s.Sessions.Create(user)
	if e != nil {
		t.Fatal(e)
	}
	funds := identity.NewFundsAuthorization(pool, nil)
	if e = funds.SetPassword(ctx, identity.FundsActor{User: user, SessionToken: token, ClientIP: "192.0.2.1"}, "contract-password-123", "", "attest-funds-password-456", ""); e != nil {
		t.Fatal(e)
	}
	request := func(method, path, body, grant string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		r.Header.Set("X-CSRF-Token", csrf)
		if grant != "" {
			r.Header.Set("X-Payment-Authorization", grant)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	grantFor := func(target, body string) string {
		t.Helper()
		preview, _ := json.Marshal(map[string]any{"target": target, "body": json.RawMessage(body)})
		w := request("POST", "/console/v1/admin/security/funds-authorizations/challenges", string(preview), "")
		if w.Code != 201 {
			t.Fatal("challenge", w.Code, w.Body.String())
		}
		var challenge struct{ Data identity.FundsChallenge }
		json.Unmarshal(w.Body.Bytes(), &challenge)
		w = request("POST", "/console/v1/admin/security/funds-authorizations/confirm", fmt.Sprintf(`{"challenge_id":%q,"payment_password":"attest-funds-password-456"}`, challenge.Data.ChallengeID), "")
		if w.Code != 200 {
			t.Fatal("confirm", w.Code, w.Body.String())
		}
		var grant struct{ Data identity.FundsGrant }
		json.Unmarshal(w.Body.Bytes(), &grant)
		return grant.Data.Authorization
	}
	view := func(w *httptest.ResponseRecorder) map[string]any {
		t.Helper()
		var out struct{ Data map[string]any }
		if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
			t.Fatal(w.Body.String())
		}
		return out.Data
	}
	balance := func(available, reserved int64) {
		t.Helper()
		b, e := s.Store.Balance(ctx, 2)
		if e != nil || b.Available != available || b.Reserved != reserved {
			t.Fatalf("balance %+v, want available %d reserved %d (%v)", b, available, reserved, e)
		}
	}

	sent := merchant174Invoke(t, f, "/api/pay/refund", compat.Params{"out_trade_no": o.OutTradeNo, "out_refund_no": "attest-success", "money": "1.00"})
	if sent["code"] != float64(0) || sent["refund_state"] != "processing" || p.refundWrites.Load() != 1 {
		t.Fatal("refund not sent as processing", sent)
	}
	balance(900, 100)
	// The channel cannot query: the stored state is reported, not an outage.
	if q := merchant174Invoke(t, f, "/api/pay/refundquery", compat.Params{"out_refund_no": "attest-success"}); q["code"] != float64(-1) || q["msg"] != "refund processing" || q["refund_state"] != "processing" {
		t.Fatal("unqueryable refund state not reported", q)
	}
	id := sent["refund_no"].(string)
	base := "/console/v1/admin/refunds/" + id
	read := request("GET", base+"/attestation", "", "")
	if v := view(read); read.Code != 200 || v["eligible"] != true || v["sent"] != true || v["status"] != "pending" || len(v["states"].([]any)) != 2 {
		t.Fatal("eligible unqueryable refund", read.Code, read.Body.String())
	}
	target := base + "/attest-result"
	body := `{"state":"succeeded","provider_reference":"ALI-REFUND-20261106-1","note":"已在支付宝商家后台核实该笔退款成功"}`
	if w := request("POST", target, body, ""); w.Code != 428 {
		t.Fatal("attestation escaped the mandatory funds grant", w.Code, w.Body.String())
	}
	grant := grantFor(target, body)
	if w := request("POST", target, strings.Replace(body, "succeeded", "failed", 1), grant); w.Code != 428 {
		t.Fatal("grant did not bind the attested state", w.Code, w.Body.String())
	}
	w := request("POST", target, body, grant)
	if v := view(w); w.Code != 200 || v["status"] != "succeeded" || v["eligible"] != false || v["attestation"] == nil {
		t.Fatal("attestation not recorded", w.Code, w.Body.String())
	}
	balance(900, 0)
	if w = request("POST", target, body, ""); w.Code != 409 {
		t.Fatal("a final refund was attested again", w.Code, w.Body.String())
	}
	if q := merchant174Invoke(t, f, "/api/pay/refundquery", compat.Params{"out_refund_no": "attest-success"}); q["code"] != float64(0) || q["status"] != float64(1) || q["refund_state"] != "succeeded" {
		t.Fatal("attested refund not reported final", q)
	}
	var source, operationState string
	var valid bool
	var audits int
	if e = pool.QueryRow(ctx, `SELECT e.source,p.state,v.valid,(SELECT count(*) FROM backoffice_audit WHERE resource='refunds' AND resource_id=e.refund_id AND operation='operator-attestation') FROM provider_refund_result_evidence e JOIN provider_operations p ON p.id=e.operation_id JOIN provider_refund_result_invariants v ON v.refund_id=e.refund_id WHERE e.refund_id=$1`, id).Scan(&source, &operationState, &valid, &audits); e != nil || source != "operator" || operationState != "succeeded" || !valid || audits != 1 {
		t.Fatal("operator evidence", source, operationState, valid, audits, e)
	}
	if _, e = pool.Exec(ctx, "UPDATE refund_operator_attestations SET note='changed' WHERE refund_id=$1", id); e == nil {
		t.Fatal("attestation is mutable")
	}

	// A refund attested as failed returns the reserved funds to the merchant.
	failed := merchant174Invoke(t, f, "/api/pay/refund", compat.Params{"out_trade_no": o.OutTradeNo, "out_refund_no": "attest-failed", "money": "2.00"})
	balance(700, 200)
	target = "/console/v1/admin/refunds/" + failed["refund_no"].(string) + "/attest-result"
	body = `{"state":"failed","provider_reference":"ALI-REFUND-20261106-2","note":"支付宝商家后台显示退款失败，原路未退"}`
	if w = request("POST", target, body, grantFor(target, body)); w.Code != 200 || view(w)["status"] != "failed" {
		t.Fatal("failed attestation", w.Code, w.Body.String())
	}
	balance(900, 0)

	// A reservation that was never sent to the provider can only have failed.
	unsent, e := s.Store.ReserveRefund(ctx, 2, o.OutTradeNo, "attest-unsent", 100)
	if e != nil {
		t.Fatal(e)
	}
	base = fmt.Sprintf("/console/v1/admin/refunds/%d", unsent.ID)
	if v := view(request("GET", base+"/attestation", "", "")); v["eligible"] != true || v["sent"] != false || len(v["states"].([]any)) != 1 || v["states"].([]any)[0] != "failed" {
		t.Fatal("unsent reservation eligibility", v)
	}
	body = `{"state":"succeeded","provider_reference":"none","note":"未发送到支付机构"}`
	if w = request("POST", base+"/attest-result", body, ""); w.Code != 409 {
		t.Fatal("an unsent refund was attested as succeeded", w.Code, w.Body.String())
	}
	body = `{"state":"failed","provider_reference":"none","note":"未发送到支付机构，撤销预留"}`
	if w = request("POST", base+"/attest-result", body, grantFor(base+"/attest-result", body)); w.Code != 200 {
		t.Fatal("unsent refund attestation", w.Code, w.Body.String())
	}
	if e = pool.QueryRow(ctx, "SELECT source FROM provider_refund_result_evidence WHERE refund_id=$1", unsent.ID).Scan(&source); e != nil || source != "internal" {
		t.Fatal("unsent refund evidence", source, e)
	}
	balance(900, 0)
	report, e := s.Store.CheckLedger(ctx)
	if e != nil || report.InvalidProviderRefundResults != 0 || report.InvalidJournals != 0 || report.InvalidAccounts != 0 {
		t.Fatal("reconciliation rejected operator evidence", report, e)
	}

	// A channel that queries refund outcomes is left to automatic reconciliation.
	s.ProviderFlow.Providers[7] = p
	queryable := merchant174Invoke(t, f, "/api/pay/refund", compat.Params{"out_trade_no": o.OutTradeNo, "out_refund_no": "attest-queryable", "money": "1.00"})
	base = "/console/v1/admin/refunds/" + queryable["refund_no"].(string)
	read = request("GET", base+"/attestation", "", "")
	if v := view(read); read.Code != 200 || v["eligible"] != false || v["reason"] != "automatic_query" {
		t.Fatal("queryable refund offered for manual closing", read.Body.String())
	}
	body = `{"state":"succeeded","provider_reference":"x","note":"y"}`
	if w = request("POST", base+"/attest-result", body, ""); w.Code != 409 {
		t.Fatal("queryable refund attested", w.Code, w.Body.String())
	}

	// Only the super administrator's own session may read or attest.
	operator := identity.User{Username: "operator197", Role: "admin", ID: 2, AuthVersion: 1, AdminClass: "operator"}
	if _, e = pool.Exec(ctx, "INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) VALUES('operator197','admin',2,'Operator','unused','operator')"); e != nil {
		t.Fatal(e)
	}
	opToken, _, e := s.Sessions.Create(operator)
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("GET", base+"/attestation", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: opToken})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	if rec.Code != 403 {
		t.Fatal("operator reached refund attestation", rec.Code, rec.Body.String())
	}
	r = httptest.NewRequest("GET", "/console/v1/merchant/refunds/"+id+"/attestation", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: f.token})
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	if rec.Code != 403 {
		t.Fatal("merchant reached refund attestation", rec.Code, rec.Body.String())
	}
}
