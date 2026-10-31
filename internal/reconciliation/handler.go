// Package reconciliation resolves uncertain provider outcomes using authenticated
// read operations. It never submits a payment, close, transfer, or refund.
package reconciliation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"goepay/internal/payment"
	"goepay/internal/platform/jobs"
)

var ErrPending = errors.New("verified provider outcome remains pending")
var ErrJob = errors.New("invalid reconciliation task")

type Handler struct {
	Pool *pgxpool.Pool
	Flow *payment.ProviderFlow
}

func (h Handler) Handle(ctx context.Context, j jobs.Job) error {
	if h.Pool == nil || h.Flow == nil || h.Flow.Store == nil {
		return ErrJob
	}
	if j.Kind == "payment.reconcile" || j.Kind == "channel-sharing.reconcile" || j.Kind == "channel-sharing-finish.reconcile" {
		if e := h.Flow.Store.VerifyScheduledJobClaim(ctx, j); e != nil {
			return e
		}
	}
	switch j.Kind {
	case "channel-sharing-finish.reconcile":
		var id int64
		if e := decodeSingle(j.Payload, "finish_id", &id); e != nil || id <= 0 {
			return ErrJob
		}
		result, e := h.Flow.ReconcileChannelSharingFinish(ctx, id)
		if e != nil {
			return e
		}
		if !result.FundsReleased && result.State != "failed" {
			return ErrPending
		}
		return nil
	case "channel-sharing-return.reconcile":
		var id int64
		if e := decodeSingle(j.Payload, "return_id", &id); e != nil || id <= 0 {
			return ErrJob
		}
		result, e := h.Flow.ReconcileChannelSharingReturn(ctx, id)
		if e != nil {
			return e
		}
		if result.State != "succeeded" && result.State != "failed" {
			return ErrPending
		}
		return nil
	case "channel-sharing.reconcile":
		var id int64
		if e := decodeSingle(j.Payload, "sharing_id", &id); e != nil || id <= 0 {
			return ErrJob
		}
		result, e := h.Flow.ReconcileChannelSharing(ctx, id)
		if e != nil {
			return e
		}
		if !result.FundsReleased && !(result.State == "failed" && !result.Completed) {
			return ErrPending
		}
		return nil
	case "authorization.release.reconcile":
		var trade string
		if e := decodeSingle(j.Payload, "trade_no", &trade); e != nil || trade == "" {
			return ErrJob
		}
		o, e := h.Flow.Store.OrderByTradeNo(ctx, trade)
		if e != nil {
			return e
		}
		// This query resolves only the recorded UA operation. It never calls
		// ReleaseAuthorization or acquires an opportunity to send an unfreeze.
		result, e := h.Flow.QueryAuthorizationRelease(ctx, o.MerchantID, trade)
		if e != nil {
			return e
		}
		if result.State != "succeeded" && result.State != "failed" {
			return ErrPending
		}
		return nil
	case "payment.reconcile":
		var trade string
		if e := decodeSingle(j.Payload, "trade_no", &trade); e != nil || trade == "" {
			return ErrJob
		}
		o, e := h.Flow.Store.OrderByTradeNo(ctx, trade)
		if e != nil {
			return e
		}
		// Already committed terminal outcomes are safe to acknowledge repeatedly.
		if o.Status == "paid" {
			return nil
		}
		result, e := h.Flow.Query(ctx, o.MerchantID, trade)
		if e != nil {
			return e
		}
		o, e = h.Flow.Store.OrderByTradeNo(ctx, trade)
		if e != nil {
			return e
		}
		if o.Status != "paid" {
			switch result.State {
			case "CLOSED", "TRADE_CLOSED", "REVOKED":
				return nil
			}
			return ErrPending
		}
		return nil
	case "refund.reconcile":
		var id int64
		if e := decodeSingle(j.Payload, "refund_id", &id); e != nil || id <= 0 {
			return ErrJob
		}
		var merchant int64
		var number, status string
		if e := h.Pool.QueryRow(ctx, "SELECT merchant_id,refund_no,status FROM refunds WHERE id=$1", id).Scan(&merchant, &number, &status); e != nil {
			return e
		}
		if status == "succeeded" || status == "failed" {
			return nil
		}
		// Resume only an existing durable provider operation. Each combined
		// share needs its own persisted send claim before any provider write.
		r, e := h.Flow.ResumeRefund(ctx, merchant, number)
		if e != nil {
			return e
		}
		if r.Status != "succeeded" && r.Status != "failed" {
			return ErrPending
		}
		return nil
	case "complaint.sync":
		// Provider-backed complaint pull. Gated by complain_open; the channel's
		// built provider lends its signing client to the complaint adapter.
		on, e := h.complainOpen(ctx)
		if e != nil {
			return e
		}
		var channel, num, days int64
		if e := decodeComplaintSync(j.Payload, &channel, &num, &days); e != nil || channel <= 0 {
			return ErrJob
		}
		if !on {
			// Feature turned off after enqueue: acknowledge without pulling.
			return nil
		}
		_, syncErr := h.Flow.SyncComplaints(ctx, channel, int(num), int(days))
		if re := h.Flow.Store.RecordComplaintSyncResult(ctx, channel, syncErr); re != nil {
			return re
		}
		if syncErr != nil {
			// A pending/unknown provider outcome retries; a definitive provider
			// refusal is recorded and acknowledged so the worker moves on.
			if errors.Is(syncErr, payment.ErrInvalid) {
				return nil
			}
			return syncErr
		}
		return nil
	case "transfer.reconcile", "transfer.bank_reconcile":
		var id int64
		if e := decodeSingle(j.Payload, "transfer_id", &id); e != nil || id <= 0 {
			return ErrJob
		}
		var merchant int64
		var number, status string
		if e := h.Pool.QueryRow(ctx, "SELECT merchant_id,transfer_no,status FROM transfers WHERE id=$1", id).Scan(&merchant, &number, &status); e != nil {
			return e
		}
		transfer, e := h.Flow.Store.Transfer(ctx, merchant, number)
		if e != nil {
			return e
		}
		if transfer.Type == "bank" {
			if transfer.Status == "reversed" || transfer.Status == "failed" {
				return nil
			}
			// Guarantee the next authenticated read before acknowledging this
			// generation, including after an upstream timeout. Bank SUCCESS can
			// still become Wechat BANK_FAIL or Alipay REFUND; terminal shortcuts do not
			// apply. This path never sends a new transfer.
			if e := h.Flow.Store.ScheduleBankRecheck(ctx, id); e != nil {
				return e
			}
			_, _ = h.Flow.QueryTransfer(ctx, merchant, number)
			return nil
		}
		if j.Kind == "transfer.bank_reconcile" {
			return ErrJob
		}
		if status == "succeeded" || status == "failed" {
			return nil
		}
		result, e := h.Flow.QueryTransfer(ctx, merchant, number)
		if e != nil {
			return e
		}
		if result.Status != "succeeded" && result.Status != "failed" {
			return ErrPending
		}
		return nil
	default:
		return ErrJob
	}
}

