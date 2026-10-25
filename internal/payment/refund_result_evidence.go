package payment

import (
	"context"
	"crypto/hmac"
	"errors"

	"github.com/jackc/pgx/v5"
)

type providerRefundResultEvidence struct {
	RefundID, OperationID, OrderID, ChannelID, Amount              int64
	RefundNo, PriorRefundState, PriorOperationState, State, Source string
	ProofHash                                                      []byte
}

func (s *Store) refundResultProof(r Refund, p providerRefundResultEvidence) ([]byte, error) {
	return s.referralDigest("trusted-provider-refund-result", []any{p.RefundID, p.OperationID, p.OrderID, p.ChannelID, p.Amount, p.RefundNo, p.PriorRefundState, p.PriorOperationState, p.State, p.Source, r.MerchantID, r.MerchantAmount, r.FeeAmount})
}

func (s *Store) recordProviderRefundResult(ctx context.Context, tx pgx.Tx, operationID int64, r Refund, priorOperation, state string) error {
	if state != "succeeded" && state != "failed" {
		return nil
	}
	if r.Status == "succeeded" || r.Status == "failed" {
		return s.verifyProviderRefundResult(ctx, tx, r)
	}
	p := providerRefundResultEvidence{RefundID: r.ID, OperationID: operationID, OrderID: r.OrderID, Amount: r.Amount, RefundNo: r.RefundNo, PriorRefundState: r.Status, PriorOperationState: priorOperation, State: state, Source: "provider"}
	if err := tx.QueryRow(ctx, "SELECT channel_id FROM provider_operations WHERE id=$1 AND refund_id=$2 AND order_id=$3 AND kind='refund'", operationID, r.ID, r.OrderID).Scan(&p.ChannelID); err != nil {
		return err
	}
	return s.insertRefundResultEvidence(ctx, tx, r, p)
}

func (s *Store) insertRefundResultEvidence(ctx context.Context, tx pgx.Tx, r Refund, p providerRefundResultEvidence) error {
	proof, err := s.refundResultProof(r, p)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO provider_refund_result_evidence(refund_id,operation_id,order_id,channel_id,amount,refund_no,prior_refund_state,prior_operation_state,state,proof_hash,source) VALUES($1,NULLIF($2::bigint,0),$3,NULLIF($4::bigint,0),$5,$6,$7,NULLIF($8::text,''),$9,$10,$11)`, p.RefundID, p.OperationID, p.OrderID, p.ChannelID, p.Amount, p.RefundNo, p.PriorRefundState, p.PriorOperationState, p.State, proof, p.Source)
	return err
}

// Historical version 1 refunds are grandfathered explicitly. All new terminal
// refunds, including trusted internal/sandbox resolutions, require a proof.
func (s *Store) verifyProviderRefundResult(ctx context.Context, q referralReader, r Refund) error {
	var p providerRefundResultEvidence
	err := q.QueryRow(ctx, `SELECT refund_id,COALESCE(operation_id,0),order_id,COALESCE(channel_id,0),amount,refund_no,prior_refund_state,COALESCE(prior_operation_state,''),state,proof_hash,source FROM provider_refund_result_evidence WHERE refund_id=$1`, r.ID).Scan(&p.RefundID, &p.OperationID, &p.OrderID, &p.ChannelID, &p.Amount, &p.RefundNo, &p.PriorRefundState, &p.PriorOperationState, &p.State, &p.ProofHash, &p.Source)
	if errors.Is(err, pgx.ErrNoRows) {
		if r.Status == "succeeded" || r.Status == "failed" {
			var version int
			if err = q.QueryRow(ctx, "SELECT result_evidence_version FROM refunds WHERE id=$1", r.ID).Scan(&version); err != nil {
				return err
			}
			if version != 1 {
				return ErrConflict
			}
		}
		return nil
	}
	if err != nil {
		return err
	}
	if p.RefundID != r.ID || p.OrderID != r.OrderID || p.Amount != r.Amount || p.RefundNo != r.RefundNo || p.State != r.Status {
		return ErrConflict
	}
	proof, err := s.refundResultProof(r, p)
	if err != nil || !hmac.Equal(proof, p.ProofHash) {
		return ErrConflict
	}
	var valid bool
	if err = q.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM provider_refund_result_invariants WHERE refund_id=$1 AND valid IS TRUE)", r.ID).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return ErrConflict
	}
	return nil
}

func (s *Store) checkProviderRefundResults(ctx context.Context, tx pgx.Tx, report *LedgerCheck) error {
	if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM provider_refund_result_evidence p WHERE NOT EXISTS(SELECT 1 FROM provider_refund_result_invariants v WHERE v.refund_id=p.refund_id AND v.valid IS TRUE))+(SELECT count(*) FROM refunds r WHERE r.result_evidence_version=2 AND r.status IN ('succeeded','failed') AND NOT EXISTS(SELECT 1 FROM provider_refund_result_evidence p WHERE p.refund_id=r.id))`).Scan(&report.InvalidProviderRefundResults); err != nil {
		return err
	}
	for cursor := int64(0); ; {
		ids, err := sharingEvidenceIDsPage(ctx, tx, `SELECT p.refund_id FROM provider_refund_result_evidence p WHERE p.refund_id>$1 AND EXISTS(SELECT 1 FROM provider_refund_result_invariants v WHERE v.refund_id=p.refund_id AND v.valid IS TRUE) ORDER BY p.refund_id LIMIT 100`, cursor)
		if err != nil {
			return err
		}
		for _, id := range ids {
			r, err := scanRefund(tx.QueryRow(ctx, "SELECT "+refundColumns+" FROM refunds WHERE id=$1", id))
			if err == nil {
				err = s.verifyProviderRefundResult(ctx, tx, r)
			}
			if err != nil {
				if invalidReferralEvidence(err) {
					report.InvalidProviderRefundResults++
				} else {
					return err
				}
			}
			cursor = id
		}
		if len(ids) < 100 {
			break
		}
	}
	return nil
}

func (s *Store) resolveInternalRefund(ctx context.Context, id int64, state string) error {
	if state != "unknown" && state != "succeeded" && state != "failed" {
		return ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var orderID int64
	if err = tx.QueryRow(ctx, "SELECT order_id FROM refunds WHERE id=$1", id).Scan(&orderID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "SELECT id FROM orders WHERE id=$1 FOR UPDATE", orderID); err != nil {
		return err
	}
	r, err := scanRefund(tx.QueryRow(ctx, "SELECT "+refundColumns+" FROM refunds WHERE id=$1", id))
	if err != nil {
		return err
	}
	if err = s.verifyReferralRefundEvidence(ctx, tx, r); err != nil {
		return err
	}
	if r.Status == state {
		return tx.Commit(ctx)
	}
	if r.Status == "succeeded" || r.Status == "failed" {
		return ErrState
	}
	if err = s.resolveRefundTx(ctx, tx, id, state); err != nil {
		return err
	}
	if state == "succeeded" || state == "failed" {
		p := providerRefundResultEvidence{RefundID: r.ID, OrderID: r.OrderID, Amount: r.Amount, RefundNo: r.RefundNo, PriorRefundState: r.Status, State: state, Source: "internal"}
		if err = s.insertRefundResultEvidence(ctx, tx, r, p); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
