package payment

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"goepay/internal/ledger"
	"math"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type SettlementBatchInput struct {
	ExternalNo string
	RequestIDs []int64
}
type SettlementBatchMember struct {
	RequestID    int64                    `json:"request_id,string"`
	MerchantID   int64                    `json:"uid,string"`
	GrossAmount  int64                    `json:"gross_amount_cents,string"`
	FeeAmount    int64                    `json:"fee_amount_cents,string"`
	NetAmount    int64                    `json:"net_amount_cents,string"`
	Position     int                      `json:"position"`
	Type         string                   `json:"type"`
	Status       string                   `json:"status"`
	FundingState string                   `json:"funding_state"`
	PayoutState  string                   `json:"payout_state"`
	TransferID   int64                    `json:"transfer_id,string"`
	Recipient    *SettlementRecipientView `json:"recipient,omitempty"`
}
type SettlementBatch struct {
	ID          int64                   `json:"id,string"`
	Batch       string                  `json:"batch"`
	ExternalNo  string                  `json:"external_no"`
	AllMoney    string                  `json:"allmoney"`
	Count       int                     `json:"count"`
	GrossAmount int64                   `json:"gross_amount_cents,string"`
	FeeAmount   int64                   `json:"fee_amount_cents,string"`
	NetAmount   int64                   `json:"net_amount_cents,string"`
	At          time.Time               `json:"time"`
	Status      string                  `json:"status"`
	AllPaid     bool                    `json:"all_paid"`
	Items       []SettlementBatchMember `json:"items"`
}

func settlementBatchIDs(in SettlementBatchInput) ([]int64, error) {
	if in.ExternalNo == "" || len(in.ExternalNo) > 128 || !utf8.ValidString(in.ExternalNo) || strings.TrimSpace(in.ExternalNo) != in.ExternalNo || len(in.RequestIDs) < 1 || len(in.RequestIDs) > 100 {
		return nil, ErrInvalid
	}
	for _, c := range in.ExternalNo {
		if c < 32 || c == 127 {
			return nil, ErrInvalid
		}
	}
	ids := append([]int64(nil), in.RequestIDs...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for i, id := range ids {
		if id <= 0 || i > 0 && ids[i-1] == id {
			return nil, ErrInvalid
		}
	}
	return ids, nil
}
func (s *Store) CreateSettlementBatch(ctx context.Context, in SettlementBatchInput, actor TransferBatchActor) (SettlementBatch, error) {
	var empty SettlementBatch
	if s == nil || s.pool == nil || ctx == nil || actor.Role != "admin" || !actor.valid(0) {
		return empty, ErrInvalid
	}
	ids, err := settlementBatchIDs(in)
	if err != nil {
		return empty, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('settlement-batches84:'||$1,0))`, in.ExternalNo); err != nil {
		return empty, err
	}
	var old int64
	err = tx.QueryRow(ctx, `SELECT id FROM settlement_batches WHERE external_no=$1`, in.ExternalNo).Scan(&old)
	if err == nil {
		previous, e := s.readSettlementBatch(ctx, tx, old)
		if e != nil {
			return empty, e
		}
		if previous.Count != len(ids) {
			return empty, ErrConflict
		}
		for i, item := range previous.Items {
			if item.RequestID != ids[i] {
				return empty, ErrConflict
			}
		}
		return previous, tx.Commit(ctx)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return empty, err
	}
	// Match the existing settlement path: merchant locks precede request locks.
	rows, err := tx.Query(ctx, `SELECT id FROM merchants WHERE id IN(SELECT merchant_id FROM ledger_requests WHERE id=ANY($1::bigint[])) ORDER BY id FOR NO KEY UPDATE`, ids)
	if err != nil {
		return empty, err
	}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return empty, err
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return empty, err
	}
	var locked int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM(SELECT id FROM ledger_requests WHERE id=ANY($1::bigint[]) ORDER BY merchant_id,id FOR UPDATE) locked`, ids).Scan(&locked); err != nil {
		return empty, err
	}
	if locked != len(ids) {
		return empty, ErrInvalid
	}
	var assigned bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM settlement_batch_items WHERE request_id=ANY($1::bigint[]))`, ids).Scan(&assigned); err != nil {
		return empty, err
	}
	if assigned {
		return empty, ErrConflict
	}
	header := settlementBatchHeader{Actor: actor}
	v := &header.View
	v.ExternalNo = in.ExternalNo
	v.Count = len(ids)
	v.Status = "active"
	v.Items = make([]SettlementBatchMember, 0, len(ids))
	if err = tx.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('settlement_batches','id')),clock_timestamp(),pg_current_xact_id()::text`).Scan(&v.ID, &v.At, &header.Transaction); err != nil {
		return empty, err
	}
	v.At = v.At.UTC()
	v.Batch = v.At.In(time.FixedZone("UTC+8", 28800)).Format("20060102") + fmt.Sprintf("%012d", v.ID)
	members := make([]settlementBatchStoredMember, 0, len(ids))
	proofs := make([][]byte, 0, len(ids))
	for i, id := range ids {
		source, e := s.settlementBatchSource(ctx, tx, id)
		if errors.Is(e, pgx.ErrNoRows) || errors.Is(e, ErrNotFound) {
			return empty, ErrInvalid
		}
		if e != nil {
			return empty, e
		}
		if source.FundingState != "reserved" || source.Member.Status != "pending" && source.Member.Status != "approved" {
			return empty, ErrState
		}
		m := settlementBatchStoredMember{View: source.Member, SourceHash: source.Hash}
		m.View.Position = i + 1
		m.Proof, e = s.settlementBatchMemberProof(v.ID, m)
		if e != nil {
			return empty, e
		}
		if v.GrossAmount > math.MaxInt64-m.View.GrossAmount || v.FeeAmount > math.MaxInt64-m.View.FeeAmount || v.NetAmount > math.MaxInt64-m.View.NetAmount {
			return empty, ErrInvalid
		}
		v.GrossAmount += m.View.GrossAmount
		v.FeeAmount += m.View.FeeAmount
		v.NetAmount += m.View.NetAmount
		members = append(members, m)
		proofs = append(proofs, m.Proof)
		v.Items = append(v.Items, m.View)
	}
	v.AllMoney = ledger.Format(v.NetAmount)
	header.MembersHash, err = s.referralDigest("settlement-batches84/members", proofs)
	if err != nil {
		return empty, err
	}
	header.Proof, err = s.settlementBatchHeaderProof(header)
	if err != nil {
		return empty, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO settlement_batches(id,batch,external_no,item_count,gross_amount,fee_amount,net_amount,actor_id,actor_name,actor_class,created_at,transaction_id,members_hash,proof_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::xid8,$13,$14)`, v.ID, v.Batch, v.ExternalNo, v.Count, v.GrossAmount, v.FeeAmount, v.NetAmount, actor.ID, actor.Name, actor.Class, v.At, header.Transaction, header.MembersHash, header.Proof); err != nil {
		return empty, err
	}
	for _, m := range members {
		a := m.View
		if _, err = tx.Exec(ctx, `INSERT INTO settlement_batch_items(batch_id,request_id,position,merchant_id,gross_amount,fee_amount,net_amount,source_hash,proof_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, v.ID, a.RequestID, a.Position, a.MerchantID, a.GrossAmount, a.FeeAmount, a.NetAmount, m.SourceHash, m.Proof); err != nil {
			return empty, err
		}
	}
	if err = s.appendSettlementBatchEvent(ctx, tx, header, "create", actor); err != nil {
		return empty, err
	}
	return *v, tx.Commit(ctx)
}
