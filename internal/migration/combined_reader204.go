package migration

import (
	"context"
	"crypto/hmac"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/ledger"
)

var ErrCombinedOrderNotFound204 = errors.New("combined order history not found")
var ErrCombinedHistorySource204 = fmt.Errorf("%w: combined order historical source cannot be confirmed", ErrConflict)

type CombinedOrderItem204 struct {
	SubTradeNo  string  `json:"sub_trade_no"`
	APITradeNo  string  `json:"api_trade_no"`
	Money       string  `json:"money"`
	RefundMoney *string `json:"refundmoney"`
	Status      int     `json:"status"`
	Settle      int     `json:"settle"`
}
type CombinedOrderHistory204 struct {
	TradeNo               string                 `json:"trade_no"`
	ReadOnly              bool                   `json:"read_only"`
	VerifiedPaymentSource bool                   `json:"verified_payment_source"`
	Source                string                 `json:"source"`
	ExportID              string                 `json:"export_id"`
	ExportedAt            string                 `json:"exported_at"`
	ParentSettle          int                    `json:"parent_settle"`
	ChargedMoney          string                 `json:"charged_money"`
	ChildrenMoney         string                 `json:"children_money"`
	AmountsMatch          bool                   `json:"amounts_match"`
	Items                 []CombinedOrderItem204 `json:"items"`
}
type CombinedOrderHistoryReader struct {
	pool   *pgxpool.Pool
	master []byte
}

func NewCombinedOrderHistoryReader(pool *pgxpool.Pool, master []byte) (*CombinedOrderHistoryReader, error) {
	if pool == nil || len(master) != 32 {
		return nil, ErrValidation
	}
	return &CombinedOrderHistoryReader{pool: pool, master: append([]byte(nil), master...)}, nil
}

func combinedSchema204(ctx context.Context, tx pgx.Tx) (bool, error) {
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relkind='r' AND c.relname=ANY($1::text[])`, []string{"legacy_combined_export_manifests204", "legacy_combined_order_manifests204", "legacy_suborder_history204"}).Scan(&count); err != nil {
		return false, err
	}
	if count == 3 {
		return true, nil
	}
	if count != 0 {
		return false, ErrCombinedHistorySource204
	}
	var receipts bool
	if err := tx.QueryRow(ctx, `SELECT to_regclass(current_schema()||'.schema_migrations') IS NOT NULL`).Scan(&receipts); err != nil {
		return false, err
	}
	if receipts {
		var installed bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name='000116_legacy_combined_orders204.up.sql')`).Scan(&installed); err != nil {
			return false, err
		}
		if installed {
			return false, ErrCombinedHistorySource204
		}
	}
	return false, nil
}

