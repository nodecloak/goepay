package payment

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

type settlementBatchHeader struct {
	View               SettlementBatch
	Actor              TransferBatchActor
	Transaction        string
	MembersHash, Proof []byte
}
type settlementBatchStoredMember struct {
	View              SettlementBatchMember
	SourceHash, Proof []byte
}
type settlementBatchEvent struct {
	ID, BatchID            int64
	Operation, Transaction string
	Actor                  TransferBatchActor
	At                     time.Time
	Proof                  []byte
}

func (s *Store) settlementBatchMemberProof(batch int64, m settlementBatchStoredMember) ([]byte, error) {
	v := m.View
	return s.referralDigest("settlement-batches84/member", []any{batch, v.Position, v.RequestID, v.MerchantID, v.GrossAmount, v.FeeAmount, v.NetAmount, m.SourceHash})
}
func (s *Store) settlementBatchHeaderProof(h settlementBatchHeader) ([]byte, error) {
	v := h.View
	return s.referralDigest("settlement-batches84/header", []any{v.ID, v.Batch, v.ExternalNo, v.Count, v.GrossAmount, v.FeeAmount, v.NetAmount, v.At.UTC().Format(time.RFC3339Nano), h.Transaction, h.Actor, h.MembersHash})
}
func (s *Store) settlementBatchEventProof(h settlementBatchHeader, e settlementBatchEvent) ([]byte, error) {
	return s.referralDigest("settlement-batches84/event", []any{e.ID, e.BatchID, e.Operation, e.Transaction, e.Actor, e.At.UTC().Format(time.RFC3339Nano), h.Proof})
}
func settlementBatchAudit(e settlementBatchEvent) []byte {
	raw, _ := json.Marshal(map[string]string{"event_id": fmt.Sprint(e.ID), "proof": hex.EncodeToString(e.Proof)})
	return raw
}
