package migration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/identity"
)

type wizardFixture234 struct {
	p        *pgxpool.Pool
	w        *Wizard234
	a        WizardActor234
	sessions *identity.Sessions
}

func wizardFixtureFor234(t *testing.T) wizardFixture234 {
	t.Helper()
	p := migrationPool(t)
	if _, e := p.Exec(t.Context(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) VALUES('wizard234','admin',234,'Synthetic migration operator','not-used-in-session-check','super')`); e != nil {
		t.Fatal(e)
	}
	u := identity.User{Username: "wizard234", Role: "admin", ID: 234, AuthVersion: 1, AdminClass: "super"}
	s := identity.NewPersistentSessions(p, false)
	token, _, e := s.Create(u)
	if e != nil {
		t.Fatal(e)
	}
	w, e := NewWizard234(p, bytes.Repeat([]byte{6}, 32))
	if e != nil {
		t.Fatal(e)
	}
	return wizardFixture234{p, w, WizardActor234{User: u, Token: token}, s}
}
func (f wizardFixture234) prepare(t *testing.T, raw string) WizardView234 {
	t.Helper()
	v, e := f.w.Prepare(t.Context(), f.a, []byte(raw), "epay_pro", "")
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func wizardApplyInput234(v WizardView234) ApplyInput234 {
	return ApplyInput234{v.SHA256, true, true, "MIGRATE"}
}
func wizardAssertNoBusiness234(t *testing.T, p *pgxpool.Pool) {
	t.Helper()
	var count int
	if e := p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM merchants)+(SELECT count(*) FROM orders)+(SELECT count(*) FROM journals)+(SELECT count(*) FROM ledger_entries)+(SELECT count(*) FROM jobs)+(SELECT count(*) FROM legacy_import_runs)+(SELECT count(*) FROM accounts WHERE owner_id<>0 OR balance<>0)`).Scan(&count); e != nil || count != 0 {
		t.Fatal("business rollback failed", count, e)
	}
}

