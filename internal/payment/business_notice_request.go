package payment

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"goepay/internal/businessnotice"
	"time"
)

// Historical facts exclude current request/payment status, account balances,
// current group and current recipient configuration. Those can change later.
func (s *Store) BusinessNoticeRequestFacts(ctx context.Context, tx pgx.Tx, id int64) (businessnotice.RequestFacts, error) {
	var out businessnotice.RequestFacts
	if ctx == nil || tx == nil || id <= 0 {
		return out, ErrInvalid
	}
	v, err := scanLedgerRequest(tx.QueryRow(ctx, "SELECT "+requestColumns+" FROM ledger_requests WHERE id=$1", id))
	if err != nil {
		return out, err
	}
	if v.Kind != "settlements" && v.Kind != "internal-transfers" {
		return out, ErrState
	}
	out.ID, out.Kind, out.MerchantID, out.Gross, out.Net = v.ID, v.Kind, v.MerchantID, v.Amount, v.Amount
	if v.RecipientID != nil {
		out.RecipientID = *v.RecipientID
	}
	var journal int64
	var journalAt time.Time
	if err = tx.QueryRow(ctx, `SELECT id,transaction_id::text,created_at FROM journals WHERE event_key=$1`, fmt.Sprintf("request:%d:reserve", id)).Scan(&journal, &out.Transaction, &journalAt); err != nil {
		return out, err
	}
	var account int64
	var recipientHash []byte
	if v.Kind == "settlements" {
		v, err = attachSettlementTerms(ctx, tx, v)
		if err != nil {
			return out, err
		}
		if v.Settlement == nil || v.Settlement.GrossAmount != v.Amount || v.Settlement.NetAmount <= 0 || v.Settlement.NetAmount > v.Amount {
			return out, ErrConflict
		}
		out.Net = v.Settlement.NetAmount
		out.DisplayType = "未知"
		var gross, fundingJournal int64
		if err = tx.QueryRow(ctx, `SELECT account_id,gross_amount,reserve_journal_id FROM settlement_fundings WHERE request_id=$1`, id).Scan(&account, &gross, &fundingJournal); err != nil {
			return out, err
		}
		if gross != v.Amount || fundingJournal != journal {
			return out, ErrConflict
		}
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM settlement_recipients WHERE request_id=$1)`, id).Scan(&exists); err != nil {
			return out, err
		}
		if exists {
			var recipient SettlementRecipientInput
			recipient, recipientHash, err = s.originalSettlementRecipient(ctx, tx, id)
			if err != nil {
				return out, err
			}
			switch recipient.Type {
			case "alipay":
				out.DisplayType = "支付宝"
			case "wxpay":
				out.DisplayType = "微信"
			case "bank":
				out.DisplayType = "银行卡"
			default:
				return out, ErrConflict
			}
		}
	}
	out.OriginalHash, err = s.referralDigest("admin-business83/request-facts", []any{v.ID, v.Kind, v.MerchantID, out.RecipientID, v.ExternalNo, v.Amount, v.CreatedAt.UTC().Format(time.RFC3339Nano), out.Net, out.DisplayType, v.Settlement, account, journal, journalAt.UTC().Format(time.RFC3339Nano), out.Transaction, recipientHash})
	return out, err
}
