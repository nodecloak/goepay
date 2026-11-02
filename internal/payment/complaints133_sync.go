package payment

import (
	"context"
	"errors"
	"strconv"
	"time"

	provider "goepay/internal/adapters/payment"

	"github.com/jackc/pgx/v5"
)

// Complaint sync: a provider-backed pull the job worker runs with provider
// access. ProviderFlow holds the channel's built provider in Providers[channel]
// exactly as the payment path does; ComplaintAdapterFromProvider lends its
// verified signing client to the complaint adapter, so no config is re-read and
// no new signing path is introduced. The generic scheduler, which has no
// provider client, only ENQUEUES these jobs (see scanComplaintSyncTasks).
//
// Upsert is idempotent by the provider complaint id: identity/order/amount are
// append-only (the 000133 forward-only trigger), status/last-seen only advance.
// Failures accumulate on complaint_tasks133.consecutive_failures; at
// complaintMaxFailures the task auto-disables and an admin notice is recorded,
// mirroring the original cron's MAX_FAILURES stop.

const complaintMaxFailures = 10

// SyncComplaints pulls one page for a channel and upserts the results. It is
// called from the job worker (provider access). num is 1..200 (the task's
// per-run page size). days is the 获取条件 window: when > 0 the pull covers the
// last `days` days (UTC+8 business days, capped to the provider's reach);
// days <= 0 keeps the original rolling 7-day window. Returns the number of
// complaints upserted.
func (g ProviderFlow) SyncComplaints(ctx context.Context, channelID int64, num, days int) (int, error) {
	if g.Store == nil || channelID <= 0 {
		return 0, ErrInvalid
	}
	if num < 1 || num > 200 {
		num = 20
	}
	p := g.Providers[channelID]
	if p == nil {
		return 0, provider.ErrComplaintUnsupported
	}
	adapter, e := provider.ComplaintAdapterFromProvider(p)
	if e != nil {
		return 0, e
	}
	// Window: the original syncs recent complaints over a rolling 7-day window
	// ending today (UTC+8 business day). A task's 获取条件 (days > 0) widens or
	// narrows that span to the admin's setting; days <= 0 keeps the 7-day default.
	span := 7
	if days > 0 {
		span = days
	}
	loc := time.FixedZone("CST", 8*3600)
	today := time.Now().In(loc)
	begin := today.AddDate(0, 0, -(span - 1)).Format("2006-01-02")
	end := today.Format("2006-01-02")
	page, e := provider.SyncComplaintsPage(ctx, adapter, begin, end, 1, num)
	if e != nil {
		return 0, e
	}
	upserted := 0
	for i := range page.Items {
		if e := g.Store.upsertComplaint133(ctx, channelID, page.Items[i], p); e != nil {
			return upserted, e
		}
		if e := g.Store.autoHandleComplaint(ctx, channelID, page.Items[i]); e != nil {
			return upserted, e
		}
		upserted++
	}
	return upserted, nil
}

