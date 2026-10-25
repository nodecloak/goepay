package payment

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	notice "goepay/internal/merchantnotice"
	"goepay/internal/platform/jobs"
)

type noticeSender80 struct {
	calls  atomic.Int64
	before func()
	fail   bool
}

func (*noticeSender80) NoticeTransport() notice.Transport { return notice.SMTP }
func (*noticeSender80) NoticeScope() string               { return "synthetic-smtp-account80" }
func (*noticeSender80) ValidateNoticeRecipient(to string) error {
	if to != "owner80@example.test" {
		return errors.New("wrong source recipient")
	}
	return nil
}
func (s *noticeSender80) SendMerchantNotice(_ context.Context, to string, m notice.Message) error {
	if s.before != nil {
		s.before()
	}
	s.calls.Add(1)
	if to != "owner80@example.test" || !m.Valid() || !strings.Contains(m.HTML, "&lt;safe&gt;") {
		return errors.New("source message changed")
	}
	if s.fail {
		return errors.New("synthetic timeout; must not be retried")
	}
	return nil
}

type noticeResolver80 struct {
	sender *noticeSender80
	calls  atomic.Int64
}

type noticeResolverFunc80 func(context.Context, notice.Transport, notice.Account) (notice.Sender, error)

func (f noticeResolverFunc80) ResolveMerchantNotice(c context.Context, k notice.Transport, a notice.Account) (notice.Sender, error) {
	return f(c, k, a)
}

type wxNoticeSender80 struct {
	calls atomic.Int64
	app   string
}

type recordedNoticeSender80 struct {
	noticeSender80
	text string
}

func (s *recordedNoticeSender80) SendMerchantNotice(_ context.Context, _ string, m notice.Message) error {
	s.text = m.Text
	s.calls.Add(1)
	return nil
}

func (*wxNoticeSender80) NoticeTransport() notice.Transport { return notice.Wechat }
func (s *wxNoticeSender80) NoticeAccountID() string         { return s.app }
func (*wxNoticeSender80) NoticeScope() string               { return "synthetic-wechat-scope80" }
func (*wxNoticeSender80) ValidateNoticeRecipient(to string) error {
	if to != "openid80" {
		return errors.New("wrong OpenID")
	}
	return nil
}
func (s *wxNoticeSender80) SendMerchantNotice(_ context.Context, _ string, _ notice.Message) error {
	s.calls.Add(1)
	return nil
}

func (r *noticeResolver80) ResolveMerchantNotice(_ context.Context, kind notice.Transport, _ notice.Account) (notice.Sender, error) {
	r.calls.Add(1)
	if kind != notice.SMTP {
		return nil, errors.New("unexpected route")
	}
	return r.sender, nil
}

