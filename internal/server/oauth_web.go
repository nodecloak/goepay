package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	provider "goepay/internal/adapters/payment"
	"goepay/internal/backoffice"
	"goepay/internal/compat"
	"goepay/internal/payment"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const oauthBrowserCookie = "__Host-goepay_oauth"

func validOAuthOrigin(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && (u.Path == "" || u.Path == "/")
}
func (s *Server) oauthChannelReady(merchantID, channelID int64, method string) (provider.WebOAuthProvider, bool) {
	if s.Sandbox || s.Store == nil || s.Pool == nil || s.ProviderFlow == nil || s.ProviderFlow.LocalSimulation || !s.ProviderFlow.AllowLive || !validOAuthOrigin(s.ProviderFlow.CallbackBaseURL) || method != "jsapi" {
		return nil, false
	}
	allowed := false
	for _, id := range s.MerchantChannels[merchantID] {
		if id == channelID {
			allowed = true
		}
	}
	if !allowed {
		return nil, false
	}
	p := s.ProviderFlow.Providers[channelID]
	oauth := s.OAuthProviders[channelID]
	if p == nil || oauth == nil || p.ID() != oauth.ID() {
		return nil, false
	}
	identity, ok := p.(provider.ApplicationIdentity)
	if !ok || identity.ApplicationID() == "" || identity.ApplicationID() != oauth.ApplicationID() {
		return nil, false
	}
	supported := false
	for _, m := range p.Capabilities().Methods {
		if m == method {
			supported = true
		}
	}
	return oauth, supported
}
func (s *Server) oauthRouteAllowed(merchantID, channelID int64, method string, p compat.Params) bool {
	if p["oauth"] != "1" || p["sub_openid"] != "" || p["is_applet"] != "" && p["is_applet"] != "0" {
		return false
	}
	oauth, ok := s.oauthChannelReady(merchantID, channelID, method)
	return ok && (p["sub_appid"] == "" || p["sub_appid"] == oauth.ApplicationID())
}
func (s *Server) oauthBindingEnabled(ctx context.Context, b payment.OAuthBinding) bool {
	p, ok := s.oauthChannelReady(b.MerchantID, b.ChannelID, b.Method)
	if !ok || p.ID() != b.Provider || p.ApplicationID() != b.AppID {
		return false
	}
	var enabled bool
	e := s.Pool.QueryRow(ctx, "SELECT enabled FROM merchants WHERE id=$1", b.MerchantID).Scan(&enabled)
	return e == nil && enabled
}
func (s *Server) oauthCreate(w http.ResponseWriter, r *http.Request, legacy bool, merchantID int64, p compat.Params, order payment.Order, channelID int64, method string) bool {
	if p["oauth"] != "1" {
		return false
	}
	if !s.oauthRouteAllowed(merchantID, channelID, method, p) {
		s.wire(w, legacy, nil, "OAuth payment route unavailable")
		return true
	}
	oauth := s.OAuthProviders[channelID]
	b := payment.OAuthBinding{TradeNo: order.TradeNo, MerchantID: merchantID, ChannelID: channelID, Provider: order.Type, AppID: oauth.ApplicationID(), Method: method, Purpose: "payment-jsapi"}
	if e := s.Store.PrepareOAuthPayment(r.Context(), b); e != nil {
		s.wire(w, legacy, nil, "OAuth payment intent rejected")
		return true
	}
	link := s.checkoutURL(order.TradeNo, "checkout")
	if r.URL.Path == "/submit.php" || r.URL.Path == "/api/pay/submit" {
		http.Redirect(w, r, link, http.StatusFound)
		return true
	}
	out := map[string]any{"trade_no": order.TradeNo}
	if legacy {
		out["payurl"] = link
	} else {
		out["pay_type"] = "jump"
		out["pay_info"] = link
	}
	s.wire(w, legacy, out, "")
	return true
}
func oauthCookie(r *http.Request) (string, bool) {
	value := ""
	count := 0
	for _, cookie := range r.Cookies() {
		if cookie.Name == oauthBrowserCookie {
			value = cookie.Value
			count++
		}
	}
	if count != 1 || len(value) != 64 {
		return "", false
	}
	if _, e := hex.DecodeString(value); e != nil {
		return "", false
	}
	return value, true
}
func (s *Server) oauthCheckout(w http.ResponseWriter, r *http.Request, order payment.Order) bool {
	b, e := s.Store.OAuthPaymentIntent(r.Context(), order.TradeNo)
	if errors.Is(e, payment.ErrNotFound) {
		return false
	}
	if e != nil {
		http.Error(w, "authorization unavailable", 503)
		return true
	}
	if _, e = s.Store.ProviderAttempt(r.Context(), order.TradeNo); e == nil {
		return false
	} else if !errors.Is(e, payment.ErrNotFound) {
		http.Error(w, "authorization unavailable", 503)
		return true
	}
	checkoutPath := "/checkout/" + url.PathEscape(order.TradeNo)
	if r.URL.Path != checkoutPath {
		if r.URL.Path == checkoutPath+"/return" && s.checkoutAuthorized(r, order.TradeNo, "return") {
			http.Error(w, "payment authorization is incomplete", 202)
		} else {
			http.NotFound(w, r)
		}
		return true
	}
	if !s.checkoutAuthorized(r, order.TradeNo, "checkout") {
		http.Error(w, "invalid checkout token", 403)
		return true
	}
	if !s.oauthBindingEnabled(r.Context(), b) {
		http.Error(w, "authorization unavailable", 503)
		return true
	}
	browser, ok := oauthCookie(r)
	if !ok {
		nonce := make([]byte, 32)
		if _, e = rand.Read(nonce); e != nil {
			http.Error(w, "authorization unavailable", 503)
			return true
		}
		browser = hex.EncodeToString(nonce)
	}
	state, e := s.Store.IssueOAuthState(r.Context(), b, browser)
	if e != nil {
		http.Error(w, "authorization requires a fresh payment query", 409)
		return true
	}
	callback := strings.TrimRight(s.ProviderFlow.CallbackBaseURL, "/") + fmt.Sprintf("/oauth/callback/%s/%d/%s/", b.Provider, b.ChannelID, b.TradeNo)
	target, e := s.OAuthProviders[b.ChannelID].OAuthAuthorizationURL(state, callback)
	if e != nil {
		http.Error(w, "authorization unavailable", 503)
		return true
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.SetCookie(w, &http.Cookie{Name: oauthBrowserCookie, Value: browser, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 300})
	http.Redirect(w, r, target, http.StatusFound)
	return true
}
func strictOAuthQuery(raw string) (url.Values, error) {
	p, e := url.ParseQuery(raw)
	if e != nil {
		return nil, payment.ErrOAuthState
	}
	for k, vs := range p {
		if len(vs) != 1 || strings.ContainsAny(k, "[]\x00") {
			return nil, payment.ErrOAuthState
		}
	}
	return p, nil
}
func (s *Server) oauthCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	reject := func(status int) { http.Error(w, "authorization could not be completed", status) }
	if r.Method != "GET" {
		reject(405)
		return
	}
	parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/oauth/callback/"), "/"), "/")
	if len(parts) != 3 || s.Store == nil {
		reject(400)
		return
	}
	channelID, e := strconv.ParseInt(parts[1], 10, 64)
	if e != nil || channelID <= 0 || strconv.FormatInt(channelID, 10) != parts[1] {
		reject(400)
		return
	}
	b, e := s.Store.OAuthPaymentIntent(r.Context(), parts[2])
	if e != nil || b.Provider != parts[0] || b.ChannelID != channelID {
		reject(400)
		return
	}
	p, e := strictOAuthQuery(r.URL.RawQuery)
	if e != nil {
		reject(400)
		return
	}
	browser, ok := oauthCookie(r)
	if !ok {
		reject(400)
		return
	}
	code := p.Get("code")
	if b.Provider == "alipay" {
		if code != "" || p.Get("app_id") != b.AppID || p.Get("scope") != "" && p.Get("scope") != "auth_base" {
			reject(400)
			return
		}
		code = p.Get("auth_code")
	} else if p.Get("auth_code") != "" || p.Get("app_id") != "" && p.Get("app_id") != b.AppID || p.Get("scope") != "" && p.Get("scope") != "snsapi_base" {
		reject(400)
		return
	}
	// Consume before any token exchange. A disabled/reconfigured channel or a
	// failed exchange cannot revive this browser authorization after a restart.
	if e = s.Store.ConsumeOAuthState(r.Context(), p.Get("state"), browser, code, b); e != nil {
		reject(400)
		return
	}
	if !s.oauthBindingEnabled(r.Context(), b) {
		reject(503)
		return
	}
	identity, e := s.OAuthProviders[b.ChannelID].ExchangeOAuth(r.Context(), code)
	if e != nil || identity.Provider != b.Provider || identity.AppID != b.AppID || identity.SubjectID == "" {
		reject(400)
		return
	}
	if !s.oauthBindingEnabled(r.Context(), b) {
		reject(503)
		return
	}
	order, e := s.Store.OrderByTradeNo(r.Context(), b.TradeNo)
	if e != nil {
		reject(503)
		return
	}
	var originalIP string
	if e = s.Pool.QueryRow(r.Context(), "SELECT client_ip FROM order_reporting WHERE order_id=$1", order.ID).Scan(&originalIP); e != nil || net.ParseIP(originalIP) == nil {
		reject(503)
		return
	}
	actualIP, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		actualIP = r.RemoteAddr
	}
	if net.ParseIP(actualIP) == nil {
		reject(400)
		return
	}
	policy := backoffice.New(s.Pool, s.Store)
	for _, ip := range []string{originalIP, actualIP} {
		if e = policy.CheckPayment(r.Context(), backoffice.PaymentContext{MerchantID: b.MerchantID, ClientIP: ip, Buyer: identity.SubjectID, NotifyURL: order.NotifyURL, ReturnURL: order.ReturnURL}); e != nil {
			reject(403)
			return
		}
	}
	if !s.oauthBindingEnabled(r.Context(), b) {
		reject(503)
		return
	}
	attempt, e := s.ProviderFlow.Submit(r.Context(), b.TradeNo, b.ChannelID, b.Method, originalIP, identity.SubjectID)
	if e != nil && attempt.ID == 0 {
		reject(503)
		return
	}
	http.Redirect(w, r, s.checkoutURL(b.TradeNo, "checkout"), http.StatusFound)
}
