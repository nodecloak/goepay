package payment

import (
	"context"
	"encoding/json"
	provider "goepay/internal/adapters/payment"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func appAuthFixture66(t *testing.T) (*Store, string, ApplicationAuthorizationBinding) {
	t.Helper()
	s := testStore(t)
	ctx := context.Background()
	token := strings.Repeat("A", 43)
	if e := s.CreateMerchant(ctx, Merchant{ID: 1066, Name: "synthetic-target66", APIKey: "synthetic-key66"}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class)VALUES('app-auth-super66','admin',88066,'synthetic super','synthetic-hash','super')`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `INSERT INTO console_sessions(token_hash,username,role,subject_id,csrf,expires_at,auth_version)VALUES($1,'app-auth-super66','admin',88066,'synthetic-csrf',clock_timestamp()+interval '1 hour',1)`, lookupSessionHash(token)); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class)VALUES('app-auth-backup66','admin',88067,'synthetic backup','synthetic-hash','super')`); e != nil {
		t.Fatal(e)
	}
	return s, token, ApplicationAuthorizationBinding{MerchantID: 1066, ProfileID: 66, ProfileRevision: 1, IssuerAppID: "2021000000000066", ExpectedAuthAppID: "2021000000001066", LaunchKind: "basic", Fingerprint: strings.Repeat("a", 64)}
}
func appAuthStart66(t *testing.T, s *Store, token string, b ApplicationAuthorizationBinding) (ApplicationAuthorizationRequest, string, string) {
	t.Helper()
	ctx := context.Background()
	q, e := s.CreateApplicationAuthorization(ctx, token, "admin", b)
	if e != nil {
		t.Fatal(e)
	}
	browser := strings.Repeat("b", 64)
	_, state, e := s.StartApplicationAuthorization(ctx, q.AuthorizationToken, browser, b)
	if e != nil {
		t.Fatal(e)
	}
	return q, state, browser
}
func appAuthResult66(b ApplicationAuthorizationBinding) provider.ApplicationAuthorizationResult {
	return provider.ApplicationAuthorizationResult{Provider: "alipay", IssuerAppID: b.IssuerAppID, AuthAppID: b.ExpectedAuthAppID, UserID: "2088000000000066", AppAuthToken: "synthetic-private-app-token66", TokenExpiresInSeconds: 3600}
}
func TestApplicationAuthorization66SingleExchangeAndReveal(t *testing.T) {
	s, token, b := appAuthFixture66(t)
	ctx := context.Background()
	q, state, browser := appAuthStart66(t, s, token, b)
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.ConsumeApplicationAuthorization(ctx, q.ID, state, browser, "synthetic-app-code66", b); e == nil {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal("code wins", wins.Load())
	}
	result := appAuthResult66(b)
	if e := s.CompleteApplicationAuthorization(ctx, q.ID, result, b); e != nil {
		t.Fatal(e)
	}
	metadata, e := s.ReadApplicationAuthorization(ctx, q.ID, token, "admin", b)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(metadata)
	if strings.Contains(string(raw), result.AppAuthToken) || strings.Contains(string(raw), q.AuthorizationToken) || strings.Contains(string(raw), b.Fingerprint) {
		t.Fatal("metadata leaked secret")
	}
	wins.Store(0)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, e := s.RevealApplicationAuthorization(ctx, q.ID, token, "admin", b)
			if e == nil && out.AppAuthToken == result.AppAuthToken {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal("reveal wins", wins.Load())
	}
	for _, table := range []string{"orders", "provider_attempts", "verified_payment_receipts", "ledger_entries", "jobs"} {
		var n int
		if e := s.pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); e != nil || n != 0 {
			t.Fatal("financial effect", table, n, e)
		}
	}
}

