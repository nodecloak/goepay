package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"goepay/internal/compat"
	"goepay/internal/identity"
	"goepay/internal/pagetemplate"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func templateConfig(t *testing.T, kind, brand string) json.RawMessage {
	t.Helper()
	m := map[string]any{"brand_name": brand, "notice": "Local appearance only", "accent_color": "#007c91", "background_color": "#ddeeff", "logo_asset_id": "", "density": "comfortable"}
	if kind == "pay" {
		m["show_order_name"] = true
	}
	b, e := json.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestPageTemplateActuallyRendersCheckoutCollectionAndHomeWithoutChangingPaymentIdentity(t *testing.T) {
	s, pool, key, calls := routingHTTPFixture(t)
	ctx := context.Background()
	templates := pagetemplate.New(pool)
	admin := pagetemplate.Actor{Role: "admin", Class: "super", ID: 1, Name: "admin"}
	if _, e := templates.SaveConfig(ctx, "pay", "default", 0, templateConfig(t, "pay", "PAY-BRAND <img src=x onerror=1>"), admin); e != nil {
		t.Fatal(e)
	}
	out := routingInvoke(t, s, key, 1, "template-original-order", "Original order", "1.00", compat.Params{"channel_id": "7"})
	trade, link := fmt.Sprint(out["trade_no"]), fmt.Sprint(out["payurl"])
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}
	page := get(link)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "PAY-BRAND &lt;img") || !strings.Contains(page.Body.String(), `data-template="default"`) || strings.Contains(page.Body.String(), "<img src=x") {
		t.Fatal("saved appearance not consumed safely", page.Code, page.Body.String())
	}
	if !strings.Contains(page.Header().Get("Content-Security-Policy"), "script-src 'nonce-") {
		t.Fatal("checkout CSP lost", page.Header())
	}
	if _, e := templates.Select(ctx, "pay", "compact", 1, admin); e != nil {
		t.Fatal(e)
	}
	page = get(link)
	if page.Code != 200 || !strings.Contains(page.Body.String(), `data-template="compact"`) || !strings.Contains(page.Body.String(), "max-width:440px") {
		t.Fatal("selection not rendered", page.Code, page.Body.String())
	}
	// The anime (二次元) pay page frames the same checkout, then compact is
	// selected again for the fallback check below.
	if _, e := templates.Select(ctx, "pay", "anime", 2, admin); e != nil {
		t.Fatal(e)
	}
	page = get(link)
	if body := page.Body.String(); page.Code != 200 || !strings.Contains(body, `data-template="anime"`) || !strings.Contains(body, `<span class="template-mascot" aria-hidden="true">`) || !strings.Contains(page.Header().Get("Content-Security-Policy"), "script-src 'nonce-") {
		t.Fatal("anime pay page not rendered", page.Code, body)
	}
	if _, e := templates.Select(ctx, "pay", "compact", 3, admin); e != nil {
		t.Fatal(e)
	}
	if get("/checkout/"+trade).Code != 403 {
		t.Fatal("styling bypassed checkout HMAC")
	}
	if _, e := templates.SaveConfig(ctx, "qrpay", "default", 0, templateConfig(t, "qrpay", "QR-BRAND"), admin); e != nil {
		t.Fatal(e)
	}
	var codeID string
	if e := pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,owner_id,body)VALUES('collection-codes',1,'{"name":"local","title":"Local collect","enabled":true}')RETURNING id::text`).Scan(&codeID); e != nil {
		t.Fatal(e)
	}
	token, _, e := s.Sessions.Create(identity.User{Role: "merchant", Username: "merchant", ID: 1})
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("GET", "/console/v1/merchant/collection-codes/"+codeID+"/link", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	var result struct {
		Data struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if e = json.Unmarshal(w.Body.Bytes(), &result); e != nil || w.Code != 200 {
		t.Fatal(w.Code, e)
	}
	page = get(result.Data.URL)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "QR-BRAND") || !strings.Contains(page.Body.String(), "#ddeeff") {
		t.Fatal("collection appearance not consumed", page.Code, page.Body.String())
	}
	if get("/collect/"+codeID).Code != 403 {
		t.Fatal("template bypassed collect HMAC")
	}
	var styleID string
	if e = pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,owner_id,body)VALUES('collection-styles',1,'{"enabled":true,"background_color":"#112233","text_color":"#445566"}')RETURNING id::text`).Scan(&styleID); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{style_id}',to_jsonb($2::text)) WHERE id=$1::bigint`, codeID, styleID); e != nil {
		t.Fatal(e)
	}
	page = get(result.Data.URL)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "background:#112233") || !strings.Contains(page.Body.String(), "color:#445566") || !strings.Contains(page.Body.String(), "QR-BRAND") {
		t.Fatal("explicit per-code styles lost to global frame", page.Code, page.Body.String())
	}
	if _, e := templates.SaveConfig(ctx, "home", "default", 0, templateConfig(t, "home", "HOME-BRAND"), admin); e != nil {
		t.Fatal(e)
	}
	page = get("/")
	// The published site name supplies the home brand, while the selected
	// template still controls its own appearance and notice.
	if page.Code != 200 || !strings.Contains(page.Body.String(), "GOEPAY") || strings.Contains(page.Body.String(), "HOME-BRAND") || !strings.Contains(page.Body.String(), "background:#ddeeff") || !strings.Contains(page.Body.String(), "Local appearance only") {
		t.Fatal("home selection is metadata only", page.Code, page.Body.String())
	}
	order, e := s.Store.OrderByTradeNo(ctx, trade)
	if e != nil || order.MerchantID != 1 || order.Amount != 100 || order.Name != "Original order" || len(calls.list(trade)) != 1 {
		t.Fatal("template changed original intent/provider I/O", order, e, calls.list(trade))
	}
	b, e := s.Store.Balance(ctx, 1)
	if e != nil || b.Available != 0 {
		t.Fatal("appearance credited payment", b, e)
	}
	if _, e = pool.Exec(ctx, `INSERT INTO page_template_configs(kind,name,config,revision)VALUES('pay','compact','{"brand_name":"incomplete"}',2)`); e != nil {
		t.Fatal(e)
	}
	page = get(link)
	if page.Code != 200 || page.Header().Get("X-Goepay-Template-Fallback") != "invalid_config" || !strings.Contains(page.Body.String(), `data-template="default"`) || strings.Contains(page.Body.String(), "incomplete") {
		t.Fatal("invalid appearance fallback not observable or safe", page.Code, page.Header())
	}
	if get("/checkout/"+trade).Code != 403 {
		t.Fatal("appearance fallback hid security rejection")
	}
}

