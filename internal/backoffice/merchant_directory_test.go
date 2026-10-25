package backoffice

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func TestMerchantDirectoryQueryRejectsAmbiguousAndUnrepresentableValues(t *testing.T) {
	for _, raw := range []string{"page=01", "from=0000-01-01", "limit=101", "uid=9223372036854775808", "column=uid&q=01", "pay=2&dstatus=pay_0", "uid=1&uid=2", "q=" + url.QueryEscape(strings.Repeat("中", 86)), "column=api_key", "order=money_desc%3BDELETE"} {
		values, err := url.ParseQuery(raw)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err = ParseMerchantDirectoryQuery(values); err == nil {
			t.Errorf("accepted invalid filter %q", raw)
		}
	}
	for _, raw := range []string{"page=100000&limit=100", "uid=9223372036854775807", "gid=0", "from=2024-02-29&to=2024-02-29", "q=" + url.QueryEscape(strings.Repeat("中", 85)), "order=id_desc"} {
		values, _ := url.ParseQuery(raw)
		if _, _, err := ParseMerchantDirectoryQuery(values); err != nil {
			t.Errorf("rejected valid filter %q: %v", raw, err)
		}
	}
}

func TestMerchantDirectoryUsesEffectiveGroupOwnedContactAndImmutableLogin(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	base, err := s.Create(ctx, admin, "groups", input(`{"name":"目录基础组","rate_bps":"500"}`))
	if err != nil {
		t.Fatal(err)
	}
	vip, err := s.Create(ctx, admin, "groups", input(`{"name":"目录会员组","rate_bps":"100","price_cents":"0","duration_days":"30"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Update(ctx, admin, "merchants", 101, input(fmt.Sprintf(`{"group_id":"%d"}`, base.ID))); err != nil {
		t.Fatal(err)
	}
	if _, err = s.store.PurchaseMembership(ctx, 101, vip.ID, "directory-membership", "merchant:101"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, `UPDATE merchants SET profile=profile||'{"email":"owner@example.invalid","phone":"13000000001"}'::jsonb WHERE id=101;
 INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES('directory-owner','merchant',101,'目录账户','unused');
 INSERT INTO console_login_audit(username,role,ip,success,subject_id,auth_version,source_transaction,created_at) VALUES
 ('old-username','merchant','192.0.2.1',true,101,1,pg_current_xact_id(),now()-interval '1 day'),
 ('other-username','merchant','192.0.2.2',true,102,1,pg_current_xact_id(),now());
 INSERT INTO console_login_audit(username,role,ip,success) VALUES('directory-owner','merchant','192.0.2.3',false),('directory-owner','merchant','192.0.2.4',true)`); err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct {
		domain            string
		approved, enabled bool
	}{{"owned.example.invalid", true, true}, {"pending.example.invalid", false, true}, {"disabled.example.invalid", true, false}} {
		if _, err = s.Create(ctx, admin, "domains", input(fmt.Sprintf(`{"merchant_id":"101","domain":"%s","approved":%t,"enabled":%t}`, v.domain, v.approved, v.enabled))); err != nil {
			t.Fatal(err)
		}
	}
	check := func(raw string, want ...int64) {
		t.Helper()
		values, _ := url.ParseQuery(raw)
		f, search, e := ParseMerchantDirectoryQuery(values)
		if e != nil {
			t.Fatal(e)
		}
		page, e := s.List(ctx, admin, "merchants", Query{Page: 1, Limit: 20, Search: search, Merchant: f})
		if e != nil {
			t.Fatalf("%s: %v", raw, e)
		}
		if page.Total != int64(len(want)) || len(page.Items) != len(want) {
			t.Fatalf("%s: total=%d items=%d", raw, page.Total, len(page.Items))
		}
		for i, id := range want {
			if page.Items[i].ID != id {
				t.Fatalf("%s: item%d got%d want%d", raw, i, page.Items[i].ID, id)
			}
		}
	}
	check(fmt.Sprintf("gid=%d", vip.ID), 101)
	check(fmt.Sprintf("gid=%d", base.ID))
	check("gid=0", 102)
	check("column=email&q=owner%40example.invalid", 101)
	check("column=phone&q=13000000001", 101)
	check("column=login_username&q=directory-owner", 101)
	check("column=domain&q=owned.example.invalid", 101)
	check("column=domain&q=pending.example.invalid")
	check("column=domain&q=disabled.example.invalid")
	check("order=lasttime_desc", 102, 101)
	check("order=lasttime_asc", 101, 102)
	check("upid=101")
	if _, err = s.pool.Exec(ctx, `UPDATE memberships SET expires_at=now()-interval '1 second' WHERE merchant_id=101`); err != nil {
		t.Fatal(err)
	}
	check(fmt.Sprintf("gid=%d", base.ID), 101)
	check(fmt.Sprintf("gid=%d", vip.ID))
	if _, err = s.pool.Exec(ctx, `UPDATE merchants SET group_baseline_unresolved=true WHERE id=101`); err != nil {
		t.Fatal(err)
	}
	check("gid=0", 102)
	check(fmt.Sprintf("gid=%d", base.ID))
}

func TestMerchantDirectoryRegistrationDatesUseShanghaiInclusiveCalendarDays(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	for _, v := range []struct {
		id int
		at string
	}{{201, "2026-10-24T23:59:59.999999+08:00"}, {202, "2026-10-25T00:00:00+08:00"}, {203, "2026-10-25T23:59:59.999999+08:00"}, {204, "2026-10-26T00:00:00+08:00"}} {
		if _, err := s.pool.Exec(ctx, `INSERT INTO merchants(id,name,api_key,created_at) SELECT $1,'本地日期边界',api_key,$2::timestamptz FROM merchants WHERE id=101`, v.id, v.at); err != nil {
			t.Fatal(err)
		}
	}
	values, _ := url.ParseQuery("column=name&q=" + url.QueryEscape("本地日期边界") + "&from=2026-10-25&to=2026-10-25")
	f, search, err := ParseMerchantDirectoryQuery(values)
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.List(ctx, admin, "merchants", Query{Page: 1, Limit: 1, Search: search, Merchant: f})
	if err != nil || page.Total != 2 || len(page.Items) != 1 || page.Items[0].ID != 203 {
		t.Fatalf("first boundary page %#v %v", page, err)
	}
	page, err = s.List(ctx, admin, "merchants", Query{Page: 2, Limit: 1, Search: search, Merchant: f})
	if err != nil || page.Total != 2 || len(page.Items) != 1 || page.Items[0].ID != 202 {
		t.Fatalf("second boundary page %#v %v", page, err)
	}
}
