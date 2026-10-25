package payment

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

var settlementPlanAdmin = SettlementPlanActor{Role: "admin", Class: "super", ID: 1, Name: "local-plan-admin"}

func settlementPlanInputFor(mid int64) SettlementPlanInput {
	return SettlementPlanInput{Name: "local generate only", Mode: "generate_only", MerchantIDs: []int64{mid}, Schedule: SettlementScheduleSpec{Frequency: "hour", Interval: 1, AnchorAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}}
}

func TestSettlementPlanDisabledDefinitionCannotAuthorizeWithoutRequiredFundsGuard(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if err := s.CreateMerchant(ctx, Merchant{ID: 901, Name: "local planned merchant", APIKey: "local-only"}); err != nil {
		t.Fatal(err)
	}
	p, err := s.CreateSettlementPlan(ctx, settlementPlanInputFor(901), settlementPlanAdmin)
	if err != nil || p.Enabled || p.ID <= 0 || p.Revision != 1 || p.LastAuthorizationID != 0 {
		t.Fatal("new definition must be disabled with no money authority", p, err)
	}
	view, err := s.SettlementPlan(ctx, p.ID, settlementPlanAdmin)
	if err != nil || view.Revision != 1 || view.Enabled {
		t.Fatal(view, err)
	}
	actor := settlementPlanAdmin
	actor.Class = "operator"
	if _, err = s.CreateSettlementPlan(ctx, settlementPlanInputFor(901), actor); !errors.Is(err, ErrInvalid) {
		t.Fatal("operator issued a future financial plan", err)
	}
	var now time.Time
	if err = s.pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		t.Fatal(err)
	}
	_, err = s.AuthorizeSettlementPlan(ctx, p.ID, 1, SettlementPlanAuthorizationInput{ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), MaxItemGross: 100, MaxRunGross: 100, MaxDayGross: 100, MaxDayItems: 1}, settlementPlanAdmin)
	if err == nil {
		t.Fatal("missing request-local funds guard created durable authority")
	}
	var authorizations, journals int
	if err = s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM settlement_plan_authorizations),(SELECT count(*) FROM journals)").Scan(&authorizations, &journals); err != nil || authorizations != 0 || journals != 0 {
		t.Fatal("disabled/missing-grant plan changed money or authority", authorizations, journals, err)
	}
}

func TestSettlementPlanPreferenceIsStrictOwnerCASAndBindsOriginalRecipient(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if err := s.CreateMerchant(ctx, Merchant{ID: 901, Name: "local schedule preference", APIKey: "local-only"}); err != nil {
		t.Fatal(err)
	}
	a := SettlementPlanActor{Role: "merchant", ID: 901, Name: "local-owner"}
	initial, err := s.SettlementSchedulePreference(ctx, 901, a)
	if err != nil || initial.Enabled || initial.Revision != 0 {
		t.Fatal("missing preference must default off", initial, err)
	}
	zero := int64(0)
	recipient, err := s.SetSettlementRecipient(ctx, 901, SettlementRecipientInput{Type: "alipay", Account: "local-payee@example.test", Name: "Local Payee"}, &zero, SettlementRecipientActor{Role: "merchant", ID: 901, Name: a.Name})
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.SetSettlementSchedulePreference(ctx, 901, 0, true, recipient.ConfigID, a)
	if err != nil || !p.Enabled || p.Revision != 1 || p.RecipientConfigID != recipient.ConfigID {
		t.Fatal(p, err)
	}
	if _, err = s.SetSettlementSchedulePreference(ctx, 901, 0, false, 0, a); !errors.Is(err, ErrConflict) {
		t.Fatal("stale preference overwrote owner choice", err)
	}
	if _, err = s.SetSettlementSchedulePreference(ctx, 901, 1, false, 0, settlementPlanAdmin); !errors.Is(err, ErrInvalid) {
		t.Fatal("admin impersonated merchant opt-in", err)
	}
	if saved, err := s.SetSettlementSchedulePreference(ctx, 901, 1, false, 0, a); err != nil || saved.Enabled || saved.Revision != 2 {
		t.Fatal("owner cannot disable versioned opt-in", saved, err)
	}
}

