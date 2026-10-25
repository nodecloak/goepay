package payment

import (
	"context"
	"strings"
	"testing"
	"time"

	adapter "goepay/internal/adapters/payment"
	notice "goepay/internal/merchantnotice"
	"goepay/internal/platform/jobs"
)

func TestMerchantNotice80DailyTransfersUseOriginalRecipientTypeAndGroupOverride(t *testing.T) {
	for _, scenario := range []struct {
		name, group     string
		globalOff, want bool
	}{{"enabled", "", false, true}, {"legacy-group-zero-inherits", "0", false, true}, {"group-overrides-global-off", "1", true, true}, {"inherited-global-off", "0", true, false}} {
		t.Run(scenario.name, func(t *testing.T) {
			s, g, _ := transferFixture(t)
			ctx := context.Background()
			if _, err := s.pool.Exec(ctx, `UPDATE merchants SET profile=profile||'{"email":"owner80@example.test"}'::jsonb WHERE id=501`); err != nil {
				t.Fatal(err)
			}
			if scenario.group != "" {
				var gid int64
				if err := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups',jsonb_build_object('name','local-report80','enabled',true,'user_transfer',$1::text)) RETURNING id`, scenario.group).Scan(&gid); err != nil {
					t.Fatal(err)
				}
				if _, err := s.pool.Exec(ctx, `UPDATE merchants SET base_group_id=$2 WHERE id=$1`, int64(501), gid); err != nil {
					t.Fatal(err)
				}
			}
			transfer, err := g.Transfer(ctx, 501, 7, "notice-daily80", adapter.TransferRequest{Type: "wxpay", Account: "original-openid80", Name: "Owner", Remark: "Local test", Amount: 100})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = g.QueryTransfer(ctx, 501, transfer.TransferNo); err != nil {
				t.Fatal(err)
			}
			if scenario.globalOff {
				transferSetting(t, s, "transfer_enabled", "false")
			}
			var at time.Time
			if err = s.pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); err != nil {
				t.Fatal(err)
			}
			from, _, _, _ := notice.DailyWindow(at, 0)
			if _, err = s.pool.Exec(ctx, `UPDATE transfers SET created_at=$2 WHERE id=$1`, transfer.ID, from.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			ns, err := notice.NewStore(s.pool, make([]byte, 32))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = ns.SaveSettings(ctx, 501, 0, notice.SettingsWrite{Preferences: notice.Preferences{DailyReport: 2}}, notice.Actor{Role: "merchant", ID: 501, Name: "merchant"}); err != nil {
				t.Fatal(err)
			}
			if _, err = ns.GenerateScheduledPage(ctx, 0, 200, 0, s); err != nil {
				t.Fatal(err)
			}
			job, err := (jobs.Queue{Pool: s.pool}).Claim(ctx, []string{"merchant.notice"}, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			sender := &recordedNoticeSender80{}
			if err = ns.Deliver(ctx, job.ID, job.Token, notice.SMTP, notice.GlobalSwitches{}, noticeResolverFunc80(func(context.Context, notice.Transport, notice.Account) (notice.Sender, error) { return sender, nil })); err != nil {
				t.Fatal(err)
			}
			hasTransfer := strings.Contains(sender.text, "代付总额：¥1.00") && strings.Contains(sender.text, "微信代付：¥1.00")
			if hasTransfer != scenario.want {
				t.Fatal("daily transfer condition/type mismatch", scenario.name, sender.text)
			}
		})
	}
}
