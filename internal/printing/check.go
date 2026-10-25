package printing

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"goepay/internal/payment"
)

// Check verifies each printing parent and its immutable evidence in one read
// snapshot. The returned count is bad records, not distinct financial orders.
// Optional printing projections absent after an isolated payment fault are
// intentionally not inferred from paid orders or manufactured during checks.
func (s *Store) Check(ctx context.Context) (int64, error) {
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return 0, e
	}
	defer tx.Rollback(ctx)
	var bad int64
	checkErr := func(e error) error {
		if e == nil {
			return nil
		}
		if errors.Is(e, ErrConflict) || errors.Is(e, ErrNotFound) || errors.Is(e, ErrState) || errors.Is(e, payment.ErrConflict) || errors.Is(e, payment.ErrNotFound) {
			bad++
			return nil
		}
		return e
	}
	scan := func(table, column, where string, verify func(int64) error) error {
		var cursor int64
		for {
			rows, e := tx.Query(ctx, "SELECT "+column+" FROM "+table+" WHERE "+column+">$1 AND ("+where+") ORDER BY "+column+" LIMIT 100", cursor)
			if e != nil {
				return e
			}
			ids := make([]int64, 0, 100)
			for rows.Next() {
				var id int64
				if e = rows.Scan(&id); e != nil {
					rows.Close()
					return e
				}
				ids = append(ids, id)
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
			for _, id := range ids {
				if e = checkErr(verify(id)); e != nil {
					return e
				}
			}
			if len(ids) < 100 {
				return nil
			}
			cursor = ids[len(ids)-1]
		}
	}
	if e = scan("order_print_origins", "order_id", "true", func(id int64) error { return s.source.VerifyPrintingOrigin(ctx, tx, id) }); e != nil {
		return bad, e
	}
	if e = scan("printing_platform_configs", "id", "true", func(id int64) error { _, e := s.readPlatform(ctx, tx, id); return e }); e != nil {
		return bad, e
	}
	var platformHistory bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM printing_platform_configs)").Scan(&platformHistory); e != nil {
		return bad, e
	}
	if platformHistory {
		_, e = s.readPlatform(ctx, tx, 0)
		if e = checkErr(e); e != nil {
			return bad, e
		}
	}
	if e = scan("printing_merchant_configs", "id", "true", func(id int64) error {
		var mid int64
		if e := tx.QueryRow(ctx, "SELECT merchant_id FROM printing_merchant_configs WHERE id=$1", id).Scan(&mid); e != nil {
			return e
		}
		_, e := s.readMerchant(ctx, tx, mid, id)
		return e
	}); e != nil {
		return bad, e
	}
	if e = scan("merchants", "id", "EXISTS(SELECT 1 FROM printing_merchant_configs c WHERE c.merchant_id=merchants.id) OR EXISTS(SELECT 1 FROM printing_merchant_heads h WHERE h.merchant_id=merchants.id)", func(mid int64) error { _, e := s.readMerchant(ctx, tx, mid, 0); return e }); e != nil {
		return bad, e
	}
	if e = scan("payment_print_sources", "order_id", "true", func(id int64) error {
		f, e := s.source.PrintingPaidSource(ctx, tx, id)
		if e != nil {
			return e
		}
		var valid bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM payment_print_sources s WHERE s.order_id=$1 AND s.merchant_id=$2) AND EXISTS(SELECT 1 FROM jobs j WHERE j.event_key=$3 AND j.kind='receipt-print.prepare' AND j.payload=jsonb_build_object('order_id',$1::bigint::text))`, id, f.MerchantID, fmt.Sprintf("receipt-paid:%d", id)).Scan(&valid); e != nil {
			return e
		}
		if !valid {
			return ErrConflict
		}
		return nil
	}); e != nil {
		return bad, e
	}
	if e = scan("printing_intents", "id", "true", func(id int64) error {
		r, e := s.readJob(ctx, tx, 0, id)
		if e != nil {
			return e
		}
		if r.V.TaskID <= 0 || r.V.Reason == "task_unconfirmed" {
			return ErrConflict
		}
		return nil
	}); e != nil {
		return bad, e
	}
	// Scan child parents too: a broken restore can omit their original intent,
	// and must not disappear merely because a join-based projection has no row.
	for _, table := range []string{"printing_claims", "printing_results"} {
		if e = scan(table, "intent_id", "true", func(id int64) error { _, e := s.readJob(ctx, tx, 0, id); return e }); e != nil {
			return bad, e
		}
	}
	if e = scan("printing_recoveries", "id", "true", func(id int64) error {
		var intent int64
		if e := tx.QueryRow(ctx, "SELECT intent_id FROM printing_recoveries WHERE id=$1", id).Scan(&intent); e != nil {
			return e
		}
		_, e := s.readJob(ctx, tx, 0, intent)
		return e
	}); e != nil {
		return bad, e
	}
	if e = scan("jobs", "id", "kind IN ('receipt-print.prepare','receipt-print.send') OR event_key LIKE 'receipt-paid:%' OR event_key LIKE 'receipt-intent:%'", func(id int64) error {
		var kind, key string
		var raw []byte
		if e := tx.QueryRow(ctx, "SELECT kind,event_key,payload FROM jobs WHERE id=$1", id).Scan(&kind, &key, &raw); e != nil {
			return e
		}
		target, e := parsePrintingTask(raw, kind)
		if e != nil {
			return ErrConflict
		}
		if kind == "receipt-print.prepare" {
			if key != fmt.Sprintf("receipt-paid:%d", target) {
				return ErrConflict
			}
			_, e := s.source.PrintingPaidSource(ctx, tx, target)
			return e
		}
		if _, e = s.readJob(ctx, tx, 0, target); e != nil {
			return e
		}
		var valid bool
		if e = tx.QueryRow(ctx, `SELECT $2::text='receipt-intent:'||$1::bigint::text OR EXISTS(SELECT 1 FROM printing_recoveries r WHERE r.intent_id=$1 AND r.new_task_id=$3 AND $2::text='receipt-intent:'||$1::bigint::text||':recovery:'||r.id::text)`, target, key, id).Scan(&valid); e != nil {
			return e
		}
		if !valid {
			return ErrConflict
		}
		return nil
	}); e != nil {
		return bad, e
	}
	// Heads with no corresponding history cannot be hidden by parent scans.
	var orphan int64
	if e = tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM printing_platform_head h WHERE NOT EXISTS(SELECT 1 FROM printing_platform_configs c WHERE (c.id,c.revision)=(h.config_id,h.revision)))+(SELECT count(*) FROM printing_merchant_heads h WHERE NOT EXISTS(SELECT 1 FROM printing_merchant_configs c WHERE (c.id,c.merchant_id,c.revision)=(h.config_id,h.merchant_id,h.revision)))`).Scan(&orphan); e != nil {
		return bad, e
	}
	bad += orphan
	return bad, tx.Commit(ctx)
}