// A local guard models a transactional consumed grant. It is never used by
// the scheduler, which must consume its own sealed bounded authority instead.
func settlementPlanGuard(t *testing.T, s *Store) context.Context {
	t.Helper()
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, "CREATE TABLE local_plan_grants(action_key text PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	return WithFundsActionGuard(ctx, func(ctx context.Context, tx pgx.Tx, kind, key string) error {
		if kind != "settlement-plan-authorize" {
			return ErrInvalid
		}
		_, err := tx.Exec(ctx, "INSERT INTO local_plan_grants(action_key) VALUES($1)", key)
		return err
	})
}

func settlementPlanReady(t *testing.T, s *Store, mid int64) SettlementPlanView {
	t.Helper()
	ctx := context.Background()
	if err := s.CreateMerchant(ctx, Merchant{ID: mid, Name: "local ready plan merchant", APIKey: "local-only"}); err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	cfg, err := s.SetSettlementRecipient(ctx, mid, SettlementRecipientInput{Type: "alipay", Account: "local-recipient@example.test", Name: "Local Recipient"}, &zero, SettlementRecipientActor{Role: "merchant", ID: mid, Name: "local-owner"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetSettlementSchedulePreference(ctx, mid, 0, true, cfg.ConfigID, SettlementPlanActor{Role: "merchant", ID: mid, Name: "local-owner"}); err != nil {
		t.Fatal(err)
	}
	p, err := s.CreateSettlementPlan(ctx, settlementPlanInputFor(mid), settlementPlanAdmin)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func settlementPlanAuthorizationInput(t *testing.T, s *Store) SettlementPlanAuthorizationInput {
	t.Helper()
	var now time.Time
	if err := s.pool.QueryRow(context.Background(), "SELECT clock_timestamp()").Scan(&now); err != nil {
		t.Fatal(err)
	}
	return SettlementPlanAuthorizationInput{ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), MaxItemGross: 1000, MaxRunGross: 1000, MaxDayGross: 1000, MaxDayItems: 1}
}

func TestSettlementPlanAuthorizationReadyScopeRequiresOneTransactionalGrant(t *testing.T) {
	s := testStore(t)
	p := settlementPlanReady(t, s, 901)
	in := settlementPlanAuthorizationInput(t, s)
	if _, err := s.AuthorizeSettlementPlan(context.Background(), p.ID, 1, in, settlementPlanAdmin); err == nil {
		t.Fatal("ready scope bypassed mandatory financial confirmation")
	}
	guarded := settlementPlanGuard(t, s)
	a, err := s.AuthorizeSettlementPlan(guarded, p.ID, 1, in, settlementPlanAdmin)
	if err != nil || a.ID <= 0 || a.PlanID != p.ID || a.PlanRevision != 1 || a.Revoked {
		t.Fatal("explicit bounded authority was not created", a, err)
	}
	view, err := s.SettlementPlan(context.Background(), p.ID, settlementPlanAdmin)
	if err != nil || !view.Enabled || view.LastAuthorizationID != a.ID {
		t.Fatal(view, err)
	}
	if _, err = s.AuthorizeSettlementPlan(guarded, p.ID, 1, in, settlementPlanAdmin); !errors.Is(err, ErrConflict) {
		t.Fatal("old authority pointer consumed another grant", err)
	}
	var grants, authorizations int
	if err = s.pool.QueryRow(context.Background(), "SELECT (SELECT count(*) FROM local_plan_grants),(SELECT count(*) FROM settlement_plan_authorizations)").Scan(&grants, &authorizations); err != nil || grants != 1 || authorizations != 1 {
		t.Fatal(grants, authorizations, err)
	}
	if err = s.RevokeSettlementPlanAuthorization(context.Background(), a.ID, settlementPlanAdmin); err != nil {
		t.Fatal(err)
	}
	view, err = s.SettlementPlan(context.Background(), p.ID, settlementPlanAdmin)
	if err != nil || view.Enabled {
		t.Fatal("revoked authority remained armed", view, err)
	}
}
