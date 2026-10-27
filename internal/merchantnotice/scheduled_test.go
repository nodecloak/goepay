package merchantnotice_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	notice "goepay/internal/merchantnotice"
	"goepay/internal/payment"
	"goepay/internal/platform/jobs"
)

func TestMerchantNotice80DailyWindowUsesUTC8YesterdayAndOriginalHour(t *testing.T) {
	for _, tc := range []struct {
		at, date string
		due      bool
	}{
		{"2026-10-24T00:59:59Z", "2026-10-23", false},
		{"2026-10-24T01:00:00Z", "2026-10-23", true},
		{"2026-10-24T16:00:00Z", "2026-10-24", false},
	} {
		at, _ := time.Parse(time.RFC3339, tc.at)
		from, until, due, err := notice.DailyWindow(at, 9)
		if err != nil || due != tc.due || from.Format("2006-01-02") != tc.date || until.Sub(from) != 24*time.Hour || from.Hour() != 0 {
			t.Fatal(tc, from, until, due, err)
		}
	}
	if _, _, _, err := notice.DailyWindow(time.Now(), 24); err == nil {
		t.Fatal("invalid hour accepted")
	}
}

type scheduledSender80 struct{ text, title string }

func (*scheduledSender80) NoticeTransport() notice.Transport    { return notice.SMTP }
func (*scheduledSender80) NoticeScope() string                  { return "synthetic-scheduled-smtp80" }
func (*scheduledSender80) ValidateNoticeRecipient(string) error { return nil }
func (s *scheduledSender80) SendMerchantNotice(_ context.Context, _ string, m notice.Message) error {
	s.text = m.Text
	s.title = m.Title
	return nil
}

func TestMerchantNotice80CapturesOriginalSiteNameAndGoAliasBeforeLaterSettingsChange(t *testing.T) {
	for _, original := range []bool{false, true} {
		t.Run(map[bool]string{false: "go-alias", true: "original-precedence"}[original], func(t *testing.T) {
			s, pool := profileStore80(t)
			ctx := context.Background()
			if _, err := s.SaveSettings(ctx, 1000, 0, notice.SettingsWrite{Preferences: notice.Preferences{Balance: 2, BalanceMoney: 1}}, owner80); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"site_name","value":"Go站名"}')`); err != nil {
				t.Fatal(err)
			}
			want := "商户余额不足提醒 - Go站名"
			if original {
				if _, err := pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"sitename","value":"原版站名"}')`); err != nil {
					t.Fatal(err)
				}
				want = "商户余额不足提醒 - 原版站名"
			}
			if _, err := s.GenerateScheduledPage(ctx, 0, 200, 9, nil); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{value}','"后来站名"'::jsonb) WHERE resource='settings' AND body->>'key' IN('sitename','site_name')`); err != nil {
				t.Fatal(err)
			}
			job, err := (jobs.Queue{Pool: pool}).Claim(ctx, []string{"merchant.notice"}, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			sender := &scheduledSender80{}
			if err = s.Deliver(ctx, job.ID, job.Token, notice.SMTP, notice.GlobalSwitches{MailRobot: true}, scheduledResolver80{sender}); err != nil || sender.title != want {
				t.Fatal("site name not captured from original configuration", sender.title, want, err)
			}
		})
	}
}

type scheduledResolver80 struct{ sender *scheduledSender80 }

func (r scheduledResolver80) ResolveMerchantNotice(context.Context, notice.Transport, notice.Account) (notice.Sender, error) {
	return r.sender, nil
}

type scheduledResolverFunc80 func(context.Context, notice.Transport, notice.Account) (notice.Sender, error)

func (f scheduledResolverFunc80) ResolveMerchantNotice(c context.Context, k notice.Transport, a notice.Account) (notice.Sender, error) {
	return f(c, k, a)
}

type scheduledWechat80 struct{ calls int }

func (*scheduledWechat80) NoticeTransport() notice.Transport    { return notice.Wechat }
func (*scheduledWechat80) NoticeScope() string                  { return "synthetic-balance-wechat80" }
func (*scheduledWechat80) NoticeAccountID() string              { return "app80" }
func (*scheduledWechat80) ValidateNoticeRecipient(string) error { return nil }
func (s *scheduledWechat80) SendMerchantNotice(context.Context, string, notice.Message) error {
	s.calls++
	return nil
}