// complainOpen reports whether the complain_open feature gate is on (default
// off). A missing row means off, matching the server's merchant-feature gate.
func (h Handler) complainOpen(ctx context.Context) (bool, error) {
	var value string
	e := h.Pool.QueryRow(ctx, `SELECT body->>'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'='complain_open' ORDER BY id LIMIT 1`).Scan(&value)
	if errors.Is(e, pgx.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	return value == "1", nil
}

// decodeComplaintSync reads the {channel_id,num,days} payload of a
// complaint.sync job. days is optional (older enqueued jobs omit it → 0 =
// default 7-day window).
func decodeComplaintSync(raw []byte, channel, num, days *int64) error {
	if len(raw) > 4096 {
		return ErrJob
	}
	var payload struct {
		ChannelID int64 `json:"channel_id"`
		Num       int64 `json:"num"`
		Days      int64 `json:"days"`
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(&payload); e != nil {
		return ErrJob
	}
	if d.More() {
		return ErrJob
	}
	*channel, *num, *days = payload.ChannelID, payload.Num, payload.Days
	return nil
}

func decodeSingle(raw []byte, key string, target any) error {
	if len(raw) > 4096 {
		return ErrJob
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	token, e := d.Token()
	if e != nil || token != json.Delim('{') {
		return ErrJob
	}
	if !d.More() {
		return ErrJob
	}
	token, e = d.Token()
	if e != nil || token != key {
		return ErrJob
	}
	if e = d.Decode(target); e != nil {
		return ErrJob
	}
	if d.More() {
		return ErrJob
	}
	token, e = d.Token()
	if e != nil || token != json.Delim('}') {
		return ErrJob
	}
	if _, e = d.Token(); e != io.EOF {
		return ErrJob
	}
	return nil
}
