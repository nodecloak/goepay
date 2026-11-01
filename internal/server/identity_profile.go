package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"goepay/internal/channelconfig"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

const identityProfileCookie = "__Host-goepay_profile_identity"

func (s *Server) identityProfileOrigin() string {
	if s.IdentityProfileCallbackBaseURL != "" {
		return strings.TrimRight(s.IdentityProfileCallbackBaseURL, "/")
	}
	if s.ProviderFlow != nil {
		return strings.TrimRight(s.ProviderFlow.CallbackBaseURL, "/")
	}
	return ""
}
func (s *Server) identityProfilesUnavailable() string {
	if !s.IdentityProfileEnabled {
		return "independent identity profiles are disabled"
	}
	if s.Store == nil || s.Pool == nil || s.Sessions == nil || s.Sandbox || s.ProviderFlow != nil && s.ProviderFlow.LocalSimulation || !validOAuthOrigin(s.identityProfileOrigin()) {
		return "independent identity profiles are unavailable"
	}
	return ""
}
func (s *Server) identityProfileBinding(ctx context.Context, mid, pid int64) (channelconfig.IdentityProfile, payment.IdentityProfileBinding, bool) {
	resolved, e := s.managedWeixinRuntimeIDs(ctx, []int64{pid}, false)
	if e != nil {
		return channelconfig.IdentityProfile{}, payment.IdentityProfileBinding{}, false
	}
	return resolved.identityProfileBindingFromCatalog(ctx, mid, pid)
}

