package backoffice

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestMerchantBaselineProjectionReadOnlyAndNoProfilePollution(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	if _, e := s.pool.Exec(ctx, "UPDATE merchants SET group_baseline_unresolved=true WHERE id=101"); e != nil {
		t.Fatal(e)
	}
	item, e := s.Get(ctx, admin, "merchants", 101)
	if e != nil || item.Fields["base_group_id"] != "0" || item.Fields["group_baseline_unresolved"] != true {
		t.Fatalf("baseline hidden %+v %v", item, e)
	}
	schema, e := s.Schema(admin, "merchants")
	if e != nil {
		t.Fatal(e)
	}
	found := 0
	for _, f := range schema.Fields {
		if f.Name == "base_group_id" || f.Name == "group_baseline_unresolved" {
			found++
			if !f.ReadOnly {
				t.Fatal("baseline writable", f)
			}
		}
	}
	if found != 2 {
		t.Fatal("baseline schema missing")
	}
	if _, e = s.Update(ctx, admin, "merchants", 101, input(`{"base_group_id":"999"}`)); !errors.Is(e, ErrForbidden) {
		t.Fatalf("direct baseline input %v", e)
	}
	updated, e := s.Update(ctx, admin, "merchants", 101, input(`{"name":"new-name"}`))
	if e != nil || updated.Fields["group_baseline_unresolved"] != true {
		t.Fatalf("ordinary edit changed baseline %+v %v", updated, e)
	}
	var polluted bool
	if e = s.pool.QueryRow(ctx, "SELECT profile ?| ARRAY['base_group_id','group_baseline_unresolved'] FROM merchants WHERE id=101").Scan(&polluted); e != nil || polluted {
		t.Fatal("read-only metadata persisted in profile", polluted, e)
	}
	page, e := s.List(ctx, admin, "merchants", Query{Page: 1, Limit: 20})
	if e != nil {
		t.Fatal(e)
	}
	seen := false
	for _, it := range page.Items {
		if it.ID == 101 {
			seen = true
			if it.Fields["base_group_id"] != "0" || it.Fields["group_baseline_unresolved"] != true {
				t.Fatal("list baseline missing", it)
			}
		}
	}
	if !seen {
		t.Fatal("merchant list missing")
	}
	admins, e := s.Schema(admin, "admin-users")
	if e != nil {
		t.Fatal(e)
	}
	for _, f := range admins.Fields {
		if f.Name == "username" && !f.CreateOnly {
			t.Fatal("administrator username must be create-only")
		}
	}
}

func TestGroupAssignmentAuditDistinguishesMergedEditsAndExplicitConfirmation(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	group, e := s.Create(ctx, admin, "groups", input(`{"name":"baseline","rate_bps":"1000"}`))
	if e != nil {
		t.Fatal(e)
	}
	assignment := input(fmt.Sprintf(`{"group_id":"%d"}`, group.ID))
	if _, e = s.Update(ctx, admin, "merchants", 101, assignment); e != nil {
		t.Fatal(e)
	}
	var source string
	if e = s.pool.QueryRow(ctx, "SELECT changes->'group_assignment'->>'source' FROM backoffice_audit WHERE resource='merchants' AND resource_id=101 ORDER BY id DESC LIMIT 1").Scan(&source); e != nil || source != "changed" {
		t.Fatal("actual assignment marker missing", source, e)
	}
	if _, e = s.Update(ctx, admin, "merchants", 101, input(fmt.Sprintf(`{"name":"name-only","group_id":"%d"}`, group.ID))); e != nil {
		t.Fatal(e)
	}
	var marked bool
	if e = s.pool.QueryRow(ctx, "SELECT changes ? 'group_assignment' FROM backoffice_audit WHERE resource='merchants' AND resource_id=101 ORDER BY id DESC LIMIT 1").Scan(&marked); e != nil || marked {
		t.Fatal("merged name edit claimed group assignment", marked, e)
	}
	if _, e = s.pool.Exec(ctx, "UPDATE merchants SET group_baseline_unresolved=true WHERE id=101"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Update(ctx, admin, "merchants", 101, input(fmt.Sprintf(`{"group_id":"%d","confirm_group_assignment":true}`, group.ID))); e != nil {
		t.Fatal(e)
	}
	if e = s.pool.QueryRow(ctx, "SELECT changes->'group_assignment'->>'source' FROM backoffice_audit WHERE resource='merchants' AND resource_id=101 ORDER BY id DESC LIMIT 1").Scan(&source); e != nil || source != "confirmed" {
		t.Fatal("confirmation marker missing", source, e)
	}
	var unresolved, polluted bool
	if e = s.pool.QueryRow(ctx, "SELECT group_baseline_unresolved,profile ? 'confirm_group_assignment' FROM merchants WHERE id=101").Scan(&unresolved, &polluted); e != nil || unresolved || polluted {
		t.Fatal("confirmation not applied cleanly", unresolved, polluted, e)
	}
}
