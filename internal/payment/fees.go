package payment

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"strconv"
)

// calculateFee mirrors Release's rounding of merchant retained money, rather than
// independently rounding the deducted fee. Division before multiplication keeps
// every intermediate in int64 even for a maximum-sized amount.
func calculateFee(gross, feeRateBPS int64) (net, fee int64, err error) {
	if gross <= 0 || feeRateBPS < 0 || feeRateBPS > 10000 {
		return 0, 0, ErrInvalid
	}
	retained := 10000 - feeRateBPS
	net = gross/10000*retained + (gross%10000*retained+5000)/10000
	return net, gross - net, nil
}

type feeSnapshot struct {
	Policy, Payer           string
	Rate, GroupID, Net, Fee int64
}

func feePolicy(ctx context.Context, tx pgx.Tx, group string, gross int64) (feeSnapshot, error) {
	f := feeSnapshot{Policy: "none", Payer: "platform", Net: gross}
	enabled := false
	rows, e := tx.Query(ctx, "SELECT body->>'key',body->>'value' FROM backoffice_records WHERE resource='settings' AND body->>'key' IN ('payment_fee_enabled','refund_fee_payer') FOR SHARE")
	if e != nil {
		return f, e
	}
	for rows.Next() {
		var k, v string
		if e = rows.Scan(&k, &v); e != nil {
			rows.Close()
			return f, e
		}
		switch k {
		case "payment_fee_enabled":
			if v != "true" && v != "false" {
				rows.Close()
				return f, ErrInvalid
			}
			enabled = v == "true"
		case "refund_fee_payer":
			if v != "platform" && v != "merchant" {
				rows.Close()
				return f, ErrInvalid
			}
			f.Payer = v
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return f, e
	}
	if !enabled {
		return f, nil
	}
	f.Policy = "merchant_deduct"
	if group == "" {
		return f, nil
	}
	f.GroupID, e = strconv.ParseInt(group, 10, 64)
	if e != nil || f.GroupID <= 0 {
		return f, ErrInvalid
	}
	var raw string
	e = tx.QueryRow(ctx, "SELECT COALESCE(body->>'rate_bps','0') FROM backoffice_records WHERE resource='groups' AND id=$1 AND body->>'enabled'='true' FOR SHARE", f.GroupID).Scan(&raw)
	if errors.Is(e, pgx.ErrNoRows) {
		return f, ErrState
	}
	if e != nil {
		return f, e
	}
	f.Rate, e = strconv.ParseInt(raw, 10, 64)
	if e != nil {
		return f, ErrInvalid
	}
	f.Net, f.Fee, e = calculateFee(gross, f.Rate)
	return f, e
}
func validateOrderReplay(o Order, in CreateOrderInput) error {
	if o.Version != in.Version || o.Amount != in.Amount || o.Name != in.Name || o.Type != in.Type || o.NotifyURL != in.NotifyURL || o.ReturnURL != in.ReturnURL || o.Param != in.Param {
		return ErrConflict
	}
	return nil
}
func (s *Store) createOrderWithFees(ctx context.Context, in CreateOrderInput) (Order, error) {
	return s.createOrderWithFeesPurpose(ctx, in, "payment", "")
}

// CreateRechargeOrder fixes private wallet purpose in the same transaction as
// the new order and its negative reward terms, before any provider attempt.
func (s *Store) CreateRechargeOrder(ctx context.Context, in CreateOrderInput, actor string) (Order, error) {
	in, err := normalizeCreateOrderInput(in)
	if err != nil || !sharingSafeField(actor, 128, true) {
		return Order{}, ErrInvalid
	}
	return s.createOrderWithFeesPurpose(ctx, in, "recharge", actor)
}

func (s *Store) validateOrderPurposeReplay(ctx context.Context, tx pgx.Tx, o Order, purpose string) error {
	var recharge bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM recharge_intents WHERE order_id=$1)", o.ID).Scan(&recharge); err != nil {
		return err
	}
	if recharge != (purpose == "recharge") {
		return ErrConflict
	}
	t, err := s.readReferralTerms(ctx, tx, o)
	if errors.Is(err, ErrNotFound) {
		return nil // Historical order: never invent a new reward snapshot.
	}
	if err != nil {
		return err
	}
	if t.Purpose != purpose {
		return ErrConflict
	}
	return nil
}

func (s *Store) createOrderWithFeesPurpose(ctx context.Context, in CreateOrderInput, purpose, actor string) (Order, error) {
	return s.createOrderWithPrintOrigin(ctx, in, purpose, actor, 0)
}