func TestApplicationAuthorization66SuperOnlyAndPurposeIsolation(t *testing.T) {
	s, token, b := appAuthFixture66(t)
	ctx := context.Background()
	merchantToken := strings.Repeat("m", 43)
	lookupTestSession(t, s, merchantToken, "app-auth-merchant66", 1067)
	if _, e := s.CreateApplicationAuthorization(ctx, merchantToken, "admin", b); e == nil {
		t.Fatal("merchant relabelled admin")
	}
	if _, e := s.CreateApplicationAuthorization(ctx, token, "merchant", b); e == nil {
		t.Fatal("caller role accepted")
	}
	for _, class := range []string{"operator", "super"} {
		if _, e := s.pool.Exec(ctx, `UPDATE console_users SET admin_class=$1 WHERE username='app-auth-super66'`, class); e != nil {
			t.Fatal(e)
		}
		if class == "operator" {
			if _, e := s.CreateApplicationAuthorization(ctx, token, "admin", b); e == nil {
				t.Fatal("operator accepted")
			}
		}
	}
	// Changing admin class advances durable auth_version; issue a fresh authentic
	// session after the policy transition, never rewrite the old epoch.
	if _, e := s.pool.Exec(ctx, `UPDATE console_sessions SET auth_version=(SELECT auth_version FROM console_users WHERE username='app-auth-super66') WHERE token_hash=$1`, lookupSessionHash(token)); e != nil {
		t.Fatal(e)
	}
	q, state, browser := appAuthStart66(t, s, token, b)
	if _, e := s.ReadApplicationAuthorization(ctx, q.ID, merchantToken, "admin", b); e == nil {
		t.Fatal("other session read")
	}
	if _, _, e := s.StartProfileIdentityLookup(ctx, q.AuthorizationToken, browser, profileBinding64(b.MerchantID, "web")); e == nil {
		t.Fatal("authorization crossed to identity purpose")
	}
	if _, e := s.ConsumeApplicationAuthorization(ctx, q.ID, q.AuthorizationToken, browser, "app-code", b); e == nil {
		t.Fatal("authorization used as state")
	}
	if _, e := s.ConsumeApplicationAuthorization(ctx, q.ID, state, strings.Repeat("c", 64), "app-code", b); e == nil {
		t.Fatal("wrong browser")
	}
	if _, e := s.ConsumeApplicationAuthorization(ctx, q.ID, state, browser, "app-code", b); e != nil {
		t.Fatal(e)
	}
	bad := appAuthResult66(b)
	bad.AuthAppID = "2021000000009999"
	if e := s.CompleteApplicationAuthorization(ctx, q.ID, bad, b); e == nil {
		t.Fatal("wrong result app")
	}
	if e := s.CompleteApplicationAuthorization(ctx, q.ID, appAuthResult66(b), b); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `UPDATE console_users SET enabled=false WHERE username='app-auth-super66'`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.RevealApplicationAuthorization(ctx, q.ID, token, "admin", b); e == nil {
		t.Fatal("revoked actor revealed")
	}
}
func TestApplicationAuthorization66CodeUniqueAcrossRequestsAndIssuers(t *testing.T) {
	s, token, b := appAuthFixture66(t)
	ctx := context.Background()
	q1, st1, br1 := appAuthStart66(t, s, token, b)
	q2, st2, br2 := appAuthStart66(t, s, token, b)
	var wins atomic.Int32
	var wg sync.WaitGroup
	for _, item := range []struct {
		q              ApplicationAuthorizationRequest
		state, browser string
	}{{q1, st1, br1}, {q2, st2, br2}} {
		item := item
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.ConsumeApplicationAuthorization(ctx, item.q.ID, item.state, item.browser, "shared-app-code66", b); e == nil {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal("same issuer code reused", wins.Load())
	}
	for _, q := range []ApplicationAuthorizationRequest{q1, q2} {
		if _, e := s.ConsumeApplicationAuthorization(ctx, q.ID, st1, br1, "different-code66", b); e == nil {
			t.Fatal("processing or burned request reused")
		}
	}
	// App authorization is a different exchange protocol from ordinary user OAuth.
	// A synthetic ordinary claim with the same text does not consume this realm.
	q3, st3, br3 := appAuthStart66(t, s, token, b)
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	ok, e := s.claimOAuthAuthorizationCode(ctx, tx, "alipay", b.IssuerAppID, "web", "separate-realm66", []byte(strings.Repeat("s", 32)))
	if e != nil || !ok {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ConsumeApplicationAuthorization(ctx, q3.ID, st3, br3, "separate-realm66", b); e != nil {
		t.Fatal("ordinary user realm blocked app realm", e)
	}
	b2 := b
	b2.IssuerAppID = "2021000000000067"
	q4, st4, br4 := appAuthStart66(t, s, token, b2)
	if _, e = s.ConsumeApplicationAuthorization(ctx, q4.ID, st4, br4, "shared-app-code66", b2); e != nil {
		t.Fatal("different issuer namespace blocked", e)
	}
	var claims int
	if e = s.pool.QueryRow(ctx, `SELECT count(*) FROM application_authorization_code_claims`).Scan(&claims); e != nil || claims != 3 {
		t.Fatal(claims, e)
	}
}
func TestApplicationAuthorization66RuntimeBindingInvalidatesPermanently(t *testing.T) {
	for _, kind := range []string{"fingerprint", "profile", "revision", "issuer", "auth-app", "launch", "types", "merchant"} {
		t.Run(kind, func(t *testing.T) {
			s, token, b := appAuthFixture66(t)
			ctx := context.Background()
			q, state, browser := appAuthStart66(t, s, token, b)
			bad := b
			switch kind {
			case "fingerprint":
				bad.Fingerprint = strings.Repeat("b", 64)
			case "profile":
				bad.ProfileID++
			case "revision":
				bad.ProfileRevision++
			case "issuer":
				bad.IssuerAppID = "2021000000000099"
			case "auth-app":
				bad.ExpectedAuthAppID = "2021000000000098"
			case "launch":
				bad.LaunchKind = "assigned"
				bad.AppTypes = []provider.AlipayAuthorizationAppType{provider.AlipayAuthorizationWebApp}
			case "types":
				bad.AppTypes = []provider.AlipayAuthorizationAppType{provider.AlipayAuthorizationWebApp}
			case "merchant":
				bad.MerchantID++
			}
			if s.MatchesApplicationAuthorization(q, bad) {
				t.Fatal("changed binding matched")
			}
			if _, e := s.ConsumeApplicationAuthorization(ctx, q.ID, state, browser, "changed-binding-code", bad); e == nil {
				t.Fatal("changed binding consumed")
			}
			if _, e := s.ConsumeApplicationAuthorization(ctx, q.ID, state, browser, "restored-binding-code", b); e == nil {
				t.Fatal("restoring config revived failed request")
			}
		})
	}
}
func TestApplicationAuthorization66MerchantRevisionAndSourceTamper(t *testing.T) {
	s, token, b := appAuthFixture66(t)
	ctx := context.Background()
	pending, state, browser := appAuthStart66(t, s, token, b)
	verified, st2, br2 := appAuthStart66(t, s, token, b)
	processing, st3, br3 := appAuthStart66(t, s, token, b)
	if _, e := s.ConsumeApplicationAuthorization(ctx, verified.ID, st2, br2, "verified66", b); e != nil {
		t.Fatal(e)
	}
	if e := s.CompleteApplicationAuthorization(ctx, verified.ID, appAuthResult66(b), b); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ConsumeApplicationAuthorization(ctx, processing.ID, st3, br3, "processing66", b); e != nil {
		t.Fatal(e)
	}
	for _, enabled := range []bool{false, true} {
		m, e := s.Merchant(ctx, b.MerchantID)
		if e != nil {
			t.Fatal(e)
		}
		m.Enabled = enabled
		if e = s.ReplaceMerchantAtomic(ctx, m, []byte(`{}`), nil); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := s.ConsumeApplicationAuthorization(ctx, pending.ID, state, browser, "after-reenabled66", b); e == nil {
		t.Fatal("pending revived")
	}
	if e := s.CompleteApplicationAuthorization(ctx, processing.ID, appAuthResult66(b), b); e == nil {
		t.Fatal("processing revived")
	}
	if _, e := s.RevealApplicationAuthorization(ctx, verified.ID, token, "admin", b); e == nil {
		t.Fatal("verified revived")
	}
	if _, e := s.pool.Exec(ctx, `UPDATE application_authorization_requests SET merchant_revision=merchant_revision+2 WHERE id=$1`, pending.ID); e == nil {
		t.Fatal("immutable source changed")
	}
	if _, e := s.pool.Exec(ctx, `ALTER TABLE application_authorization_requests DISABLE TRIGGER USER;UPDATE application_authorization_requests SET merchant_revision=(SELECT config_revision FROM merchants WHERE id=1066);ALTER TABLE application_authorization_requests ENABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ReadApplicationAuthorization(ctx, pending.ID, token, "admin", b); e == nil {
		t.Fatal("offline revision rewrite bypassed HMAC")
	}
	fresh, _, _ := appAuthStart66(t, s, token, b)
	if _, e := s.ReadApplicationAuthorization(ctx, fresh.ID, token, "admin", b); e != nil {
		t.Fatal("fresh revision rejected", e)
	}
}
func TestApplicationAuthorization66AuditRollbackAndCipherTamper(t *testing.T) {
	s, token, b := appAuthFixture66(t)
	ctx := context.Background()
	q, state, browser := appAuthStart66(t, s, token, b)
	if _, e := s.pool.Exec(ctx, `CREATE FUNCTION fail_app_authorization_audit66() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.operation='processing' THEN RAISE EXCEPTION 'synthetic audit fault';END IF;RETURN NEW;END$$;CREATE TRIGGER fail_app_authorization_audit66 BEFORE INSERT ON application_authorization_events FOR EACH ROW EXECUTE FUNCTION fail_app_authorization_audit66()`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ConsumeApplicationAuthorization(ctx, q.ID, state, browser, "audit-rollback66", b); e == nil {
		t.Fatal("claim escaped failed audit")
	}
	var n int
	if e := s.pool.QueryRow(ctx, `SELECT count(*) FROM application_authorization_code_claims`).Scan(&n); e != nil || n != 0 {
		t.Fatal("claim committed on audit failure", n, e)
	}
	if _, e := s.pool.Exec(ctx, `DROP TRIGGER fail_app_authorization_audit66 ON application_authorization_events`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ConsumeApplicationAuthorization(ctx, q.ID, state, browser, "audit-rollback66", b); e != nil {
		t.Fatal("known rollback unusable", e)
	}
	if e := s.CompleteApplicationAuthorization(ctx, q.ID, appAuthResult66(b), b); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `ALTER TABLE application_authorization_requests DISABLE TRIGGER USER;UPDATE application_authorization_requests SET result_cipher=decode(repeat('00',48),'hex');ALTER TABLE application_authorization_requests ENABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	if out, e := s.RevealApplicationAuthorization(ctx, q.ID, token, "admin", b); e == nil || out.AppAuthToken != "" {
		t.Fatal("tamper revealed")
	}
}
func TestApplicationAuthorization66LimitAndAssignedTypes(t *testing.T) {
	s, token, b := appAuthFixture66(t)
	ctx := context.Background()
	bad := b
	bad.LaunchKind = "assigned"
	bad.AppTypes = []provider.AlipayAuthorizationAppType{"UNKNOWN"}
	if _, e := s.CreateApplicationAuthorization(ctx, token, "admin", bad); e == nil {
		t.Fatal("unknown assigned type")
	}
	bad.AppTypes = []provider.AlipayAuthorizationAppType{provider.AlipayAuthorizationWebApp, provider.AlipayAuthorizationWebApp}
	if _, e := s.CreateApplicationAuthorization(ctx, token, "admin", bad); e == nil {
		t.Fatal("duplicate assigned type")
	}
	b.LaunchKind = "assigned"
	b.AppTypes = []provider.AlipayAuthorizationAppType{provider.AlipayAuthorizationWebApp, provider.AlipayAuthorizationTinyApp}
	for i := 0; i < 5; i++ {
		q, e := s.CreateApplicationAuthorization(ctx, token, "admin", b)
		if e != nil {
			t.Fatal(e)
		}
		if !s.MatchesApplicationAuthorization(q, b) {
			t.Fatal("original assigned binding mismatch")
		}
	}
	if _, e := s.CreateApplicationAuthorization(ctx, token, "admin", b); e == nil {
		t.Fatal("sixth active request allowed")
	}
}

func TestApplicationAuthorization66DeadlineAndAuditFailure(t *testing.T) {
	s, token, b := appAuthFixture66(t)
	ctx := context.Background()
	if _, e := s.pool.Exec(ctx, `UPDATE console_sessions SET expires_at=clock_timestamp()+interval '2 seconds' WHERE token_hash=$1`, lookupSessionHash(token)); e != nil {
		t.Fatal(e)
	}
	q, state, browser := appAuthStart66(t, s, token, b)
	if _, e := s.pool.Exec(ctx, `CREATE FUNCTION delay_app_authorization_audit66() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.operation='processing' THEN PERFORM pg_sleep(2.2);END IF;RETURN NEW;END$$;CREATE TRIGGER delay_app_authorization_audit66 BEFORE INSERT ON application_authorization_events FOR EACH ROW EXECUTE FUNCTION delay_app_authorization_audit66()`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ConsumeApplicationAuthorization(ctx, q.ID, state, browser, "deadline-code66", b); e == nil {
		t.Fatal("expired authorization escaped delayed audit")
	}
	if _, e := s.ReadApplicationAuthorization(ctx, q.ID, token, "admin", b); e == nil {
		t.Fatal("expired request readable")
	}
}
func TestApplicationAuthorization66RevealCommitBeforeReturn(t *testing.T) {
	s, token, b := appAuthFixture66(t)
	ctx := context.Background()
	q, state, browser := appAuthStart66(t, s, token, b)
	if _, e := s.ConsumeApplicationAuthorization(ctx, q.ID, state, browser, "reveal-rollback66", b); e != nil {
		t.Fatal(e)
	}
	if e := s.CompleteApplicationAuthorization(ctx, q.ID, appAuthResult66(b), b); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `CREATE FUNCTION fail_app_reveal_audit66() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.operation='revealed' THEN RAISE EXCEPTION 'synthetic reveal fault';END IF;RETURN NEW;END$$;CREATE TRIGGER fail_app_reveal_audit66 BEFORE INSERT ON application_authorization_events FOR EACH ROW EXECUTE FUNCTION fail_app_reveal_audit66()`); e != nil {
		t.Fatal(e)
	}
	if out, e := s.RevealApplicationAuthorization(ctx, q.ID, token, "admin", b); e == nil || out.AppAuthToken != "" {
		t.Fatal("uncommitted reveal returned token")
	}
	if _, e := s.pool.Exec(ctx, `DROP TRIGGER fail_app_reveal_audit66 ON application_authorization_events`); e != nil {
		t.Fatal(e)
	}
	if out, e := s.RevealApplicationAuthorization(ctx, q.ID, token, "admin", b); e != nil || out.AppAuthToken != appAuthResult66(b).AppAuthToken {
		t.Fatal("known rollback not recoverable", e)
	}
}
func TestApplicationAuthorization66ForgedProcessingAndClaimTamper(t *testing.T) {
	s, token, b := appAuthFixture66(t)
	ctx := context.Background()
	q, _, _ := appAuthStart66(t, s, token, b)
	if _, e := s.pool.Exec(ctx, `ALTER TABLE application_authorization_requests DISABLE TRIGGER USER;UPDATE application_authorization_requests SET status='processing';ALTER TABLE application_authorization_requests ENABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	if e := s.CompleteApplicationAuthorization(ctx, q.ID, appAuthResult66(b), b); e == nil {
		t.Fatal("processing status without original app claim accepted")
	}
}
func TestApplicationAuthorization66DelegatedAndDeletedSession(t *testing.T) {
	s, token, b := appAuthFixture66(t)
	ctx := context.Background()
	q, _, _ := appAuthStart66(t, s, token, b)
	if _, e := s.pool.Exec(ctx, `INSERT INTO console_session_issuers(token_hash,issuer_session_hash,issuer_username,issuer_id,issuer_auth_version)VALUES($1,$2,'synthetic-issuer',771,1)`, lookupSessionHash(token), lookupSessionHash(strings.Repeat("d", 43))); e != nil {
		t.Fatal(e)
	}
	if _, e := s.CreateApplicationAuthorization(ctx, token, "admin", b); e == nil {
		t.Fatal("delegated super created")
	}
	if _, e := s.ReadApplicationAuthorization(ctx, q.ID, token, "admin", b); e == nil {
		t.Fatal("delegated session read")
	}
	if _, e := s.pool.Exec(ctx, `DELETE FROM console_sessions WHERE token_hash=$1`, lookupSessionHash(token)); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ApplicationAuthorizationMetadata(ctx, q.ID); e == nil {
		t.Fatal("deleted session metadata alive")
	}
}

func TestApplicationAuthorization66PrintableTokenAndCipherCannotMove(t *testing.T) {
	s, token, b := appAuthFixture66(t)
	ctx := context.Background()
	var qs []ApplicationAuthorizationRequest
	for _, code := range []string{"cipher-one66", "cipher-two66"} {
		q, state, browser := appAuthStart66(t, s, token, b)
		if _, e := s.ConsumeApplicationAuthorization(ctx, q.ID, state, browser, code, b); e != nil {
			t.Fatal(e)
		}
		result := appAuthResult66(b)
		result.AppAuthToken = "synthetic-private+/=token66"
		if e := s.CompleteApplicationAuthorization(ctx, q.ID, result, b); e != nil {
			t.Fatal("adapter-accepted printable token rejected", e)
		}
		qs = append(qs, q)
	}
	if _, e := s.pool.Exec(ctx, `ALTER TABLE application_authorization_requests DISABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `UPDATE application_authorization_requests SET result_cipher=(SELECT result_cipher FROM application_authorization_requests WHERE id=$1) WHERE id=$2`, qs[0].ID, qs[1].ID); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `ALTER TABLE application_authorization_requests ENABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	if out, e := s.RevealApplicationAuthorization(ctx, qs[1].ID, token, "admin", b); e == nil || out.AppAuthToken != "" {
		t.Fatal("cipher moved between requests")
	}
	if out, e := s.RevealApplicationAuthorization(ctx, qs[0].ID, token, "admin", b); e != nil || out.AppAuthToken != "synthetic-private+/=token66" {
		t.Fatal("original valid cipher rejected", e)
	}
}
func TestApplicationAuthorization66ClaimProofIndependentOfSQLGuard(t *testing.T) {
	s, token, b := appAuthFixture66(t)
	ctx := context.Background()
	q, state, browser := appAuthStart66(t, s, token, b)
	if _, e := s.ConsumeApplicationAuthorization(ctx, q.ID, state, browser, "claim-tamper66", b); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `UPDATE application_authorization_code_claims SET claim_proof=decode(repeat('00',32),'hex')`); e == nil {
		t.Fatal("immutable code claim changed")
	}
	if _, e := s.pool.Exec(ctx, `ALTER TABLE application_authorization_code_claims DISABLE TRIGGER USER;UPDATE application_authorization_code_claims SET claim_proof=decode(repeat('00',32),'hex');ALTER TABLE application_authorization_code_claims ENABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	if e := s.CompleteApplicationAuthorization(ctx, q.ID, appAuthResult66(b), b); e == nil {
		t.Fatal("offline claim proof corruption accepted")
	}
	if _, e := s.ReadApplicationAuthorization(ctx, q.ID, token, "admin", b); e == nil {
		t.Fatal("corrupt source claim readable")
	}
}

func TestApplicationAuthorization66RejectsWrongTypedResultAndExpiry(t *testing.T) {
	s, token, b := appAuthFixture66(t)
	ctx := context.Background()
	q, state, browser := appAuthStart66(t, s, token, b)
	if _, e := s.ConsumeApplicationAuthorization(ctx, q.ID, state, browser, "typed-result66", b); e != nil {
		t.Fatal(e)
	}
	for _, kind := range []string{"provider", "issuer", "app", "user", "token", "lifetime"} {
		bad := appAuthResult66(b)
		switch kind {
		case "provider":
			bad.Provider = "wxpay"
		case "issuer":
			bad.IssuerAppID = "2021000000009999"
		case "app":
			bad.AuthAppID = "2021000000009998"
		case "user":
			bad.UserID = "not-a-merchant-uid"
		case "token":
			bad.AppAuthToken = "secret\ncontrol"
		case "lifetime":
			bad.TokenExpiresInSeconds = -1
		}
		if e := s.CompleteApplicationAuthorization(ctx, q.ID, bad, b); e == nil {
			t.Fatal("bad typed result", kind)
		}
	}
	if e := s.CompleteApplicationAuthorization(ctx, q.ID, appAuthResult66(b), b); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `UPDATE console_sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE token_hash=$1`, lookupSessionHash(token)); e != nil {
		t.Fatal(e)
	}
	if out, e := s.RevealApplicationAuthorization(ctx, q.ID, token, "admin", b); e == nil || out.AppAuthToken != "" {
		t.Fatal("expired original session revealed")
	}
}
