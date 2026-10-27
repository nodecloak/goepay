package server

import (
	"errors"
	"goepay/internal/enrollment"
	"goepay/internal/identity"
	"strings"
	"sync"
	"testing"
)

func TestEmail197ChangingClaimInvalidatesOldRecoveryAndPreservesPaymentCredential(t *testing.T) {
	f := email197Local(t)
	mid, username := f.legacy(t)
	before, e := f.s.Store.Merchant(t.Context(), mid)
	if e != nil {
		t.Fatal(e)
	}
	old, e := f.service.Issue(t.Context(), enrollment.VerificationRequest{Purpose: "recover", Email: "old197@example.test", Source: "192.0.2.196"})
	if e != nil {
		t.Fatal(e)
	}
	f.mu.Lock()
	oldCode := f.messages[len(f.messages)-1].code
	f.mu.Unlock()
	if _, e = f.p.Exec(t.Context(), `UPDATE enrollment_challenges SET created_at=created_at-interval '2 minutes'`); e != nil {
		t.Fatal(e)
	}
	id := strings.Repeat("x", 43)
	f.start(t, id)
	v := email197Data(t, f.request("POST", "/"+id+"/old-code", map[string]any{}))
	f.mu.Lock()
	code := f.messages[len(f.messages)-1].code
	f.mu.Unlock()
	if e = f.service.Recover(t.Context(), enrollment.RecoverInput{ChallengeID: v["old_challenge_id"].(string), Code: code, Email: "old197@example.test", Source: "192.0.2.197", Password: "forbidden-contact-recovery197"}); !errors.Is(e, enrollment.ErrCode) {
		t.Fatal("unused contact proof used for recovery", e)
	}
	email197Data(t, f.request("POST", "/"+id+"/old-verify", map[string]any{"code": code}))
	email197Data(t, f.request("POST", "/"+id+"/new-code", map[string]any{"email": "new-recovery197@example.test"}))
	f.mu.Lock()
	code = f.messages[len(f.messages)-1].code
	f.mu.Unlock()
	email197Data(t, f.request("POST", "/"+id+"/complete", map[string]any{"code": code}))
	if e = f.service.Recover(t.Context(), enrollment.RecoverInput{ChallengeID: old.ID, Code: oldCode, Email: "old197@example.test", Source: "192.0.2.196", Password: "forbidden-old-recovery197"}); !errors.Is(e, enrollment.ErrCode) {
		t.Fatal("old recovery proof survived binding change", e)
	}
	var hash string
	if e = f.p.QueryRow(t.Context(), `SELECT password_hash FROM console_users WHERE username=$1 AND role='merchant'`, username).Scan(&hash); e != nil || !identity.CheckPassword(hash, "local-email197-password") {
		t.Fatal("binding changed the login password", e)
	}
	if _, e = f.p.Exec(t.Context(), `UPDATE enrollment_challenges SET created_at=created_at-interval '2 minutes'`); e != nil {
		t.Fatal(e)
	}
	next, e := f.service.Issue(t.Context(), enrollment.VerificationRequest{Purpose: "recover", Email: "new-recovery197@example.test", Source: "192.0.2.198"})
	if e != nil {
		t.Fatal(e)
	}
	f.mu.Lock()
	code = f.messages[len(f.messages)-1].code
	f.mu.Unlock()
	if e = f.service.Recover(t.Context(), enrollment.RecoverInput{ChallengeID: next.ID, Code: code, Email: "new-recovery197@example.test", Source: "192.0.2.198", Password: "new-verified-recovery197"}); e != nil {
		t.Fatal("new verified recovery identity unusable", e)
	}
	if e = f.p.QueryRow(t.Context(), `SELECT password_hash FROM console_users WHERE username=$1 AND role='merchant'`, username).Scan(&hash); e != nil || !identity.CheckPassword(hash, "new-verified-recovery197") {
		t.Fatal("new address did not authorize recovery", e)
	}
	after, e := f.s.Store.Merchant(t.Context(), mid)
	if e != nil || after.APIKey != before.APIKey {
		t.Fatal("email lifecycle changed payment credentials", e)
	}
}
func TestEmail197ConcurrentCompletionHasOneClaimAndOneImmutableOutcome(t *testing.T) {
	f := email197Local(t)
	id := strings.Repeat("y", 43)
	code := email197Target(t, f, id)
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for range 2 {
		wg.Go(func() { results <- f.request("POST", "/"+id+"/complete", map[string]any{"code": code}).Code })
	}
	wg.Wait()
	close(results)
	success := 0
	for status := range results {
		if status == 200 {
			success++
		} else if status != 401 && status != 403 && status != 409 {
			t.Fatal("unexpected concurrent completion status", status)
		}
	}
	var claims, outcomes int
	if e := f.p.QueryRow(t.Context(), `SELECT(SELECT count(*)FROM enrollment_emails),(SELECT count(*)FROM email_binding_events197 WHERE kind='complete')`).Scan(&claims, &outcomes); e != nil || success != 1 || claims != 1 || outcomes != 1 {
		t.Fatal("concurrent proof used more than once", success, claims, outcomes, e)
	}
}
func TestEmail197AdministratorDelegationAndMissingCSRFCannotBindRecoveryIdentity(t *testing.T) {
	f := email197Local(t)
	csrf := f.csrf
	f.csrf = ""
	if w := f.request("POST", "", map[string]any{}); w.Code != 403 {
		t.Fatal("missing CSRF accepted", w.Code)
	}
	f.csrf = csrf
	tx, e := f.p.Begin(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(t.Context())
	token, csrf, _, e := f.s.Sessions.CreateDelegated(t.Context(), tx, identity.User{ID: 1, Role: "merchant", Username: "email197", AuthVersion: 1}, identity.User{ID: 168, Role: "admin", Username: "admin168", AdminClass: "super", AuthVersion: 1}, f.channel168Fixture.token)
	if e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(t.Context()); e != nil {
		t.Fatal(e)
	}
	f.token, f.csrf = token, csrf
	if w := f.request("GET", "", nil); w.Code != 403 {
		t.Fatal("administrator delegation read protected contact source", w.Code)
	}
	var claims int
	if e = f.p.QueryRow(t.Context(), `SELECT count(*)FROM enrollment_emails`).Scan(&claims); e != nil || claims != 0 {
		t.Fatal("rejected binding established recovery identity", claims, e)
	}
}
func TestEmail197OccupiedTargetDoesNotRevokeSessionOrStealOtherRecoveryClaim(t *testing.T) {
	f := email197Local(t)
	originalToken, originalCSRF := f.token, f.csrf
	other, _ := f.legacy(t)
	f.token, f.csrf = originalToken, originalCSRF
	id := strings.Repeat("z", 43)
	f.start(t, id)
	email197Data(t, f.request("POST", "/"+id+"/new-code", map[string]any{"email": "old197@example.test"}))
	f.mu.Lock()
	code := f.messages[len(f.messages)-1].code
	f.mu.Unlock()
	if w := f.request("POST", "/"+id+"/complete", map[string]any{"code": code}); w.Code != 409 {
		t.Fatal("occupied recovery identity taken", w.Code)
	}
	var owner int64
	var ownClaims, outcomes int
	if e := f.p.QueryRow(t.Context(), `SELECT merchant_id FROM enrollment_emails WHERE email='old197@example.test'`).Scan(&owner); e != nil || owner != other {
		t.Fatal("other merchant recovery authority changed", owner, e)
	}
	if e := f.p.QueryRow(t.Context(), `SELECT(SELECT count(*)FROM enrollment_emails WHERE merchant_id=1),(SELECT count(*)FROM email_binding_events197 WHERE kind='complete')`).Scan(&ownClaims, &outcomes); e != nil || ownClaims != 0 || outcomes != 0 {
		t.Fatal("conflict left partial binding evidence", ownClaims, outcomes, e)
	}
	email197Data(t, f.request("GET", "", nil))
}
