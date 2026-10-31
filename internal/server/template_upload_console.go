package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"time"
	"unicode/utf8"

	"goepay/internal/pagetemplate"
)

// The console API of uploaded templates (上传模板), under
// /console/v1/admin/templates/<kind>/. templateConsole has already required
// the super administrator's own session, CSRF for writes and no query string.

// One package at a time is unpacked and checked.
var templateUploadSlots = make(chan struct{}, 1)

// templateUploadPath is a POST that carries a template package; the request
// body limit is raised for these two addresses only.
var templateUploadPath = regexp.MustCompile(`^/console/v1/admin/templates/(home|pay|qrpay)/uploads(/check)?$`)

const templateUploadBodyLimit = pagetemplate.MaxPackageZip + 1<<20

// homeUploadCSP is the policy of an uploaded home page: its scripts run, but
// in a sandbox without the site's origin, so they cannot read cookies,
// storage or the console API.
func homeUploadCSP(frameAncestors string) string {
	return "default-src 'none'; script-src 'self' 'unsafe-inline' https:; style-src 'self' 'unsafe-inline' https:; img-src 'self' data: blob: https:; font-src 'self' data: https:; media-src 'self' https:; connect-src 'self' https:; frame-src https:; form-action 'none'; base-uri 'self'; frame-ancestors " + frameAncestors + "; sandbox allow-scripts allow-popups allow-popups-to-escape-sandbox allow-modals"
}

