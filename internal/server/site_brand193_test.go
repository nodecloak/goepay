package server

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSiteBrand193HomeUsesOriginalHomeLogoSeparatelyFromGlobalIcon(t *testing.T) {
	f := channel168Local(t)
	state := channel168Data(t, brand193Read(f))
	state = channel168Data(t, brand193Upload(f, "logo", state["fingerprint"].(string), brand193PNG(t, color.NRGBA{R: 100, A: 255})))
	channel168Data(t, brand193Upload(f, "icon", state["fingerprint"].(string), brand193PNG(t, color.NRGBA{G: 100, A: 255})))
	w := brand193Public(f, "/")
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`src="/static/img/logo.png"`)) || bytes.Contains(w.Body.Bytes(), []byte(`src="/static/img/icon.png"`)) {
		t.Fatal("home did not use original home logo", w.Code, w.Body.String())
	}
}

func TestSiteBrand193RejectsUnsafeImagesPreservesGIFAndDetectsSourceCorruption(t *testing.T) {
	f := channel168Local(t)
	first := channel168Data(t, brand193Read(f))
	fp := first["fingerprint"].(string)
	for _, raw := range [][]byte{[]byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"></svg>`), []byte(`<?php echo 'not image';`), bytes.Repeat([]byte("x"), 2<<20)} {
		w := brand193Upload(f, "logo", fp, raw)
		if w.Code != 422 && w.Code != 413 {
			t.Fatal("unsafe image accepted", w.Code)
		}
	}
	canvas := image.NewPaletted(image.Rect(0, 0, 16, 12), color.Palette{color.White, color.Black})
	canvas.SetColorIndex(1, 1, 1)
	var animation bytes.Buffer
	if e := gif.EncodeAll(&animation, &gif.GIF{Image: []*image.Paletted{canvas, canvas}, Delay: []int{1, 1}}); e != nil {
		t.Fatal(e)
	}
	w := brand193Upload(f, "icon", fp, animation.Bytes())
	if w.Code != 200 {
		t.Fatal("original GIF upload rejected", w.Code)
	}
	saved := channel168Data(t, w)
	if public := brand193Public(f, "/static/img/icon.png"); public.Code != 200 || public.Header().Get("Content-Type") != "image/png" {
		t.Fatal("GIF not normalized to first-frame PNG", public.Code)
	}
	if _, e := f.p.Exec(t.Context(), `UPDATE site_brand_head193 SET revision=0`); e != nil {
		t.Fatal(e)
	}
	if w = brand193Read(f); w.Code != 409 && w.Code != 503 {
		t.Fatal("old valid empty head restored defaults", w.Code)
	}
	if w = brand193Public(f, "/static/img/icon.png"); w.Code != 503 {
		t.Fatal("public source corruption served asset", w.Code, saved["revision"])
	}
}

func TestSiteBrand193FinalSessionExpiryRollsBackAssetRevisionAndAudit(t *testing.T) {
	f := channel168Local(t)
	state := channel168Data(t, brand193Read(f))
	if _, e := f.p.Exec(t.Context(), `CREATE SEQUENCE brand193_final_probe;CREATE FUNCTION brand193_delay()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN PERFORM nextval('brand193_final_probe');PERFORM pg_sleep(3);RETURN NEW;END$$;CREATE TRIGGER brand193_delay_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW WHEN(NEW.resource='site-branding193')EXECUTE FUNCTION brand193_delay();UPDATE console_sessions SET expires_at=clock_timestamp()+interval '2 seconds' WHERE role='admin'`); e != nil {
		t.Fatal(e)
	}
	w := brand193Upload(f, "logo", state["fingerprint"].(string), brand193PNG(t, color.White))
	if w.Code != 403 {
		t.Fatal("expired final session committed image", w.Code, w.Body.String())
	}
	var entered bool
	var effects int
	if e := f.p.QueryRow(t.Context(), `SELECT is_called FROM brand193_final_probe`).Scan(&entered); e != nil || !entered {
		t.Fatal("did not reach final audit wait", entered, e)
	}
	if e := f.p.QueryRow(t.Context(), `SELECT(SELECT count(*)FROM site_brand_assets193)+(SELECT count(*)FROM site_brand_revisions193)+(SELECT count(*)FROM backoffice_audit WHERE resource='site-branding193')+(SELECT revision::int FROM site_brand_head193)`).Scan(&effects); e != nil || effects != 0 {
		t.Fatal("expired logo upload committed effects", effects, e)
	}
}