func TestPageTemplateConsoleHTTPAuthCASPrivateImageAndNoFinancialSideEffects(t *testing.T) {
	s, p, _ := fixture(t)
	admin, csrf, e := s.Sessions.Create(identity.User{Role: "admin", AdminClass: "super", ID: 1, Username: "admin", AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	merchant, _, e := s.Sessions.Create(identity.User{Role: "merchant", ID: 1, Username: "merchant", AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	tls := httptest.NewTLSServer(s.Handler())
	defer tls.Close()
	request := func(method, path, token, csrf, contentType string, body []byte) *http.Response {
		t.Helper()
		r, e := http.NewRequest(method, tls.URL+path, bytes.NewReader(body))
		if e != nil {
			t.Fatal(e)
		}
		if token != "" {
			r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		}
		if csrf != "" {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		if contentType != "" {
			r.Header.Set("Content-Type", contentType)
		}
		res, e := tls.Client().Do(r)
		if e != nil {
			t.Fatal(e)
		}
		return res
	}
	read := func(res *http.Response) []byte {
		t.Helper()
		defer res.Body.Close()
		b, e := io.ReadAll(res.Body)
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	assertCode := func(method, path, token, cs string, body []byte, want int) {
		t.Helper()
		r := request(method, path, token, cs, "application/json", body)
		b := read(r)
		if r.StatusCode != want {
			t.Fatalf("%s %s status %d want %d: %s", method, path, r.StatusCode, want, b)
		}
	}
	assertCode("GET", "/console/v1/admin/templates?type=pay", "", "", nil, 401)
	assertCode("GET", "/console/v1/admin/templates?type=pay", merchant, "", nil, 401)
	for _, path := range []string{"/console/v1/admin/templates?type=pay&type=home", "/console/v1/admin/templates?type=pay&file=anything", "/console/v1/admin/templates/pay/default/config?owner=2"} {
		assertCode("GET", path, admin, "", nil, 400)
	}
	assertCode("GET", "/console/v1/admin/templates?type=pay", admin, "", nil, 200)
	body := []byte(`{"expected_revision":"0","config":` + string(templateConfig(t, "pay", "HTTP local brand")) + `}`)
	assertCode("PUT", "/console/v1/admin/templates/pay/default/config", admin, "", body, 403)
	assertCode("PUT", "/console/v1/admin/templates/pay/default/config", admin, csrf, body, 200)
	assertCode("PUT", "/console/v1/admin/templates/pay/default/config", admin, csrf, body, 409)
	assertCode("PUT", "/console/v1/admin/templates/pay/selection", admin, csrf, []byte(`{"expected_revision":"1","name":"compact","name":"default"}`), 400)
	assertCode("PUT", "/console/v1/admin/templates/pay/selection", admin, csrf, []byte(`{"expected_revision":"01","name":"compact"}`), 422)
	// Decode and re-encode strips all trailing metadata before immutable image storage.
	var pngBytes bytes.Buffer
	png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	raw := append(pngBytes.Bytes(), []byte("private-upload-metadata")...)
	var form bytes.Buffer
	mp := multipart.NewWriter(&form)
	part, _ := mp.CreateFormFile("file", "local.png")
	part.Write(raw)
	mp.Close()
	upload := request("POST", "/console/v1/admin/templates/images", admin, csrf, mp.FormDataContentType(), form.Bytes())
	uploaded := read(upload)
	if upload.StatusCode != 201 || bytes.Contains(uploaded, []byte("private-upload-metadata")) {
		t.Fatal("image upload", upload.StatusCode, string(uploaded))
	}
	var envelope struct {
		Data pagetemplate.Asset `json:"data"`
	}
	if e = json.Unmarshal(uploaded, &envelope); e != nil || len(envelope.Data.ID) != 64 {
		t.Fatal(e, string(uploaded))
	}
	id := envelope.Data.ID
	beforePublish := request("GET", "/template-assets/"+id, "", "", "", nil)
	read(beforePublish)
	if beforePublish.StatusCode != 404 || beforePublish.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("private asset rejection may be cached across publication", beforePublish.StatusCode, beforePublish.Header)
	}
	private := request("GET", "/console/v1/admin/templates/images/"+id, admin, "", "", nil)
	canonical := read(private)
	if private.StatusCode != 200 || private.Header.Get("Cache-Control") != "no-store" || bytes.Contains(canonical, []byte("private-upload-metadata")) {
		t.Fatal("private image leak", private.StatusCode)
	}
	var cfg map[string]any
	json.Unmarshal(templateConfig(t, "pay", "HTTP local brand"), &cfg)
	cfg["logo_asset_id"] = id
	cfgBytes, _ := json.Marshal(cfg)
	assertCode("PUT", "/console/v1/admin/templates/pay/default/config", admin, csrf, []byte(`{"expected_revision":"1","config":`+string(cfgBytes)+`}`), 200)
	published := request("GET", "/template-assets/"+id, "", "", "", nil)
	data := read(published)
	if published.StatusCode != 200 || published.Header.Get("Content-Type") != "image/png" || published.Header.Get("Cache-Control") != "no-store" || !bytes.Equal(data, canonical) {
		t.Fatal("published asset", published.StatusCode)
	}
	preview := request("GET", "/console/v1/admin/templates/pay/default/preview", admin, "", "", nil)
	previewBody := read(preview)
	if preview.StatusCode != 200 || !bytes.Contains(previewBody, []byte("PREVIEW-ONLY")) || !strings.Contains(preview.Header.Get("Content-Security-Policy"), "script-src 'none'") || !bytes.Contains(previewBody, []byte("/console/v1/admin/templates/images/"+id)) {
		t.Fatal("unsafe preview", preview.StatusCode, string(previewBody))
	}
	assertCode("PUT", "/console/v1/admin/templates/pay/selection", admin, csrf, []byte(`{"expected_revision":"2","name":"compact"}`), 200)
	assertCode("GET", "/template-assets/"+id, "", "", nil, 404)
	for _, table := range []string{"orders", "ledger_entries", "jobs"} {
		var n int
		if e = p.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); e != nil {
			t.Fatal(e)
		}
		if n != 0 {
			t.Fatalf("appearance changed financial table %s: %d", table, n)
		}
	}
	var audit string
	if e = p.QueryRow(context.Background(), "SELECT coalesce(jsonb_agg(changes)::text,'') FROM backoffice_audit WHERE resource='page-templates'").Scan(&audit); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(audit, "HTTP local brand") || strings.Contains(audit, "private-upload-metadata") {
		t.Fatal("raw config/image audit leak")
	}
}
