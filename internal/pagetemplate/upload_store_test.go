package pagetemplate

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func samplePackageWith(t *testing.T, kind string, change func(files map[string][]byte)) []byte {
	t.Helper()
	raw, e := SamplePackage(kind)
	if e != nil {
		t.Fatal(e)
	}
	zr, e := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if e != nil {
		t.Fatal(e)
	}
	files := map[string][]byte{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		files[f.Name] = b
	}
	change(files)
	out, e := zipFiles(files)
	if e != nil {
		t.Fatal(e)
	}
	return out
}

func TestPostgresUploadedTemplateVersionsSelectionRemovalAndNotices(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	home, e := SamplePackage("home")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.UploadTemplate(ctx, "home", 0, home, Actor{Role: "admin", Class: "operator", ID: 2, Name: "op"}); !errors.Is(e, ErrForbidden) {
		t.Fatal("operator uploaded", e)
	}
	check, e := s.CheckUpload(ctx, "home", home, localAdmin)
	if e != nil || check.Name != "sample-home" || check.Exists || check.NextVersion != 1 || check.Identical || check.Files < 5 {
		t.Fatal("check before upload", check, e)
	}
	saved, e := s.UploadTemplate(ctx, "home", 0, home, localAdmin)
	if e != nil || saved.Version != 1 || saved.Revision != 1 || !saved.Created || saved.Selected {
		t.Fatal("upload", saved, e)
	}
	if _, e = s.UploadTemplate(ctx, "home", 0, home, localAdmin); !errors.Is(e, ErrConflict) {
		t.Fatal("stale revision accepted", e)
	}
	_, e = s.UploadTemplate(ctx, "home", 1, home, localAdmin)
	refusedWith(t, e, "完全相同")
	if check, e = s.CheckUpload(ctx, "home", home, localAdmin); e != nil || !check.Exists || check.NextVersion != 2 || !check.Identical {
		t.Fatal("check after upload", check, e)
	}
	list, e := s.List(ctx, "home", localAdmin)
	if e != nil || list.Revision != 1 || len(list.Items) != 18 {
		t.Fatal("list", list, e)
	}
	up := list.Items[17]
	if up.Name != "sample-home" || up.Source != "upload" || up.UploadVersion != 1 || up.UpdatedBy != localAdmin.Name || !strings.HasPrefix(up.PictureURL, "/template-files/") || !strings.HasSuffix(up.PictureURL, "/preview.png") || list.Items[0].Source != "builtin" {
		t.Fatal("uploaded list item", up)
	}
	key := strings.Split(up.PictureURL, "/")[2]
	if f, e := s.PublicFile(ctx, key, "js/main.js"); e != nil || !strings.HasPrefix(f.MIME, "text/javascript") || len(f.Content) == 0 {
		t.Fatal("public script", e)
	}
	for _, p := range []string{"index.html", "template.json", "README.md", "../index.html", "js/none.js"} {
		if _, e = s.PublicFile(ctx, key, p); !errors.Is(e, ErrNotFound) {
			t.Fatal("served", p, e)
		}
	}
	// The uploaded template takes typed configuration like a built-in one.
	if _, e = s.SaveConfig(ctx, "home", "sample-home", 1, configJSON(t, "home", func(m map[string]any) { m["brand_name"] = "上传品牌" }), localAdmin); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Select(ctx, "home", "sample-home", 2, localAdmin); e != nil {
		t.Fatal(e)
	}
	r, e := s.Resolve(ctx, "home")
	if e != nil || r.Fallback != "" || r.Upload == nil || r.Upload.Version != 1 || r.Config.BrandName != "上传品牌" {
		t.Fatal("resolve", r, e)
	}
	var page bytes.Buffer
	if e = RenderHome(&page, r, HomeData{SiteName: "站点"}); e != nil || !strings.Contains(page.String(), `<base href="/template-files/`+key+`/">`) {
		t.Fatal("render", e)
	}
	second := samplePackageWith(t, "home", func(files map[string][]byte) {
		files["index.html"] = bytes.Replace(files["index.html"], []byte("<main>"), []byte("<main data-version=\"2\">"), 1)
	})
	if saved, e = s.UploadTemplate(ctx, "home", 3, second, localAdmin); e != nil || saved.Version != 2 || saved.Created || !saved.Selected {
		t.Fatal("second version", saved, e)
	}
	if r, _ = s.Resolve(ctx, "home"); r.Upload == nil || r.Upload.Version != 2 {
		t.Fatal("the newest version is shown", r)
	}
	_, e = s.RestoreUpload(ctx, "home", "sample-home", 4, 2, localAdmin)
	refusedWith(t, e, "最新版本")
	if _, e = s.RestoreUpload(ctx, "home", "sample-home", 4, 9, localAdmin); !errors.Is(e, ErrNotFound) {
		t.Fatal("restore of a missing version", e)
	}
	restored, e := s.RestoreUpload(ctx, "home", "sample-home", 4, 1, localAdmin)
	if e != nil || restored.Version != 3 || restored.Revision != 5 {
		t.Fatal("restore", restored, e)
	}
	if r, _ = s.Resolve(ctx, "home"); r.Upload == nil || r.Upload.Version != 3 || r.Upload.Key == key {
		t.Fatal("restored version has its own key", r)
	}
	versions, e := s.UploadVersions(ctx, "home", "sample-home", localAdmin)
	if e != nil || len(versions) != 3 || versions[0].Operation != "restore" || versions[0].RestoredFrom != 1 || versions[1].Operation != "upload" || versions[0].Notice != "pending" || versions[2].Actor != localAdmin.Name {
		t.Fatal("versions", versions, e)
	}
	zipped, e := s.UploadPackage(ctx, "home", "sample-home", 1, localAdmin)
	if e != nil {
		t.Fatal(e)
	}
	if back, e := ParsePackage("home", zipped); e != nil || back.Manifest.Name != "sample-home" {
		t.Fatal("downloaded package does not upload again", e)
	}
	_, e = s.RemoveUpload(ctx, "home", "sample-home", 5, localAdmin)
	refusedWith(t, e, "正在使用")
	if _, e = s.Select(ctx, "home", "default", 5, localAdmin); e != nil {
		t.Fatal(e)
	}
	if revision, e := s.RemoveUpload(ctx, "home", "sample-home", 6, localAdmin); e != nil || revision != 7 {
		t.Fatal("remove", revision, e)
	}
	if list, _ = s.List(ctx, "home", localAdmin); len(list.Items) != 17 {
		t.Fatal("removed template still listed", len(list.Items))
	}
	if _, e = s.PublicFile(ctx, key, "js/main.js"); !errors.Is(e, ErrNotFound) {
		t.Fatal("files of a removed template stay public", e)
	}
	if _, e = s.Config(ctx, "home", "sample-home", localAdmin); !errors.Is(e, ErrNotFound) {
		t.Fatal("config of a removed template", e)
	}
	if _, e = s.Select(ctx, "home", "sample-home", 7, localAdmin); !errors.Is(e, ErrNotFound) {
		t.Fatal("removed template selected", e)
	}
	// Uploading the name again brings it back as the next version.
	if saved, e = s.UploadTemplate(ctx, "home", 7, home, localAdmin); e != nil || saved.Version != 4 || saved.Created {
		t.Fatal("revive", saved, e)
	}
	// Pay templates frame the checkout through Open.
	pay, _ := SamplePackage("pay")
	if _, e = s.UploadTemplate(ctx, "pay", 0, pay, localAdmin); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Select(ctx, "pay", "sample-pay", 1, localAdmin); e != nil {
		t.Fatal(e)
	}
	if r, e = s.Resolve(ctx, "pay"); e != nil || r.Upload == nil {
		t.Fatal("pay resolve", r, e)
	}
	var checkout bytes.Buffer
	tail, e := Open(&checkout, r, Page{Title: "支付收银台", TradeNo: "T1", Amount: "2.00"})
	if e != nil || !strings.Contains(checkout.String(), `<main class="goepay-checkout goepay-checkout-pay">`) || !strings.HasSuffix(tail, "</body></html>") || len(tail) < 40 {
		t.Fatal("pay open", e, tail)
	}
	// One event and one notice job each: three uploads, a select, a restore
	// and a removal on home (selecting a built-in has none); upload and
	// select on pay.
	var events, jobs, matched int
	if e = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM page_template_upload_events),(SELECT count(*) FROM jobs WHERE kind=$1),(SELECT count(*) FROM jobs j JOIN page_template_upload_events v ON j.event_key='page-template-event:'||v.id::text AND j.payload=jsonb_build_object('event_id',v.id::text) WHERE j.kind=$1 AND j.state='pending')`, UploadNoticeKind).Scan(&events, &jobs, &matched); e != nil || events != 8 || jobs != 8 || matched != 8 {
		t.Fatal("events and notice jobs", events, jobs, matched, e)
	}
	var audits int
	if e = s.pool.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE resource='page-templates' AND operation IN('upload-template','restore-template','remove-template')`).Scan(&audits); e != nil || audits != 6 {
		t.Fatal("audit", audits, e)
	}
	// Versions, files and contents never change.
	for _, q := range []string{`UPDATE page_template_upload_versions SET title='x'`, `DELETE FROM page_template_upload_files`, `UPDATE page_template_blobs SET content='x'`, `DELETE FROM page_template_upload_events`} {
		if _, e = s.pool.Exec(ctx, q); e == nil {
			t.Fatal("history changed:", q)
		}
	}
}
