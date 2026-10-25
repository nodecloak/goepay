package payment

import (
	"context"
	"errors"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/platform/jobs"
	"strings"
	"testing"
	"time"

	notice "goepay/internal/merchantnotice"
)

func TestMerchantNotice80SettlementBornOnlyWithOriginalSuccessfulPayout(t *testing.T) {
	s, g, p, v := payoutFixture(t)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `UPDATE merchants SET profile=profile||'{"email":"owner80@example.test"}'::jsonb WHERE id=501`); err != nil {
		t.Fatal(err)
	}
	ns, err := notice.NewStore(s.pool, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	actor := notice.Actor{Role: "merchant", ID: 501, Name: "merchant"}
	if _, err = ns.SaveSettings(ctx, 501, 0, notice.SettingsWrite{Preferences: notice.Preferences{Settle: 2}}, actor); err != nil {
		t.Fatal(err)
	}
	out, err := g.PrepareSettlementPayout(ctx, v.ID, 7, settlementAdmin, nil)
	if err != nil {
		t.Fatal(err)
	}
	p.unknown = true
	var unknown *adapter.UnknownOutcome
	if _, err = g.ExecuteSettlementPayout(ctx, v.ID, settlementAdmin, nil); !errors.As(err, &unknown) {
		t.Fatal(err)
	}
	events, err := ns.Events(ctx, 501, 0, 20, actor)
	if err != nil || len(events) != 0 {
		t.Fatal("unknown payout notified success", events, err)
	}
	if _, err = g.QuerySettlementPayout(ctx, v.ID, settlementAdmin); err != nil {
		t.Fatal(err)
	}
	events, err = ns.Events(ctx, 501, 0, 20, actor)
	if err != nil || len(events) != 1 || events[0].Scene != "settle" || events[0].State != "queued" {
		t.Fatal("successful original payout lost notice", events, err)
	}
	if _, err = g.QuerySettlementPayout(ctx, v.ID, settlementAdmin); err != nil {
		t.Fatal(err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = ns.CaptureSettlement(ctx, tx, out.Transfer.ID, "original-payout-openid"); !errors.Is(err, notice.ErrConflict) {
		t.Fatal("historical success recaptured", err)
	}
	tx.Rollback(ctx)
	sender := &recordedNoticeSender80{}
	job, err := (jobs.Queue{Pool: s.pool}).Claim(ctx, []string{"merchant.notice"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	err = ns.Deliver(ctx, job.ID, job.Token, notice.SMTP, notice.GlobalSwitches{MailRobot: true}, noticeResolverFunc80(func(context.Context, notice.Transport, notice.Account) (notice.Sender, error) { return sender, nil }))
	if err != nil || sender.calls.Load() != 1 || !strings.Contains(sender.text, "10.00") || !strings.Contains(sender.text, "9.70") || !strings.Contains(sender.text, "original-payout-openid") {
		t.Fatal("wrong original settlement notice", err, sender.text)
	}
}

func TestMerchantNotice80SettlementBankNoticeSurvivesReturnWithoutReplayingSuccess(t *testing.T) {
	for _, product := range []string{adapter.BankProductWechat, adapter.BankProductAlipay} {
		t.Run(product, func(t *testing.T) {
			s, g, v, mode := settlementBankFixture(t, product)
			ctx := context.Background()
			ns, err := notice.NewStore(s.pool, make([]byte, 32))
			if err != nil {
				t.Fatal(err)
			}
			actor := notice.Actor{Role: "merchant", ID: 501, Name: "merchant"}
			if _, err = ns.SaveSettings(ctx, 501, 0, notice.SettingsWrite{Preferences: notice.Preferences{Settle: 2}}, actor); err != nil {
				t.Fatal(err)
			}
			if _, err = g.PrepareSettlementPayout(ctx, v.ID, 7, settlementAdmin, nil); err != nil {
				t.Fatal(err)
			}
			if _, err = g.ExecuteSettlementPayout(ctx, v.ID, settlementAdmin, nil); err != nil {
				t.Fatal(err)
			}
			if _, err = g.QuerySettlementPayout(ctx, v.ID, settlementAdmin); err != nil {
				t.Fatal(err)
			}
			events, err := ns.Events(ctx, 501, 0, 20, actor)
			if err != nil || len(events) != 1 || events[0].Scene != "settle" {
				t.Fatal("missing original bank notice", events, err)
			}
			mode("returned")
			if _, err = g.QuerySettlementPayout(ctx, v.ID, settlementAdmin); err != nil {
				t.Fatal(err)
			}
			events, err = ns.Events(ctx, 501, 0, 20, actor)
			if err != nil || len(events) != 1 {
				t.Fatal("return invalidated history or added success", events, err)
			}
		})
	}
}

func TestMerchantNotice80SettlementProjectionFailureCannotRollBackPayout(t *testing.T) {
	s, g, _, v := payoutFixture(t)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `CREATE FUNCTION fail_settlement_notice80() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic optional notice failure'; END $$;CREATE TRIGGER fail_settlement_notice80 BEFORE INSERT ON merchant_notice_events FOR EACH ROW EXECUTE FUNCTION fail_settlement_notice80()`); err != nil {
		t.Fatal(err)
	}
	if _, err := g.PrepareSettlementPayout(ctx, v.ID, 7, settlementAdmin, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := g.ExecuteSettlementPayout(ctx, v.ID, settlementAdmin, nil); err != nil {
		t.Fatal(err)
	}
	out, err := g.QuerySettlementPayout(ctx, v.ID, settlementAdmin)
	if err != nil || out.State != "succeeded" {
		t.Fatal("optional notice undid successful payout", out, err)
	}
	balance, err := s.Balance(ctx, 501)
	if err != nil || balance.Reserved != 0 || balance.Available != 0 {
		t.Fatal(balance, err)
	}
}
