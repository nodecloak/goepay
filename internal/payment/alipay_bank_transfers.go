package payment

import (
	"context"
	"errors"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"time"
)

// A rejected signature/identity/amount query must not make an old successful
// receipt look like a newly verified successful payout. No balance is changed.
func (g ProviderFlow) alipayBankQueryFailure(ctx context.Context, t Transfer, cause error) (Transfer, error) {
	if t.BankProduct != adapter.BankProductAlipay || !errors.Is(cause, adapter.ErrAuthentication) {
		return t, cause
	}
	work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	t.Status = "reversal_pending"
	t.BankLifecycle = "reversal_pending"
	if e := g.Store.markAlipayBankQueryUnverified(work, t.ID); e != nil {
		return t, errors.Join(cause, e)
	}
	current, e := g.Store.Transfer(work, t.MerchantID, t.TransferNo)
	if e != nil {
		return t, errors.Join(cause, e)
	}
	return current, cause
}
func (s *Store) markAlipayBankQueryUnverified(ctx context.Context, id int64) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var state, kind string
	if e = tx.QueryRow(ctx, "SELECT state,provider_kind FROM bank_transfer_lifecycle WHERE transfer_id=$1 FOR UPDATE", id).Scan(&state, &kind); e != nil {
		return e
	}
	if kind != adapter.BankProductAlipay {
		return ErrConflict
	}
	if state == "reversed" || state == "failed" {
		return nil
	}
	if _, e = tx.Exec(ctx, "UPDATE bank_transfer_lifecycle SET state='reversal_pending',review_reason='query_authentication_failed',updated_at=now() WHERE transfer_id=$1", id); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO alipay_bank_observations(transfer_id,provider_state,provider_transfer_no,result_state,reason) VALUES($1,'UNVERIFIED','','reversal_pending','query_authentication_failed')", id); e != nil {
		return e
	}
	if e = s.scheduleBankCheckTx(ctx, tx, id); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func providerBankProduct(p adapter.Provider) string {
	if v, ok := p.(adapter.BankTransferProduct); ok {
		return v.BankTransferProduct()
	}
	return adapter.BankProductWechat // old persisted bank intents predate product metadata.
}
func (s *Store) resolveBankResult(ctx context.Context, t Transfer, r adapter.TransferResult, submission ...bool) error {
	if t.BankProduct == adapter.BankProductAlipay {
		return s.resolveAlipayBankResult(ctx, t, r, len(submission) > 0 && submission[0])
	}
	if t.BankProduct != adapter.BankProductWechat {
		return ErrInvalid
	}
	return s.resolveBankTransferResult(ctx, t, r)
}

