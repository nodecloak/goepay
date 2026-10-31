package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strconv"
	"unicode/utf8"

	"goepay/internal/pagetemplate"
)

// The console API of the custom home template (自定义 HTML), under
// /console/v1/admin/templates/home/custom/. templateConsole has already
// required the super administrator's own session, CSRF for writes and no
// query string.

// customTemplateBodyLimit allows 64 KiB of HTML and 32 KiB of CSS even when
// JSON escaping doubles them.
const customTemplateBodyLimit = 512 << 10

func writeHomePreview(w http.ResponseWriter, r pagetemplate.Resolved) {
	var b bytes.Buffer
	demo := pagetemplate.HomeData{SiteName: "预览站点名称", Title: "首页模板预览", Org: "示例科技有限公司", Email: "support@example.com", QQ: "10001", TestOpen: true, RegistrationOpen: true, Preview: true}
	if e := pagetemplate.RenderHome(&b, r, demo); e != nil {
		templateFail(w, e)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src 'self'; script-src 'none'; form-action 'none'; base-uri 'none'; frame-ancestors 'self'; sandbox allow-same-origin")
	w.Write(b.Bytes())
}

// customVersionNumber parses a canonical positive version number.
func customVersionNumber(text string) (int, bool) {
	n, e := strconv.Atoi(text)
	return n, e == nil && n > 0 && n <= 1<<30 && strconv.Itoa(n) == text
}

// decodeCustomTemplateJSON reads one JSON object with exactly the given
// fields, each at most once.
func decodeCustomTemplateJSON(w http.ResponseWriter, r *http.Request, fields ...string) (map[string]json.RawMessage, bool) {
	media, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || media != "application/json" {
		response(w, 415, -1, "application/json required", nil)
		return nil, false
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, customTemplateBodyLimit+1))
	if e != nil || len(raw) > customTemplateBodyLimit {
		response(w, 413, -1, "request body exceeds 512KiB", nil)
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

func customExpected(w http.ResponseWriter, raw json.RawMessage) (int64, bool) {
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

func (s *Server) templateCustom(ctx context.Context, w http.ResponseWriter, r *http.Request, store *pagetemplate.Store, actor pagetemplate.Actor, rest []string) {
	method := func(want string) bool {
		if r.Method != want {
			response(w, 405, -1, want+" required", nil)
			return false
		}
		return true
	}
	switch {
	case len(rest) == 1 && rest[0] == "sample":
		if !method("GET") {
			return
		}
		sample := pagetemplate.CustomSample()
		response(w, 200, 0, "starter custom home page", map[string]any{"html": sample.HTML, "css": sample.CSS, "frame": sample.Frame})
	case len(rest) == 1 && rest[0] == "html":
		switch r.Method {
		case "GET":
			out, e := store.CustomCurrent(ctx, actor)
			if e != nil {
				templateFail(w, e)
				return
			}
			response(w, 200, 0, "newest custom home page source; revision covers the home template type", out)
		case "PUT":
			body, ok := decodeCustomTemplateJSON(w, r, "expected_revision", "html", "css", "frame")
			if !ok {
				return
			}
			expected, ok := customExpected(w, body["expected_revision"])
			if !ok {
				return
			}
			var in pagetemplate.CustomInput
			if json.Unmarshal(body["html"], &in.HTML) != nil || json.Unmarshal(body["css"], &in.CSS) != nil || json.Unmarshal(body["frame"], &in.Frame) != nil {
				response(w, 422, -1, "html and css must be text and frame a switch", nil)
				return
			}
			out, e := store.SaveCustom(ctx, expected, in, actor)
			if e != nil {
				templateFail(w, e)
				return
			}
			response(w, 200, 0, "custom home page saved as a new version; administrators are notified", out)
		default:
			response(w, 405, -1, "GET or PUT required", nil)
		}
	case len(rest) == 1 && rest[0] == "versions":
		if !method("GET") {
			return
		}
		items, e := store.CustomVersions(ctx, actor)
		if e != nil {
			templateFail(w, e)
			return
		}
		response(w, 200, 0, "newest 50 custom home page versions", map[string]any{"items": items})
	case len(rest) >= 2 && rest[0] == "versions":
		version, ok := customVersionNumber(rest[1])
		if !ok || len(rest) > 3 {
			response(w, 404, -1, "template operation not found", nil)
			return
		}
		if len(rest) == 2 {
			if !method("GET") {
				return
			}
			out, e := store.CustomVersion(ctx, version, actor)
			if e != nil {
				templateFail(w, e)
				return
			}
			response(w, 200, 0, "custom home page version source", out)
			return
		}
		switch rest[2] {
		case "restore":
			if !method("POST") {
				return
			}
			body, ok := decodeCustomTemplateJSON(w, r, "expected_revision")
			if !ok {
				return
			}
			expected, ok := customExpected(w, body["expected_revision"])
			if !ok {
				return
			}
			out, e := store.RestoreCustom(ctx, expected, version, actor)
			if e != nil {
				templateFail(w, e)
				return
			}
			response(w, 200, 0, "older custom home page restored as a new version; administrators are notified", out)
		case "preview":
			if !method("GET") {
				return
			}
			view, e := store.Config(ctx, "home", "custom", actor)
			if e != nil {
				templateFail(w, e)
				return
			}
			raw, _ := json.Marshal(view.Config)
			cfg, e := pagetemplate.ParseConfig("home", "custom", raw)
			if e != nil {
				templateFail(w, e)
				return
			}
			custom, e := store.CustomPreview(ctx, version, actor)
			if e != nil {
				templateFail(w, e)
				return
			}
			writeHomePreview(w, pagetemplate.Resolved{Type: "home", Name: "custom", Revision: view.Revision, Config: cfg, Custom: custom})
		default:
			response(w, 404, -1, "template operation not found", nil)
		}
	default:
		response(w, 404, -1, "template operation not found", nil)
	}
}
