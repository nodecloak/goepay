package server

import (
	"context"
	"errors"
	"fmt"
	provider "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/ledger"
	"goepay/internal/pagetemplate"
	"goepay/internal/payment"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// A launch only carries an opaque payment context into the configured mini
// program. The separate, one-use SDK-code exchange proves the payer later.
func (s *Server) miniLaunchChannelReady(mid, channel int64) (provider.MiniLaunchProvider, provider.MiniProgramConfig, string, bool) {
	var empty provider.MiniProgramConfig
	if s.Sandbox || s.Store == nil || s.Pool == nil || len(s.CheckoutKey) < 32 || s.ProviderFlow == nil || !s.ProviderFlow.AllowLive || s.ProviderFlow.LocalSimulation || !validOAuthOrigin(s.ProviderFlow.CallbackBaseURL) {
		return nil, empty, "", false
	}
	allowed := false
	for _, id := range s.MerchantChannels[mid] {
		if id == channel {
			allowed = true
		}
	}
	if !allowed {
		return nil, empty, "", false
	}
	p, oauth := s.ProviderFlow.Providers[channel], s.MiniOAuthProviders[channel]
	if p == nil || oauth == nil || p.ID() != oauth.ID() || (p.ID() != "wxpay" && p.ID() != "alipay") {
		return nil, empty, "", false
	}
	identity, ok := p.(provider.ApplicationIdentity)
	if !ok || identity.ApplicationID() == "" || identity.ApplicationID() != oauth.ApplicationID() {
		return nil, empty, "", false
	}
	launcher, ok := p.(provider.MiniLaunchProvider)
	if !ok {
		return nil, empty, "", false
	}
	metadata, ok := launcher.MiniProgram()
	if !ok || metadata.AppID != identity.ApplicationID() || metadata.Path == "" {
		return nil, empty, "", false
	}
	method := "jsapi"
	if p.ID() == "alipay" {
		method = "mini"
	}
	applet, underlying := false, false
	for _, m := range p.Capabilities().Methods {
		if m == "applet" {
			applet = true
		}
		if m == method {
			underlying = true
		}
	}
	return launcher, metadata, method, applet && underlying
}

func (s *Server) miniLaunchBindingReady(ctx context.Context, b payment.MiniLaunchBinding) (provider.MiniLaunchProvider, bool) {
	p, metadata, method, ok := s.miniLaunchChannelReady(b.MerchantID, b.ChannelID)
	if !ok || b.Provider != s.ProviderFlow.Providers[b.ChannelID].ID() || b.AppID != metadata.AppID || b.Path != metadata.Path || b.Method != method || b.Purpose != "mini-payment" {
		return nil, false
	}
	var enabled bool
	e := s.Pool.QueryRow(ctx, "SELECT enabled FROM merchants WHERE id=$1", b.MerchantID).Scan(&enabled)
	return p, e == nil && enabled
}

func (s *Server) miniCompleteURL(b payment.MiniLaunchBinding, state string) string {
	return strings.TrimRight(s.ProviderFlow.CallbackBaseURL, "/") + fmt.Sprintf("/oauth/mini/complete/%s/%d/%s/", b.Provider, b.ChannelID, url.PathEscape(b.TradeNo)) + "?" + url.Values{"state": {state}}.Encode()
}

func (s *Server) miniLaunchCreate(w http.ResponseWriter, r *http.Request, legacy bool, mid, channel int64, ip string, amount int64, params compat.Params) {
	p, metadata, method, ok := s.miniLaunchChannelReady(mid, channel)
	if !ok || params["sub_openid"] != "" || params["oauth"] != "" && params["oauth"] != "0" || params["sub_appid"] != "" && params["sub_appid"] != metadata.AppID {
		s.wire(w, legacy, nil, "mini payment capability unavailable")
		return
	}
	// Prove all local material prerequisites before creating an order or state.
	preflight := payment.MiniLaunchBinding{TradeNo: "preflight", MerchantID: mid, ChannelID: channel, Provider: params["type"], AppID: metadata.AppID, Method: method, Purpose: "mini-payment", Path: metadata.Path}
	if _, e := p.MiniLaunch(provider.MiniLaunchRequest{TradeNo: preflight.TradeNo, Amount: amount, CompleteURL: s.miniCompleteURL(preflight, strings.Repeat("a", 64))}); e != nil {
		s.wire(w, legacy, nil, "mini payment material unavailable")
		return
	}
	cost, e := s.ProviderFlow.PaymentCostQuote(channel, method)
	if e != nil {
		s.wire(w, legacy, nil, "payment cost configuration unavailable")
		return
	}
	o, e := s.Store.CreateOrder(r.Context(), payment.CreateOrderInput{PaymentCost: cost, Version: wireVersion(legacy), MerchantID: mid, OutTradeNo: params["out_trade_no"], Name: params["name"], Type: params["type"], Amount: amount, NotifyURL: params["notify_url"], ReturnURL: params["return_url"], Param: params["param"]})
	if e != nil {
		s.wire(w, legacy, nil, "order rejected")
		return
	}
	if e = s.Store.SetOrderClientIP(r.Context(), o.ID, ip); e != nil {
		s.wire(w, legacy, nil, "order metadata unavailable")
		return
	}
	b := preflight
	b.TradeNo = o.TradeNo
	if e = s.Store.PrepareMiniPayment(r.Context(), b); e != nil {
		s.wire(w, legacy, nil, "mini payment intent rejected")
		return
	}
	checkout := s.checkoutURL(o.TradeNo, "checkout")
	if r.URL.Path == "/submit.php" || r.URL.Path == "/api/pay/submit" {
		http.Redirect(w, r, checkout, http.StatusFound)
		return
	}
	out := map[string]any{"trade_no": o.TradeNo}
	if legacy {
		out["payurl"] = checkout
		s.wire(w, true, out, "")
		return
	}
	if params["method"] == "jump" {
		out["pay_type"] = "jump"
		out["pay_info"] = checkout
		s.wire(w, false, out, "")
		return
	}
	if _, e = s.Store.ProviderAttempt(r.Context(), o.TradeNo); e == nil {
		// A consumed identity can only reuse its original payment attempt.
		out["pay_type"] = "jump"
		out["pay_info"] = checkout
		s.wire(w, false, out, "")
		return
	} else if !errors.Is(e, payment.ErrNotFound) {
		s.wire(w, false, nil, "mini payment unavailable")
		return
	}
	material, e := s.issueMiniLaunch(r.Context(), p, b, o.Amount)
	if e != nil {
		s.wire(w, false, map[string]any{"trade_no": o.TradeNo}, "mini payment requires a fresh checkout")
		return
	}
	out["pay_type"] = material.MaterialType
	out["pay_info"] = material.Material
	s.wire(w, false, out, "")
}

func (s *Server) issueMiniLaunch(ctx context.Context, p provider.MiniLaunchProvider, b payment.MiniLaunchBinding, amount int64) (provider.CreateResult, error) {
	state, e := s.Store.IssueMiniState(ctx, b)
	if e != nil {
		return provider.CreateResult{}, e
	}
	return p.MiniLaunch(provider.MiniLaunchRequest{TradeNo: b.TradeNo, Amount: amount, CompleteURL: s.miniCompleteURL(b, state)})
}

// The caller must authenticate the checkout token before invoking this hook.
func (s *Server) miniLaunchCheckout(w http.ResponseWriter, r *http.Request, o payment.Order) bool {
	b, e := s.Store.MiniPaymentIntent(r.Context(), o.TradeNo)
	if errors.Is(e, payment.ErrNotFound) {
		return false
	}
	if e != nil {
		http.Error(w, "mini payment unavailable", 503)
		return true
	}
	if _, e = s.Store.ProviderAttempt(r.Context(), o.TradeNo); e == nil {
		return false
	} else if !errors.Is(e, payment.ErrNotFound) {
		http.Error(w, "mini payment unavailable", 503)
		return true
	}
	path := "/checkout/" + url.PathEscape(o.TradeNo)
	if r.URL.Path != path {
		if r.URL.Path == path+"/return" {
			http.Error(w, "mini payment authorization is incomplete", 202)
		} else {
			http.NotFound(w, r)
		}
		return true
	}
	p, ok := s.miniLaunchBindingReady(r.Context(), b)
	if !ok {
		http.Error(w, "mini payment unavailable", 503)
		return true
	}
	material, e := s.issueMiniLaunch(r.Context(), p, b, o.Amount)
	if e != nil {
		http.Error(w, "mini payment requires a fresh payment query", 409)
		return true
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src 'self'; base-uri 'none'; frame-ancestors 'none'")
	if !s.pageTemplateOpen(w, r, "pay", pagetemplate.Page{Title: "小程序支付", TradeNo: o.TradeNo, Amount: ledger.Format(o.Amount), OrderName: o.Name, Sandbox: s.Sandbox}, nil) {
		return true
	}
	if material.MaterialType == "urlscheme" && o.Type == "alipay" {
		fmt.Fprintf(w, `<a href="%s">打开支付宝小程序</a>`, html.EscapeString(material.Material))
	} else {
		fmt.Fprintf(w, "<p>请由商户客户端使用以下参数打开可信微信小程序：</p><pre>%s</pre>", html.EscapeString(material.Material))
	}
	io.WriteString(w, "<p>启动材料不代表付款成功；请在小程序完成身份授权和支付，结果以查单或可信异步通知为准。</p></main></html>")
	return true
}
