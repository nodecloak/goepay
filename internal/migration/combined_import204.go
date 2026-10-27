package migration

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/ledger"
)

type combinedParent204 struct {
	OrderID, MerchantID, NominalAmount, ChargedAmount, ChildrenAmount int64
	ExportID, TradeNo, SourcePlugin, Currency, Digest                 string
	Settle, Count                                                     int
	ChildrenHash, Proof                                               []byte
}
type combinedChildRecord204 struct {
	OrderID, MerchantID, Amount                       int64
	ExportID, TradeNo, SubTradeNo, APITradeNo, Digest string
	Position, Status, Settle                          int
	RefundAmount                                      *int64
	Proof                                             []byte
}
type combinedExport204 struct {
	ExportID, Source, Digest, ExportedAt string
	Parents, Children                    int64
	ParentsHash, Proof                   []byte
	At                                   time.Time
}

func combinedProof204(master []byte, kind string, value any) []byte {
	raw, _ := json.Marshal(value)
	mac := hmac.New(sha256.New, master)
	mac.Write([]byte("goepay/legacy-combined204/v1/" + kind + "\x00"))
	mac.Write(raw)
	return mac.Sum(nil)
}
func combinedParentProof204(master []byte, p combinedParent204) []byte {
	return combinedProof204(master, "parent", []any{p.OrderID, p.MerchantID, p.ExportID, p.TradeNo, p.SourcePlugin, p.Currency, p.NominalAmount, p.ChargedAmount, p.Settle, p.Count, p.ChildrenAmount, p.ChildrenHash, p.Digest})
}
func combinedChildProof204(master []byte, c combinedChildRecord204) []byte {
	return combinedProof204(master, "child", []any{c.OrderID, c.MerchantID, c.ExportID, c.TradeNo, c.Position, c.SubTradeNo, c.APITradeNo, c.Amount, c.RefundAmount, c.Status, c.Settle, c.Digest})
}
func combinedExportProof204(master []byte, e combinedExport204) []byte {
	return combinedProof204(master, "export", []any{e.ExportID, e.Source, e.Digest, e.ExportedAt, e.Parents, e.Children, e.ParentsHash, e.At.UTC().Format(time.RFC3339Nano)})
}
func combinedHash204(proofs [][]byte) []byte {
	h := sha256.New()
	for _, proof := range proofs {
		h.Write(proof)
	}
	return h.Sum(nil)
}

func archiveCombinedOrders204(ctx context.Context, tx pgx.Tx, master []byte, plan Plan, digest string, orders map[string]int64) error {
	if plan.CombinedOrders == nil {
		return nil
	}
	parents := make([]combinedParent204, 0, len(plan.CombinedOrders))
	export := combinedExport204{ExportID: plan.ExportID, Source: plan.Source, Digest: digest, ExportedAt: plan.ExportedAt, Parents: int64(len(plan.CombinedOrders))}
	for _, g := range plan.CombinedOrders {
		p := combinedParent204{OrderID: orders[g.TradeNo], MerchantID: g.MerchantID, ExportID: plan.ExportID, TradeNo: g.TradeNo, SourcePlugin: g.SourcePlugin, Currency: g.Currency, Digest: digest, Settle: g.Settle, Count: len(g.SubOrders)}
		p.ChargedAmount, _ = ledger.Parse(g.ChargedMoney)
		if err := tx.QueryRow(ctx, `SELECT amount FROM orders WHERE id=$1 AND merchant_id=$2 AND trade_no=$3`, p.OrderID, p.MerchantID, p.TradeNo).Scan(&p.NominalAmount); err != nil {
			return err
		}
		proofs := make([][]byte, 0, len(g.SubOrders))
		for i, sub := range g.SubOrders {
			c := combinedChildRecord204{OrderID: p.OrderID, MerchantID: p.MerchantID, ExportID: p.ExportID, TradeNo: p.TradeNo, Position: i + 1, SubTradeNo: sub.SubTradeNo, APITradeNo: sub.APITradeNo, Status: sub.Status, Settle: sub.Settle, Digest: digest}
			c.Amount, _ = ledger.Parse(sub.Money)
			if sub.RefundMoney != nil {
				n, _ := ledger.Parse(*sub.RefundMoney)
				c.RefundAmount = &n
			}
			c.Proof = combinedChildProof204(master, c)
			if _, err := tx.Exec(ctx, `INSERT INTO legacy_suborder_history204(order_id,export_id,merchant_id,trade_no,position,sub_trade_no,api_trade_no,amount,refund_amount,legacy_status,settle,source_hash,proof_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, c.OrderID, c.ExportID, c.MerchantID, c.TradeNo, c.Position, c.SubTradeNo, c.APITradeNo, c.Amount, c.RefundAmount, c.Status, c.Settle, c.Digest, c.Proof); err != nil {
				return err
			}
			p.ChildrenAmount += c.Amount
			proofs = append(proofs, c.Proof)
			export.Children++
		}
		p.ChildrenHash = combinedHash204(proofs)
		p.Proof = combinedParentProof204(master, p)
		if _, err := tx.Exec(ctx, `INSERT INTO legacy_combined_order_manifests204(order_id,export_id,merchant_id,trade_no,source_plugin,currency,nominal_amount,charged_amount,parent_settle,child_count,children_amount,children_hash,source_hash,proof_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, p.OrderID, p.ExportID, p.MerchantID, p.TradeNo, p.SourcePlugin, p.Currency, p.NominalAmount, p.ChargedAmount, p.Settle, p.Count, p.ChildrenAmount, p.ChildrenHash, p.Digest, p.Proof); err != nil {
			return err
		}
		parents = append(parents, p)
	}
	sort.Slice(parents, func(i, j int) bool { return parents[i].OrderID < parents[j].OrderID })
	proofs := make([][]byte, 0, len(parents))
	for _, p := range parents {
		proofs = append(proofs, p.Proof)
	}
	export.ParentsHash = combinedHash204(proofs)
	if err := tx.QueryRow(ctx, `SELECT transaction_timestamp()`).Scan(&export.At); err != nil {
		return err
	}
	export.Proof = combinedExportProof204(master, export)
	_, err := tx.Exec(ctx, `INSERT INTO legacy_combined_export_manifests204(export_id,source,source_hash,exported_at,parent_count,child_count,parents_hash,proof_hash,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, export.ExportID, export.Source, export.Digest, export.ExportedAt, export.Parents, export.Children, export.ParentsHash, export.Proof, export.At)
	return err
}
