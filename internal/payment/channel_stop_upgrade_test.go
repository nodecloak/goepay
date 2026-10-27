package payment

import (
	"context"
	"github.com/jackc/pgx/v5"
	"os"
	"testing"
)

func TestChannelStop76UpgradePreservesActual75UnknownFundsWithoutInventingStopsOrNotices(t *testing.T) {
	s, _ := actualHistoricalStore155(t, "stop75")
	ctx := context.Background()
	const mid int64 = 7633
	rows, e := s.pool.Query(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema=current_schema() AND table_type='BASE TABLE' ORDER BY table_name`)
	if e != nil {
		t.Fatal(e)
	}
	tables := []string{}
	for rows.Next() {
		var name string
		if e = rows.Scan(&name); e != nil {
			rows.Close()
			t.Fatal(e)
		}
		tables = append(tables, name)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		t.Fatal(e)
	}
	hash := func(name string) string {
		t.Helper()
		var value string
		if e = s.pool.QueryRow(ctx, `SELECT md5(COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text) FROM `+pgx.Identifier{name}.Sanitize()+` t`).Scan(&value); e != nil {
			t.Fatal(e)
		}
		return value
	}
	before := map[string]string{}
	for _, name := range tables {
		before[name] = hash(name)
	}
	raw, e := os.ReadFile("../../db/migrations/000076_channel_auto_stop.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, string(raw)); e != nil {
		t.Fatal(e)
	}
	for _, name := range tables {
		if before[name] != hash(name) {
			t.Fatal("76 changed historical table rows", name)
		}
	}
	var children int
	if e = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM channel_stop_events)+(SELECT count(*) FROM channel_stop_heads)+(SELECT count(*) FROM channel_stop_notice_claims)`).Scan(&children); e != nil || children != 0 {
		t.Fatal("historical error text invented stop authority", children, e)
	}
	status, e := s.ChannelWriteStatus(ctx, 7)
	if e != nil || status.EventID != 0 || status.Blocked || status.NoticeState != "" {
		t.Fatal(status, e)
	}
	balance, e := s.Balance(ctx, mid)
	if e != nil || balance.Available != 1000 || balance.Reserved != 1000 {
		t.Fatal("unknown gross changed during upgrade", balance, e)
	}
	check, e := s.CheckLedger(ctx)
	if e != nil || check.InvalidChannelStops != 0 || check.UnknownTransfers != 1 || check.InvalidAccounts != 0 || check.InvalidJournals != 0 {
		t.Fatal(check, e)
	}
	t.Logf("preserved %d original75 tables, original unknown gross and no backfill", len(tables))
}
