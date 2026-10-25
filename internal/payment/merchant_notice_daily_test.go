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
	for _, groupOff := range []bool{false, true} {
		t.Run(map[bool]string{false: "enabled", true: "group-disabled"}[groupOff], func(t *testing.T) {
			s, g, _ := transferFixture(t)
			ctx := context.Background()
			if _, err := s.pool.Exec(ctx, `UPDATE merchants SET profile=profile||'{"email":"owner80@example.test"}'::jsonb WHERE id=501`); err != nil {
				t.Fatal(err)
			}
			if groupOff {
				var gid int64
				if err := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"local-report80","enabled":true,"user_transfer":"0"}') RETURNING id`).Scan(&gid); err != nil {
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
			if hasTransfer == groupOff {
				t.Fatal("daily transfer condition/type mismatch", groupOff, sender.text)
			}
		})
	}
}
