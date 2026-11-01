package payment

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
)

func reviewWeixinSpec(id int64) WeixinApplicationSpec {
	return WeixinApplicationSpec{ID: id, Revision: 1, Name: "Synthetic credential review", Kind: "official_account", AppID: "wxreview", AppSecret: "synthetic-review-secret"}
}

func TestWeixinApplicationReviewAEADAndVersionProjectionRejectTamperingAndMissingKeys(t *testing.T) {
	s, e := NewWithKey(nil, bytes.Repeat([]byte{1}, 32))
	if e != nil {
		t.Fatal(e)
	}
	p := reviewWeixinSpec(18601)
	v, e := s.sealWeixinApplication(p, time.Date(2042, 6, 1, 1, 2, 3, 0, time.UTC))
	if e != nil {
		t.Fatal(e)
	}
	if read, e := s.openWeixinApplication(v); e != nil || read.AppSecret != p.AppSecret || read.ID != p.ID {
		t.Fatal("valid private version did not round-trip", e)
	}
	for _, tc := range []struct {
		name   string
		change func(*weixinApplicationVersion)
		resign bool
	}{
		{"proof", func(v *weixinApplicationVersion) { v.proof[0] ^= 1 }, false},
		{"ciphertext", func(v *weixinApplicationVersion) { v.ciphertext[0] ^= 1 }, true},
		{"nonce", func(v *weixinApplicationVersion) { v.nonce[0] ^= 1 }, true},
		{"id-aad", func(v *weixinApplicationVersion) { v.id++ }, true},
		{"revision-aad", func(v *weixinApplicationVersion) { v.revision++ }, true},
		{"source-aad", func(v *weixinApplicationVersion) { v.source++ }, true},
		{"kind-aad", func(v *weixinApplicationVersion) { v.kind = "mini_program" }, true},
		{"enabled-projection", func(v *weixinApplicationVersion) { v.enabled = !v.enabled }, false},
		{"archive-projection", func(v *weixinApplicationVersion) { v.archived = !v.archived }, false},
		{"draft-projection", func(v *weixinApplicationVersion) { v.draft = !v.draft }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy := v
			copy.nonce = append([]byte(nil), v.nonce...)
			copy.ciphertext = append([]byte(nil), v.ciphertext...)
			copy.proof = append([]byte(nil), v.proof...)
			tc.change(&copy)
			if tc.resign {
				// Isolate AEAD/AAD from the independent outer MAC check.
				copy.proof, e = s.weixinApplicationVersionProof(copy)
				if e != nil {
					t.Fatal(e)
				}
			}
			if _, e := s.openWeixinApplication(copy); !errors.Is(e, ErrConflict) {
				t.Errorf("tampered private version was not rejected: %v", e)
			}
		})
	}
	withoutKey := &Store{}
	if _, e := withoutKey.openWeixinApplication(v); e == nil {
		t.Error("credential opened without its encryption/index keys")
	}
	if _, e := withoutKey.sealWeixinApplication(p, time.Now()); e == nil {
		t.Error("credential was accepted without encryption/index keys")
	}
}

