package payment

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	notice "goepay/internal/merchantnotice"
	"goepay/internal/platform/jobs"
)

func scheduler171ExpiredPurchase(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	membership72Buyer(t, s, 901, 0)
	if _, e := s.pool.Exec(ctx, `UPDATE merchants SET profile=profile||'{"email":"synthetic171@example.test"}'::jsonb WHERE id=901`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.notice.SaveSettings(ctx, 901, 0, notice.SettingsWrite{Preferences: notice.Preferences{Group: 2, DailyReport: 2, Balance: 2, BalanceMoney: 100}}, notice.Actor{Role: "merchant", ID: 901, Name: "Synthetic Owner"}); e != nil {
		t.Fatal(e)
	}
	group := membership72Group(t, s, 0)
	p, e := s.PurchaseMembership(ctx, 901, group, "scheduler171-expiry", "merchant:901")
	if e != nil {
		t.Fatal(e)
	}
	source, e := s.readMembershipReferralSource(ctx, s.pool, p)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.pool.QueryRow(ctx, `SELECT clock_timestamp()+interval '100 milliseconds'`).Scan(&p.ExpiresAt); e != nil {
		t.Fatal(e)
	}
	hash, e := s.membershipReferralSourceProof(p, source)
	if e != nil {
		t.Fatal(e)
	}
	event, e := s.membershipReferralDigest("event", []any{p.ID, "source", fmt.Sprintf("membership-referral:%d:source", p.ID), p.Actor, hex.EncodeToString(hash)})
	if e != nil {
		t.Fatal(e)
	}
	// Only this isolated synthetic schema shortens the actual free purchase; all
	// independent original source/event proofs are regenerated consistently.
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	for _, table := range []string{"membership_purchases", "membership_referral_sources", "membership_referral_events"} {
		if _, e = tx.Exec(ctx, `ALTER TABLE `+table+` DISABLE TRIGGER USER`); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = tx.Exec(ctx, `UPDATE membership_purchases SET expires_at=$2 WHERE id=$1`, p.ID, p.ExpiresAt); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `UPDATE membership_referral_sources SET proof_hash=$2 WHERE purchase_id=$1`, p.ID, hash); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `UPDATE membership_referral_events SET subject_hash=$2,proof_hash=$3 WHERE purchase_id=$1 AND operation='source'`, p.ID, hash, event); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `UPDATE memberships SET expires_at=$2 WHERE merchant_id=$1`, p.MerchantID, p.ExpiresAt); e != nil {
		t.Fatal(e)
	}
	for _, table := range []string{"membership_purchases", "membership_referral_sources", "membership_referral_events"} {
		if _, e = tx.Exec(ctx, `ALTER TABLE `+table+` ENABLE TRIGGER USER`); e != nil {
			t.Fatal(e)
		}
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	time.Sleep(120 * time.Millisecond)
}

func TestScheduler171OrderPublishesAuthenticatedExpiryOnlyAndRetainsHistory(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := scheduler171Admin(t, s)
	scheduler171ExpiredPurchase(t, s)
	before, e := s.ScheduledTasks(ctx, a, "order")
	if e != nil || before[0].ControlMode != "legacy_managed" {
		t.Fatal("unconfigured daily core falsely advertised stopped despite original group/referral consumers", before, e)
	}
	scheduler171ConfigureManaged(t, s, a, "order", true)
	scheduler171Pending(t, s, a, "order")
	if tick, e := s.RunScheduledTasksOnce(ctx, 20); e != nil || tick.Completed != 1 || tick.Failed != 0 {
		t.Fatal("original daily core not executable", tick, e)
	}
	var group, daily, balance, purchases int
	if e := s.pool.QueryRow(ctx, `SELECT(SELECT count(*)FROM merchant_notice_timer_sources WHERE scene='group'),(SELECT count(*)FROM merchant_notice_timer_sources WHERE scene='daily_report'),(SELECT count(*)FROM merchant_notice_timer_sources WHERE scene='balance'),(SELECT count(*)FROM membership_purchases)`).Scan(&group, &daily, &balance, &purchases); e != nil || group != 1 || daily != 0 || balance != 0 || purchases != 1 {
		t.Fatal("daily scope/history", group, daily, balance, purchases, e)
	}
	// Legacy periodic publisher cannot bypass an explicitly disabled Order policy.
	v, e := s.ScheduledTasks(ctx, a, "order")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.UpdateScheduledTask(ctx, a, "order", ScheduledTaskInput{Enabled: false, Schedule: *v[0].Schedule, Params: ScheduledTaskParams{}, ExpectedFingerprint: v[0].Fingerprint}); e != nil {
		t.Fatal(e)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	allowed, e := s.ScheduledNoticeSceneLegacyAllowedTx(ctx, tx, "group")
	tx.Rollback(ctx)
	if e != nil || allowed {
		t.Fatal("disabled daily policy ignored by legacy publisher", allowed, e)
	}
	var calls atomic.Int32
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(204) }))
	defer sink.Close()
	q := jobs.Queue{Pool: s.pool}
	j, e := q.Claim(ctx, []string{"merchant.notice"}, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	resolver := scheduler171NoticeResolver{&scheduler171NoticeSender{sink.URL, sink.Client()}}
	for range 2 {
		if e = s.notice.Deliver(ctx, j.ID, j.Token, notice.SMTP, notice.GlobalSwitches{MailRobot: true}, resolver); e != nil {
			t.Fatal(e)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("expiry publication had no actual idempotent consumer", calls.Load())
	}
}
