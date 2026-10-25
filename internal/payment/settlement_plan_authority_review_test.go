package payment

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// This deliberately independent fixture records grant consumption in the same
// transaction as the authority. The production HTTP guard is tested separately.
func settlementAuthorityReviewGuard(t *testing.T, s *Store) context.Context {
	t.Helper()
	if _, err := s.pool.Exec(context.Background(), "CREATE TABLE local_authority_review_grants(id bigserial PRIMARY KEY,action_key text NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	return WithFundsActionGuard(context.Background(), func(ctx context.Context, tx pgx.Tx, kind, key string) error {
		if kind != "settlement-plan-authorize" {
			return ErrInvalid
		}
		_, err := tx.Exec(ctx, "INSERT INTO local_authority_review_grants(action_key) VALUES($1)", key)
		return err
	})
}

func TestSettlementPlanAuthorityReviewCannotRewindSupersededAuthority(t *testing.T) {
	s := testStore(t)
	p := settlementPlanReady(t, s, 901)
	ctx := settlementAuthorityReviewGuard(t, s)
	in := settlementPlanAuthorizationInput(t, s)
	a1, err := s.AuthorizeSettlementPlan(ctx, p.ID, p.Revision, in, settlementPlanAdmin)
	if err != nil {
		t.Fatal(err)
	}
	in.ExpectedAuthorizationID = a1.ID
	in.MaxItemGross, in.MaxRunGross, in.MaxDayGross = 100, 100, 100
	a2, err := s.AuthorizeSettlementPlan(ctx, p.ID, p.Revision, in, settlementPlanAdmin)
	if err != nil || a2.ID <= a1.ID {
		t.Fatal(a2, err)
	}
	// A SQL writer cannot forge either HMAC. Replaying the old, still signed
	// pointer must nevertheless fail closed after the owner reduced the budget.
	_, err = s.pool.Exec(ctx, "UPDATE settlement_plans SET last_authorization_id=$2 WHERE id=$1", p.ID, a1.ID)
	if err != nil {
		var guardError *pgconn.PgError
		if !errors.As(err, &guardError) || guardError.Code != "P0001" || guardError.Message != "settlement plan requires a fresh chained authority" {
			t.Fatal("rewind failed for an unrelated reason", err)
		}
		t.Log("SQL head guard rejects replaying superseded signed authority")
	} else {
		view, e := s.SettlementPlan(ctx, p.ID, settlementPlanAdmin)
		if !errors.Is(e, ErrConflict) {
			t.Fatalf("superseded larger authority became current again: view=%+v old=%d new=%d err=%v", view, a1.ID, a2.ID, e)
		}
	}
	// A damaged restore can lose both immediate and deferred head guards.
	// Restoring those guards is insufficient: the reader must reject the pointer.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "ALTER TABLE settlement_plans DISABLE TRIGGER settlement_plan_head"); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "ALTER TABLE settlement_plans DISABLE TRIGGER settlement_plan_complete"); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "UPDATE settlement_plans SET last_authorization_id=$2 WHERE id=$1", p.ID, a1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "SET CONSTRAINTS ALL IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "ALTER TABLE settlement_plans ENABLE TRIGGER settlement_plan_head"); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "ALTER TABLE settlement_plans ENABLE TRIGGER settlement_plan_complete"); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal("damaged-restore fixture did not reach keyed reader", err)
	}
	view, err := s.SettlementPlan(ctx, p.ID, settlementPlanAdmin)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("damaged restore reactivated superseded authority: view=%+v old=%d new=%d err=%v", view, a1.ID, a2.ID, err)
	}
	if _, err = s.readSettlementPlanAuthorization(ctx, s.pool, a1.ID); err != nil {
		t.Fatal("historical proof must remain readable independently from current head", err)
	}
}

