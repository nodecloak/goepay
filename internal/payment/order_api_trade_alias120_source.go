package payment

import (
	"context"
	"encoding/hex"
	"errors"

	"github.com/jackc/pgx/v5"
)

// OrderAPITradeAliasSource120 contains only authenticated original facts. The
// birth fields are immutable; an absent receipt may become a real receipt after
// ordinary paid confirmation. None of these values are supplied by the editor.
type OrderAPITradeAliasSource120 struct {
	OrderID, MerchantID                                         int64
	TradeNo, OutTradeNo, Name, PaymentType, Version             string
	OriginalAmount, CustomerCharge, MerchantAmount, PlatformFee int64
	FeeRateBPS                                                  int64
	FeePolicy, RefundFeePayer, ChargeProof, CreatedAt           string
	ReceiptChannelID                                            int64
	VerifiedAPITradeNo                                          string
}

// OrderAPITradeAliasSourceTx120 uses only the caller's connection. The caller
// locks the original administrator/session before this order/receipt reader.
// It does not mint payment evidence, adopt unsealed history or call a provider.
func (s *Store) OrderAPITradeAliasSourceTx120(ctx context.Context, tx pgx.Tx, orderID, merchantID int64, tradeNo string) (OrderAPITradeAliasSource120, error) {
	var out OrderAPITradeAliasSource120
	if s == nil || tx == nil || orderID <= 0 || merchantID <= 0 || tradeNo == "" {
		return out, ErrInvalid
	}
	var locked int64
	err := tx.QueryRow(ctx, `SELECT id FROM orders WHERE id=$1 AND merchant_id=$2 AND trade_no=$3 FOR SHARE`, orderID, merchantID, tradeNo).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	o, err := s.OrderByTradeNoTx(ctx, tx, tradeNo)
	if err != nil {
		return out, err
	}
	if o.ID != orderID || o.MerchantID != merchantID || o.Type != "alipay" && o.Type != "wxpay" {
		return out, ErrConflict
	}
	charge, proof, err := s.authenticatedOrderCharge104(ctx, tx, o)
	if err != nil {
		return out, err
	}
	// A legacy amount fallback is not an authenticated native payment birth.
	if len(proof) != 32 || charge <= 0 {
		return out, ErrConflict
	}
	out = OrderAPITradeAliasSource120{
		OrderID: o.ID, MerchantID: o.MerchantID, TradeNo: o.TradeNo, OutTradeNo: o.OutTradeNo,
		Name: o.Name, PaymentType: o.Type, Version: o.Version, OriginalAmount: o.Amount,
		CustomerCharge: charge, MerchantAmount: o.MerchantAmount, PlatformFee: o.PlatformFee,
		FeeRateBPS: o.FeeRateBPS, FeePolicy: o.FeePolicy, RefundFeePayer: o.RefundFeePayer,
		ChargeProof: hex.EncodeToString(proof), CreatedAt: o.CreatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
	}
	err = tx.QueryRow(ctx, `SELECT channel_id,provider_trade_no FROM verified_payment_receipts WHERE order_id=$1 FOR SHARE`, o.ID).Scan(&out.ReceiptChannelID, &out.VerifiedAPITradeNo)
	if errors.Is(err, pgx.ErrNoRows) {
		if o.Status == "paid" {
			return OrderAPITradeAliasSource120{}, ErrConflict
		}
		out.ReceiptChannelID = 0
		out.VerifiedAPITradeNo = ""
		return out, nil
	}
	if err != nil {
		return OrderAPITradeAliasSource120{}, err
	}
	if o.Status != "paid" || out.ReceiptChannelID <= 0 || out.VerifiedAPITradeNo == "" || len(out.VerifiedAPITradeNo) > 256 {
		return OrderAPITradeAliasSource120{}, ErrConflict
	}
	var channel int64
	var upstream string
	if err = tx.QueryRow(ctx, `SELECT channel_id,provider_trade_no FROM provider_attempts WHERE order_id=$1 FOR SHARE`, o.ID).Scan(&channel, &upstream); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = ErrConflict
		}
		return OrderAPITradeAliasSource120{}, err
	}
	if channel != out.ReceiptChannelID || upstream != "" && upstream != out.VerifiedAPITradeNo {
		return OrderAPITradeAliasSource120{}, ErrConflict
	}
	return out, nil
}
