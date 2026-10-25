package merchantnotice_test

import (
	"context"
	"testing"
	"time"

	notice "goepay/internal/merchantnotice"
	"goepay/internal/platform/jobs"
)

type unknownSender80 struct{}

func (unknownSender80) NoticeTransport() notice.Transport    { return notice.SMTP }
func (unknownSender80) NoticeScope() string                  { return "local-integrity80" }
func (unknownSender80) ValidateNoticeRecipient(string) error { return nil }
func (unknownSender80) SendMerchantNotice(context.Context, string, notice.Message) error {
	panic("simulated lost process after permanent claim")
}

type integrityResolver80 struct{}

func (integrityResolver80) ResolveMerchantNotice(context.Context, notice.Transport, notice.Account) (notice.Sender, error) {
	return unknownSender80{}, nil
}

func TestMerchantNotice80IntegrityChecksHistoricalProfilesAndUnknownClaimsWithoutRepair(t *testing.T) {
	s, pool := profileStore80(t)
	ctx := context.Background()
	first, err := s.SaveSettings(ctx, 1000, 0, notice.SettingsWrite{Preferences: notice.Preferences{Login: 2}}, owner80)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES('notice-owner80','merchant',1000,'Local','synthetic-only80')`); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var audit int64
	if err = tx.QueryRow(ctx, `INSERT INTO console_login_audit(username,role,ip,success,subject_id,auth_version,source_transaction) VALUES('notice-owner80','merchant','192.0.2.80',true,1000,1,pg_current_xact_id()) RETURNING id`).Scan(&audit); err != nil {
		t.Fatal(err)
	}
	if err = s.CaptureLoginAudit(ctx, tx, audit); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	job, err := (jobs.Queue{Pool: pool}).Claim(ctx, []string{"merchant.notice"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("simulated interruption did not occur")
			}
		}()
		_ = s.Deliver(ctx, job.ID, job.Token, notice.SMTP, notice.GlobalSwitches{MailRobot: true}, integrityResolver80{})
	}()
	report, err := s.Check(ctx)
	if err != nil || report.InvalidProfiles != 0 || report.InvalidEvents != 0 || report.InvalidDeliveries != 0 || report.UnknownDeliveries != 1 {
		t.Fatal("lost ACK must remain unknown", report, err)
	}
	views, err := s.Events(ctx, 1000, 0, 20, owner80)
	if err != nil || len(views) != 1 || views[0].State != "unknown" {
		t.Fatal("unconfirmed claim was displayed as accepted", views, err)
	}
	if _, err = s.SaveSettings(ctx, 1000, first.ID, notice.SettingsWrite{Preferences: notice.Preferences{Login: 4}}, owner80); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `ALTER TABLE merchant_notice_profiles DISABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE merchant_notice_profiles SET payload_cipher=decode(repeat('ef',40),'hex') WHERE id=$1`, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `ALTER TABLE merchant_notice_profiles ENABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	report, err = s.Check(ctx)
	if err != nil || report.InvalidProfiles != 1 || report.InvalidDeliveries != 1 {
		t.Fatal("historical corruption was hidden by newer head", report, err)
	}
	if _, err = s.Events(ctx, 1000, 0, 20, owner80); err == nil {
		t.Fatal("event view hid corruption of its historical profile")
	}
	var claims, results int
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM merchant_notice_claims),(SELECT count(*) FROM merchant_notice_results)`).Scan(&claims, &results); err != nil || claims != 1 || results != 0 {
		t.Fatal("check repaired/replayed unknown claim", claims, results, err)
	}
}

func TestMerchantNotice80IntegrityFindsTimerSourceWhoseEventAndAuditsWereRemoved(t *testing.T) {
	s, pool := profileStore80(t)
	ctx := context.Background()
	if _, err := s.SaveSettings(ctx, 1000, 0, notice.SettingsWrite{Preferences: notice.Preferences{Balance: 2, BalanceMoney: 1}}, owner80); err != nil {
		t.Fatal(err)
	}
	if page, err := s.GenerateScheduledPage(ctx, 0, 200, 9, nil); err != nil || page.Created != 1 {
		t.Fatal(page, err)
	}
	// Only this random schema: model selective event/audit loss, leaving the
	// independent timer source behind. A missing audit alone is not the check.
	if _, err := pool.Exec(ctx, `ALTER TABLE merchant_notice_events DISABLE TRIGGER USER;ALTER TABLE backoffice_audit DISABLE TRIGGER USER;DELETE FROM merchant_notice_events;DELETE FROM backoffice_audit WHERE resource IN('merchant-notice-events','merchant-notice-timers');ALTER TABLE merchant_notice_events ENABLE TRIGGER USER;ALTER TABLE backoffice_audit ENABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	report, err := s.Check(ctx)
	if err != nil || report.InvalidEvents == 0 {
		t.Fatal("orphaned original timer hidden", report, err)
	}
}
