package payment

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"time"
)

type settlementBatchSource struct {
	Member       SettlementBatchMember
	Hash         []byte
	FundingState string
}

// Bind only the original request, original fee terms, dedicated reserve journal
// and original recipient identity. Later review/payment/balance/profile changes
// are deliberately excluded from this immutable membership fingerprint.
func (s *Store) settlementBatchSource(ctx context.Context, tx pgx.Tx, id int64) (settlementBatchSource, error) {
	var out settlementBatchSource
	v, err := scanLedgerRequest(tx.QueryRow(ctx, "SELECT "+requestColumns+" FROM ledger_requests WHERE id=$1 AND kind='settlements'", id))
	if err != nil {
		return out, err
	}
	if err = s.verifyScheduledRequestSource(ctx, tx, id); err != nil {
		return out, err
	}
	v, err = attachSettlementTerms(ctx, tx, v)
	if err != nil {
		return out, err
	}
	if v.Settlement == nil || v.Funding == nil || v.Funding.State == "legacy_unscoped" || v.Settlement.GrossAmount != v.Amount || v.Settlement.NetAmount <= 0 || v.Settlement.FeeAmount < 0 || v.Settlement.NetAmount != v.Amount-v.Settlement.FeeAmount {
		return out, ErrConflict
	}
	var valid bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM settlement_funding_invariants WHERE request_id=$1 AND valid IS TRUE)`, id).Scan(&valid); err != nil {
		return out, err
	}
	if !valid {
		return out, ErrConflict
	}
	var journal, account int64
	var at time.Time
	var sourceTx string
	if err = tx.QueryRow(ctx, `SELECT f.account_id,j.id,j.created_at,j.transaction_id::text FROM settlement_fundings f JOIN journals j ON j.id=f.reserve_journal_id WHERE f.request_id=$1 AND j.event_key=$2`, id, fmt.Sprintf("request:%d:reserve", id)).Scan(&account, &journal, &at, &sourceTx); err != nil {
		return out, err
	}
	v, err = s.attachSettlementRecipient(ctx, tx, v)
	if err != nil {
		return out, err
	}
	typ := "unknown"
	var recipientConfigID int64
	var recipientHash []byte
	if v.SettlementRecipient != nil && v.SettlementRecipient.State == "recorded" {
		recipient, hash, e := s.originalSettlementRecipient(ctx, tx, id)
		if e != nil {
			return out, e
		}
		typ, recipientHash = recipient.Type, hash
		recipientConfigID = v.SettlementRecipient.ConfigID
	}
	out.Member = SettlementBatchMember{RequestID: v.ID, MerchantID: v.MerchantID, GrossAmount: v.Amount, FeeAmount: v.Settlement.FeeAmount, NetAmount: v.Settlement.NetAmount, Type: typ, Status: v.Status}
	out.Member.Recipient = v.SettlementRecipient
	out.FundingState = v.Funding.State
	out.Member.FundingState = v.Funding.State
	out.Member.PayoutState = "not_prepared"
	payout, payoutErr := s.settlementPayout(ctx, tx, id)
	if payoutErr == nil {
		out.Member.PayoutState = payout.State
		out.Member.TransferID = payout.TransferID
		// A bank can report a possible return after the principal was paid.
		// Preserve that legitimate historical membership while AllPaid becomes
		// false; the existing signed bank lifecycle still controls any release.
		bankReturnPending := typ == "bank" && payout.Transfer.Type == "bank" && payout.State == "reversal_pending"
		if (v.Funding.State == "paid" || v.Funding.State == "paid_fee_held") && payout.State != "succeeded" && !bankReturnPending {
			return out, ErrConflict
		}
	} else if !errors.Is(payoutErr, ErrNotFound) {
		return out, payoutErr
	}
	out.Hash, err = s.referralDigest("settlement-batches84/original-request", []any{v.ID, v.Kind, v.MerchantID, v.ExternalNo, v.Amount, v.CreatedAt.UTC().Format(time.RFC3339Nano), v.Settlement, account, journal, at.UTC().Format(time.RFC3339Nano), sourceTx, typ, recipientHash, recipientConfigID})
	return out, err
}
