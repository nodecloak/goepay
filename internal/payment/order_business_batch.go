package payment

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
)

type OrderBusinessBatchActor struct {
	User      identity.User
	Token     string
	Delegated bool
}
type OrderBusinessBatchSelection struct {
	TradeNo string `json:"trade_no"`
}
type OrderBusinessBatchInput struct {
	ExternalNo string                        `json:"external_no"`
	Action     string                        `json:"action"`
	Reason     string                        `json:"reason"`
	Items      []OrderBusinessBatchSelection `json:"items"`
}
type orderBusinessBatchSource struct {
	CustomerCharge104                   *orderChargeCommitment104 `json:",omitempty"`
	Position                            int
	OrderID, MerchantID                 int64
	TradeNo, Status                     string
	Amount, MerchantAmount, PlatformFee int64
	RouteFeeProof                       []byte
	RefundAmount                        int64
	OperationAmount, HoldID             int64
	CreatedAt                           time.Time
}
type OrderBusinessBatchItem struct {
	Position    int    `json:"position"`
	OrderID     int64  `json:"order_id,string"`
	MerchantID  int64  `json:"merchant_id,string"`
	TradeNo     string `json:"trade_no"`
	State       string `json:"state"`
	Code        string `json:"code,omitempty"`
	OrderStatus string `json:"order_status,omitempty"`
}
type OrderBusinessBatch struct {
	ID          int64                    `json:"id,string"`
	ExternalNo  string                   `json:"external_no"`
	Action      string                   `json:"action"`
	Reason      string                   `json:"reason"`
	State       string                   `json:"state"`
	CreatedAt   time.Time                `json:"created_at"`
	Items       []OrderBusinessBatchItem `json:"items"`
	Replayed    bool                     `json:"replayed"`
	Fingerprint string                   `json:"fingerprint"`
	Counts      OrderBusinessBatchCounts `json:"counts"`
}
type orderBusinessBatchRecord struct {
	OrderBusinessBatch
	ActorID                                  int64
	ActorName                                string
	RequestHash, SourceHash                  []byte
	Sources                                  []orderBusinessBatchSource
	lastHash                                 []byte
	eventCount                               int64
	authorized                               bool
	authorizationEpoch, authorizationVersion int64
}

