package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"goepay/db"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

func TestScheduler171DisabledAndAmbiguousFlagsNeverConnect(t *testing.T) {
	t.Setenv("DATABASE_URL", "invalid:must-not-connect")
	t.Setenv("GOEPAY_MASTER_KEY", "invalid-must-not-read")
	for _, flag := range []string{"", "false"} {
		t.Setenv("GOEPAY_TASK_SCHEDULER_ENABLED", flag)
		if e := run(context.Background(), true); e != nil {
			t.Fatal("disabled runtime read database or credentials", e)
		}
	}
	for _, flag := range []string{"TRUE", "1", "yes", " false", "true "} {
		t.Setenv("GOEPAY_TASK_SCHEDULER_ENABLED", flag)
		if e := run(context.Background(), true); e == nil || !strings.Contains(e.Error(), "enable flag") {
			t.Fatal("ambiguous opt-in reached startup", flag, e)
		}
	}
}
func TestScheduler171CLIOnceActuallyConsumesOriginalPendingRun(t *testing.T) {
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close()
	schema := fmt.Sprintf("scheduler171_cli_%d", time.Now().UnixNano())
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	u, e := url.Parse(dsn)
	if e != nil {
		t.Fatal(e)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	pool, e := pgxpool.New(ctx, u.String())
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	if e = db.Migrate(ctx, pool); e != nil {
		t.Fatal(e)
	}
	key := make([]byte, 32)
	store, e := payment.NewWithKey(pool, key)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class)VALUES('scheduler171-cli','admin',171,'Synthetic CLI','unused-hash','super')`); e != nil {
		t.Fatal(e)
	}
	user := identity.User{Username: "scheduler171-cli", ID: 171, Role: "admin", AdminClass: "super", AuthVersion: 1}
	token, _, e := identity.NewPersistentSessions(pool, false).Create(user)
	if e != nil {
		t.Fatal(e)
	}
	actor := payment.MerchantCredentialActor{User: user, SessionToken: token}
	view, e := store.ScheduledTasks(ctx, actor, "referral_daily")
	if e != nil {
		t.Fatal(e)
	}
	saved, e := store.UpdateScheduledTask(ctx, actor, "referral_daily", payment.ScheduledTaskInput{Enabled: true, Schedule: payment.ScheduledTaskSchedule{Frequency: "once", Interval: 1, ExecuteAt: time.Now().UTC().Add(time.Hour).Truncate(time.Second)}, Params: payment.ScheduledTaskParams{"limit": 20}, ExpectedFingerprint: view[0].Fingerprint})
	if e != nil {
		t.Fatal(e)
	}
	pending, e := store.RequestScheduledTaskRun(ctx, actor, "referral_daily", saved.Fingerprint)
	if e != nil {
		t.Fatal(e)
	}
	t.Setenv("DATABASE_URL", u.String())
	t.Setenv("GOEPAY_MASTER_KEY", hex.EncodeToString(key))
	t.Setenv("GOEPAY_TASK_SCHEDULER_ENABLED", "true")
	if e = run(ctx, true); e != nil {
		t.Fatal("real CLI tick failed", e)
	}
	page, e := store.ScheduledTaskRuns(ctx, actor, "referral_daily", 0, 20)
	if e != nil || len(page.Items) != 1 || page.Items[0].ID != pending.ID || page.Items[0].Status != "success" || page.Items[0].StartedAt == nil || page.Items[0].CompletedAt == nil {
		t.Fatal("CLI did not execute persisted original intent", page, e)
	}
	if e = run(ctx, true); e != nil {
		t.Fatal(e)
	}
	var runs int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM scheduled_task_runs`).Scan(&runs); e != nil || runs != 1 {
		t.Fatal("CLI restart manufactured another manual or future once run", runs, e)
	}
}
