package payment

import (
	"context"
	"fmt"
	"testing"
)

func TestMembershipReferral72CheckerInspectsSourceAndOrphanAuditBeyondFirstPage(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	membership72Buyer(t, s, 7291, 0)
	gid := membership72Group(t, s, 0)
	var last MembershipPurchase
	for i := 0; i < 102; i++ {
		var e error
		last, e = s.PurchaseMembership(ctx, 7291, gid, fmt.Sprintf("member72-tail-%03d", i), "merchant:7291")
		if e != nil {
			t.Fatal(i, e)
		}
	}
	check, e := s.CheckLedger(ctx)
	if e != nil || check.InvalidReferralTerms != 0 {
		t.Fatal("valid full pages", check, e)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "SET LOCAL session_replication_role=replica"); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "DELETE FROM membership_referral_sources WHERE purchase_id=$1", last.ID); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	check, e = s.CheckLedger(ctx)
	if e != nil || check.InvalidReferralTerms == 0 {
		t.Fatal("tail missing source/orphan audit escaped", check, e)
	}
}
