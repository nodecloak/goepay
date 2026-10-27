package payment

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func orderBusinessBatchNotificationState185(ctx context.Context, tx pgx.Tx, trade string) (string, error) {
	var state string
	// Pin the same original job until this decision commits. Otherwise the
	// worker can complete it between this read and the active-job lookup,
	// which would enqueue an already delivered notification a second time.
	err := tx.QueryRow(ctx, `SELECT state FROM jobs WHERE kind='payment.notify' AND payload->>'trade_no'=$1 ORDER BY created_at DESC,id DESC LIMIT 1 FOR SHARE`, trade).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return "none", nil
	}
	return state, err
}

// ExecuteOrderBusinessBatchNotifications atomically records each original
// notification intent and its queued result. Delivery stays in the worker.
func (s *Store) ExecuteOrderBusinessBatchNotifications(ctx context.Context, a OrderBusinessBatchActor, id int64, limit int) (OrderBusinessBatch, error) {
	var out OrderBusinessBatch
	if id <= 0 || limit < 1 || limit > 100 {
		return out, ErrInvalid
	}
	for i := 0; i < limit; i++ {
		var more bool
		var e error
		out, more, e = s.queueOrderBusinessBatchNotification185(ctx, a, id)
		if e != nil {
			return out, e
		}
		if !more {
			return out, nil
		}
	}
	return out, nil
}
func (s *Store) queueOrderBusinessBatchNotification185(ctx context.Context, a OrderBusinessBatchActor, id int64) (OrderBusinessBatch, bool, error) {
	var out OrderBusinessBatch
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, false, e
	}
	defer tx.Rollback(ctx)
	r, e := s.lockOrderBusinessBatch185(ctx, tx, a, id)
	if e != nil {
		return out, false, e
	}
	if r.Action != "notify" {
		return out, false, ErrInvalid
	}
	if e = s.checkOrderBusinessBatchActor(ctx, tx, a); e != nil {
		return out, false, e
	}
	position := 0
	for _, item := range r.Items {
		if item.State == "unsent" {
			position = item.Position
			break
		}
	}
	if position == 0 {
		return r.OrderBusinessBatch, false, tx.Commit(ctx)
	}
	source := r.Sources[position-1]
	o, e := scanOrder(tx.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE id=$1 FOR UPDATE", source.OrderID))
	if e != nil {
		return out, false, e
	}
	if o.Status != "paid" || o.NotifyURL == "" || o.MerchantID != source.MerchantID || o.TradeNo != source.TradeNo || o.Amount != source.Amount {
		return out, false, ErrState
	}
	if _, e = s.validateOrderRouteFeeTerms(ctx, tx, o); e != nil {
		return out, false, e
	}
	if e = s.authenticateFrozenCharge104(ctx, tx, o, source.CustomerCharge104); e != nil {
		return out, false, e
	}
	if e = s.appendOrderBusinessBatchEvent(ctx, tx, r, position, "claim", orderBusinessBatchEventFacts{State: "running"}); e != nil {
		return out, false, e
	}
	latest, e := orderBusinessBatchNotificationState185(ctx, tx, o.TradeNo)
	if e != nil {
		return out, false, e
	}
	if latest == "succeeded" {
		r, e = s.readOrderBusinessBatchTx(ctx, tx, a, id)
		if e != nil {
			return out, false, e
		}
		if e = s.appendOrderBusinessBatchEvent(ctx, tx, r, position, "result", orderBusinessBatchEventFacts{State: "succeeded", Code: "original_notification_delivered", OrderStatus: o.Status}); e != nil {
			return out, false, e
		}
		r, e = s.readOrderBusinessBatchTx(ctx, tx, a, id)
		if e != nil {
			return out, false, e
		}
		if e = s.checkOrderBusinessBatchActor(ctx, tx, a); e != nil {
			return out, false, e
		}
		return r.OrderBusinessBatch, r.State != "completed", tx.Commit(ctx)
	}
	requestNo := fmt.Sprintf("order-batch185:%d:%d", r.ID, position)
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte("goepay:notification-resend:v1\x00"+requestNo)))
	var jobID int64
	var state string
	e = tx.QueryRow(ctx, `SELECT j.id,j.state FROM notification_resend_requests n JOIN jobs j ON j.id=n.job_id WHERE n.order_id=$1 AND n.request_hash=$2`, o.ID, digest).Scan(&jobID, &state)
	if errors.Is(e, pgx.ErrNoRows) {
		e = tx.QueryRow(ctx, `SELECT id,state FROM jobs WHERE kind='payment.notify' AND payload->>'trade_no'=$1 AND state IN ('pending','running') ORDER BY created_at DESC,id DESC LIMIT 1`, o.TradeNo).Scan(&jobID, &state)
		if errors.Is(e, pgx.ErrNoRows) {
			tag, err := tx.Exec(ctx, `INSERT INTO notification_send_controls(order_id,last_manual_at) VALUES($1,statement_timestamp()) ON CONFLICT(order_id) DO UPDATE SET last_manual_at=statement_timestamp() WHERE notification_send_controls.last_manual_at<=statement_timestamp()-interval '60 seconds'`, o.ID)
			if err != nil {
				return out, false, err
			}
			if tag.RowsAffected() != 1 {
				return out, false, ErrState
			}
			e = tx.QueryRow(ctx, `INSERT INTO jobs(event_key,kind,payload) VALUES($1,'payment.notify',jsonb_build_object('trade_no',$2::text)) RETURNING id,state`, fmt.Sprintf("notify-manual:%d:%s", o.ID, digest), o.TradeNo).Scan(&jobID, &state)
		}
		if e != nil {
			return out, false, e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO notification_resend_requests(order_id,request_hash,job_id,actor_role,actor_id) VALUES($1,$2,$3,'admin',$4)`, o.ID, digest, jobID, a.User.ID); e != nil {
			return out, false, e
		}
	}
	if e != nil {
		return out, false, e
	}
	r, e = s.readOrderBusinessBatchTx(ctx, tx, a, id)
	if e != nil {
		return out, false, e
	}
	if e = s.appendOrderBusinessBatchEvent(ctx, tx, r, position, "result", orderBusinessBatchEventFacts{State: "queued", Code: "original_notification_queued", OrderStatus: o.Status, JobID: jobID}); e != nil {
		return out, false, e
	}
	r, e = s.readOrderBusinessBatchTx(ctx, tx, a, id)
	if e != nil {
		return out, false, e
	}
	if e = s.checkOrderBusinessBatchActor(ctx, tx, a); e != nil {
		return out, false, e
	}
	return r.OrderBusinessBatch, r.State != "completed", tx.Commit(ctx)
}