func TestWizard234EncryptedArchiveAtomicImportAndReplay(t *testing.T) {
	f := wizardFixtureFor234(t)
	c, e := f.w.Capabilities(t.Context(), f.a)
	if e != nil || !c.TargetReady || !c.Enabled || len(c.Runs) != 0 {
		t.Fatal("fresh capabilities", c, e)
	}
	raw := dumpFixture234 + "CREATE TABLE pre_custom(secret text);INSERT INTO pre_custom VALUES('synthetic-private-secret-234');"
	v := f.prepare(t, raw)
	if v.Status != "prepared" || v.Merchants != 1 || v.Orders != 1 || v.AvailableTotal != "12.34" || !v.TargetReady || v.Report != nil {
		t.Fatal("preview", v)
	}
	var archive, snapshotCipher, metadata []byte
	if e = f.p.QueryRow(t.Context(), `SELECT archive_cipher,snapshot_cipher,payload_cipher FROM migration_wizard_runs234 WHERE id=$1`, v.ID).Scan(&archive, &snapshotCipher, &metadata); e != nil {
		t.Fatal(e)
	}
	for _, b := range [][]byte{archive, snapshotCipher, metadata, mustJSON234(t, v)} {
		if bytes.Contains(b, []byte("synthetic-private-secret-234")) || bytes.Contains(b, []byte("Synthetic merchant")) {
			t.Fatal("plaintext source leaked")
		}
	}
	tx, e := f.p.Begin(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	r, p, e := f.w.read(t.Context(), tx, v.ID, false)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.w.inputs(t.Context(), tx, &r, &p); e != nil {
		t.Fatal(e)
	}
	if e = f.w.inputs(t.Context(), tx, &r, &p); e != nil || !bytes.Equal(p.Raw, []byte(raw)) {
		t.Fatal("complete original backup not preserved", e)
	}
	tx.Rollback(t.Context())
	done, e := f.w.Apply(t.Context(), f.a, v.ID, wizardApplyInput234(v))
	if e != nil || done.Status != "completed" || done.Progress != 100 || done.Report == nil || !done.Report.Applied {
		t.Fatal("actual import", done, e)
	}
	replay, e := f.w.Apply(t.Context(), f.a, v.ID, wizardApplyInput234(v))
	if e != nil || replay.Status != "completed" {
		t.Fatal("replay", e)
	}
	var available, clearing int64
	var merchants, orders, journals, jobs int
	e = f.p.QueryRow(t.Context(), `SELECT (SELECT balance FROM accounts WHERE owner_id=9001 AND kind='available'),(SELECT balance FROM accounts WHERE owner_id=0 AND kind='clearing'),(SELECT count(*) FROM merchants),(SELECT count(*) FROM orders),(SELECT count(*) FROM journals),(SELECT count(*) FROM jobs)+(SELECT count(*) FROM outbox)+(SELECT count(*) FROM refunds)`).Scan(&available, &clearing, &merchants, &orders, &journals, &jobs)
	if e != nil || available != 1234 || clearing != -1234 || merchants != 1 || orders != 1 || journals != 1 || jobs != 0 {
		t.Fatal("duplicate/side-effect/ledger", available, clearing, merchants, orders, journals, jobs, e)
	}
	var enabled bool
	if e = f.p.QueryRow(t.Context(), `SELECT enabled FROM merchants WHERE id=9001`).Scan(&enabled); e != nil || enabled {
		t.Fatal("merchant activated", e)
	}
	c, e = f.w.Capabilities(t.Context(), f.a)
	if e != nil || c.TargetReady || len(c.Runs) != 1 || c.Runs[0].Status != "completed" {
		t.Fatal("post-import state", c, e)
	}
}

func TestWizard234BlockedPrecheckAndOwnerBoundary(t *testing.T) {
	f := wizardFixtureFor234(t)
	bad := f.prepare(t, "INSERT INTO pre_user VALUES('private-invalid-literal');")
	if bad.Status != "blocked" || len(bad.Issues) == 0 || strings.Contains(string(mustJSON234(t, bad)), "private-invalid-literal") {
		t.Fatal("unsafe failed precheck", bad)
	}
	if _, e := f.w.Apply(t.Context(), f.a, bad.ID, wizardApplyInput234(bad)); !errors.Is(e, ErrWizardConflict234) {
		t.Fatal("blocked apply", e)
	}
	v := f.prepare(t, dumpFixture234)
	delegated := f.a
	delegated.Delegated = true
	if _, e := f.w.Status(t.Context(), delegated, v.ID); !errors.Is(e, identity.ErrAdminSession) {
		t.Fatal("delegated read", e)
	}
	if _, e := f.p.Exec(t.Context(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) VALUES('other234','admin',235,'Other synthetic operator','unused','super')`); e != nil {
		t.Fatal(e)
	}
	u := identity.User{Username: "other234", Role: "admin", ID: 235, AuthVersion: 1, AdminClass: "super"}
	token, _, e := f.sessions.Create(u)
	if e != nil {
		t.Fatal(e)
	}
	other := WizardActor234{User: u, Token: token}
	if _, e = f.w.Status(t.Context(), other, v.ID); !errors.Is(e, identity.ErrAdminSession) {
		t.Fatal("cross-owner read", e)
	}
	if _, e = f.w.Apply(t.Context(), other, v.ID, wizardApplyInput234(v)); !errors.Is(e, identity.ErrAdminSession) {
		t.Fatal("cross-owner apply", e)
	}
	cap, e := f.w.Capabilities(t.Context(), other)
	if e != nil || len(cap.Runs) != 0 {
		t.Fatal("cross-owner list", cap, e)
	}
	wizardAssertNoBusiness234(t, f.p)
}

func TestWizard234ExpiredAdmittedClaimCannotImport(t *testing.T) {
	f := wizardFixtureFor234(t)
	v := f.prepare(t, dumpFixture234)
	tx, e := f.p.Begin(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(t.Context())
	r, p, e := f.w.read(t.Context(), tx, v.ID, false)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.w.inputs(t.Context(), tx, &r, &p); e != nil {
		t.Fatal(e)
	}
	r.ID = "23423423423423423423423423423424"
	r.Status = "applying"
	r.Created = time.Now().UTC().Add(-25 * time.Hour).Truncate(time.Microsecond)
	r.Expires = r.Created.Add(24 * time.Hour)
	p.View.ID = r.ID
	p.View.Status = r.Status
	p.View.ExpiresAt = r.Expires
	p.View.Progress = 10
	if e = f.w.save(t.Context(), tx, &r, p, true); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(t.Context()); e != nil {
		t.Fatal(e)
	}
	if _, e = f.w.execute(t.Context(), f.a, r); !errors.Is(e, ErrWizardConflict234) {
		t.Fatal("expired admitted claim imported", e)
	}
	wizardAssertNoBusiness234(t, f.p)
}

func TestWizard234PreparationQuotaProtectsEncryptedStaging(t *testing.T) {
	f := wizardFixtureFor234(t)
	for range 20 {
		v := f.prepare(t, "invalid SQL backup")
		if v.Status != "blocked" {
			t.Fatal("invalid source not blocked")
		}
	}
	if _, e := f.w.Prepare(t.Context(), f.a, []byte(dumpFixture234), "epay_pro", ""); !errors.Is(e, ErrWizardCapacity234) {
		t.Fatal("staging row quota bypass", e)
	}
	var count int
	if e := f.p.QueryRow(t.Context(), `SELECT count(*) FROM migration_wizard_runs234`).Scan(&count); e != nil || count != 20 {
		t.Fatal("quota created extra archived source", count, e)
	}
	c, e := f.w.Capabilities(t.Context(), f.a)
	if e != nil || len(c.Runs) != 20 || !c.TargetReady {
		t.Fatal("quota hid operator history or created business", c, e)
	}
	wizardAssertNoBusiness234(t, f.p)
}

func TestWizard234OrphanMerchantIdentityBlocksFreshTarget(t *testing.T) {
	f := wizardFixtureFor234(t)
	if _, e := f.p.Exec(t.Context(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES('orphan234','merchant',9001,'Unrelated old identity','unused')`); e != nil {
		t.Fatal(e)
	}
	c, e := f.w.Capabilities(t.Context(), f.a)
	if e != nil || c.TargetReady {
		t.Fatal("orphan merchant account accepted", c, e)
	}
	v := f.prepare(t, dumpFixture234)
	if v.Status != "blocked" || v.TargetReady {
		t.Fatal("orphan identity did not block preparation", v)
	}
	wizardAssertNoBusiness234(t, f.p)
}

func TestWizard234ConcurrentIdentityWriterRefusesWithoutDeadlock(t *testing.T) {
	f := wizardFixtureFor234(t)
	v := f.prepare(t, dumpFixture234)
	block, e := f.p.Begin(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	defer block.Rollback(t.Context())
	if _, e = block.Exec(t.Context(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES('racing234','merchant',9001,'Racing synthetic identity','unused')`); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if _, e = f.w.Apply(ctx, f.a, v.ID, wizardApplyInput234(v)); !errors.Is(e, ErrWizardBusy234) {
		t.Fatal("identity write not fenced by bounded refusal", e)
	}
	if e = block.Commit(t.Context()); e != nil {
		t.Fatal(e)
	}
	s, e := f.w.Status(t.Context(), f.a, v.ID)
	if e != nil || s.Status != "failed" || len(s.Issues) == 0 || s.Issues[len(s.Issues)-1].Code != "target_busy" {
		t.Fatal("busy outcome not recoverable", s, e)
	}
	wizardAssertNoBusiness234(t, f.p)
}

func TestWizard234ConcurrentBusinessReaderRefusesWithoutDeadlock(t *testing.T) {
	f := wizardFixtureFor234(t)
	v := f.prepare(t, dumpFixture234)
	block, e := f.p.Begin(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	defer block.Rollback(t.Context())
	if _, e = block.Exec(t.Context(), `SELECT id FROM merchants LIMIT 1`); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if _, e = f.w.Apply(ctx, f.a, v.ID, wizardApplyInput234(v)); !errors.Is(e, ErrWizardBusy234) {
		t.Fatal("business reader deadlock/unsafe overlap", e)
	}
	if e = block.Commit(t.Context()); e != nil {
		t.Fatal(e)
	}
	wizardAssertNoBusiness234(t, f.p)
}

func TestWizard234ProgressRemainsReadableDuringAtomicImport(t *testing.T) {
	f := wizardFixtureFor234(t)
	v := f.prepare(t, dumpFixture234)
	if _, e := f.p.Exec(t.Context(), `CREATE FUNCTION pause_wizard234() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(74973236); RETURN NEW; END $$;CREATE TRIGGER pause_wizard234 BEFORE INSERT ON orders FOR EACH ROW EXECUTE FUNCTION pause_wizard234()`); e != nil {
		t.Fatal(e)
	}
	block, e := f.p.Begin(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	defer block.Rollback(t.Context())
	if _, e = block.Exec(t.Context(), "SELECT pg_advisory_xact_lock(74973236)"); e != nil {
		t.Fatal(e)
	}
	result := make(chan error, 1)
	go func() { _, e := f.w.Apply(t.Context(), f.a, v.ID, wizardApplyInput234(v)); result <- e }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		if e = f.p.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND classid=0 AND objid=74973236 AND NOT granted)`).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("import did not reach protected atomic business write")
		}
		time.Sleep(10 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	status, e := f.w.Status(ctx, f.a, v.ID)
	cancel()
	if e != nil || status.Status != "applying" || status.Progress != 10 {
		t.Fatal("status blocked behind import", status, e)
	}
	ctx, cancel = context.WithTimeout(t.Context(), time.Second)
	cap, e := f.w.Capabilities(ctx, f.a)
	cancel()
	if e != nil || cap.TargetReady || len(cap.Runs) != 1 || cap.Runs[0].Status != "applying" || len(cap.Issues) != 1 || cap.Issues[0].Code != "target_busy" {
		t.Fatal("capabilities blocked behind import", cap, e)
	}
	if e = block.Commit(t.Context()); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-result:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("import did not complete after release")
	}
	status, e = f.w.Status(t.Context(), f.a, v.ID)
	if e != nil || status.Status != "completed" {
		t.Fatal("completion missing", status, e)
	}
}

func TestWizard234RejectsNonemptyTargetAndLateBusinessChange(t *testing.T) {
	f := wizardFixtureFor234(t)
	v := f.prepare(t, dumpFixture234)
	if _, e := f.p.Exec(t.Context(), `INSERT INTO merchants(id,name,api_key,enabled) VALUES(77,'Existing synthetic merchant','\x00',false)`); e != nil {
		t.Fatal(e)
	}
	if _, e := f.w.Apply(t.Context(), f.a, v.ID, wizardApplyInput234(v)); !errors.Is(e, ErrWizardTarget234) {
		t.Fatal("late target race accepted", e)
	}
	status, e := f.w.Status(t.Context(), f.a, v.ID)
	if e != nil || status.Status != "failed" {
		t.Fatal("late target status", status, e)
	}
	v = f.prepare(t, dumpFixture234)
	if v.Status != "blocked" || v.TargetReady {
		t.Fatal("nonempty target precheck", v)
	}
	var count int
	if e = f.p.QueryRow(t.Context(), `SELECT count(*) FROM merchants WHERE id=77`).Scan(&count); e != nil || count != 1 {
		t.Fatal("existing merchant changed", count, e)
	}
	if e = f.p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM journals)+(SELECT count(*) FROM legacy_import_runs)`).Scan(&count); e != nil || count != 0 {
		t.Fatal("late race created funds", count, e)
	}
}

func TestWizard234LateImportFailureRollsBackBusinessAndCommitsFailure(t *testing.T) {
	f := wizardFixtureFor234(t)
	v := f.prepare(t, dumpFixture234)
	if _, e := f.p.Exec(t.Context(), `CREATE FUNCTION fail_wizard_order234() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic late import refusal'; END $$;CREATE TRIGGER fail_wizard_order234 BEFORE INSERT ON orders FOR EACH ROW EXECUTE FUNCTION fail_wizard_order234()`); e != nil {
		t.Fatal(e)
	}
	if _, e := f.w.Apply(t.Context(), f.a, v.ID, wizardApplyInput234(v)); e == nil {
		t.Fatal("late error accepted")
	}
	wizardAssertNoBusiness234(t, f.p)
	status, e := f.w.Status(t.Context(), f.a, v.ID)
	if e != nil || status.Status != "failed" || len(status.Issues) == 0 {
		t.Fatal("no durable failure", status, e)
	}
	if _, e = f.w.Apply(t.Context(), f.a, v.ID, wizardApplyInput234(v)); !errors.Is(e, ErrWizardConflict234) {
		t.Fatal("failed operation automatically reusable", e)
	}
}

