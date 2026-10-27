package server

import (
	"context"
	"fmt"
	"goepay/internal/payment"
	"html"
	"net"
	"net/http"
	"net/url"
	"strings"
)

func coreRiskHTTPMaterial(a payment.ProviderAttempt) bool {
	if a.Method != "native" && a.Method != "wap" && a.Method != "h5" || a.MaterialType != "qrcode" && a.MaterialType != "jump" {
		return false
	}
	u, e := url.Parse(a.Material)
	return e == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "" && u.User == nil && u.Fragment == "" && !strings.ContainsAny(a.Material, "\r\n\x00")
}
func (s *Server) coreRiskPublicPaymentMaterial(ctx context.Context, o payment.Order, a payment.ProviderAttempt) (string, error) {
	if !coreRiskHTTPMaterial(a) {
		return a.Material, nil
	}
	rule, e := s.Store.CoreRiskScanRegion(ctx, o.TradeNo, "")
	if e != nil {
		return "", e
	}
	if rule.Applicable {
		return s.checkoutURL(o.TradeNo, "scan-region"), nil
	}
	return a.Material, nil
}
func (s *Server) coreRiskScanCheckout(w http.ResponseWriter, r *http.Request, o payment.Order, a payment.ProviderAttempt, purpose string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; base-uri 'none'; frame-ancestors 'none'; style-src 'unsafe-inline'")
	if o.Status == "paid" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<p>该订单已确认支付，请查询原订单。</p>")
		return
	}
	if a.State != "pending" || !coreRiskHTTPMaterial(a) {
		http.Error(w, "original scan material unavailable; query the original order", 503)
		return
	}
	ip, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		ip = r.RemoteAddr
	}
	rule, e := s.Store.CoreRiskScanRegion(r.Context(), o.TradeNo, ip)
	if e != nil {
		http.Error(w, "无法核实原下单与当前扫码地区，请稍后重试或查询原订单。", 503)
		return
	}
	if !rule.Applicable {
		http.NotFound(w, r)
		return
	}
	if rule.Mismatch && rule.Mode == "1" {
		http.Error(w, "扫码地区与下单地区不一致，禁止支付。", 403)
		return
	}
	if rule.Mismatch && purpose != "scan-region-continue" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html lang="zh-CN"><meta name="viewport" content="width=device-width,initial-scale=1"><title>支付安全提示</title><body><main><h1>支付安全提示</h1><p>扫码地区与下单地区不一致，请确认订单由本人发起，谨防陌生人代付诈骗。</p><p>订单：%s</p><a href="%s">已核对订单，继续支付</a></main></body></html>`, html.EscapeString(o.TradeNo), html.EscapeString(s.checkoutURL(o.TradeNo, "scan-region-continue")))
		return
	}
	http.Redirect(w, r, a.Material, http.StatusFound)
}
