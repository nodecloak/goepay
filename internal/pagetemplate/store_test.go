package pagetemplate

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/db"
	"os"
	"sync"
	"testing"
)

var localAdmin = Actor{Role: "admin", Class: "super", ID: 1, Name: "local-template-admin"}

func fixture(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	base, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	var nonce [8]byte
	if _, e = rand.Read(nonce[:]); e != nil {
		t.Fatal(e)
	}
	schema := "templates_" + hex.EncodeToString(nonce[:])
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, e = base.Exec(ctx, "CREATE SCHEMA "+quoted); e != nil {
		t.Fatal(e)
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	cfg.MaxConns = 4
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { pool.Close(); base.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE"); base.Close() })
	if e = db.Migrate(ctx, pool); e != nil {
		t.Fatal(e)
	}
	return New(pool)
}
func TestPostgresTypedCASPersistsDraftsAndConcurrentSelection(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	initial, e := s.List(ctx, "pay", localAdmin)
	if e != nil || initial.Revision != 0 || initial.Selected != "default" {
		t.Fatal(initial, e)
	}
	saved, e := s.SaveConfig(ctx, "pay", "default", 0, configJSON(t, "pay", func(m map[string]any) { m["brand_name"] = "Persisted Brand" }), localAdmin)
	if e != nil || saved.Revision != 1 {
		t.Fatal(saved, e)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Select(ctx, "pay", "compact", 1, localAdmin)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				wins++
			} else if !errors.Is(err, ErrConflict) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatal("CAS had multiple winners", wins)
	}
	current, e := s.Resolve(ctx, "pay")
	if e != nil || current.Name != "compact" || current.Revision != 2 {
		t.Fatal(current, e)
	}
	if _, e = s.Select(ctx, "pay", "default", 2, localAdmin); e != nil {
		t.Fatal(e)
	}
	current, e = s.Resolve(ctx, "pay")
	if e != nil || current.Config.BrandName != "Persisted Brand" {
		t.Fatal("switch lost saved template draft", current, e)
	}
	if _, e = s.SaveConfig(ctx, "pay", "default", 3, configJSON(t, "pay", func(m map[string]any) { m["logo_asset_id"] = hex.EncodeToString(make([]byte, 32)) }), localAdmin); !errors.Is(e, ErrInvalid) {
		t.Fatal("missing asset accepted", e)
	}
	for _, actor := range []Actor{{Role: "merchant", ID: 1, Name: "m"}, {Role: "admin", Class: "operator", ID: 1, Name: "op"}} {
		if _, e = s.List(ctx, "pay", actor); !errors.Is(e, ErrForbidden) {
			t.Fatal("unauthorized registry access", e)
		}
	}
	if _, e = s.pool.Exec(ctx, `UPDATE page_template_configs SET config='{"accent_color":"javascript:evil"}' WHERE kind='pay' AND name='default'`); e != nil {
		t.Fatal(e)
	}
	current, e = s.Resolve(ctx, "pay")
	if e != nil || current.Fallback != "invalid_config" || current.Config.BrandName != "GOEPAY" {
		t.Fatal("unsafe configuration did not visibly fall back", current, e)
	}
}
func TestPostgresAssetPrivateUntilSelectedReferenceAndRevokedAfterSwitch(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	a, e := s.Upload(ctx, imageBytes(t, "png"), localAdmin)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Asset(ctx, a.ID, localAdmin); e != nil {
		t.Fatal(e)
	}
	if _, e = s.PublishedAsset(ctx, a.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("draft asset publicly readable", e)
	}
	if _, e = s.SaveConfig(ctx, "pay", "compact", 0, configJSON(t, "pay", func(m map[string]any) { m["logo_asset_id"] = a.ID }), localAdmin); e != nil {
		t.Fatal(e)
	}
	if _, e = s.PublishedAsset(ctx, a.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("unselected template asset public", e)
	}
	if _, e = s.Select(ctx, "pay", "compact", 1, localAdmin); e != nil {
		t.Fatal(e)
	}
	if got, e := s.PublishedAsset(ctx, a.ID); e != nil || got.ID != a.ID || len(got.Bytes) == 0 {
		t.Fatal("selected image unavailable", got, e)
	}
	if _, e = s.Select(ctx, "pay", "default", 2, localAdmin); e != nil {
		t.Fatal(e)
	}
	if _, e = s.PublishedAsset(ctx, a.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("unreferenced asset remained public", e)
	}
}
func TestPostgresAuditFailureRollsBackConfigAndRevision(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	if _, e := s.pool.Exec(ctx, `CREATE FUNCTION fail_template_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='page-templates' THEN RAISE EXCEPTION 'local audit failure'; END IF; RETURN NEW; END $$;CREATE TRIGGER local_template_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION fail_template_audit()`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.SaveConfig(ctx, "pay", "default", 0, configJSON(t, "pay", nil), localAdmin); e == nil {
		t.Fatal("save ignored missing audit")
	}
	v, e := s.List(ctx, "pay", localAdmin)
	if e != nil || v.Revision != 0 {
		t.Fatal("audit failure advanced head", v, e)
	}
	var rows int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM page_template_configs").Scan(&rows); e != nil || rows != 0 {
		t.Fatal("audit failure persisted draft", rows, e)
	}
}
