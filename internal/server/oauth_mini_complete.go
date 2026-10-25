package server

import (
	"bytes"
	"encoding/json"
	"goepay/internal/backoffice"
	"io"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
)

// The mini SDK posts only a code. A browser cookie has no authority here: the
// fixed provider endpoint proves this code's application and payer identity.
func miniCompletionBody(r *http.Request) (code, state string, err error) {
	kind, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || kind != "application/json" {
		return "", "", io.ErrUnexpectedEOF
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, 8193))
	if e != nil || len(raw) > 8192 {
		return "", "", io.ErrUnexpectedEOF
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	first, e := d.Token()
	if e != nil || first != json.Delim('{') {
		return "", "", io.ErrUnexpectedEOF
	}
	seen := map[string]bool{}
	for d.More() {
		key, e := d.Token()
		name, ok := key.(string)
		if e != nil || !ok || seen[name] || name != "code" && name != "state" {
			return "", "", io.ErrUnexpectedEOF
		}
		seen[name] = true
		var value string
		if d.Decode(&value) != nil {
			return "", "", io.ErrUnexpectedEOF
		}
		if name == "code" {
			code = value
		} else {
			state = value
		}
	}
	if _, e = d.Token(); e != nil {
		return "", "", io.ErrUnexpectedEOF
	}
	if _, e = d.Token(); e != io.EOF {
		return "", "", io.ErrUnexpectedEOF
	}
	if !seen["code"] || len(code) > 4096 || len(state) > 128 || seen["state"] && state == "" {
		return "", "", io.ErrUnexpectedEOF
	}
	return code, state, nil
}
func (s *Server) handleMiniOAuthComplete(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	reject := func(status int) { response(w, status, -1, "mini authorization could not be completed", nil) }
	if r.Method != "POST" {
		reject(405)
		return
	}
	if s.Store == nil || !strings.HasSuffix(r.URL.Path, "/") || r.URL.RawPath != "" {
		reject(400)
		return
	}
	parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/oauth/mini/complete/"), "/"), "/")
	if len(parts) != 3 {
		reject(400)
		return
	}
	channel, e := strconv.ParseInt(parts[1], 10, 64)
	if e != nil || channel <= 0 || strconv.FormatInt(channel, 10) != parts[1] {
		reject(400)
		return
	}
	b, e := s.Store.MiniPaymentIntent(r.Context(), parts[2])
	if e != nil || b.Provider != parts[0] || b.ChannelID != channel {
		reject(400)
		return
	}
	q, e := strictOAuthQuery(r.URL.RawQuery)
	if e != nil || len(q) != 1 || q.Get("state") == "" {
		reject(400)
		return
	}
	code, bodyState, e := miniCompletionBody(r)
	if e != nil || bodyState != "" && bodyState != q.Get("state") {
		reject(400)
		return
	}
	// Committed before all external I/O. Restoring a disabled channel or losing
	// an exchange response never makes this state/code eligible for resending.
	if e = s.Store.ConsumeMiniState(r.Context(), q.Get("state"), code, b); e != nil {
		reject(400)
		return
	}
	if _, ok := s.miniLaunchBindingReady(r.Context(), b); !ok {
		reject(503)
		return
	}
	identity, e := s.MiniOAuthProviders[channel].ExchangeMiniCode(r.Context(), code)
	if e != nil || identity.Provider != b.Provider || identity.AppID != b.AppID || identity.SubjectID == "" {
		reject(400)
		return
	}
	if _, ok := s.miniLaunchBindingReady(r.Context(), b); !ok {
		reject(503)
		return
	}
	o, e := s.Store.OrderByTradeNo(r.Context(), b.TradeNo)
	if e != nil {
		reject(503)
		return
	}
	var originalIP string
	if e = s.Pool.QueryRow(r.Context(), "SELECT client_ip FROM order_reporting WHERE order_id=$1", o.ID).Scan(&originalIP); e != nil || net.ParseIP(originalIP) == nil {
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
		if e = policy.CheckPayment(r.Context(), backoffice.PaymentContext{MerchantID: b.MerchantID, ClientIP: ip, Buyer: identity.SubjectID, NotifyURL: o.NotifyURL, ReturnURL: o.ReturnURL}); e != nil {
			reject(403)
			return
		}
	}
	if _, ok := s.miniLaunchBindingReady(r.Context(), b); !ok {
		reject(503)
		return
	}
	attempt, e := s.ProviderFlow.Submit(r.Context(), b.TradeNo, b.ChannelID, b.Method, originalIP, identity.SubjectID)
	if e != nil && attempt.ID == 0 {
		reject(503)
		return
	}
	response(w, 200, 0, "mini payer verified; payment requires provider confirmation", map[string]any{"trade_no": b.TradeNo, "checkout_url": s.checkoutURL(b.TradeNo, "checkout"), "attempt": attempt})
}