func TestMerchantNotice80BalanceWechatSourceIncludesOriginalUserAndTimeMappings(t *testing.T) {
	s, pool := profileStore80(t)
	ctx := context.Background()
	openid, appid := "openid80", "app80"
	if _, err := s.SaveSettings(ctx, 1000, 0, notice.SettingsWrite{Preferences: notice.Preferences{Balance: 1, BalanceMoney: 1}, Targets: notice.TargetPatch{WXUID: &openid, WXAppID: &appid}}, owner80); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"wxnotice_tpl_balance","value":"local-template80"}'),('settings','{"key":"wxnotice_tpl_balance_user","value":"character_string1"}'),('settings','{"key":"wxnotice_tpl_balance_time","value":"time2"}'),('settings','{"key":"wxnotice_tpl_balance_money","value":"amount3"}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GenerateScheduledPage(ctx, 0, 200, 9, nil); err != nil {
		t.Fatal(err)
	}
	job, err := (jobs.Queue{Pool: pool}).Claim(ctx, []string{"merchant.notice"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	sender := &scheduledWechat80{}
	err = s.Deliver(ctx, job.ID, job.Token, notice.Wechat, notice.GlobalSwitches{}, scheduledResolverFunc80(func(_ context.Context, _ notice.Transport, a notice.Account) (notice.Sender, error) {
		cfg := a.Template
		cfg.ConsoleURL = "https://console.example.test"
		if _, err := notice.BuildWechatTemplate(a.Input, cfg); err != nil {
			return nil, err
		}
		if a.Input.Params["user"] != "1000" || a.Input.Params["time"] == "" {
			return nil, notice.ErrInvalid
		}
		return sender, nil
	}))
	if err != nil || sender.calls != 1 {
		t.Fatal("incomplete original balance template source", sender.calls, err)
	}
}

func TestMerchantNotice80DailySnapshotUsesOrderCreationDayAndRetainsRefundedGross(t *testing.T) {
	s, pool := profileStore80(t)
	ctx := context.Background()
	base, err := payment.NewWithKey(pool, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	var now time.Time
	if err = pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	from, _, _, _ := notice.DailyWindow(now, 0)
	for _, row := range []struct {
		no      string
		amount  int64
		paid    bool
		created time.Time
	}{
		{"daily-paid80", 1234, true, from.Add(time.Hour)},
		{"daily-pending80", 2000, false, from.Add(2 * time.Hour)},
		{"daily-excluded80", 9000, true, from.Add(-time.Second)},
	} {
		// Historical daily cohorts are imported pre104 orders. A current104
		// order's birth timestamp is authenticated and must not be backdated.
		var trade string
		if e := pool.QueryRow(ctx, `INSERT INTO orders(trade_no,merchant_id,out_trade_no,name,type,amount,merchant_amount,platform_fee,fee_policy,refund_fee_payer,created_at) VALUES(nextval('trade_no_seq')::text,1000,$1,'report80','wxpay',$2,$2,0,'none','platform',$3) RETURNING trade_no`, row.no, row.amount, row.created).Scan(&trade); e != nil {
			t.Fatal(e)
		}
		if row.paid {
			if e := base.MarkPaid(ctx, trade, "local-"+row.no); e != nil {
				t.Fatal(e)
			}
		}
	}
	refund, err := base.ReserveRefund(ctx, 1000, "daily-paid80", "daily-refund80", 100)
	if err != nil {
		t.Fatal(err)
	}
	if err = base.ResolveRefund(ctx, refund.ID, "succeeded"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveSettings(ctx, 1000, 0, notice.SettingsWrite{Preferences: notice.Preferences{DailyReport: 2}}, owner80); err != nil {
		t.Fatal(err)
	}
	page, err := s.GenerateScheduledPage(ctx, 0, 200, 0, nil)
	if err != nil || page.Created != 1 {
		t.Fatal("daily source not generated", page, err)
	}
	job, err := (jobs.Queue{Pool: pool}).Claim(ctx, []string{"merchant.notice"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	sender := &scheduledSender80{}
	if err = s.Deliver(ctx, job.ID, job.Token, notice.SMTP, notice.GlobalSwitches{}, scheduledResolver80{sender}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{from.Format("2006-01-02"), "订单金额总计：¥12.34", "分成金额总计：¥12.34", "订单总数：2", "成功订单数量：1", "订单成功率：50.00%"} {
		if !strings.Contains(sender.text, want) {
			t.Fatalf("daily original contract missing %s: %s", want, sender.text)
		}
	}
	if _, err = base.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1000, OutTradeNo: "late-created80", Name: "late", Type: "alipay", Amount: 100}); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"transfer_enabled","value":"invalid-later-setting"}')`); err != nil {
		t.Fatal(err)
	}
	next, err := s.GenerateScheduledPage(ctx, 0, 200, 0, nil)
	if err != nil || next.Created != 0 {
		t.Fatal("daily snapshot changed or duplicate send", next, err)
	}
	report, err := s.Check(ctx)
	if err != nil || report.InvalidEvents != 0 || report.InvalidDeliveries != 0 {
		t.Fatal("late business mutation invalidated original report", report, err)
	}
}

func TestMerchantNotice80BalanceScheduleStrictThresholdOncePerDayAndBoundedPages(t *testing.T) {
	s, pool := profileStore80(t)
	ctx := context.Background()
	if _, err := s.SaveSettings(ctx, 1000, 0, notice.SettingsWrite{Preferences: notice.Preferences{Balance: 2, BalanceMoney: 1}}, owner80); err != nil {
		t.Fatal(err)
	}
	first, err := s.GenerateScheduledPage(ctx, 0, 1, 9, nil)
	if err != nil || first.Scanned != 1 || first.LastMerchantID != 1000 || first.Created != 1 {
		t.Fatal("bounded first merchant page", first, err)
	}
	next, err := s.GenerateScheduledPage(ctx, first.LastMerchantID, 1, 9, nil)
	if err != nil || next.Scanned != 1 || next.LastMerchantID != 1001 || next.Created != 0 {
		t.Fatal("cursor skipped or duplicated merchant", next, err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.GenerateScheduledPage(ctx, 0, 200, 9, nil); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	events, err := s.Events(ctx, 1000, 0, 20, owner80)
	if err != nil || len(events) != 1 || events[0].Scene != "balance" {
		t.Fatal("daily duplicate source", events, err)
	}
	var jobs int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE kind='merchant.notice'`).Scan(&jobs); err != nil || jobs != 1 {
		t.Fatal("duplicate queued deliveries", jobs, err)
	}
	if _, err = s.GenerateScheduledPage(ctx, 0, 201, 9, nil); err == nil {
		t.Fatal("unbounded merchant scan")
	}
}
