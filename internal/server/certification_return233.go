package server

import (
	"errors"
	"goepay/internal/kyc233"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var returnID233 = regexp.MustCompile(`^[a-f0-9]{32}$`)
var returnCode233 = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func (s *Server) certificationConsoleReturn233() (string, bool) {
	const path = "/merchant/account/certificate"
	if s.KYC233ConsoleOrigin == "" {
		return path, true
	}
	origin := s.KYC233ConsoleOrigin
	u, e := url.Parse(origin)
	if e != nil || len(origin) > 2048 || strings.ContainsAny(origin, "\\\r\n\x00") || u.User != nil || u.Opaque != "" || u.Hostname() == "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || (u.Path != "" && u.Path != "/") {
		return "", false
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")) {
		return "", false
	}
	u.Path = path
	return u.String(), true
}

func certificationReturnFailure233(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	io.WriteString(w, `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>实名认证返回</title><main><h1>认证返回信息尚未确认</h1><p>请回到商户实名认证页面读取当前状态。不要重复提交身份核验。</p><a href="/merchant/account/certificate">返回实名认证</a></main></html>`)
}

// The public OAuth return is an encrypted, state-bound inbox only. It never
// queries the provider, certifies a merchant, charges a fee or accepts a target
// URL. The original merchant must log in and explicitly query the saved attempt.
func (s *Server) certificationReturn233(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/callbacks/certification233/return" {
		return false
	}
	identityHeaders(w)
	w.Header().Set("Content-Security-Policy", "default-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	if r.Method != "GET" {
		w.Header().Set("Allow", "GET")
		certificationReturnFailure233(w, 405)
		return true
	}
	if r.URL.RawPath != "" || len(r.URL.RawQuery) > 8192 || r.ContentLength != 0 || len(r.TransferEncoding) > 0 {
		certificationReturnFailure233(w, 400)
		return true
	}
	q, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		certificationReturnFailure233(w, 400)
		return true
	}
	for k, v := range q {
		if len(v) != 1 || len(v[0]) > 2048 {
			certificationReturnFailure233(w, 400)
			return true
		}
		switch k {
		case "attempt_id", "state", "auth_code", "app_id", "source", "scope":
		default:
			certificationReturnFailure233(w, 400)
			return true
		}
	}
	if !returnID233.MatchString(q.Get("attempt_id")) || !returnID233.MatchString(q.Get("state")) || !returnCode233.MatchString(q.Get("auth_code")) {
		certificationReturnFailure233(w, 400)
		return true
	}
	if !s.allowCollectionAttempt(r.RemoteAddr, "certification-return233", time.Now()) {
		w.Header().Set("Retry-After", "60")
		certificationReturnFailure233(w, 429)
		return true
	}
	location, valid := s.certificationConsoleReturn233()
	if !valid {
		certificationReturnFailure233(w, 503)
		return true
	}
	service, e := s.certification233Service()
	if e != nil {
		certificationReturnFailure233(w, 503)
		return true
	}
	if e = service.CacheReturn(r.Context(), q.Get("attempt_id"), q.Get("state"), q.Get("auth_code")); e != nil {
		status := 503
		if errors.Is(e, kyc233.ErrInvalid) || errors.Is(e, kyc233.ErrConflict) {
			status = 400
		}
		certificationReturnFailure233(w, status)
		return true
	}
	w.Header().Set("Location", location)
	w.WriteHeader(http.StatusSeeOther)
	return true
}
