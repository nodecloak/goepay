package payment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"time"
)

type PrintingPaidSource struct {
	OrderID          int64  `json:"order_id,string"`
	MerchantID       int64  `json:"merchant_id,string"`
	TradeNo          string `json:"trade_no"`
	Name             string `json:"name"`
	Amount           int64  `json:"amount,string"`
	Type             string `json:"type"`
	Remark           string `json:"remark"`
	PaidAt           string `json:"paid_at"`
	ChannelID        int64  `json:"channel_id,string"`
	ProviderTradeNo  string `json:"provider_trade_no"`
	MerchantAmount   int64  `json:"merchant_amount,string"`
	PlatformFee      int64  `json:"platform_fee,string"`
	Origin           string `json:"origin"`
	CollectionCodeID int64  `json:"collection_code_id,string"`
	PlatformConfigID int64  `json:"platform_config_id,string"`
	MerchantConfigID int64  `json:"merchant_config_id,string"`
	PlatformEnabled  bool   `json:"platform_enabled"`
	Mode             int    `json:"mode"`
	SnapshotState    string `json:"snapshot_state"`
	Fresh            bool   `json:"fresh"`
}

func (s *Store) PrintingProtectionKey() []byte {
	m := hmac.New(sha256.New, s.indexKey)
	m.Write([]byte("goepay/printing/protection/v1"))
	return m.Sum(nil)
}
func (s *Store) writePrintOrigin(ctx context.Context, tx pgx.Tx, o Order, purpose string, collectionID int64) error {
	kind := "api"
	if purpose == "recharge" {
		kind = "recharge"
	}
	if collectionID > 0 {
		kind = "collection"
	}
	proof, e := s.referralDigest("order-print-origin", []any{o.ID, o.MerchantID, kind, collectionID})
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, "INSERT INTO order_print_origins(order_id,kind,collection_code_id,proof_hash) VALUES($1,$2,NULLIF($3::bigint,0),$4)", o.ID, kind, collectionID, proof)
	return e
}
func (s *Store) readPrintOrigin(ctx context.Context, q fundsReader, o Order) (string, int64, error) {
	var kind string
	var cid int64
	var proof []byte
	e := q.QueryRow(ctx, "SELECT kind,COALESCE(collection_code_id,0),proof_hash FROM order_print_origins WHERE order_id=$1", o.ID).Scan(&kind, &cid, &proof)
	if errors.Is(e, pgx.ErrNoRows) {
		return "legacy", 0, nil
	}
	if e != nil {
		return "", 0, e
	}
	want, e := s.referralDigest("order-print-origin", []any{o.ID, o.MerchantID, kind, cid})
	if e != nil || !hmac.Equal(want, proof) {
		return "", 0, ErrConflict
	}
	return kind, cid, nil
}
func (s *Store) validatePrintOriginReplay(ctx context.Context, q fundsReader, o Order, purpose string, cid int64) error {
	kind, got, e := s.readPrintOrigin(ctx, q, o)
	if e != nil {
		return e
	}
	want := "api"
	if purpose == "recharge" {
		want = "recharge"
	}
	if cid > 0 {
		want = "collection"
	}
	if (kind != want && kind != "legacy") || got != cid || kind == "legacy" && cid > 0 {
		return ErrConflict
	}
	return nil
}

