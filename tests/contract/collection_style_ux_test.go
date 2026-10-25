package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCollectionStyleUXOriginalFieldsRevisionAndLinkLayout(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	token, csrf, e := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, "/console/v1/admin"+path, bytes.NewReader(raw))
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	body := map[string]any{"name": "original_style", "label": "原字段样式", "enabled": true, "sort": "2", "url": "https://images.example.test/poster.png", "img_width": "900", "img_height": "1200", "qr_width": "380", "qr_height": "380", "qr_left": "245", "qr_top": "326", "foreground": "#555555", "background": "transparent", "font": "60px '黑体'", "font_color": "#FFFFFF", "rec_name_left": "", "rec_name_top": "210", "word": "2"}
	w := request("POST", "/collection-styles", body)
	if w.Code != 201 {
		t.Fatalf("original style fields %d %s", w.Code, w.Body.String())
	}
	var created struct {
		Data struct {
			ID, Revision string
			Style        map[string]any
		}
	}
	if e = json.Unmarshal(w.Body.Bytes(), &created); e != nil || created.Data.ID == "" || len(created.Data.Revision) != 64 {
		t.Fatalf("created style %s %v", w.Body.String(), e)
	}
	var codeID string
	if e = pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,owner_id,body) VALUES('collection-codes',1,jsonb_build_object('name','Poster code','title','Collect','style_id',$1::text,'enabled',true)) RETURNING id::text`, created.Data.ID).Scan(&codeID); e != nil {
		t.Fatal(e)
	}
	body["qr_left"] = "899"
	if w = request("PUT", "/collection-styles/"+created.Data.ID, map[string]any{"expected_revision": created.Data.Revision, "style": body}); w.Code != 422 {
		t.Fatalf("QR outside canvas %d %s", w.Code, w.Body.String())
	}
	body["qr_left"] = "200"
	w = request("PUT", "/collection-styles/"+created.Data.ID, map[string]any{"expected_revision": created.Data.Revision, "style": body})
	if w.Code != 200 {
		t.Fatalf("versioned update %d %s", w.Code, w.Body.String())
	}
	if w = request("PUT", "/collection-styles/"+created.Data.ID, map[string]any{"expected_revision": created.Data.Revision, "style": body}); w.Code != 409 {
		t.Fatalf("stale style update %d", w.Code)
	}
	if w = request("PATCH", "/resources/collection-styles/"+created.Data.ID, map[string]any{"label": "bypass revision"}); w.Code != 409 {
		t.Fatalf("generic version bypass %d %s", w.Code, w.Body.String())
	}
	if w = request("GET", "/collection-styles", nil); w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"qr_left":200`)) {
		t.Fatalf("list omitted consumed geometry %d %s", w.Code, w.Body.String())
	}
	s.CheckoutKey = bytes.Repeat([]byte{7}, 32)
	s.ProviderFlow = &payment.ProviderFlow{CallbackBaseURL: "http://127.0.0.1:8180"}
	w = request("GET", "/collection-codes/"+codeID+"/link", nil)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"layout":`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"qr_left":200`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"merchant_name":`)) {
		t.Fatalf("link ignored original style %d %s", w.Code, w.Body.String())
	}
}

func TestCollectionStyleUXUploadedAssetPublication(t *testing.T) {
	s, _, _ := fixture(t)
	token, csrf, e := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	var picture bytes.Buffer
	if e = png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 900, 1200))); e != nil {
		t.Fatal(e)
	}
	var payload bytes.Buffer
	writer := multipart.NewWriter(&payload)
	part, e := writer.CreateFormFile("file", "background.png")
	if e != nil {
		t.Fatal(e)
	}
	part.Write(picture.Bytes())
	writer.Close()
	r := httptest.NewRequest("POST", "/console/v1/admin/collection-style-images", bytes.NewReader(payload.Bytes()))
	r.Header.Set("Content-Type", writer.FormDataContentType())
	r.Header.Set("X-CSRF-Token", csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 201 {
		t.Fatalf("upload original 900x1200 background %d %s", w.Code, w.Body.String())
	}
	var asset struct {
		Data struct {
			ID            string
			Width, Height int
		}
	}
	if e = json.Unmarshal(w.Body.Bytes(), &asset); e != nil || len(asset.Data.ID) != 64 || asset.Data.Width != 900 || asset.Data.Height != 1200 {
		t.Fatalf("normalized image metadata %s %v", w.Body.String(), e)
	}
	assetURL := "/collection-style-assets/" + asset.Data.ID
	request := func(method, path string, body any, authenticated bool) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		if authenticated {
			r.Header.Set("X-CSRF-Token", csrf)
			r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	if w = request("GET", assetURL, nil, false); w.Code != 404 {
		t.Fatalf("private upload published %d", w.Code)
	}
	if w = request("GET", "/console/v1/admin/collection-style-images/"+asset.Data.ID, nil, true); w.Code != 200 || w.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("private preview %d", w.Code)
	}
	body := map[string]any{"name": "uploaded_style", "label": "上传背景", "enabled": true, "sort": "0", "url": assetURL, "img_width": "900", "img_height": "1200", "qr_width": "380", "qr_height": "380", "qr_left": "245", "qr_top": "326", "foreground": "#555555", "background": "#FFFFFF", "font": "60px '黑体'", "font_color": "#FFFFFF", "rec_name_left": "", "rec_name_top": "210", "word": "2"}
	w = request("POST", "/console/v1/admin/collection-styles", body, true)
	if w.Code != 201 {
		t.Fatalf("publish reference %d %s", w.Code, w.Body.String())
	}
	var style struct{ Data struct{ ID, Revision string } }
	json.Unmarshal(w.Body.Bytes(), &style)
	if w = request("GET", assetURL, nil, false); w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("active reference unavailable %d", w.Code)
	}
	if w = request("HEAD", assetURL, nil, false); w.Code != 200 || w.Body.Len() != 0 {
		t.Fatalf("asset HEAD %d len=%d", w.Code, w.Body.Len())
	}
	body["enabled"] = false
	if w = request("PUT", "/console/v1/admin/collection-styles/"+style.Data.ID, map[string]any{"expected_revision": style.Data.Revision, "style": body}, true); w.Code != 200 {
		t.Fatalf("disable %d %s", w.Code, w.Body.String())
	}
	if w = request("GET", assetURL, nil, false); w.Code != 404 {
		t.Fatalf("disabled style kept asset public %d", w.Code)
	}
	body["url"] = "/collection-style-assets/" + string(bytes.Repeat([]byte{'a'}, 64))
	if w = request("POST", "/console/v1/admin/collection-styles", body, true); w.Code != 422 {
		t.Fatalf("nonexistent asset accepted %d %s", w.Code, w.Body.String())
	}
	if _, e := s.Pool.Exec(context.Background(), `CREATE FUNCTION reject_style_image_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='collection-style-images' THEN RAISE EXCEPTION 'synthetic image audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER test_image_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION reject_style_image_audit()`); e != nil {
		t.Fatal(e)
	}
	newImage := image.NewRGBA(image.Rect(0, 0, 123, 234))
	newImage.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var encoded bytes.Buffer
	png.Encode(&encoded, newImage)
	var failedPayload bytes.Buffer
	mw := multipart.NewWriter(&failedPayload)
	fp, _ := mw.CreateFormFile("file", "audit.png")
	fp.Write(encoded.Bytes())
	mw.Close()
	r = httptest.NewRequest("POST", "/console/v1/admin/collection-style-images", bytes.NewReader(failedPayload.Bytes()))
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.Header.Set("X-CSRF-Token", csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 500 {
		t.Fatalf("database audit failure reported as user input %d", w.Code)
	}
	var count int
	if e = s.Pool.QueryRow(context.Background(), "SELECT count(*) FROM collection_style_assets").Scan(&count); e != nil || count != 1 {
		t.Fatalf("failed audit leaked image %d %v", count, e)
	}
}

