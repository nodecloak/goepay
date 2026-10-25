package payment

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
	"time"
)

var ErrBankReversalPending = errors.New("bank return requires verified reconciliation")

func (s *Store) resolveBankTransferResult(ctx context.Context, t Transfer, r adapter.TransferResult) error {
	if t.Type != "bank" || t.BankProduct != adapter.BankProductWechat || r.TransferNo != fmt.Sprintf("TF%d", t.ID) || r.Amount != t.Amount || len(r.ProviderTransferNo) > 256 || len(r.PayDate) > 64 {
		return ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = s.lockSettlementTransfer(ctx, tx, t.ID); e != nil {
		return e
	}
	var local, currentAttempt, providerNo, payDate string
	if e = tx.QueryRow(ctx, "SELECT status FROM transfers WHERE id=$1 FOR UPDATE", t.ID).Scan(&local); e != nil {
		return e
	}
	if e = tx.QueryRow(ctx, "SELECT state,provider_transfer_no,pay_date FROM provider_transfer_attempts WHERE transfer_id=$1 FOR UPDATE", t.ID).Scan(&currentAttempt, &providerNo, &payDate); e != nil {
		return e
	}
	var state, kind string
	var fee *int64
	var settled, returnSeen bool
	var held, returned, returnedService, returnedBank int64
	if e = tx.QueryRow(ctx, "SELECT state,bank_fee_amount,settled,return_observed,service_fee_held,returned_principal,returned_service_fee,returned_bank_fee,provider_kind FROM bank_transfer_lifecycle WHERE transfer_id=$1 FOR UPDATE", t.ID).Scan(&state, &fee, &settled, &returnSeen, &held, &returned, &returnedService, &returnedBank, &kind); e != nil {
		return e
	}
	if kind != adapter.BankProductWechat {
		return ErrInvalid
	}
	if state == "reversed" || state == "failed" || state == "cancelled" {
		return nil
	}
	providerState := r.State
	switch providerState {
	case "PROCESSING", "SUCCESS", "FAILED", "BANK_FAIL":
	default:
		providerState = "UNKNOWN"
	}
	reason := ""
	if providerState == "BANK_FAIL" {
		returnSeen = true
	}
	if r.ProviderTransferNo == "" || providerNo != "" && providerNo != r.ProviderTransferNo {
		reason = "provider_identity_mismatch"
	}
	if !r.BankFeeKnown || r.BankFeeAmount < 0 {
		reason = "bank_fee_unknown"
	} else if fee != nil && *fee != r.BankFeeAmount {
		reason = "bank_fee_mismatch"
	}
	if reason == "" && fee == nil {
		amount := r.BankFeeAmount
		fee = &amount
	}
	if reason != "" {
		state = "reversal_pending"
	} else if returnSeen && providerState != "BANK_FAIL" {
		state = "reversal_pending"
		reason = "observed_return_requires_matching_proof"
	} else {
		if providerState == "SUCCESS" || providerState == "FAILED" || providerState == "BANK_FAIL" {
			if e = lockBankPostingAccounts(ctx, tx, t); e != nil {
				return e
			}
		}
		switch providerState {
		case "SUCCESS":
			if local == "failed" {
				state = "reversal_pending"
				reason = "unexpected_local_terminal_state"
				break
			}
			if !settled {
				if local == "succeeded" {
					state = "reversal_pending"
					reason = "legacy_bank_settlement_requires_review"
					break
				}
				handled, err := s.postSettlementBank(ctx, tx, t, "principal")
				if err != nil {
					return err
				}
				if !handled {
					if e = post(ctx, tx, fmt.Sprintf("transfer:%d:resolve", t.ID), "bank transfer principal", t.MerchantID, "reserved", 0, "clearing", t.Amount); e != nil {
						return e
					}
				}
				if t.FeeAmount > 0 {
					if e = post(ctx, tx, fmt.Sprintf("transfer:%d:bank-service-fee-hold", t.ID), "bank transfer service fee held", t.MerchantID, "reserved", 0, "bank_service_fee_hold", t.FeeAmount); e != nil {
						return e
					}
				}
				if *fee > 0 {
					if e = post(ctx, tx, fmt.Sprintf("transfer:%d:bank-provider-fee", t.ID), "bank provider fee expense", 0, "bank_fee_expense", 0, "clearing", *fee); e != nil {
						return e
					}
				}
				if _, e = tx.Exec(ctx, "UPDATE transfers SET status='succeeded',updated_at=now() WHERE id=$1", t.ID); e != nil {
					return e
				}
				settled = true
				held = t.FeeAmount
				local = "succeeded"
			}
			state = "succeeded"
		case "FAILED":
			if settled || local == "succeeded" {
				state = "reversal_pending"
				reason = "failed_is_not_bank_return"
				break
			}
			if e = s.resolveTransferTx(ctx, tx, t.ID, "failed"); e != nil {
				return e
			}
			state = "failed"
			local = "failed"
		case "BANK_FAIL":
			if settled && local == "succeeded" {
				handled, err := s.postSettlementBank(ctx, tx, t, "return-settled")
				if err != nil {
					return err
				}
				if !handled {
					if e = post(ctx, tx, fmt.Sprintf("transfer:%d:bank-return:principal", t.ID), "verified bank return principal", 0, "clearing", t.MerchantID, "available", t.Amount); e != nil {
						return e
					}
				}
				if held != t.FeeAmount {
					return ErrState
				}
				if held > 0 {
					if e = post(ctx, tx, fmt.Sprintf("transfer:%d:bank-return:service-fee", t.ID), "verified bank return service fee", 0, "bank_service_fee_hold", t.MerchantID, "available", held); e != nil {
						return e
					}
				}
				if *fee > 0 {
					if e = post(ctx, tx, fmt.Sprintf("transfer:%d:bank-return:provider-fee", t.ID), "verified bank provider fee return", 0, "clearing", 0, "bank_fee_expense", *fee); e != nil {
						return e
					}
				}
				returnedBank = *fee
			} else if !settled && (local == "pending" || local == "unknown") {
				handled, err := s.postSettlementBank(ctx, tx, t, "return-reserved")
				if err != nil {
					return err
				}
				if !handled {
					if e = post(ctx, tx, fmt.Sprintf("transfer:%d:bank-return:reservation", t.ID), "verified bank return original reservation", t.MerchantID, "reserved", t.MerchantID, "available", t.CostAmount); e != nil {
						return e
					}
				}
				if _, e = tx.Exec(ctx, "UPDATE transfers SET status='failed',updated_at=now() WHERE id=$1", t.ID); e != nil {
					return e
				}
				local = "failed"
			} else {
				state = "reversal_pending"
				reason = "unexpected_local_terminal_state"
				break
			}
			state = "reversed"
			returned = t.Amount
			returnedService = t.FeeAmount
			held = 0
		case "PROCESSING", "UNKNOWN":
			if state != "succeeded" && state != "reversal_pending" {
				state = "processing"
			}
		}
	}
	if currentAttempt != "succeeded" && currentAttempt != "failed" {
		attemptState := "pending"
		if currentAttempt == "unknown" || state == "reversal_pending" || providerState == "UNKNOWN" {
			attemptState = "unknown"
		}
		if local == "succeeded" || local == "failed" {
			attemptState = local
		}
		if providerNo == "" && reason != "provider_identity_mismatch" {
			providerNo = r.ProviderTransferNo
		}
		if local == "succeeded" && payDate == "" {
			payDate = r.PayDate
		}
		if _, e = tx.Exec(ctx, "UPDATE provider_transfer_attempts SET state=$2,provider_transfer_no=$3,pay_date=$4,updated_at=now() WHERE transfer_id=$1", t.ID, attemptState, providerNo, payDate); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(ctx, `UPDATE bank_transfer_lifecycle SET state=$2,bank_fee_amount=$3,settled=$4,return_observed=$5,service_fee_held=$6,returned_principal=$7,returned_service_fee=$8,returned_bank_fee=$9,last_provider_state=$10,review_reason=$11,next_check_at=CASE WHEN $2 IN ('reversed','failed') THEN NULL ELSE next_check_at END,updated_at=now() WHERE transfer_id=$1`, t.ID, state, fee, settled, returnSeen, held, returned, returnedService, returnedBank, providerState, reason); e != nil {
		return e
	}
	var observedFee *int64
	if r.BankFeeKnown && r.BankFeeAmount >= 0 {
		observedFee = &r.BankFeeAmount
	}
	if _, e = tx.Exec(ctx, "INSERT INTO bank_transfer_observations(transfer_id,provider_state,provider_transfer_no,amount,bank_fee_amount,result_state,reason) VALUES($1,$2,$3,$4,$5,$6,$7)", t.ID, providerState, r.ProviderTransferNo, r.Amount, observedFee, state, reason); e != nil {
		return e
	}
	if e = s.scheduleBankCheckTx(ctx, tx, t.ID); e != nil {
		return e
	}
	if local == "succeeded" {
		s.recordMerchantNoticeSettlement(ctx, tx, t.ID)
	}
	return tx.Commit(ctx)
}

// ScheduleBankRecheck durably guarantees a single next six-hour query before a
// worker acknowledges its current check. Final returns never schedule another.
func (s *Store) ScheduleBankRecheck(ctx context.Context, id int64) error {
	if id <= 0 {
		return ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = s.scheduleBankCheckTx(ctx, tx, id); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Store) scheduleBankCheckTx(ctx context.Context, tx pgx.Tx, id int64) error {
	var state string
	var due *time.Time
	var generation int64
	var now time.Time
	if e := tx.QueryRow(ctx, "SELECT state,next_check_at,generation,now() FROM bank_transfer_lifecycle WHERE transfer_id=$1 FOR UPDATE", id).Scan(&state, &due, &generation, &now); e != nil {
		return e
	}
	if state == "reversed" || state == "failed" || state == "cancelled" {
		return nil
	}
	if due == nil || !due.After(now) {
		next := now.Add(6 * time.Hour)
		due = &next
		generation++
		if _, e := tx.Exec(ctx, "UPDATE bank_transfer_lifecycle SET next_check_at=$2,generation=$3 WHERE transfer_id=$1", id, due, generation); e != nil {
			return e
		}
	}
	_, e := tx.Exec(ctx, "INSERT INTO jobs(event_key,kind,payload,run_at) VALUES($1,'transfer.bank_reconcile',jsonb_build_object('transfer_id',$2::bigint),$3) ON CONFLICT DO NOTHING", fmt.Sprintf("bank:%d:recheck:%d", id, generation), id, due)
	return e
}
