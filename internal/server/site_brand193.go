package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"goepay/internal/identity"
	"goepay/internal/sitebrand193"
)

func brandError193(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, identity.ErrAdminSession):
		response(w, 403, -1, "original super administrator session required", nil)
	case errors.Is(e, sitebrand193.ErrInvalid):
		response(w, 422, -1, "PNG/JPEG/GIF image below2MiB and at most one megapixel required", nil)
	case errors.Is(e, sitebrand193.ErrConflict):
		response(w, 409, -1, "brand image source changed or could not be confirmed; reload first", nil)
	default:
		response(w, 503, -1, "brand image settings unavailable", nil)
	}
}
func (s *Server) siteBrandConsole193(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) < 3 || parts[1] != "settings" || parts[2] != "branding" {
		return false
	}
	identityHeaders(w)
	if role != "admin" || session.User.AdminClass != "super" || session.Issuer != nil {
		response(w, 403, -1, "original super administrator session required", nil)
		return true
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "query parameters not accepted", nil)
		return true
	}
	if s.Pool == nil || len(s.CheckoutKey) < 32 {
		response(w, 503, -1, "brand image settings unavailable", nil)
		return true
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	store := sitebrand193.New(s.Pool, s.CheckoutKey)
	actor := sitebrand193.Actor{User: session.User, Token: token, Delegated: session.Issuer != nil}
	if len(parts) == 3 {
		if r.Method != "GET" {
			w.Header().Set("Allow", "GET")
			response(w, 405, -1, "method not allowed", nil)
			return true
		}
		view, e := store.Read(ctx, actor)
		if e != nil {
			brandError193(w, e)
		} else {
			response(w, 200, 0, "", view)
		}
		return true
	}
	if len(parts) != 4 || (parts[3] != "logo" && parts[3] != "icon") {
		response(w, 404, -1, "not found", nil)
		return true
	}
	if r.Method != "POST" {
		w.Header().Set("Allow", "POST")
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	expected := r.Header.Values("X-Expected-Fingerprint")
	if len(expected) != 1 || !identityHex(expected[0]) {
		response(w, 400, -1, "exact current branding fingerprint required", nil)
		return true
	}
	if !s.allowLogin(r.RemoteAddr, "site-brand-image", strconv.FormatInt(session.User.ID, 10), time.Now()) {
		w.Header().Set("Retry-After", "900")
		response(w, 429, -1, "image upload rate limited", nil)
		return true
	}
	select {
	case templateImageSlots <- struct{}{}:
		defer func() { <-templateImageSlots }()
	default:
		response(w, 429, -1, "image processing busy", nil)
		return true
	}
	r.Body = http.MaxBytesReader(w, r.Body, sitebrand193.MaxInput+16384)
	reader, e := r.MultipartReader()
	if e != nil {
		response(w, 415, -1, "one multipart file required", nil)
		return true
	}
	part, e := reader.NextPart()
	if e != nil || part.FormName() != "file" || part.FileName() == "" {
		response(w, 422, -1, "one image file required", nil)
		return true
	}
	raw, e := io.ReadAll(io.LimitReader(part, sitebrand193.MaxInput))
	part.Close()
	if e != nil || len(raw) >= sitebrand193.MaxInput {
		response(w, 413, -1, "image must be smaller than2MiB", nil)
		return true
	}
	extra, e := reader.NextPart()
	if e != io.EOF {
		if extra != nil {
			extra.Close()
		}
		response(w, 422, -1, "only one file part accepted", nil)
		return true
	}
	view, e := store.Publish(ctx, actor, parts[3], expected[0], raw)
	if e != nil {
		brandError193(w, e)
	} else {
		response(w, 200, 0, "", view)
	}
	return true
}
func (s *Server) siteBrandPublic193(w http.ResponseWriter, r *http.Request) bool {
	var slot string
	switch r.URL.Path {
	case "/static/img/logo.png":
		slot = "logo"
	case "/static/img/icon.png":
		slot = "icon"
	default:
		if strings.HasPrefix(r.URL.Path, "/static/img/") {
			w.Header().Set("Cache-Control", "no-store")
			http.NotFound(w, r)
			return true
		}
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		http.Error(w, "query parameters not accepted", 400)
		return true
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", 405)
		return true
	}
	if s.Pool == nil {
		http.Error(w, "brand image unavailable", 503)
		return true
	}
	image, e := sitebrand193.New(s.Pool, s.CheckoutKey).Public(r.Context(), slot)
	if errors.Is(e, sitebrand193.ErrNotFound) {
		http.NotFound(w, r)
		return true
	}
	if e != nil {
		http.Error(w, "brand image unavailable", 503)
		return true
	}
	w.Header().Set("Content-Type", image.MIME)
	w.Header().Set("Content-Length", strconv.Itoa(len(image.Bytes)))
	if r.Method != "HEAD" {
		w.Write(image.Bytes)
	}
	return true
}