func TestSettlementPlanAuthorityReviewCASAndAuditRollback(t *testing.T) {
	s := testStore(t)
	p := settlementPlanReady(t, s, 901)
	ctx := settlementAuthorityReviewGuard(t, s)
	in := settlementPlanAuthorizationInput(t, s)
	if _, err := s.pool.Exec(ctx, `CREATE FUNCTION local_authority_audit_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='settlement-plan-authorizations' AND NEW.operation='authorize' THEN RAISE EXCEPTION 'local authority audit failure'; END IF; RETURN NEW; END $$;
CREATE TRIGGER local_authority_audit_fail BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION local_authority_audit_fail()`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthorizeSettlementPlan(ctx, p.ID, 1, in, settlementPlanAdmin); err == nil {
		t.Fatal("audit failure was ignored")
	}
	var grants, authorities int
	if err := s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM local_authority_review_grants),(SELECT count(*) FROM settlement_plan_authorizations)").Scan(&grants, &authorities); err != nil || grants != 0 || authorities != 0 {
		t.Fatal("failed authorization consumed grant or persisted authority", grants, authorities, err)
	}
	view, err := s.SettlementPlan(ctx, p.ID, settlementPlanAdmin)
	if err != nil || view.Enabled || view.LastAuthorizationID != 0 {
		t.Fatal(view, err)
	}
	if _, err = s.pool.Exec(ctx, "DROP TRIGGER local_authority_audit_fail ON backoffice_audit"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.AuthorizeSettlementPlan(ctx, p.ID, 1, in, settlementPlanAdmin)
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	success := 0
	for err := range errs {
		if err == nil {
			success++
		} else if !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	if err = s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM local_authority_review_grants),(SELECT count(*) FROM settlement_plan_authorizations)").Scan(&grants, &authorities); err != nil || success != 1 || grants != 1 || authorities != 1 {
		t.Fatal("CAS allowed multiple grant consumers", success, grants, authorities, err)
	}
	view, err = s.SettlementPlan(ctx, p.ID, settlementPlanAdmin)
	if err != nil || !view.Enabled {
		t.Fatal(view, err)
	}
	if _, err = s.pool.Exec(ctx, `CREATE OR REPLACE FUNCTION local_authority_audit_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='settlement-plan-authorizations' THEN RAISE EXCEPTION 'local revoke audit failure'; END IF; RETURN NEW; END $$;
CREATE TRIGGER local_authority_audit_fail BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION local_authority_audit_fail()`); err != nil {
		t.Fatal(err)
	}
	if err = s.RevokeSettlementPlanAuthorization(ctx, view.LastAuthorizationID, settlementPlanAdmin); err == nil {
		t.Fatal("revoke ignored missing audit")
	}
	if after, e := s.SettlementPlan(ctx, p.ID, settlementPlanAdmin); e != nil || !after.Enabled || after.LastAuthorizationID != view.LastAuthorizationID {
		t.Fatal("failed revoke changed authority", after, e)
	}
	var revoked int
	if err = s.pool.QueryRow(ctx, "SELECT count(*) FROM settlement_plan_revocations").Scan(&revoked); err != nil || revoked != 0 {
		t.Fatal("failed revoke persisted proof", revoked, err)
	}
}

func TestSettlementPlanAuthorityReviewSnapshotUsesCallerTransactionAndConfigAuditRollback(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := settlementPlanReady(t, s, 901)
	in := settlementPlanAuthorizationInput(t, s)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "UPDATE merchants SET enabled=false WHERE id=901"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SettlementPlanAuthorizationSnapshot(ctx, tx, p.ID, 1, in); !errors.Is(err, ErrState) {
		t.Fatal("snapshot escaped caller transaction's uncommitted scope", err)
	}
	if _, err = s.SettlementPlanAuthorizationSnapshot(ctx, s.pool, p.ID, 1, in); err != nil {
		t.Fatal("uncommitted merchant change escaped transaction", err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, `CREATE FUNCTION local_config_audit_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource IN ('settlement-plan-versions','settlement-schedule-preferences') THEN RAISE EXCEPTION 'local configuration audit failure'; END IF; RETURN NEW; END $$;
CREATE TRIGGER local_config_audit_fail BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION local_config_audit_fail()`); err != nil {
		t.Fatal(err)
	}
	changed := settlementPlanInputFor(901)
	changed.Name = "must roll back"
	if _, err = s.UpdateSettlementPlan(ctx, p.ID, 1, changed, settlementPlanAdmin); err == nil {
		t.Fatal("definition ignored audit failure")
	}
	owner := SettlementPlanActor{Role: "merchant", ID: 901, Name: "local-owner"}
	if _, err = s.SetSettlementSchedulePreference(ctx, 901, 1, false, 0, owner); err == nil {
		t.Fatal("preference ignored audit failure")
	}
	current, err := s.SettlementPlan(ctx, p.ID, settlementPlanAdmin)
	if err != nil || current.Revision != 1 || current.Name != p.Name {
		t.Fatal("failed config audit changed plan head", current, err)
	}
	pref, err := s.SettlementSchedulePreference(ctx, 901, owner)
	if err != nil || pref.Revision != 1 || !pref.Enabled {
		t.Fatal("failed config audit changed owner opt-in", pref, err)
	}
	var versions, preferences int
	if err = s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM settlement_plan_versions),(SELECT count(*) FROM settlement_schedule_preferences)").Scan(&versions, &preferences); err != nil || versions != 1 || preferences != 1 {
		t.Fatal("failed audit leaked immutable configuration history", versions, preferences, err)
	}
}

