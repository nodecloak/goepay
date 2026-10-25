package businessnotice_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/db"
	"goepay/internal/businessnotice"
	"os"
	"testing"
)

func businessPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("real isolated PostgreSQL required")
	}
	ctx := context.Background()
	base, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	n := make([]byte, 8)
	if _, err = rand.Read(n); err != nil {
		t.Fatal(err)
	}
	schema := "business83_" + hex.EncodeToString(n)
	if _, err = base.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close(); base.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); base.Close() })
	if err = db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestAdminBusiness83SettingsKeepOriginalNamespaceAndExplicitEmptyDestination(t *testing.T) {
	p := businessPool(t)
	ctx := context.Background()
	if _, err := p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"msgconfig_domain","value":"2"}'),('settings','{"key":"msgconfig_risk","value":"0"}'),('settings','{"key":"mail_recv","value":""}'),('settings','{"key":"site_name","value":"Go别名"}'),('settings','{"key":"sitename","value":"原易支付"}'),('settings','{"key":"private_token","value":"must-not-read"}')`); err != nil {
		t.Fatal(err)
	}
	settings, err := businessnotice.ReadSettings(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if settings["msgconfig_domain"] != "2" || settings["mail_recv"] != "" || settings["sitename"] != "原易支付" {
		t.Fatal("original settings changed", settings)
	}
	if _, ok := settings["mail_recv"]; !ok {
		t.Fatal("saved empty was lost and could adopt a process fallback")
	}
	if _, ok := settings["private_token"]; ok {
		t.Fatal("read protected configuration through generic settings")
	}
	if _, ok := settings["msgconfig_risk"]; ok {
		t.Fatal("business configuration adopted risk namespace")
	}
	for _, raw := range []string{`3`, `"3"`, `null`, `false`, `" 1"`, `"00"`} {
		if _, err = p.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{value}',$1::jsonb) WHERE resource='settings' AND body->>'key'='msgconfig_domain'`, raw); err != nil {
			t.Fatal(err)
		}
		if _, err = businessnotice.ReadSettings(ctx, p); !errors.Is(err, businessnotice.ErrInvalid) {
			t.Fatal("invalid typed/switch setting was accepted", raw, err)
		}
	}
}

func TestAdminBusiness83SettingsRejectDuplicateImportedSources(t *testing.T) {
	p := businessPool(t)
	ctx := context.Background()
	if _, err := p.Exec(ctx, `DROP INDEX backoffice_setting_unique`); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"msgconfig_apply","value":"1"}'),('settings','{"key":"msgconfig_apply","value":"2"}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := businessnotice.ReadSettings(ctx, p); !errors.Is(err, businessnotice.ErrInvalid) {
		t.Fatal("duplicate source switch picked an arbitrary branch", err)
	}
}
