package identity

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

const fundsLogin = "fixture-login-password-123"
const fundsPassword = "separate-funds-password-456"

func fundsFixture(t *testing.T) (*pgxpool.Pool, *FundsAuthorization, FundsActor) {
	t.Helper()
	p := identityTestPool(t)
	ctx := context.Background()
	hash, e := HashPassword(fundsLogin)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) VALUES('funds','admin',81,'Funds',$1,'super')`, hash); e != nil {
		t.Fatal(e)
	}
	u := User{Username: "funds", Role: "admin", ID: 81, AuthVersion: 1, AdminClass: "super"}
	token, _, e := NewPersistentSessions(p, false).Create(u)
	if e != nil {
		t.Fatal(e)
	}
	return p, NewFundsAuthorization(p, nil), FundsActor{User: u, SessionToken: token, ClientIP: "192.0.2.1"}
}
func fundsAction(t *testing.T, n string) FundsAction {
	t.Helper()
	a, e := NewFundsAction("transfer.create", n, map[string]string{"merchant_id": "1", "amount_cents": "125", "recipient": "synthetic-account", "channel_id": "7"})
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func grantFor(t *testing.T, f *FundsAuthorization, actor FundsActor, a FundsAction) string {
	t.Helper()
	c, e := f.Prepare(context.Background(), actor, a)
	if e != nil {
		t.Fatal(e)
	}
	g, e := f.Authorize(context.Background(), actor, c.ChallengeID, fundsPassword)
	if e != nil {
		t.Fatal(e)
	}
	return g.Authorization
}
func enableFunds(t *testing.T, p *pgxpool.Pool) {
	t.Helper()
	if _, e := p.Exec(context.Background(), `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"admin_funds_authorization_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
}

func TestFundsCredentialIndependentArgonAndNoDefault(t *testing.T) {
	p, f, a := fundsFixture(t)
	ctx := context.Background()
	status, e := f.Status(ctx, a)
	if e != nil || status.Configured || status.Enforced {
		t.Fatal(status, e)
	}
	if _, e = f.Prepare(ctx, a, fundsAction(t, "unconfigured")); !errors.Is(e, ErrFundsNotConfigured) {
		t.Fatal(e)
	}
	for _, password := range []string{"123456", fundsLogin, strings.Repeat("a", 1025)} {
		if e = f.SetPassword(ctx, a, fundsLogin, "", password, ""); e == nil {
			t.Fatal("weak/equal password accepted")
		}
	}
	if e = f.SetPassword(ctx, a, "wrong-login", "", fundsPassword, ""); !errors.Is(e, ErrFundsAuthentication) {
		t.Fatal(e)
	}
	if e = f.SetPassword(ctx, a, fundsLogin, "", fundsPassword, ""); e != nil {
		t.Fatal(e)
	}
	var hash string
	var epoch int64
	if e = p.QueryRow(ctx, "SELECT password_hash,credential_epoch FROM admin_funds_credentials WHERE username='funds'").Scan(&hash, &epoch); e != nil || !strings.HasPrefix(hash, "$argon2id$") || !CheckPassword(hash, fundsPassword) || epoch != 1 {
		t.Fatal("not hashed", epoch, e)
	}
	if e = f.SetPassword(ctx, a, fundsLogin, "wrong-existing", "another-funds-secret-789", ""); !errors.Is(e, ErrFundsAuthentication) {
		t.Fatal("did not require old funds password", e)
	}
	if e = f.SetPassword(ctx, a, fundsLogin, fundsPassword, "another-funds-secret-789", ""); e != nil {
		t.Fatal(e)
	}
	if e = p.QueryRow(ctx, "SELECT credential_epoch FROM admin_funds_credentials WHERE username='funds'").Scan(&epoch); e != nil || epoch != 2 {
		t.Fatal(epoch, e)
	}
}