func TestSiteBrand193RejectsExtraPartsAndUnownedAuthorityWithoutPublishing(t *testing.T) {
	f := channel168Local(t)
	state := channel168Data(t, brand193Read(f))
	raw := brand193PNG(t, color.White)
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, _ := form.CreateFormFile("file", "logo.png")
	file.Write(raw)
	form.WriteField("path", "../../config.php")
	form.Close()
	request := func(csrf string, query bool) *httptest.ResponseRecorder {
		target := "/console/v1/admin/settings/branding/logo"
		if query {
			target += "?"
		}
		r := httptest.NewRequest("POST", target, bytes.NewReader(body.Bytes()))
		r.Header.Set("Content-Type", form.FormDataContentType())
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("X-Expected-Fingerprint", state["fingerprint"].(string))
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		return w
	}
	if w := request(f.csrf, false); w.Code != 422 {
		t.Fatal("extra arbitrary path form accepted", w.Code)
	}
	if w := request("wrong-csrf", false); w.Code != 403 {
		t.Fatal("wrong CSRF accepted", w.Code)
	}
	if w := request(f.csrf, true); w.Code != 400 {
		t.Fatal("empty query accepted", w.Code)
	}
	if _, e := f.p.Exec(t.Context(), `UPDATE console_users SET admin_class='operator' WHERE username='admin168'`); e != nil {
		t.Fatal(e)
	}
	if w := brand193Upload(f, "logo", state["fingerprint"].(string), raw); w.Code == 200 {
		t.Fatal("current operator published brand")
	}
	var effects int
	if e := f.p.QueryRow(t.Context(), `SELECT(SELECT count(*)FROM site_brand_assets193)+(SELECT count(*)FROM site_brand_revisions193)+(SELECT count(*)FROM backoffice_audit WHERE resource='site-branding193')`).Scan(&effects); e != nil || effects != 0 {
		t.Fatal("rejected image authority changed state", effects, e)
	}
}

func TestSiteBrand193CurrentImageTamperingAndHistoricalGuessedURLCannotServeBytes(t *testing.T) {
	f := channel168Local(t)
	state := channel168Data(t, brand193Read(f))
	state = channel168Data(t, brand193Upload(f, "logo", state["fingerprint"].(string), brand193PNG(t, color.White)))
	old := state["logo"].(map[string]any)["id"].(string)
	state = channel168Data(t, brand193Upload(f, "logo", state["fingerprint"].(string), brand193PNG(t, color.Black)))
	for _, path := range []string{"/static/img/" + old + ".png", "/template-assets/" + old, "/static/img/logo.png?id=" + old} {
		w := brand193Public(f, path)
		if w.Code == 200 {
			t.Fatal("historical unselected logo exposed", path)
		}
	}
	current := state["logo"].(map[string]any)["id"].(string)
	tx, e := f.p.Begin(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(t.Context())
	if _, e = tx.Exec(t.Context(), `ALTER TABLE site_brand_assets193 DISABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(t.Context(), `UPDATE site_brand_assets193 SET content=$2 WHERE id=$1`, current, []byte("not a normalized image")); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(t.Context(), `ALTER TABLE site_brand_assets193 ENABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(t.Context()); e != nil {
		t.Fatal(e)
	}
	if w := brand193Public(f, "/static/img/logo.png"); w.Code != 503 || bytes.Contains(w.Body.Bytes(), []byte("not a normalized image")) {
		t.Fatal("tampered image served", w.Code)
	}
}

func brand193Read(f channel168Fixture) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "/console/v1/admin/settings/branding", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}
func brand193Upload(f channel168Fixture, slot, expected string, data []byte) *httptest.ResponseRecorder {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, _ := form.CreateFormFile("file", "untrusted-filename.png")
	part.Write(data)
	form.Close()
	r := httptest.NewRequest("POST", "/console/v1/admin/settings/branding/"+slot, &body)
	r.Header.Set("Content-Type", form.FormDataContentType())
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.Header.Set("X-Expected-Fingerprint", expected)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}
func brand193PNG(t *testing.T, c color.Color) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 16, 12))
	for y := 0; y < 12; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if e := png.Encode(&b, img); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func brand193Public(f channel168Fixture, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	return w
}

