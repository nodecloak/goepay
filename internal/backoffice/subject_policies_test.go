package backoffice

import (
	"context"
	"errors"
	"testing"
)

func TestSubjectPoliciesSuperOnlyCRUDAndStrictScope(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	super := Principal{Role: "admin", SubjectID: 1, Username: "super", AdminClass: "super"}
	good := input(`{"scope":"merchant","scope_id":"101","template":"title-[name]","rules":"[]","enabled":true}`)
	for _, p := range []Principal{{Role: "admin", SubjectID: 1, AdminClass: "operator"}, {Role: "admin", SubjectID: 1}, {Role: "merchant", SubjectID: 101}} {
		if _, e := s.Schema(p, "order-subject-policies"); !errors.Is(e, ErrForbidden) {
			t.Fatalf("schema exposed %v", e)
		}
		if _, e := s.Create(ctx, p, "order-subject-policies", good); !errors.Is(e, ErrForbidden) {
			t.Fatalf("write exposed %v", e)
		}
		if _, e := s.List(ctx, p, "order-subject-policies", Query{}); !errors.Is(e, ErrForbidden) {
			t.Fatalf("list exposed %v", e)
		}
	}
	if _, e := s.Schema(super, "order-subject-policies"); e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{`{"scope":"global","scope_id":"1","enabled":true}`, `{"scope":"merchant","scope_id":"9999","enabled":false}`, `{"scope":"channel","scope_id":"07","enabled":true}`, `{"scope":"global","scope_id":"0","rules":"[{\"name\":\"x\",\"min\":1,\"max\":\"2\"}]","enabled":true}`} {
		if _, e := s.Create(ctx, super, "order-subject-policies", input(raw)); e == nil {
			t.Fatal("saved invalid policy", raw)
		}
	}
	item, e := s.Create(ctx, super, "order-subject-policies", good)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Create(ctx, super, "order-subject-policies", good); !errors.Is(e, ErrConflict) {
		t.Fatalf("duplicate active %v", e)
	}
	if _, e = s.Update(ctx, super, "order-subject-policies", item.ID, input(`{"template":"next"}`)); e != nil {
		t.Fatal(e)
	}
	if e = s.Delete(ctx, super, "order-subject-policies", item.ID); e != nil {
		t.Fatal(e)
	}
	var n int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM backoffice_audit WHERE resource='order-subject-policies'").Scan(&n); e != nil || n != 3 {
		t.Fatal(n, e)
	}
}
