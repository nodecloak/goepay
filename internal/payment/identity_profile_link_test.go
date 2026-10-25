package payment

import (
	"context"
	"encoding/json"
	"errors"
	provider "goepay/internal/adapters/payment"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func link67Fixture(t *testing.T) (*Store, ProfileIdentityLookup, IdentityProfileBinding, string) {
	t.Helper()
	s := testStore(t)
	token := strings.Repeat("l", 43)
	lookupTestSession(t, s, token, "link67-owner", 967)
	b := profileBinding64(967, "mini")
	b.Provider = "wxpay"
	b.AppID = "wx-link67"
	q, e := s.CreateProfileIdentityLookup(context.Background(), token, "merchant", b)
	if e != nil {
		t.Fatal(e)
	}
	return s, q, b, token
}

func link67Result(q ProfileIdentityLookup) provider.WechatMiniLinkResult {
	return provider.WechatMiniLinkResult{Provider: q.Provider, AppID: q.AppID, RequestID: q.ID, URLLink: "https://wxaurl.cn/link67", RequestedExpiresAt: time.Unix(q.ExpiresAt.Unix(), 0).UTC()}
}

func TestIdentityProfileLink67CommittedSingleClaim(t *testing.T) {
	s, q, b, token := link67Fixture(t)
	ctx := context.Background()
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, send, e := s.BeginProfileIdentityLink(ctx, q.ID, token, "merchant", q.State, b)
			if e != nil {
				t.Error(e)
				return
			}
			if send {
				wins.Add(1)
				if v.Generation.State != q.State || v.Generation.RequestID != q.ID {
					t.Error("original state/request lost")
				}
				var birth time.Time
				if e = s.pool.QueryRow(ctx, `SELECT created_at FROM profile_identity_lookup_requests WHERE id=$1`, q.ID).Scan(&birth); e != nil || !v.Generation.CreatedAt.Equal(birth) || !v.Generation.Deadline.Equal(q.ExpiresAt) {
					t.Error("generation did not retain original lifetime", e)
				}
				var n int
				if e = s.pool.QueryRow(ctx, `SELECT count(*) FROM profile_identity_link_events WHERE request_id=$1 AND operation IN ('created','claimed')`, q.ID).Scan(&n); e != nil || n != 2 {
					t.Error("claim returned before durable audit", n, e)
				}
			} else if v.Generation.State != "" {
				t.Error("state revived")
			}
			raw, _ := json.Marshal(v)
			if strings.Contains(string(raw), q.State) {
				t.Error("state leaked")
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal("send winners", wins.Load())
	}
	if e := s.CompleteProfileIdentityLink(ctx, q.ID, token, "merchant", b, link67Result(q)); e != nil {
		t.Fatal(e)
	}
	v, e := s.ReadProfileIdentityLink(ctx, q.ID, token, "merchant", b)
	if e != nil || v.Status != "ready" || v.URLLink != "https://wxaurl.cn/link67" {
		t.Fatal(v, e)
	}
	var cipher []byte
	if e = s.pool.QueryRow(ctx, `SELECT link_cipher FROM profile_identity_links WHERE request_id=$1`, q.ID).Scan(&cipher); e != nil || strings.Contains(string(cipher), "wxaurl") {
		t.Fatal("plaintext persisted", e)
	}
	if _, send, e := s.BeginProfileIdentityLink(ctx, q.ID, token, "merchant", q.State, b); e != nil || send {
		t.Fatal("ready rearmed", send, e)
	}
}

func TestIdentityProfileLink67UnknownNeverRetrySDKIndependent(t *testing.T) {
	s, q, b, token := link67Fixture(t)
	ctx := context.Background()
	if _, send, e := s.BeginProfileIdentityLink(ctx, q.ID, token, "merchant", q.State, b); e != nil || !send {
		t.Fatal(send, e)
	}
	if e := s.FailProfileIdentityLink(ctx, q.ID, token, "merchant", b, &provider.WechatMiniLinkUnknownError{}); e != nil {
		t.Fatal(e)
	}
	v, send, e := s.BeginProfileIdentityLink(ctx, q.ID, token, "merchant", q.State, b)
	if e != nil || send || v.Status != "unknown" {
		t.Fatal(v, send, e)
	}
	if e = s.CompleteProfileIdentityLink(ctx, q.ID, token, "merchant", b, link67Result(q)); e == nil {
		t.Fatal("unknown completed")
	}
	if _, e = s.ConsumeProfileIdentityLookup(ctx, q.ID, q.State, "", "sdk-independent-code", b); e != nil {
		t.Fatal("SDK request was cancelled", e)
	}
	if _, e = s.ReadProfileIdentityLink(ctx, q.ID, token, "merchant", b); e == nil {
		t.Fatal("consumed parent allowed link reveal")
	}
	identity := provider.OAuthIdentity{Provider: b.Provider, AppID: b.AppID, SubjectID: "link67-independent-sdk-subject"}
	if e = s.CompleteProfileIdentityLookup(ctx, q.ID, identity, b); e != nil {
		t.Fatal("link failure prevented SDK completion", e)
	}
	if got, e := s.RevealProfileIdentityLookup(ctx, q.ID, token, "merchant", b); e != nil || got.SubjectID != identity.SubjectID {
		t.Fatal("link failure prevented SDK reveal", e)
	}
	other, e := s.CreateProfileIdentityLookup(ctx, token, "merchant", b)
	if e != nil {
		t.Fatal(e)
	}
	link67Begin(t, s, other, b, token)
	if e = s.FailProfileIdentityLink(ctx, other.ID, token, "merchant", b, errors.New("unclassified send outcome")); e != nil {
		t.Fatal(e)
	}
	if v, send, e := s.BeginProfileIdentityLink(ctx, other.ID, token, "merchant", other.State, b); e != nil || send || v.Status != "unknown" {
		t.Fatal("unclassified outcome was retryable", e)
	}
}

func TestIdentityProfileLink67AuditRollbackAndOwner(t *testing.T) {
	s, q, b, token := link67Fixture(t)
	ctx := context.Background()
	if _, e := s.pool.Exec(ctx, `CREATE FUNCTION reject_link67_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic audit fault'; END $$; CREATE TRIGGER reject_link67 BEFORE INSERT ON profile_identity_link_events FOR EACH ROW EXECUTE FUNCTION reject_link67_audit()`); e != nil {
		t.Fatal(e)
	}
	if _, send, e := s.BeginProfileIdentityLink(ctx, q.ID, token, "merchant", q.State, b); e == nil || send {
		t.Fatal("audit fault granted claim")
	}
	var n int
	s.pool.QueryRow(ctx, `SELECT count(*) FROM profile_identity_links`).Scan(&n)
	if n != 0 {
		t.Fatal("rolled back intent persisted")
	}
	if _, e := s.pool.Exec(ctx, `DROP TRIGGER reject_link67 ON profile_identity_link_events`); e != nil {
		t.Fatal(e)
	}
	bad := b
	bad.Fingerprint = strings.Repeat("b", 64)
	for _, x := range []struct {
		token, state string
		b            IdentityProfileBinding
	}{{strings.Repeat("x", 43), q.State, b}, {token, strings.Repeat("a", 64), b}, {token, q.State, bad}} {
		if _, send, e := s.BeginProfileIdentityLink(ctx, q.ID, x.token, "merchant", x.state, x.b); e == nil || send {
			t.Fatal("invalid source granted claim")
		}
	}
	if _, send, e := s.BeginProfileIdentityLink(ctx, q.ID, token, "merchant", q.State, b); e != nil || !send {
		t.Fatal("known rollback could not retry", send, e)
	}
	if e := s.FailProfileIdentityLink(ctx, q.ID, token, "merchant", b, provider.ErrWechatMiniLinkRejected); e != nil {
		t.Fatal(e)
	}
	v, send, e := s.BeginProfileIdentityLink(ctx, q.ID, token, "merchant", q.State, b)
	if e != nil || send || v.Status != "failed" {
		t.Fatal(v, send, e)
	}
	if e = s.FailProfileIdentityLink(ctx, q.ID, token, "merchant", b, errors.New("unexpected transport outcome")); e == nil {
		t.Fatal("failed state changed")
	}
}
