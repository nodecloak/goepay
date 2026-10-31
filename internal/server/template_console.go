package server

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"goepay/internal/identity"
	"goepay/internal/pagetemplate"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

var templateImageSlots = make(chan struct{}, 2)

// templatePreviewBody is the demo content under a pay/qrpay template in the
// console preview. It looks like the real page but is inert: no form, button,
// script or external image (the QR code is inline SVG of a demo text).
func templatePreviewBody(kind string) string {
	if kind == "qrpay" {
		return `<p>收款说明：模板演示</p><span class="template-demo-label">金额（元）</span><span class="template-demo-input template-demo-amount">0.00</span><span class="template-demo-label">支付方式</span><span class="template-demo-input">微信支付 / 支付宝</span><span class="template-demo-button">前往支付</span><p>演示材料：没有支付按钮，不创建订单。</p>`
	}
	return `<p>请使用支付宝或微信扫描支付二维码。</p>` + previewQRSVG("GOEPAY-TEMPLATE-PREVIEW") + `<p>演示材料：没有支付按钮，不创建订单。</p>`
}

// previewQRSVG draws a QR code of text as inline SVG modules.
func previewQRSVG(text string) string {
	q, e := qrcode.New(text, qrcode.Medium)
	if e != nil {
		return ""
	}
	q.DisableBorder = true
	bm := q.Bitmap()
	var b strings.Builder
	fmt.Fprintf(&b, `<svg viewBox="0 0 %d %d" role="img" aria-label="演示二维码" shape-rendering="crispEdges"><path fill="#111" d="`, len(bm), len(bm))
	for y, row := range bm {
		for x, on := range row {
			if on {
				fmt.Fprintf(&b, "M%d %dh1v1h-1z", x, y)
			}
		}
	}
	b.WriteString(`"/></svg>`)
	return b.String()
}