func (s *Store) resolveAlipayBankResult(ctx context.Context, t Transfer, r adapter.TransferResult, submission bool) error {
	if t.Type != "bank" || t.BankProduct != adapter.BankProductAlipay || r.TransferNo != fmt.Sprintf("TF%d", t.ID) || r.Amount != t.Amount || len(r.ProviderTransferNo) > 256 || len(r.PayDate) > 64 {
		return ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var local, attempt, providerNo, payDate string
	if _, e = s.lockSettlementTransfer(ctx, tx, t.ID); e != nil {
		return e
	}
	if e = tx.QueryRow(ctx, "SELECT status FROM transfers WHERE id=$1 FOR UPDATE", t.ID).Scan(&local); e != nil {
		return e
	}
	if e = tx.QueryRow(ctx, "SELECT state,provider_transfer_no,pay_date FROM provider_transfer_attempts WHERE transfer_id=$1 FOR UPDATE", t.ID).Scan(&attempt, &providerNo, &payDate); e != nil {
		return e
	}
	var state, kind string
	var settled, returnSeen bool
	var held, returned, returnedService int64
	if e = tx.QueryRow(ctx, "SELECT state,provider_kind,settled,return_observed,service_fee_held,returned_principal,returned_service_fee FROM bank_transfer_lifecycle WHERE transfer_id=$1 FOR UPDATE", t.ID).Scan(&state, &kind, &settled, &returnSeen, &held, &returned, &returnedService); e != nil {
		return e
	}
	if kind != adapter.BankProductAlipay {
		return ErrConflict
	}
	if state == "reversed" || state == "failed" {
		return nil
	}
	var bound bool
	var boundNo string
	var estimate *int64
	if e = tx.QueryRow(ctx, "SELECT submit_bound,submit_order_id,estimated_fee_amount FROM alipay_bank_bindings WHERE transfer_id=$1 FOR UPDATE", t.ID).Scan(&bound, &boundNo, &estimate); e != nil {
		return e
	}
	proof := r.AlipayBank
	reason := ""
	if proof == nil || proof.Product != adapter.BankProductAlipay {
		reason = "alipay_product_not_verified"
	}
	if r.ProviderTransferNo == "" || providerNo != "" && providerNo != r.ProviderTransferNo || boundNo != "" && boundNo != r.ProviderTransferNo {
		reason = "provider_identity_mismatch"
	}
	if reason == "" && submission && proof.SubmissionBound {
		bound = true
		boundNo = r.ProviderTransferNo
	}
	if reason == "" && !bound {
		reason = "original_recipient_binding_missing"
	}
	providerState := r.State
	switch providerState {
	case "SUCCESS", "FAIL", "REFUND", "PROCESSING":
	default:
		providerState = "UNKNOWN"
	}
	if providerState == "REFUND" {
		returnSeen = true
	}
	if reason == "" && proof.EstimatedFeeKnown {
		if proof.EstimatedFee < 0 {
			reason = "invalid_estimated_fee"
		} else {
			n := proof.EstimatedFee
			estimate = &n
		}
	}
	if reason == "" && (providerState == "SUCCESS" || providerState == "FAIL" || providerState == "REFUND") && !proof.AmountKnown {
		reason = "amount_not_verified"
	}
	if reason == "" && providerState == "SUCCESS" && r.PayDate == "" {
		reason = "success_date_missing"
	}
	if reason != "" {
		state = "reversal_pending"
	} else if returnSeen && providerState != "REFUND" {
		state = "reversal_pending"
		reason = "observed_refund_requires_matching_proof"
	} else {
		if providerState == "SUCCESS" || providerState == "FAIL" || providerState == "REFUND" {
			if e = lockBankPostingAccounts(ctx, tx, t); e != nil {
				return e
			}
		}
		switch providerState {
		case "SUCCESS":
			if !settled {
				if local != "pending" && local != "unknown" {
					state = "reversal_pending"
					reason = "unexpected_local_state"
					break
				}
				handled, err := s.postSettlementBank(ctx, tx, t, "principal")
				if err != nil {
					return err
				}
				if !handled {
					if e = post(ctx, tx, fmt.Sprintf("transfer:%d:resolve", t.ID), "alipay bank principal", t.MerchantID, "reserved", 0, "clearing", t.Amount); e != nil {
						return e
					}
				}
				if t.FeeAmount > 0 {
					if e = post(ctx, tx, fmt.Sprintf("transfer:%d:bank-service-fee-hold", t.ID), "alipay bank service fee held", t.MerchantID, "reserved", 0, "bank_service_fee_hold", t.FeeAmount); e != nil {
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
		case "FAIL":
			if settled || local == "succeeded" {
				state = "reversal_pending"
				reason = "fail_is_not_verified_refund"
				break
			}
			if e = s.resolveTransferTx(ctx, tx, t.ID, "failed"); e != nil {
				return e
			}
			local = "failed"
			state = "failed"
		case "REFUND":
			if settled && local == "succeeded" {
				if held != t.FeeAmount {
					return ErrState
				}
				handled, err := s.postSettlementBank(ctx, tx, t, "return-settled")
				if err != nil {
					return err
				}
				if !handled {
					if e = post(ctx, tx, fmt.Sprintf("transfer:%d:bank-return:alipay-principal", t.ID), "verified alipay bank refund principal", 0, "clearing", t.MerchantID, "available", t.Amount); e != nil {
						return e
					}
				}
				if held > 0 {
					if e = post(ctx, tx, fmt.Sprintf("transfer:%d:bank-return:alipay-service-fee", t.ID), "verified alipay bank refund service fee", 0, "bank_service_fee_hold", t.MerchantID, "available", held); e != nil {
						return e
					}
				}
			} else if !settled && (local == "pending" || local == "unknown") {
				handled, err := s.postSettlementBank(ctx, tx, t, "return-reserved")
				if err != nil {
					return err
				}
				if !handled {
					if e = post(ctx, tx, fmt.Sprintf("transfer:%d:bank-return:alipay-reservation", t.ID), "verified alipay bank refund reservation", t.MerchantID, "reserved", t.MerchantID, "available", t.CostAmount); e != nil {
						return e
					}
				}
				if _, e = tx.Exec(ctx, "UPDATE transfers SET status='failed',updated_at=now() WHERE id=$1", t.ID); e != nil {
					return e
				}
				local = "failed"
			} else {
				state = "reversal_pending"
				reason = "unexpected_local_state"
				break
			}
			state = "reversed"
			returned = t.Amount
			returnedService = t.FeeAmount
			held = 0
		case "PROCESSING", "UNKNOWN":
			if settled {
				state = "reversal_pending"
				reason = "nonterminal_after_success"
			} else if state != "reversal_pending" {
				state = "processing"
			}
		}
	}
	if _, e = tx.Exec(ctx, "UPDATE alipay_bank_bindings SET submit_bound=$2,submit_order_id=$3,estimated_fee_amount=$4 WHERE transfer_id=$1", t.ID, bound, boundNo, estimate); e != nil {
		return e
	}
	if attempt != "succeeded" && attempt != "failed" {
		next := "pending"
		if attempt == "unknown" || state == "reversal_pending" || providerState == "UNKNOWN" {
			next = "unknown"
		}
		if local == "succeeded" || local == "failed" {
			next = local
		}
		if providerNo == "" && bound {
			providerNo = boundNo
		}
		if local == "succeeded" && payDate == "" {
			payDate = r.PayDate
		}
		if _, e = tx.Exec(ctx, "UPDATE provider_transfer_attempts SET state=$2,provider_transfer_no=$3,pay_date=$4,updated_at=now() WHERE transfer_id=$1", t.ID, next, providerNo, payDate); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(ctx, "UPDATE bank_transfer_lifecycle SET state=$2,settled=$3,return_observed=$4,service_fee_held=$5,returned_principal=$6,returned_service_fee=$7,last_provider_state=$8,review_reason=$9,next_check_at=CASE WHEN $2 IN ('reversed','failed') THEN NULL ELSE next_check_at END,updated_at=now() WHERE transfer_id=$1", t.ID, state, settled, returnSeen, held, returned, returnedService, providerState, reason); e != nil {
		return e
	}
	var amount, fee any
	if proof != nil && proof.AmountKnown {
		amount = r.Amount
	}
	if proof != nil && proof.EstimatedFeeKnown && proof.EstimatedFee >= 0 {
		fee = proof.EstimatedFee
	}
	if _, e = tx.Exec(ctx, "INSERT INTO alipay_bank_observations(transfer_id,provider_state,provider_transfer_no,amount,estimated_fee_amount,result_state,reason) VALUES($1,$2,$3,$4,$5,$6,$7)", t.ID, providerState, r.ProviderTransferNo, amount, fee, state, reason); e != nil {
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
