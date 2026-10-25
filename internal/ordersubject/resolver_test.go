package ordersubject

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/db"
	"goepay/internal/payment"
)

func subjectFixture(t *testing.T) (*pgxpool.Pool, *payment.Store) {
	t.Helper()
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	b := make([]byte, 8)
	rand.Read(b)
	schema := "subject_" + hex.EncodeToString(b)
	if _, e = pool.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	p, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { p.Close(); pool.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); pool.Close() })
	if e = db.Migrate(ctx, p); e != nil {
		t.Fatal(e)
	}
	s, e := payment.NewWithKey(p, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.CreateMerchant(ctx, payment.Merchant{ID: 101, Name: "test", APIKey: "temporary-only"}); e != nil {
		t.Fatal(e)
	}
	return p, s
}
func putPolicy(t *testing.T, p *pgxpool.Pool, scope, id, template, rules string) int64 {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"scope": scope, "scope_id": id, "template": template, "rules": rules, "enabled": true})
	var n int64
	if e := p.QueryRow(context.Background(), "INSERT INTO backoffice_records(resource,body) VALUES('order-subject-policies',$1::jsonb) RETURNING id", string(body)).Scan(&n); e != nil {
		t.Fatal(e)
	}
	return n
}
func newSubjectOrder(t *testing.T, s *payment.Store, no, name string, amount int64) payment.Order {
	t.Helper()
	o, e := s.CreateOrder(context.Background(), payment.CreateOrderInput{MerchantID: 101, OutTradeNo: no, Name: name, Amount: amount, Type: "wxpay"})
	if e != nil {
		t.Fatal(e)
	}
	return o
}

func TestSubjectSnapshotConcurrencyFallbackAndConfigChanges(t *testing.T) {
	p, s := subjectFixture(t)
	ctx := context.Background()
	r := New(p)
	putPolicy(t, p, "global", "0", "global-[name]", `[{"name":"small-[outorder]","min":"0","max":"1.00"}]`)
	putPolicy(t, p, "merchant", "101", "merchant-[order]", "")
	cid := putPolicy(t, p, "channel", "7", "", `[]`)
	o := newSubjectOrder(t, s, "repeat", "raw", 100)
	errs := make(chan error, 12)
	subjects := make(chan string, 12)
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() { defer wg.Done(); x, e := r.Resolve(ctx, o, 7); subjects <- x; errs <- e }()
	}
	wg.Wait()
	close(errs)
	close(subjects)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	for v := range subjects {
		if v != "merchant-"+o.TradeNo {
			t.Fatal(v)
		}
	}
	var n int
	if e := p.QueryRow(ctx, "SELECT count(*) FROM provider_subject_snapshots WHERE order_id=$1", o.ID).Scan(&n); e != nil || n != 1 {
		t.Fatal(n, e)
	}
	if _, e := p.Exec(ctx, "UPDATE backoffice_records SET body=jsonb_set(body,'{template}','\"changed\"') WHERE id=$1", cid); e != nil {
		t.Fatal(e)
	}
	got, e := r.Resolve(ctx, o, 7)
	if e != nil || got != "merchant-"+o.TradeNo {
		t.Fatal(got, e)
	}
	if _, e = r.Resolve(ctx, o, 8); !errors.Is(e, payment.ErrConflict) {
		t.Fatalf("changed channel %v", e)
	}
	o2 := newSubjectOrder(t, s, "new", "raw", 100)
	got, e = r.Resolve(ctx, o2, 8)
	if e != nil || got != "small-new" {
		t.Fatal(got, e)
	}
	if _, e = p.Exec(ctx, "UPDATE provider_subject_snapshots SET subject='mutated' WHERE order_id=$1", o.ID); e == nil {
		t.Fatal("mutable snapshot")
	}
	if _, e = p.Exec(ctx, "DELETE FROM provider_subject_snapshots WHERE order_id=$1", o.ID); e == nil {
		t.Fatal("deletable snapshot")
	}
	saved, e := s.OrderByTradeNo(ctx, o.TradeNo)
	if e != nil || saved.Name != "raw" {
		t.Fatal("merchant name changed", saved, e)
	}
}

func TestSubjectNoConfigurationAndHistoricalAttemptPreserveOriginal(t *testing.T) {
	p, s := subjectFixture(t)
	r := New(p)
	ctx := context.Background()
	raw := strings.Repeat("界", 60) + "[time]"
	o := newSubjectOrder(t, s, "unconfigured", raw, 100)
	got, e := r.Resolve(ctx, o, 7)
	if e != nil || got != raw {
		t.Fatal("default compatibility", got, e)
	}
	old := newSubjectOrder(t, s, "historical", raw, 100)
	if _, _, e = s.BeginProviderAttempt(ctx, old.TradeNo, 7, "native"); e != nil {
		t.Fatal(e)
	}
	putPolicy(t, p, "global", "0", "new-title", "")
	for _, order := range []payment.Order{o, old} {
		got, e = r.Resolve(ctx, order, 7)
		if e != nil || got != raw {
			t.Fatal("old title reinterpreted", got, e)
		}
	}
	var n int
	p.QueryRow(ctx, "SELECT count(*) FROM provider_subject_snapshots WHERE order_id=$1", old.ID).Scan(&n)
	if n != 0 {
		t.Fatal("backfilled historical unknown request")
	}
}

func TestSubjectConfiguredInvalidAndTrustedContactVariables(t *testing.T) {
	p, s := subjectFixture(t)
	r := New(p)
	ctx := context.Background()
	putPolicy(t, p, "global", "0", "[qq]-[phone]-[name]", "")
	if _, e := p.Exec(ctx, `UPDATE merchants SET profile='{"qq":"123","phone":"456"}'::jsonb WHERE id=101`); e != nil {
		t.Fatal(e)
	}
	o := newSubjectOrder(t, s, "contact", "name", 100)
	got, e := r.Resolve(ctx, o, 7)
	if e != nil || got != "123-456-name" {
		t.Fatal(got, e)
	}
	bad := newSubjectOrder(t, s, "too-long", strings.Repeat("界", 43), 100)
	if _, e = r.Resolve(ctx, bad, 7); !errors.Is(e, payment.ErrInvalid) {
		t.Fatal("invalid output", e)
	}
	var n int
	p.QueryRow(ctx, "SELECT count(*) FROM provider_subject_snapshots WHERE order_id=$1", bad.ID).Scan(&n)
	if n != 0 {
		t.Fatal("invalid snapshot saved")
	}
}
