package payment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/jackc/pgx/v5"
	"os"
	"reflect"
	"testing"
)

func sharingSchemaRows79(t *testing.T, s *Store) map[string]string {
	t.Helper()
	ctx := context.Background()
	rows, e := s.pool.Query(ctx, "SELECT tablename FROM pg_tables WHERE schemaname=current_schema() ORDER BY tablename")
	if e != nil {
		t.Fatal(e)
	}
	var names []string
	for rows.Next() {
		var name string
		if e = rows.Scan(&name); e != nil {
			t.Fatal(e)
		}
		names = append(names, name)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		t.Fatal(e)
	}
	out := map[string]string{}
	for _, name := range names {
		var raw string
		if e = s.pool.QueryRow(ctx, "SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text FROM "+pgx.Identifier{name}.Sanitize()+" t").Scan(&raw); e != nil {
			t.Fatal(name, e)
		}
		sum := sha256.Sum256([]byte(raw))
		out[name] = hex.EncodeToString(sum[:])
	}
	return out
}

func TestChannelSharingV2Single79UpgradePreservesEvery78TableAndOldOriginalMAC(t *testing.T) {
	s := membership72HistoricalStore(t, 78)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 751, Name: "sharing-owner", APIKey: "local-only"}); e != nil {
		t.Fatal(e)
	}
	_, g, calls, _ := sharingReceiverFlowFixture(t)
	g.Store = s
	g.CallbackBaseURL = "https://local.example.invalid"
	sharingPaymentRule(t, g, 2000)
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 751, OutTradeNo: "old-positive78", Type: "wxpay", Name: "local78", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = g.Submit(ctx, o.TradeNo, 7, "native", "127.0.0.1", ""); e != nil {
		t.Fatal(e)
	}
	if e = s.markPaid(ctx, o.TradeNo, "old-paid78", true, PaidReceipt{ChannelID: 7, ProviderTradeNo: "OLD_TX78"}); e != nil {
		t.Fatal(e)
	}
	old, e := s.ChannelSharingPayment(ctx, 751, o.TradeNo, channelSharingActor)
	if e != nil || !old.Declared {
		t.Fatal(old, e)
	}
	legacy, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 751, OutTradeNo: "old-negative78", Type: "wxpay", Name: "local78", Amount: 123})
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.BeginRecoverableScanAttempt(ctx, legacy.TradeNo, 7, "local-app", "120000000000000078", "old-scan-scope78"); e != nil {
		t.Fatal(e)
	}
	before := sharingSchemaRows79(t, s)
	raw, e := os.ReadFile("../../db/migrations/000079_wechat_v2_single_sharing.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, string(raw)); e != nil {
		t.Fatal(e)
	}
	after := sharingSchemaRows79(t, s)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("79 modified existing rows, ledgers, audits, MACs or source history")
	}
	current, e := s.ChannelSharingPayment(ctx, 751, o.TradeNo, channelSharingActor)
	if e != nil || !reflect.DeepEqual(old, current) {
		t.Fatal("V3 original MAC no longer valid", e)
	}
	count := calls.Load()
	if _, e = g.Submit(ctx, o.TradeNo, 7, "native", "127.0.0.1", ""); e != nil || calls.Load() != count {
		t.Fatal("upgrade replay resent old payment", e)
	}
	report, e := s.CheckLedger(ctx)
	if e != nil || report != (LedgerCheck{}) {
		t.Fatal(report, e)
	}
}