func (s *Store) createOrderWithPrintOrigin(ctx context.Context, in CreateOrderInput, purpose, actor string, collectionID int64) (Order, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Order{}, e
	}
	defer tx.Rollback(ctx)
	var enabled bool
	var payStatus int
	var group string
	e = tx.QueryRow(ctx, "SELECT enabled,COALESCE((to_jsonb(merchants)->>'pay')::int,1),CASE WHEN group_baseline_unresolved THEN '-1' ELSE COALESCE(NULLIF(base_group_id,0)::text,'') END FROM merchants WHERE id=$1 FOR SHARE", in.MerchantID).Scan(&enabled, &payStatus, &group)
	if errors.Is(e, pgx.ErrNoRows) {
		return Order{}, ErrNotFound
	}
	if e != nil {
		return Order{}, e
	}
	if !enabled || payStatus != 1 {
		return Order{}, ErrState
	}
	if collectionID > 0 {
		var owner int64
		if e = tx.QueryRow(ctx, "SELECT owner_id FROM backoffice_records WHERE id=$1 AND resource='collection-codes' AND body->>'enabled'='true' FOR SHARE", collectionID).Scan(&owner); e != nil {
			return Order{}, e
		}
		if owner != in.MerchantID || purpose != "payment" {
			return Order{}, ErrConflict
		}
		if e = s.validateCollectionGenerationOrder(ctx, tx, collectionID, in); e != nil {
			return Order{}, e
		}
	}
	existing, e := scanOrder(tx.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE merchant_id=$1 AND out_trade_no=$2", in.MerchantID, in.OutTradeNo))
	if e == nil {
		if e = validateOrderReplay(existing, in); e != nil {
			return Order{}, e
		}
		if e = s.validateOrderPurposeReplay(ctx, tx, existing, purpose); e != nil {
			return Order{}, e
		}
		if e = s.validatePrintOriginReplay(ctx, tx, existing, purpose, collectionID); e != nil {
			return Order{}, e
		}
		return existing, nil
	}
	if !errors.Is(e, ErrNotFound) {
		return Order{}, e
	}
	// The merchant lock serializes purchases and administrator assignments with
	// this new-order snapshot. Expiration never changes an existing order.
	var purchased string
	e = tx.QueryRow(ctx, "SELECT group_id::text FROM memberships WHERE merchant_id=$1 AND expires_at>now()", in.MerchantID).Scan(&purchased)
	if e == nil {
		group = purchased
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return Order{}, e
	}
	f, e := feePolicy(ctx, tx, group, in.Amount)
	if e != nil {
		return Order{}, e
	}
	var trade string
	if e = tx.QueryRow(ctx, "SELECT nextval('trade_no_seq')::text").Scan(&trade); e != nil {
		return Order{}, e
	}
	o, e := scanOrder(tx.QueryRow(ctx, "INSERT INTO orders(trade_no,merchant_id,out_trade_no,name,type,amount,notify_url,return_url,param,version,fee_policy,fee_rate_bps,fee_group_id,merchant_amount,platform_fee,refund_fee_payer) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) ON CONFLICT(merchant_id,out_trade_no) DO NOTHING RETURNING "+orderColumns, trade, in.MerchantID, in.OutTradeNo, in.Name, in.Type, in.Amount, in.NotifyURL, in.ReturnURL, in.Param, in.Version, f.Policy, f.Rate, f.GroupID, f.Net, f.Fee, f.Payer))
	if errors.Is(e, ErrNotFound) {
		o, e = scanOrder(tx.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE merchant_id=$1 AND out_trade_no=$2", in.MerchantID, in.OutTradeNo))
		if e != nil {
			return Order{}, e
		}
		if e = validateOrderReplay(o, in); e != nil {
			return Order{}, e
		}
		if e = s.validateOrderPurposeReplay(ctx, tx, o, purpose); e != nil {
			return Order{}, e
		}
		if e = s.validatePrintOriginReplay(ctx, tx, o, purpose, collectionID); e != nil {
			return Order{}, e
		}
		return o, nil
	}
	if e != nil {
		return Order{}, e
	}
	if e = validateOrderReplay(o, in); e != nil {
		return Order{}, e
	}
	if purpose == "recharge" {
		if _, e = tx.Exec(ctx, "INSERT INTO recharge_intents(order_id,created_by) VALUES($1,$2)", o.ID, actor); e != nil {
			return Order{}, e
		}
	}
	if e = s.snapshotReferralTerms(ctx, tx, o, purpose, in.PaymentCost); e != nil {
		return Order{}, e
	}
	if e = s.writePrintOrigin(ctx, tx, o, purpose, collectionID); e != nil {
		return Order{}, e
	}
	return o, tx.Commit(ctx)
}