// upsertComplaint133 inserts or forward-updates one complaint. (pay_type,
// third_id) is the idempotency key; the 000133 trigger enforces that identity,
// trade linkage and a once-known amount never change and state never moves
// backward, so a re-sync of the same complaint is a safe no-op or a
// forward-only status/last-seen advance.
func (s *Store) upsertComplaint133(ctx context.Context, channelID int64, rec provider.ComplaintRecord, providers ...provider.Provider) error {
	if rec.ThirdID == "" || (rec.PayType != "wxpay" && rec.PayType != "alipay") {
		return ErrInvalid
	}
	local := localComplaintState(rec.PayType, rec.Status)
	var detail []byte
	if len(rec.Raw) > 0 {
		detail = []byte(rec.Raw)
	}
	tradeNos := rec.TradeNos
	if tradeNos == nil {
		tradeNos = []string{}
	}
	// The upsert itself is unchanged; it now runs in a transaction only so the
	// append of a subscription event (for a new complaint or a forward state
	// change on a known-owner complaint) is atomic with the row it reports.
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	// Upstream merchant order numbers are OUR platform trade_no. Merchant
	// out_trade_no is scoped per merchant and must never be matched globally.
	var p provider.Provider
	if len(providers) == 1 {
		p = providers[0]
	}
	owner, verifiedTrades, e := s.complaintOwner231(ctx, tx, channelID, rec.PayType, tradeNos, p)
	if e != nil {
		return e
	}
	var sourceReady bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='complaints133'::regclass AND attname='notification_transaction231' AND NOT attisdropped)`).Scan(&sourceReady); e != nil {
		return e
	}
	// Old migration fixtures stay readable. New runtime installations freeze
	// the original birth transaction on INSERT, never on a later resync.
	extraColumn, extraValue := "", ""
	if sourceReady {
		extraColumn = ",notification_transaction231"
		extraValue = ",pg_current_xact_id()"
	}
	var complaintID, ownerID int64
	var resultState string
	e = tx.QueryRow(ctx, `INSERT INTO complaints133
 (pay_type,third_id,channel_id,owner_id,trade_nos,third_mch_id,category,title,content,complaint_times,amount_cents,provider_status,local_state,phone,detail,last_seen_at`+extraColumn+`)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,now()`+extraValue+`)
 ON CONFLICT (pay_type,third_id) DO UPDATE SET
   provider_status=EXCLUDED.provider_status,
   local_state=CASE WHEN jsonb_build_object('open',0,'processing',1,'done',2)->>EXCLUDED.local_state >= jsonb_build_object('open',0,'processing',1,'done',2)->>complaints133.local_state THEN EXCLUDED.local_state ELSE complaints133.local_state END,
   title=COALESCE(EXCLUDED.title,complaints133.title),
   content=COALESCE(EXCLUDED.content,complaints133.content),
   phone=COALESCE(EXCLUDED.phone,complaints133.phone),
   detail=COALESCE(EXCLUDED.detail,complaints133.detail),
   amount_cents=COALESCE(complaints133.amount_cents,EXCLUDED.amount_cents),
   owner_id=EXCLUDED.owner_id,
   last_seen_at=now()
 RETURNING id,local_state,COALESCE(owner_id,0)`,
		rec.PayType, rec.ThirdID, channelID, owner, tradeNos, nullString(rec.ThirdMchID), nullString(rec.Type), nullString(rec.Title), nullString(rec.Content), maxInt(rec.Times, 1), rec.AmountCents, rec.Status, local, nullString(rec.Phone), detail).Scan(&complaintID, &resultState, &ownerID)
	if e != nil {
		return e
	}
	// Enqueue one subscription event per (complaint, state). CaptureComplaint is
	// idempotent on (complaint_id, state), so a re-sync at the same state appends
	// nothing and enqueues no job; only a forward state change is a new event. An
	// unknown owner (owner_id=0) or an install without the 201 subscription
	// tables simply skips it — the sync's own semantics are unchanged.
	// Historical guessed owner_id is not delivery authority. Only the current
	// signed account and complete exact order set may authorize a recipient.
	if owner != nil && *owner == ownerID {
		if e = s.captureComplaintEvent201(ctx, tx, complaintID, ownerID, resultState, complaintRecordView{PayType: rec.PayType, ThirdID: rec.ThirdID}); e != nil {
			return e
		}
	}
	if sourceReady {
		s.recordComplaintNotices231(ctx, tx, complaintID, channelID, rec, owner, verifiedTrades)
	}
	return tx.Commit(ctx)
}

// localComplaintState maps a provider status string to the coarse local state.
func localComplaintState(payType, status string) string {
	switch payType {
	case "wxpay":
		switch status {
		case "PENDING":
			return "open"
		case "PROCESSING":
			return "processing"
		case "PROCESSED":
			return "done"
		}
	case "alipay":
		switch status {
		case "WAIT_MERCHANT_PROCESS", "WAIT_PLATFORM_PROCESS":
			return "open"
		case "MERCHANT_PROCESSING", "PROCESSING":
			return "processing"
		case "FINISHED", "CLOSED":
			return "done"
		}
	}
	return "open"
}

// RecordComplaintSyncResult advances or stops a channel's sync task after a run.
// On success the failure counter resets and last_status='ok'. On failure it
// increments; at complaintMaxFailures the task disables and an admin notice is
// recorded, mirroring the original BaseComplain MAX_FAILURES stop + alert.
func (s *Store) RecordComplaintSyncResult(ctx context.Context, channelID int64, syncErr error) error {
	var present bool
	if e := s.pool.QueryRow(ctx, `SELECT to_regclass('complaint_tasks133') IS NOT NULL`).Scan(&present); e != nil {
		return e
	}
	if !present {
		return nil
	}
	if syncErr == nil {
		_, e := s.pool.Exec(ctx, `UPDATE complaint_tasks133
 SET consecutive_failures=0,last_status='ok',last_reason=NULL,last_run_at=now(),updated_at=now()
 WHERE channel_id=$1`, channelID)
		return e
	}
	reason := syncErr.Error()
	if len(reason) > 500 {
		reason = reason[:500]
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var failures int
	var disabled bool
	e = tx.QueryRow(ctx, `UPDATE complaint_tasks133
 SET consecutive_failures=consecutive_failures+1,
     last_status='failed',last_reason=$2,last_run_at=now(),updated_at=now(),
     enabled=CASE WHEN consecutive_failures+1>=$3 THEN false ELSE enabled END
 WHERE channel_id=$1
 RETURNING consecutive_failures,(NOT enabled)`, channelID, reason, complaintMaxFailures).Scan(&failures, &disabled)
	if errors.Is(e, pgx.ErrNoRows) {
		return tx.Commit(ctx)
	}
	if e != nil {
		return e
	}
	if disabled && failures >= complaintMaxFailures {
		// Admin notice that the task auto-stopped, mirroring the original's alert.
		// It is recorded on the task row (last_reason carries the stop cause) and
		// surfaced to the admin console; a dedicated notice row is not written
		// because the shared backoffice resource allow-list does not include a
		// complaint alert kind and adding one needs a migration this change does
		// not own.
		if _, e = tx.Exec(ctx, `UPDATE complaint_tasks133
 SET last_reason=$2 WHERE channel_id=$1`, channelID,
			"连续同步失败"+strconv.Itoa(failures)+"次，任务已自动停用："+reason); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
