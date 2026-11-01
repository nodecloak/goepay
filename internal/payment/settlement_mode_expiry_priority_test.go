package payment

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestSettlementModeRecheckPrecedesExpiredGroupTermsWithoutReserving(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `CREATE TABLE settlement_mode_guard_marker(mid bigint NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for i, tc := range []struct {
		name, baseMode string
		wantMode       bool
	}{
		{"manual_becomes_automatic_only", "1", true},
		{"manual_remains_allowed_but_old_terms_expire", "3", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mid := int64(11807 + i)
			if err := s.CreateMerchant(ctx, Merchant{ID: mid, Name: tc.name, APIKey: "local-mode-priority-only"}); err != nil {
				t.Fatal(err)
			}
			incomePaidOrder(t, s, mid, tc.name+"-income")
			if _, err := s.SetSettlementRecipient(ctx, mid, SettlementRecipientInput{Type: "alipay", Account: "local-mode@example.invalid", Name: "Synthetic Recipient"}, nil, SettlementRecipientActor{Role: "merchant", ID: mid, Name: "local-mode-owner"}); err != nil {
				t.Fatal(err)
			}
			var base, vip int64
			if err := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body)VALUES('groups',jsonb_build_object('name',$2::text,'enabled',true,'settle_open',$1::text))RETURNING id`, tc.baseMode, "base-"+tc.name).Scan(&base); err != nil {
				t.Fatal(err)
			}
			if err := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body)VALUES('groups',jsonb_build_object('name',$1::text,'enabled',true,'settle_open','2','price_cents','0','duration_days','30'))RETURNING id`, "vip-"+tc.name).Scan(&vip); err != nil {
				t.Fatal(err)
			}
			if _, err := s.pool.Exec(ctx, `UPDATE merchants SET base_group_id=$1 WHERE id=$2`, base, mid); err != nil {
				t.Fatal(err)
			}
			if _, err := s.PurchaseMembership(ctx, mid, vip, tc.name+"-membership", fmt.Sprintf("merchant:%d", mid)); err != nil {
				t.Fatal(err)
			}
			var expires time.Time
			if err := s.pool.QueryRow(ctx, `UPDATE memberships SET expires_at=clock_timestamp()+interval '2 seconds' WHERE merchant_id=$1 RETURNING expires_at`, mid).Scan(&expires); err != nil {
				t.Fatal(err)
			}
			quote, err := s.SettlementQuote(ctx, mid, 100)
			if err != nil || !quote.CanApply || quote.Access.GroupID != vip {
				t.Fatal("active membership did not reach confirmation", quote, err)
			}
			counts := func() [6]int64 {
				t.Helper()
				var n [6]int64
				if err := s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM ledger_requests),(SELECT count(*) FROM ledger_request_events),(SELECT count(*) FROM settlement_fundings),(SELECT count(*) FROM settlement_request_terms),(SELECT count(*) FROM settlement_recipients),(SELECT count(*) FROM journals)`).Scan(&n[0], &n[1], &n[2], &n[3], &n[4], &n[5]); err != nil {
					t.Fatal(err)
				}
				return n
			}
			beforeCounts := counts()
			before, err := s.Balance(ctx, mid)
			if err != nil || before.Available != 100 || before.Reserved != 0 {
				t.Fatal("invalid local income fixture", before, err)
			}
			guardCalls := 0
			guarded := WithFundsActionGuard(ctx, func(c context.Context, tx pgx.Tx, kind, key string) error {
				guardCalls++
				if kind != "funds-request" || key != fmt.Sprintf("settlements:%d:%s", mid, tc.name) {
					return ErrInvalid
				}
				if _, e := tx.Exec(c, `INSERT INTO settlement_mode_guard_marker VALUES($1)`, mid); e != nil {
					return e
				}
				_, e := tx.Exec(c, `SELECT pg_sleep(GREATEST(0,extract(epoch FROM ($1::timestamptz-clock_timestamp())))+0.03)`, expires)
				return e
			})
			_, refused := s.RequestFunds(guarded, "settlements", mid, 0, tc.name, 100, fmt.Sprintf("merchant:%d", mid))
			// Continue through the safety assertions even on the initial red run.
			if tc.wantMode {
				if !errors.Is(refused, ErrSettlementMode) {
					t.Errorf("mode closure lost its business refusal: %v", refused)
				}
			} else if !errors.Is(refused, ErrConflict) || errors.Is(refused, ErrSettlementMode) {
				t.Errorf("still-allowed mode must retain expired-term conflict: %v", refused)
			}
			if guardCalls != 1 {
				t.Error("confirmation wait was not reached exactly once", guardCalls)
			}
			afterCounts := counts()
			if afterCounts != beforeCounts {
				t.Error("refused request persisted a financial intent, snapshot or journal", beforeCounts, afterCounts)
			}
			after, err := s.Balance(ctx, mid)
			if err != nil || after.Available != before.Available || after.Reserved != 0 {
				t.Error("refused request reserved funds", after, err)
			}
			var markers int
			if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM settlement_mode_guard_marker WHERE mid=$1`, mid).Scan(&markers); err != nil || markers != 0 {
				t.Error("funds guard transaction did not roll back", markers, err)
			}
			t.Logf("refused=%v available=%d reserved=%d financial_rows_before=%v after=%v guard_markers=%d", refused, after.Available, after.Reserved, beforeCounts, afterCounts, markers)
		})
	}
}