func TestWizard234ArchiveTamperAndEarlierValidRowFailClosed(t *testing.T) {
	f := wizardFixtureFor234(t)
	v := f.prepare(t, dumpFixture234)
	if _, e := f.p.Exec(t.Context(), `UPDATE migration_wizard_runs234 SET archive_cipher=decode('00','hex') WHERE id=$1`, v.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := f.w.Apply(t.Context(), f.a, v.ID, wizardApplyInput234(v)); !errors.Is(e, ErrWizardConflict234) {
		t.Fatal("archive tamper accepted", e)
	}
	wizardAssertNoBusiness234(t, f.p)
	v = f.prepare(t, dumpFixture234)
	var cipher, proof []byte
	if e := f.p.QueryRow(t.Context(), `SELECT payload_cipher,proof_hash FROM migration_wizard_runs234 WHERE id=$1`, v.ID).Scan(&cipher, &proof); e != nil {
		t.Fatal(e)
	}
	if _, e := f.w.Apply(t.Context(), f.a, v.ID, wizardApplyInput234(v)); e != nil {
		t.Fatal(e)
	}
	if _, e := f.p.Exec(t.Context(), `UPDATE migration_wizard_runs234 SET status='prepared',revision=1,payload_cipher=$2,proof_hash=$3 WHERE id=$1`, v.ID, cipher, proof); e != nil {
		t.Fatal(e)
	}
	if _, e := f.w.Status(t.Context(), f.a, v.ID); !errors.Is(e, ErrWizardConflict234) {
		t.Fatal("whole valid earlier row rollback accepted", e)
	}
	if _, e := f.p.Exec(t.Context(), `UPDATE migration_wizard_events234 SET status='prepared' WHERE run_id=$1`, v.ID); e == nil {
		t.Fatal("event evidence mutable")
	}
}

func TestWizard234ConcurrentApplyCreatesOneOpening(t *testing.T) {
	f := wizardFixtureFor234(t)
	v := f.prepare(t, dumpFixture234)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := f.w.Apply(t.Context(), f.a, v.ID, wizardApplyInput234(v)); errs <- e }()
	}
	wg.Wait()
	close(errs)
	passed := 0
	for e := range errs {
		if e == nil {
			passed++
		} else if !errors.Is(e, ErrWizardConflict234) {
			t.Fatal(e)
		}
	}
	if passed < 1 {
		t.Fatal("no successful caller")
	}
	var balance int64
	var count int
	if e := f.p.QueryRow(t.Context(), `SELECT (SELECT balance FROM accounts WHERE owner_id=9001 AND kind='available'),(SELECT count(*) FROM journals)`).Scan(&balance, &count); e != nil || balance != 1234 || count != 1 {
		t.Fatal("duplicate opening", balance, count, e)
	}
}

