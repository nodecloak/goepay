package payment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	notice "goepay/internal/merchantnotice"
	"goepay/internal/platform/jobs"
)

type scheduler171NoticeSender struct {
	target string
	client *http.Client
}

func (*scheduler171NoticeSender) NoticeTransport() notice.Transport    { return notice.SMTP }
func (*scheduler171NoticeSender) NoticeScope() string                  { return "synthetic-scheduler171-notice-sink" }
func (*scheduler171NoticeSender) ValidateNoticeRecipient(string) error { return nil }
func (s *scheduler171NoticeSender) SendMerchantNotice(ctx context.Context, _ string, message notice.Message) error {
	raw, e := json.Marshal(message)
	if e != nil {
		return e
	}
	r, e := http.NewRequestWithContext(ctx, "POST", s.target, bytes.NewReader(raw))
	if e != nil {
		return e
	}
	response, e := s.client.Do(r)
	if e != nil {
		return e
	}
	defer response.Body.Close()
	if response.StatusCode != 204 {
		return fmt.Errorf("synthetic sink refused")
	}
	return nil
}

type scheduler171NoticeResolver struct{ sender *scheduler171NoticeSender }

func (r scheduler171NoticeResolver) ResolveMerchantNotice(context.Context, notice.Transport, notice.Account) (notice.Sender, error) {
	return r.sender, nil
}

func TestScheduler171DailyAndBalancePublishActualIndependentSourcesConsumedOnce(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := scheduler171Admin(t, s)
	current, e := s.ScheduledTasks(ctx, a, "daily_report")
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.UpdateScheduledTask(ctx, a, "daily_report", ScheduledTaskInput{Enabled: true, Schedule: ScheduledTaskSchedule{"once", 1, time.Now().UTC().Add(time.Hour).Truncate(time.Second)}, Params: ScheduledTaskParams{"send_hour": 0}, ExpectedFingerprint: current[0].Fingerprint})
	if e != nil {
		t.Fatal("real scene publisher unavailable", e)
	}
	if e = s.CreateMerchant(ctx, Merchant{ID: 901, Name: "Synthetic notice merchant", APIKey: "synthetic171-only"}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE merchants SET profile=profile||'{"email":"synthetic171@example.test"}'::jsonb WHERE id=901`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.notice.SaveSettings(ctx, 901, 0, notice.SettingsWrite{Preferences: notice.Preferences{DailyReport: 2, Balance: 2, BalanceMoney: 100}}, notice.Actor{Role: "merchant", ID: 901, Name: "Synthetic Owner"}); e != nil {
		t.Fatal(e)
	}
	scheduler171Pending(t, s, a, "daily_report")
	if tick, e := s.RunScheduledTasksOnce(ctx, 20); e != nil || tick.Completed != 1 || tick.Failed != 0 {
		t.Fatal("daily source task not executed", tick, e)
	}
	var daily, balance int
	if e = s.pool.QueryRow(ctx, `SELECT(SELECT count(*)FROM merchant_notice_timer_sources WHERE scene='daily_report'),(SELECT count(*)FROM merchant_notice_timer_sources WHERE scene='balance')`).Scan(&daily, &balance); e != nil || daily != 1 || balance != 0 {
		t.Fatal("daily task crossed scene scope", daily, balance, e)
	}
	state, e := s.ScheduledTasks(ctx, a, "balance_notice")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.UpdateScheduledTask(ctx, a, "balance_notice", ScheduledTaskInput{Enabled: true, Schedule: ScheduledTaskSchedule{"once", 1, time.Now().UTC().Add(time.Hour).Truncate(time.Second)}, Params: ScheduledTaskParams{}, ExpectedFingerprint: state[0].Fingerprint}); e != nil {
		t.Fatal(e)
	}
	scheduler171Pending(t, s, a, "balance_notice")
	if tick, e := s.RunScheduledTasksOnce(ctx, 20); e != nil || tick.Completed != 1 {
		t.Fatal(tick, e)
	}
	if e = s.pool.QueryRow(ctx, `SELECT(SELECT count(*)FROM merchant_notice_timer_sources WHERE scene='daily_report'),(SELECT count(*)FROM merchant_notice_timer_sources WHERE scene='balance')`).Scan(&daily, &balance); e != nil || daily != 1 || balance != 1 {
		t.Fatal("balance source not independent", daily, balance, e)
	}
	for _, key := range []string{"daily_report", "balance_notice"} {
		scheduler171Pending(t, s, a, key)
	}
	if tick, e := s.RunScheduledTasksOnce(ctx, 20); e != nil || tick.Completed != 2 {
		t.Fatal(tick, e)
	}
	var events int
	if e = s.pool.QueryRow(ctx, `SELECT count(*)FROM merchant_notice_timer_sources`).Scan(&events); e != nil || events != 2 {
		t.Fatal("same-day task repeated original notice sources", events, e)
	}
	var sends atomic.Int64
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Error("wrong synthetic delivery method")
		}
		sends.Add(1)
		w.WriteHeader(204)
	}))
	defer sink.Close()
	resolver := scheduler171NoticeResolver{&scheduler171NoticeSender{sink.URL, sink.Client()}}
	queue := jobs.Queue{Pool: s.pool}
	for i := 0; i < 2; i++ {
		job, e := queue.Claim(ctx, []string{"merchant.notice"}, time.Minute)
		if e != nil {
			t.Fatal("publisher had no existing worker job", e)
		}
		if e = s.notice.Deliver(ctx, job.ID, job.Token, notice.SMTP, notice.GlobalSwitches{MailRobot: true}, resolver); e != nil {
			t.Fatal("real notice consumer rejected original scene source", e)
		}
		if e = queue.Complete(ctx, job); e != nil {
			t.Fatal(e)
		}
	}
	if sends.Load() != 2 {
		t.Fatal("source consumer did not execute exactly one delivery each", sends.Load())
	}
}
