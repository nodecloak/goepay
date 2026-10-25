package payment

import (
	"context"
	"strings"
	"testing"
	"time"
)

func link67Exec(t *testing.T, s *Store, sql string, args ...any) {
	t.Helper()
	if _, e := s.pool.Exec(context.Background(), sql, args...); e != nil {
		t.Fatal(e)
	}
}
func link67Begin(t *testing.T, s *Store, q ProfileIdentityLookup, b IdentityProfileBinding, token string) {
	t.Helper()
	if _, send, e := s.BeginProfileIdentityLink(context.Background(), q.ID, token, "merchant", q.State, b); e != nil || !send {
		t.Fatal(send, e)
	}
}

func TestIdentityProfileLink67StrictVendorGrammar(t *testing.T) {
	for _, u := range []string{"https://wxaurl.cn/abc", "https://wxmpurl.cn/abc?cq=hello%2Bworld"} {
		if !profileIdentityLinkURL(u) {
			t.Fatal("official link rejected", u)
		}
	}
	for _, u := range []string{"http://wxaurl.cn/abc", "https://@wxaurl.cn/abc", "https://user@wxaurl.cn/abc", "https://wxaurl.cn:443/abc", "https://wxaurl.cn/abc?", "https://wxaurl.cn/abc#", "https://wxaurl.cn/abc#x", "https://wxaurl.cn.evil/abc", "https://WXAURL.CN/abc", "https://wxaurl.cn/../abc", "https://wxaurl.cn/%2e%2e/abc", "https://wxaurl.cn/%00", "https://wxaurl.cn/abc?state=secret", "https://wxaurl.cn/abc?cq=x&cq=y", "https://wxaurl.cn/abc?cq=", "https://wxaurl.cn/ab\\c", "https://wxaurl.cn/ab\nc", "https://wxaurl.cn/" + string([]byte{0xff})} {
		if profileIdentityLinkURL(u) {
			t.Errorf("unsafe link accepted %q", u)
		}
	}
}

