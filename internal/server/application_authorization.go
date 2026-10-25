package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	provider "goepay/internal/adapters/payment"
	"goepay/internal/channelconfig"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

const applicationAuthorizationCookie = "__Host-goepay_application_authorization"
const applicationAuthorizationPrefix = "/identity/application/"

func (s *Server) applicationAuthorizationOrigin() string {
	return strings.TrimRight(s.ApplicationAuthorizationCallbackBaseURL, "/")
}
func (s *Server) applicationAuthorizationUnavailable() string {
	if !s.ApplicationAuthorizationEnabled {
		return "application authorization is disabled"
	}
	if s.Store == nil || s.Pool == nil || s.Sessions == nil || s.Sandbox || s.ProviderFlow != nil && s.ProviderFlow.LocalSimulation || !validOAuthOrigin(s.applicationAuthorizationOrigin()) {
		return "application authorization is unavailable"
	}
	return ""
}
func (s *Server) applicationAuthorizationBinding(ctx context.Context, mid, pid int64) (channelconfig.ApplicationAuthorizationProfile, payment.ApplicationAuthorizationBinding, bool) {
	var b payment.ApplicationAuthorizationBinding
	p, ok := s.ApplicationAuthorizationProfiles[pid]
	if !ok || s.applicationAuthorizationUnavailable() != "" || p.ID != pid || p.Revision <= 0 || !p.AllowsMerchant(mid) || p.Adapter == nil || p.Adapter.ID() != "alipay" || p.Adapter.ApplicationID() != p.IssuerAppID {
		return p, b, false
	}
	var enabled bool
	if e := s.Pool.QueryRow(ctx, `SELECT enabled FROM merchants WHERE id=$1`, mid).Scan(&enabled); e != nil || !enabled {
		return p, b, false
	}
	b = payment.ApplicationAuthorizationBinding{MerchantID: mid, ProfileID: p.ID, ProfileRevision: p.Revision, IssuerAppID: p.IssuerAppID, ExpectedAuthAppID: p.ExpectedAuthAppID, LaunchKind: p.LaunchKind, AppTypes: p.AppTypes, Fingerprint: p.Fingerprint}
	return p, b, true
}
func (s *Server) failApplicationAuthorization(id string, b payment.ApplicationAuthorizationBinding) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = s.Store.FailApplicationAuthorization(ctx, id, b)
}

