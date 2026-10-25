package server

import (
	"io"
	"net/http"
	"strings"
)

// preauthorizationCheckout preserves the original route without exposing
// authorization material to someone who merely knows a platform trade number.
func (s *Server) preauthorizationCheckout(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "method not allowed", 405)
		return
	}
	trade := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/pay/preauth/"), "/")
	if trade == "" || strings.Contains(trade, "/") {
		http.NotFound(w, r)
		return
	}
	if !s.checkoutAuthorized(r, trade, "checkout") {
		http.Error(w, "invalid or expired checkout token", 403)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, "/checkout/"+trade+"?"+r.URL.RawQuery, http.StatusFound)
}
func (s *Server) preauthorizationCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fail := func(status int) { w.WriteHeader(status); io.WriteString(w, "fail") }
	if r.Method != "POST" {
		fail(http.StatusMethodNotAllowed)
		return
	}
	trade := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/pay/preauthnotify/"), "/")
	if trade == "" || strings.Contains(trade, "/") {
		fail(http.StatusNotFound)
		return
	}
	if s.ProviderFlow == nil || s.Store == nil {
		fail(http.StatusServiceUnavailable)
		return
	}
	attempt, e := s.Store.ProviderAttempt(r.Context(), trade)
	if e != nil || attempt.Method != "preauth" {
		fail(http.StatusNotFound)
		return
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, 1<<20+1))
	if e != nil || len(raw) > 1<<20 {
		fail(http.StatusBadRequest)
		return
	}
	if e = s.ProviderFlow.HandlePreauthorizationNotification(r.Context(), attempt.ChannelID, trade, r.Header, raw); e != nil {
		fail(http.StatusBadRequest)
		return
	}
	io.WriteString(w, "success")
}
