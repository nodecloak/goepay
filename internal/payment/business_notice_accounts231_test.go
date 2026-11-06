package payment

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/businessnotice"
	"goepay/internal/businesssms"
	"goepay/internal/identity"
	"goepay/internal/merchantnotice"
	"goepay/internal/noticeconfig"
	"goepay/internal/platform/jobs"
	"goepay/internal/smsgateway"
	"testing"
	"time"
)

type accountsFixture231 struct {
	store  *Store
	config *noticeconfig.Store
	actor  noticeconfig.Actor
}

func businessAccountsLocal231(t *testing.T, mode string) accountsFixture231 {
	t.Helper()
	s := testStore(t)
	ctx := context.Background()
	if _, e := s.pool.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class,auth_version)VALUES('accounts-admin231','admin',23100,'Accounts','synthetic-no-password','super',1)`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body)VALUES('settings',jsonb_build_object('key','msgconfig_complain_all','value',$1::text))`, mode); e != nil {
		t.Fatal(e)
	}
	u := identity.User{Username: "accounts-admin231", Role: "admin", ID: 23100, AuthVersion: 1, AdminClass: "super"}
	token, csrf, e := identity.NewPersistentSessions(s.pool, false).Create(u)
	if e != nil {
		t.Fatal(e)
	}
	config, e := noticeconfig.FromMaster(s.pool, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	return accountsFixture231{s, config, noticeconfig.Actor{User: u, Token: token, CSRF: csrf}}
}
func (f accountsFixture231) complaint(t *testing.T) jobs.Job {
	t.Helper()
	ctx := context.Background()
	if e := f.store.upsertComplaint133(ctx, 7, adapter.ComplaintRecord{PayType: "wxpay", ThirdID: "accounts-complaint231", Status: "PENDING", Times: 1, Title: "synthetic complaint", TradeNos: []string{"platform-order231"}}); e != nil {
		t.Fatal(e)
	}
	j, e := (jobs.Queue{Pool: f.store.pool}).Claim(ctx, []string{businessnotice.JobKind}, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	return j
}

type accountSender231 struct {
	fixture        accountsFixture231
	kind           merchantnotice.Transport
	app, recipient string
	calls          int
	err            error
}

func (s *accountSender231) NoticeTransport() merchantnotice.Transport { return s.kind }
func (s *accountSender231) NoticeScope() string {
	return "synthetic-account231/" + string(s.kind) + "/" + s.app
}
func (s *accountSender231) NoticeAccountID() string { return s.app }
func (s *accountSender231) ValidateNoticeRecipient(to string) error {
	if to != s.recipient {
		return errors.New("wrong synthetic recipient")
	}
	return nil
}
func (s *accountSender231) SendMerchantNotice(ctx context.Context, to string, m merchantnotice.Message) error {
	var claims int
	if e := s.fixture.store.pool.QueryRow(ctx, `SELECT count(*) FROM admin_business_notice_claims`).Scan(&claims); e != nil || claims != 1 {
		return errors.New("claim not committed before simulated I/O")
	}
	if !m.Valid() || to != s.recipient {
		return errors.New("wrong rendered notice")
	}
	s.calls++
	return s.err
}

type accountResolver231 struct {
	sender                 *accountSender231
	called, legacyExpected int
	t                      *testing.T
	inspect                func(businessnotice.Account)
}

func (r *accountResolver231) BusinessNoticeAccountID(context.Context, merchantnotice.Transport) (string, error) {
	r.legacyExpected++
	return "wrong-environment-app231", nil
}
func (r *accountResolver231) ResolveBusinessNotice(_ context.Context, kind merchantnotice.Transport, a businessnotice.Account) (businessnotice.Route, error) {
	r.called++
	if kind != r.sender.kind {
		r.t.Fatal("wrong notification route")
	}
	if r.inspect != nil {
		r.inspect(a)
	}
	return businessnotice.Route{Sender: r.sender, Recipient: r.sender.recipient}, nil
}

func TestBusinessAccounts231RobotRotationCannotAdoptOldComplaint(t *testing.T) {
	f := businessAccountsLocal231(t, "2")
	ctx := context.Background()
	v, e := f.config.Update(ctx, f.actor, "robot", noticeconfig.Input{ExpectedRevision: 0, Enabled: true, Action: "replace", AppID: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic231"})
	if e != nil {
		t.Fatal(e)
	}
	j := f.complaint(t)
	if _, e = f.config.Update(ctx, f.actor, "robot", noticeconfig.Input{ExpectedRevision: v.Revision, Enabled: true, Action: "keep"}); e != nil {
		t.Fatal(e)
	}
	r := &accountResolver231{sender: &accountSender231{fixture: f, kind: merchantnotice.Robot, recipient: "unused"}, t: t}
	if e = f.store.business.HandleJob(ctx, j.ID, j.Token, r); e == nil || r.called != 0 {
		t.Fatal("changed robot account adopted historical event", e, r.called)
	}
}

func TestBusinessAccounts231QQBoundAppOverridesLegacyEnvironmentExpectation(t *testing.T) {
	f := businessAccountsLocal231(t, "5")
	ctx := context.Background()
	secret := "synthetic-qq-secret231"
	v, e := f.config.Update(ctx, f.actor, "qqbot", noticeconfig.Input{ExpectedRevision: 0, Enabled: true, Action: "replace", AppID: "app231", Secret: secret})
	if e != nil {
		t.Fatal(e)
	}
	a, e := f.config.StartBinding(ctx, f.actor, v.Revision)
	if e != nil {
		t.Fatal(e)
	}
	seed := []byte(secret)
	for len(seed) < 32 {
		seed = append(seed, seed...)
	}
	key := ed25519.NewKeyFromSeed(seed[:32])
	raw, _ := json.Marshal(map[string]any{"op": 0, "t": "C2C_MESSAGE_CREATE", "id": "qq-bind231", "d": map[string]any{"content": a.Challenge, "author": map[string]string{"user_openid": "recipient231"}}})
	stamp := fmt.Sprint(time.Now().Unix())
	signature := hex.EncodeToString(ed25519.Sign(key, append([]byte(stamp), raw...)))
	if _, e = f.config.QQCallback(ctx, "app231", stamp, signature, raw, time.Now()); e != nil {
		t.Fatal(e)
	}
	j := f.complaint(t)
	sender := &accountSender231{fixture: f, kind: merchantnotice.QQBot, app: "app231", recipient: "recipient231"}
	r := &accountResolver231{sender: sender, t: t, inspect: func(a businessnotice.Account) {
		if !a.Managed231 || a.Credential231.AppID != "app231" || a.Credential231.Recipient != "recipient231" {
			t.Fatal("managed bound account not supplied")
		}
	}}
	if e = f.store.business.HandleJob(ctx, j.ID, j.Token, r); e != nil || sender.calls != 1 || r.legacyExpected != 0 {
		t.Fatal("managed QQ expected app still taken from environment", e, sender.calls, r.legacyExpected)
	}
	if e = f.store.business.HandleJob(ctx, j.ID, j.Token, r); e != nil || sender.calls != 1 {
		t.Fatal("bound QQ source replayed", e, sender.calls)
	}
}

func shaCheckout231() []byte {
	key := sha256.Sum256(append([]byte("checkout-signing:"), make([]byte, 32)...))
	return key[:]
}
func (f accountsFixture231) sms(t *testing.T) (*businesssms.Store, businesssms.View, noticeconfig.View) {
	t.Helper()
	ctx := context.Background()
	v, e := f.config.Update(ctx, f.actor, "robot", noticeconfig.Input{ExpectedRevision: 0, Enabled: false, Action: "replace", Phone: "+8613800138000"})
	if e != nil {
		t.Fatal("SMS recipient required unrelated webhook", e)
	}
	key := shaCheckout231()
	gateway := smsgateway.New(f.store.pool, key)
	actor := smsgateway.Actor{User: f.actor.User, Token: f.actor.Token}
	before, e := gateway.Read(ctx, actor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = gateway.Save(ctx, actor, smsgateway.Input{Provider: "aliyun", Credentials: &smsgateway.Credentials{AccessKeyID: "AK231", AccessKeySecret: "synthetic-sms-secret231", Sign: "Test", Template: "SMS_VERIFY"}, Active: "aliyun", Status: 1, Expected: before.Fingerprint}); e != nil {
		t.Fatal(e)
	}
	sms, e := businesssms.FromMaster(f.store.pool, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	templates := map[string]string{}
	for _, p := range []string{"aliyun", "qcloud", "smsbao"} {
		for _, scene := range []string{"balance", "complain", "group"} {
			templates[p+"_"+scene] = ""
		}
	}
	templates["aliyun_complain"] = "SMS_COMPLAIN"
	sv, e := sms.Save(ctx, actor, businesssms.Input{ExpectedRevision: 0, Templates: templates})
	if e != nil {
		t.Fatal(e)
	}
	return sms, sv, v
}

func TestBusinessAccounts231AdminSMSClaimsBeforeIOAndNeverRepeatsUnknown(t *testing.T) {
	f := businessAccountsLocal231(t, "3")
	f.sms(t)
	j := f.complaint(t)
	sender := &accountSender231{fixture: f, kind: merchantnotice.SMS, recipient: "13800138000", err: errors.New("simulated unknown")}
	r := &accountResolver231{sender: sender, t: t, inspect: func(a businessnotice.Account) {
		if a.SMS231 == nil || a.SMS231.Snapshot.Phone != "13800138000" || a.SMS231.Snapshot.Template != "SMS_COMPLAIN" || a.SMS231.Credentials.Template != "SMS_VERIFY" {
			t.Fatal("administrator SMS lost original phone/template")
		}
	}}
	for i := 0; i < 2; i++ {
		if e := f.store.business.HandleJob(context.Background(), j.ID, j.Token, r); e != nil {
			t.Fatal(e)
		}
	}
	if sender.calls != 1 {
		t.Fatal("unknown administrator SMS retried", sender.calls)
	}
}

func TestBusinessAccounts231AdminSMSPhoneAndTemplateRotationsBlockOldSource(t *testing.T) {
	for _, change := range []string{"phone", "template"} {
		t.Run(change, func(t *testing.T) {
			f := businessAccountsLocal231(t, "3")
			sms, sv, v := f.sms(t)
			j := f.complaint(t)
			ctx := context.Background()
			if change == "phone" {
				if _, e := f.config.Update(ctx, f.actor, "robot", noticeconfig.Input{ExpectedRevision: v.Revision, Enabled: false, Action: "replace", Phone: "13900139000"}); e != nil {
					t.Fatal(e)
				}
			} else {
				sv.Templates["aliyun_complain"] = "SMS_OTHER"
				if _, e := sms.Save(ctx, smsgateway.Actor{User: f.actor.User, Token: f.actor.Token}, businesssms.Input{ExpectedRevision: sv.Revision, Templates: sv.Templates}); e != nil {
					t.Fatal(e)
				}
			}
			r := &accountResolver231{sender: &accountSender231{fixture: f, kind: merchantnotice.SMS, recipient: "unused"}, t: t}
			if e := f.store.business.HandleJob(ctx, j.ID, j.Token, r); e == nil || r.called != 0 {
				t.Fatal("old SMS source adopted different recipient/template", e, r.called)
			}
		})
	}
}
