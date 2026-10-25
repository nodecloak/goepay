package payment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func membership72HistoricalStore(t *testing.T, last int) *Store {
	t.Helper()
	ctx := context.Background()
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL required")
	}
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	nonce := make([]byte, 8)
	if _, e = rand.Read(nonce); e != nil {
		t.Fatal(e)
	}
	schema := "member72_" + hex.EncodeToString(nonce)
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	cfg.MaxConns = 6
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
	})
	paths, e := filepath.Glob("../../db/migrations/*.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	for _, path := range paths {
		version, e := strconv.Atoi(filepath.Base(path)[:6])
		if e != nil {
			t.Fatal(e)
		}
		if version > last {
			continue
		}
		raw, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, string(raw)); e != nil {
			t.Fatal(filepath.Base(path), e)
		}
	}
	s, e := NewWithKey(pool, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	return s
}

func TestMembershipReferral72UpgradeCapturesOnlyActualHistoricalPurchasesAndNeverBackfills(t *testing.T) {
	s := membership72HistoricalStore(t, 71)
	ctx := context.Background()
	promoter, buyer := int64(7220), int64(7221)
	referralBoundMerchants(t, s, promoter, buyer)
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: buyer, OutTradeNo: "member72-legacy-credit", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, o.TradeNo, "member72-legacy-paid"); e != nil {
		t.Fatal(e)
	}
	group := membership72Group(t, s, 300)
	var id int64
	// Historical schema71 SQL, not the current PurchaseMembership API. There
	// was no membership reward source, version field or isolated income then.
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	if e = tx.QueryRow(ctx, `INSERT INTO membership_purchases(merchant_id,group_id,price,group_name,external_no,duration_days,expires_at,actor) VALUES($1,$2,300,'historical','member72-original-before-upgrade',30,now()+interval '30 days','merchant:7221') RETURNING id`, buyer, group).Scan(&id); e != nil {
		t.Fatal(e)
	}
	if e = post(ctx, tx, fmt.Sprintf("membership:%d", id), "membership purchase", buyer, "available", 0, "clearing", 300); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "INSERT INTO memberships(merchant_id,group_id,expires_at) SELECT merchant_id,group_id,expires_at FROM membership_purchases WHERE id=$1", id); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile("../../db/migrations/000072_membership_referral.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, string(raw)); e != nil {
		t.Fatal(e)
	}
	yes, rate := true, int64(10000)
	if _, e = s.SetMembershipReferralPolicy(ctx, "global", 0, 0, MembershipReferralPolicyInput{Enabled: &yes, RateBPS: &rate}, referralPolicyAdmin); e != nil {
		t.Fatal(e)
	}
	p, e := s.PurchaseMembership(ctx, buyer, group, "member72-original-before-upgrade", "merchant:7221")
	if e != nil || p.ID != id || p.SourceVersion != 0 {
		t.Fatal("legacy replay failed or was repriced", p, e)
	}
	tz, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tz.Exec(ctx, "SET LOCAL TIME ZONE 'Asia/Shanghai'"); e != nil {
		t.Fatal(e)
	}
	e = s.verifyMembershipPurchase(ctx, tz, p)
	tz.Rollback(ctx)
	if e != nil {
		t.Fatal("historical identity depended on connection time zone", e)
	}
	var legacy, sources, rewards int64
	if e = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM membership_referral_legacy_purchases),(SELECT count(*) FROM membership_referral_sources),(SELECT count(*) FROM membership_referral_rewards)`).Scan(&legacy, &sources, &rewards); e != nil || legacy != 1 || sources != 0 || rewards != 0 {
		t.Fatal("historical purchase received retroactive income or reward", legacy, sources, rewards, e)
	}
	if _, e = s.PurchaseMembership(ctx, buyer, group, "member72-after-upgrade", "merchant:7221"); e != nil {
		t.Fatal(e)
	}
	b, e := s.Balance(ctx, promoter)
	if e != nil || b.Available != 300 {
		t.Fatal("new purchase not independently funded", b, e)
	}
	// The historical71->72 assertions above precede any later migration. The
	// current checker verifies through76, so complete later schemas only now.
	for _, name := range []string{"000073_settlement_auto_payout.up.sql", "000074_settlement_threshold.up.sql", "000075_preauth_remaining_release.up.sql", "000076_channel_auto_stop.up.sql"} {
		later, e := os.ReadFile("../../db/migrations/" + name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.pool.Exec(ctx, string(later)); e != nil {
			t.Fatal(e)
		}
	}
	report, e := s.CheckLedger(ctx)
	if e != nil || report.InvalidReferralTerms != 0 || report.InvalidReferralRewards != 0 || report.InvalidAccounts != 0 || report.InvalidJournals != 0 {
		t.Fatal("valid upgrade failed checker", report, e)
	}
	if _, e = s.pool.Exec(ctx, `INSERT INTO membership_purchases(merchant_id,group_id,price,group_name,external_no,duration_days,expires_at,actor,source_version) VALUES($1,$2,0,'forged','member72-forged-old',30,now()+interval '30 days','merchant:7221',0)`, buyer, group); e == nil {
		t.Fatal("new version zero purchase bypassed source requirement")
	}
	if _, e = s.pool.Exec(ctx, "INSERT INTO membership_referral_legacy_purchases SELECT id,to_jsonb(p),sha256(convert_to(to_jsonb(p)::text,'UTF8')) FROM membership_purchases p WHERE external_no='member72-after-upgrade'"); e == nil {
		t.Fatal("captured legacy roster accepted a new purchase")
	}
}
