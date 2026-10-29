package payment

import (
	"context"
	"testing"
)

// The 同步任务 config lives in complaint_tasks133, upserted per channel. A task
// is only allowed for a channel whose provider supports complaints (wxpay |
// alipay); disabling keeps the row + failure history; bounds match the original
// (frequency 1..1440 min, num 1..50); every upsert is audited. These seed a
// `channels` backoffice record so the provider check has something to resolve.

func seedChannel(t *testing.T, s *Store, id int64, provider, name string) {
	t.Helper()
	_, e := s.pool.Exec(context.Background(), `INSERT INTO backoffice_records(id,resource,body)
 VALUES($1,'channels',jsonb_build_object('name',$3::text,'provider',$2::text,'enabled',true))
 ON CONFLICT(id) DO UPDATE SET body=EXCLUDED.body`, id, provider, name)
	if e != nil {
		t.Fatal("seed channel:", e)
	}
}

func TestComplaintSyncTaskUpsertDisableUnsupportedAndList(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	seedChannel(t, s, 9500, "wxpay", "微信直连")
	seedChannel(t, s, 9501, "alipay", "支付宝直连")
	seedChannel(t, s, 9502, "qqpay", "QQ通道") // unsupported
	t.Cleanup(func() {
		s.pool.Exec(context.Background(), `DELETE FROM complaint_tasks133 WHERE channel_id IN (9500,9501,9502)`)
		s.pool.Exec(context.Background(), `DELETE FROM backoffice_records WHERE resource='channels' AND id IN (9500,9501,9502)`)
		s.pool.Exec(context.Background(), `DELETE FROM backoffice_audit WHERE resource='complaint-sync-tasks' AND resource_id IN (9500,9501,9502)`)
	})

	// Create (enabled) for a wxpay channel, with a 获取条件 of 30 days.
	v, e := s.UpsertComplaintSyncTask(ctx, 9500, true, 30, 6, 20, 1, "admin")
	if e != nil || !v.Enabled || v.Days != 30 || v.Frequency != 6 || v.Num != 20 || v.Provider != "wxpay" || v.Name != "微信直连" {
		t.Fatalf("create: %+v err=%v", v, e)
	}

	// An unsupported provider is refused with ErrInvalid.
	if _, e := s.UpsertComplaintSyncTask(ctx, 9502, true, 0, 6, 20, 1, "admin"); e != ErrInvalid {
		t.Fatalf("unsupported channel not refused: %v", e)
	}
	// An unknown channel is ErrNotFound.
	if _, e := s.UpsertComplaintSyncTask(ctx, 99999, true, 0, 6, 20, 1, "admin"); e != ErrNotFound {
		t.Fatalf("unknown channel: %v", e)
	}
	// Out-of-range bounds refused: frequency 0, num 0, num 201, days < 0.
	if _, e := s.UpsertComplaintSyncTask(ctx, 9500, true, 0, 0, 20, 1, "admin"); e != ErrInvalid {
		t.Fatalf("frequency 0 not refused: %v", e)
	}
	if _, e := s.UpsertComplaintSyncTask(ctx, 9500, true, 0, 6, 201, 1, "admin"); e != ErrInvalid {
		t.Fatalf("num 201 not refused: %v", e)
	}
	if _, e := s.UpsertComplaintSyncTask(ctx, 9500, true, -1, 6, 20, 1, "admin"); e != ErrInvalid {
		t.Fatalf("days -1 not refused: %v", e)
	}
	// num 200 is now within range (epay_pro's 20..200 editor).
	if v200, e := s.UpsertComplaintSyncTask(ctx, 9500, true, 0, 6, 200, 1, "admin"); e != nil || v200.Num != 200 || v200.Days != 0 {
		t.Fatalf("num 200 refused: %+v err=%v", v200, e)
	}

	// Record a failure, then disable: the row + failure history survive.
	if e := s.RecordComplaintSyncResult(ctx, 9500, context.DeadlineExceeded); e != nil {
		t.Fatal(e)
	}
	v, e = s.UpsertComplaintSyncTask(ctx, 9500, false, 7, 10, 30, 1, "admin")
	if e != nil || v.Enabled || v.Days != 7 || v.Frequency != 10 || v.Num != 30 {
		t.Fatalf("disable: %+v err=%v", v, e)
	}
	var failures int
	if e := s.pool.QueryRow(ctx, `SELECT consecutive_failures FROM complaint_tasks133 WHERE channel_id=9500`).Scan(&failures); e != nil {
		t.Fatal(e)
	}
	if failures != 1 {
		t.Fatalf("disable wiped failure history: failures=%d", failures)
	}

	// Re-enable resets the failure counter.
	v, e = s.UpsertComplaintSyncTask(ctx, 9500, true, 0, 6, 20, 1, "admin")
	if e != nil || !v.Enabled {
		t.Fatalf("re-enable: %+v err=%v", v, e)
	}
	if e := s.pool.QueryRow(ctx, `SELECT consecutive_failures FROM complaint_tasks133 WHERE channel_id=9500`).Scan(&failures); e != nil {
		t.Fatal(e)
	}
	if failures != 0 {
		t.Fatalf("re-enable did not reset failures: %d", failures)
	}

	// Inline status toggle (setTaskStatus): disable then re-enable.
	if v, e := s.SetComplaintTaskStatus(ctx, 9500, false, 1, "admin"); e != nil || v.Enabled {
		t.Fatalf("status disable: %+v err=%v", v, e)
	}
	if v, e := s.SetComplaintTaskStatus(ctx, 9500, true, 1, "admin"); e != nil || !v.Enabled {
		t.Fatalf("status enable: %+v err=%v", v, e)
	}
	// Status toggle on a non-existent task is ErrNotFound.
	if _, e := s.SetComplaintTaskStatus(ctx, 99999, true, 1, "admin"); e != ErrNotFound {
		t.Fatalf("status on missing task: %v", e)
	}

	// Audit rows recorded for the successful upserts.
	var audits int
	if e := s.pool.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE resource='complaint-sync-tasks' AND resource_id=9500 AND operation='configure'`).Scan(&audits); e != nil {
		t.Fatal(e)
	}
	if audits < 3 {
		t.Fatalf("expected audit rows for each upsert, got %d", audits)
	}

	// Add the alipay task; the list joins channel name/provider for both.
	if _, e := s.UpsertComplaintSyncTask(ctx, 9501, true, 0, 6, 20, 1, "admin"); e != nil {
		t.Fatal(e)
	}
	list, e := s.ComplaintSyncTasks(ctx)
	if e != nil {
		t.Fatal(e)
	}
	seen := map[string]ComplaintSyncTaskView{}
	for _, it := range list {
		seen[it.ChannelID] = it
	}
	if seen["9500"].Name != "微信直连" || seen["9500"].Provider != "wxpay" {
		t.Fatalf("list 9500: %+v", seen["9500"])
	}
	if seen["9501"].Name != "支付宝直连" || seen["9501"].Provider != "alipay" {
		t.Fatalf("list 9501: %+v", seen["9501"])
	}

	// Supported-channels picker lists wxpay+alipay, marks the enabled ones.
	chs, e := s.ComplaintSupportedChannels(ctx)
	if e != nil {
		t.Fatal(e)
	}
	var sawWx, sawAli, sawQQ bool
	for _, c := range chs {
		switch c.ChannelID {
		case "9500":
			sawWx = c.Enabled
		case "9501":
			sawAli = c.Enabled
		case "9502":
			sawQQ = true
		}
	}
	if !sawWx || !sawAli {
		t.Fatalf("supported channels missing enabled wxpay/alipay: wx=%v ali=%v", sawWx, sawAli)
	}
	if sawQQ {
		t.Fatal("unsupported qqpay channel listed as complaint-capable")
	}

	// Batch delete removes the task rows (and is idempotent on unknown ids); the
	// per-channel delete path shares DeleteComplaintSyncTasks.
	deleted, e := s.DeleteComplaintSyncTasks(ctx, []int64{9500, 9501, 99999}, 1, "admin")
	if e != nil || deleted != 2 {
		t.Fatalf("batch delete: deleted=%d err=%v", deleted, e)
	}
	var remaining int
	if e := s.pool.QueryRow(ctx, `SELECT count(*) FROM complaint_tasks133 WHERE channel_id IN (9500,9501)`).Scan(&remaining); e != nil {
		t.Fatal(e)
	}
	if remaining != 0 {
		t.Fatalf("tasks not deleted: remaining=%d", remaining)
	}
	// A delete audit row was recorded for each removed channel.
	var delAudits int
	if e := s.pool.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE resource='complaint-sync-tasks' AND operation='delete' AND resource_id IN (9500,9501)`).Scan(&delAudits); e != nil {
		t.Fatal(e)
	}
	if delAudits < 2 {
		t.Fatalf("delete audits missing: %d", delAudits)
	}
}