func TestSiteBrand193OriginalSeparateUploadsPublishOnlyCurrentImageWithCAS(t *testing.T) {
	f := channel168Local(t)
	w := brand193Read(f)
	if w.Code != 200 {
		t.Fatal("original brand settings missing", w.Code)
	}
	first := channel168Data(t, w)
	if first["logo"] != nil || first["icon"] != nil {
		t.Fatal("unconfigured brand invented", first)
	}
	if w = brand193Public(f, "/static/img/logo.png"); w.Code != 404 {
		t.Fatal("unconfigured public logo invented", w.Code)
	}
	raw := brand193PNG(t, color.NRGBA{R: 35, G: 120, B: 210, A: 255})
	raw = append(raw, []byte("<?php never execute ?>")...)
	w = brand193Upload(f, "logo", first["fingerprint"].(string), raw)
	if w.Code != 200 {
		t.Fatal("original home logo upload missing", w.Code, w.Body.String())
	}
	saved := channel168Data(t, w)
	logo := saved["logo"].(map[string]any)
	if saved["icon"] != nil || logo["width"] != float64(16) || logo["height"] != float64(12) || saved["fingerprint"] == first["fingerprint"] {
		t.Fatal("logo/icon state mixed", saved)
	}
	published := brand193Public(f, "/static/img/logo.png")
	if published.Code != 200 || published.Header().Get("Content-Type") != "image/png" || published.Header().Get("Cache-Control") != "no-store" || published.Header().Get("X-Content-Type-Options") != "nosniff" || bytes.Contains(published.Body.Bytes(), []byte("<?php")) {
		t.Fatal("published original executable bytes or wrong headers", published.Code)
	}
	if w = brand193Upload(f, "icon", first["fingerprint"].(string), raw); w.Code != 409 {
		t.Fatal("stale logo revision accepted", w.Code)
	}
	icon := brand193PNG(t, color.NRGBA{R: 180, G: 30, B: 25, A: 255})
	current := channel168Data(t, brand193Upload(f, "icon", saved["fingerprint"].(string), icon))
	if current["logo"].(map[string]any)["id"] != logo["id"] || current["icon"].(map[string]any)["id"] == logo["id"] {
		t.Fatal("independent original image overwritten", current)
	}
	if w = brand193Public(f, "/static/img/icon.png"); w.Code != 200 || bytes.Equal(w.Body.Bytes(), published.Body.Bytes()) {
		t.Fatal("global image not separately served", w.Code)
	}
	for _, path := range []string{"/static/img/logo.png?", "/static/img/logo.png?path=../secret"} {
		if w = brand193Public(f, path); w.Code != 400 {
			t.Fatal("public logo accepted query", path, w.Code)
		}
	}
	var effects int
	if e := f.p.QueryRow(t.Context(), `SELECT(SELECT count(*)FROM orders)+(SELECT count(*)FROM provider_attempts)+(SELECT count(*)FROM journals)+(SELECT count(*)FROM jobs)`).Scan(&effects); e != nil || effects != 0 {
		t.Fatal("logo touched financial state", effects, e)
	}
}
