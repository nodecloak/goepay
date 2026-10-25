package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
	"goepay/internal/ledger"
	"goepay/internal/payment"
)

// The payout uses an existing scoped gross reserve, not a fresh
// available-balance transfer. Bind its original recipient and fee snapshots.
func (s *Server) fundsSettlementPayout(ctx context.Context, q identity.FundsQuerier, id int64, summary map[string]string) (json.RawMessage, int64, int64, error) {
	var mid, channel, gross, net, fee int64
	var raw []byte
	var bankProduct string
	e := q.QueryRow(ctx, `SELECT r.merchant_id,p.channel_id,p.gross_amount,p.net_amount,p.fee_amount,
 jsonb_build_object('source','settlement','payout',to_jsonb(p)-'send_state'-'claimed_at'-'cancelled_at',
 'request',to_jsonb(r)-'status'-'reviewed_by'-'reviewed_at','funding',to_jsonb(f),'terms',to_jsonb(st),
 'recipient',to_jsonb(sr),'recipient_config',to_jsonb(c),'transfer',to_jsonb(t)-'status',
 'bank_binding',to_jsonb(bp),
 'attempt',jsonb_build_object('channel_id',a.channel_id,'recipient_cipher',encode(a.recipient_cipher,'hex'))),COALESCE(bp.bank_product,'')
 FROM settlement_payouts p JOIN ledger_requests r ON r.id=p.request_id
 JOIN settlement_fundings f ON f.request_id=p.request_id JOIN settlement_request_terms st ON st.request_id=p.request_id
 JOIN settlement_recipients sr ON sr.request_id=p.request_id JOIN settlement_recipient_configs c ON c.id=sr.config_id
 JOIN transfers t ON t.id=p.transfer_id JOIN provider_transfer_attempts a ON a.transfer_id=t.id LEFT JOIN settlement_bank_payouts bp ON bp.request_id=p.request_id WHERE p.request_id=$1`, id).Scan(&mid, &channel, &gross, &net, &fee, &raw, &bankProduct)
	if errors.Is(e, pgx.ErrNoRows) {
		e = payment.ErrNotFound
	}
	if e != nil {
		return nil, 0, 0, e
	}
	view, e := s.Store.FundsSettlementRecipientView(ctx, q, id)
	if e != nil {
		return nil, 0, 0, e
	}
	summary["funding_source"] = "settlement"
	summary["request_id"] = fmt.Sprint(id)
	summary["channel_id"] = fmt.Sprint(channel)
	summary["gross_amount"] = ledger.Format(gross)
	summary["fee_amount"] = ledger.Format(fee)
	summary["net_amount"] = ledger.Format(net)
	summary["recipient"] = view.AccountMasked
	summary["recipient_name"] = view.NameMasked
	summary["recipient_config_id"] = fmt.Sprint(view.ConfigID)
	summary["available_balance_debit"] = "0.00"
	if bankProduct != "" {
		summary["bank_product"] = bankProduct
		summary["settlement_fee_held"] = ledger.Format(fee)
		summary["fee_policy"] = "hold-and-return-v1"
	}
	if summary["action"] == "settlement-payout-cancel" {
		summary["released_to_available"] = ledger.Format(gross)
	}
	return json.RawMessage(raw), mid, channel, nil
}