func templateFail(w http.ResponseWriter, e error) {
	status, message := 500, "template service unavailable"
	var refused *pagetemplate.CustomError
	switch {
	case errors.As(e, &refused):
		// A refused custom page names its reason for the administrator.
		status, message = 422, refused.Reason
	case errors.Is(e, pagetemplate.ErrForbidden):
		status, message = 403, "own super administrator required"
	case errors.Is(e, pagetemplate.ErrInvalid):
		status, message = 422, "invalid typed template settings or image"
	case errors.Is(e, pagetemplate.ErrConflict):
		status, message = 409, "template revision conflict; reload original settings"
	case errors.Is(e, pagetemplate.ErrNotFound):
		status, message = 404, "template or image not found"
	}
	response(w, status, -1, message, nil)
}
func templateRevision(text string) (int64, bool) {
	v, e := strconv.ParseInt(text, 10, 64)
	return v, e == nil && v >= 0 && strconv.FormatInt(v, 10) == text
}
func (s *Server) templateConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) < 2 || parts[1] != "templates" {
		return false
	}
	if role != "admin" || session.User.Role != "admin" || session.User.AdminClass != "super" || session.User.ID <= 0 || session.User.Username == "" || session.Issuer != nil {
		response(w, 403, -1, "own super administrator required", nil)
		return true
	}
	if r.Method != "GET" && r.Method != "HEAD" && (session.CSRF == "" || subtle.ConstantTimeCompare([]byte(session.CSRF), []byte(r.Header.Get("X-CSRF-Token"))) != 1) {
		response(w, 403, -1, "invalid CSRF token", nil)
		return true
	}
	if s.Pool == nil {
		response(w, 503, -1, "template service unavailable", nil)
		return true
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	store := pagetemplate.New(s.Pool)
	actor := pagetemplate.Actor{Role: "admin", Class: "super", ID: session.User.ID, Name: session.User.Username}
	if len(parts) == 2 {
		if r.Method != "GET" {
			response(w, 405, -1, "GET required", nil)
			return true
		}
		q, e := url.ParseQuery(r.URL.RawQuery)
		if e != nil || len(q) != 1 || len(q["type"]) != 1 {
			response(w, 400, -1, "one supported template type required", nil)
			return true
		}
		out, e := store.List(ctx, q.Get("type"), actor)
		if e != nil {
			templateFail(w, e)
		} else {
			response(w, 200, 0, "built-in templates only", out)
		}
		return true
	}
	if r.URL.RawQuery != "" {
		response(w, 400, -1, "query parameters not accepted", nil)
		return true
	}
	if parts[2] == "images" {
		if len(parts) == 4 && r.Method == "GET" {
			a, e := store.Asset(ctx, parts[3], actor)
			if e != nil {
				templateFail(w, e)
			} else {
				writeTemplateAsset(w, r, a)
			}
			return true
		}
		if len(parts) != 3 || r.Method != "POST" {
			response(w, 405, -1, "image POST or exact private GET required", nil)
			return true
		}
		if !s.allowLogin(r.RemoteAddr, "template-image", fmt.Sprint(actor.ID), time.Now()) {
			w.Header().Set("Retry-After", "900")
			response(w, 429, -1, "template image upload rate limited", nil)
			return true
		}
		select {
		case templateImageSlots <- struct{}{}:
			defer func() { <-templateImageSlots }()
		default:
			response(w, 429, -1, "template image processing busy", nil)
			return true
		}
		reader, e := r.MultipartReader()
		if e != nil {
			response(w, 415, -1, "one multipart image file required", nil)
			return true
		}
		part, e := reader.NextPart()
		if e != nil || part.FormName() != "file" {
			response(w, 422, -1, "one image file required", nil)
			return true
		}
		raw, e := io.ReadAll(io.LimitReader(part, pagetemplate.MaxImageInput+1))
		part.Close()
		if e != nil || len(raw) > pagetemplate.MaxImageInput {
			response(w, 413, -1, "image exceeds 512KiB limit", nil)
			return true
		}
		if extra, e := reader.NextPart(); e != io.EOF {
			if extra != nil {
				extra.Close()
			}
			response(w, 422, -1, "only one file part is allowed", nil)
			return true
		}
		a, e := store.Upload(ctx, raw, actor)
		if e != nil {
			templateFail(w, e)
		} else {
			response(w, 201, 0, "private image uploaded; becomes public only while referenced by the selected template", a)
		}
		return true
	}
	if len(parts) == 4 && parts[3] == "selection" {
		if r.Method != "PUT" {
			response(w, 405, -1, "PUT required", nil)
			return true
		}
		var in struct {
			Expected string `json:"expected_revision"`
			Name     string `json:"name"`
		}
		if !decodeFundsJSON(w, r, &in) {
			return true
		}
		expected, ok := templateRevision(in.Expected)
		if !ok {
			response(w, 422, -1, "canonical expected revision required", nil)
			return true
		}
		out, e := store.Select(ctx, parts[2], in.Name, expected, actor)
		if e != nil {
			templateFail(w, e)
		} else {
			response(w, 200, 0, "selected built-in template now affects page appearance only", out)
		}
		return true
	}
	if len(parts) >= 5 && parts[2] == "home" && parts[3] == "custom" && (parts[4] == "html" || parts[4] == "sample" || parts[4] == "versions") {
		s.templateCustom(ctx, w, r, store, actor, parts[4:])
		return true
	}
	if len(parts) == 5 && (parts[4] == "config" || parts[4] == "preview") {
		if parts[4] == "preview" {
			if r.Method != "GET" {
				response(w, 405, -1, "GET required", nil)
				return true
			}
			v, e := store.Config(ctx, parts[2], parts[3], actor)
			if e != nil {
				templateFail(w, e)
				return true
			}
			raw, _ := json.Marshal(v.Config)
			cfg, e := pagetemplate.ParseConfig(v.Type, v.Name, raw)
			if e != nil {
				templateFail(w, e)
				return true
			}
			if v.Type == "home" {
				var custom *pagetemplate.CustomContent
				if v.Name == "custom" {
					if custom, e = store.CustomPreview(ctx, 0, actor); e != nil {
						templateFail(w, e)
						return true
					}
				}
				writeHomePreview(w, pagetemplate.Resolved{Type: v.Type, Name: v.Name, Revision: v.Revision, Config: cfg, Custom: custom})
				return true
			}
			var b bytes.Buffer
			page := pagetemplate.Page{Title: "支付收银台", TradeNo: "PREVIEW-ONLY", Amount: "1.00", OrderName: "模板演示订单", Preview: true}
			if v.Type == "qrpay" {
				page = pagetemplate.Page{Title: "向商户付款", MerchantName: "示例商户", Preview: true}
			}
			if e = pagetemplate.Open(&b, pagetemplate.Resolved{Type: v.Type, Name: v.Name, Revision: v.Revision, Config: cfg}, page); e != nil {
				templateFail(w, e)
				return true
			}
			io.WriteString(&b, templatePreviewBody(v.Type))
			pagetemplate.Close(&b)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src 'self'; script-src 'none'; form-action 'none'; base-uri 'none'; frame-ancestors 'self'; sandbox allow-same-origin")
			w.Write(b.Bytes())
			return true
		}
		switch r.Method {
		case "GET":
			out, e := store.Config(ctx, parts[2], parts[3], actor)
			if e != nil {
				templateFail(w, e)
			} else {
				response(w, 200, 0, "typed built-in configuration; revision covers this template type", out)
			}
		case "PUT":
			var in struct {
				Expected string          `json:"expected_revision"`
				Config   json.RawMessage `json:"config"`
			}
			if !decodeFundsJSON(w, r, &in) {
				return true
			}
			expected, ok := templateRevision(in.Expected)
			if !ok {
				response(w, 422, -1, "canonical expected revision required", nil)
				return true
			}
			out, e := store.SaveConfig(ctx, parts[2], parts[3], expected, in.Config, actor)
			if e != nil {
				templateFail(w, e)
			} else {
				response(w, 200, 0, "template configuration saved; selected template appearance updates immediately", out)
			}
		default:
			response(w, 405, -1, "GET or PUT required", nil)
		}
		return true
	}
	response(w, 404, -1, "template operation not found", nil)
	return true
}
