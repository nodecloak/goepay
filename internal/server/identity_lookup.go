package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	provider "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const identityBrowserCookie = "__Host-goepay_identity"

func identityHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func (s *Server) failIdentityLookup(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// The callback context may have been canceled by a transport timeout. State
	// and code are already burned; persist failure without reviving either one.
	_ = s.Store.FailIdentityLookup(ctx, id)
}
func identityPositiveID(raw string) (int64, bool) {
	id, e := strconv.ParseInt(raw, 10, 64)
	return id, e == nil && id > 0 && strconv.FormatInt(id, 10) == raw
}
func identityHex(raw string) bool {
	if len(raw) != 64 {
		return false
	}
	_, e := hex.DecodeString(raw)
	return e == nil && strings.ToLower(raw) == raw
}
func identityCookie(r *http.Request, name string) (string, bool) {
	var value string
	n := 0
	for _, c := range r.Cookies() {
		if c.Name == name {
			value = c.Value
			n++
		}
	}
	if n > 1 {
		return "duplicate-cookie", false
	}
	return value, n == 1
}

// Parse exact JSON objects, including duplicate-member rejection. Callers choose
// the entire allowlist; no callback redirect/subject/session fields are accepted.
func identityJSON(w http.ResponseWriter, r *http.Request, allowed ...string) (map[string]string, bool) {
	fail := func() (map[string]string, bool) {
		response(w, 400, -1, "invalid identity lookup JSON", nil)
		return nil, false
	}
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		return fail()
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	v, e := d.Token()
	if e != nil || v != json.Delim('{') {
		return fail()
	}
	out := map[string]string{}
	keys := map[string]bool{}
	for _, k := range allowed {
		keys[k] = true
	}
	for d.More() {
		v, e = d.Token()
		if e != nil {
			return fail()
		}
		key, ok := v.(string)
		if !ok || !keys[key] {
			return fail()
		}
		if _, exists := out[key]; exists {
			return fail()
		}
		var raw json.RawMessage
		var value string
		if e = d.Decode(&raw); e != nil || string(raw) == "null" || json.Unmarshal(raw, &value) != nil {
			return fail()
		}
		out[key] = value
	}
	if v, e = d.Token(); e != nil || v != json.Delim('}') {
		return fail()
	}
	if _, e = d.Token(); e != io.EOF {
		return fail()
	}
	return out, true
}
func (s *Server) identityUnavailable() string {
	if s.Sandbox {
		return "sandbox identity lookup is unavailable"
	}
	if s.Store == nil || s.Pool == nil || s.Sessions == nil || s.ProviderFlow == nil {
		return "identity lookup is not configured"
	}
	if s.ProviderFlow.LocalSimulation {
		return "local simulation identity lookup is unavailable"
	}
	if !s.ProviderFlow.AllowLive {
		return "web OAuth identity lookup is not enabled"
	}
	if !validOAuthOrigin(s.ProviderFlow.CallbackBaseURL) {
		return "HTTPS OAuth callback origin is required"
	}
	return ""
}
func (s *Server) identityLookupProvider(ctx context.Context, merchant, channel int64) (provider.WebOAuthProvider, payment.IdentityLookupBinding, bool) {
	var b payment.IdentityLookupBinding
	if s.identityUnavailable() != "" {
		return nil, b, false
	}
	oauth, ok := s.oauthChannelReady(merchant, channel, "jsapi")
	if !ok {
		return nil, b, false
	}
	p := s.ProviderFlow.Providers[channel]
	// Require a concrete, public provider-account dimension. An interface-only
	// test provider with weaker scope is not a supported account configuration.
	if _, ok = p.(provider.TransferAccountIdentity); !ok {
		return nil, b, false
	}
	account, e := provider.TransferAccountScope(p)
	if e != nil {
		return nil, b, false
	}
	scope := "auth_base"
	if oauth.ID() == "wxpay" {
		scope = "snsapi_base"
	} else if oauth.ID() != "alipay" {
		return nil, b, false
	}
	var enabled bool
	e = s.Pool.QueryRow(ctx, `SELECT m.enabled AND NOT EXISTS(SELECT 1 FROM backoffice_records c WHERE c.resource='channels' AND c.id=$2 AND (c.body->>'enabled' IS DISTINCT FROM 'true' OR c.body->>'provider' IS DISTINCT FROM $3)) FROM merchants m WHERE m.id=$1`, merchant, channel, oauth.ID()).Scan(&enabled)
	if e != nil || !enabled {
		return nil, b, false
	}
	b = payment.IdentityLookupBinding{MerchantID: merchant, ChannelID: channel, Provider: oauth.ID(), AppID: oauth.ApplicationID(), Scope: scope, AccountScope: account}
	return oauth, b, true
}
func (s *Server) identityLookupReady(ctx context.Context, q payment.IdentityLookup) (provider.WebOAuthProvider, bool) {
	p, b, ok := s.identityLookupProvider(ctx, q.MerchantID, q.ChannelID)
	return p, ok && b.Provider == q.Provider && b.AppID == q.AppID && b.Scope == q.Scope && s.Store.IdentityLookupAccountMatches(q, b.AccountScope)
}

