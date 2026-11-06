package payment

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/identity"
	notice "goepay/internal/merchantnotice"
	"goepay/internal/platform/jobs"
	"goepay/internal/voiceconfig"
	"testing"
	"time"
)

type voiceSender230 struct{ calls int }

func (*voiceSender230) NoticeTransport() notice.Transport    { return notice.Voice }
func (*voiceSender230) NoticeScope() string                  { return "synthetic-voice230" }
func (*voiceSender230) ValidateNoticeRecipient(string) error { return nil }
func (s *voiceSender230) SendMerchantNotice(context.Context, string, notice.Message) error {
	s.calls++
	return nil
}

type voiceResolver230 struct {
	t      *testing.T
	sender *voiceSender230
}

func (r voiceResolver230) ResolveMerchantNotice(_ context.Context, k notice.Transport, a notice.Account) (notice.Sender, error) {
	r.t.Helper()
	if !a.VoiceResolved || !a.VoiceManaged || a.VoiceRevision <= 0 || a.VoiceCredential.AgentID != "agent230" {
		r.t.Fatal("voice configuration was not transaction-bound")
	}
	return r.sender, nil
}
func TestNotice230VoiceDoesNotQueueWhileDisabledOrAdoptHistoricalEvents(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	master := make([]byte, 32)
	if e := s.CreateMerchant(ctx, Merchant{ID: 230001, Name: "local230", APIKey: "synthetic230"}); e != nil {
		t.Fatal(e)
	}
	notifications, e := notice.NewStore(s.pool, master)
	if e != nil {
		t.Fatal(e)
	}
	device := "device230"
	if _, e = notifications.SaveSettings(ctx, 230001, 0, notice.SettingsWrite{VoiceOrder: true, Targets: notice.TargetPatch{VoiceDeviceID: &device}}, notice.Actor{Role: "merchant", ID: 230001, Name: "local230"}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) VALUES('voiceadmin230','admin',230,'Local','synthetic','super')`); e != nil {
		t.Fatal(e)
	}
	user := identity.User{ID: 230, Username: "voiceadmin230", Role: "admin", AdminClass: "super", AuthVersion: 1}
	sessions := identity.NewPersistentSessions(s.pool, false)
	token, csrf, e := sessions.Create(user)
	if e != nil {
		t.Fatal(e)
	}
	store, e := voiceconfig.FromMaster(s.pool, master)
	if e != nil {
		t.Fatal(e)
	}
	actor := voiceconfig.Actor{User: user, Token: token, CSRF: csrf}
	v, e := store.Update(ctx, actor, voiceconfig.Input{Action: "replace", AgentID: "agent230", Secret: "secret230"})
	if e != nil {
		t.Fatal(e)
	}
	set := func(enabled bool) {
		t.Helper()
		var e error
		v, e = store.Update(ctx, actor, voiceconfig.Input{ExpectedRevision: v.Revision, Enabled: enabled, Action: "keep"})
		if e != nil {
			t.Fatal(e)
		}
	}
	pay := func(no string) {
		t.Helper()
		o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 230001, OutTradeNo: no, Name: "local230", Type: "wxpay", Amount: 123})
		if e != nil {
			t.Fatal(e)
		}
		if _, own, e := s.BeginProviderAttempt(ctx, o.TradeNo, 7, "native"); e != nil || !own {
			t.Fatal(e)
		}
		if e = s.markPaid(ctx, o.TradeNo, "event-"+no, false, PaidReceipt{RealMoney: o.Amount, ChannelID: 7, ProviderTradeNo: "provider-" + no}); e != nil {
			t.Fatal(e)
		}
	}
	pay("voice-off230")
	var queued int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM jobs WHERE kind='merchant.notice'").Scan(&queued); e != nil || queued != 0 {
		t.Fatal("disabled voice queued historical notification", queued, e)
	}
	set(true)
	pay("voice-old230")
	old, e := (jobs.Queue{Pool: s.pool}).Claim(ctx, []string{"merchant.notice"}, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	set(false)
	set(true)
	sender := &voiceSender230{}
	if e = notifications.Deliver(ctx, old.ID, old.Token, notice.Voice, notice.GlobalSwitches{Voice: true}, voiceResolver230{t, sender}); e == nil || sender.calls != 0 {
		t.Fatal("new configuration adopted old event", e, sender.calls)
	}
	pay("voice-fresh230")
	fresh, e := (jobs.Queue{Pool: s.pool}).Claim(ctx, []string{"merchant.notice"}, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	cfg := s.pool.Config()
	cfg.MaxConns = 1
	low, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer low.Close()
	single, e := notice.NewStore(low, master)
	if e != nil {
		t.Fatal(e)
	}
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if e = single.Deliver(bounded, fresh.ID, fresh.Token, notice.Voice, notice.GlobalSwitches{Voice: true}, voiceResolver230{t, sender}); e != nil || sender.calls != 1 {
		t.Fatal("single-connection voice flow failed", e, sender.calls)
	}
}
