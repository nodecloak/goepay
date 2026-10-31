package pagetemplate

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestPostgresCustomHTMLVersionsRestoreAuditAndNoticeJobs(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	empty, e := s.CustomCurrent(ctx, localAdmin)
	if e != nil || empty.Version != 0 || empty.Revision != 0 || !empty.Frame || empty.HTML != "" {
		t.Fatal("no version yet", empty, e)
	}
	var ce *CustomError
	if _, e = s.Select(ctx, "home", "custom", 0, localAdmin); !errors.As(e, &ce) {
		t.Fatal("custom selected without a saved version", e)
	}
	if _, e = s.SaveCustom(ctx, 5, CustomInput{HTML: "<p>x</p>", Frame: true}, localAdmin); !errors.Is(e, ErrConflict) {
		t.Fatal("stale revision accepted", e)
	}
	if _, e = s.SaveCustom(ctx, 0, CustomInput{HTML: "<p>x</p>", CSS: "p{background:url(x)}"}, localAdmin); !errors.As(e, &ce) {
		t.Fatal("unsafe CSS stored", e)
	}
	if _, e = s.SaveCustom(ctx, 0, CustomInput{HTML: `<img src="/template-assets/` + testAsset + `">`}, localAdmin); !errors.As(e, &ce) || !strings.Contains(ce.Reason, "不存在") {
		t.Fatal("missing template image accepted", e)
	}
	other := Actor{Role: "admin", Class: "operator", ID: 2, Name: "operator"}
	if _, e = s.SaveCustom(ctx, 0, CustomInput{HTML: "<p>x</p>"}, other); !errors.Is(e, ErrForbidden) {
		t.Fatal("non-super administrator saved custom HTML", e)
	}
	if _, e = s.CustomVersions(ctx, other); !errors.Is(e, ErrForbidden) {
		t.Fatal("non-super administrator read versions", e)
	}
	first, e := s.SaveCustom(ctx, 0, CustomInput{HTML: `<h1 onclick="x()">一</h1><script>x()</script>`, CSS: "h1{color:red}", Frame: true}, localAdmin)
	if e != nil || first.Version != 1 || first.Revision != 1 || first.Removed["<script>"] != 1 || first.Removed["onclick"] != 1 {
		t.Fatal("first version", first, e)
	}
	second, e := s.SaveCustom(ctx, 1, CustomInput{HTML: "<p>二</p>", Frame: false}, localAdmin)
	if e != nil || second.Version != 2 || second.Revision != 2 || len(second.Removed) != 0 {
		t.Fatal("second version", second, e)
	}
	restored, e := s.RestoreCustom(ctx, 2, 1, localAdmin)
	if e != nil || restored.Version != 3 || restored.Revision != 3 {
		t.Fatal("restore", restored, e)
	}
	if _, e = s.RestoreCustom(ctx, 3, 9, localAdmin); !errors.Is(e, ErrNotFound) {
		t.Fatal("restored a missing version", e)
	}
	current, e := s.CustomCurrent(ctx, localAdmin)
	if e != nil || current.Version != 3 || current.RestoredFrom != 1 || current.HTML != `<h1 onclick="x()">一</h1><script>x()</script>` || current.CSS != "h1{color:red}" || !current.Frame || current.Actor != localAdmin.Name || current.CreatedAt == "" || current.Revision != 3 {
		t.Fatal("the editor reads the administrator's source of the newest version", current, e)
	}
	v2, e := s.CustomVersion(ctx, 2, localAdmin)
	if e != nil || v2.HTML != "<p>二</p>" || v2.Frame {
		t.Fatal("version 2", v2, e)
	}
	list, e := s.CustomVersions(ctx, localAdmin)
	if e != nil || len(list) != 3 || list[0].Version != 3 || list[0].RestoredFrom != 1 || list[2].Version != 1 || list[0].Notice != "pending" || list[1].HTMLBytes != len("<p>二</p>") {
		t.Fatal("versions", list, e)
	}
	var jobs, audits int
	if e = s.pool.QueryRow(ctx, `SELECT count(*) FROM jobs j JOIN page_template_custom_versions v ON j.event_key='page-template-custom:'||v.id::text AND j.payload=jsonb_build_object('version_id',v.id::text) WHERE j.kind=$1 AND j.state='pending'`, CustomNoticeKind).Scan(&jobs); e != nil || jobs != 3 {
		t.Fatal("one notice job per version", jobs, e)
	}
	if e = s.pool.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE resource='page-templates' AND operation IN('save-html','restore-html') AND changes->>'name'='custom'`).Scan(&audits); e != nil || audits != 3 {
		t.Fatal("one audit row per version", audits, e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE page_template_custom_versions SET html='<script>x()</script>' WHERE version=1`); e == nil {
		t.Fatal("saved versions must be immutable")
	}
	if _, e = s.pool.Exec(ctx, `DELETE FROM page_template_custom_versions WHERE version=1`); e == nil {
		t.Fatal("saved versions must not be deleted")
	}
}

func TestPostgresSelectedCustomHomePublishesOnlyItsImagesAndFallsBackWhenUnusable(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	used, e := s.Upload(ctx, imageBytes(t, "png"), localAdmin)
	if e != nil {
		t.Fatal(e)
	}
	unused, e := s.Upload(ctx, imageBytes(t, "jpeg"), localAdmin)
	if e != nil || unused.ID == used.ID {
		t.Fatal(e)
	}
	if _, e = s.SaveCustom(ctx, 0, CustomInput{HTML: `<p>图</p><img src="/template-assets/` + used.ID + `" alt="">`, Frame: true}, localAdmin); e != nil {
		t.Fatal(e)
	}
	if _, e = s.PublishedAsset(ctx, used.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("image public before the custom template is selected", e)
	}
	if _, e = s.Select(ctx, "home", "custom", 1, localAdmin); e != nil {
		t.Fatal(e)
	}
	r, e := s.Resolve(ctx, "home")
	if e != nil || r.Name != "custom" || r.Fallback != "" || r.Custom == nil || r.Custom.Version != 1 || len(r.Custom.Assets) != 1 {
		t.Fatal("selected custom page", r, e)
	}
	var b strings.Builder
	if e = RenderHome(&b, r, HomeData{SiteName: "站点"}); e != nil || !strings.Contains(b.String(), `<img src="/template-assets/`+used.ID+`" alt="">`) {
		t.Fatal("public page", e)
	}
	if got, e := s.PublishedAsset(ctx, used.ID); e != nil || got.ID != used.ID {
		t.Fatal("image of the selected custom page not public", e)
	}
	if _, e = s.PublishedAsset(ctx, unused.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("unreferenced image public", e)
	}
	// A version that no longer passes the checks (written around the store)
	// makes the public page fall back to the default template.
	if _, e = s.pool.Exec(ctx, `INSERT INTO page_template_custom_versions(kind,version,html,css,frame,digest,actor_id,actor_name)VALUES('home',2,'<p>x</p>','p{background:url(x)}',true,repeat('0',64),1,'x')`); e != nil {
		t.Fatal(e)
	}
	if r, e = s.Resolve(ctx, "home"); e != nil || r.Name != "default" || r.Fallback != "invalid_custom_html" {
		t.Fatal("unusable custom page did not fall back", r, e)
	}
	if _, e = s.PublishedAsset(ctx, used.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("image stays public after the fallback", e)
	}
}
