package migration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCombinedOrders204ReaderBindsParentOwnerAndPreservesReadOnlyOriginalFields(t *testing.T) {
	p := migrationPool(t)
	key := bytes.Repeat([]byte{3}, 32)
	d := combinedData204(t)
	d["orders"] = append(d["orders"].([]any), map[string]any{"trade_no": "204-ordinary", "merchant_id": 9001, "out_trade_no": "ordinary204", "name": "Ordinary imported order", "type": "wxpay", "money": "2.00", "status": "created", "created_at": "2026-10-20T12:00:00+08:00"})
	if _, err := Apply(t.Context(), p, key, encodeCombined204(t, d)); err != nil {
		t.Fatal(err)
	}
	reader, err := NewCombinedOrderHistoryReader(p, key)
	if err != nil {
		t.Fatal(err)
	}
	view, err := reader.Read(t.Context(), 9001, "204-parent")
	if err != nil || view.TradeNo != "204-parent" || !view.ReadOnly || view.VerifiedPaymentSource || view.Source != "epay_pro" || view.ExportID != "sanitized-combined204" || view.ExportedAt != "2026-10-23T12:00:00+08:00" || view.ParentSettle != 2 || view.ChargedMoney != "5.25" || view.ChildrenMoney != "5.25" || !view.AmountsMatch || len(view.Items) != 3 {
		t.Fatal("original historical combined view was not preserved", view, err)
	}
	if view.Items[0].RefundMoney != nil || view.Items[1].RefundMoney == nil || *view.Items[1].RefundMoney != "0.25" || view.Items[2].RefundMoney == nil || *view.Items[2].RefundMoney != "0.00" {
		t.Fatal("reader collapsed NULL and zero refunds", view.Items)
	}
	if _, err = reader.Read(t.Context(), 9002, "204-parent"); !errors.Is(err, ErrCombinedOrderNotFound204) {
		t.Fatal("reader trusted a different merchant's parent", err)
	}
	if _, err = reader.Read(t.Context(), 0, "204-parent"); err != nil {
		t.Fatal("internally authorized admin read failed", err)
	}
	if _, err = reader.Read(t.Context(), 9001, "204-ordinary"); !errors.Is(err, ErrCombinedOrderNotFound204) {
		t.Fatal("ordinary parent was fabricated into a combined graph", err)
	}
	var parent, ordinary int64
	if err = p.QueryRow(t.Context(), `SELECT(SELECT id FROM orders WHERE trade_no='204-parent'),(SELECT id FROM orders WHERE trade_no='204-ordinary')`).Scan(&parent, &ordinary); err != nil {
		t.Fatal(err)
	}
	caps, err := reader.Capabilities(t.Context(), 9001, []int64{parent, ordinary})
	if err != nil || !caps[parent] || caps[ordinary] {
		t.Fatal("capability was not tied to the complete protected parent graph", caps, err)
	}
	foreign, err := reader.Capabilities(t.Context(), 9002, []int64{parent, ordinary})
	if err != nil || foreign[parent] || foreign[ordinary] {
		t.Fatal("capability leaked across merchant ownership", foreign, err)
	}
	wrong, err := NewCombinedOrderHistoryReader(p, bytes.Repeat([]byte{4}, 32))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = wrong.Read(t.Context(), 9001, "204-parent"); !errors.Is(err, ErrCombinedHistorySource204) {
		t.Fatal("wrong MAC key accepted historical source", err)
	}
}

