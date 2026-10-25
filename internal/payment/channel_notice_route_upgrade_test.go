package payment

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestChannelNotice77Actual76SMTPClaimUpgradePreservesEveryOldColumnAndNeverResends(t *testing.T) {
	ctx := context.Background()
	s, job, lease, event := noticeSourceStore77(t, membership72HistoricalStore(t, 76))
	legacy := &uncertainNoticeSender76{}
	if e := s.DeliverChannelStopNotice(ctx, job, lease, "local-admin@example.test", legacy); e != nil || legacy.calls.Load() != 1 {
		t.Fatal("actual76 SMTP attempt unavailable", e)
	}
	before, e := s.readChannelNoticeClaim(ctx, s.pool, event)
	if e != nil || before.RouteVersion != 0 || before.State != "unknown" {
		t.Fatal(before, e)
	}
	rows, e := s.pool.Query(ctx, `SELECT c.table_name,c.column_name FROM information_schema.columns c JOIN information_schema.tables t ON (t.table_schema,t.table_name)=(c.table_schema,c.table_name) WHERE c.table_schema=current_schema() AND t.table_type='BASE TABLE' ORDER BY c.table_name,c.ordinal_position`)
	if e != nil {
		t.Fatal(e)
	}
	columns := map[string][]string{}
	for rows.Next() {
		var table, column string
		if e = rows.Scan(&table, &column); e != nil {
			t.Fatal(e)
		}
		columns[table] = append(columns[table], pgx.Identifier{column}.Sanitize())
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		t.Fatal(e)
	}
	// Actual76 has181 business tables; the authorization helper deliberately
	// adds one local_plan_grants fixture table. Preserve its rows too.
	if len(columns) != 182 || len(columns["local_plan_grants"]) != 1 {
		t.Fatal("expected181 actual76 tables plus original local grant fixture", len(columns))
	}
	hash := func(table string) string {
		t.Helper()
		var value string
		e := s.pool.QueryRow(ctx, `SELECT md5(COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text) FROM (SELECT `+strings.Join(columns[table], ",")+` FROM `+pgx.Identifier{table}.Sanitize()+`)t`).Scan(&value)
		if e != nil {
			t.Fatal(e)
		}
		return value
	}
	hashes := map[string]string{}
	for table := range columns {
		hashes[table] = hash(table)
	}
	raw, e := os.ReadFile("../../db/migrations/000077_admin_notice_routes.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, string(raw)); e != nil {
		t.Fatal(e)
	}
	for table, before := range hashes {
		if after := hash(table); after != before {
			t.Fatal("migration changed historical columns/rows", table)
		}
	}
	after, e := s.readChannelNoticeClaim(ctx, s.pool, event)
	if e != nil || after.RouteVersion != 0 || after.State != before.State || string(after.Proof) != string(before.Proof) {
		t.Fatal("legacy proof changed", after, e)
	}
	status, e := s.ChannelWriteStatus(ctx, 7)
	if e != nil || !status.Blocked || status.NoticeState != "unknown" || status.NoticeTransport != "smtp" {
		t.Fatal("legacy notice status changed", status, e)
	}
	mail := &routedNotice77{mode: "smtp", scope: strings.Repeat("a", 64), to: "local-admin@example.test"}
	if e = s.DeliverChannelStopNotice(ctx, job, lease, mail.to, mail); e != nil || mail.calls.Load() != 0 {
		t.Fatal("legacy attempt reopened", e)
	}
	replacement := &routedNotice77{mode: "telegram", scope: strings.Repeat("b", 64), to: "-12345"}
	if e = s.DeliverChannelStopNotice(ctx, job, lease, replacement.to, replacement); !errors.Is(e, ErrConflict) || replacement.calls.Load() != 0 {
		t.Fatal("legacy attempt changed transport", e)
	}
	report, e := s.CheckLedger(ctx)
	if e != nil || report.InvalidChannelStops != 0 || report.UnknownTransfers != 1 {
		t.Fatal(report, e)
	}
	balance, e := s.Balance(ctx, 7395)
	if e != nil || balance.Available != 1000 || balance.Reserved != 1000 {
		t.Fatal("notification upgrade changed original funds", balance, e)
	}
	t.Logf("preserved all original columns of %d actual76 tables including original unknown SMTP claim and result", len(columns))
}