func reviewWeixinActor(t *testing.T, s *Store) ParentChannelActor {
	t.Helper()
	user := identity.User{ID: 18601, Role: "admin", Username: "weixin-review186", AdminClass: "super", AuthVersion: 1}
	if _, e := s.pool.Exec(t.Context(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) VALUES($1,'admin',$2,'Synthetic review','unused-synthetic','super')`, user.Username, user.ID); e != nil {
		t.Fatal(e)
	}
	token, csrf, e := identity.NewPersistentSessions(s.pool, false).Create(user)
	if e != nil {
		t.Fatal(e)
	}
	return ParentChannelActor{User: user, Token: token, CSRF: csrf}
}

func reviewWeixinInput(request string) WeixinApplicationInput {
	return WeixinApplicationInput{RequestID: request, Name: "Synthetic review draft", Kind: "official_account", AppID: "wxreviewdraft", AppSecret: "synthetic-review-draft-secret"}
}

func TestWeixinApplicationReviewHeadAndReceiptTamperingCannotBecomeTrustedResults(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	actor := reviewWeixinActor(t, s)
	in := reviewWeixinInput(strings.Repeat("a", 64))
	view, e := s.MutateWeixinApplication(ctx, actor, 0, in, WeixinApplicationScope{}, false)
	if e != nil {
		t.Fatal(e)
	}
	var updated time.Time
	if e = s.pool.QueryRow(ctx, `SELECT updated_at FROM runtime_weixin_app_heads WHERE id=$1`, view.ID).Scan(&updated); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE runtime_weixin_app_heads SET updated_at=updated_at+interval '1 second' WHERE id=$1`, view.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.CurrentWeixinApplicationSpecs(ctx); !errors.Is(e, ErrConflict) {
		t.Errorf("unauthenticated current head accepted: %v", e)
	}
	if original, e := s.WeixinApplicationReceipt(ctx, actor, in.RequestID); e != nil || original.Revision != 1 || original.Fingerprint != view.Fingerprint {
		t.Errorf("current head damage erased the immutable original receipt: %+v %v", original, e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE runtime_weixin_app_heads SET updated_at=$2 WHERE id=$1`, view.ID, updated); e != nil {
		t.Fatal(e)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `ALTER TABLE runtime_weixin_app_requests DISABLE TRIGGER immutable_weixin_app_requests`); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `UPDATE runtime_weixin_app_requests SET request_hash=set_byte(request_hash,0,get_byte(request_hash,0)#1) WHERE request_id=$1`, in.RequestID); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `ALTER TABLE runtime_weixin_app_requests ENABLE TRIGGER immutable_weixin_app_requests`); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = s.WeixinApplicationReceipt(ctx, actor, in.RequestID); !errors.Is(e, ErrConflict) {
		t.Errorf("tampered receipt became a confirmed result: %v", e)
	}
	withoutKey := &Store{pool: s.pool}
	if _, e = withoutKey.CurrentWeixinApplicationSpecs(ctx); e == nil {
		t.Error("unkeyed current catalog exposed credential material")
	}
	if _, e = withoutKey.MutateWeixinApplication(ctx, actor, 0, reviewWeixinInput(strings.Repeat("b", 64)), WeixinApplicationScope{}, false); e == nil {
		t.Error("unkeyed store persisted a configuration")
	}
	var heads, versions, requests int
	if e = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM runtime_weixin_app_heads),(SELECT count(*) FROM runtime_weixin_app_versions),(SELECT count(*) FROM runtime_weixin_app_requests)`).Scan(&heads, &versions, &requests); e != nil || heads != 1 || versions != 1 || requests != 1 {
		t.Errorf("failed private reads/writes changed configuration: %d/%d/%d %v", heads, versions, requests, e)
	}
}

func reviewWeixinSeedDrafts(t *testing.T, s *Store, n int) {
	t.Helper()
	ctx := t.Context()
	var at time.Time
	if e := s.pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); e != nil {
		t.Fatal(e)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	batch := &pgx.Batch{}
	for id := int64(1); id <= int64(n); id++ {
		p := reviewWeixinSpec(id)
		v, e := s.sealWeixinApplication(p, at)
		if e != nil {
			t.Fatal(e)
		}
		proof, e := s.weixinApplicationHeadProof(id, 1, v.proof, at, at)
		if e != nil {
			t.Fatal(e)
		}
		batch.Queue(`INSERT INTO runtime_weixin_app_heads(id,revision,proof_hash,created_at,updated_at) VALUES($1,1,$2,$3,$3)`, id, proof, at)
		batch.Queue(`INSERT INTO runtime_weixin_app_versions(app_id,revision,source_channel_id,kind,nonce,ciphertext,proof_hash,created_at,enabled,archived,draft) VALUES($1,1,0,'official_account',$2,$3,$4,$5,$6,$7,$8)`, id, v.nonce, v.ciphertext, v.proof, at, v.enabled, v.archived, v.draft)
	}
	if e = tx.SendBatch(ctx, batch).Close(); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `SELECT setval(pg_get_serial_sequence('runtime_weixin_app_heads','id'),$1,true)`, n); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
}

func TestWeixinApplicationReviewConcurrentCreatesCannotExceedCatalogCapacity(t *testing.T) {
	s := testStore(t)
	actor := reviewWeixinActor(t, s)
	reviewWeixinSeedDrafts(t, s, 999)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if _, e := s.pool.Exec(ctx, `CREATE FUNCTION app_review_capacity_barrier() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(186000123::bigint); RETURN NEW; END $$; CREATE TRIGGER app_review_capacity_barrier BEFORE INSERT ON runtime_weixin_app_heads FOR EACH ROW WHEN (NEW.id>=1000) EXECUTE FUNCTION app_review_capacity_barrier()`); e != nil {
		t.Fatal(e)
	}
	blocker, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer blocker.Rollback(ctx)
	if _, e = blocker.Exec(ctx, `SELECT pg_advisory_xact_lock(186000123::bigint)`); e != nil {
		t.Fatal(e)
	}
	results := make(chan error, 2)
	for _, request := range []string{strings.Repeat("c", 64), strings.Repeat("d", 64)} {
		go func(request string) {
			_, e := s.MutateWeixinApplication(ctx, actor, 0, reviewWeixinInput(request), WeixinApplicationScope{}, false)
			results <- e
		}(request)
	}
	for {
		var waiting int
		if e = s.pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND (query LIKE 'INSERT INTO runtime_weixin_app_heads%' OR query LIKE 'SELECT pg_advisory_xact_lock(hashtextextended%')`).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting >= 2 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("concurrent creates did not reach both catalog/insert waits", ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
	if e = blocker.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	accepted, refused := 0, 0
	for range 2 {
		select {
		case e := <-results:
			if e == nil {
				accepted++
			} else if errors.Is(e, ErrState) {
				refused++
			} else {
				t.Errorf("unexpected capacity error: %v", e)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	var heads, versions, receipts int
	if e = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM runtime_weixin_app_heads),(SELECT count(*) FROM runtime_weixin_app_versions),(SELECT count(*) FROM runtime_weixin_app_requests)`).Scan(&heads, &versions, &receipts); e != nil || accepted != 1 || refused != 1 || heads != 1000 || versions != 1000 || receipts != 1 {
		t.Fatalf("capacity race committed inconsistent configuration: accepted=%d refused=%d heads=%d versions=%d receipts=%d err=%v", accepted, refused, heads, versions, receipts, e)
	}
	if _, e = s.MutateWeixinApplication(ctx, actor, 0, reviewWeixinInput(strings.Repeat("e", 64)), WeixinApplicationScope{}, false); !errors.Is(e, ErrState) {
		t.Errorf("full catalog accepted another draft: %v", e)
	}
	if _, e = s.CurrentWeixinApplicationSpecs(ctx); e != nil {
		t.Fatal(fmt.Errorf("accepted capacity boundary poisoned the current catalog: %w", e))
	}
}
