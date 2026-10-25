package payment

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type recipientSummaryTracer struct{ calls atomic.Int32 }

func (t *recipientSummaryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	t.calls.Add(1)
	return ctx
}
func (*recipientSummaryTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func recipientSummaryFixture(t *testing.T, s *Store, mid int64, account string) SettlementRecipientView {
	t.Helper()
	ctx := context.Background()
	if err := s.CreateMerchant(ctx, Merchant{ID: mid, Name: "synthetic-summary", APIKey: "synthetic-summary-only-key"}); err != nil {
		t.Fatal(err)
	}
	v, err := s.SetSettlementRecipient(ctx, mid, SettlementRecipientInput{Type: "alipay", Account: account, Name: "Synthetic Private Recipient Name"}, nil, SettlementRecipientActor{Role: "merchant", ID: mid, Name: "synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestSettlementRecipientSummariesBatchSafeAndSingleQuery(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	first := recipientSummaryFixture(t, s, 8101, "synthetic-private-recipient-1234")
	second := recipientSummaryFixture(t, s, 8102, "short")
	extra := map[int64]SettlementRecipientView{}
	for mid, in := range map[int64]SettlementRecipientInput{
		8104: {Type: "wxpay", AppID: "synthetic-app", Account: "synthetic-private-openid", Name: "Synthetic Private Recipient Name"},
		8105: {Type: "bank", Account: "1234567890123456", Name: "Synthetic Private Recipient Name"},
	} {
		if err := s.CreateMerchant(ctx, Merchant{ID: mid, Name: "synthetic-summary", APIKey: "synthetic-summary-only-key"}); err != nil {
			t.Fatal(err)
		}
		v, err := s.SetSettlementRecipient(ctx, mid, in, nil, SettlementRecipientActor{Role: "merchant", ID: mid, Name: "synthetic"})
		if err != nil {
			t.Fatal(err)
		}
		extra[mid] = v
	}
	if err := s.CreateMerchant(ctx, Merchant{ID: 8103, Name: "unconfigured", APIKey: "synthetic-summary-only-key"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE merchants SET profile='{"account":"legacy-plaintext-account","username":"Legacy Private Name"}' WHERE id=8103`); err != nil {
		t.Fatal(err)
	}
	cfg := s.pool.Config()
	tracer := &recipientSummaryTracer{}
	cfg.ConnConfig.Tracer = tracer
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	s.pool = pool
	views, err := s.SettlementRecipientSummaries(ctx, []int64{8103, 8102, 8101, 8104, 8105})
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 5 || views[8101] != first || views[8102] != second || views[8103].State != "missing" || views[8103].AccountMasked != "" || views[8104] != extra[8104] || views[8105] != extra[8105] {
		t.Fatalf("wrong safe summaries: %#v", views)
	}
	if tracer.calls.Load() != 1 {
		t.Fatalf("batch issued %d SQL queries, want exactly one", tracer.calls.Load())
	}
	raw, err := json.Marshal(views)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"synthetic-private-recipient", "synthetic-private-openid", "1234567890123456", "Synthetic Private Recipient Name", "legacy-plaintext-account", "Legacy Private Name", "recipient_cipher", "identity_hash", "short"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("summary JSON disclosed private material")
		}
	}
	oversized := make([]int64, 101)
	for i := range oversized {
		oversized[i] = int64(9000 + i)
	}
	for _, ids := range [][]int64{nil, {}, {0}, {-1}, {1, 1}, oversized} {
		if _, err := s.SettlementRecipientSummaries(ctx, ids); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid IDs accepted: %v: %v", ids, err)
		}
	}
	if tracer.calls.Load() != 1 {
		t.Fatal("invalid IDs reached SQL")
	}
	boundary, err := s.SettlementRecipientSummaries(ctx, oversized[:100])
	if err != nil || len(boundary) != 100 || tracer.calls.Load() != 2 {
		t.Fatal("100 unique IDs must use one bounded query", len(boundary), err, tracer.calls.Load())
	}
}

func TestSettlementRecipientSummariesCorruptionIsUnavailable(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	other := recipientSummaryFixture(t, s, 8200, "other-private-recipient-9876")
	var ids []int64
	for i, kind := range []string{"cipher", "hash", "type", "appid", "merchant", "missing-config", "copied-cipher", "foreign-current"} {
		mid := int64(8201 + i)
		cfg := recipientSummaryFixture(t, s, mid, "private-recipient-1234")
		ids = append(ids, mid)
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, `ALTER TABLE settlement_recipient_configs DISABLE TRIGGER ALL; ALTER TABLE settlement_recipient_current DISABLE TRIGGER ALL`); err != nil {
			t.Fatal(err)
		}
		var sql string
		args := []any{cfg.ConfigID}
		switch kind {
		case "cipher":
			sql = `UPDATE settlement_recipient_configs SET recipient_cipher=decode(repeat('00',64),'hex') WHERE id=$1`
		case "hash":
			sql = `UPDATE settlement_recipient_configs SET identity_hash=decode(repeat('00',32),'hex') WHERE id=$1`
		case "type":
			sql = `UPDATE settlement_recipient_configs SET recipient_type='bank' WHERE id=$1`
		case "appid":
			sql = `UPDATE settlement_recipient_configs SET app_id='different-app' WHERE id=$1`
		case "merchant":
			sql = `UPDATE settlement_recipient_configs SET merchant_id=8200 WHERE id=$1`
		case "missing-config":
			sql = `DELETE FROM settlement_recipient_configs WHERE id=$1`
		case "copied-cipher":
			sql = `UPDATE settlement_recipient_configs SET recipient_cipher=(SELECT recipient_cipher FROM settlement_recipient_configs WHERE id=$2) WHERE id=$1`
			args = append(args, other.ConfigID)
		case "foreign-current":
			sql = `UPDATE settlement_recipient_current SET config_id=$2 WHERE merchant_id=$1`
			args = []any{mid, other.ConfigID}
		}
		if _, err = tx.Exec(ctx, sql, args...); err != nil {
			t.Fatal(kind, err)
		}
		if _, err = tx.Exec(ctx, `ALTER TABLE settlement_recipient_configs ENABLE TRIGGER ALL; ALTER TABLE settlement_recipient_current ENABLE TRIGGER ALL`); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(kind, err)
		}
	}
	views, err := s.SettlementRecipientSummaries(ctx, ids)
	if err != nil || len(views) != len(ids) {
		t.Fatal("damaged records must remain representable without a private value", len(views), err)
	}
	for _, mid := range ids {
		if v := views[mid]; v != (SettlementRecipientView{State: "unavailable"}) {
			t.Fatal("corruption exposed a mask or became missing", mid, v)
		}
	}
	good, err := s.SettlementRecipientSummaries(ctx, []int64{8200})
	if err != nil || good[8200] != other {
		t.Fatal("foreign configuration mutated or leaked", good, err)
	}
	var financialRows int
	if err = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM ledger_entries)+(SELECT count(*) FROM jobs)+(SELECT count(*) FROM transfers)+(SELECT count(*) FROM ledger_requests)`).Scan(&financialRows); err != nil || financialRows != 0 {
		t.Fatal("summary changed money or jobs", financialRows, err)
	}
}

func TestSettlementRecipientSummariesSQLFailureIsError(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `ALTER TABLE settlement_recipient_current RENAME TO synthetic_recipient_current_unavailable`); err != nil {
		t.Fatal(err)
	}
	v, err := s.SettlementRecipientSummaries(ctx, []int64{8300})
	var sqlErr *pgconn.PgError
	if v != nil || !errors.As(err, &sqlErr) || sqlErr.Code != "42P01" {
		t.Fatal("database error hidden as recipient state", v, err)
	}
}