func TestIdentityProfileLink67ResultBindingAndCipherReplay(t *testing.T) {
	s, q, b, token := link67Fixture(t)
	ctx := context.Background()
	link67Begin(t, s, q, b, token)
	good := link67Result(q)
	for _, change := range []func(){func() { good.Provider = "alipay" }, func() { good.AppID = "other" }, func() { good.RequestID = strings.Repeat("a", 64) }, func() { good.RequestedExpiresAt = good.RequestedExpiresAt.Add(time.Second) }, func() { good.URLLink = "https://@wxaurl.cn/abc" }} {
		good = link67Result(q)
		change()
		if e := s.CompleteProfileIdentityLink(ctx, q.ID, token, "merchant", b, good); e == nil {
			t.Fatal("wrong result accepted")
		}
	}
	if e := s.CompleteProfileIdentityLink(ctx, q.ID, token, "merchant", b, link67Result(q)); e != nil {
		t.Fatal(e)
	}
	other, e := s.CreateProfileIdentityLookup(ctx, token, "merchant", b)
	if e != nil {
		t.Fatal(e)
	}
	link67Begin(t, s, other, b, token)
	if e = s.CompleteProfileIdentityLink(ctx, other.ID, token, "merchant", b, link67Result(other)); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE profile_identity_links SET status='creating',completed_at=NULL,link_cipher=NULL WHERE request_id=$1`, q.ID); e == nil {
		t.Fatal("SQL terminal rearm allowed")
	}
	if _, e = s.pool.Exec(ctx, `DELETE FROM profile_identity_links WHERE request_id=$1`, q.ID); e == nil {
		t.Fatal("SQL intent deletion allowed")
	}
	if _, e = s.pool.Exec(ctx, `DELETE FROM profile_identity_link_events WHERE request_id=$1`, q.ID); e == nil {
		t.Fatal("SQL audit deletion allowed")
	}
	// Damaged restore bypasses SQL guards, but state HMAC and AEAD remain independent.
	link67Exec(t, s, `ALTER TABLE profile_identity_links DISABLE TRIGGER USER`)
	link67Exec(t, s, `UPDATE profile_identity_links SET link_cipher=(SELECT link_cipher FROM profile_identity_links WHERE request_id=$2) WHERE request_id=$1`, q.ID, other.ID)
	link67Exec(t, s, `ALTER TABLE profile_identity_links ENABLE TRIGGER USER`)
	if _, e = s.ReadProfileIdentityLink(ctx, q.ID, token, "merchant", b); e == nil {
		t.Fatal("cross-request cipher accepted")
	}
	// Even an independently signed row cannot authenticate another request's cipher.
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	var r profileLinkRecord
	r.RequestID = q.ID
	e = tx.QueryRow(ctx, `SELECT source_proof,link_cipher,status,completed_at FROM profile_identity_links WHERE request_id=$1`, q.ID).Scan(&r.sourceProof, &r.cipher, &r.Status, &r.completed)
	tx.Rollback(ctx)
	if e != nil {
		t.Fatal(e)
	}
	link67Exec(t, s, `ALTER TABLE profile_identity_links DISABLE TRIGGER USER`)
	link67Exec(t, s, `UPDATE profile_identity_links SET state_proof=$2 WHERE request_id=$1`, q.ID, s.profileLinkState(r))
	link67Exec(t, s, `ALTER TABLE profile_identity_links ENABLE TRIGGER USER`)
	if _, e = s.ReadProfileIdentityLink(ctx, q.ID, token, "merchant", b); e == nil {
		t.Fatal("cross-request AEAD accepted")
	}
	link67Exec(t, s, `ALTER TABLE profile_identity_links DISABLE TRIGGER USER`)
	link67Exec(t, s, `UPDATE profile_identity_links SET source_proof=decode(repeat('00',32),'hex') WHERE request_id=$1`, other.ID)
	link67Exec(t, s, `ALTER TABLE profile_identity_links ENABLE TRIGGER USER`)
	if _, e = s.ReadProfileIdentityLink(ctx, other.ID, token, "merchant", b); e == nil {
		t.Fatal("damaged link source HMAC accepted")
	}
}

func TestIdentityProfileLink67ParentAndSourceTamper(t *testing.T) {
	s, q, b, token := link67Fixture(t)
	ctx := context.Background()
	link67Begin(t, s, q, b, token)
	// Parent creation is read from the original DB row and sealed in link source.
	link67Exec(t, s, `ALTER TABLE profile_identity_lookup_requests DISABLE TRIGGER USER`)
	link67Exec(t, s, `UPDATE profile_identity_lookup_requests SET created_at=created_at+interval '1 microsecond' WHERE id=$1`, q.ID)
	link67Exec(t, s, `ALTER TABLE profile_identity_lookup_requests ENABLE TRIGGER USER`)
	if _, e := s.ReadProfileIdentityLink(ctx, q.ID, token, "merchant", b); e == nil {
		t.Fatal("changed original birth accepted")
	}
	link67Exec(t, s, `ALTER TABLE profile_identity_lookup_requests DISABLE TRIGGER USER`)
	link67Exec(t, s, `UPDATE profile_identity_lookup_requests SET created_at=created_at-interval '1 microsecond',source_proof=decode(repeat('00',32),'hex') WHERE id=$1`, q.ID)
	link67Exec(t, s, `ALTER TABLE profile_identity_lookup_requests ENABLE TRIGGER USER`)
	if _, _, e := s.BeginProfileIdentityLink(ctx, q.ID, token, "merchant", q.State, b); e == nil {
		t.Fatal("damaged parent HMAC accepted")
	}
}

func TestIdentityProfileLink67TooShortLifetime(t *testing.T) {
	s, q, b, token := link67Fixture(t)
	ctx := context.Background()
	// A correctly sealed short-lived original request is not eligible for URLLink.
	q.ExpiresAt = time.Now().UTC().Add(65 * time.Second).Truncate(time.Microsecond)
	q.sourceProof = s.profileSource(q)
	q.launchProof = s.profileLaunch(q)
	link67Exec(t, s, `ALTER TABLE profile_identity_lookup_requests DISABLE TRIGGER USER`)
	link67Exec(t, s, `UPDATE profile_identity_lookup_requests SET expires_at=$2,source_proof=$3,launch_proof=$4 WHERE id=$1`, q.ID, q.ExpiresAt, q.sourceProof, q.launchProof)
	link67Exec(t, s, `ALTER TABLE profile_identity_lookup_requests ENABLE TRIGGER USER`)
	if _, send, e := s.BeginProfileIdentityLink(ctx, q.ID, token, "merchant", q.State, b); e == nil || send {
		t.Fatal("short lifetime claimed")
	}
	var n int
	if e := s.pool.QueryRow(ctx, `SELECT count(*) FROM profile_identity_links`).Scan(&n); e != nil || n != 0 {
		t.Fatal(n, e)
	}
	// The URLLink policy must not shorten the independent SDK request lifetime.
	if _, e := s.ConsumeProfileIdentityLookup(ctx, q.ID, q.State, "", "short-independent-code", b); e != nil {
		t.Fatal(e)
	}
}

func TestIdentityProfileLink67RevocationAndBinding(t *testing.T) {
	s, q, b, token := link67Fixture(t)
	ctx := context.Background()
	link67Begin(t, s, q, b, token)
	if e := s.CompleteProfileIdentityLink(ctx, q.ID, token, "merchant", b, link67Result(q)); e != nil {
		t.Fatal(e)
	}
	other := strings.Repeat("x", 43)
	link67Exec(t, s, `INSERT INTO console_sessions(token_hash,username,role,subject_id,csrf,expires_at,auth_version) SELECT $1,username,role,subject_id,'synthetic',expires_at,auth_version FROM console_sessions WHERE token_hash=$2`, lookupSessionHash(other), lookupSessionHash(token))
	bad := b
	bad.Fingerprint = strings.Repeat("f", 64)
	for _, x := range []struct {
		token, role string
		b           IdentityProfileBinding
	}{{other, "merchant", b}, {token, "admin", b}, {token, "merchant", bad}} {
		if _, e := s.ReadProfileIdentityLink(ctx, q.ID, x.token, x.role, x.b); e == nil {
			t.Fatal("different owner or binding revealed")
		}
	}
	// Delegated sessions are rejected even when their durable account is the owner.
	link67Exec(t, s, `INSERT INTO console_session_issuers(token_hash,issuer_session_hash,issuer_username,issuer_id,issuer_auth_version) VALUES($1,$1,'link67-owner',967,1)`, lookupSessionHash(token))
	if _, e := s.ReadProfileIdentityLink(ctx, q.ID, token, "merchant", b); e == nil {
		t.Fatal("delegation revealed")
	}
	link67Exec(t, s, `DELETE FROM console_session_issuers WHERE token_hash=$1`, lookupSessionHash(token))
	link67Exec(t, s, `DELETE FROM console_sessions WHERE token_hash=$1`, lookupSessionHash(token))
	if _, e := s.ReadProfileIdentityLink(ctx, q.ID, token, "merchant", b); e == nil {
		t.Fatal("revoked session revealed")
	}
}

func TestIdentityProfileLink67MerchantOffOn(t *testing.T) {
	s, q, b, token := link67Fixture(t)
	ctx := context.Background()
	link67Begin(t, s, q, b, token)
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
	if _, send, e := s.BeginProfileIdentityLink(ctx, q.ID, token, "merchant", q.State, b); e == nil || send {
		t.Fatal("merchant toggle revived claim")
	}
	if e := s.CompleteProfileIdentityLink(ctx, q.ID, token, "merchant", b, link67Result(q)); e == nil {
		t.Fatal("merchant toggle completed")
	}
	if _, e := s.ReadProfileIdentityLink(ctx, q.ID, token, "merchant", b); e == nil {
		t.Fatal("merchant toggle read")
	}
}

func TestIdentityProfileLink67OwnOperatorAndDowngrade(t *testing.T) {
	s, q, b, _ := link67Fixture(t)
	ctx := context.Background()
	token := strings.Repeat("o", 43)
	link67Exec(t, s, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) VALUES('link67-operator','admin',6701,'operator','synthetic','operator'),('link67-super','admin',6702,'super','synthetic','super'),('link67-spare-super','admin',6703,'spare','synthetic','super')`)
	for _, name := range []string{"link67-operator", "link67-super"} {
		token = strings.Repeat("o", 43)
		if name == "link67-super" {
			token = strings.Repeat("s", 43)
		}
		link67Exec(t, s, `INSERT INTO console_sessions(token_hash,username,role,subject_id,csrf,expires_at,auth_version) SELECT $1,username,role,subject_id,'synthetic',clock_timestamp()+interval '1 hour',auth_version FROM console_users WHERE username=$2`, lookupSessionHash(token), name)
		var e error
		q, e = s.CreateProfileIdentityLookup(ctx, token, "admin", b)
		if e != nil {
			t.Fatal("own original admin profile rejected", e)
		}
		if _, send, e := s.BeginProfileIdentityLink(ctx, q.ID, token, "admin", q.State, b); e != nil || !send {
			t.Fatal("original operator/super denied", e)
		}
		if e = s.CompleteProfileIdentityLink(ctx, q.ID, token, "admin", b, link67Result(q)); e != nil {
			t.Fatal(e)
		}
		if _, e = s.ReadProfileIdentityLink(ctx, q.ID, token, "admin", b); e != nil {
			t.Fatal(e)
		}
		if name == "link67-super" {
			link67Exec(t, s, `UPDATE console_users SET admin_class='operator' WHERE username=$1`, name)
			if _, e = s.ReadProfileIdentityLink(ctx, q.ID, token, "admin", b); e == nil {
				t.Fatal("downgraded actor revealed")
			}
			if _, send, e := s.BeginProfileIdentityLink(ctx, q.ID, token, "admin", q.State, b); e == nil || send {
				t.Fatal("downgraded actor rearmed")
			}
		}
	}
}

