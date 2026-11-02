package merchantnotice_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/businesssms231"
	"goepay/internal/identity"
	notice "goepay/internal/merchantnotice"
	"goepay/internal/platform/jobs"
	"goepay/internal/smsgateway134"
	"strings"
	"testing"
	"time"
)

type smsFixture231 struct {
	notice  *notice.Store
	pool    *pgxpool.Pool
	sms     *businesssms231.Store
	gateway *smsgateway134.Store
	actor   smsgateway134.Actor
	view    businesssms231.View
}

func smsLocal231(t *testing.T) smsFixture231 {
	t.Helper()
	s, p := profileStore80(t)
	ctx := context.Background()
	if _, e := p.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class,auth_version)VALUES('sms-admin231','admin',231,'SMS','synthetic-no-password','super',1)`); e != nil {
		t.Fatal(e)
	}
	user := identity.User{Role: "admin", Username: "sms-admin231", ID: 231, AdminClass: "super", AuthVersion: 1}
	token, _, e := identity.NewPersistentSessions(p, false).Create(user)
	if e != nil {
		t.Fatal(e)
	}
	actor := smsgateway134.Actor{User: user, Token: token}
	key := sha256.Sum256(append([]byte("checkout-signing:"), make([]byte, 32)...))
	gateway := smsgateway134.New(p, key[:])
	before, e := gateway.Read(ctx, actor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = gateway.Save(ctx, actor, smsgateway134.Input{Provider: "aliyun", Credentials: &smsgateway134.Credentials{AccessKeyID: "AK231", AccessKeySecret: "synthetic-only231", Template: "SMS_VERIFICATION", Sign: "Test"}, Active: "aliyun", Status: 1, Expected: before.Fingerprint}); e != nil {
		t.Fatal(e)
	}
	sms, e := businesssms231.New(p, key[:])
	if e != nil {
		t.Fatal(e)
	}
	templates := map[string]string{}
	for _, provider := range []string{"aliyun", "qcloud", "smsbao"} {
		for _, scene := range []string{"balance", "complain", "group"} {
			templates[provider+"_"+scene] = ""
		}
	}
	templates["aliyun_balance"] = "SMS_BALANCE"
	templates["aliyun_complain"] = "SMS_COMPLAIN"
	templates["aliyun_group"] = "SMS_GROUP"
	view, e := sms.Save(ctx, actor, businesssms231.Input{ExpectedRevision: 0, Templates: templates})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(ctx, `INSERT INTO backoffice_records(resource,body)VALUES('settings','{"key":"smsnotice","value":"1"}');UPDATE merchants SET profile=profile||'{"bind_mobile":"13800138000"}'::jsonb WHERE id=1000`); e != nil {
		t.Fatal(e)
	}
	return smsFixture231{s, p, sms, gateway, actor, view}
}

type smsSender231 struct {
	calls int
	pool  *pgxpool.Pool
	t     *testing.T
}

func (*smsSender231) NoticeTransport() notice.Transport { return notice.SMS }
func (*smsSender231) NoticeScope() string               { return "local-synthetic-sms231" }
func (*smsSender231) ValidateNoticeRecipient(to string) error {
	if to != "13800138000" {
		return notice.ErrInvalid
	}
	return nil
}
func (s *smsSender231) SendMerchantNotice(ctx context.Context, _ string, m notice.Message) error {
	var claimed bool
	if e := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM merchant_notice_claims WHERE transport='sms')`).Scan(&claimed); e != nil || !claimed {
		s.t.Fatal("I/O preceded permanent claim", e)
	}
	if !m.Valid() {
		s.t.Fatal("invalid rendered source")
	}
	s.calls++
	return errors.New("simulated unknown transport outcome")
}
func balanceJob231(t *testing.T, f smsFixture231) jobs.Job {
	t.Helper()
	ctx := context.Background()
	if _, e := f.notice.SaveSettings(ctx, 1000, 0, notice.SettingsWrite{Preferences: notice.Preferences{Balance: 3, BalanceMoney: 1000}}, owner80); e != nil {
		t.Fatal(e)
	}
	if _, e := f.notice.GenerateScheduledPage(ctx, 0, 200, 9, nil); e != nil {
		t.Fatal(e)
	}
	job, e := (jobs.Queue{Pool: f.pool}).Claim(ctx, []string{"merchant.notice"}, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	return job
}
func TestBusinessSMS231SourceClaimAndUnknownOutcomeNeverReplay(t *testing.T) {
	f := smsLocal231(t)
	job := balanceJob231(t, f)
	sender := &smsSender231{pool: f.pool, t: t}
	ctx := context.Background()
	resolver := scheduledResolverFunc80(func(_ context.Context, kind notice.Transport, a notice.Account) (notice.Sender, error) {
		if kind != notice.SMS || a.SMS231 == nil || a.SMS231.Snapshot.Template != "SMS_BALANCE" || a.SMS231.Credentials.Template != "SMS_VERIFICATION" || a.Input.Params["msgmoney"] != "10.00" {
			t.Fatal("business route used verification template or lost original threshold")
		}
		return sender, nil
	})
	for i := 0; i < 2; i++ {
		if e := f.notice.Deliver(ctx, job.ID, job.Token, notice.SMS, notice.GlobalSwitches{}, resolver); e != nil {
			t.Fatal(e)
		}
	}
	if sender.calls != 1 {
		t.Fatal("unknown delivery repeated", sender.calls)
	}
	var leaked bool
	if e := f.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE changes::text LIKE '%13800138000%' OR changes::text LIKE '%synthetic-only231%')`).Scan(&leaked); e != nil || leaked {
		t.Fatal("plaintext recipient/secret audit", e)
	}
}
func TestBusinessSMS231RotationNeverAdoptsHistoricalSource(t *testing.T) {
	for _, change := range []string{"gateway-secret", "template", "phone", "disable"} {
		t.Run(change, func(t *testing.T) {
			f := smsLocal231(t)
			job := balanceJob231(t, f)
			ctx := context.Background()
			switch change {
			case "gateway-secret":
				v, e := f.gateway.Read(ctx, f.actor)
				if e != nil {
					t.Fatal(e)
				}
				_, e = f.gateway.Save(ctx, f.actor, smsgateway134.Input{Provider: "aliyun", Credentials: &smsgateway134.Credentials{AccessKeyID: "AK231", AccessKeySecret: "different-secret231", Template: "SMS_VERIFICATION", Sign: "Test"}, Active: "aliyun", Status: 1, Expected: v.Fingerprint})
				if e != nil {
					t.Fatal(e)
				}
			case "template":
				f.view.Templates["aliyun_balance"] = "SMS_NEW"
				if _, e := f.sms.Save(ctx, f.actor, businesssms231.Input{ExpectedRevision: f.view.Revision, Templates: f.view.Templates}); e != nil {
					t.Fatal(e)
				}
			case "phone":
				if _, e := f.pool.Exec(ctx, `UPDATE merchants SET profile=jsonb_set(profile,'{bind_mobile}','"13900139000"'::jsonb)WHERE id=1000`); e != nil {
					t.Fatal(e)
				}
			case "disable":
				if _, e := f.pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{value}','"0"'::jsonb)WHERE resource='settings' AND body->>'key'='smsnotice'`); e != nil {
					t.Fatal(e)
				}
			}
			called := false
			resolver := scheduledResolverFunc80(func(context.Context, notice.Transport, notice.Account) (notice.Sender, error) {
				called = true
				return nil, notice.ErrInvalid
			})
			if e := f.notice.Deliver(ctx, job.ID, job.Token, notice.SMS, notice.GlobalSwitches{}, resolver); e == nil || called {
				t.Fatal("historical SMS adopted changed configuration", e, called)
			}
		})
	}
}
func TestBusinessSMS231ContactPhoneDoesNotAuthorizeAndDeletedTemplateFailsClosed(t *testing.T) {
	f := smsLocal231(t)
	ctx := context.Background()
	if _, e := f.pool.Exec(ctx, `UPDATE merchants SET profile=(profile-'bind_mobile')||'{"phone":"13800138000"}'::jsonb WHERE id=1000`); e != nil {
		t.Fatal(e)
	}
	tx, e := f.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	resolved, e := f.sms.ResolveTx(ctx, tx, 1000, "balance")
	tx.Rollback(ctx)
	if e != nil || resolved != nil {
		t.Fatal("editable contact phone authorized SMS", e)
	}
	if _, e = f.pool.Exec(ctx, `DELETE FROM business_sms_settings231`); e != nil {
		t.Fatal(e)
	}
	if _, e = f.sms.Read(ctx, f.actor); !errors.Is(e, businesssms231.ErrConflict) {
		t.Fatal("deleted managed template fell back", e)
	}
	stale := f.view
	stale.Templates["aliyun_balance"] = "SMS_STALE"
	if _, e = f.sms.Save(ctx, f.actor, businesssms231.Input{ExpectedRevision: 0, Templates: stale.Templates}); e == nil || !strings.Contains(e.Error(), "changed") {
		t.Fatal("stale/deleted snapshot accepted", e)
	}
}
