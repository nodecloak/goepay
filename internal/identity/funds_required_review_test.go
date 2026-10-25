package identity

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestFundsRequiredReviewPolicyOffPreservesSessionAuditAndSingleUse(t *testing.T) {
	p, f, actor := fundsFixture(t)
	ctx := context.Background()
	if _, err := p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"admin_funds_authorization_enabled","value":"false"}')`); err != nil {
		t.Fatal(err)
	}
	if err := f.SetPassword(ctx, actor, fundsLogin, "", fundsPassword, ""); err != nil {
		t.Fatal(err)
	}
	action, err := NewFundsAction("admin.settlement_plan_authorize", "/console/v1/admin/settlement-plans/1/authorizations", map[string]string{"body_hmac": "local-body", "snapshot_hmac": "local-plan-v1"})
	if err != nil {
		t.Fatal(err)
	}
	grant := grantFor(t, f, actor, action)
	checkDenied := func(a FundsActor, token string, act FundsAction, want error) {
		t.Helper()
		tx, err := p.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if err = f.ConsumeRequiredTx(ctx, tx, a, token, act, "1:1"); !errors.Is(err, want) {
			t.Fatalf("mandatory grant accepted wrong session/authority: %v want%v", err, want)
		}
	}
	checkDenied(actor, "", action, ErrFundsAuthorization)
	other := actor
	other.SessionToken, _, err = NewPersistentSessions(p, false).Create(actor.User)
	if err != nil {
		t.Fatal(err)
	}
	checkDenied(other, grant, action, ErrFundsAuthorization)
	otherGrant := grantFor(t, f, other, action)
	if _, err = p.Exec(ctx, `INSERT INTO console_session_issuers(token_hash,issuer_session_hash,issuer_username,issuer_id,issuer_auth_version) VALUES($1,$2,$3,$4,$5)`, tokenDigest(other.SessionToken), tokenDigest(actor.SessionToken), actor.User.Username, actor.User.ID, actor.User.AuthVersion); err != nil {
		t.Fatal(err)
	}
	// A caller cannot hide durable delegation by presenting Delegated=false.
	checkDenied(other, otherGrant, action, ErrFundsAuthentication)
	delegated := actor
	delegated.Delegated = true
	checkDenied(delegated, grant, action, ErrFundsAuthentication)
	changed, err := NewFundsAction(action.Kind, action.ResourceID, map[string]string{"body_hmac": "local-body", "snapshot_hmac": "local-plan-v2"})
	if err != nil {
		t.Fatal(err)
	}
	checkDenied(actor, grant, changed, ErrFundsAuthorization)
	// A real audit failure after the consumed_at update must roll back the
	// original grant. The optional policy switch remains explicitly false.
	if _, err = p.Exec(ctx, `CREATE FUNCTION fail_required_consume_review() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.operation='consume' THEN RAISE EXCEPTION 'local required consume audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER fail_required_consume_review BEFORE INSERT ON admin_funds_audit FOR EACH ROW EXECUTE FUNCTION fail_required_consume_review()`); err != nil {
		t.Fatal(err)
	}
	tx, err := p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.ConsumeRequiredTx(ctx, tx, actor, grant, action, "1:1"); err == nil {
		t.Error("required grant committed without its audit")
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var consumed bool
	if err = p.QueryRow(ctx, "SELECT consumed_at IS NOT NULL FROM admin_funds_grants WHERE grant_hash=$1", tokenDigest(grant)).Scan(&consumed); err != nil || consumed {
		t.Fatal("audit failure burned the authority grant", consumed, err)
	}
	if _, err = p.Exec(ctx, "DROP TRIGGER fail_required_consume_review ON admin_funds_audit"); err != nil {
		t.Fatal(err)
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	unexpected := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tx, err := p.Begin(ctx)
			if err != nil {
				unexpected <- err
				return
			}
			defer tx.Rollback(ctx)
			err = f.ConsumeRequiredTx(ctx, tx, actor, grant, action, "1:1")
			if err == nil {
				if err = tx.Commit(ctx); err == nil {
					successes.Add(1)
				} else {
					unexpected <- err
				}
			} else if !errors.Is(err, ErrFundsAuthorization) {
				unexpected <- err
			}
		}()
	}
	wg.Wait()
	close(unexpected)
	for err := range unexpected {
		t.Error(err)
	}
	var audits int
	if err = p.QueryRow(ctx, "SELECT count(*) FROM admin_funds_audit WHERE operation='consume'").Scan(&audits); err != nil || successes.Load() != 1 || audits != 1 {
		t.Fatalf("optional off bypassed mandatory one-use: success%d audits%d %v", successes.Load(), audits, err)
	}
}