func delivery80(t *testing.T) (*Store, *notice.Store, jobs.Job, notice.SettingsView) {
	t.Helper()
	s := testStore(t)
	ctx := context.Background()
	if err := s.CreateMerchant(ctx, Merchant{ID: 80003, Name: "notice-delivery80", APIKey: "synthetic-only80"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE merchants SET profile=profile||'{"email":"owner80@example.test"}'::jsonb WHERE id=80003`); err != nil {
		t.Fatal(err)
	}
	n, err := notice.NewStore(s.pool, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	prefs, err := n.SaveSettings(ctx, 80003, 0, notice.SettingsWrite{Preferences: notice.Preferences{Order: 2}}, notice.Actor{Role: "merchant", ID: 80003, Name: "notice-delivery80"})
	if err != nil {
		t.Fatal(err)
	}
	o, err := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 80003, OutTradeNo: "protected-notice80", Name: "<safe>商品80", Type: "wxpay", Amount: 1234})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.BeginProviderAttempt(ctx, o.TradeNo, 7, "native"); err != nil {
		t.Fatal(err)
	}
	if err = s.markPaid(ctx, o.TradeNo, "notice-delivery-event80", false, PaidReceipt{ChannelID: 7, ProviderTradeNo: "local-verified-notice80"}); err != nil {
		t.Fatal(err)
	}
	job, err := (jobs.Queue{Pool: s.pool}).Claim(ctx, []string{"merchant.notice"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return s, n, job, prefs
}

func TestMerchantNotice80ConcurrentDeliveryCommitsOneClaimBeforeSending(t *testing.T) {
	s, n, job, _ := delivery80(t)
	ctx := context.Background()
	sender := &noticeSender80{before: func() {
		var claims, audits int
		err := s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM merchant_notice_claims),(SELECT count(*) FROM backoffice_audit WHERE resource='merchant-notice-deliveries' AND operation='attempt')`).Scan(&claims, &audits)
		if err != nil || claims != 1 || audits != 1 {
			t.Errorf("network before committed authority %d %d %v", claims, audits, err)
		}
	}}
	resolver := &noticeResolver80{sender: sender}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- n.Deliver(ctx, job.ID, job.Token, notice.SMTP, notice.GlobalSwitches{MailRobot: true}, resolver)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	if sender.calls.Load() != 1 || resolver.calls.Load() != 1 {
		t.Fatal("duplicate sending or credential resolution", sender.calls.Load(), resolver.calls.Load())
	}
	var state string
	if err := s.pool.QueryRow(ctx, `SELECT state FROM merchant_notice_results`).Scan(&state); err != nil || state != "accepted" {
		t.Fatal("provider acceptance not preserved", state, err)
	}
}
func TestMerchantNotice80UnknownDeliveryCannotAcquireAnotherSendAfterConfigurationChanges(t *testing.T) {
	s, n, job, prefs := delivery80(t)
	ctx := context.Background()
	sender := &noticeSender80{fail: true}
	resolver := &noticeResolver80{sender: sender}
	if err := n.Deliver(ctx, job.ID, job.Token, notice.SMTP, notice.GlobalSwitches{MailRobot: true}, resolver); err != nil {
		t.Fatal(err)
	}
	if _, err := n.SaveSettings(ctx, 80003, prefs.ID, notice.SettingsWrite{Preferences: notice.Preferences{Order: 4}}, notice.Actor{Role: "merchant", ID: 80003, Name: "notice-delivery80"}); err != nil {
		t.Fatal(err)
	}
	if err := n.Deliver(ctx, job.ID, job.Token, notice.SMTP, notice.GlobalSwitches{MailRobot: true}, resolver); err != nil {
		t.Fatal(err)
	}
	if sender.calls.Load() != 1 || resolver.calls.Load() != 1 {
		t.Fatal("unknown send repeated", sender.calls.Load(), resolver.calls.Load())
	}
	var state string
	if err := s.pool.QueryRow(ctx, `SELECT state FROM merchant_notice_results`).Scan(&state); err != nil || state != "unknown" {
		t.Fatal(state, err)
	}
}
func TestMerchantNotice80ChangedProfileAndExpiredLeaseNeverReachTransport(t *testing.T) {
	for _, change := range []string{"profile", "lease", "switch", "source"} {
		t.Run(change, func(t *testing.T) {
			s, n, job, prefs := delivery80(t)
			ctx := context.Background()
			sender := &noticeSender80{}
			resolver := &noticeResolver80{sender: sender}
			global := notice.GlobalSwitches{MailRobot: true}
			switch change {
			case "profile":
				if _, err := n.SaveSettings(ctx, 80003, prefs.ID, notice.SettingsWrite{Preferences: notice.Preferences{Order: 4}}, notice.Actor{Role: "merchant", ID: 80003, Name: "notice-delivery80"}); err != nil {
					t.Fatal(err)
				}
			case "lease":
				if _, err := s.pool.Exec(ctx, `UPDATE jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, job.ID); err != nil {
					t.Fatal(err)
				}
			case "switch":
				global.MailRobot = false
			case "source":
				if _, err := s.pool.Exec(ctx, `ALTER TABLE merchant_notice_events DISABLE TRIGGER USER;UPDATE merchant_notice_events SET payload_cipher=decode(repeat('ab',40),'hex');ALTER TABLE merchant_notice_events ENABLE TRIGGER USER`); err != nil {
					t.Fatal(err)
				}
			}
			if err := n.Deliver(ctx, job.ID, job.Token, notice.SMTP, global, resolver); err == nil {
				t.Fatal("invalid original source accepted")
			}
			if sender.calls.Load() != 0 || resolver.calls.Load() != 0 {
				t.Fatal("transport reached before source/lease/config verification")
			}
		})
	}
}

func TestMerchantNotice80WorkerUsesOriginalRoutesAndLiveOriginalSceneSwitch(t *testing.T) {
	s, n, job, _ := delivery80(t)
	ctx := context.Background()
	sender := &noticeSender80{}
	resolver := &noticeResolver80{sender: sender}
	if err := n.HandleJob(ctx, job.ID, job.Token, resolver); err == nil {
		t.Fatal("missing platform switch silently enabled delivery")
	}
	if sender.calls.Load() != 0 || resolver.calls.Load() != 0 {
		t.Fatal("disabled platform reached constructor or network")
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"msgconfig_order","value":"1"}')`); err != nil {
		t.Fatal(err)
	}
	if err := n.HandleJob(ctx, job.ID, job.Token, resolver); err != nil {
		t.Fatal(err)
	}
	if err := n.HandleJob(ctx, job.ID, job.Token, resolver); err != nil {
		t.Fatal(err)
	}
	if sender.calls.Load() != 1 {
		t.Fatal("worker repeated original route", sender.calls.Load())
	}
}

func TestMerchantNotice80LateAcceptedResponseSurvivesRevocationAndRequestCancellation(t *testing.T) {
	s, n, job, prefs := delivery80(t)
	base := context.Background()
	ctx, cancel := context.WithCancel(base)
	defer cancel()
	sender := &noticeSender80{before: func() {
		if _, err := n.SaveSettings(base, 80003, prefs.ID, notice.SettingsWrite{Preferences: notice.Preferences{}}, notice.Actor{Role: "merchant", ID: 80003, Name: "notice-delivery80"}); err != nil {
			t.Error(err)
		}
		if _, err := s.pool.Exec(base, `UPDATE merchants SET enabled=false WHERE id=80003`); err != nil {
			t.Error(err)
		}
		cancel()
	}}
	if err := n.Deliver(ctx, job.ID, job.Token, notice.SMTP, notice.GlobalSwitches{MailRobot: true}, &noticeResolver80{sender: sender}); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := s.pool.QueryRow(base, `SELECT state FROM merchant_notice_results`).Scan(&state); err != nil || state != "accepted" || sender.calls.Load() != 1 {
		t.Fatal("late authentic acceptance lost", state, err, sender.calls.Load())
	}
}
func TestMerchantNotice80LostResultWriteCannotRepeatThePermanentSend(t *testing.T) {
	s, n, job, _ := delivery80(t)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `CREATE FUNCTION reject_notice80_result() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic response storage failure'; END $$;CREATE TRIGGER reject_notice80_result BEFORE INSERT ON merchant_notice_results FOR EACH ROW EXECUTE FUNCTION reject_notice80_result()`); err != nil {
		t.Fatal(err)
	}
	sender := &noticeSender80{}
	resolver := &noticeResolver80{sender: sender}
	if err := n.Deliver(ctx, job.ID, job.Token, notice.SMTP, notice.GlobalSwitches{MailRobot: true}, resolver); err == nil {
		t.Fatal("result storage failure hidden")
	}
	if _, err := s.pool.Exec(ctx, `DROP TRIGGER reject_notice80_result ON merchant_notice_results`); err != nil {
		t.Fatal(err)
	}
	if err := n.Deliver(ctx, job.ID, job.Token, notice.SMTP, notice.GlobalSwitches{MailRobot: true}, resolver); err != nil {
		t.Fatal(err)
	}
	var claims, results int
	if err := s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM merchant_notice_claims),(SELECT count(*) FROM merchant_notice_results)`).Scan(&claims, &results); err != nil || claims != 1 || results != 0 || sender.calls.Load() != 1 || resolver.calls.Load() != 1 {
		t.Fatal("lost response repeated or invented acceptance", claims, results, sender.calls.Load(), resolver.calls.Load(), err)
	}
}

func TestMerchantNotice80WechatSourceMappingsAndOriginalAppCannotBeReplaced(t *testing.T) {
	for _, mode := range []string{"original", "wrong-app", "duplicate-config"} {
		t.Run(mode, func(t *testing.T) {
			s, n, _, prefs := delivery80(t)
			ctx := context.Background()
			openid, appid := "openid80", "wxoriginal80"
			if _, err := n.SaveSettings(ctx, 80003, prefs.ID, notice.SettingsWrite{Preferences: notice.Preferences{Order: 1}, Targets: notice.TargetPatch{WXUID: &openid, WXAppID: &appid}}, notice.Actor{Role: "merchant", ID: 80003, Name: "notice-delivery80"}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"wxnotice_tpl_order","value":"local-template80"}'),('settings','{"key":"wxnotice_tpl_order_money","value":"amount1"}')`); err != nil {
				t.Fatal(err)
			}
			if mode == "duplicate-config" {
				// This random-schema lab bypasses only the index so the reader's
				// duplicate protection is tested independently of SQL uniqueness.
				if _, err := s.pool.Exec(ctx, `DROP INDEX backoffice_setting_unique`); err != nil {
					t.Fatal(err)
				}
				if _, err := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"wxnotice_tpl_order","value":"other-template80"}')`); err != nil {
					t.Fatal(err)
				}
			}
			o, err := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 80003, OutTradeNo: "wechat-app80", Name: "local", Type: "wxpay", Amount: 1234})
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = s.BeginProviderAttempt(ctx, o.TradeNo, 7, "native"); err != nil {
				t.Fatal(err)
			}
			if err = s.markPaid(ctx, o.TradeNo, "local-wx-event80", false, PaidReceipt{ChannelID: 7, ProviderTradeNo: "local-wx-receipt80"}); err != nil {
				t.Fatal(err)
			}
			job, err := (jobs.Queue{Pool: s.pool}).Claim(ctx, []string{"merchant.notice"}, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			sender := &wxNoticeSender80{app: appid}
			if mode == "wrong-app" {
				sender.app = "wxdifferent80"
			}
			resolved := 0
			resolver := noticeResolverFunc80(func(_ context.Context, kind notice.Transport, a notice.Account) (notice.Sender, error) {
				resolved++
				if kind != notice.Wechat || a.WXAppID != appid || a.Template.ID != "local-template80" || a.Template.Fields["wxnotice_tpl_order_money"] != "amount1" || a.Input.Params["trade_no"] != o.TradeNo {
					t.Error("source account/template rebound", kind, a.Template.ID)
				}
				return sender, nil
			})
			err = n.HandleJob(ctx, job.ID, job.Token, resolver)
			if mode == "original" {
				if err != nil || sender.calls.Load() != 1 || resolved != 1 {
					t.Fatal("original app unavailable", err, sender.calls.Load(), resolved)
				}
			} else if err == nil || sender.calls.Load() != 0 {
				t.Fatal("foreign or ambiguous Wechat account reached recipient", err, sender.calls.Load())
			}
		})
	}
}