func TestSettlementPlanAuthorityReviewShapeValidForgeriesAreNotTrusted(t *testing.T) {
	for _, forged := range []string{"authority", "revocation"} {
		t.Run(forged, func(t *testing.T) {
			s := testStore(t)
			p := settlementPlanReady(t, s, 901)
			ctx := settlementAuthorityReviewGuard(t, s)
			a, err := s.AuthorizeSettlementPlan(ctx, p.ID, 1, settlementPlanAuthorizationInput(t, s), settlementPlanAdmin)
			if err != nil {
				t.Fatal(err)
			}
			tx, err := s.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			var id int64
			if forged == "authority" {
				err = tx.QueryRow(ctx, `INSERT INTO settlement_plan_authorizations(plan_id,plan_revision,expected_authorization_id,valid_from,expires_at,max_item_gross,max_run_gross,max_day_gross,max_day_items,scope_snapshot,actor_id,actor_name,proof_hash)
SELECT plan_id,plan_revision,id,valid_from,expires_at,max_item_gross,max_run_gross,max_day_gross,max_day_items,scope_snapshot,actor_id,actor_name,decode(repeat('00',32),'hex') FROM settlement_plan_authorizations WHERE id=$1 RETURNING id`, a.ID).Scan(&id)
				if err == nil {
					err = settlementPlanAudit(ctx, tx, "admin", settlementPlanAdmin.ID, settlementPlanAdmin.Name, "settlement-plan-authorizations", id, "authorize", map[string]string{"proof": "untrusted-local-fixture"})
				}
				if err == nil {
					_, err = tx.Exec(ctx, "UPDATE settlement_plans SET last_authorization_id=$2 WHERE id=$1", p.ID, id)
				}
			} else {
				id = a.ID
				_, err = tx.Exec(ctx, "INSERT INTO settlement_plan_revocations(authorization_id,plan_id,actor_id,actor_name,proof_hash) VALUES($1,$2,$3,$4,decode(repeat('00',32),'hex'))", a.ID, p.ID, settlementPlanAdmin.ID, settlementPlanAdmin.Name)
				if err == nil {
					err = settlementPlanAudit(ctx, tx, "admin", settlementPlanAdmin.ID, settlementPlanAdmin.Name, "settlement-plan-authorizations", id, "revoke", map[string]string{"proof": "untrusted-local-fixture"})
				}
				if err == nil {
					_, err = tx.Exec(ctx, "UPDATE settlement_plans SET enabled=false WHERE id=$1", p.ID)
				}
			}
			if err != nil {
				t.Fatal("fixture must reach the keyed reader with every structural gate enabled", err)
			}
			if err = tx.Commit(ctx); err != nil {
				t.Fatal("shape-valid forgery fixture failed structural validation", err)
			}
			if _, err = s.readSettlementPlanAuthorization(ctx, s.pool, id); !errors.Is(err, ErrConflict) {
				t.Fatal("shape-valid SQL forgery became trusted authority", err)
			}
			if _, err = s.SettlementPlan(ctx, p.ID, settlementPlanAdmin); !errors.Is(err, ErrConflict) {
				t.Fatal("public head accepted forged signature", err)
			}
		})
	}
}