func checkCombinedExports204(ctx context.Context, tx pgx.Tx, master []byte) (map[string]combinedExport204, error) {
	exports := map[string]combinedExport204{}
	ready, err := combinedSchema204(ctx, tx)
	if err != nil || !ready {
		return exports, err
	}
	var orphan bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM legacy_combined_export_manifests204 m WHERE NOT EXISTS(SELECT 1 FROM legacy_import_runs r WHERE r.export_id=m.export_id)) OR EXISTS(SELECT 1 FROM legacy_combined_order_manifests204 p WHERE NOT EXISTS(SELECT 1 FROM legacy_combined_export_manifests204 m WHERE m.export_id=p.export_id)) OR EXISTS(SELECT 1 FROM legacy_suborder_history204 c WHERE NOT EXISTS(SELECT 1 FROM legacy_combined_order_manifests204 p WHERE (p.order_id,p.export_id,p.merchant_id,p.trade_no)=(c.order_id,c.export_id,c.merchant_id,c.trade_no)))`).Scan(&orphan); err != nil {
		return nil, err
	}
	if orphan {
		return nil, ErrCombinedHistorySource204
	}
	rows, err := tx.Query(ctx, `SELECT r.export_id,r.source,r.sha256,r.combined_history_version204,COALESCE(m.source,''),COALESCE(m.source_hash,''),COALESCE(m.exported_at,''),COALESCE(m.parent_count,-1),COALESCE(m.child_count,-1),m.parents_hash,m.proof_hash,COALESCE(m.created_at,r.imported_at) FROM legacy_import_runs r LEFT JOIN legacy_combined_export_manifests204 m ON m.export_id=r.export_id WHERE r.combined_history_version204=1 OR m.export_id IS NOT NULL ORDER BY r.export_id`)
	if err != nil {
		return nil, err
	}
	var all []combinedExport204
	for rows.Next() {
		var e combinedExport204
		var source, digest string
		var version int
		if err = rows.Scan(&e.ExportID, &source, &digest, &version, &e.Source, &e.Digest, &e.ExportedAt, &e.Parents, &e.Children, &e.ParentsHash, &e.Proof, &e.At); err != nil {
			rows.Close()
			return nil, err
		}
		if version != 1 || source != e.Source || digest != e.Digest || e.Parents < 0 || e.Children < 0 || len(e.ParentsHash) != 32 || len(e.Proof) != 32 || !hmac.Equal(e.Proof, combinedExportProof204(master, e)) {
			rows.Close()
			return nil, ErrCombinedHistorySource204
		}
		if _, err = time.Parse(time.RFC3339, e.ExportedAt); err != nil {
			rows.Close()
			return nil, ErrCombinedHistorySource204
		}
		all = append(all, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, e := range all {
		var parents, children int64
		if err = tx.QueryRow(ctx, `SELECT(SELECT count(*) FROM legacy_combined_order_manifests204 WHERE export_id=$1),(SELECT count(*) FROM legacy_suborder_history204 WHERE export_id=$1)`, e.ExportID).Scan(&parents, &children); err != nil {
			return nil, err
		}
		if parents != e.Parents || children != e.Children {
			return nil, ErrCombinedHistorySource204
		}
		proofRows, err := tx.Query(ctx, `SELECT proof_hash FROM legacy_combined_order_manifests204 WHERE export_id=$1 ORDER BY order_id`, e.ExportID)
		if err != nil {
			return nil, err
		}
		var proofs [][]byte
		for proofRows.Next() {
			var proof []byte
			if err = proofRows.Scan(&proof); err != nil {
				proofRows.Close()
				return nil, err
			}
			if len(proof) != 32 {
				proofRows.Close()
				return nil, ErrCombinedHistorySource204
			}
			proofs = append(proofs, proof)
		}
		err = proofRows.Err()
		proofRows.Close()
		if err != nil {
			return nil, err
		}
		if !hmac.Equal(e.ParentsHash, combinedHash204(proofs)) {
			return nil, ErrCombinedHistorySource204
		}
		exports[e.ExportID] = e
	}
	return exports, nil
}

const combinedParentColumns204 = `p.order_id,p.merchant_id,p.export_id,p.trade_no,p.source_plugin,p.currency,p.nominal_amount,p.charged_amount,p.parent_settle,p.child_count,p.children_amount,p.children_hash,p.source_hash,p.proof_hash`
const combinedChildColumns204 = `order_id,merchant_id,export_id,trade_no,position,sub_trade_no,api_trade_no,amount,refund_amount,legacy_status,settle,source_hash,proof_hash`

func readCombinedGroups204(ctx context.Context, tx pgx.Tx, master []byte, mid int64, ids []int64, exports map[string]combinedExport204) (map[int64]CombinedOrderHistory204, error) {
	out := map[int64]CombinedOrderHistory204{}
	parents := map[int64]combinedParent204{}
	rows, err := tx.Query(ctx, `SELECT `+combinedParentColumns204+`,o.amount,o.merchant_id,o.trade_no FROM legacy_combined_order_manifests204 p JOIN orders o ON o.id=p.order_id WHERE p.order_id=ANY($1::bigint[]) AND ($2::bigint=0 OR o.merchant_id=$2) ORDER BY p.order_id`, ids, mid)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p combinedParent204
		var nominal, owner int64
		var trade string
		if err = rows.Scan(&p.OrderID, &p.MerchantID, &p.ExportID, &p.TradeNo, &p.SourcePlugin, &p.Currency, &p.NominalAmount, &p.ChargedAmount, &p.Settle, &p.Count, &p.ChildrenAmount, &p.ChildrenHash, &p.Digest, &p.Proof, &nominal, &owner, &trade); err != nil {
			rows.Close()
			return nil, err
		}
		e, ok := exports[p.ExportID]
		if !ok || e.Digest != p.Digest || p.MerchantID != owner || p.TradeNo != trade || p.NominalAmount != nominal || p.Currency != "CNY" || p.Count < 1 || p.Count > 50 || p.ChargedAmount <= 0 || p.ChildrenAmount != p.ChargedAmount || p.Settle < 0 || p.Settle > 3 || !combinedText204(p.SourcePlugin, 64, true) || len(p.ChildrenHash) != 32 || !hmac.Equal(p.Proof, combinedParentProof204(master, p)) {
			rows.Close()
			return nil, ErrCombinedHistorySource204
		}
		parents[p.OrderID] = p
		out[p.OrderID] = CombinedOrderHistory204{TradeNo: p.TradeNo, ReadOnly: true, Source: e.Source, ExportID: e.ExportID, ExportedAt: e.ExportedAt, ParentSettle: p.Settle, ChargedMoney: ledger.Format(p.ChargedAmount), ChildrenMoney: ledger.Format(p.ChildrenAmount), AmountsMatch: true, Items: []CombinedOrderItem204{}}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(parents) == 0 {
		return out, nil
	}
	childRows, err := tx.Query(ctx, `SELECT `+combinedChildColumns204+` FROM legacy_suborder_history204 WHERE order_id=ANY($1::bigint[]) ORDER BY order_id,position`, ids)
	if err != nil {
		return nil, err
	}
	proofs := map[int64][][]byte{}
	totals := map[int64]int64{}
	for childRows.Next() {
		var c combinedChildRecord204
		if err = childRows.Scan(&c.OrderID, &c.MerchantID, &c.ExportID, &c.TradeNo, &c.Position, &c.SubTradeNo, &c.APITradeNo, &c.Amount, &c.RefundAmount, &c.Status, &c.Settle, &c.Digest, &c.Proof); err != nil {
			childRows.Close()
			return nil, err
		}
		p, ok := parents[c.OrderID]
		if !ok {
			continue
		}
		v := out[c.OrderID]
		if c.MerchantID != p.MerchantID || c.ExportID != p.ExportID || c.TradeNo != p.TradeNo || c.Digest != p.Digest || c.Position != len(v.Items)+1 || c.Amount <= 0 || c.Amount > math.MaxInt64-totals[c.OrderID] || c.RefundAmount != nil && (*c.RefundAmount < 0 || *c.RefundAmount > c.Amount) || c.Status < 0 || c.Status > 4 || c.Settle < 0 || c.Settle > 1 || !combinedText204(c.SubTradeNo, 25, false) || !combinedText204(c.APITradeNo, 150, true) || !hmac.Equal(c.Proof, combinedChildProof204(master, c)) {
			childRows.Close()
			return nil, ErrCombinedHistorySource204
		}
		var refund *string
		if c.RefundAmount != nil {
			formatted := ledger.Format(*c.RefundAmount)
			refund = &formatted
		}
		v.Items = append(v.Items, CombinedOrderItem204{SubTradeNo: c.SubTradeNo, APITradeNo: c.APITradeNo, Money: ledger.Format(c.Amount), RefundMoney: refund, Status: c.Status, Settle: c.Settle})
		out[c.OrderID] = v
		totals[c.OrderID] += c.Amount
		proofs[c.OrderID] = append(proofs[c.OrderID], c.Proof)
	}
	err = childRows.Err()
	childRows.Close()
	if err != nil {
		return nil, err
	}
	for id, p := range parents {
		if len(out[id].Items) != p.Count || totals[id] != p.ChargedAmount || !hmac.Equal(p.ChildrenHash, combinedHash204(proofs[id])) {
			return nil, ErrCombinedHistorySource204
		}
	}
	return out, nil
}

func (r *CombinedOrderHistoryReader) Read(ctx context.Context, mid int64, tradeNo string) (CombinedOrderHistory204, error) {
	var empty CombinedOrderHistory204
	if r == nil || r.pool == nil || mid < 0 || !combinedText204(tradeNo, 128, false) {
		return empty, ErrValidation
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	var id int64
	if err = tx.QueryRow(ctx, `SELECT id FROM orders WHERE trade_no=$1 AND ($2::bigint=0 OR merchant_id=$2)`, tradeNo, mid).Scan(&id); errors.Is(err, pgx.ErrNoRows) {
		return empty, ErrCombinedOrderNotFound204
	} else if err != nil {
		return empty, err
	}
	exports, err := checkCombinedExports204(ctx, tx, r.master)
	if err != nil {
		return empty, err
	}
	ready, err := combinedSchema204(ctx, tx)
	if err != nil {
		return empty, err
	}
	if !ready {
		return empty, ErrCombinedOrderNotFound204
	}
	groups, err := readCombinedGroups204(ctx, tx, r.master, mid, []int64{id}, exports)
	if err != nil {
		return empty, err
	}
	out, ok := groups[id]
	if !ok {
		return empty, ErrCombinedOrderNotFound204
	}
	return out, tx.Commit(ctx)
}

func (r *CombinedOrderHistoryReader) Capabilities(ctx context.Context, mid int64, ids []int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	if r == nil || r.pool == nil || mid < 0 || len(ids) > 200 {
		return nil, ErrValidation
	}
	for _, id := range ids {
		if id <= 0 {
			return nil, ErrValidation
		}
		out[id] = false
	}
	if len(ids) == 0 {
		return out, nil
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT id FROM orders WHERE id=ANY($1::bigint[]) AND ($2::bigint=0 OR merchant_id=$2)`, ids, mid)
	if err != nil {
		return nil, err
	}
	var owned []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		owned = append(owned, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(owned) == 0 {
		return out, tx.Commit(ctx)
	}
	exports, err := checkCombinedExports204(ctx, tx, r.master)
	if err != nil {
		return out, err
	}
	ready, err := combinedSchema204(ctx, tx)
	if err != nil {
		return out, err
	}
	if !ready {
		return out, tx.Commit(ctx)
	}
	groups, err := readCombinedGroups204(ctx, tx, r.master, mid, owned, exports)
	if err != nil {
		return out, err
	}
	for id := range groups {
		out[id] = true
	}
	return out, tx.Commit(ctx)
}

func verifyCombinedReplay204(ctx context.Context, tx pgx.Tx, master []byte, plan Plan) error {
	exports, err := checkCombinedExports204(ctx, tx, master)
	if err != nil {
		return err
	}
	if plan.CombinedOrders == nil {
		return nil
	}
	export, ok := exports[plan.ExportID]
	if !ok || export.Parents != int64(len(plan.CombinedOrders)) {
		return ErrCombinedHistorySource204
	}
	ids := make([]int64, 0, len(plan.CombinedOrders))
	for _, g := range plan.CombinedOrders {
		var id int64
		if err = tx.QueryRow(ctx, `SELECT id FROM orders WHERE merchant_id=$1 AND trade_no=$2`, g.MerchantID, g.TradeNo).Scan(&id); err != nil {
			return ErrCombinedHistorySource204
		}
		ids = append(ids, id)
	}
	groups, err := readCombinedGroups204(ctx, tx, master, 0, ids, exports)
	if err != nil {
		return err
	}
	if len(groups) != len(plan.CombinedOrders) {
		return ErrCombinedHistorySource204
	}
	return nil
}