func TestMerchantNotice80TransportConstructionCannotRewriteTheOriginalMessage(t *testing.T) {
	_, n, job, _ := delivery80(t)
	sender := &recordedNoticeSender80{}
	resolver := noticeResolverFunc80(func(_ context.Context, _ notice.Transport, account notice.Account) (notice.Sender, error) {
		account.Input.Params["money"] = "999.99"
		return sender, nil
	})
	if err := n.Deliver(context.Background(), job.ID, job.Token, notice.SMTP, notice.GlobalSwitches{MailRobot: true}, resolver); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sender.text, "¥12.34") || strings.Contains(sender.text, "999.99") {
		t.Fatal("transport constructor rewrote the protected source message", sender.text)
	}
}

func TestMerchantNotice80RecreatedJobCannotAcquireTheOriginalSendingAuthority(t *testing.T) {
	s, n, job, _ := delivery80(t)
	ctx := context.Background()
	// Bypass only this random-schema lab's SQL references, to verify the source
	// MAC independently. A copied event key must not authorize another job ID.
	if _, err := s.pool.Exec(ctx, `ALTER TABLE jobs DISABLE TRIGGER ALL`); err != nil {
		t.Fatal(err)
	}
	var replaced int64
	if err := s.pool.QueryRow(ctx, `WITH old AS(DELETE FROM jobs WHERE id=$1 RETURNING event_key,kind,payload) INSERT INTO jobs(event_key,kind,payload,state,lease_token,lease_until) SELECT event_key,kind,payload,'running',$2,clock_timestamp()+interval '1 minute' FROM old RETURNING id`, job.ID, job.Token).Scan(&replaced); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `ALTER TABLE jobs ENABLE TRIGGER ALL`); err != nil {
		t.Fatal(err)
	}
	sender := &noticeSender80{}
	resolver := &noticeResolver80{sender: sender}
	if err := n.Deliver(ctx, replaced, job.Token, notice.SMTP, notice.GlobalSwitches{MailRobot: true}, resolver); err == nil || sender.calls.Load() != 0 || resolver.calls.Load() != 0 {
		t.Fatal("replacement job adopted original sending authority", err, sender.calls.Load(), resolver.calls.Load())
	}
}
