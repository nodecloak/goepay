package backoffice

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"
)

func TestOriginalDefaultGroupIsRealAndCannotBeDeleted(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	g, e := s.Get(ctx, admin, "groups", 0)
	if e != nil || g.ID != 0 || g.Fields["name"] != "默认用户组" {
		t.Fatal("default group absent from real directory", g, e)
	}
	if _, e = s.Update(ctx, admin, "groups", 0, input(`{"name":"默认商户组","info":{"alipay":{"channel":"0","type":"","rate":"0"}}}`)); e != nil {
		t.Fatal("default group cannot be edited", e)
	}
	if e = s.Delete(ctx, admin, "groups", 0); !errors.Is(e, ErrForbidden) {
		t.Fatal("built-in group could be deleted", e)
	}
	for _, query := range []string{`DELETE FROM backoffice_records WHERE id=0`, `UPDATE backoffice_records SET id=99999 WHERE id=0`, `UPDATE backoffice_records SET resource='channels' WHERE id=0`} {
		if _, e = s.pool.Exec(ctx, query); e == nil {
			t.Fatal("database did not protect default identity", query)
		}
	}
	g, e = s.Get(ctx, admin, "groups", 0)
	if e != nil || g.Fields["name"] != "默认商户组" {
		t.Fatal("default edit lost", g, e)
	}
}

func TestOriginalDefaultGroupCanBeExplicitlyAssignedAndResolvesOnlyTargetBaseline(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	g, e := s.Create(ctx, admin, "groups", input(`{"name":"explicit positive group154"}`))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Update(ctx, admin, "merchants", 101, input(fmt.Sprintf(`{"group_id":"%d","confirm_group_assignment":true}`, g.ID))); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE merchants SET group_baseline_unresolved=true WHERE id=101`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Update(ctx, admin, "merchants", 101, input(`{"group_id":"0","confirm_group_assignment":true}`)); e != nil {
		t.Fatal("canonical original zero assignment refused", e)
	}
	var base, other int64
	var unresolved bool
	if e = s.pool.QueryRow(ctx, `SELECT base_group_id,group_baseline_unresolved,(SELECT base_group_id FROM merchants WHERE id=102) FROM merchants WHERE id=101`).Scan(&base, &unresolved, &other); e != nil || base != 0 || unresolved || other != 0 {
		t.Fatal("explicit default did not resolve only target", base, unresolved, other, e)
	}
}

func TestOriginalDefaultGroupHTTPZeroIsResourceSpecificAndReadOnlyForMerchants(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	if _, e := s.pool.Exec(ctx, `INSERT INTO backoffice_records(id,resource,body) VALUES(0,'groups','{"name":"默认用户组","enabled":true,"info":{}}') ON CONFLICT(id) DO NOTHING`); e != nil {
		t.Fatal(e)
	}
	w := httptest.NewRecorder()
	s.Serve(w, httptest.NewRequest("GET", "/groups/0", nil), admin, []string{"groups", "0"})
	if w.Code != 200 {
		t.Fatal("original group 0 HTTP refused", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	s.Serve(w, httptest.NewRequest("GET", "/channels/0", nil), admin, []string{"channels", "0"})
	if w.Code == 200 {
		t.Fatal("zero expanded other resource identity")
	}
	merchant := Principal{Role: "merchant", SubjectID: 101, Username: "m1"}
	if _, e := s.Get(ctx, merchant, "groups", 0); e != nil {
		t.Fatal("merchant could not read same public group settings as positive groups", e)
	}
	if _, e := s.Update(ctx, merchant, "groups", 0, input(`{"name":"forbidden"}`)); !errors.Is(e, ErrForbidden) {
		t.Fatal("merchant edited protected group", e)
	}
}

func TestOriginalDefaultGroupGuardPreservesOrdinaryResourceDeletion(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	g, e := s.Create(ctx, admin, "groups", input(`{"name":"ordinary removable group"}`))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Delete(ctx, admin, "groups", g.ID); e != nil {
		t.Fatal("default guard broke ordinary deletion", e)
	}
	if _, e = s.Get(ctx, admin, "groups", g.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("ordinary deletion was silently skipped", e)
	}
}
