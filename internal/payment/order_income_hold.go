package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

type IncomeHoldActor struct {
	ID          int64
	Name, Class string
}

func (a IncomeHoldActor) valid() bool {
	return a.ID > 0 && a.Name != "" && len(a.Name) <= 128 && (a.Class == "super" || a.Class == "operator") && !strings.ContainsAny(a.Name, "\r\n\x00")
}

type OrderIncomeHold struct {
	FundingID               int64      `json:"funding_id,string"`
	SourceMode              string     `json:"source_mode"`
	RemainingAmount         int64      `json:"remaining_amount,string"`
	RefundReservedAmount    int64      `json:"refund_reserved_amount,string"`
	RefundSucceededAmount   int64      `json:"refund_succeeded_amount,string"`
	RefundPendingCount      int64      `json:"refund_pending_count,string"`
	ID                      int64      `json:"id,string"`
	OrderID                 int64      `json:"order_id,string"`
	MerchantID              int64      `json:"merchant_id,string"`
	TradeNo                 string     `json:"trade_no"`
	RequestNo               string     `json:"external_no"`
	MerchantNet             int64      `json:"merchant_net,string"`
	RefundMerchantAmount    int64      `json:"refund_merchant_amount,string"`
	ChannelSharingAmount    int64      `json:"channel_sharing_amount,string"`
	InternalAllocatedAmount int64      `json:"internal_allocated_amount,string"`
	EvidenceVersion         int        `json:"evidence_version"`
	Amount                  int64      `json:"amount,string"`
	State                   string     `json:"state"`
	CreatedAt               time.Time  `json:"created_at"`
	ReleasedAt              *time.Time `json:"released_at"`
}

const incomeHoldColumns = "h.id,h.order_id,h.merchant_id,o.trade_no,h.request_no,h.merchant_net,h.refund_merchant_amount,h.channel_sharing_amount,h.internal_allocated_amount,h.evidence_version,h.amount,h.state,h.created_at,h.released_at"

func scanIncomeHold(row pgx.Row) (OrderIncomeHold, error) {
	var h OrderIncomeHold
	e := row.Scan(&h.ID, &h.OrderID, &h.MerchantID, &h.TradeNo, &h.RequestNo, &h.MerchantNet, &h.RefundMerchantAmount, &h.ChannelSharingAmount, &h.InternalAllocatedAmount, &h.EvidenceVersion, &h.Amount, &h.State, &h.CreatedAt, &h.ReleasedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrNotFound
	}
	return h, e
}
func (s *Store) OrderIncomeHold(ctx context.Context, merchantID int64, tradeNo string) (OrderIncomeHold, error) {
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return OrderIncomeHold{}, e
	}
	defer tx.Rollback(ctx)
	h, e := scanIncomeHold(tx.QueryRow(ctx, "SELECT "+incomeHoldColumns+" FROM order_income_holds h JOIN orders o ON o.id=h.order_id WHERE o.trade_no=$1 AND h.merchant_id=$2 ORDER BY h.id DESC LIMIT 1", tradeNo, merchantID))
	if e != nil {
		return h, e
	}
	h, _, _, e = s.readIncomeHold(ctx, tx, h)
	if e != nil {
		return h, e
	}
	return h, tx.Commit(ctx)
}

// Called with the original order locked by reservation callers. The database
// trigger remains a backstop for every insert or result update outside them.
func (s *Store) orderIncomeRefundAllowed(ctx context.Context, tx pgx.Tx, orderID int64) error {
	var blocked bool
	if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM order_income_holds h WHERE h.order_id=$1 AND h.state='held' AND NOT EXISTS(SELECT 1 FROM order_income_hold_fundings f WHERE f.hold_id=h.id)) OR EXISTS(SELECT 1 FROM channel_sharing_fundings f WHERE order_id=$1 AND NOT EXISTS(SELECT 1 FROM channel_sharing_fund_releases c WHERE c.order_id=f.order_id))", orderID).Scan(&blocked); e != nil {
		return e
	}
	if blocked {
		return ErrState
	}
	if e := s.verifyOrderSharingReturnEvidence(ctx, tx, orderID); e != nil {
		return e
	}
	return s.verifyOrderSharingFinishEvidence(ctx, tx, orderID)
}

func incomeHoldAudit(ctx context.Context, tx pgx.Tx, h OrderIncomeHold, action string, actor IncomeHoldActor) error {
	if _, e := tx.Exec(ctx, "INSERT INTO order_income_hold_events(hold_id,action,actor_id,actor_name,actor_class) VALUES($1,$2,$3,$4,$5)", h.ID, action, actor.ID, actor.Name, actor.Class); e != nil {
		return e
	}
	raw, e := json.Marshal(map[string]any{"trade_no": h.TradeNo, "hold_id": fmt.Sprint(h.ID), "merchant_id": fmt.Sprint(h.MerchantID), "external_no": h.RequestNo, "amount": fmt.Sprint(h.Amount), "state": h.State})
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'orders',$3,$4,$5)", actor.ID, actor.Name, h.OrderID, action+"-income", raw)
	return e
}

