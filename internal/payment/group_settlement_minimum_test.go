package payment

import (
	"context"
	"errors"
	"testing"
)

func setGroupSettlementMinimum(t *testing.T, s *Store, id int64, value string) {
	t.Helper()
	if _, err := s.pool.Exec(t.Context(), `UPDATE backoffice_records SET body=jsonb_set(body,'{settle_minmoney}',to_jsonb($2::text)) WHERE resource='groups' AND id=$1`, id, value); err != nil {
		t.Fatal(err)
	}
}

func TestOriginalGroupSettlementMinimumQuoteRequestInheritanceZeroAndReplay(t *testing.T) {
	s := testStore(t)
	settlementMerchant(t, s)
	ctx := context.Background()
	transferSetting(t, s, "settlement_minimum_cents", "101")
	setGroupSettlementMinimum(t, s, 0, "1.23")
	if _, err := s.SettlementQuote(ctx, 681, 122); !errors.Is(err, ErrInvalid) {
		t.Fatal("quote ignored exact group minimum", err)
	}
	for _, actor := range []string{"merchant:681", "admin:1"} {
		if _, err := s.RequestFunds(ctx, "settlements", 681, 0, actor, 122, actor); !errors.Is(err, ErrInvalid) {
			t.Fatal("new request bypassed group minimum", actor, err)
		}
	}
	var count int
	balance, err := s.Balance(ctx, 681)
	if err != nil || balance.Available != 10000 || s.pool.QueryRow(ctx, `SELECT count(*) FROM ledger_requests WHERE kind='settlements'`).Scan(&count) != nil || count != 0 {
		t.Fatal("refused minimum changed funds or history", balance, count, err)
	}
	quote, err := s.SettlementQuote(ctx, 681, 123)
	if err != nil || quote.Policy.MinimumAmount != 123 || quote.Terms == nil || !quote.CanApply {
		t.Fatal("exact minimum quote unavailable", quote, err)
	}
	original, err := s.RequestFunds(ctx, "settlements", 681, 0, "group-minimum", 123, "merchant:681")
	if err != nil {
		t.Fatal(err)
	}
	setGroupSettlementMinimum(t, s, 0, "2.00")
	if replay, err := s.RequestFunds(ctx, "settlements", 681, 0, "group-minimum", 123, "admin:1"); err != nil || replay.ID != original.ID {
		t.Fatal("new minimum rewrote historical application", replay, err)
	}
	if _, err := s.RequestFunds(ctx, "settlements", 681, 0, "new-below-minimum", 123, "merchant:681"); !errors.Is(err, ErrInvalid) {
		t.Fatal("new application used historical minimum", err)
	}
	setGroupSettlementMinimum(t, s, 0, "0")
	if quote, err := s.SettlementQuote(ctx, 681, 1); err != nil || quote.Policy.MinimumAmount != 0 || !quote.CanApply {
		t.Fatal("explicit zero did not override global minimum", quote, err)
	}
	for _, inherit := range []string{"", "_"} {
		setGroupSettlementMinimum(t, s, 0, inherit)
		if _, err := s.SettlementQuote(ctx, 681, 100); !errors.Is(err, ErrInvalid) {
			t.Fatal("inherit did not restore global minimum", inherit, err)
		}
		if quote, err := s.SettlementQuote(ctx, 681, 101); err != nil || quote.Policy.MinimumAmount != 101 {
			t.Fatal("inherited exact minimum unavailable", quote, err)
		}
	}
}

func TestOriginalGroupSettlementMinimumUsesCurrentMembershipAndRejectsCorruptValue(t *testing.T) {
	s := testStore(t)
	settlementMerchant(t, s)
	ctx := context.Background()
	setGroupSettlementMinimum(t, s, 0, "2.00")
	var vip int64
	if err := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"minimum VIP","enabled":true,"settle_minmoney":"0"}') RETURNING id`).Scan(&vip); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES(681,$1,clock_timestamp()+interval '1 day')`, vip); err != nil {
		t.Fatal(err)
	}
	if quote, err := s.SettlementQuote(ctx, 681, 1); err != nil || quote.Policy.MinimumAmount != 0 {
		t.Fatal("active member group minimum ignored", quote, err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE memberships SET expires_at=clock_timestamp()-interval '1 second' WHERE merchant_id=681`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequestFunds(ctx, "settlements", 681, 0, "expired-group-minimum", 199, "merchant:681"); !errors.Is(err, ErrInvalid) {
		t.Fatal("expired group still waived minimum", err)
	}
	for _, value := range []string{"-1", "1.001", "1000000000.01", "bad"} {
		setGroupSettlementMinimum(t, s, 0, value)
		if _, err := s.SettlementQuote(ctx, 681, 200); !errors.Is(err, ErrInvalid) {
			t.Fatal("corrupt group minimum accepted", value, err)
		}
	}
}

func TestOriginalGroupSettlementMinimumScheduledAuthorityKeepsGroupMinimum(t *testing.T) {
	for _, tc := range []struct {
		name, minimum, global, state string
		created                      int64
	}{
		{"minimum", "10.01", "0", "skipped", 0},
		{"explicit-zero", "0", "2000", "created", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			plan := settlementPlanReady(t, s, 901)
			transferSetting(t, s, "settlement_minimum_cents", tc.global)
			setGroupSettlementMinimum(t, s, 0, tc.minimum)
			order, err := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 901, OutTradeNo: "group-minimum-schedule", Amount: 2000})
			if err != nil {
				t.Fatal(err)
			}
			if err = s.MarkPaid(ctx, order.TradeNo, "group-minimum-schedule-paid"); err != nil {
				t.Fatal(err)
			}
			if _, err = s.AuthorizeSettlementPlan(settlementPlanGuard(t, s), plan.ID, plan.Revision, settlementPlanAuthorizationInput(t, s), settlementPlanAdmin); err != nil {
				t.Fatal("group minimum could not be authorized", err)
			}
			run, err := s.CreateSettlementPlanRun(ctx, plan.ID, 1, settlementPlanAdmin)
			if err != nil {
				t.Fatal("authorized group minimum could not be read", err)
			}
			if err = s.GenerateSettlementPlanRun(ctx, run.ID); err != nil {
				t.Fatal(err)
			}
			result, err := s.SettlementPlanRun(ctx, run.ID, settlementPlanAdmin)
			if err != nil || len(result.Items) != 1 || result.CreatedCount != tc.created || result.Items[0].State != tc.state {
				t.Fatal("scheduled quote did not use group minimum", result, err)
			}
		})
	}
}
