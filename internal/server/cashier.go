package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"goepay/internal/backoffice"
	"goepay/internal/pagetemplate"
)

// Cashier (收银台) — the original empty-type / pay_force_cashier_pids page flow
// (epay_release includes/lib/api/Pay.php:160-169, :425-435, cashier.php). GOEPAY
// never mutates an order's type after creation, so the empty-type page submit
// does NOT create an order: it runs every normal check (signature, merchant,
// notify/return, amount limits, blockname, forceqq, blacklist/domain) first,
// then reserves the request as a signed CASHIER INTENT (tamper-evident, with an
// expiry) and renders a page listing only the pay types this merchant can
// really use. The payer's choice runs the identical provider-routed create with
// that type exactly once; a repeat click resumes the same order (create is
// idempotent on out_trade_no).

// cashierIntent is the verified request carried by the signed cashier token. It
// is exactly the fields a submit with a type would have used; no order exists
// until the payer chooses.
type cashierIntent struct {
	// Version belongs to the authenticated entry point, independently of the
	// browser rendering mode or request signature algorithm. Omitted v is an
	// already issued pre-217 token and retains its original V1 behavior.
	Version    string `json:"v,omitempty"`
	MerchantID int64  `json:"m"`
	OutTradeNo string `json:"o"`
	Name       string `json:"n"`
	Money      string `json:"a"`
	NotifyURL  string `json:"nu"`
	ReturnURL  string `json:"ru"`
	Param      string `json:"p"`
	Buyer      string `json:"b"`
	ClientIP   string `json:"ip"`
	Expires    int64  `json:"e"`
}

// cashierToken signs an intent so a tampered or expired one is rejected without
// any server-side state, using the same CheckoutKey as the checkout tokens.
func (s *Server) cashierToken(raw []byte) string {
	mac := hmac.New(sha256.New, s.CheckoutKey)
	mac.Write([]byte("goepay-cashier-v1\n"))
	mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// cashierURL packs the verified request into a signed token and returns the
// GOEPAY cashier page URL.
func (s *Server) cashierURL(in cashierIntent) string {
	raw, _ := json.Marshal(in)
	payload := base64.RawURLEncoding.EncodeToString(raw)
	q := url.Values{"i": {payload}, "t": {s.cashierToken(raw)}}
	base := ""
	if s.ProviderFlow != nil {
		base = strings.TrimRight(s.ProviderFlow.CallbackBaseURL, "/")
	}
	return base + "/cashier?" + q.Encode()
}

// decodeCashierIntent verifies the token and expiry and returns the intent.
func (s *Server) decodeCashierIntent(r *http.Request) (cashierIntent, bool) {
	var in cashierIntent
	if len(s.CheckoutKey) < 32 {
		return in, false
	}
	q := r.URL.Query()
	payload, token := q.Get("i"), q.Get("t")
	if payload == "" || token == "" {
		return in, false
	}
	raw, e := base64.RawURLEncoding.DecodeString(payload)
	if e != nil {
		return in, false
	}
	if !hmac.Equal([]byte(s.cashierToken(raw)), []byte(token)) {
		return in, false
	}
	if json.Unmarshal(raw, &in) != nil || in.MerchantID <= 0 || in.OutTradeNo == "" {
		return in, false
	}
	if in.Version == "" {
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			return in, false
		}
		if _, explicit := fields["v"]; explicit {
			return in, false
		}
		in.Version = "v1"
	} else if in.Version != "v1" && in.Version != "v2" {
		return in, false
	}
	if in.Expires < time.Now().Unix() {
		return in, false
	}
	return in, true
}

// cashierPayType is one selectable pay type on the cashier page.
type cashierPayType struct{ Name, ShowName string }

// cashierAllowedTypes lists the pay types this merchant can really use now:
// the catalog type is open AND at least one of the merchant's channels has a
// provider that serves it. Mirrors the original Channel::getTypes filter so the
// page offers only usable rails; the real create still enforces full routing on
// the payer's choice.
func (s *Server) cashierAllowedTypes(r *http.Request, mid int64) []cashierPayType {
	out := []cashierPayType{}
	if s.ProviderFlow == nil {
		return out
	}
	for _, t := range originalPayTypes {
		open := false
		if tx, e := s.Pool.Begin(r.Context()); e == nil {
			o, qe := payTypeCatalogOpen(r.Context(), tx, t.Name)
			tx.Rollback(r.Context())
			open = qe == nil && o
		}
		if !open {
			continue
		}
		served := false
		for _, id := range s.MerchantChannels[mid] {
			p := s.ProviderFlow.Providers[id]
			if p != nil && p.ID() == t.Name && len(p.Capabilities().Methods) > 0 {
				served = true
				break
			}
		}
		if served {
			out = append(out, cashierPayType{Name: t.Name, ShowName: t.ShowName})
		}
	}
	return out
}

