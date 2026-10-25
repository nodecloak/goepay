package payment

import (
	"context"
	"crypto/sha256"
	"errors"
	provider "goepay/internal/adapters/payment"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func lookupTestSession(t *testing.T, s *Store, token, username string, merchant int64) {
	t.Helper()
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: merchant, Name: username, APIKey: "synthetic-lookup-secret"}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES($1,'merchant',$2,$1,'synthetic-hash')`, username, merchant); e != nil {
		t.Fatal(e)
	}
	hash := sha256.Sum256([]byte(token))
	if _, e := s.pool.Exec(ctx, `INSERT INTO console_sessions(token_hash,username,role,subject_id,csrf,expires_at,auth_version) VALUES($1,$2,'merchant',$3,'synthetic-csrf',clock_timestamp()+interval '1 hour',1)`, hash[:], username, merchant); e != nil {
		t.Fatal(e)
	}
}

func TestIdentityLookupFirstBrowserConcurrentAndPaymentCodeCannotCrossPurpose(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	token := strings.Repeat("p", 43)
	lookupTestSession(t, s, token, "browser-race", 904)
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 904, OutTradeNo: "payment-first", Name: "test", Type: "alipay", Amount: 1})
	if e != nil {
		t.Fatal(e)
	}
	b := oauthTestBinding(t, s, o)
	browser := strings.Repeat("f", 64)
	state, e := s.IssueOAuthState(ctx, b, browser)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ConsumeOAuthState(ctx, state, browser, "payment-first-code", b); e != nil {
		t.Fatal(e)
	}
	q, e := s.CreateIdentityLookup(ctx, token, "merchant", IdentityLookupBinding{MerchantID: 904, ChannelID: 7, Provider: "alipay", AppID: "app", Scope: "auth_base", AccountScope: "trusted-account"})
	if e != nil {
		t.Fatal(e)
	}
	public := q.AuthorizationToken
	var wins atomic.Int32
	var wg sync.WaitGroup
	var winnerBrowser, winnerState string
	var mu sync.Mutex
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			candidate := strings.Repeat(string("01234567"[i]), 64)
			_, st, err := s.StartIdentityLookup(ctx, public, candidate)
			if err == nil {
				wins.Add(1)
				mu.Lock()
				winnerBrowser, winnerState = candidate, st
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal("multiple first browsers", wins.Load())
	}
	if !s.IdentityLookupAccountMatches(q, "trusted-account") || s.IdentityLookupAccountMatches(q, "changed-account") {
		t.Fatal("public account scope is not bound")
	}
	if _, e = s.ConsumeIdentityLookup(ctx, q.ID, "alipay", 7, winnerState, winnerBrowser, "payment-first-code"); e == nil {
		t.Fatal("payment code crossed to lookup")
	}
	got, e := s.ReadIdentityLookup(ctx, q.ID, token, "merchant")
	if e != nil || got.Status != "failed" {
		t.Fatal("collision did not leave terminal failure", got.Status, e)
	}
}

func TestIdentityLookupRevealDeadlineRecheckedAfterAuditWait(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	token := strings.Repeat("t", 43)
	lookupTestSession(t, s, token, "deadline", 905)
	q, e := s.CreateIdentityLookup(ctx, token, "merchant", IdentityLookupBinding{MerchantID: 905, ChannelID: 7, Provider: "wxpay", AppID: "app", Scope: "snsapi_base", AccountScope: "account"})
	if e != nil {
		t.Fatal(e)
	}
	// A short-lived fixture establishes the edge without mutating immutable policy.
	shortID := strings.Repeat("9", 64)
	cipherNonce := make([]byte, s.aead.NonceSize())
	short := q
	short.ID = shortID
	cipher := s.aead.Seal(cipherNonce, cipherNonce, []byte("private-openid"), short.aad())
	if _, e = s.pool.Exec(ctx, `INSERT INTO identity_lookup_requests(id,authorization_hash,session_hash,actor_username,actor_role,actor_id,actor_auth_version,merchant_id,channel_id,provider,app_id,scope,account_scope_hash,status,subject_cipher,created_at,expires_at,completed_at) SELECT $1,$2,session_hash,actor_username,actor_role,actor_id,actor_auth_version,merchant_id,channel_id,provider,app_id,scope,account_scope_hash,'verified',$3,clock_timestamp(),clock_timestamp()+interval '400 milliseconds',clock_timestamp() FROM identity_lookup_requests WHERE id=$4`, shortID, s.oauthHash("identity-authorization", "short-fixture"), cipher, q.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `CREATE FUNCTION delay_identity_reveal() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.operation='revealed' THEN PERFORM pg_sleep(0.6); END IF; RETURN NEW; END $$; CREATE TRIGGER delay_reveal BEFORE INSERT ON identity_lookup_events FOR EACH ROW EXECUTE FUNCTION delay_identity_reveal()`); e != nil {
		t.Fatal(e)
	}
	started := time.Now()
	if _, e = s.RevealIdentityLookup(ctx, shortID, token, "merchant"); e == nil {
		t.Fatal("expired identity was returned after blocking audit")
	}
	if time.Since(started) < 500*time.Millisecond {
		t.Fatal("test did not exercise audit wait")
	}
	var status string
	if e = s.pool.QueryRow(ctx, `SELECT status FROM identity_lookup_requests WHERE id=$1`, shortID).Scan(&status); e != nil || status != "verified" {
		t.Fatal("expired reveal did not rollback", status, e)
	}
}

func TestIdentityLookupIndependentBrowserBindingEncryptedOneTimeReveal(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	token := strings.Repeat("a", 43)
	lookupTestSession(t, s, token, "lookup-user", 901)
	b := IdentityLookupBinding{MerchantID: 901, ChannelID: 7, Provider: "wxpay", AppID: "wx-app", Scope: "snsapi_base", AccountScope: "synthetic-wx-account"}
	q, e := s.CreateIdentityLookup(ctx, token, "merchant", b)
	if e != nil {
		t.Fatal(e)
	}
	browser := strings.Repeat("b", 64)
	authorizationToken := q.AuthorizationToken
	q, state, e := s.StartIdentityLookup(ctx, authorizationToken, browser)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.StartIdentityLookup(ctx, authorizationToken, strings.Repeat("c", 64)); e == nil {
		t.Fatal("public token rebound browser")
	}
	if _, e = s.ConsumeIdentityLookup(ctx, q.ID, "wxpay", 7, state, strings.Repeat("c", 64), "one-code"); e == nil {
		t.Fatal("foreign browser consumed state")
	}
	if _, e = s.ConsumeIdentityLookup(ctx, q.ID, "wxpay", 7, state, browser, "one-code"); e != nil {
		t.Fatal(e)
	}
	id := provider.OAuthIdentity{Provider: "wxpay", AppID: "wx-app", SubjectID: "synthetic-private-openid"}
	if e = s.CompleteIdentityLookup(ctx, q.ID, id); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReadIdentityLookup(ctx, q.ID, strings.Repeat("x", 43), "merchant"); e == nil {
		t.Fatal("foreign console session read result")
	}
	var raw string
	if e = s.pool.QueryRow(ctx, `SELECT to_jsonb(q)::text FROM identity_lookup_requests q WHERE id=$1`, q.ID).Scan(&raw); e != nil || strings.Contains(raw, id.SubjectID) || strings.Contains(raw, token) || strings.Contains(raw, "one-code") {
		t.Fatal("plaintext persisted", e)
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := s.RevealIdentityLookup(ctx, q.ID, token, "merchant")
			if err == nil {
				if got != id {
					t.Error("wrong identity")
				}
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatal("reveal was not once", successes.Load())
	}
	for _, table := range []string{"orders", "provider_attempts", "journals"} {
		var n int
		if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); e != nil || n != 0 {
			t.Fatal("lookup touched payment", table, n, e)
		}
	}
}

func TestIdentityLookupRevocationAndCrossPurposeCodeClaim(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	token := strings.Repeat("d", 43)
	lookupTestSession(t, s, token, "lookup-revoke", 902)
	b := IdentityLookupBinding{MerchantID: 902, ChannelID: 7, Provider: "alipay", AppID: "app", Scope: "auth_base", AccountScope: "synthetic-alipay-account"}
	browser := strings.Repeat("e", 64)
	first, e := s.CreateIdentityLookup(ctx, token, "merchant", b)
	if e != nil {
		t.Fatal(e)
	}
	first, state, e := s.StartIdentityLookup(ctx, first.AuthorizationToken, browser)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ConsumeIdentityLookup(ctx, first.ID, "alipay", 7, state, browser, "shared-web-code"); e != nil {
		t.Fatal(e)
	}
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 902, OutTradeNo: "payment-cross-purpose", Name: "synthetic", Type: "alipay", Amount: 1})
	if e != nil {
		t.Fatal(e)
	}
	pay := oauthTestBinding(t, s, o)
	payState, e := s.IssueOAuthState(ctx, pay, browser)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ConsumeOAuthState(ctx, payState, browser, "shared-web-code", pay); !errors.Is(e, ErrOAuthState) {
		t.Fatal("lookup code reused for payment", e)
	}
	if e = s.ConsumeOAuthState(ctx, payState, browser, "fresh-web-code", pay); e == nil {
		t.Fatal("collision did not burn payment state")
	}
	if _, e = s.pool.Exec(ctx, `UPDATE console_users SET auth_version=auth_version+1 WHERE username='lookup-revoke'`); e != nil {
		t.Fatal(e)
	}
	if e = s.CompleteIdentityLookup(ctx, first.ID, provider.OAuthIdentity{Provider: "alipay", AppID: "app", SubjectID: "private-id"}); e == nil {
		t.Fatal("revoked session accepted slow exchange result")
	}
	if _, e = s.ReadIdentityLookup(ctx, first.ID, token, "merchant"); e == nil {
		t.Fatal("revoked session read metadata")
	}
}