func TestFundsGrantTransactionalSingleUseAndImmutableBinding(t *testing.T) {
	p, f, a := fundsFixture(t)
	ctx := context.Background()
	enableFunds(t, p)
	if e := f.SetPassword(ctx, a, fundsLogin, "", fundsPassword, ""); e != nil {
		t.Fatal(e)
	}
	action := fundsAction(t, "immutable-key")
	grant := grantFor(t, f, a, action)
	tx, e := p.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	changed := fundsAction(t, "changed-key")
	if e = f.ConsumeTx(ctx, tx, a, grant, changed, "transfer:1"); !errors.Is(e, ErrFundsAuthorization) {
		t.Fatal("unbound action", e)
	}
	tx.Rollback(ctx)
	tx, _ = p.Begin(ctx)
	if e = f.ConsumeTx(ctx, tx, a, grant, action, "transfer:1"); e != nil {
		t.Fatal(e)
	}
	tx.Rollback(ctx)
	var successes atomic.Int32
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			other := NewFundsAuthorization(p, nil)
			tx, e := p.Begin(ctx)
			if e != nil {
				t.Error(e)
				return
			}
			defer tx.Rollback(ctx)
			if e = other.ConsumeTx(ctx, tx, a, grant, action, "transfer:1"); e == nil {
				if e = tx.Commit(ctx); e != nil {
					t.Error(e)
				} else {
					successes.Add(1)
				}
			} else if !errors.Is(e, ErrFundsAuthorization) {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatal("grant owners", successes.Load())
	}
	var n int
	if e = p.QueryRow(ctx, "SELECT count(*) FROM admin_funds_audit WHERE operation='consume'").Scan(&n); e != nil || n != 1 {
		t.Fatal(n, e)
	}
	// A consumed grant can never authorize a second send, including the same key.
	tx, _ = p.Begin(ctx)
	defer tx.Rollback(ctx)
	if e = f.ConsumeTx(ctx, tx, a, grant, action, "transfer:1"); !errors.Is(e, ErrFundsAuthorization) {
		t.Fatal("grant replay", e)
	}
}

func TestFundsGrantSessionCredentialAndAuthenticationEpochRevocation(t *testing.T) {
	for _, kind := range []string{"new-session", "logout", "auth-epoch", "credential-epoch", "delegation", "expired"} {
		t.Run(kind, func(t *testing.T) {
			p, f, a := fundsFixture(t)
			ctx := context.Background()
			enableFunds(t, p)
			if e := f.SetPassword(ctx, a, fundsLogin, "", fundsPassword, ""); e != nil {
				t.Fatal(e)
			}
			action := fundsAction(t, kind)
			grant := grantFor(t, f, a, action)
			switch kind {
			case "new-session":
				token, _, e := NewPersistentSessions(p, false).Create(a.User)
				if e != nil {
					t.Fatal(e)
				}
				a.SessionToken = token
			case "logout":
				NewPersistentSessions(p, false).Delete(a.SessionToken)
			case "auth-epoch":
				p.Exec(ctx, "UPDATE console_users SET auth_version=auth_version+1 WHERE username='funds'")
			case "credential-epoch":
				if e := f.SetPassword(ctx, a, fundsLogin, fundsPassword, "changed-funds-password", ""); e != nil {
					t.Fatal(e)
				}
			case "delegation":
				a.Delegated = true
			case "expired":
				if _, e := p.Exec(ctx, "UPDATE admin_funds_grants SET created_at=clock_timestamp()-interval '2 minutes',expires_at=clock_timestamp()-interval '1 second'"); e != nil {
					t.Fatal(e)
				}
			}
			tx, _ := p.Begin(ctx)
			defer tx.Rollback(ctx)
			if e := f.ConsumeTx(ctx, tx, a, grant, action, "operation"); e == nil {
				t.Fatal("revoked grant consumed")
			}
		})
	}
}

func TestFundsPasswordPersistentFailuresAndAuditFailureRollback(t *testing.T) {
	p, f, a := fundsFixture(t)
	ctx := context.Background()
	if e := f.SetPassword(ctx, a, fundsLogin, "", fundsPassword, ""); e != nil {
		t.Fatal(e)
	}
	action := fundsAction(t, "limit")
	c, e := f.Prepare(ctx, a, action)
	if e != nil {
		t.Fatal(e)
	}
	for range 5 {
		if _, e = NewFundsAuthorization(p, nil).Authorize(ctx, a, c.ChallengeID, "wrong"); !errors.Is(e, ErrFundsAuthentication) {
			t.Fatal(e)
		}
	}
	if _, e = f.Authorize(ctx, a, c.ChallengeID, fundsPassword); !errors.Is(e, ErrFundsRateLimited) {
		t.Fatal("persistent failures bypassed", e)
	}
	var n int
	p.QueryRow(ctx, "SELECT count(*) FROM admin_funds_grants").Scan(&n)
	if n != 0 {
		t.Fatal("wrong password created grants")
	}
	// A fresh account's challenge cannot be issued if durable audit storage fails.
	if _, e = p.Exec(ctx, "CREATE FUNCTION reject_funds_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected audit failure'; END $$; CREATE TRIGGER fail_funds_audit BEFORE INSERT ON admin_funds_audit FOR EACH ROW EXECUTE FUNCTION reject_funds_audit()"); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Prepare(ctx, a, fundsAction(t, "audit-fails")); e == nil {
		t.Fatal("challenge without audit")
	}
	p.QueryRow(ctx, "SELECT count(*) FROM admin_funds_challenges").Scan(&n)
	if n != 1 {
		t.Fatal("audit failure leaked challenge", n)
	}
}
