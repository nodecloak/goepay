package payment

import (
	"context"
	"fmt"
	"os"
	"testing"
)

func TestBaselineRepairRejectsPostPurchaseMergedAuditAndIsIdempotent(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	feeMerchant(t, s, 610, 1200, "platform")
	var base, vip, manual int64
	if e := s.pool.QueryRow(ctx, "SELECT base_group_id FROM merchants WHERE id=610").Scan(&base); e != nil {
		t.Fatal(e)
	}
	if e := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"repair-vip","enabled":true,"rate_bps":"100"}') RETURNING id`).Scan(&vip); e != nil {
		t.Fatal(e)
	}
	if e := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"repair-manual","enabled":true,"rate_bps":"500"}') RETURNING id`).Scan(&manual); e != nil {
		t.Fatal(e)
	}
	for _, mid := range []int64{610, 611, 612, 613} {
		if mid != 610 {
			if e := s.CreateMerchant(ctx, Merchant{ID: mid, Name: "legacy", APIKey: "repair-test-secret"}); e != nil {
				t.Fatal(e)
			}
		}
		if mid != 613 {
			if _, e := s.pool.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes,created_at) VALUES('admin',1,'admin','merchants',$1,'create',jsonb_build_object('group_id',$2::text),now()-interval '3 hours')`, mid, fmt.Sprint(base)); e != nil {
				t.Fatal(e)
			}
		}
		if _, e := s.pool.Exec(ctx, `INSERT INTO membership_purchases(merchant_id,group_id,external_no,price,group_name,duration_days,expires_at,actor,created_at) VALUES($1,$2,'legacy',0,'vip',30,now()+interval '30 days','merchant',now()-interval '2 hours')`, mid, vip); e != nil {
			t.Fatal(e)
		}
		if _, e := s.pool.Exec(ctx, `INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES($1,$2,now()+interval '30 days')`, mid, vip); e != nil {
			t.Fatal(e)
		}
		if _, e := s.pool.Exec(ctx, `UPDATE merchants SET profile=jsonb_build_object('group_id',$2::text) WHERE id=$1`, mid, fmt.Sprint(vip)); e != nil {
			t.Fatal(e)
		}
		if mid != 611 {
			audited := vip
			if mid == 612 {
				audited = manual
			}
			if _, e := s.pool.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes,created_at) VALUES('admin',1,'admin','merchants',$1,'update',jsonb_build_object('name','only-name-edited','email','new@example.invalid','group_id',$2::text),now()-interval '1 hour')`, mid, fmt.Sprint(audited)); e != nil {
				t.Fatal(e)
			}
		}
	}
	// This is the false recovery produced by frozen migration 18: an unrelated
	// edit's merged snapshot was accepted as an administrator group assignment.
	var wrong int64
	var unresolved bool
	if e := s.pool.QueryRow(ctx, "SELECT base_group_id,group_baseline_unresolved FROM merchants WHERE id=610").Scan(&wrong, &unresolved); e != nil || wrong != vip || unresolved {
		t.Fatalf("legacy fixture %d,%v %v", wrong, unresolved, e)
	}
	raw, e := os.ReadFile("../../db/migrations/000019_membership_baseline_evidence.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, string(raw)); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		mid, baseline int64
		unresolved    bool
	}{{610, 0, true}, {611, base, false}, {612, manual, false}, {613, 0, true}} {
		var actual int64
		var ambiguous bool
		if e = s.pool.QueryRow(ctx, "SELECT base_group_id,group_baseline_unresolved FROM merchants WHERE id=$1", tc.mid).Scan(&actual, &ambiguous); e != nil || actual != tc.baseline || ambiguous != tc.unresolved {
			t.Fatalf("repair%+v got%d,%v %v", tc, actual, ambiguous, e)
		}
	}
	active, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 610, OutTradeNo: "repair-active", Amount: 10000})
	if e != nil || active.FeeRateBPS != 100 {
		t.Fatalf("active purchase %+v %v", active, e)
	}
	if _, e = s.pool.Exec(ctx, "UPDATE memberships SET expires_at=now()-interval '1 second'"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateOrder(ctx, CreateOrderInput{MerchantID: 610, OutTradeNo: "repair-expired", Amount: 10000}); e == nil {
		t.Fatal("merged name/email snapshot created permanent membership discount")
	}
	if _, e = s.pool.Exec(ctx, `UPDATE merchants SET profile=jsonb_set(profile,'{group_id}',to_jsonb($1::text)) WHERE id=610`, fmt.Sprint(base)); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, string(raw)); e != nil {
		t.Fatal("repair rerun", e)
	}
	restored, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 610, OutTradeNo: "repair-confirmed", Amount: 10000})
	if e != nil || restored.FeeRateBPS != 1200 {
		t.Fatalf("repair overwrote later manual assignment %+v %v", restored, e)
	}
	var records int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM membership_baseline_repairs").Scan(&records); e != nil || records != 4 {
		t.Fatalf("nonidempotent repair records%d %v", records, e)
	}
	if _, e = s.pool.Exec(ctx, "UPDATE membership_baseline_repairs SET reason='changed'"); e == nil {
		t.Fatal("repair evidence mutable")
	}
}

