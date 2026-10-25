package payment

import (
	"context"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
	"os"
	"testing"
)

func TestChannelStop76UpgradePreservesActual75UnknownFundsWithoutInventingStopsOrNotices(t *testing.T) {
	s := membership72HistoricalStore(t, 75)
	ctx := context.Background()
	const mid int64 = 7633
	if e := s.CreateMerchant(ctx, Merchant{ID: mid, Name: "Local historical payer", APIKey: "synthetic-only76"}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `UPDATE merchants SET profile='{"transfer_api_enabled":true}' WHERE id=$1`, mid); e != nil {
		t.Fatal(e)
	}
	transferSetting(t, s, "transfer_enabled", "true")
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: mid, OutTradeNo: "before76-local-credit", Amount: 2000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, o.TradeNo, "before76-local-paid"); e != nil {
		t.Fatal(e)
	}
	provider := &autoScopeProvider73{kind: "alipay", account: "seller:2088000000000073", product: "alipay.fund.trans.toaccount.transfer"}
	scope, e := adapter.TransferAccountScope(provider)
	if e != nil {
		t.Fatal(e)
	}
	old, owner, e := s.beginTransfer(ctx, mid, 7, "before76-unknown", adapter.TransferRequest{Type: "alipay", Account: "local-recipient@example.test", Name: "Local Recipient", Amount: 1000, Remark: "Local upgrade fixture"}, func(adapter.TransferRequest) error { return nil }, "", scope)
	if e != nil || !owner {
		t.Fatal(old, owner, e)
	}
	if e = s.resolveProviderTransfer(ctx, old.ID, "unknown", "", ""); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE jobs SET last_error='40004:PAYER_BALANCE_NOT_ENOUGH' WHERE kind='transfer.reconcile'`); e != nil {
		t.Fatal(e)
	}
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
