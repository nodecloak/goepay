package backoffice

import (
	"context"
	"strings"
	"testing"
)

// 公众号小程序 carries Pro's 类别 toolbar filter (全部类别 / 公众号 / 小程序)
// on the stored kind field. The schema advertises it and the generic list
// applies it; the value enum stays official_account / mini_program.
func TestWeixinAppsKindFilter(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	if _, e := s.Create(ctx, admin, "weixin-apps", input(`{"name":"mp-a","app_id":"wxoa001","kind":"official_account","enabled":true}`)); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Create(ctx, admin, "weixin-apps", input(`{"name":"mini-b","app_id":"wxma002","kind":"mini_program","enabled":true}`)); e != nil {
		t.Fatal(e)
	}

	sc, e := s.Schema(admin, "weixin-apps")
	if e != nil {
		t.Fatal(e)
	}
	if len(sc.Filters) != 1 || sc.Filters[0].Param != "kind" {
		t.Fatalf("weixin-apps filters wrong: %+v", sc.Filters)
	}
	labels := []string{}
	for _, o := range sc.Filters[0].Options {
		labels = append(labels, o.Label)
	}
	if strings.Join(labels, ",") != "全部类别,公众号,小程序" {
		t.Fatalf("类别 options = %v", labels)
	}

	// kind=official_account returns only the 公众号 record.
	oa, e := s.List(ctx, admin, "weixin-apps", Query{Filter: map[string]string{"kind": "official_account"}})
	if e != nil || oa.Total != 1 || oa.Items[0].Fields["app_id"] != "wxoa001" {
		t.Fatalf("kind=official_account page=%+v err=%v", oa, e)
	}
	// kind=mini_program returns only the 小程序 record.
	mini, e := s.List(ctx, admin, "weixin-apps", Query{Filter: map[string]string{"kind": "mini_program"}})
	if e != nil || mini.Total != 1 || mini.Items[0].Fields["app_id"] != "wxma002" {
		t.Fatalf("kind=mini_program page=%+v err=%v", mini, e)
	}
	// Empty filter returns both.
	all, e := s.List(ctx, admin, "weixin-apps", Query{Filter: map[string]string{"kind": ""}})
	if e != nil || all.Total != 2 {
		t.Fatalf("kind='' total=%d err=%v", all.Total, e)
	}
	// An undeclared value is rejected.
	if _, e := s.List(ctx, admin, "weixin-apps", Query{Filter: map[string]string{"kind": "nope"}}); e != ErrInvalid {
		t.Fatalf("unknown kind value err=%v want ErrInvalid", e)
	}
}