func TestWizard234AuthorizationRecheckedAfterGlobalLockWait(t *testing.T) {
	f := wizardFixtureFor234(t)
	v := f.prepare(t, dumpFixture234)
	block, e := f.p.Begin(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	defer block.Rollback(t.Context())
	if _, e = block.Exec(t.Context(), "SELECT pg_advisory_xact_lock(74973232)"); e != nil {
		t.Fatal(e)
	}
	result := make(chan error, 1)
	go func() { _, e := f.w.Apply(t.Context(), f.a, v.ID, wizardApplyInput234(v)); result <- e }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var state string
		if e = f.p.QueryRow(t.Context(), `SELECT status FROM migration_wizard_runs234 WHERE id=$1`, v.ID).Scan(&state); e != nil {
			t.Fatal(e)
		}
		if state == "applying" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("claim did not become durable")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, e = f.p.Exec(t.Context(), `UPDATE console_sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE username='wizard234'`); e != nil {
		t.Fatal(e)
	}
	if e = block.Commit(t.Context()); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-result:
		if !errors.Is(e, identity.ErrAdminSession) {
			t.Fatal("expired actor imported after waiting", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("apply blocked")
	}
	wizardAssertNoBusiness234(t, f.p)
	token, _, e := f.sessions.Create(f.a.User)
	if e != nil {
		t.Fatal(e)
	}
	f.a.Token = token
	status, e := f.w.Status(t.Context(), f.a, v.ID)
	if e != nil || status.Status != "failed" {
		t.Fatal("same owner relogin could not recover result", status, e)
	}
}

func TestWizard234ExpiredPreparationAndConfirmationCannotRun(t *testing.T) {
	f := wizardFixtureFor234(t)
	v := f.prepare(t, dumpFixture234)
	for _, in := range []ApplyInput234{{v.SHA256, false, true, "MIGRATE"}, {v.SHA256, true, false, "MIGRATE"}, {v.SHA256, true, true, "migrate"}, {strings.Repeat("0", 64), true, true, "MIGRATE"}} {
		if _, e := f.w.Apply(t.Context(), f.a, v.ID, in); e == nil {
			t.Fatal("missing confirmation/digest accepted")
		}
	}
	tx, e := f.p.Begin(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(t.Context())
	r, p, e := f.w.read(t.Context(), tx, v.ID, false)
	if e != nil {
		t.Fatal(e)
	}
	r.ID = "23423423423423423423423423423423"
	r.Created = time.Now().UTC().Add(-25 * time.Hour).Truncate(time.Microsecond)
	r.Expires = r.Created.Add(24 * time.Hour)
	p.View.ID = r.ID
	p.View.ExpiresAt = r.Expires
	if e = f.w.save(t.Context(), tx, &r, p, true); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(t.Context()); e != nil {
		t.Fatal(e)
	}
	if _, e = f.w.Apply(t.Context(), f.a, r.ID, wizardApplyInput234(p.View)); !errors.Is(e, ErrWizardConflict234) {
		t.Fatal("expired preparation imported", e)
	}
	s, e := f.w.Status(context.Background(), f.a, r.ID)
	if e != nil || len(s.Issues) == 0 || s.Issues[len(s.Issues)-1].Code != "staging_expired" {
		b, _ := json.Marshal(s)
		t.Fatal("expiry missing", string(b), e)
	}
	wizardAssertNoBusiness234(t, f.p)
}