// cashierPublic serves GET /cashier (the page) and POST /cashier (the payer's
// chosen type → the real provider-routed create). Returns true when it handled
// the request.
func (s *Server) cashierPublic(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/cashier" {
		return false
	}
	if s.Pool == nil || s.Store == nil || s.ProviderFlow == nil || !s.ProviderFlow.AllowLive || len(s.CheckoutKey) < 32 {
		http.Error(w, "cashier unavailable", http.StatusServiceUnavailable)
		return true
	}
	in, ok := s.decodeCashierIntent(r)
	if !ok {
		http.Error(w, "收银台链接无效或已过期，请返回来源地重新发起请求", http.StatusBadRequest)
		return true
	}
	if r.Method == "POST" {
		s.cashierChoose(w, r, in)
		return true
	}
	if r.Method != "GET" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return true
	}
	s.cashierPage(w, r, in)
	return true
}

// cashierPage renders the pay-type picker through the public page template with
// Pro's look (订单名称/金额 header, a radio list of pay types, one 立即支付).
func (s *Server) cashierPage(w http.ResponseWriter, r *http.Request, in cashierIntent) {
	types := s.cashierAllowedTypes(r, in.MerchantID)
	w.Header().Set("Content-Security-Policy", "default-src 'none'; base-uri 'none'; frame-ancestors 'none'; style-src 'unsafe-inline'; img-src 'self'; form-action 'self'")
	page := pagetemplate.Page{Title: "收银台", Amount: in.Money, OrderName: backoffice.CheckoutDisplayName(s.cashierHideName(r), in.Name, ""), Sandbox: s.Sandbox}
	tail, ok := s.pageTemplateOpen(w, r, "pay", page, nil)
	if !ok {
		return
	}
	if len(types) == 0 {
		io.WriteString(w, "<p>当前没有可用的支付方式，请联系商户。</p>"+tail)
		return
	}
	token := r.URL.Query().Get("t")
	payload := r.URL.Query().Get("i")
	io.WriteString(w, `<form method="post" action="/cashier?i=`+html.EscapeString(url.QueryEscape(payload))+`&amp;t=`+html.EscapeString(url.QueryEscape(token))+`"><fieldset><legend>支付方式</legend>`)
	for i, t := range types {
		checked := ""
		if i == 0 {
			checked = " checked"
		}
		io.WriteString(w, `<label><input type="radio" name="type" value="`+html.EscapeString(t.Name)+`"`+checked+`> `+html.EscapeString(t.ShowName)+`</label>`)
	}
	io.WriteString(w, `</fieldset><button type="submit">立即支付</button></form>`+tail)
}

// cashierHideName reads pageordername so the cashier page follows the same
// goods-name masking as the checkout page.
func (s *Server) cashierHideName(r *http.Request) string {
	v, _ := backoffice.New(s.Pool, s.Store).SettingValue(r.Context(), "pageordername")
	return v
}

// cashierChoose runs the identical provider-routed create for the chosen type,
// exactly once, then continues to payment exactly as a submit with that type
// would. A replay (same out_trade_no) resumes the same order.
func (s *Server) cashierChoose(w http.ResponseWriter, r *http.Request, in cashierIntent) {
	if e := r.ParseForm(); e != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	chosen := r.PostFormValue("type")
	allowed := false
	for _, t := range s.cashierAllowedTypes(r, in.MerchantID) {
		if t.Name == chosen {
			allowed = true
			break
		}
	}
	if !allowed {
		http.Error(w, "支付方式(type)不存在", http.StatusBadRequest)
		return
	}
	// Rebuild the verified request with the chosen type and run the SAME
	// browser-page create a submit with that type uses. The create is signed by
	// the cashier token, not re-verified here: the intent carries the already
	// verified request fields. SetOrderClientIP uses the stored client IP.
	p := map[string]string{
		"out_trade_no": in.OutTradeNo,
		"name":         in.Name,
		"money":        in.Money,
		"notify_url":   in.NotifyURL,
		"return_url":   in.ReturnURL,
		"param":        in.Param,
		"type":         chosen,
		"clientip":     in.ClientIP,
	}
	if in.Buyer != "" {
		p["sub_openid"] = in.Buyer
	}
	// The /cashier request path already selects the browser redirect in
	// providerCreate. Retain the authenticated protocol for the stored order
	// and its later callback instead of using V1 to request browser rendering.
	s.providerCreate(w, r, in.Version == "v1", in.MerchantID, p)
}
