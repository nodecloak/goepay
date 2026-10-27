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
	s, facts := actualHistoricalStore155(t, "sharing78")
	ctx := context.Background()
	var o Order
	facts.decode(t, "order", &o)
	_, g, calls, _ := sharingReceiverFlowFixture(t)
	g.Store = s
	g.CallbackBaseURL = "https://local.example.invalid"
	old, e := s.ChannelSharingPayment(ctx, 751, o.TradeNo, channelSharingActor)
	if e != nil || !old.Declared {
		t.Fatal(old, e)
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