// This endpoint has its own purpose and privilege checks. Possessing an OAuth
// code or another administrator's request ID does not grant console ownership.
func (s *Server) applicationAuthorizationConsole(w http.ResponseWriter, r *http.Request, role string, _ identity.Session, parts []string, token string) bool {
	if len(parts) < 2 || parts[1] != "application-authorizations" {
		return false
	}
	identityHeaders(w)
	if s.Store == nil || s.Pool == nil || s.Sessions == nil {
		response(w, 503, -1, "application authorization unavailable", nil)
		return true
	}
	cookie, present := identityCookie(r, "goepay_"+role)
	session, active := s.Sessions.Get(token, role)
	var class string
	if role != "admin" || !present || subtle.ConstantTimeCompare([]byte(cookie), []byte(token)) != 1 || !active || session.Issuer != nil || session.User.Role != "admin" {
		response(w, 403, -1, "original super administrator session required", nil)
		return true
	}
	if e := s.Pool.QueryRow(r.Context(), `SELECT admin_class FROM console_users WHERE username=$1 AND role='admin' AND subject_id=$2 AND enabled AND auth_version=$3`, session.User.Username, session.User.ID, session.User.AuthVersion).Scan(&class); e != nil || class != "super" {
		response(w, 403, -1, "original super administrator session required", nil)
		return true
	}
	if r.Method != "GET" && (session.CSRF == "" || subtle.ConstantTimeCompare([]byte(session.CSRF), []byte(r.Header.Get("X-CSRF-Token"))) != 1) {
		response(w, 403, -1, "invalid CSRF token", nil)
		return true
	}
	if len(parts) == 3 && parts[2] == "capabilities" {
		s.applicationAuthorizationCapabilities(w, r, token)
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
		in, ok := identityJSON(w, r, "merchant_id", "profile_id", "profile_revision")
		if !ok {
			return true
		}
		mid, mok := identityPositiveID(in["merchant_id"])
		pid, pok := identityPositiveID(in["profile_id"])
		rev, rok := identityPositiveID(in["profile_revision"])
		if !mok || !pok || !rok {
			response(w, 400, -1, "canonical merchant, profile and revision required", nil)
			return true
		}
		if s.Store.AuthorizeIdentityLookup(r.Context(), token, role, mid) != nil {
			response(w, 403, -1, "merchant unavailable", nil)
			return true
		}
		p, b, ready := s.applicationAuthorizationBinding(r.Context(), mid, pid)
		if !ready {
			response(w, 503, -1, "approved application authorization unavailable", nil)
			return true
		}
		if rev != p.Revision {
			response(w, 409, -1, "profile changed; reload original capabilities", nil)
			return true
		}
		if !s.allowLogin(r.RemoteAddr, "application-authorization", fmt.Sprint(session.User.ID), time.Now()) {
			response(w, 429, -1, "application authorization rate limited", nil)
			return true
		}
		q, e := s.Store.CreateApplicationAuthorization(r.Context(), token, role, b)
		if e != nil {
			response(w, 409, -1, "application authorization request unavailable", nil)
			return true
		}
		out := map[string]any{"request": q, "authorization_url": s.applicationAuthorizationOrigin() + applicationAuthorizationPrefix + "authorize/" + q.AuthorizationToken}
		if q.LaunchKind == "assigned" {
			out["wallet_authorization_url"] = s.applicationAuthorizationOrigin() + applicationAuthorizationPrefix + "wallet/" + q.AuthorizationToken
		}
		response(w, 201, 0, "independent application authorization; no channel binding", out)
		return true
	}
	if len(parts) != 3 && len(parts) != 4 || !identityHex(parts[2]) {
		response(w, 404, -1, "application authorization unavailable", nil)
		return true
	}
	q, e := s.Store.ApplicationAuthorizationMetadata(r.Context(), parts[2])
	if e != nil {
		response(w, 404, -1, "application authorization unavailable", nil)
		return true
	}
	_, b, _ := s.applicationAuthorizationBinding(r.Context(), q.MerchantID, q.ProfileID)
	owned, e := s.Store.ReadApplicationAuthorization(r.Context(), q.ID, token, role, b)
	if e != nil {
		response(w, 403, -1, "original request unavailable or application changed", nil)
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
	if parts[3] != "reveal" {
		response(w, 404, -1, "application operation unavailable", nil)
		return true
	}
	if r.Method != "POST" {
		response(w, 405, -1, "POST required", nil)
		return true
	}
	if _, ok := identityJSON(w, r); !ok {
		return true
	}
	result, e := s.Store.RevealApplicationAuthorization(r.Context(), q.ID, token, role, b)
	if e != nil {
		response(w, 409, -1, "application credential unavailable or already revealed", nil)
		return true
	}
	response(w, 200, 0, "one-time application credential", map[string]any{"provider": result.Provider, "issuer_app_id": result.IssuerAppID, "auth_app_id": result.AuthAppID, "user_id": result.UserID, "app_auth_token": result.AppAuthToken, "token_expires_in_seconds": strconv.FormatInt(result.TokenExpiresInSeconds, 10)})
	return true
}
func (s *Server) applicationAuthorizationCapabilities(w http.ResponseWriter, r *http.Request, token string) {
	if r.Method != "GET" {
		response(w, 405, -1, "GET required", nil)
		return
	}
	query, e := strictOAuthQuery(r.URL.RawQuery)
	if e != nil || len(query) != 1 || !query.Has("merchant_id") {
		response(w, 400, -1, "explicit merchant_id required", nil)
		return
	}
	mid, ok := identityPositiveID(query.Get("merchant_id"))
	if !ok {
		response(w, 400, -1, "canonical merchant_id required", nil)
		return
	}
	if s.Store.AuthorizeIdentityLookup(r.Context(), token, "admin", mid) != nil {
		response(w, 403, -1, "merchant unavailable", nil)
		return
	}
	ids := make([]int64, 0, len(s.ApplicationAuthorizationProfiles))
	for id := range s.ApplicationAuthorizationProfiles {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	profiles := []channelconfig.ApplicationAuthorizationProfile{}
	reason := s.applicationAuthorizationUnavailable()
	if reason == "" {
		for _, id := range ids {
			if p, _, ready := s.applicationAuthorizationBinding(r.Context(), mid, id); ready {
				profiles = append(profiles, p)
			}
		}
		if len(profiles) == 0 {
			reason = "no approved application authorization profile is available for this merchant"
		}
	}
	origin := ""
	if len(profiles) > 0 {
		origin = s.applicationAuthorizationOrigin()
	}
	response(w, 200, 0, "", map[string]any{"merchant_id": strconv.FormatInt(mid, 10), "available": len(profiles) > 0, "reason": reason, "profiles": profiles, "authorization_origin": origin})
}

// Validate the complete SDK launch target, including the fixed wallet page.
// Neither a custom runtime implementation nor callback parameters can select a
// different origin, app, redirect, state, application type or wallet action.
func applicationAuthorizationTarget(target string, p channelconfig.ApplicationAuthorizationProfile, state, callback string, wallet bool) bool {
	if p.LaunchKind == "basic" {
		return !wallet && target == "https://openauth.alipay.com/oauth2/appToAppAuth.htm?"+url.Values{"app_id": {p.IssuerAppID}, "redirect_uri": {callback}, "state": {state}}.Encode()
	}
	if p.LaunchKind != "assigned" {
		return false
	}
	biz := struct {
		Platform string `json:"platformCode"`
		Task     string `json:"taskType"`
		Agent    struct {
			Redirect string `json:"redirectUri"`
			Types    any    `json:"appTypes"`
			App      string `json:"isvAppId"`
			State    string `json:"state"`
		} `json:"agentOpParam"`
	}{Platform: "O", Task: "INTERFACE_AUTH"}
	biz.Agent.Redirect, biz.Agent.Types, biz.Agent.App, biz.Agent.State = callback, p.AppTypes, p.IssuerAppID, state
	raw, e := json.Marshal(biz)
	if e != nil {
		return false
	}
	encoded := strings.ReplaceAll(url.QueryEscape(string(raw)), "+", "%20")
	if wallet {
		return target == "alipays://platformapi/startapp?appId=2021003130652097&page=pages%2Fauthorize%2Findex%3FbizData%3D"+encoded
	}
	return target == "https://b.alipay.com/page/message/tasksDetail?bizData="+encoded
}
func (s *Server) applicationAuthorizationPublic(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, applicationAuthorizationPrefix) {
		return false
	}
	identityHeaders(w)
	reject := func(status int) { http.Error(w, "application authorization could not be completed", status) }
	if s.applicationAuthorizationUnavailable() != "" {
		reject(503)
		return true
	}
	if r.URL.RawPath != "" {
		reject(400)
		return true
	}
	for _, kind := range []string{"authorize", "wallet"} {
		prefix := applicationAuthorizationPrefix + kind + "/"
		if !strings.HasPrefix(r.URL.Path, prefix) {
			continue
		}
		authorization := strings.TrimPrefix(r.URL.Path, prefix)
		if r.Method != "GET" || !identityHex(authorization) || r.URL.RawQuery != "" || r.URL.ForceQuery {
			reject(400)
			return true
		}
		q, e := s.Store.ApplicationAuthorizationByAuthorization(r.Context(), authorization)
		if e != nil {
			reject(409)
			return true
		}
		p, b, ready := s.applicationAuthorizationBinding(r.Context(), q.MerchantID, q.ProfileID)
		if !ready || kind == "wallet" && p.LaunchKind != "assigned" {
			reject(503)
			return true
		}
		browser, present := identityCookie(r, applicationAuthorizationCookie)
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
		q, state, e := s.Store.StartApplicationAuthorization(r.Context(), authorization, browser, b)
		if e != nil {
			reject(409)
			return true
		}
		callback := s.applicationAuthorizationOrigin() + fmt.Sprintf(applicationAuthorizationPrefix+"callback/%d/%s/", q.ProfileID, q.ID)
		var target string
		if q.LaunchKind == "basic" {
			target, e = p.Adapter.ApplicationAuthorizationURL(state, callback)
		} else {
			urls, err := p.Adapter.AssignedApplicationAuthorizationURLs(state, callback, p.AppTypes)
			e = err
			target = urls.PCURL
			if kind == "wallet" {
				target = urls.WalletURL
			}
		}
		if e != nil || !applicationAuthorizationTarget(target, p, state, callback, kind == "wallet") {
			s.failApplicationAuthorization(q.ID, b)
			reject(503)
			return true
		}
		age := int(time.Until(q.ExpiresAt).Seconds())
		if age < 1 {
			reject(409)
			return true
		}
		http.SetCookie(w, &http.Cookie{Name: applicationAuthorizationCookie, Value: browser, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: age})
		http.Redirect(w, r, target, http.StatusFound)
		return true
	}
	prefix := applicationAuthorizationPrefix + "callback/"
	if !strings.HasPrefix(r.URL.Path, prefix) || !strings.HasSuffix(r.URL.Path, "/") {
		reject(404)
		return true
	}
	if r.Method != "GET" {
		reject(405)
		return true
	}
	parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, prefix), "/"), "/")
	if len(parts) != 2 || !identityHex(parts[1]) {
		reject(400)
		return true
	}
	pid, ok := identityPositiveID(parts[0])
	if !ok {
		reject(400)
		return true
	}
	values, e := strictOAuthQuery(r.URL.RawQuery)
	if e != nil {
		reject(400)
		return true
	}
	for key := range values {
		if key != "state" && key != "app_id" && key != "app_auth_code" && key != "enctraceid" {
			reject(400)
			return true
		}
	}
	q, e := s.Store.ApplicationAuthorizationMetadata(r.Context(), parts[1])
	if e != nil || q.ProfileID != pid {
		reject(400)
		return true
	}
	if values.Get("app_id") != q.IssuerAppID {
		reject(400)
		return true
	}
	browser, present := identityCookie(r, applicationAuthorizationCookie)
	if !present || !identityHex(browser) {
		reject(400)
		return true
	}
	_, b, _ := s.applicationAuthorizationBinding(r.Context(), q.MerchantID, q.ProfileID)
	q, e = s.Store.ConsumeApplicationAuthorization(r.Context(), q.ID, values.Get("state"), browser, values.Get("app_auth_code"), b)
	if e != nil {
		reject(409)
		return true
	}
	p, b, ready := s.applicationAuthorizationBinding(r.Context(), q.MerchantID, q.ProfileID)
	if !ready || !s.Store.MatchesApplicationAuthorization(q, b) {
		s.failApplicationAuthorization(q.ID, b)
		reject(503)
		return true
	}
	result, e := p.Adapter.ExchangeApplicationAuthorization(r.Context(), provider.ApplicationAuthorizationExchange{Code: values.Get("app_auth_code"), ExpectedAuthAppID: q.ExpectedAuthAppID})
	if e != nil {
		s.failApplicationAuthorization(q.ID, b)
		reject(400)
		return true
	}
	_, b, ready = s.applicationAuthorizationBinding(r.Context(), q.MerchantID, q.ProfileID)
	if !ready || s.Store.CompleteApplicationAuthorization(r.Context(), q.ID, result, b) != nil {
		s.failApplicationAuthorization(q.ID, b)
		reject(409)
		return true
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, "应用授权完成，请返回原管理页面单次查看凭证。")
	return true
}
