package backoffice

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestMerchantDisplayUsesEffectiveMembershipAndApprovedOwnedDomains(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	base, err := s.Create(ctx, admin, "groups", input(`{"name":"Baseline","rate_bps":"500"}`))
	if err != nil {
		t.Fatal(err)
	}
	vip, err := s.Create(ctx, admin, "groups", input(`{"name":"Purchased","rate_bps":"100","price_cents":"0","duration_days":"30"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Update(ctx, admin, "merchants", 101, input(fmt.Sprintf(`{"group_id":"%d"}`, base.ID))); err != nil {
		t.Fatal(err)
	}
	if _, err = s.store.PurchaseMembership(ctx, 101, vip.ID, "display-membership", "merchant:101"); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		mid               int
		domain            string
		approved, enabled bool
	}{{101, "z.example.invalid", true, true}, {101, "a.example.invalid", true, true}, {101, "unapproved.example.invalid", false, true}, {101, "disabled.example.invalid", true, false}, {102, "other-tenant.example.invalid", true, true}} {
		if _, err = s.Create(ctx, admin, "domains", input(fmt.Sprintf(`{"merchant_id":"%d","domain":"%s","approved":%t,"enabled":%t}`, row.mid, row.domain, row.approved, row.enabled))); err != nil {
			t.Fatal(err)
		}
	}
	assert := func(item Item, groupID int64, name string, active bool) {
		t.Helper()
		if item.Fields["effective_group_id"] != fmt.Sprint(groupID) || item.Fields["effective_group_name"] != name || item.Fields["group_membership_active"] != active {
			t.Error("effective group does not match current membership", item.Fields)
		}
		if _, err := time.Parse(time.RFC3339Nano, fmt.Sprint(item.Fields["membership_expires_at"])); err != nil {
			t.Error("original expiry missing", item.Fields["membership_expires_at"])
		}
		if !reflect.DeepEqual(item.Fields["approved_domains"], []any{"a.example.invalid", "z.example.invalid"}) {
			t.Error("domains unapproved, unsorted, or cross-tenant", item.Fields["approved_domains"])
		}
	}
	item, err := s.Get(ctx, admin, "merchants", 101)
	if err != nil {
		t.Fatal(err)
	}
	assert(item, vip.ID, "Purchased", true)
	page, err := s.List(ctx, admin, "merchants", Query{Page: 1, Limit: 20, Search: "Purchased"})
	if err != nil || page.Total != 1 {
		t.Fatal("effective group search", page, err)
	}
	assert(page.Items[0], vip.ID, "Purchased", true)
	if _, err = s.pool.Exec(ctx, `UPDATE memberships SET expires_at=now()-interval '1 second' WHERE merchant_id=101`); err != nil {
		t.Fatal(err)
	}
	item, err = s.Get(ctx, admin, "merchants", 101)
	if err != nil {
		t.Fatal(err)
	}
	assert(item, base.ID, "Baseline", false)
	if _, err = s.pool.Exec(ctx, `UPDATE merchants SET group_baseline_unresolved=true WHERE id=101`); err != nil {
		t.Fatal(err)
	}
	item, err = s.Get(ctx, admin, "merchants", 101)
	if err != nil {
		t.Fatal(err)
	}
	if item.Fields["effective_group_id"] != "" || item.Fields["effective_group_name"] != "" || item.Fields["group_membership_active"] != false {
		t.Fatal("unresolved baseline fabricated an effective group", item.Fields)
	}
	if _, err = s.pool.Exec(ctx, `UPDATE memberships SET expires_at=now()+interval '1 day' WHERE merchant_id=101`); err != nil {
		t.Fatal(err)
	}
	item, err = s.Get(ctx, admin, "merchants", 101)
	if err != nil {
		t.Fatal(err)
	}
	assert(item, vip.ID, "Purchased", true)
	if _, err = s.Update(ctx, admin, "merchants", 101, input(`{"name":"ordinary display edit"}`)); err != nil {
		t.Fatal(err)
	}
	var polluted bool
	if err = s.pool.QueryRow(ctx, `SELECT profile ?| ARRAY['effective_group_id','effective_group_name','membership_expires_at','group_membership_active','approved_domains'] FROM merchants WHERE id=101`).Scan(&polluted); err != nil || polluted {
		t.Fatal("derived group/domain data persisted", polluted, err)
	}
}

func TestMerchantDisplayNeverExportsOrSearchesArbitraryPrivateProfile(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `UPDATE merchants SET profile='{"email":"local@example.invalid","account":"synthetic-legacy-private-account","api_key":"synthetic-legacy-private-key","private_metadata":{"keep":"migration-source"},"compat_profile":"release-legacy"}' WHERE id=101`); err != nil {
		t.Fatal(err)
	}
	item, err := s.Get(ctx, admin, "merchants", 101)
	if err != nil {
		t.Fatal(err)
	}
	if item.Fields["email"] != "local@example.invalid" {
		t.Error("permitted contact missing", item.Fields)
	}
	for _, key := range []string{"account", "api_key", "private_metadata", "compat_profile"} {
		if _, exists := item.Fields[key]; exists {
			t.Error("arbitrary private profile was exported", key)
		}
	}
	page, err := s.List(ctx, admin, "merchants", Query{Page: 1, Limit: 20, Search: "synthetic-legacy-private"})
	if err != nil || page.Total != 0 {
		t.Error("private profile can be searched", page.Total, err)
	}
	if _, err = s.Update(ctx, admin, "merchants", 101, input(`{"name":"ordinary safe edit"}`)); err != nil {
		t.Fatal(err)
	}
	var preserved bool
	if err = s.pool.QueryRow(ctx, `SELECT profile->'private_metadata'->>'keep'='migration-source' AND profile->>'compat_profile'='release-legacy' FROM merchants WHERE id=101`).Scan(&preserved); err != nil || !preserved {
		t.Fatal("private migration metadata lost during ordinary edit", preserved, err)
	}
}

func TestMerchantDisplayRejectsNestedLegacyValuesInPublicProfileFields(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `UPDATE merchants SET profile='{"email":{"api_key":"nested-legacy-private"},"transfer_api_enabled":{"token":"nested-legacy-private"},"transfer_rate":{"password":"nested-legacy-private"}}' WHERE id=101`); err != nil {
		t.Fatal(err)
	}
	item, err := s.Get(ctx, admin, "merchants", 101)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"email", "group_id", "transfer_api_enabled", "transfer_rate"} {
		if _, exists := item.Fields[key]; exists {
			t.Error("invalid legacy field shape exported", key)
		}
	}
	page, err := s.List(ctx, admin, "merchants", Query{Page: 1, Limit: 20, Search: "nested-legacy-private"})
	if err != nil || page.Total != 0 {
		t.Fatal("nested private legacy values searchable", page.Total, err)
	}
}
