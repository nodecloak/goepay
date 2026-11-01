package payment

import (
	"context"
	"errors"
	"testing"
	"time"

	"goepay/internal/merchantpolicy"
)

// A repaired, unresolved administrative baseline is not evidence against an
// independently purchased membership. It becomes relevant only when no valid
// membership can be selected; it must never become a default-group fallback.
func TestEffectiveGroupActiveMembershipSurvivesUnresolvedBaseline(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	const mid int64 = 186810
	if err := s.CreateMerchant(ctx, Merchant{ID: mid, Name: "unresolved baseline", APIKey: "local-reader-test-only"}); err != nil {
		t.Fatal(err)
	}
	var vip int64
	if err := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"independent paid membership","enabled":true,"rate_bps":"100"}') RETURNING id`).Scan(&vip); err != nil {
		t.Fatal(err)
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := s.pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE merchants SET base_group_id=0,group_baseline_unresolved=true WHERE id=$1`, mid)
	reset := func() {
		t.Helper()
		exec(`UPDATE merchants SET enabled=true WHERE id=$1`, mid)
		exec(`UPDATE backoffice_records SET resource='groups',body=jsonb_set(body,'{enabled}','true') WHERE id=$1`, vip)
		exec(`INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES($1,$2,clock_timestamp()+interval '1 hour') ON CONFLICT(merchant_id) DO UPDATE SET group_id=excluded.group_id,expires_at=excluded.expires_at`, mid, vip)
	}
	for _, tc := range []struct {
		name       string
		change     func()
		strictFail bool
		setupFail  bool
	}{
		{"active_membership", func() {}, false, false},
		{"expired_membership", func() {
			exec(`UPDATE memberships SET expires_at=clock_timestamp()-interval '1 second' WHERE merchant_id=$1`, mid)
		}, true, true},
		{"missing_membership", func() { exec(`DELETE FROM memberships WHERE merchant_id=$1`, mid) }, true, true},
		{"disabled_purchased_group", func() {
			exec(`UPDATE backoffice_records SET body=jsonb_set(body,'{enabled}','false') WHERE id=$1`, vip)
		}, true, true},
		// The foreign key still points to a real record, but it is not a group.
		{"missing_purchased_group", func() { exec(`UPDATE backoffice_records SET resource='settings' WHERE id=$1`, vip) }, true, true},
		{"inactive_merchant", func() { exec(`UPDATE merchants SET enabled=false WHERE id=$1`, mid) }, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reset()
			tc.change()
			for _, lock := range []bool{false, true} {
				for _, setup := range []bool{false, true} {
					tx, err := s.pool.Begin(ctx)
					if err != nil {
						t.Fatal(err)
					}
					var got merchantpolicy.EffectiveGroup
					if setup {
						got, err = merchantpolicy.ReadEffectiveGroupForSetup(ctx, tx, mid, lock)
					} else {
						got, err = merchantpolicy.ReadEffectiveGroup(ctx, tx, mid, lock)
					}
					_ = tx.Rollback(ctx)
					wantErr := tc.strictFail
					if setup {
						wantErr = tc.setupFail
					}
					if wantErr {
						if !errors.Is(err, merchantpolicy.ErrGroupPolicy) {
							t.Errorf("setup=%v lock=%v: unavailable group accepted: %+v err=%v", setup, lock, got, err)
						}
					} else if err != nil || got.ID != vip || got.ValidUntil.IsZero() || !time.Now().Before(got.ValidUntil) {
						t.Errorf("setup=%v lock=%v: active purchase lost to unresolved baseline: %+v err=%v", setup, lock, got, err)
					}
				}
			}
		})
	}
	var unresolved bool
	if err := s.pool.QueryRow(ctx, `SELECT group_baseline_unresolved FROM merchants WHERE id=$1`, mid).Scan(&unresolved); err != nil || !unresolved {
		t.Fatal("reader resolved the administrative baseline", unresolved, err)
	}
}

func TestEffectiveGroupUnresolvedMembershipExpiryAfterGroupLock(t *testing.T) {
	s := testStore(t)
	for _, setup := range []bool{false, true} {
		name := "money_policy"
		if setup {
			name = "setup_projection"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			mid := int64(186811)
			if setup {
				mid++
			}
			if err := s.CreateMerchant(ctx, Merchant{ID: mid, Name: name, APIKey: "local-lock-test-only"}); err != nil {
				t.Fatal(err)
			}
			var vip int64
			if err := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"enabled":true}') RETURNING id`).Scan(&vip); err != nil {
				t.Fatal(err)
			}
			if _, err := s.pool.Exec(ctx, `UPDATE merchants SET group_baseline_unresolved=true WHERE id=$1`, mid); err != nil {
				t.Fatal(err)
			}
			if _, err := s.pool.Exec(ctx, `INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES($1,$2,clock_timestamp()+interval '2 seconds')`, mid, vip); err != nil {
				t.Fatal(err)
			}
			block, err := s.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer block.Rollback(context.Background())
			var locked int64
			if err = block.QueryRow(ctx, `SELECT id FROM backoffice_records WHERE id=$1 FOR UPDATE`, vip).Scan(&locked); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			pids := make(chan int, 1)
			go func() {
				tx, e := s.pool.Begin(ctx)
				if e != nil {
					done <- e
					return
				}
				defer tx.Rollback(context.Background())
				var pid int
				if e = tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
					done <- e
					return
				}
				pids <- pid
				if setup {
					_, e = merchantpolicy.ReadEffectiveGroupForSetup(ctx, tx, mid, true)
				} else {
					_, e = merchantpolicy.ReadEffectiveGroup(ctx, tx, mid, true)
				}
				done <- e
			}()
			var pid int
			select {
			case pid = <-pids:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			for {
				var waiting bool
				if err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event_type='Lock')`, pid).Scan(&waiting); err != nil {
					t.Fatal(err)
				}
				if waiting {
					break
				}
				select {
				case early := <-done:
					t.Fatal("reader rejected before checking the purchased group", early)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-time.After(10 * time.Millisecond):
				}
			}
			if _, err = s.pool.Exec(ctx, `SELECT pg_sleep(GREATEST(0,extract(epoch FROM expires_at-clock_timestamp()))::double precision+0.05) FROM memberships WHERE merchant_id=$1`, mid); err != nil {
				t.Fatal(err)
			}
			if err = block.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case err = <-done:
				if !errors.Is(err, merchantpolicy.ErrGroupPolicy) {
					t.Fatal("expired purchase used unresolved baseline", err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		})
	}
}