func TestSettlementPlanAuthorityReviewHistoryValidityAndTimezone(t *testing.T) {
	s := testStore(t)
	p := settlementPlanReady(t, s, 901)
	ctx := settlementAuthorityReviewGuard(t, s)
	in := settlementPlanAuthorizationInput(t, s)
	bad := in
	bad.ExpiresAt = bad.ExpiresAt.Add(time.Nanosecond)
	if _, err := s.AuthorizeSettlementPlan(ctx, p.ID, 1, bad, settlementPlanAdmin); !errors.Is(err, ErrInvalid) {
		t.Fatal("submicrosecond authority accepted", err)
	}
	bad = in
	bad.ValidFrom, bad.ExpiresAt = in.ValidFrom.Add(-2*time.Hour), in.ValidFrom.Add(-time.Hour)
	if _, err := s.AuthorizeSettlementPlan(ctx, p.ID, 1, bad, settlementPlanAdmin); !errors.Is(err, ErrState) {
		t.Fatal("already expired authority accepted", err)
	}
	in.ValidFrom = in.ValidFrom.In(time.FixedZone("offset", -7*3600))
	in.ExpiresAt = in.ExpiresAt.In(time.FixedZone("offset", -7*3600))
	a1, err := s.AuthorizeSettlementPlan(ctx, p.ID, 1, in, settlementPlanAdmin)
	if err != nil {
		t.Fatal(err)
	}
	original, err := s.readSettlementPlanAuthorization(ctx, s.pool, a1.ID)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SET LOCAL TIME ZONE 'Pacific/Honolulu'"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.readSettlementPlanAuthorization(ctx, tx, a1.ID); err != nil {
		t.Fatal("signed authority depends on reader timezone", err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	owner := SettlementPlanActor{Role: "merchant", ID: 901, Name: "local-owner"}
	if _, err = s.SetSettlementSchedulePreference(ctx, 901, 1, false, 0, owner); err != nil {
		t.Fatal(err)
	}
	history, err := s.readSettlementPreferenceVersion(ctx, s.pool, original.Scope.Merchants[0].PreferenceID)
	if err != nil || !history.View.Enabled || history.View.Revision != 1 || history.View.Recipient.ConfigID != original.Scope.Merchants[0].RecipientConfigID {
		t.Fatal("owner disable corrupted original immutable opt-in", history, err)
	}
	if _, err = s.readSettlementPlanAuthorization(ctx, s.pool, a1.ID); err != nil {
		t.Fatal("later preference invalidated signed historical authorization", err)
	}
	if _, err = s.SetSettlementSchedulePreference(ctx, 901, 2, true, history.View.RecipientConfigID, owner); err != nil {
		t.Fatal(err)
	}
	changed := settlementPlanInputFor(901)
	changed.Name = "revised local plan"
	p2, err := s.UpdateSettlementPlan(ctx, p.ID, 1, changed, settlementPlanAdmin)
	if err != nil || p2.Enabled || p2.Revision != 2 {
		t.Fatal(p2, err)
	}
	in.ExpectedAuthorizationID = a1.ID
	in.ValidFrom, in.ExpiresAt = in.ExpiresAt.Add(time.Hour), in.ExpiresAt.Add(2*time.Hour)
	a2, err := s.AuthorizeSettlementPlan(ctx, p.ID, 2, in, settlementPlanAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RevokeSettlementPlanAuthorization(ctx, a1.ID, settlementPlanAdmin); err != nil {
		t.Fatal("historical revoke failed after definition change", err)
	}
	current, err := s.SettlementPlan(ctx, p.ID, settlementPlanAdmin)
	if err != nil || current.LastAuthorizationID != a2.ID || current.Enabled {
		t.Fatal("future authority incorrectly active or historical revoke changed its pointer", current, err)
	}
	var armed bool
	if err = s.pool.QueryRow(ctx, "SELECT enabled FROM settlement_plans WHERE id=$1", p.ID).Scan(&armed); err != nil || !armed {
		t.Fatal("revoking old authority disabled replacement", armed, err)
	}
	old, err := s.readSettlementPlanAuthorization(ctx, s.pool, a1.ID)
	if err != nil || !old.View.Revoked || old.View.PlanRevision != 1 {
		t.Fatal(old, err)
	}
	if _, err = s.pool.Exec(ctx, "UPDATE settlement_plan_authorizations SET max_item_gross=1 WHERE id=$1", a1.ID); err == nil {
		t.Fatal("immutable authority updated")
	}
	if _, err = s.pool.Exec(ctx, "DELETE FROM settlement_schedule_preferences WHERE id=$1", history.ID); err == nil {
		t.Fatal("immutable preference deleted")
	}
}