// IdentityLookupConsole is called after shared console middleware. It repeats
// original-session and CSRF checks so direct hook wiring cannot bypass them.
func (s *Server) IdentityLookupConsole(w http.ResponseWriter, r *http.Request, role string, _ identity.Session, parts []string, sessionToken string) bool {
	if len(parts) < 2 || parts[1] != "identity-lookups" {
		return false
	}
	identityHeaders(w)
	if parts[0] != role {
		response(w, 403, -1, "console role mismatch", nil)
		return true
	}
	if s.Store == nil || s.Pool == nil || s.Sessions == nil {
		response(w, 503, -1, "identity lookup is not configured", nil)
		return true
	}
	cookie, ok := identityCookie(r, "goepay_"+role)
	session, active := s.Sessions.Get(sessionToken, role)
	if !ok || subtle.ConstantTimeCompare([]byte(cookie), []byte(sessionToken)) != 1 || !active || session.Issuer != nil || session.User.Role != role || role != "merchant" && role != "admin" {
		response(w, 401, -1, "original console session required", nil)
		return true
	}
	if r.Method != "GET" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(session.CSRF)) != 1 {
		response(w, 403, -1, "invalid CSRF token", nil)
		return true
	}
	if len(parts) == 3 && parts[2] == "capabilities" {
		s.identityLookupCapabilities(w, r, role, session.User.ID, sessionToken)
		return true
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "identity lookup query is not allowed", nil)
		return true
	}
	if len(parts) == 2 {
		if r.Method != "POST" {
			response(w, 405, -1, "method not allowed", nil)
			return true
		}
		in, ok := identityJSON(w, r, "channel_id", "merchant_id")
		if !ok {
			return true
		}
		merchant := session.User.ID
		if role == "admin" || in["merchant_id"] != "" {
			var valid bool
			merchant, valid = identityPositiveID(in["merchant_id"])
			if !valid || role == "merchant" && merchant != session.User.ID {
				response(w, 403, -1, "merchant is not available", nil)
				return true
			}
		}
		channel, valid := identityPositiveID(in["channel_id"])
		if !valid {
			response(w, 400, -1, "invalid channel", nil)
			return true
		}
		if e := s.Store.AuthorizeIdentityLookup(r.Context(), sessionToken, role, merchant); e != nil {
			response(w, 403, -1, "merchant is not available", nil)
			return true
		}
		_, b, ready := s.identityLookupProvider(r.Context(), merchant, channel)
		if !ready {
			response(w, 503, -1, "selected web OAuth identity route is unavailable", nil)
			return true
		}
		q, e := s.Store.CreateIdentityLookup(r.Context(), sessionToken, role, b)
		if e != nil {
			response(w, 409, -1, "identity request could not be created", nil)
			return true
		}
		response(w, 201, 0, "", map[string]any{"request_id": q.ID, "merchant_id": strconv.FormatInt(q.MerchantID, 10), "channel_id": strconv.FormatInt(q.ChannelID, 10), "provider": q.Provider, "app_id": q.AppID, "scope": q.Scope, "status": q.Status, "expires_at": q.ExpiresAt, "authorization_url": strings.TrimRight(s.ProviderFlow.CallbackBaseURL, "/") + "/identity/authorize/" + q.AuthorizationToken})
		return true
	}
	if len(parts) != 3 && len(parts) != 4 || !identityHex(parts[2]) || len(parts) == 4 && parts[3] != "reveal" {
		response(w, 404, -1, "identity request not found", nil)
		return true
	}
	if len(parts) == 3 && r.Method != "GET" || len(parts) == 4 && r.Method != "POST" {
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	if len(parts) == 4 {
		if _, ok := identityJSON(w, r); !ok {
			return true
		}
	}
	q, e := s.Store.ReadIdentityLookup(r.Context(), parts[2], sessionToken, role)
	if e != nil {
		response(w, 404, -1, "identity request is unavailable or expired", nil)
		return true
	}
	if _, ready := s.identityLookupReady(r.Context(), q); !ready {
		response(w, 503, -1, "identity route is no longer available", nil)
		return true
	}
	if len(parts) == 3 {
		response(w, 200, 0, "", q)
		return true
	}
	result, e := s.Store.RevealIdentityLookup(r.Context(), q.ID, sessionToken, role)
	if e != nil {
		response(w, 409, -1, "identity result is unavailable or already revealed", nil)
		return true
	}
	label := "支付宝用户标识"
	if result.Provider == "wxpay" {
		label = "微信 OpenID"
	}
	response(w, 200, 0, "", map[string]string{"subject_id": result.SubjectID, "provider": result.Provider, "app_id": result.AppID, "label": label})
	return true
}
func (s *Server) identityLookupCapabilities(w http.ResponseWriter, r *http.Request, role string, self int64, token string) {
	if r.Method != "GET" {
		response(w, 405, -1, "method not allowed", nil)
		return
	}
	query, e := strictOAuthQuery(r.URL.RawQuery)
	if e != nil || len(query) > 1 || len(query) == 1 && !query.Has("merchant_id") {
		response(w, 400, -1, "invalid capabilities query", nil)
		return
	}
	merchant := self
	if role == "admin" || query.Has("merchant_id") {
		var ok bool
		merchant, ok = identityPositiveID(query.Get("merchant_id"))
		if !ok || role == "merchant" && merchant != self {
			response(w, 403, -1, "merchant is not available", nil)
			return
		}
	}
	if e = s.Store.AuthorizeIdentityLookup(r.Context(), token, role, merchant); e != nil {
		response(w, 403, -1, "merchant is not available", nil)
		return
	}
	channels := []map[string]string{}
	reason := s.identityUnavailable()
	if reason == "" {
		ids := append([]int64{}, s.MerchantChannels[merchant]...)
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		seen := map[int64]bool{}
		for _, id := range ids {
			if seen[id] {
				continue
			}
			seen[id] = true
			if p, _, ok := s.identityLookupProvider(r.Context(), merchant, id); ok {
				channels = append(channels, map[string]string{"channel_id": strconv.FormatInt(id, 10), "provider": p.ID(), "app_id": p.ApplicationID()})
			}
		}
		if len(channels) == 0 {
			reason = "no enabled standard web OAuth channel is configured for this merchant"
		}
	}
	response(w, 200, 0, "", map[string]any{"merchant_id": strconv.FormatInt(merchant, 10), "available": len(channels) > 0, "reason": reason, "channels": channels})
}

