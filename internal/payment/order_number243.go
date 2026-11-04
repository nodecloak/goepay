package payment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const maxOrderSequence243 int64 = 9999999999999

var orderNumberZone243 = time.FixedZone("UTC+8", 8*3600)

// orderTradeNumber243 uses the database transaction's timestamp, also used by
// orders.created_at. A fixed-width suffix keeps ordinary identifiers distinct
// from combined child identifiers, which append their position to the parent.
func orderTradeNumber243(at time.Time, sequence int64) (string, error) {
	local := at.In(orderNumberZone243)
	if sequence < 1 || sequence > maxOrderSequence243 || local.Year() < 1 || local.Year() > 9999 {
		return "", ErrInvalid
	}
	return fmt.Sprintf("%s%013d", local.Format("20060102150405"), sequence), nil
}

// insertOrderRow243 preserves the merchant-idempotency INSERT. A historical
// import can already own a generated system number; only that specific unique
// violation is retried. Its failed INSERT is isolated from the order transaction
// with a savepoint, so other constraint failures retain their normal behavior.
func insertOrderRow243(ctx context.Context, tx pgx.Tx, in CreateOrderInput, f feeSnapshot, routed bool, actualCharge *int64) (Order, error) {
	for attempt := 0; attempt < 8; attempt++ {
		var sequence int64
		var at time.Time
		if err := tx.QueryRow(ctx, "SELECT nextval('order_trade_no_seq'),transaction_timestamp()").Scan(&sequence, &at); err != nil {
			return Order{}, err
		}
		trade, err := orderTradeNumber243(at, sequence)
		if err != nil {
			return Order{}, err
		}
		savepoint, err := tx.Begin(ctx)
		if err != nil {
			return Order{}, err
		}
		o, err := scanOrder(savepoint.QueryRow(ctx, "INSERT INTO orders(trade_no,merchant_id,out_trade_no,name,type,amount,notify_url,return_url,param,version,fee_policy,fee_rate_bps,fee_group_id,merchant_amount,platform_fee,refund_fee_payer,runtime_fee_quote_required,actual_charge_amount104,actual_charge_required104) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19) ON CONFLICT(merchant_id,out_trade_no) DO NOTHING RETURNING "+orderColumns, trade, in.MerchantID, in.OutTradeNo, in.Name, in.Type, in.Amount, in.NotifyURL, in.ReturnURL, in.Param, in.Version, f.Policy, f.Rate, f.GroupID, f.Net, f.Fee, f.Payer, routed, actualCharge, actualCharge != nil))
		if err != nil && !errors.Is(err, ErrNotFound) {
			if rollbackErr := savepoint.Rollback(ctx); rollbackErr != nil {
				return Order{}, rollbackErr
			}
			var unique *pgconn.PgError
			if errors.As(err, &unique) && unique.Code == "23505" && unique.TableName == "orders" && unique.ConstraintName == "orders_trade_no_key" {
				continue
			}
			return Order{}, err
		}
		if releaseErr := savepoint.Commit(ctx); releaseErr != nil {
			return Order{}, releaseErr
		}
		return o, err
	}
	return Order{}, ErrConflict
}