func (s *Server) identityProfileBindingFromCatalog(ctx context.Context, mid, pid int64) (channelconfig.IdentityProfile, payment.IdentityProfileBinding, bool) {
	var b payment.IdentityProfileBinding
	p, ok := s.IdentityProfiles[pid]
	if !ok || s.identityProfilesUnavailable() != "" || p.ID != pid || p.Revision <= 0 || !p.AllowsMerchant(mid) {
		return p, b, false
	}
	allowed := false
	for _, id := range s.MerchantChannels[mid] {
		if id == p.ChannelID {
			allowed = true
		}
	}
	if !allowed {
		return p, b, false
	}
	if p.Kind == "web" && (p.Web == nil || p.Web.ID() != p.Provider || p.Web.ApplicationID() != p.AppID) || p.Kind == "mini" && (p.Mini == nil || p.Mini.ID() != p.Provider || p.Mini.ApplicationID() != p.AppID) || p.Kind != "web" && p.Kind != "mini" {
		return p, b, false
	}
	var enabled bool
	e := s.Pool.QueryRow(ctx, `SELECT m.enabled AND NOT EXISTS(SELECT 1 FROM backoffice_records c WHERE c.resource='channels' AND c.id=$2 AND (c.body->>'enabled' IS DISTINCT FROM 'true' OR c.body->>'provider' IS DISTINCT FROM $3)) FROM merchants m WHERE m.id=$1`, mid, p.ChannelID, p.Provider).Scan(&enabled)
	if e != nil || !enabled {
		return p, b, false
	}
	b = payment.IdentityProfileBinding{MerchantID: mid, ChannelID: p.ChannelID, ProfileID: p.ID, ProfileRevision: p.Revision, Provider: p.Provider, AppID: p.AppID, Kind: p.Kind, Path: p.Path, Fingerprint: p.Fingerprint}
	return p, b, true
}
func (s *Server) failProfile(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = s.Store.FailProfileIdentityLookup(ctx, id)
}
func (s *Server) IdentityProfileConsole(w http.ResponseWriter, r *http.Request, role string, _ identity.Session, parts []string, token string) bool {
	if len(parts) < 2 || parts[1] != "identity-profile-lookups" {
		return false
	}
	identityHeaders(w)
	if s.Store == nil || s.Pool == nil || s.Sessions == nil {
		response(w, 503, -1, "independent identity service unavailable", nil)
		return true
	}
	cookie, hasCookie := identityCookie(r, "goepay_"+role)
	session, active := s.Sessions.Get(token, role)
	if !hasCookie || subtle.ConstantTimeCompare([]byte(cookie), []byte(token)) != 1 || !active || session.Issuer != nil || session.User.Role != role || role != "admin" && role != "merchant" {
		response(w, 401, -1, "original console session required", nil)
		return true
	}
	if r.Method != "GET" && (session.CSRF == "" || subtle.ConstantTimeCompare([]byte(session.CSRF), []byte(r.Header.Get("X-CSRF-Token"))) != 1) {
		response(w, 403, -1, "invalid CSRF token", nil)
		return true
	}
	if len(parts) == 3 && parts[2] == "capabilities" {
		s.profileCapabilities(w, r, role, session.User.ID, token)
		return true
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "query not accepted", nil)
		return true
	}
	if len(parts) == 2 {
		if r.Method != "POST" {
			response(w, 405, -1, "POST required", nil)
			return true
		}
		in, ok := identityJSON(w, r, "profile_id", "profile_revision", "merchant_id")
		if !ok {
			return true
		}
		mid := session.User.ID
		if role == "admin" || in["merchant_id"] != "" {
			mid, ok = identityPositiveID(in["merchant_id"])
			if !ok || role == "merchant" && mid != session.User.ID {
				response(w, 403, -1, "merchant unavailable", nil)
				return true
			}
		}
		pid, ok := identityPositiveID(in["profile_id"])
		rev, valid := identityPositiveID(in["profile_revision"])
		if !ok || !valid {
			response(w, 400, -1, "canonical profile and revision required", nil)
			return true
		}
		if e := s.Store.AuthorizeIdentityLookup(r.Context(), token, role, mid); e != nil {
			response(w, 403, -1, "merchant unavailable", nil)
			return true
		}
		p, b, ready := s.identityProfileBinding(r.Context(), mid, pid)
		if !ready {
			response(w, 503, -1, "selected independent profile unavailable", nil)
			return true
		}
		if rev != p.Revision {
			response(w, 409, -1, "profile changed; reload original capabilities", nil)
			return true
		}
		if !s.allowLogin(r.RemoteAddr, "identity-profile", fmt.Sprint(session.User.ID), time.Now()) {
			response(w, 429, -1, "identity creation rate limited", nil)
			return true
		}
		q, e := s.Store.CreateProfileIdentityLookup(r.Context(), token, role, b)
		if e != nil {
			response(w, 409, -1, "identity request unavailable", nil)
			return true
		}
		out := map[string]any{"request": q, "url_link_available": profileMiniLinkReady(p)}
		if q.Kind == "web" {
			out["authorization_url"] = s.identityProfileOrigin() + "/identity/profile/authorize/" + q.AuthorizationToken
		} else {
			out["launch"] = map[string]any{"kind": "sdk", "provider": q.Provider, "app_id": q.AppID, "path": q.Path, "method": "POST", "completion_url": s.identityProfileOrigin() + fmt.Sprintf("/identity/profile/mini/%s/%d/%s/", q.Provider, q.ProfileID, q.ID), "body": map[string]string{"state": q.State}, "expires_at": q.ExpiresAt}
		}
		response(w, 201, 0, "independent identity only; no payment", out)
		return true
	}
	if len(parts) != 3 && len(parts) != 4 || !identityHex(parts[2]) {
		response(w, 404, -1, "identity request unavailable", nil)
		return true
	}
	id := parts[2]
	q, e := s.Store.ProfileIdentityLookupMetadata(r.Context(), id)
	if e != nil {
		response(w, 404, -1, "identity request unavailable", nil)
		return true
	}
	p, b, _ := s.identityProfileBinding(r.Context(), q.MerchantID, q.ProfileID)
	// The owner check runs in core before a changed/missing profile can terminate
	// this request. Metadata never authorizes a response or a one-time reveal.
	owned, e := s.Store.ReadProfileIdentityLookup(r.Context(), id, token, role, b)
	if e != nil {
		response(w, 403, -1, "identity request unavailable or profile changed", nil)
		return true
	}
	if len(parts) == 3 {
		if r.Method != "GET" {
			response(w, 405, -1, "GET required", nil)
		} else {
			response(w, 200, 0, "", owned)
		}
		return true
	}
	if parts[3] == "url-link" {
		s.profileMiniLink(w, r, id, token, role, p, b)
		return true
	}
	if r.Method != "POST" {
		response(w, 405, -1, "POST required", nil)
		return true
	}
	switch parts[3] {
	case "reveal":
		if _, ok := identityJSON(w, r); !ok {
			return true
		}
		result, e := s.Store.RevealProfileIdentityLookup(r.Context(), id, token, role, b)
		if e != nil {
			response(w, 409, -1, "identity result unavailable or already revealed", nil)
			return true
		}
		label := "支付宝用户标识"
		if result.Provider == "wxpay" {
			label = "微信OpenID"
		}
		response(w, 200, 0, "one-time identity result", map[string]string{"subject_id": result.SubjectID, "label": label, "provider": result.Provider, "app_id": result.AppID})
	case "complete":
		if q.Kind != "mini" {
			response(w, 422, -1, "mini profile required", nil)
			return true
		}
		in, ok := identityJSON(w, r, "state", "code")
		if !ok {
			return true
		}
		s.exchangeProfileMini(w, r, q, in["state"], in["code"])
	default:
		response(w, 404, -1, "identity operation unavailable", nil)
	}
	return true
}
func (s *Server) profileCapabilities(w http.ResponseWriter, r *http.Request, role string, self int64, token string) {
	if r.Method != "GET" {
		response(w, 405, -1, "GET required", nil)
		return
	}
	query, e := strictOAuthQuery(r.URL.RawQuery)
	if e != nil || len(query) > 1 || len(query) == 1 && !query.Has("merchant_id") {
		response(w, 400, -1, "invalid capabilities query", nil)
		return
	}
	mid := self
	if role == "admin" || query.Has("merchant_id") {
		var ok bool
		mid, ok = identityPositiveID(query.Get("merchant_id"))
		if !ok || role == "merchant" && mid != self {
			response(w, 403, -1, "merchant unavailable", nil)
			return
		}
	}
	if s.Store.AuthorizeIdentityLookup(r.Context(), token, role, mid) != nil {
		response(w, 403, -1, "merchant unavailable", nil)
		return
	}
	ids := []int64{}
	resolved, resolveErr := s.managedWeixinRuntime(r.Context())
	if resolveErr != nil {
		response(w, 503, -1, "independent identity application catalog unavailable", nil)
		return
	}
	for id := range resolved.IdentityProfiles {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	profiles := []channelconfig.IdentityProfile{}
	reason := s.identityProfilesUnavailable()
	if reason == "" {
		for _, id := range ids {
			if p, _, ready := resolved.identityProfileBindingFromCatalog(r.Context(), mid, id); ready {
				profiles = append(profiles, p)
			}
		}
		if len(profiles) == 0 {
			reason = "no approved independent OAuth application is available for this merchant"
		}
	}
	linkAvailable := false
	for _, p := range profiles {
		linkAvailable = linkAvailable || profileMiniLinkReady(p)
	}
	response(w, 200, 0, "", map[string]any{"merchant_id": strconv.FormatInt(mid, 10), "available": len(profiles) > 0, "reason": reason, "profiles": profiles, "url_link_available": linkAvailable})
}
func (s *Server) exchangeProfileMini(w http.ResponseWriter, r *http.Request, q payment.ProfileIdentityLookup, state, code string) {
	p, b, _ := s.identityProfileBinding(r.Context(), q.MerchantID, q.ProfileID)
	claimed, e := s.Store.ConsumeProfileIdentityLookup(r.Context(), q.ID, state, "", code, b)
	if e != nil {
		response(w, 409, -1, "mini identity state unavailable or consumed", nil)
		return
	}
	p, b, ready := s.identityProfileBinding(r.Context(), claimed.MerchantID, claimed.ProfileID)
	if !ready || !s.Store.MatchesProfileIdentityLookup(claimed, b) {
		s.failProfile(claimed.ID)
		response(w, 503, -1, "original identity profile unavailable", nil)
		return
	}
	result, e := p.Mini.ExchangeMiniCode(r.Context(), code)
	if e != nil {
		s.failProfile(claimed.ID)
		response(w, 400, -1, "mini identity exchange failed", nil)
		return
	}
	_, b, ready = s.identityProfileBinding(r.Context(), claimed.MerchantID, claimed.ProfileID)
	if !ready || s.Store.CompleteProfileIdentityLookup(r.Context(), claimed.ID, result, b) != nil {
		s.failProfile(claimed.ID)
		response(w, 409, -1, "mini identity could not be stored", nil)
		return
	}
	response(w, 200, 0, "identity verified; return to the initiating console for one-time reveal", map[string]string{"request_id": claimed.ID, "status": "verified"})
}
func (s *Server) IdentityProfilePublic(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/identity/profile/") {
		return false
	}
	identityHeaders(w)
	reject := func(status int) { http.Error(w, "independent identity authorization could not be completed", status) }
	if s.Store == nil || s.Pool == nil || s.Sessions == nil {
		reject(503)
		return true
	}
	if r.URL.RawPath != "" {
		reject(400)
		return true
	}
	if strings.HasPrefix(r.URL.Path, "/identity/profile/authorize/") {
		authorization := strings.TrimPrefix(r.URL.Path, "/identity/profile/authorize/")
		if r.Method != "GET" || !identityHex(authorization) || r.URL.RawQuery != "" || r.URL.ForceQuery {
			reject(400)
			return true
		}
		q, e := s.Store.ProfileIdentityLookupByAuthorization(r.Context(), authorization)
		if e != nil {
			reject(409)
			return true
		}
		p, b, _ := s.identityProfileBinding(r.Context(), q.MerchantID, q.ProfileID)
		browser, present := identityCookie(r, identityProfileCookie)
		if !present {
			if browser != "" {
				reject(400)
				return true
			}
			nonce := make([]byte, 32)
			if _, e = rand.Read(nonce); e != nil {
				reject(503)
				return true
			}
			browser = hex.EncodeToString(nonce)
		} else if !identityHex(browser) {
			reject(400)
			return true
		}
		q, state, e := s.Store.StartProfileIdentityLookup(r.Context(), authorization, browser, b)
		if e != nil {
			reject(409)
			return true
		}
		callback := s.identityProfileOrigin() + fmt.Sprintf("/identity/profile/callback/%s/%d/%s/", q.Provider, q.ProfileID, q.ID)
		target, e := p.Web.OAuthAuthorizationURL(state, callback)
		scope := "auth_base"
		if q.Provider == "wxpay" {
			scope = "snsapi_base"
		}
		legacy := payment.IdentityLookup{IdentityLookupBinding: payment.IdentityLookupBinding{Provider: q.Provider, AppID: q.AppID, Scope: scope}}
		if e != nil || !identityAuthorizationURL(target, legacy, state, callback) {
			s.failProfile(q.ID)
			reject(503)
			return true
		}
		age := int(time.Until(q.ExpiresAt).Seconds())
		if age < 1 {
			reject(409)
			return true
		}
		http.SetCookie(w, &http.Cookie{Name: identityProfileCookie, Value: browser, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: age})
		http.Redirect(w, r, target, 302)
		return true
	}
	isMini := strings.HasPrefix(r.URL.Path, "/identity/profile/mini/")
	prefix := "/identity/profile/callback/"
	if isMini {
		prefix = "/identity/profile/mini/"
	}
	if !strings.HasPrefix(r.URL.Path, prefix) || !strings.HasSuffix(r.URL.Path, "/") {
		reject(404)
		return true
	}
	parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, prefix), "/"), "/")
	if len(parts) != 3 || !identityHex(parts[2]) {
		reject(400)
		return true
	}
	pid, ok := identityPositiveID(parts[1])
	if !ok {
		reject(400)
		return true
	}
	q, e := s.Store.ProfileIdentityLookupMetadata(r.Context(), parts[2])
	if e != nil || q.Provider != parts[0] || q.ProfileID != pid || isMini != (q.Kind == "mini") {
		reject(400)
		return true
	}
	if isMini {
		if r.Method != "POST" || r.URL.RawQuery != "" || r.URL.ForceQuery {
			reject(400)
			return true
		}
		in, ok := identityJSON(w, r, "state", "code")
		if !ok {
			return true
		}
		s.exchangeProfileMini(w, r, q, in["state"], in["code"])
		return true
	}
	if r.Method != "GET" {
		reject(405)
		return true
	}
	values, e := strictOAuthQuery(r.URL.RawQuery)
	allowed := map[string]bool{"state": true, "app_id": true, "scope": true}
	codeKey := "code"
	if q.Provider == "alipay" {
		codeKey = "auth_code"
	}
	allowed[codeKey] = true
	for key := range values {
		if !allowed[key] {
			reject(400)
			return true
		}
	}
	if e != nil {
		reject(400)
		return true
	}
	browser, present := identityCookie(r, identityProfileCookie)
	if !present || !identityHex(browser) {
		reject(400)
		return true
	}
	p, b, _ := s.identityProfileBinding(r.Context(), q.MerchantID, q.ProfileID)
	q, e = s.Store.ConsumeProfileIdentityLookup(r.Context(), q.ID, values.Get("state"), browser, values.Get(codeKey), b)
	if e != nil {
		reject(409)
		return true
	}
	scope := "auth_base"
	if q.Provider == "wxpay" {
		scope = "snsapi_base"
	}
	if q.Provider == "alipay" && values.Get("app_id") != q.AppID || values.Has("app_id") && values.Get("app_id") != q.AppID || values.Has("scope") && values.Get("scope") != scope {
		s.failProfile(q.ID)
		reject(400)
		return true
	}
	p, b, ready := s.identityProfileBinding(r.Context(), q.MerchantID, q.ProfileID)
	if !ready || !s.Store.MatchesProfileIdentityLookup(q, b) {
		s.failProfile(q.ID)
		reject(503)
		return true
	}
	result, e := p.Web.ExchangeOAuth(r.Context(), values.Get(codeKey))
	if e != nil {
		s.failProfile(q.ID)
		reject(400)
		return true
	}
	_, b, ready = s.identityProfileBinding(r.Context(), q.MerchantID, q.ProfileID)
	if !ready || s.Store.CompleteProfileIdentityLookup(r.Context(), q.ID, result, b) != nil {
		s.failProfile(q.ID)
		reject(409)
		return true
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, "授权完成，请返回原管理页面单次查看标识。")
	return true
}