func (s *Store) FreezeOrderIncome(ctx context.Context, merchantID int64, tradeNo, requestNo string, actor IncomeHoldActor) (OrderIncomeHold, error) {
	if !actor.valid() || merchantID <= 0 || requestNo == "" || len(requestNo) > 128 || strings.TrimSpace(requestNo) != requestNo || strings.ContainsAny(requestNo, "\r\n\x00") {
		return OrderIncomeHold{}, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return OrderIncomeHold{}, e
	}
	defer tx.Rollback(ctx)
	o, e := scanOrder(tx.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE merchant_id=$1 AND trade_no=$2 FOR UPDATE", merchantID, tradeNo))
	if e != nil {
		return OrderIncomeHold{}, e
	}
	old, e := scanIncomeHold(tx.QueryRow(ctx, "SELECT "+incomeHoldColumns+" FROM order_income_holds h JOIN orders o ON o.id=h.order_id WHERE h.merchant_id=$1 AND h.request_no=$2", merchantID, requestNo))
	if e == nil {
		if old.OrderID != o.ID {
			return old, ErrConflict
		}
		old, _, _, e = s.readIncomeHold(ctx, tx, old)
		if e != nil {
			return old, e
		}
		return old, tx.Commit(ctx)
	}
	if !errors.Is(e, ErrNotFound) {
		return old, e
	}
	var held bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM order_income_holds WHERE order_id=$1 AND state='held')", o.ID).Scan(&held); e != nil {
		return OrderIncomeHold{}, e
	}
	if held {
		return OrderIncomeHold{}, ErrState
	}
	source, e := s.orderIncomeSource(ctx, tx, o)
	if e != nil {
		return OrderIncomeHold{}, e
	}
	if e = s.orderIncomeRefundAllowed(ctx, tx, o.ID); e != nil {
		return OrderIncomeHold{}, e
	}
	if e = requireFundsAction(ctx, tx, "income-freeze", fmt.Sprintf("%d:%s", merchantID, requestNo)); e != nil {
		return OrderIncomeHold{}, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO accounts(owner_id,kind) VALUES($1,'order_income_hold') ON CONFLICT DO NOTHING", merchantID); e != nil {
		return OrderIncomeHold{}, e
	}
	h, e := scanIncomeHold(tx.QueryRow(ctx, "WITH inserted AS (INSERT INTO order_income_holds(order_id,merchant_id,request_no,merchant_net,refund_merchant_amount,channel_sharing_amount,internal_allocated_amount,amount) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING *) SELECT "+incomeHoldColumns+" FROM inserted h JOIN orders o ON o.id=h.order_id", o.ID, merchantID, requestNo, source.MerchantNet, source.RefundMerchantAmount, source.ChannelSharingAmount, source.InternalAllocatedAmount, source.Remaining))
	if e != nil {
		return h, e
	}
	if e = incomeHoldAudit(ctx, tx, h, "freeze", actor); e != nil {
		return h, e
	}
	if _, e = s.createIncomeFunding(ctx, tx, o, h, actor, "fresh", requestNo); e != nil {
		return h, e
	}
	h, _, _, e = s.readIncomeHold(ctx, tx, h)
	if e != nil {
		return h, e
	}
	if e = checkOrderBusinessBatch185LocalFinal(ctx, tx); e != nil {
		return h, e
	}
	return h, tx.Commit(ctx)
}
func (s *Store) UnfreezeOrderIncome(ctx context.Context, merchantID int64, tradeNo string, holdID int64, actor IncomeHoldActor) (OrderIncomeHold, error) {
	if !actor.valid() || merchantID <= 0 || holdID <= 0 {
		return OrderIncomeHold{}, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return OrderIncomeHold{}, e
	}
	defer tx.Rollback(ctx)
	var orderID int64
	if e = tx.QueryRow(ctx, "SELECT id FROM orders WHERE merchant_id=$1 AND trade_no=$2 FOR UPDATE", merchantID, tradeNo).Scan(&orderID); e != nil {
		return OrderIncomeHold{}, ErrNotFound
	}
	h, e := scanIncomeHold(tx.QueryRow(ctx, "SELECT "+incomeHoldColumns+" FROM order_income_holds h JOIN orders o ON o.id=h.order_id WHERE h.id=$1 AND h.order_id=$2 AND h.merchant_id=$3 FOR UPDATE OF h", holdID, orderID, merchantID))
	if e != nil {
		return h, e
	}
	h, f, _, e := s.readIncomeHold(ctx, tx, h)
	if e != nil {
		return h, e
	}
	if h.State == "released" {
		return h, tx.Commit(ctx)
	}
	if e = requireFundsAction(ctx, tx, "income-unfreeze", fmt.Sprintf("%d", h.ID)); e != nil {
		return h, e
	}
	if f.HoldID > 0 {
		if e = s.releaseIncomeFunding(ctx, tx, h, f, actor); e != nil {
			return h, e
		}
	} else if e = post(ctx, tx, fmt.Sprintf("order-income:%d:unfreeze", h.ID), "unfreeze original legacy income", merchantID, "order_income_hold", merchantID, "available", h.Amount); e != nil {
		return h, e
	}
	if _, e = tx.Exec(ctx, "UPDATE order_income_holds SET state='released',released_at=statement_timestamp() WHERE id=$1", h.ID); e != nil {
		return h, e
	}
	h.State = "released"
	if e = incomeHoldAudit(ctx, tx, h, "unfreeze", actor); e != nil {
		return h, e
	}
	h, e = scanIncomeHold(tx.QueryRow(ctx, "SELECT "+incomeHoldColumns+" FROM order_income_holds h JOIN orders o ON o.id=h.order_id WHERE h.id=$1", h.ID))
	if e != nil {
		return h, e
	}
	h, _, _, e = s.readIncomeHold(ctx, tx, h)
	if e != nil {
		return h, e
	}
	if e = checkOrderBusinessBatch185LocalFinal(ctx, tx); e != nil {
		return h, e
	}
	return h, tx.Commit(ctx)
}
