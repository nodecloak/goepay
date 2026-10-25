package payment

import (
	"context"
	"crypto/hmac"
	"errors"
	"github.com/jackc/pgx/v5"
	"goepay/internal/ledger"
	"math"
)

func (s *Store) readSettlementBatchHeader(ctx context.Context, tx pgx.Tx, id int64) (settlementBatchHeader, error) {
	var h settlementBatchHeader
	v := &h.View
	h.Actor.Role = "admin"
	err := tx.QueryRow(ctx, `SELECT id,batch,external_no,item_count,gross_amount,fee_amount,net_amount,actor_id,actor_name,actor_class,created_at,transaction_id::text,members_hash,proof_hash FROM settlement_batches WHERE id=$1`, id).Scan(&v.ID, &v.Batch, &v.ExternalNo, &v.Count, &v.GrossAmount, &v.FeeAmount, &v.NetAmount, &h.Actor.ID, &h.Actor.Name, &h.Actor.Class, &v.At, &h.Transaction, &h.MembersHash, &h.Proof)
	if errors.Is(err, pgx.ErrNoRows) {
		return h, ErrNotFound
	}
	if err != nil {
		return h, err
	}
	v.At = v.At.UTC()
	proof, err := s.settlementBatchHeaderProof(h)
	if err != nil {
		return h, err
	}
	if !h.Actor.valid(0) || v.Count < 1 || v.Count > 100 || len(h.Proof) != 32 || len(h.MembersHash) != 32 || !hmac.Equal(proof, h.Proof) {
		return h, ErrConflict
	}
	return h, nil
}
func (s *Store) readSettlementBatch(ctx context.Context, tx pgx.Tx, id int64) (SettlementBatch, error) {
	h, err := s.readSettlementBatchHeader(ctx, tx, id)
	if err != nil {
		return h.View, err
	}
	v := &h.View
	rows, err := tx.Query(ctx, `SELECT request_id,position,merchant_id,gross_amount,fee_amount,net_amount,source_hash,proof_hash FROM settlement_batch_items WHERE batch_id=$1 ORDER BY position`, id)
	if err != nil {
		return *v, err
	}
	stored := []settlementBatchStoredMember{}
	for rows.Next() {
		var m settlementBatchStoredMember
		a := &m.View
		if err = rows.Scan(&a.RequestID, &a.Position, &a.MerchantID, &a.GrossAmount, &a.FeeAmount, &a.NetAmount, &m.SourceHash, &m.Proof); err != nil {
			rows.Close()
			return *v, err
		}
		stored = append(stored, m)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return *v, err
	}
	if len(stored) != v.Count {
		return *v, ErrConflict
	}
	proofs := make([][]byte, 0, len(stored))
	v.Items = make([]SettlementBatchMember, 0, len(stored))
	v.AllPaid = true
	var gross, fee, net int64
	for i, m := range stored {
		a := &m.View
		proof, e := s.settlementBatchMemberProof(id, m)
		if e != nil {
			return *v, e
		}
		if a.Position != i+1 || len(m.Proof) != 32 || len(m.SourceHash) != 32 || !hmac.Equal(proof, m.Proof) {
			return *v, ErrConflict
		}
		source, e := s.settlementBatchSource(ctx, tx, a.RequestID)
		if errors.Is(e, pgx.ErrNoRows) || errors.Is(e, ErrNotFound) {
			return *v, ErrConflict
		}
		if e != nil {
			return *v, e
		}
		if !hmac.Equal(m.SourceHash, source.Hash) || a.MerchantID != source.Member.MerchantID || a.GrossAmount != source.Member.GrossAmount || a.FeeAmount != source.Member.FeeAmount || a.NetAmount != source.Member.NetAmount {
			return *v, ErrConflict
		}
		if gross > math.MaxInt64-a.GrossAmount || fee > math.MaxInt64-a.FeeAmount || net > math.MaxInt64-a.NetAmount {
			return *v, ErrConflict
		}
		gross += a.GrossAmount
		fee += a.FeeAmount
		net += a.NetAmount
		a.Type, a.Status = source.Member.Type, source.Member.Status
		a.Recipient = source.Member.Recipient
		a.FundingState, a.PayoutState, a.TransferID = source.Member.FundingState, source.Member.PayoutState, source.Member.TransferID
		if a.FundingState != "paid" && a.FundingState != "paid_fee_held" || a.PayoutState != "succeeded" {
			v.AllPaid = false
		}
		v.Items = append(v.Items, *a)
		proofs = append(proofs, m.Proof)
	}
	members, err := s.referralDigest("settlement-batches84/members", proofs)
	if err != nil {
		return *v, err
	}
	if !hmac.Equal(members, h.MembersHash) || gross != v.GrossAmount || fee != v.FeeAmount || net != v.NetAmount {
		return *v, ErrConflict
	}
	v.AllMoney = ledger.Format(net)
	v.Status, err = s.verifySettlementBatchEvents(ctx, tx, h)
	return *v, err
}
func (s *Store) verifySettlementBatchEvents(ctx context.Context, tx pgx.Tx, h settlementBatchHeader) (string, error) {
	rows, err := tx.Query(ctx, `SELECT id,batch_id,operation,actor_id,actor_name,actor_class,created_at,transaction_id::text,proof_hash FROM settlement_batch_events WHERE batch_id=$1 ORDER BY id`, h.View.ID)
	if err != nil {
		return "", err
	}
	events := []settlementBatchEvent{}
	for rows.Next() {
		var e settlementBatchEvent
		e.Actor.Role = "admin"
		if err = rows.Scan(&e.ID, &e.BatchID, &e.Operation, &e.Actor.ID, &e.Actor.Name, &e.Actor.Class, &e.At, &e.Transaction, &e.Proof); err != nil {
			rows.Close()
			return "", err
		}
		e.At = e.At.UTC()
		events = append(events, e)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return "", err
	}
	if len(events) < 1 || len(events) > 3 || events[0].Operation != "create" || events[0].Transaction != h.Transaction || events[0].Actor != h.Actor {
		return "", ErrConflict
	}
	status := "active"
	var auditCount int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE resource='settlement-batches' AND resource_id=$1`, h.View.ID).Scan(&auditCount); err != nil {
		return "", err
	}
	if auditCount != len(events) {
		return "", ErrConflict
	}
	seen := map[string]bool{}
	for _, e := range events {
		proof, err := s.settlementBatchEventProof(h, e)
		if err != nil {
			return "", err
		}
		if seen[e.Operation] || !e.Actor.valid(0) || len(e.Proof) != 32 || !hmac.Equal(proof, e.Proof) || e.At.Before(h.View.At) {
			return "", ErrConflict
		}
		seen[e.Operation] = true
		var valid bool
		if err = tx.QueryRow(ctx, `SELECT count(*)=1 FROM backoffice_audit WHERE resource='settlement-batches' AND resource_id=$1 AND operation=$2 AND actor_role='admin' AND actor_id=$3 AND actor_name=$4 AND changes=$5::jsonb`, e.BatchID, e.Operation, e.Actor.ID, e.Actor.Name, settlementBatchAudit(e)).Scan(&valid); err != nil {
			return "", err
		}
		if !valid {
			return "", ErrConflict
		}
		switch e.Operation {
		case "create":
		case "archive":
			status = "archived"
		case "complete":
			status = "completed"
		default:
			return "", ErrConflict
		}
	}
	return status, nil
}

func (s *Store) SettlementBatch(ctx context.Context, id int64) (SettlementBatch, error) {
	if s == nil || s.pool == nil || ctx == nil || id <= 0 {
		return SettlementBatch{}, ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return SettlementBatch{}, err
	}
	defer tx.Rollback(ctx)
	v, err := s.readSettlementBatch(ctx, tx, id)
	if err != nil {
		return v, err
	}
	return v, tx.Commit(ctx)
}