func TestBaselineRepairUsesRealMigrationBoundaryAndPreservesLaterAssignments(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	feeMerchant(t, s, 620, 1200, "platform")
	var base, vip int64
	if e := s.pool.QueryRow(ctx, "SELECT base_group_id FROM merchants WHERE id=620").Scan(&base); e != nil {
		t.Fatal(e)
	}
	if e := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"boundary-vip","enabled":true,"rate_bps":"100"}') RETURNING id`).Scan(&vip); e != nil {
		t.Fatal(e)
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := s.pool.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`CREATE TABLE schema_migrations(name text PRIMARY KEY,sha256 text NOT NULL,applied_at timestamptz NOT NULL); INSERT INTO schema_migrations VALUES('000018_membership_baseline.up.sql','test-only',now()-interval '1 hour')`)
	for _, mid := range []int64{620, 621, 622, 623} {
		if mid != 620 {
			if e := s.CreateMerchant(ctx, Merchant{ID: mid, Name: "boundary", APIKey: "boundary-test-secret"}); e != nil {
				t.Fatal(e)
			}
		}
		exec(`INSERT INTO membership_purchases(merchant_id,group_id,external_no,price,group_name,duration_days,expires_at,actor,created_at) VALUES($1,$2,'legacy',0,'vip',30,now()+interval '30 days','merchant',now()-interval '3 hours')`, mid, vip)
		exec(`INSERT INTO memberships VALUES($1,$2,now()+interval '30 days')`, mid, vip)
		exec(`INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes,created_at) VALUES('admin',1,'admin','merchants',$1,'create',jsonb_build_object('group_id',$2::text),now()-interval '4 hours')`, mid, fmt.Sprint(base))
		if mid != 621 {
			exec(`INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes,created_at) VALUES('admin',1,'admin','merchants',$1,'update',jsonb_build_object('name','merged-old','group_id',$2::text),now()-interval '2 hours')`, mid, fmt.Sprint(vip))
		}
		group := vip
		if mid == 620 {
			group = base
		}
		exec(`UPDATE merchants SET profile=jsonb_build_object('group_id',$2::text) WHERE id=$1`, mid, fmt.Sprint(group))
		if mid == 620 || mid == 621 {
			exec(`INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes,created_at) VALUES('admin',1,'admin','merchants',$1,'update',jsonb_build_object('group_id',$2::text),now()-interval '50 minutes')`, mid, fmt.Sprint(group))
			exec(`INSERT INTO membership_purchases(merchant_id,group_id,external_no,price,group_name,duration_days,expires_at,actor,created_at) VALUES($1,$2,'post-18',0,'new-membership',30,now()+interval '30 days','merchant',now()-interval '40 minutes')`, mid, group)
		}
		if mid == 623 {
			exec(`INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes,created_at) VALUES('admin',1,'admin','merchants',$1,'update',jsonb_build_object('group_id',$2::text,'group_assignment',jsonb_build_object('version',1,'source','confirmed','assigned',$2::text)),now()-interval '30 minutes')`, mid, fmt.Sprint(group))
		} else {
			exec(`INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes,created_at) VALUES('admin',1,'admin','merchants',$1,'update',jsonb_build_object('name','new-name','email','edit@example.invalid','group_id',$2::text),now()-interval '30 minutes')`, mid, fmt.Sprint(group))
		}
	}
	raw, e := os.ReadFile("../../db/migrations/000019_membership_baseline_evidence.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	exec(string(raw))
	for _, tc := range []struct {
		mid, group int64
		unresolved bool
	}{{620, base, false}, {621, vip, false}, {622, 0, true}, {623, vip, false}} {
		var group int64
		var unresolved bool
		if e = s.pool.QueryRow(ctx, "SELECT base_group_id,group_baseline_unresolved FROM merchants WHERE id=$1", tc.mid).Scan(&group, &unresolved); e != nil || group != tc.group || unresolved != tc.unresolved {
			t.Fatalf("migration boundary %+v ->%d,%v %v", tc, group, unresolved, e)
		}
	}
}