func identityAuthorizationURL(raw string, q payment.IdentityLookup, state, callback string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" {
		return false
	}
	query, e := strictOAuthQuery(u.RawQuery)
	if e != nil {
		return false
	}
	if query.Get("state") != state || query.Get("redirect_uri") != callback || query.Get("scope") != q.Scope {
		return false
	}
	if q.Provider == "wxpay" {
		return u.Host == "open.weixin.qq.com" && u.Path == "/connect/oauth2/authorize" && u.Fragment == "wechat_redirect" && len(query) == 5 && query.Get("appid") == q.AppID && query.Get("response_type") == "code"
	}
	return q.Provider == "alipay" && u.Host == "openauth.alipay.com" && u.Path == "/oauth2/publicAppAuthorize.htm" && u.Fragment == "" && len(query) == 4 && query.Get("app_id") == q.AppID
}

// IdentityLookupPublic handles authorization links/callbacks only. It never
// loads a console session from a caller parameter and never submits a payment.
func (s *Server) IdentityLookupPublic(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/identity/") {
		return false
	}
	identityHeaders(w)
	reject := func(status int) { http.Error(w, "identity authorization could not be completed", status) }
	if r.Method != "GET" {
		reject(405)
		return true
	}
	if s.Store == nil || s.Pool == nil {
		reject(503)
		return true
	}
	if strings.HasPrefix(r.URL.Path, "/identity/authorize/") {
		if s.identityUnavailable() != "" {
			reject(503)
			return true
		}
		token := strings.TrimPrefix(r.URL.Path, "/identity/authorize/")
		if !identityHex(token) || r.URL.RawQuery != "" || r.URL.ForceQuery {
			reject(400)
			return true
		}
		browser, ok := identityCookie(r, identityBrowserCookie)
		if !ok {
			if browser != "" {
				reject(400)
				return true
			}
			nonce := make([]byte, 32)
			if _, e := rand.Read(nonce); e != nil {
				reject(503)
				return true
			}
			browser = hex.EncodeToString(nonce)
		} else if !identityHex(browser) {
			reject(400)
			return true
		}
		q, state, e := s.Store.StartIdentityLookup(r.Context(), token, browser)
		if e != nil {
			reject(409)
			return true
		}
		p, ready := s.identityLookupReady(r.Context(), q)
		if !ready {
			s.failIdentityLookup(q.ID)
			reject(503)
			return true
		}
		callback := strings.TrimRight(s.ProviderFlow.CallbackBaseURL, "/") + "/identity/callback/" + q.Provider + "/" + strconv.FormatInt(q.ChannelID, 10) + "/" + q.ID + "/"
		target, e := p.OAuthAuthorizationURL(state, callback)
		if e != nil || !identityAuthorizationURL(target, q, state, callback) {
			s.failIdentityLookup(q.ID)
			reject(503)
			return true
		}
		age := int(time.Until(q.ExpiresAt).Seconds())
		if age < 1 {
			reject(409)
			return true
		}
		http.SetCookie(w, &http.Cookie{Name: identityBrowserCookie, Value: browser, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: age})
		http.Redirect(w, r, target, http.StatusFound)
		return true
	}
	if !strings.HasPrefix(r.URL.Path, "/identity/callback/") || !strings.HasSuffix(r.URL.Path, "/") {
		reject(404)
		return true
	}
	parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/identity/callback/"), "/"), "/")
	if len(parts) != 3 || !identityHex(parts[2]) || parts[0] != "wxpay" && parts[0] != "alipay" {
		reject(400)
		return true
	}
	channel, ok := identityPositiveID(parts[1])
	if !ok {
		reject(400)
		return true
	}
	values, e := strictOAuthQuery(r.URL.RawQuery)
	if e != nil {
		reject(400)
		return true
	}
	allowed := map[string]bool{"state": true, "app_id": true, "scope": true}
	codeKey := "code"
	if parts[0] == "alipay" {
		codeKey = "auth_code"
	}
	allowed[codeKey] = true
	for key := range values {
		if !allowed[key] {
			reject(400)
			return true
		}
	}
	browser, ok := identityCookie(r, identityBrowserCookie)
	if !ok || !identityHex(browser) {
		reject(400)
		return true
	}
	q, e := s.Store.ConsumeIdentityLookup(r.Context(), parts[2], parts[0], channel, values.Get("state"), browser, values.Get(codeKey))
	if e != nil {
		reject(400)
		return true
	}
	fail := func(status int) { s.failIdentityLookup(q.ID); reject(status) }
	if q.Provider == "alipay" && values.Get("app_id") != q.AppID || values.Has("app_id") && values.Get("app_id") != q.AppID || values.Has("scope") && values.Get("scope") != q.Scope {
		fail(400)
		return true
	}
	p, ready := s.identityLookupReady(r.Context(), q)
	if !ready {
		fail(503)
		return true
	}
	result, e := p.ExchangeOAuth(r.Context(), values.Get(codeKey))
	if e != nil || result.Provider != q.Provider || result.AppID != q.AppID {
		fail(400)
		return true
	}
	if _, ready = s.identityLookupReady(r.Context(), q); !ready {
		fail(503)
		return true
	}
	if e = s.Store.CompleteIdentityLookup(r.Context(), q.ID, result); e != nil {
		fail(409)
		return true
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(200)
	io.WriteString(w, "授权完成，请返回发起查询的管理页面。")
	return true
}
