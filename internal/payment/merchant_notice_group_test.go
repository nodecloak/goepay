package payment

import (
	"context"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	notice "goepay/internal/merchantnotice"
)

func TestMerchantNotice80MembershipExpiryUsesAuthenticatedLatestPurchaseOnce(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	membership72Buyer(t, s, 80700, 0)
	if _, err := s.pool.Exec(ctx, `UPDATE merchants SET profile=profile||'{"email":"owner80@example.test"}'::jsonb WHERE id=80700`); err != nil {
		t.Fatal(err)
	}
	ns, err := notice.NewStore(s.pool, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	owner := notice.Actor{Role: "merchant", ID: 80700, Name: "merchant"}
	if _, err = ns.SaveSettings(ctx, 80700, 0, notice.SettingsWrite{Preferences: notice.Preferences{Group: 2}}, owner); err != nil {
		t.Fatal(err)
	}
	group := membership72Group(t, s, 0)
	purchase, err := s.PurchaseMembership(ctx, 80700, group, "notice-group80", "merchant:80700")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	original, err := s.NoticeExpiredMembership(ctx, tx, 80700, time.Now())
	if err != nil || original != nil {
		t.Fatal("active membership notified expiry", original, err)
	}
	tx.Rollback(ctx)
	source, err := s.readMembershipReferralSource(ctx, s.pool, purchase)
	if err != nil {
		t.Fatal(err)
	}
	// Synthetic time fixture only: regenerate the real independent source/event
	// proofs after shortening this random schema's free purchase to 300 ms.
	if err = s.pool.QueryRow(ctx, `SELECT clock_timestamp()+interval '300 milliseconds'`).Scan(&purchase.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	hash, err := s.membershipReferralSourceProof(purchase, source)
	if err != nil {
		t.Fatal(err)
	}
	eventHash, err := s.membershipReferralDigest("event", []any{purchase.ID, "source", fmt.Sprintf("membership-referral:%d:source", purchase.ID), purchase.Actor, hex.EncodeToString(hash)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, `ALTER TABLE membership_purchases DISABLE TRIGGER USER;ALTER TABLE membership_referral_sources DISABLE TRIGGER USER;ALTER TABLE membership_referral_events DISABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, `UPDATE membership_purchases SET expires_at=$2 WHERE id=$1`, purchase.ID, purchase.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, `UPDATE membership_referral_sources SET proof_hash=$2 WHERE purchase_id=$1`, purchase.ID, hash); err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, `UPDATE membership_referral_events SET subject_hash=$2,proof_hash=$3 WHERE purchase_id=$1 AND operation='source'`, purchase.ID, hash, eventHash); err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, `UPDATE memberships SET expires_at=$2 WHERE merchant_id=$1`, purchase.MerchantID, purchase.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, `ALTER TABLE membership_purchases ENABLE TRIGGER USER;ALTER TABLE membership_referral_sources ENABLE TRIGGER USER;ALTER TABLE membership_referral_events ENABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	time.Sleep(350 * time.Millisecond)
	page, err := ns.GenerateScheduledPage(ctx, 0, 200, 9, s)
	if err != nil || page.Created != 1 {
		t.Fatal("original expired purchase not scheduled", page, err)
	}
	events, err := ns.Events(ctx, 80700, 0, 20, owner)
	if err != nil || len(events) != 1 || events[0].Scene != "group" || len(events[0].Deliveries) != 1 || events[0].Deliveries[0].Transport != notice.SMTP {
		t.Fatal("missing or duplicate expiry routes", events, err)
	}
	again, err := ns.GenerateScheduledPage(ctx, 0, 200, 9, s)
	if err != nil || again.Created != 0 {
		t.Fatal("expiry source repeated", again, err)
	}
	if _, err = s.PurchaseMembership(ctx, 80700, group, "notice-group-renew80", "merchant:80700"); err != nil {
		t.Fatal(err)
	}
	again, err = ns.GenerateScheduledPage(ctx, 0, 200, 9, s)
	if err != nil || again.Created != 0 {
		t.Fatal("renewal adopted old expiry", again, err)
	}
}
