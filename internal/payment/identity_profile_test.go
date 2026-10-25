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

func profileBinding64(mid int64, kind string) IdentityProfileBinding {
	path := ""
	if kind == "mini" {
		path = "pages/identity/index"
	}
	return IdentityProfileBinding{MerchantID: mid, ChannelID: 7, ProfileID: 11, ProfileRevision: 1, Provider: "alipay", AppID: "identity-app", Kind: kind, Path: path, Fingerprint: strings.Repeat("a", 64)}
}
func TestIdentityProfile64MiniOwnerBoundSingleExchangeAndReveal(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	token := strings.Repeat("n", 43)
	lookupTestSession(t, s, token, "profile-mini64", 965)
	b := profileBinding64(965, "mini")
	q, e := s.CreateProfileIdentityLookup(ctx, token, "merchant", b)
	if e != nil || len(q.State) != 64 || q.AuthorizationToken != "" {
		t.Fatal(q, e)
	}
	raw, _ := json.Marshal(q)
	if strings.Contains(string(raw), q.State) || strings.Contains(string(raw), b.Fingerprint) {
		t.Fatal("private state/fingerprint serialized")
	}
	if _, e = s.ReadProfileIdentityLookup(ctx, q.ID, strings.Repeat("x", 43), "merchant", b); e == nil {
		t.Fatal("different session read")
	}
	bad := b
	bad.Fingerprint = strings.Repeat("b", 64)
	if _, e = s.ConsumeProfileIdentityLookup(ctx, q.ID, q.State, "", "local-code", bad); e == nil {
		t.Fatal("changed profile consumed state")
	}
	if _, e = s.ConsumeProfileIdentityLookup(ctx, q.ID, q.State, "", "local-code", b); e == nil {
		t.Fatal("restoring old profile revived invalidated request")
	}
	q, e = s.CreateProfileIdentityLookup(ctx, token, "merchant", b)
	if e != nil {
		t.Fatal(e)
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.ConsumeProfileIdentityLookup(ctx, q.ID, q.State, "", "local-code", b); e == nil {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal("exchange claims", wins.Load())
	}
	identity := provider.OAuthIdentity{Provider: b.Provider, AppID: b.AppID, SubjectID: "local-private-identity"}
	if e = s.CompleteProfileIdentityLookup(ctx, q.ID, identity, b); e != nil {
		t.Fatal(e)
	}
	wins.Store(0)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got, e := s.RevealProfileIdentityLookup(ctx, q.ID, token, "merchant", b); e == nil && got.SubjectID == identity.SubjectID {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal("reveal winners", wins.Load())
	}
	for _, table := range []string{"orders", "provider_attempts", "ledger_entries", "jobs"} {
		var n int
		if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); e != nil || n != 0 {
			t.Fatal("financial side effect", table, n, e)
		}
	}
}

func TestIdentityProfile64DeadlineAfterAuditCannotAuthorizeExchange(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	token := strings.Repeat("s", 43)
	lookupTestSession(t, s, token, "profile-deadline64", 966)
	if _, e := s.pool.Exec(ctx, `UPDATE console_sessions SET expires_at=clock_timestamp()+interval '2 seconds' WHERE token_hash=$1`, lookupSessionHash(token)); e != nil {
		t.Fatal(e)
	}
	b := profileBinding64(966, "mini")
	q, e := s.CreateProfileIdentityLookup(ctx, token, "merchant", b)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `CREATE FUNCTION delay_identity64_processing() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.operation='processing' THEN PERFORM pg_sleep(2.2);END IF;RETURN NEW;END$$;CREATE TRIGGER delay_identity64_processing BEFORE INSERT ON profile_identity_lookup_events FOR EACH ROW EXECUTE FUNCTION delay_identity64_processing()`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ConsumeProfileIdentityLookup(ctx, q.ID, q.State, "", "slow-code", b); e == nil {
		t.Fatal("expired state authorized external exchange after audit wait")
	}
}

