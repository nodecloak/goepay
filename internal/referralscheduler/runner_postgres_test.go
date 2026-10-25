package referralscheduler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"goepay/db"
	"goepay/internal/payment"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRunnerPostgresClockAndEmptyQueueDoNotCreateMoneyOrJobs(t *testing.T) {
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	nonce := make([]byte, 12)
	if _, err = rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	schema := "referral_scheduler_" + hex.EncodeToString(nonce)
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, err := admin.Exec(cleanup, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	scoped, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer scoped.Close()
	if err = db.Migrate(ctx, scoped); err != nil {
		t.Fatal(err)
	}
	store, err := payment.NewWithKey(scoped, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	result, err := (Runner{Enabled: true, Clock: scoped, Store: store, Limit: 20, Interval: time.Minute}).RunOnce(ctx)
	if err != nil || result.Examined != 0 || result.Awarded != 0 || result.Skipped != 0 || len(result.OrderIDs) != 0 {
		t.Fatal("empty real queue changed work outcome", result, err)
	}
	for _, table := range []string{"orders", "journals", "ledger_entries", "referral_daily_terms", "referral_daily_liabilities", "referral_rewards", "jobs"} {
		var count int
		if err = scoped.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{table}.Sanitize()).Scan(&count); err != nil || count != 0 {
			t.Fatal("empty scheduled run created business state", table, count, err)
		}
	}
}
