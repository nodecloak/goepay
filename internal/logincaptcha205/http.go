package logincaptcha205

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func reply(w http.ResponseWriter, status int, message string, data any) {
	code := 0
	if status >= 400 {
		code = -1
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"code": code, "message": message, "data": data})
}
func WriteError(w http.ResponseWriter, e error) {
	status, message := 503, "验证码服务不可用，请稍后重试"
	if errors.Is(e, ErrInvalid) {
		status, message = 422, "登录验证码无效或已使用，请重新验证"
	} else if errors.Is(e, ErrConflict) {
		status, message = 409, "登录验证码配置已变化，请重新核对"
	}
	reply(w, status, message, nil)
}
func sourceIP(r *http.Request) (string, error) {
	v, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		v = r.RemoteAddr
	}
	ip := net.ParseIP(v)
	if ip == nil {
		return "", ErrInvalid
	}
	return ip.String(), nil
}
func strictUsername(w http.ResponseWriter, r *http.Request) (string, bool) {
	media, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || media != "application/json" {
		reply(w, 415, "JSON required", nil)
		return "", false
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		reply(w, 400, "exact username object required", nil)
		return "", false
	}
	username := ""
	count := 0
	for d.More() {
		t, e = d.Token()
		if e != nil || t != "username" || count != 0 || d.Decode(&username) != nil {
			reply(w, 400, "exact username object required", nil)
			return "", false
		}
		count++
	}
	if t, e = d.Token(); e != nil || t != json.Delim('}') || count != 1 || d.Decode(&struct{}{}) != io.EOF {
		reply(w, 400, "exact username object required", nil)
		return "", false
	}
	if !validUsername(username) {
		WriteError(w, ErrInvalid)
		return "", false
	}
	return username, true
}
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if s == nil {
		WriteError(w, ErrUnavailable)
		return
	}
	init := r.URL.Path == Path+"/init"
	if !init && r.URL.Path != Path {
		reply(w, 404, "not found", nil)
		return
	}
	method := "GET"
	if init {
		method = "POST"
	}
	if r.Method != method {
		w.Header().Set("Allow", method)
		reply(w, 405, "method not allowed", nil)
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != s.origin {
		reply(w, 403, "origin rejected", nil)
		return
	}
	if init {
		if r.URL.RawQuery != "" || r.URL.ForceQuery {
			reply(w, 400, "query parameters not accepted", nil)
			return
		}
		if s.origin == "" || r.Header.Get("Origin") != s.origin {
			reply(w, 403, "configured origin required", nil)
			return
		}
	} else {
		q, e := url.ParseQuery(r.URL.RawQuery)
		if e != nil || len(q) != 2 || len(q["role"]) != 1 || len(q["mode"]) != 1 || q.Get("role") != "merchant" || q.Get("mode") != "password" {
			reply(w, 400, "merchant password metadata query required", nil)
			return
		}
	}
	source, e := sourceIP(r)
	if e != nil {
		WriteError(w, e)
		return
	}
	action := "metadata"
	if init {
		action = "init"
	}
	if s.limiter != nil && !s.limiter(r.RemoteAddr, action) {
		w.Header().Set("Retry-After", "60")
		reply(w, 429, "too many login verification attempts", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	if !init {
		tx, e := s.pool.Begin(ctx)
		if e != nil {
			WriteError(w, e)
			return
		}
		defer tx.Rollback(ctx)
		p, e := s.policyTx(ctx, tx)
		if e != nil {
			WriteError(w, e)
			return
		}
		reply(w, 200, "", map[string]bool{"enabled": p.Enabled})
		return
	}
	username, ok := strictUsername(w, r)
	if !ok {
		return
	}
	s.initialize(w, r, username, source)
}

func (s *Service) initialize(w http.ResponseWriter, r *http.Request, username, source string) {
	ctx := r.Context()
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		WriteError(w, e)
		return
	}
	defer tx.Rollback(ctx)
	p, e := s.policyTx(ctx, tx)
	if e != nil {
		WriteError(w, e)
		return
	}
	if !p.Enabled {
		WriteError(w, ErrConflict)
		return
	}
	p, e = s.fullPolicyTx(ctx, tx, p)
	if e != nil {
		WriteError(w, e)
		return
	}
	value, count := "", 0
	for _, cookie := range r.Cookies() {
		if cookie.Name == CookieName {
			value = cookie.Value
			count++
		}
	}
	if count > 1 || count == 1 && !tokenPattern.MatchString(value) {
		WriteError(w, ErrInvalid)
		return
	}
	if count == 0 {
		value, e = randomToken()
		if e != nil {
			WriteError(w, e)
			return
		}
	}
	token, e := randomToken()
	if e != nil {
		WriteError(w, e)
		return
	}
	now, e := dbNow(ctx, tx)
	if e != nil {
		WriteError(w, e)
		return
	}
	until := now.Add(2 * time.Minute)
	verifyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	protocol, e := s.verifier.Init(verifyCtx, p.Config, "login", source)
	cancel()
	if e != nil {
		WriteError(w, e)
		return
	}
	if protocol.Version != p.Config.Version || protocol.GT != p.Config.ID || protocol.Success != 1 || protocol.CaptchaToken != "" || protocol.Version == 0 && (!identifier.MatchString(protocol.Challenge) || !protocol.NewCaptcha) || protocol.Version == 1 && (protocol.Challenge != "" || protocol.NewCaptcha) {
		WriteError(w, ErrUnavailable)
		return
	}
	c := challenge{Token: s.sum("token", []byte(token)), Browser: s.sum("browser", []byte(value)), Account: s.sum("account", []byte(username)), Role: "merchant", Mode: "password", Purpose: "login", Config: p.Fingerprint, Protocol: protocol, Until: until}
	raw, _ := json.Marshal(protocol)
	if _, e = tx.Exec(ctx, `INSERT INTO login_captcha205_challenges(token_hash,browser_hash,account_hash,role,mode,purpose,config_hash,protocol,expires_at,proof_hash)VALUES($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10)`, c.Token, c.Browser, c.Account, c.Role, c.Mode, c.Purpose, c.Config, string(raw), c.Until, s.challengeSeal(c)); e != nil {
		WriteError(w, e)
		return
	}
	if e = alive(ctx, tx, until); e != nil {
		WriteError(w, e)
		return
	}
	if e = tx.Commit(ctx); e != nil {
		WriteError(w, e)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: value, Path: "/", HttpOnly: true, Secure: strings.HasPrefix(s.origin, "https://"), SameSite: http.SameSiteStrictMode, MaxAge: 600})
	protocol.CaptchaToken = token
	reply(w, 200, "", protocol)
}