func TestCollectionStyleUXRoleCatalogAndImageCSRF(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	if _, e := pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('collection-styles','{"name":"public-old","title":"Public","background_color":"#ffffff","text_color":"#000000","enabled":true}'),('collection-styles','{"name":"private-old","title":"Private","background_color":"#ffffff","text_color":"#000000","enabled":false}')`); e != nil {
		t.Fatal(e)
	}
	mt, mc, e := s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant"})
	if e != nil {
		t.Fatal(e)
	}
	call := func(role, token, csrf, method, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/console/v1/"+role+path, bytes.NewReader([]byte(`{}`)))
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	if w := call("merchant", mt, mc, "GET", "/collection-styles"); w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte("public-old")) || bytes.Contains(w.Body.Bytes(), []byte("private-old")) {
		t.Fatalf("merchant catalog visibility %d %s", w.Code, w.Body.String())
	}
	for _, path := range []string{"/collection-styles", "/collection-style-images"} {
		if w := call("merchant", mt, mc, "POST", path); w.Code != 403 {
			t.Fatalf("merchant changed styles %s %d", path, w.Code)
		}
	}
	at, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	if w := call("admin", at, "wrong", "POST", "/collection-style-images"); w.Code != 403 {
		t.Fatalf("upload ignored CSRF %d", w.Code)
	}
	var count int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM collection_style_assets").Scan(&count); e != nil || count != 0 {
		t.Fatalf("unauthorized upload wrote data %d %v", count, e)
	}
}