func normalizeOrderBusinessBatch(in OrderBusinessBatchInput) (OrderBusinessBatchInput, error) {
	if len(in.ExternalNo) < 8 || len(in.ExternalNo) > 96 || !sharingSafeField(in.ExternalNo, 96, true) || len(in.Items) < 1 || len(in.Items) > 100 {
		return in, ErrInvalid
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if !sharingSafeField(in.Reason, 240, true) {
		return in, ErrInvalid
	}
	switch in.Action {
	case "query", "notify", "refund", "freeze-income", "unfreeze-income":
	default:
		return in, ErrInvalid
	}
	seen := map[string]bool{}
	for _, item := range in.Items {
		if len(item.TradeNo) > 64 || !sharingSafeField(item.TradeNo, 64, true) || seen[item.TradeNo] {
			return in, ErrInvalid
		}
		seen[item.TradeNo] = true
	}
	return in, nil
}
func (s *Store) orderBusinessBatchProof(r orderBusinessBatchRecord) ([]byte, error) {
	return s.referralDigest("order-business-batch185/source", []any{r.ID, r.ActorID, r.ActorName, r.ExternalNo, r.Action, r.Reason, r.CreatedAt, r.RequestHash, r.Sources})
}
func (s *Store) checkOrderBusinessBatchActor(ctx context.Context, tx pgx.Tx, a OrderBusinessBatchActor) error {
	return identity.CheckOriginalAdminSessionTx(ctx, tx, a.User, a.Token, a.Delegated)
}
func (s *Store) PrepareOrderBusinessBatch(ctx context.Context, a OrderBusinessBatchActor, in OrderBusinessBatchInput) (OrderBusinessBatch, error) {
	var out OrderBusinessBatch
	in, e := normalizeOrderBusinessBatch(in)
	if e != nil {
		return out, e
	}
	if a.User.Role != "admin" || a.Delegated {
		return out, identity.ErrAdminSession
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("goepay/order-business-batch185/%d/%s/%s", a.User.ID, a.User.Username, in.ExternalNo)); e != nil {
		return out, e
	}
	requestHash, e := s.referralDigest("order-business-batch185/request", []any{a.User.ID, a.User.Username, in})
	if e != nil {
		return out, e
	}
	var id int64
	e = tx.QueryRow(ctx, `SELECT id FROM order_business_batches WHERE actor_id=$1 AND actor_name=$2 AND external_no=$3`, a.User.ID, a.User.Username, in.ExternalNo).Scan(&id)
	if e == nil {
		r, err := s.readOrderBusinessBatchTx(ctx, tx, a, id)
		if err != nil {
			return out, err
		}
		if !hmac.Equal(requestHash, r.RequestHash) {
			return out, ErrConflict
		}
		r.Replayed = true
		if err = s.checkOrderBusinessBatchActor(ctx, tx, a); err != nil {
			return out, err
		}
		return r.OrderBusinessBatch, tx.Commit(ctx)
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	var anchored bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='order-business-batches' AND operation='prepare' AND actor_id=$1 AND actor_name=$2 AND changes->>'external_no'=$3)`, a.User.ID, a.User.Username, in.ExternalNo).Scan(&anchored); e != nil {
		return out, e
	}
	if anchored {
		return out, ErrConflict
	}
	// Lock platform orders in one deterministic order, while preserving the
	// explicit input positions for the original sequential batch UX.
	trades := make([]string, len(in.Items))
	for i, item := range in.Items {
		trades[i] = item.TradeNo
	}
	sort.Strings(trades)
	rows, e := tx.Query(ctx, `SELECT id FROM orders WHERE trade_no=ANY($1::text[]) ORDER BY id FOR SHARE`, trades)
	if e != nil {
		return out, e
	}
	locked := 0
	lockedIDs := []int64{}
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			break
		}
		locked++
		lockedIDs = append(lockedIDs, id)
	}
	rows.Close()
	if e != nil {
		return out, e
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	if locked != len(in.Items) {
		return out, ErrNotFound
	}
	charges104, e := s.orderChargesTx104(ctx, tx, 0, lockedIDs)
	if e != nil {
		return out, e
	}
	if len(charges104) != len(lockedIDs) {
		return out, ErrConflict
	}
	r := orderBusinessBatchRecord{OrderBusinessBatch: OrderBusinessBatch{ExternalNo: in.ExternalNo, Action: in.Action, Reason: in.Reason, State: "prepared", Items: []OrderBusinessBatchItem{}}, ActorID: a.User.ID, ActorName: a.User.Username, RequestHash: requestHash}
	for i, item := range in.Items {
		o, err := scanOrder(tx.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE trade_no=$1", item.TradeNo))
		if err != nil {
			return out, err
		}
		if in.Action == "query" && o.Status != "pending" || in.Action != "query" && o.Status != "paid" {
			return out, ErrState
		}
		if in.Action == "notify" {
			state, err := orderBusinessBatchNotificationState185(ctx, tx, o.TradeNo)
			if err != nil {
				return out, err
			}
			if o.NotifyURL == "" || state == "succeeded" {
				return out, ErrState
			}
		}
		quote, err := s.validateOrderRouteFeeTerms(ctx, tx, o)
		if err != nil {
			return out, err
		}
		chargeSource104, ok := charges104[o.ID]
		if !ok {
			return out, ErrConflict
		}
		charge104, proof104 := chargeSource104.Charge, chargeSource104.SourceProof
		source := orderBusinessBatchSource{Position: i + 1, OrderID: o.ID, MerchantID: o.MerchantID, TradeNo: o.TradeNo, Status: o.Status, Amount: o.Amount, MerchantAmount: o.MerchantAmount, PlatformFee: o.PlatformFee, CreatedAt: o.CreatedAt}
		if len(proof104) > 0 {
			source.CustomerCharge104 = &orderChargeCommitment104{charge104, proof104}
		}
		if quote != nil {
			source.RouteFeeProof = append([]byte(nil), quote.proof...)
		}
		if in.Action == "refund" {
			var used int64
			var pending bool
			if e = tx.QueryRow(ctx, `SELECT COALESCE(sum(amount) FILTER(WHERE status<>'failed'),0),COALESCE(bool_or(status IN ('pending','unknown')),false) FROM refunds WHERE order_id=$1`, o.ID).Scan(&used, &pending); e != nil {
				return out, e
			}
			if pending || used < 0 || used >= charge104 {
				return out, ErrState
			}
			source.RefundAmount = charge104 - used
			source.OperationAmount = source.RefundAmount
		}
		if in.Action == "freeze-income" {
			var held bool
			if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM order_income_holds WHERE order_id=$1 AND state='held')`, o.ID).Scan(&held); e != nil {
				return out, e
			}
			if held {
				return out, ErrState
			}
			budget, err := s.orderIncomeSource(ctx, tx, o)
			if err != nil {
				return out, err
			}
			source.OperationAmount = budget.Remaining
		}
		if in.Action == "unfreeze-income" {
			h, err := scanIncomeHold(tx.QueryRow(ctx, "SELECT "+incomeHoldColumns+" FROM order_income_holds h JOIN orders o ON o.id=h.order_id WHERE h.order_id=$1 AND h.state='held'", o.ID))
			if err != nil {
				return out, err
			}
			h, _, _, err = s.readIncomeHold(ctx, tx, h)
			if err != nil {
				return out, err
			}
			if h.RefundPendingCount != 0 || h.RemainingAmount <= 0 {
				return out, ErrState
			}
			source.HoldID = h.ID
			source.OperationAmount = h.RemainingAmount
		}
		r.Sources = append(r.Sources, source)
		r.Items = append(r.Items, OrderBusinessBatchItem{Position: i + 1, OrderID: o.ID, MerchantID: o.MerchantID, TradeNo: o.TradeNo, State: "unsent"})
	}
	if e = s.checkOrderBusinessBatchActor(ctx, tx, a); e != nil {
		return out, e
	}
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&r.CreatedAt); e != nil {
		return out, e
	}
	// Sequence allocation is part of the new entity; no old order or ledger
	// row is updated merely to create a preparation.
	if e = tx.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('order_business_batches','id'))`).Scan(&r.ID); e != nil {
		return out, e
	}
	r.SourceHash, e = s.orderBusinessBatchProof(r)
	if e != nil {
		return out, e
	}
	r.Fingerprint = hex.EncodeToString(r.SourceHash)
	r.Counts = OrderBusinessBatchCounts{Total: len(r.Items)}
	if _, e = tx.Exec(ctx, `INSERT INTO order_business_batches(id,actor_id,actor_name,external_no,action,reason,item_count,request_hash,source_hash,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, r.ID, r.ActorID, r.ActorName, r.ExternalNo, r.Action, r.Reason, len(r.Sources), r.RequestHash, r.SourceHash, r.CreatedAt); e != nil {
		return out, e
	}
	for _, source := range r.Sources {
		raw, err := json.Marshal(source)
		if err != nil {
			return out, err
		}
		if _, e = tx.Exec(ctx, `INSERT INTO order_business_batch_items(batch_id,position,order_id,merchant_id,trade_no,source) VALUES($1,$2,$3,$4,$5,$6::jsonb)`, r.ID, source.Position, source.OrderID, source.MerchantID, source.TradeNo, string(raw)); e != nil {
			return out, e
		}
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'order-business-batches',$3,'prepare',jsonb_build_object('external_no',$4::text,'source_hash',encode($5::bytea,'hex'),'action',$6::text,'items',$7::int))`, r.ActorID, r.ActorName, r.ID, r.ExternalNo, r.SourceHash, r.Action, len(r.Sources)); e != nil {
		return out, e
	}
	if e = s.checkOrderBusinessBatchActor(ctx, tx, a); e != nil {
		return out, e
	}
	return r.OrderBusinessBatch, tx.Commit(ctx)
}

func (s *Store) readOrderBusinessBatchTx(ctx context.Context, tx referralReader, a OrderBusinessBatchActor, id int64) (orderBusinessBatchRecord, error) {
	var r orderBusinessBatchRecord
	var count int
	e := tx.QueryRow(ctx, `SELECT id,actor_id,actor_name,external_no,action,reason,item_count,request_hash,source_hash,created_at FROM order_business_batches WHERE id=$1 AND actor_id=$2 AND actor_name=$3`, id, a.User.ID, a.User.Username).Scan(&r.ID, &r.ActorID, &r.ActorName, &r.ExternalNo, &r.Action, &r.Reason, &count, &r.RequestHash, &r.SourceHash, &r.CreatedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		var anchored bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE actor_role='admin' AND actor_id=$1 AND actor_name=$2 AND resource='order-business-batches' AND resource_id=$3 AND operation='prepare')`, a.User.ID, a.User.Username, id).Scan(&anchored); e != nil {
			return r, e
		}
		if anchored {
			return r, ErrConflict
		}
		return r, ErrNotFound
	}
	if e != nil {
		return r, e
	}
	rows, e := tx.Query(ctx, `SELECT position,order_id,merchant_id,trade_no,source FROM order_business_batch_items WHERE batch_id=$1 ORDER BY position`, id)
	if e != nil {
		return r, e
	}
	r.Items = []OrderBusinessBatchItem{}
	for rows.Next() {
		var item OrderBusinessBatchItem
		var raw []byte
		var source orderBusinessBatchSource
		if e = rows.Scan(&item.Position, &item.OrderID, &item.MerchantID, &item.TradeNo, &raw); e != nil {
			break
		}
		if strictOrderBusinessBatchJSON(raw, &source) != nil || source.Position != item.Position || source.OrderID != item.OrderID || source.MerchantID != item.MerchantID || source.TradeNo != item.TradeNo || item.Position != len(r.Items)+1 {
			e = ErrConflict
			break
		}
		item.State = "unsent"
		r.Items = append(r.Items, item)
		r.Sources = append(r.Sources, source)
	}
	rows.Close()
	if e != nil {
		return r, e
	}
	if e = rows.Err(); e != nil {
		return r, e
	}
	if count != len(r.Items) {
		return r, ErrConflict
	}
	proof, e := s.orderBusinessBatchProof(r)
	if e != nil {
		return r, e
	}
	if !hmac.Equal(proof, r.SourceHash) {
		return r, ErrConflict
	}
	var anchors int
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE actor_role='admin' AND actor_id=$1 AND actor_name=$2 AND resource='order-business-batches' AND resource_id=$3 AND operation='prepare' AND changes->>'external_no'=$4 AND changes->>'source_hash'=encode($5::bytea,'hex')`, r.ActorID, r.ActorName, r.ID, r.ExternalNo, r.SourceHash).Scan(&anchors); e != nil {
		return r, e
	}
	if anchors != 1 {
		return r, ErrConflict
	}
	if e = s.loadOrderBusinessBatchEvents(ctx, tx, &r); e != nil {
		return r, e
	}
	return r, nil
}

func (s *Store) OrderBusinessBatch(ctx context.Context, a OrderBusinessBatchActor, id int64) (OrderBusinessBatch, error) {
	var out OrderBusinessBatch
	if id <= 0 {
		return out, ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	r, e := s.readOrderBusinessBatchTx(ctx, tx, a, id)
	if e != nil {
		return out, e
	}
	if e = s.checkOrderBusinessBatchActor(ctx, tx, a); e != nil {
		return out, e
	}
	return r.OrderBusinessBatch, tx.Commit(ctx)
}
