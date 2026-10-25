package server

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"goepay/internal/backoffice"
	"goepay/internal/collectionstyle"
	"goepay/internal/compat"
	"goepay/internal/identity"
	"goepay/internal/ledger"
	"goepay/internal/pagetemplate"
	"goepay/internal/payment"
	"html"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type collectionRecord struct {
	Generation                              *payment.CollectionGeneration
	Owner                                   int64
	Merchant, Title, Background, Foreground string
	BackgroundOverride, ForegroundOverride  bool
	Layout                                  *collectionstyle.Layout
}

func (s *Server) collectionRecord(r *http.Request, id string) (collectionRecord, error) {
	var c collectionRecord
	var raw []byte
	var generated bool
	err := s.Pool.QueryRow(r.Context(), "SELECT c.owner_id,m.name,c.body,(c.body->>'generated'='true') IS TRUE OR EXISTS(SELECT 1 FROM collection_code_generations g WHERE g.code_id=c.id) OR EXISTS(SELECT 1 FROM backoffice_audit a WHERE a.resource='collection-generator' AND a.operation='generate' AND a.resource_id=c.id) FROM backoffice_records c JOIN merchants m ON m.id=c.owner_id AND m.enabled WHERE c.id=$1::bigint AND c.resource='collection-codes' AND c.body->>'enabled'='true' AND c.body->>'archived' IS DISTINCT FROM 'true'", id).Scan(&c.Owner, &c.Merchant, &raw, &generated)
	if err != nil {
		return c, err
	}
	var fields map[string]any
	if err = json.Unmarshal(raw, &fields); err != nil {
		return c, err
	}
	c.Title, _ = fields["title"].(string)
	if generated {
		number, _ := strconv.ParseInt(id, 10, 64)
		g, e := s.Store.CollectionGeneration(r.Context(), number)
		if e != nil || !g.Enabled {
			return c, payment.ErrConflict
		}
		c.Generation = &g
		c.Merchant = g.RecipientName
	}
	c.Background = "#f4f8fa"
	c.Foreground = "#163047"
	if style, ok := fields["style_id"].(string); ok && style != "" {
		var styleRaw []byte
		if s.Pool.QueryRow(r.Context(), "SELECT body FROM backoffice_records WHERE id=$1::bigint AND resource='collection-styles' AND body->>'enabled'='true'", style).Scan(&styleRaw) == nil {
			var data map[string]any
			if json.Unmarshal(styleRaw, &data) == nil {
				var layoutErr error
				c.Layout, layoutErr = collectionstyle.Parse(data)
				if layoutErr != nil {
					return c, layoutErr
				}
				for key, dest := range map[string]*string{"background_color": &c.Background, "text_color": &c.Foreground} {
					if color, ok := data[key].(string); ok && validCollectionColor(color) {
						*dest = color
						if key == "background_color" {
							c.BackgroundOverride = true
						} else {
							c.ForegroundOverride = true
						}
					}
				}
			}
		}
	}
	return c, nil
}
func validCollectionColor(raw string) bool {
	if len(raw) != 7 || raw[0] != '#' {
		return false
	}
	for _, c := range raw[1:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}
func (s *Server) collectionLink(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) < 2 || parts[1] != "collection-codes" {
		return false
	}
	if r.Method != "GET" || len(parts) != 4 || parts[3] != "link" {
		response(w, 404, -1, "collection action not found", nil)
		return true
	}
	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || id < 1 {
		response(w, 400, -1, "invalid collection ID", nil)
		return true
	}
	c, err := s.collectionRecord(r, parts[2])
	if err != nil || (role != "admin" && c.Owner != session.User.ID) {
		response(w, 404, -1, "collection code not found", nil)
		return true
	}
	if s.ProviderFlow == nil || len(s.CheckoutKey) < 32 {
		response(w, 503, -1, "collection provider unavailable", nil)
		return true
	}
	if c.Generation != nil {
		response(w, 200, 0, "", map[string]any{"url": s.generatedCollectionURL(*c.Generation), "expires_at": nil, "layout": c.Layout, "merchant_name": c.Merchant})
		return true
	}
	expires := time.Now().Add(2 * time.Hour)
	expiration := strconv.FormatInt(expires.Unix(), 10)
	values := url.Values{"expires": {expiration}, "token": {s.checkoutToken(parts[2], "collect", expiration)}}
	address := strings.TrimRight(s.ProviderFlow.CallbackBaseURL, "/") + "/collect/" + parts[2] + "?" + values.Encode()
	response(w, 200, 0, "collection link is valid for two hours; regenerate after expiry", map[string]any{"url": address, "expires_at": expires.UTC().Format(time.RFC3339), "layout": c.Layout, "merchant_name": c.Merchant})
	return true
}
func (s *Server) collect(w http.ResponseWriter, r *http.Request) {
	if s.Pool == nil || s.Store == nil || s.ProviderFlow == nil {
		http.Error(w, "collection unavailable", 503)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/collect/")
	number, err := strconv.ParseInt(id, 10, 64)
	if err != nil || number < 1 || strconv.FormatInt(number, 10) != id {
		http.Error(w, "invalid collection link", 403)
		return
	}
	c, err := s.collectionRecord(r, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if c.Generation != nil {
		if !s.collectionGenerationAuthorized(r, id, c.Generation.ConfigHash) {
			http.Error(w, "invalid collection link", 403)
			return
		}
	} else if r.URL.Query().Get("mode") != "" || !s.checkoutAuthorized(r, id, "collect") {
		http.Error(w, "invalid collection link", 403)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; style-src 'unsafe-inline'; img-src 'self'; form-action 'self'")
	if r.Method == "POST" {
		if !s.ProviderFlow.AllowLive {
			http.Error(w, "payment writes disabled", 503)
			return
		}
		if !s.allowCollectionAttempt(r.RemoteAddr, id, time.Now()) {
			http.Error(w, "too many payment attempts", 429)
			return
		}
		if err = r.ParseForm(); err != nil {
			http.Error(w, "invalid form", 400)
			return
		}
		for _, field := range []string{"amount", "type", "external_no"} {
			if len(r.PostForm[field]) != 1 {
				http.Error(w, "invalid form fields", 400)
				return
			}
		}
		amount, err := ledger.Parse(r.PostForm.Get("amount"))
		intent := r.PostForm.Get("external_no")
		if err != nil || amount < 1 || amount > 100000000 || len(intent) < 16 || len(intent) > 64 {
			http.Error(w, "invalid amount or intent", 400)
			return
		}
		configHash := ""
		if c.Generation != nil {
			configHash = c.Generation.ConfigHash
			if c.Generation.FixedAmount > 0 && amount != c.Generation.FixedAmount {
				http.Error(w, "fixed collection amount mismatch", 422)
				return
			}
		}
		for _, ch := range intent {
			if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_') {
				http.Error(w, "invalid intent", 400)
				return
			}
		}
		typ := r.PostForm.Get("type")
		ip, _, splitErr := net.SplitHostPort(r.RemoteAddr)
		if splitErr != nil {
			ip = r.RemoteAddr
		}
		if err = backoffice.New(s.Pool, s.Store).CheckPayment(r.Context(), backoffice.PaymentContext{MerchantID: c.Owner, ClientIP: ip}); err != nil {
			http.Error(w, "payment policy rejected", 403)
			return
		}
		params := compat.Params{"type": typ, "out_trade_no": "collect:" + id + ":" + intent, "name": c.Title, "money": ledger.Format(amount), "clientip": ip}
		channel, method, err := s.providerRoutedBrowserRequest(r.Context(), c.Owner, params)
		if err != nil {
			http.Error(w, "payment type unavailable", 422)
			return
		}
		cost, err := s.ProviderFlow.PaymentCostQuote(channel, method)
		if err != nil {
			http.Error(w, "payment cost configuration unavailable", 409)
			return
		}
		order, err := s.Store.CreateCollectionOrder(r.Context(), number, payment.CreateOrderInput{CollectionConfigHash: configHash, PaymentCost: cost, MerchantID: c.Owner, OutTradeNo: "collect:" + id + ":" + intent, Name: c.Title, Type: typ, Amount: amount})
		if err != nil {
			http.Error(w, "collection order rejected", 409)
			return
		}
		if err = s.Store.SetOrderClientIP(r.Context(), order.ID, ip); err != nil {
			http.Error(w, "order metadata unavailable", 409)
			return
		}
		attempt, err := s.ProviderFlow.Submit(r.Context(), order.TradeNo, channel, method, ip, "")
		if err != nil || attempt.State != "pending" {
			http.Error(w, "payment outcome requires query", 409)
			return
		}
		http.Redirect(w, r, s.checkoutURL(order.TradeNo, "checkout"), http.StatusFound)
		return
	}
	if r.Method != "GET" {
		http.Error(w, "method not allowed", 405)
		return
	}
	nonce := make([]byte, 18)
	if _, err = rand.Read(nonce); err != nil {
		http.Error(w, "collection unavailable", 503)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	colors := &templateColors{}
	if c.BackgroundOverride {
		colors.Background = c.Background
	}
	if c.ForegroundOverride {
		colors.Foreground = c.Foreground
	}
	if !s.pageTemplateOpen(w, r, "qrpay", pagetemplate.Page{Title: c.Title, MerchantName: c.Merchant, Sandbox: s.Sandbox}, colors) {
		return
	}
	fmt.Fprintf(w, "<p>收款说明：%s</p>", html.EscapeString(c.Title))
	fixed := ""
	if c.Generation != nil && c.Generation.FixedAmount > 0 {
		fixed = fmt.Sprintf(` value="%s" readonly`, ledger.Format(c.Generation.FixedAmount))
	}
	fmt.Fprintf(w, `<form method="post" action="%s"><input type="hidden" name="external_no" value="%s"><label>金额（元）<input name="amount" type="number" min="0.01" max="1000000" step="0.01" required%s></label><label>支付方式<select name="type"><option value="wxpay">微信支付</option><option value="alipay">支付宝</option></select></label><button type="submit">前往支付</button></form></main></html>`, html.EscapeString(r.URL.RequestURI()), base64.RawURLEncoding.EncodeToString(nonce), fixed)
}