func TestIdentityProfileLink67WrongOriginalProviderKind(t *testing.T) {
	s, _, b, token := link67Fixture(t)
	ctx := context.Background()
	for _, change := range []func(*IdentityProfileBinding){func(x *IdentityProfileBinding) { x.Provider = "alipay" }, func(x *IdentityProfileBinding) { x.Kind = "web"; x.Path = "" }} {
		bad := b
		change(&bad)
		q, e := s.CreateProfileIdentityLookup(ctx, token, "merchant", bad)
		if e != nil {
			t.Fatal(e)
		}
		if _, send, e := s.BeginProfileIdentityLink(ctx, q.ID, token, "merchant", q.State, bad); e == nil || send {
			t.Fatal("non WeChat mini accepted")
		}
	}
	var n int
	if e := s.pool.QueryRow(ctx, `SELECT count(*) FROM profile_identity_links`).Scan(&n); e != nil || n != 0 {
		t.Fatal(n, e)
	}
}

func TestIdentityProfileLink67AuditWaitRechecksSendWindow(t *testing.T) {
	s, q, b, token := link67Fixture(t)
	ctx := context.Background()
	// Couple the delay to the actual admitted claim, rather than a fixed sleep
	// that can finish before the 70-second boundary on a loaded runner. Sequence
	// calls survive rollback and prove the rejection reached this exact audit.
	link67Exec(t, s, `CREATE SEQUENCE link67_audit_entered;
 CREATE FUNCTION delay_link67_audit() RETURNS trigger LANGUAGE plpgsql AS $$ DECLARE deadline timestamptz; BEGIN IF NEW.operation='claimed' THEN
 PERFORM nextval('link67_audit_entered'); SELECT expires_at INTO deadline FROM profile_identity_lookup_requests WHERE id=NEW.request_id;
 PERFORM pg_sleep(GREATEST(0,extract(epoch FROM deadline-clock_timestamp())-69)); END IF;RETURN NEW;END $$;
 CREATE TRIGGER delay_link67 BEFORE INSERT ON profile_identity_link_events FOR EACH ROW EXECUTE FUNCTION delay_link67_audit()`)
	if e := s.pool.QueryRow(ctx, `SELECT clock_timestamp()+interval '150 seconds'`).Scan(&q.ExpiresAt); e != nil {
		t.Fatal(e)
	}
	q.sourceProof = s.profileSource(q)
	q.launchProof = s.profileLaunch(q)
	link67Exec(t, s, `ALTER TABLE profile_identity_lookup_requests DISABLE TRIGGER USER`)
	link67Exec(t, s, `UPDATE profile_identity_lookup_requests SET expires_at=$2,source_proof=$3,launch_proof=$4 WHERE id=$1`, q.ID, q.ExpiresAt, q.sourceProof, q.launchProof)
	link67Exec(t, s, `ALTER TABLE profile_identity_lookup_requests ENABLE TRIGGER USER`)
	_, send, claimErr := s.BeginProfileIdentityLink(ctx, q.ID, token, "merchant", q.State, b)
	if claimErr == nil || send {
		t.Fatal("audit wait granted stale send window", claimErr)
	}
	var entered bool
	if e := s.pool.QueryRow(ctx, `SELECT is_called FROM link67_audit_entered`).Scan(&entered); e != nil || !entered {
		t.Fatal("fixture never reached claim audit", claimErr, e)
	}
	var n int
	if e := s.pool.QueryRow(ctx, `SELECT count(*) FROM profile_identity_links`).Scan(&n); e != nil || n != 0 {
		t.Fatal("stale audit did not rollback", n, e)
	}
}