// VerifyPrintingOrigin is a read-only integrity hook for restore checks. It
// verifies existing typed origins; it never invents origins for legacy orders.
func (s *Store) VerifyPrintingOrigin(ctx context.Context, q fundsReader, id int64) error {
	o, e := scanOrder(q.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE id=$1", id))
	if e != nil {
		return e
	}
	_, _, e = s.readPrintOrigin(ctx, q, o)
	return e
}

// Only the authenticated receipt call path records this independent payment
// fact. Printer configuration parsing, decryption and all I/O are asynchronous.
func (s *Store) recordPrintingPaidSource(ctx context.Context, tx pgx.Tx, o Order, r PaidReceipt, fresh bool) error {
	// The paid journal and authenticated provider receipt are mandatory. This
	// optional printing projection is isolated as a unit: neither a broken
	// printer table nor its outbox trigger may roll back a verified payment.
	child, e := tx.Begin(ctx)
	if e != nil {
		return nil
	}
	if e = s.recordPrintingPaidSourceTx(ctx, child, o, r, fresh); e != nil {
		_ = child.Rollback(ctx)
		return nil
	}
	if e = child.Commit(ctx); e != nil {
		_ = child.Rollback(ctx)
	}
	return nil
}
func (s *Store) recordPrintingPaidSourceTx(ctx context.Context, tx pgx.Tx, o Order, r PaidReceipt, fresh bool) error {
	var exists bool
	if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM payment_print_sources WHERE order_id=$1)", o.ID).Scan(&exists); e != nil {
		return e
	}
	if exists {
		_, e := s.PrintingPaidSource(ctx, tx, o.ID)
		return e
	}
	var at time.Time
	if e := tx.QueryRow(ctx, "SELECT paid_at FROM orders WHERE id=$1 AND status='paid'", o.ID).Scan(&at); e != nil {
		return e
	}
	origin, cid, e := s.readPrintOrigin(ctx, tx, o)
	if e != nil {
		return e
	}
	f := PrintingPaidSource{OrderID: o.ID, MerchantID: o.MerchantID, TradeNo: o.TradeNo, Name: o.Name, Amount: o.Amount, Type: o.Type, Remark: o.Param, PaidAt: at.UTC().Format(time.RFC3339Nano), ChannelID: r.ChannelID, ProviderTradeNo: r.ProviderTradeNo, MerchantAmount: o.MerchantAmount, PlatformFee: o.PlatformFee, Origin: origin, CollectionCodeID: cid, Fresh: fresh, SnapshotState: "historical_reconfirmed"}
	if fresh {
		// A malformed/unavailable optional printing configuration cannot abort a
		// verified payment. Retain an explicit unavailable snapshot; never use a
		// later current configuration to retroactively create an automatic print.
		f.SnapshotState = "unavailable"
		child, ce := tx.Begin(ctx)
		if ce == nil {
			ce = child.QueryRow(ctx, `SELECT COALESCE((SELECT c.id FROM printing_platform_head h JOIN printing_platform_configs c ON c.id=h.config_id),0),COALESCE((SELECT c.enabled FROM printing_platform_head h JOIN printing_platform_configs c ON c.id=h.config_id),false),COALESCE((SELECT c.id FROM printing_merchant_heads h JOIN printing_merchant_configs c ON c.id=h.config_id WHERE h.merchant_id=$1),0),COALESCE((SELECT c.mode FROM printing_merchant_heads h JOIN printing_merchant_configs c ON c.id=h.config_id WHERE h.merchant_id=$1),0)`, o.MerchantID).Scan(&f.PlatformConfigID, &f.PlatformEnabled, &f.MerchantConfigID, &f.Mode)
			if ce == nil {
				ce = child.Commit(ctx)
			} else {
				_ = child.Rollback(ctx)
			}
			if ce == nil {
				f.SnapshotState = "captured"
			} else {
				f.PlatformConfigID = 0
				f.MerchantConfigID = 0
				f.PlatformEnabled = false
				f.Mode = 0
			}
		}
	}
	raw, e := json.Marshal(f)
	if e != nil {
		return e
	}
	proof, e := s.referralDigest("verified-print-paid-source", f)
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO payment_print_sources(order_id,merchant_id,body,proof_hash) VALUES($1,$2,$3,$4)", o.ID, o.MerchantID, raw, proof); e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO jobs(event_key,kind,payload) VALUES('receipt-paid:'||$1::bigint::text,'receipt-print.prepare',jsonb_build_object('order_id',$1::bigint::text)) ON CONFLICT(event_key) DO NOTHING`, o.ID)
	return e
}
func (s *Store) PrintingPaidSource(ctx context.Context, q fundsReader, id int64) (PrintingPaidSource, error) {
	var f PrintingPaidSource
	var raw, proof []byte
	e := q.QueryRow(ctx, "SELECT body,proof_hash FROM payment_print_sources WHERE order_id=$1", id).Scan(&raw, &proof)
	if errors.Is(e, pgx.ErrNoRows) {
		return f, ErrNotFound
	}
	if e != nil {
		return f, e
	}
	if json.Unmarshal(raw, &f) != nil || f.OrderID != id {
		return f, ErrConflict
	}
	want, e := s.referralDigest("verified-print-paid-source", f)
	if e != nil || !hmac.Equal(want, proof) {
		return f, ErrConflict
	}
	reader, ok := q.(referralReader)
	if !ok {
		return f, ErrConflict
	}
	if _, e = s.authenticatedOrderIDCharge104(ctx, reader, id); e != nil {
		return f, e
	}
	var valid bool
	e = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM orders o JOIN verified_payment_receipts v ON v.order_id=o.id WHERE o.id=$1 AND o.merchant_id=$2 AND o.status='paid' AND o.trade_no=$3 AND o.amount=$4 AND o.merchant_amount=$5 AND o.platform_fee=$6 AND v.channel_id=$7 AND v.provider_trade_no=$8 AND o.paid_at=$9::timestamptz
 AND (o.merchant_amount=0 OR EXISTS(SELECT 1 FROM journals j WHERE j.event_key='payment:'||o.trade_no AND (SELECT COALESCE(sum(x.amount),0) FROM ledger_entries x JOIN accounts a ON a.id=x.account_id WHERE x.journal_id=j.id AND a.owner_id=o.merchant_id AND a.kind='available' AND a.scope_id=0)=o.merchant_amount))
 AND (o.platform_fee=0 OR EXISTS(SELECT 1 FROM journals j WHERE j.event_key='payment-fee:'||o.trade_no AND (SELECT COALESCE(sum(x.amount),0) FROM ledger_entries x JOIN accounts a ON a.id=x.account_id WHERE x.journal_id=j.id AND a.owner_id=0 AND a.kind='fees' AND a.scope_id=0)=o.platform_fee)))`, id, f.MerchantID, f.TradeNo, f.Amount, f.MerchantAmount, f.PlatformFee, f.ChannelID, f.ProviderTradeNo, f.PaidAt).Scan(&valid)
	if e != nil {
		return f, e
	}
	if !valid {
		return f, ErrConflict
	}
	return f, nil
}
func (s *Store) CreateCollectionOrder(ctx context.Context, codeID int64, in CreateOrderInput) (Order, error) {
	if codeID <= 0 {
		return Order{}, ErrInvalid
	}
	in, e := normalizeCreateOrderInput(in)
	if e != nil {
		return Order{}, e
	}
	return s.createOrderWithPrintOrigin(ctx, in, "payment", "", codeID)
}
