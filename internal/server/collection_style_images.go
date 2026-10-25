package server

import (
	"errors"
	"goepay/internal/collectionstyle"
	"goepay/internal/identity"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var collectionImageSlots = make(chan struct{}, 2)

func writeCollectionImage(w http.ResponseWriter, r *http.Request, a collectionstyle.Asset) {
	w.Header().Set("Content-Type", a.MIME)
	w.Header().Set("Content-Length", strconv.Itoa(len(a.Bytes)))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	if r.Method != "HEAD" {
		w.Write(a.Bytes)
	}
}
func (s *Server) collectionStyleAsset(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/collection-style-assets/") {
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "GET" && r.Method != "HEAD" {
		http.Error(w, "method not allowed", 405)
		return true
	}
	if r.URL.RawQuery != "" || s.Pool == nil {
		http.NotFound(w, r)
		return true
	}
	a, e := collectionstyle.ReadAsset(r.Context(), s.Pool, strings.TrimPrefix(r.URL.Path, "/collection-style-assets/"), true)
	if e != nil {
		http.NotFound(w, r)
	} else {
		writeCollectionImage(w, r, a)
	}
	return true
}
func (s *Server) collectionStyleImagesConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) < 2 || parts[1] != "collection-style-images" {
		return false
	}
	if role != "admin" || session.Issuer != nil {
		response(w, 403, -1, "own administrator required", nil)
		return true
	}
	if r.URL.RawQuery != "" {
		response(w, 400, -1, "query not accepted", nil)
		return true
	}
	if len(parts) == 3 && r.Method == "GET" {
		a, e := collectionstyle.ReadAsset(r.Context(), s.Pool, parts[2], false)
		if e != nil {
			response(w, 404, -1, "collection image not found", nil)
		} else {
			writeCollectionImage(w, r, a)
		}
		return true
	}
	if len(parts) != 2 || r.Method != "POST" {
		response(w, 405, -1, "image upload or private GET required", nil)
		return true
	}
	if r.URL.RawQuery != "" {
		response(w, 400, -1, "query not accepted", nil)
		return true
	}
	if !s.allowLogin(r.RemoteAddr, "collection-style-image", strconv.FormatInt(session.User.ID, 10), time.Now()) {
		response(w, 429, -1, "image upload rate limited", nil)
		return true
	}
	select {
	case collectionImageSlots <- struct{}{}:
		defer func() { <-collectionImageSlots }()
	default:
		response(w, 429, -1, "image processing busy", nil)
		return true
	}
	reader, e := r.MultipartReader()
	if e != nil {
		response(w, 415, -1, "one PNG or JPEG file required", nil)
		return true
	}
	part, e := reader.NextPart()
	if e != nil || part.FormName() != "file" {
		response(w, 422, -1, "one image file required", nil)
		return true
	}
	raw, e := io.ReadAll(io.LimitReader(part, collectionstyle.MaxImageInput+1))
	part.Close()
	if e != nil || len(raw) > collectionstyle.MaxImageInput {
		response(w, 413, -1, "image exceeds 1MiB", nil)
		return true
	}
	if extra, e := reader.NextPart(); e != io.EOF {
		if extra != nil {
			extra.Close()
		}
		response(w, 422, -1, "only one image is allowed", nil)
		return true
	}
	a, e := collectionstyle.Upload(r.Context(), s.Pool, raw, session.User.ID, session.User.Username)
	if e != nil {
		if errors.Is(e, collectionstyle.ErrInvalid) {
			response(w, 422, -1, "invalid PNG/JPEG image; maximum 4MP and 4096px edge", nil)
		} else {
			s.fail(w, e)
		}
	} else {
		response(w, 201, 0, "private background uploaded; publish by referencing an enabled collection style", a)
	}
	return true
}