func TestIdentityProfile64LegacyAndProfileShareSessionLimit(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	token := strings.Repeat("l", 43)
	lookupTestSession(t, s, token, "profile-limit64", 967)
	b := profileBinding64(967, "mini")
	for i := 0; i < 5; i++ {
		if _, e := s.CreateProfileIdentityLookup(ctx, token, "merchant", b); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := s.CreateIdentityLookup(ctx, token, "merchant", IdentityLookupBinding{MerchantID: 967, ChannelID: 7, Provider: "alipay", AppID: "app", Scope: "auth_base", AccountScope: "account"}); e == nil {
		t.Fatal("legacy lookup bypassed shared five-request session cap")
	}
}

func TestIdentityProfile64CipherCannotMoveBetweenRequests(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	token := strings.Repeat("c", 43)
	lookupTestSession(t, s, token, "profile-cipher64", 968)
	if _, e := s.pool.Exec(ctx, `UPDATE console_sessions SET expires_at=clock_timestamp()+interval '2 minutes' WHERE token_hash=$1`, lookupSessionHash(token)); e != nil {
		t.Fatal(e)
	}
	b := profileBinding64(968, "mini")
	var qs []ProfileIdentityLookup
	for _, code := range []string{"cipher-code-one", "cipher-code-two"} {
		q, e := s.CreateProfileIdentityLookup(ctx, token, "merchant", b)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.ConsumeProfileIdentityLookup(ctx, q.ID, q.State, "", code, b); e != nil {
			t.Fatal(e)
		}
		if e = s.CompleteProfileIdentityLookup(ctx, q.ID, provider.OAuthIdentity{Provider: b.Provider, AppID: b.AppID, SubjectID: code}, b); e != nil {
			t.Fatal(e)
		}
		qs = append(qs, q)
	}
	if !qs[0].ExpiresAt.Equal(qs[1].ExpiresAt) {
		t.Fatal("test must exercise identical expiry")
	}
	// A damaged/offline restore can bypass SQL triggers. Reads must independently
	// reject ciphertext from a different, otherwise identical, trusted request.
	if _, e := s.pool.Exec(ctx, `ALTER TABLE profile_identity_lookup_requests DISABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `UPDATE profile_identity_lookup_requests SET subject_cipher=(SELECT subject_cipher FROM profile_identity_lookup_requests WHERE id=$1) WHERE id=$2`, qs[0].ID, qs[1].ID); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `ALTER TABLE profile_identity_lookup_requests ENABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.RevealProfileIdentityLookup(ctx, qs[1].ID, token, "merchant", b); e == nil {
		t.Fatal("different request ciphertext was accepted")
	}
	if _, e := s.RevealProfileIdentityLookup(ctx, qs[0].ID, token, "merchant", b); e != nil {
		t.Fatal("original ciphertext rejected", e)
	}
}

func TestIdentityProfile64CurrentProfileAndSessionRevocation(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	token := strings.Repeat("v", 43)
	lookupTestSession(t, s, token, "profile-revoke64", 969)
	b := profileBinding64(969, "mini")
	verified := func(code string) ProfileIdentityLookup {
		q, e := s.CreateProfileIdentityLookup(ctx, token, "merchant", b)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.ConsumeProfileIdentityLookup(ctx, q.ID, q.State, "", code, b); e != nil {
			t.Fatal(e)
		}
		if e = s.CompleteProfileIdentityLookup(ctx, q.ID, provider.OAuthIdentity{Provider: b.Provider, AppID: b.AppID, SubjectID: "synthetic-subject"}, b); e != nil {
			t.Fatal(e)
		}
		return q
	}
	q := verified("revoke-code-one")
	changed := b
	changed.ProfileRevision++
	if _, e := s.ReadProfileIdentityLookup(ctx, q.ID, token, "merchant", changed); e == nil {
		t.Fatal("changed revision read result")
	}
	if _, e := s.RevealProfileIdentityLookup(ctx, q.ID, token, "merchant", b); e == nil {
		t.Fatal("old revision restored invalidated result")
	}
	q = verified("revoke-code-two")
	if _, e := s.pool.Exec(ctx, `UPDATE console_users SET auth_version=auth_version+1 WHERE username='profile-revoke64'`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ReadProfileIdentityLookup(ctx, q.ID, token, "merchant", b); e == nil {
		t.Fatal("revoked session read result")
	}
	if _, e := s.RevealProfileIdentityLookup(ctx, q.ID, token, "merchant", b); e == nil {
		t.Fatal("revoked session revealed result")
	}
}

func TestIdentityProfile64WebFirstBrowserAndTamperProof(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	token := strings.Repeat("w", 43)
	lookupTestSession(t, s, token, "profile-web64", 970)
	b := profileBinding64(970, "web")
	q, e := s.CreateProfileIdentityLookup(ctx, token, "merchant", b)
	if e != nil {
		t.Fatal(e)
	}
	browser := strings.Repeat("1", 64)
	_, state, e := s.StartProfileIdentityLookup(ctx, q.AuthorizationToken, browser, b)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.StartProfileIdentityLookup(ctx, q.AuthorizationToken, strings.Repeat("2", 64), b); e == nil {
		t.Fatal("authorization rebound to another browser")
	}
	if _, e = s.ConsumeProfileIdentityLookup(ctx, q.ID, state, strings.Repeat("2", 64), "web-code", b); e == nil {
		t.Fatal("wrong browser consumed state")
	}
	if _, e = s.ConsumeProfileIdentityLookup(ctx, q.ID, state, browser, "web-code", b); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE profile_identity_lookup_requests SET app_id='changed-app' WHERE id=$1`, q.ID); e == nil {
		t.Fatal("SQL guard allowed binding mutation")
	}
	if _, e = s.pool.Exec(ctx, `ALTER TABLE profile_identity_lookup_requests DISABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE profile_identity_lookup_requests SET source_proof=decode(repeat('00',32),'hex') WHERE id=$1`, q.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `ALTER TABLE profile_identity_lookup_requests ENABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ProfileIdentityLookupMetadata(ctx, q.ID); e == nil {
		t.Fatal("fake source HMAC accepted by metadata")
	}
	if _, e = s.ReadProfileIdentityLookup(ctx, q.ID, token, "merchant", b); e == nil {
		t.Fatal("fake source HMAC accepted by owner read")
	}
	if e = s.CompleteProfileIdentityLookup(ctx, q.ID, provider.OAuthIdentity{Provider: b.Provider, AppID: b.AppID, SubjectID: "forged"}, b); e == nil {
		t.Fatal("fake source HMAC completed")
	}
}