func TestCombinedOrders204MissingOrAlteredHistoricalSourcesNeverBecomeEmptySuccess(t *testing.T) {
	for _, tc := range []struct{ name, sql string }{
		{"child proof", `ALTER TABLE legacy_suborder_history204 DISABLE TRIGGER USER;UPDATE legacy_suborder_history204 SET proof_hash=decode(repeat('00',32),'hex') WHERE position=1;ALTER TABLE legacy_suborder_history204 ENABLE TRIGGER USER`},
		{"child status", `ALTER TABLE legacy_suborder_history204 DISABLE TRIGGER USER;UPDATE legacy_suborder_history204 SET legacy_status=0 WHERE position=1;ALTER TABLE legacy_suborder_history204 ENABLE TRIGGER USER`},
		{"last child", `ALTER TABLE legacy_suborder_history204 DISABLE TRIGGER USER;DELETE FROM legacy_suborder_history204 WHERE position=3;ALTER TABLE legacy_suborder_history204 ENABLE TRIGGER USER`},
		{"export manifest", `ALTER TABLE legacy_combined_export_manifests204 DISABLE TRIGGER USER;DELETE FROM legacy_combined_export_manifests204;ALTER TABLE legacy_combined_export_manifests204 ENABLE TRIGGER USER`},
		{"parent amount", `ALTER TABLE legacy_combined_order_manifests204 DISABLE TRIGGER USER;UPDATE legacy_combined_order_manifests204 SET nominal_amount=nominal_amount+1;ALTER TABLE legacy_combined_order_manifests204 ENABLE TRIGGER USER`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := migrationPool(t)
			key := bytes.Repeat([]byte{3}, 32)
			if _, err := Apply(t.Context(), p, key, []byte(combinedSnapshot204)); err != nil {
				t.Fatal(err)
			}
			reader, err := NewCombinedOrderHistoryReader(p, key)
			if err != nil {
				t.Fatal(err)
			}
			var id int64
			if err = p.QueryRow(t.Context(), `SELECT id FROM orders WHERE trade_no='204-parent'`).Scan(&id); err != nil {
				t.Fatal(err)
			}
			if _, err = p.Exec(t.Context(), tc.sql); err != nil {
				t.Fatal("isolated damaged-restore fixture failed", err)
			}
			if _, err = reader.Read(t.Context(), 9001, "204-parent"); !errors.Is(err, ErrCombinedHistorySource204) {
				t.Fatal("damaged combined history was returned as an authentic graph or empty page", err)
			}
			if caps, err := reader.Capabilities(t.Context(), 9001, []int64{id}); err == nil || caps[id] {
				t.Fatal("damaged historical source advertised an ordinary successful capability", caps, err)
			}
			if replay, err := Apply(t.Context(), p, key, []byte(combinedSnapshot204)); err == nil || replay.AlreadyImported {
				t.Fatal("idempotent replay resealed or accepted damaged combined source", replay, err)
			}
		})
	}
}

func TestCombinedOrders204ImmutableBirthAndSingleConnectionReads(t *testing.T) {
	p := migrationPool(t)
	key := bytes.Repeat([]byte{3}, 32)
	if _, err := Apply(t.Context(), p, key, []byte(combinedSnapshot204)); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Exec(t.Context(), `UPDATE legacy_suborder_history204 SET settle=1 WHERE position=2`); err == nil {
		t.Fatal("historical settlement became a writable current projection")
	}
	if _, err := p.Exec(t.Context(), `INSERT INTO legacy_suborder_history204(order_id,export_id,merchant_id,trade_no,position,sub_trade_no,api_trade_no,amount,refund_amount,legacy_status,settle,source_hash,proof_hash) SELECT order_id,export_id,merchant_id,trade_no,4,'204-late-child','',1,NULL,0,0,source_hash,proof_hash FROM legacy_suborder_history204 WHERE position=1`); err == nil {
		t.Fatal("completed export accepted a later child assertion")
	}
	cfg := p.Config()
	cfg.MaxConns = 1
	single, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer single.Close()
	reader, err := NewCombinedOrderHistoryReader(single, key)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	view, err := reader.Read(ctx, 9001, "204-parent")
	if err != nil || len(view.Items) != 3 {
		t.Fatal("reader reacquired its own single pool connection", err)
	}
	encoded, err := json.Marshal(view)
	if err != nil || bytes.Contains(encoded, []byte("proof_hash")) || bytes.Contains(encoded, []byte("source_hash")) {
		t.Fatal("reader exposed private source proof material", err)
	}
	if replay, err := Apply(ctx, single, key, []byte(combinedSnapshot204)); err != nil || !replay.AlreadyImported {
		t.Fatal("combined replay required another pool connection", replay, err)
	}
}
