package payment

import (
	"context"
	provider "goepay/internal/adapters/payment"
	"strings"
	"testing"
)

func TestIdentityProfile64AdminTargetMerchantDisableEnableNeverRevivesOriginal(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	const mid int64 = 973
	if e := s.CreateMerchant(ctx, Merchant{ID: mid, Name: "target-merchant64", APIKey: "synthetic-merchant-key"}); e != nil {
		t.Fatal(e)
	}
	token := strings.Repeat("d", 43)
	// This is an actual durable super-admin session with a distinct subject,
	// not a merchant actor relabelled by the test or a caller-supplied User.
	if _, e := s.pool.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class)VALUES('profile-admin64','admin',88001,'profile admin','synthetic-hash','super');`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `INSERT INTO console_sessions(token_hash,username,role,subject_id,csrf,expires_at,auth_version)VALUES($1,'profile-admin64','admin',88001,'synthetic-csrf',clock_timestamp()+interval '1 hour',1)`, lookupSessionHash(token)); e != nil {
		t.Fatal(e)
	}
	mini := profileBinding64(mid, "mini")
	web := profileBinding64(mid, "web")
	create := func(b IdentityProfileBinding) ProfileIdentityLookup {
		q, e := s.CreateProfileIdentityLookup(ctx, token, "admin", b)
		if e != nil {
			t.Fatal(e)
		}
		return q
	}
	pending, verified, processing, notStarted := create(mini), create(mini), create(mini), create(web)
	if _, e := s.ConsumeProfileIdentityLookup(ctx, verified.ID, verified.State, "", "verified-before-toggle", mini); e != nil {
		t.Fatal(e)
	}
	identity := provider.OAuthIdentity{Provider: mini.Provider, AppID: mini.AppID, SubjectID: "synthetic-admin-target-subject"}
	if e := s.CompleteProfileIdentityLookup(ctx, verified.ID, identity, mini); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ConsumeProfileIdentityLookup(ctx, processing.ID, processing.State, "", "processing-before-toggle", mini); e != nil {
		t.Fatal(e)
	}
	original, e := s.Merchant(ctx, mid)
	if e != nil {
		t.Fatal(e)
	}
	for _, enabled := range []bool{false, true} {
		m, e := s.Merchant(ctx, mid)
		if e != nil {
			t.Fatal(e)
		}
		m.Enabled = enabled
		if e = s.ReplaceMerchantAtomic(ctx, m, []byte(`{}`), nil); e != nil {
			t.Fatal(e)
		}
	}
	current, e := s.Merchant(ctx, mid)
	if e != nil || !current.Enabled || current.ConfigRevision != original.ConfigRevision+2 {
		t.Fatal("normal merchant revision transition missing", e)
	}
	var adminVersion int64
	if e = s.pool.QueryRow(ctx, `SELECT auth_version FROM console_users WHERE username='profile-admin64'`).Scan(&adminVersion); e != nil || adminVersion != 1 {
		t.Fatal("test must leave original admin epoch unchanged", e)
	}
	if _, e = s.ConsumeProfileIdentityLookup(ctx, pending.ID, pending.State, "", "after-target-reenabled", mini); e == nil {
		t.Error("pending admin request revived after target merchant disable/enable")
	}
	if _, e = s.RevealProfileIdentityLookup(ctx, verified.ID, token, "admin", mini); e == nil {
		t.Error("verified admin result revealed after target merchant disable/enable")
	}
	if e = s.CompleteProfileIdentityLookup(ctx, processing.ID, identity, mini); e == nil {
		t.Error("processing admin request completed after target merchant disable/enable")
	}
	if _, _, e = s.StartProfileIdentityLookup(ctx, notStarted.AuthorizationToken, strings.Repeat("e", 64), web); e == nil {
		t.Error("old admin web authorization started after target merchant disable/enable")
	}
	if _, e = s.ReadProfileIdentityLookup(ctx, pending.ID, token, "admin", mini); e == nil {
		t.Error("old admin request remained readable after target merchant revision change")
	}
	fresh, e := s.CreateProfileIdentityLookup(ctx, token, "admin", mini)
	if e != nil {
		t.Fatal("fresh request under current merchant config should work", e)
	}
	if _, e = s.ConsumeProfileIdentityLookup(ctx, fresh.ID, fresh.State, "", "fresh-target-current-revision", mini); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE profile_identity_lookup_requests SET merchant_revision=merchant_revision+1 WHERE id=$1`, fresh.ID); e == nil {
		t.Fatal("SQL guard allowed immutable merchant revision change")
	}
	// A damaged restore may bypass the SQL guard. Rewriting an old binding to
	// the current revision must still fail its independently verified HMAC.
	if _, e = s.pool.Exec(ctx, `ALTER TABLE profile_identity_lookup_requests DISABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE profile_identity_lookup_requests SET merchant_revision=$2 WHERE id=$1`, pending.ID, current.ConfigRevision); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `ALTER TABLE profile_identity_lookup_requests ENABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReadProfileIdentityLookup(ctx, pending.ID, token, "admin", mini); e == nil {
		t.Fatal("rewritten merchant revision passed source HMAC verification")
	}
}
