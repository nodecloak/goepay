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

	// Create (enabled) for a wxpay channel.
	v, e := s.UpsertComplaintSyncTask(ctx, 9500, true, 6, 20, 1, "admin")
	if e != nil || !v.Enabled || v.Frequency != 6 || v.Num != 20 || v.Provider != "wxpay" || v.Name != "微信直连" {
		t.Fatalf("create: %+v err=%v", v, e)
	}

	// An unsupported provider is refused with ErrInvalid.
	if _, e := s.UpsertComplaintSyncTask(ctx, 9502, true, 6, 20, 1, "admin"); e != ErrInvalid {
		t.Fatalf("unsupported channel not refused: %v", e)
	}
	// An unknown channel is ErrNotFound.
	if _, e := s.UpsertComplaintSyncTask(ctx, 99999, true, 6, 20, 1, "admin"); e != ErrNotFound {
		t.Fatalf("unknown channel: %v", e)
	}
	// Out-of-range bounds refused.
	if _, e := s.UpsertComplaintSyncTask(ctx, 9500, true, 0, 20, 1, "admin"); e != ErrInvalid {
		t.Fatalf("frequency 0 not refused: %v", e)
	}
	if _, e := s.UpsertComplaintSyncTask(ctx, 9500, true, 6, 51, 1, "admin"); e != ErrInvalid {
		t.Fatalf("num 51 not refused: %v", e)
	}

	// Record a failure, then disable: the row + failure history survive.
	if e := s.RecordComplaintSyncResult(ctx, 9500, context.DeadlineExceeded); e != nil {
		t.Fatal(e)
	}
	v, e = s.UpsertComplaintSyncTask(ctx, 9500, false, 10, 30, 1, "admin")
	if e != nil || v.Enabled || v.Frequency != 10 || v.Num != 30 {
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
	v, e = s.UpsertComplaintSyncTask(ctx, 9500, true, 6, 20, 1, "admin")
	if e != nil || !v.Enabled {
		t.Fatalf("re-enable: %+v err=%v", v, e)
	}
	if e := s.pool.QueryRow(ctx, `SELECT consecutive_failures FROM complaint_tasks133 WHERE channel_id=9500`).Scan(&failures); e != nil {
		t.Fatal(e)
	}
	if failures != 0 {
		t.Fatalf("re-enable did not reset failures: %d", failures)
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
	if _, e := s.UpsertComplaintSyncTask(ctx, 9501, true, 6, 20, 1, "admin"); e != nil {
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
}