func writeHomePreview(w http.ResponseWriter, r pagetemplate.Resolved, pageURL, logoURL string) {
	var b bytes.Buffer
	demo := pagetemplate.HomeData{SiteName: "预览站点名称", Title: "首页模板预览", Org: "示例科技有限公司", Email: "support@example.com", QQ: "10001", TestOpen: true, RegistrationOpen: true, Preview: true, PageURL: pageURL, LogoURL: logoURL}
	if e := pagetemplate.RenderHome(&b, r, demo); e != nil {
		templateFail(w, e)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if r.Upload != nil {
		w.Header().Set("Content-Security-Policy", homeUploadCSP("'self'"))
	} else {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src 'self'; script-src 'none'; form-action 'none'; base-uri 'none'; frame-ancestors 'self'; sandbox allow-same-origin")
	}
	w.Write(b.Bytes())
}

// templatePreview writes the console preview of a template: the built-in
// one, or a version of an uploaded one (0 is the newest).
func (s *Server) templatePreview(ctx context.Context, w http.ResponseWriter, r *http.Request, store *pagetemplate.Store, actor pagetemplate.Actor, kind, name string, version int) {
	v, e := store.Config(ctx, kind, name, actor)
	if e != nil {
		templateFail(w, e)
		return
	}
	raw, _ := json.Marshal(v.Config)
	cfg, e := pagetemplate.ParseConfig(v.Type, v.Name, raw)
	if e != nil {
		templateFail(w, e)
		return
	}
	resolved := pagetemplate.Resolved{Type: v.Type, Name: v.Name, Revision: v.Revision, Config: cfg}
	if pagetemplate.ValidUploadName(name) {
		if resolved.Upload, e = store.UploadPreview(ctx, kind, name, version, actor); e != nil {
			templateFail(w, e)
			return
		}
	} else if version != 0 {
		response(w, 404, -1, "template operation not found", nil)
		return
	}
	if kind == "home" {
		logo := ""
		if resolved.Upload != nil && cfg.LogoAssetID != "" {
			// The preview of an uploaded home page has no origin of its own,
			// so the private image goes inline.
			if a, e := store.Asset(ctx, cfg.LogoAssetID, actor); e == nil {
				logo = "data:" + a.MIME + ";base64," + base64.StdEncoding.EncodeToString(a.Bytes)
			}
		}
		writeHomePreview(w, resolved, r.URL.Path, logo)
		return
	}
	var b bytes.Buffer
	page := pagetemplate.Page{Title: "支付收银台", TradeNo: "PREVIEW-ONLY", Amount: "1.00", OrderName: "模板演示订单", Preview: true}
	if kind == "qrpay" {
		page = pagetemplate.Page{Title: "向商户付款", MerchantName: "示例商户", Preview: true}
	}
	tail, e := pagetemplate.Open(&b, resolved, page)
	if e != nil {
		templateFail(w, e)
		return
	}
	io.WriteString(&b, templatePreviewBody(kind)+tail)
	csp := "default-src 'none'; style-src 'unsafe-inline'; img-src 'self'; script-src 'none'; form-action 'none'; base-uri 'none'; frame-ancestors 'self'; sandbox allow-same-origin"
	if resolved.Upload != nil {
		csp = widenTemplateCSP(csp)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", csp)
	w.Write(b.Bytes())
}

// templateVersionNumber parses a canonical positive version number.
func templateVersionNumber(text string) (int, bool) {
	n, e := strconv.Atoi(text)
	return n, e == nil && n > 0 && n <= 1<<30 && strconv.Itoa(n) == text
}

// decodeTemplateJSON reads one JSON object with exactly the given fields,
// each at most once.
func decodeTemplateJSON(w http.ResponseWriter, r *http.Request, fields ...string) (map[string]json.RawMessage, bool) {
	media, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || media != "application/json" {
		response(w, 415, -1, "application/json required", nil)
		return nil, false
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, 4097))
	if e != nil || len(raw) > 4096 {
		response(w, 413, -1, "request body exceeds 4KiB", nil)
		return nil, false
	}
	invalid := func() (map[string]json.RawMessage, bool) {
		response(w, 400, -1, "invalid or ambiguous JSON", nil)
		return nil, false
	}
	if !utf8.Valid(raw) {
		return invalid()
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if t, e := d.Token(); e != nil || t != json.Delim('{') {
		return invalid()
	}
	out := map[string]json.RawMessage{}
	for d.More() {
		t, e := d.Token()
		key, ok := t.(string)
		if e != nil || !ok {
			return invalid()
		}
		if _, dup := out[key]; dup {
			return invalid()
		}
		var value json.RawMessage
		if e = d.Decode(&value); e != nil {
			return invalid()
		}
		out[key] = value
	}
	if t, e := d.Token(); e != nil || t != json.Delim('}') {
		return invalid()
	}
	if _, e := d.Token(); e != io.EOF {
		return invalid()
	}
	if len(out) != len(fields) {
		return invalid()
	}
	for _, f := range fields {
		if _, ok := out[f]; !ok {
			return invalid()
		}
	}
	return out, true
}

func templateExpected(w http.ResponseWriter, raw json.RawMessage) (int64, bool) {
	var text string
	if json.Unmarshal(raw, &text) != nil {
		response(w, 422, -1, "canonical expected revision required", nil)
		return 0, false
	}
	expected, ok := templateRevision(text)
	if !ok {
		response(w, 422, -1, "canonical expected revision required", nil)
	}
	return expected, ok
}

// readTemplatePackage reads the multipart upload: expected_revision first
// (when wanted), then exactly one file part.
func readTemplatePackage(w http.ResponseWriter, r *http.Request, withRevision bool) (int64, []byte, bool) {
	reader, e := r.MultipartReader()
	if e != nil {
		response(w, 415, -1, "multipart template package required", nil)
		return 0, nil, false
	}
	var expected int64
	if withRevision {
		part, e := reader.NextPart()
		if e != nil || part.FormName() != "expected_revision" {
			response(w, 422, -1, "expected_revision and then one zip file required", nil)
			return 0, nil, false
		}
		text, e := io.ReadAll(io.LimitReader(part, 32))
		part.Close()
		ok := false
		if e == nil {
			expected, ok = templateRevision(string(text))
		}
		if !ok {
			response(w, 422, -1, "canonical expected revision required", nil)
			return 0, nil, false
		}
	}
	part, e := reader.NextPart()
	if e != nil || part.FormName() != "file" {
		response(w, 422, -1, "one zip file required", nil)
		return 0, nil, false
	}
	raw, e := io.ReadAll(io.LimitReader(part, pagetemplate.MaxPackageZip+1))
	part.Close()
	if e != nil || len(raw) > pagetemplate.MaxPackageZip {
		response(w, 413, -1, "模板包超过 10 MiB", nil)
		return 0, nil, false
	}
	if extra, e := reader.NextPart(); e != io.EOF {
		if extra != nil {
			extra.Close()
		}
		response(w, 422, -1, "only one file part is allowed", nil)
		return 0, nil, false
	}
	return expected, raw, true
}

func writeZip(w http.ResponseWriter, r *http.Request, name string, b []byte) {
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	if r.Method != "HEAD" {
		w.Write(b)
	}
}

// templateUploads serves the upload routes of templateConsole; parts are the
// path segments after /console/v1/, and false means none matched.
func (s *Server) templateUploads(ctx context.Context, w http.ResponseWriter, r *http.Request, store *pagetemplate.Store, actor pagetemplate.Actor, parts []string) bool {
	kind := parts[2]
	method := func(want string) bool {
		if r.Method != want {
			response(w, 405, -1, want+" required", nil)
			return false
		}
		return true
	}
	switch {
	case len(parts) == 4 && parts[3] == "sample":
		if !method("GET") {
			return true
		}
		b, e := pagetemplate.SamplePackage(kind)
		if e != nil {
			templateFail(w, e)
			return true
		}
		writeZip(w, r, "goepay-"+kind+"-sample.zip", b)
	case len(parts) == 4 && parts[3] == "uploads", len(parts) == 5 && parts[3] == "uploads" && parts[4] == "check":
		if !method("POST") {
			return true
		}
		check := len(parts) == 5
		operation := "template-upload"
		if check {
			operation = "template-upload-check"
		}
		if !s.allowLogin(r.RemoteAddr, operation, fmt.Sprint(actor.ID), time.Now()) {
			w.Header().Set("Retry-After", "900")
			response(w, 429, -1, "上传太频繁，请 15 分钟后再试", nil)
			return true
		}
		select {
		case templateUploadSlots <- struct{}{}:
			defer func() { <-templateUploadSlots }()
		default:
			response(w, 429, -1, "正在处理另一个模板包，请稍后再试", nil)
			return true
		}
		expected, raw, ok := readTemplatePackage(w, r, !check)
		if !ok {
			return true
		}
		work, cancel := context.WithTimeout(r.Context(), time.Minute)
		defer cancel()
		if check {
			out, e := store.CheckUpload(work, kind, raw, actor)
			if e != nil {
				templateFail(w, e)
				return true
			}
			response(w, 200, 0, "template package checked; nothing was saved", out)
			return true
		}
		out, e := store.UploadTemplate(work, kind, expected, raw, actor)
		if e != nil {
			templateFail(w, e)
			return true
		}
		response(w, 201, 0, "template package saved as a new version; administrators are notified", out)
	case len(parts) == 5 && parts[4] == "versions":
		if !method("GET") {
			return true
		}
		items, e := store.UploadVersions(ctx, kind, parts[3], actor)
		if e != nil {
			templateFail(w, e)
			return true
		}
		response(w, 200, 0, "newest 50 versions of the uploaded template", map[string]any{"items": items})
	case len(parts) == 5 && parts[4] == "remove":
		if !method("POST") {
			return true
		}
		body, ok := decodeTemplateJSON(w, r, "expected_revision")
		if !ok {
			return true
		}
		expected, ok := templateExpected(w, body["expected_revision"])
		if !ok {
			return true
		}
		revision, e := store.RemoveUpload(ctx, kind, parts[3], expected, actor)
		if e != nil {
			templateFail(w, e)
			return true
		}
		response(w, 200, 0, "uploaded template removed; its versions stay in the history", map[string]any{"revision": strconv.FormatInt(revision, 10)})
	case len(parts) == 7 && parts[4] == "versions":
		version, ok := templateVersionNumber(parts[5])
		if !ok {
			response(w, 404, -1, "template operation not found", nil)
			return true
		}
		switch parts[6] {
		case "preview":
			if !method("GET") {
				return true
			}
			s.templatePreview(ctx, w, r, store, actor, kind, parts[3], version)
		case "package":
			if !method("GET") {
				return true
			}
			b, e := store.UploadPackage(ctx, kind, parts[3], version, actor)
			if e != nil {
				templateFail(w, e)
				return true
			}
			writeZip(w, r, parts[3]+"-v"+strconv.Itoa(version)+".zip", b)
		case "restore":
			if !method("POST") {
				return true
			}
			body, ok := decodeTemplateJSON(w, r, "expected_revision")
			if !ok {
				return true
			}
			expected, ok := templateExpected(w, body["expected_revision"])
			if !ok {
				return true
			}
			work, cancel := context.WithTimeout(r.Context(), time.Minute)
			defer cancel()
			out, e := store.RestoreUpload(work, kind, parts[3], expected, version, actor)
			if e != nil {
				templateFail(w, e)
				return true
			}
			response(w, 200, 0, "older version restored as a new version; administrators are notified", map[string]any{"version": out.Version, "revision": strconv.FormatInt(out.Revision, 10)})
		default:
			response(w, 404, -1, "template operation not found", nil)
		}
	default:
		return false
	}
	return true
}
