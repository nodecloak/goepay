package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/payment"
)

func alipayMobileBrowser239(r *http.Request) bool {
	ua := strings.ToLower(r.UserAgent())
	return strings.Contains(ua, "mobile") || strings.Contains(ua, "android") || strings.Contains(ua, "iphone") || strings.Contains(ua, "ipad") || strings.Contains(ua, "ipod")
}

// Only the restored original adapter can authenticate and re-sign a browser
// presentation. Unsupported plugin HTML retains its existing rendering path.
func (s *Server) originalAlipayBrowser239(ctx context.Context, o payment.Order, a payment.ProviderAttempt, desktop bool) (*adapter.BrowserCheckoutPresentation, error) {
	original, e := s.originalOrderServer(ctx, o.TradeNo, a.ChannelID)
	if e != nil || original.ProviderFlow == nil {
		return nil, payment.ErrConflict
	}
	p := original.ProviderFlow.Providers[a.ChannelID]
	if p == nil || p.ID() != "alipay" {
		return nil, payment.ErrConflict
	}
	if plugin, ok := p.(interface{ PluginName() string }); ok && plugin.PluginName() != "" {
		return nil, nil
	}
	presenter, ok := p.(interface {
		PrepareBrowserCheckout(string, string, string, bool) (adapter.BrowserCheckoutPresentation, error)
	})
	if !ok {
		return nil, nil
	}
	result, e := presenter.PrepareBrowserCheckout(a.Material, o.TradeNo, s.ProviderReturnURL(o.TradeNo), desktop)
	if errors.Is(e, adapter.ErrUnsupported) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	if result.Method != a.Method || (result.Method != "page" && result.Method != "wap") {
		return nil, payment.ErrConflict
	}
	origin, e := url.Parse(result.GatewayOrigin)
	if e != nil || origin.Host == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" || (origin.Scheme != "https" && origin.Scheme != "http") || strings.ContainsAny(result.GatewayOrigin, "\r\n\x00") {
		return nil, payment.ErrInvalid
	}
	if !strings.HasPrefix(result.HTML, `<form method="post" accept-charset="UTF-8" action="`) {
		return nil, payment.ErrInvalid
	}
	return &result, nil
}

func (s *Server) alipayBrowserLaunchURL239(r *http.Request, trade string) string {
	target, e := url.Parse(s.checkoutURL(trade, "checkout"))
	if e != nil {
		return ""
	}
	query := url.Values{"expires": {r.URL.Query().Get("expires")}, "token": {r.URL.Query().Get("token")}, "browser_launch": {"1"}}
	target.RawQuery = query.Encode()
	return target.String()
}

func (s *Server) alipayBrowserQR239(w http.ResponseWriter, r *http.Request, o payment.Order, a payment.ProviderAttempt) {
	if o.Status != "pending" {
		http.Error(w, "order is no longer payable", http.StatusGone)
		return
	}
	if a.State != "pending" || a.MaterialType != "html" || o.Type != "alipay" {
		http.NotFound(w, r)
		return
	}
	presentation, e := s.originalAlipayBrowser239(r.Context(), o, a, false)
	if e != nil {
		http.Error(w, "original checkout unavailable", http.StatusServiceUnavailable)
		return
	}
	if presentation == nil {
		http.NotFound(w, r)
		return
	}
	png, e := qrcode.Encode(s.alipayBrowserLaunchURL239(r, o.TradeNo), qrcode.Medium, 320)
	if e != nil {
		http.Error(w, "QR generation unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Write(png)
}

func alipayBrowserCSP239(p adapter.BrowserCheckoutPresentation, returnURL, nonce string) string {
	frames := "'self' " + p.GatewayOrigin
	forms := frames
	gateway, _ := url.Parse(p.GatewayOrigin)
	if gateway.Scheme == "https" && (gateway.Hostname() == "alipay.com" || strings.HasSuffix(gateway.Hostname(), ".alipay.com")) {
		frames += " https://*.alipay.com"
		forms += " https://*.alipay.com"
	}
	// Form-action also governs browser redirects after a POST. Keep the
	// original merchant's return origin reachable, without allowing arbitrary
	// destinations or weakening the parent iframe navigation policy.
	if target, e := url.Parse(returnURL); e == nil && (target.Scheme == "https" || target.Scheme == "http") && target.Host != "" && target.User == nil && !strings.ContainsAny(target.Host, " \t\r\n\x00;") {
		forms += " " + target.Scheme + "://" + target.Host
	}
	return "default-src 'none'; base-uri 'none'; frame-ancestors 'none'; style-src 'unsafe-inline'; img-src 'self'; connect-src 'self'; frame-src " + frames + "; form-action " + forms + "; script-src 'nonce-" + nonce + "'"
}

func (s *Server) renderAlipayBrowser239(w http.ResponseWriter, r *http.Request, o payment.Order, p adapter.BrowserCheckoutPresentation, nonce string) {
	desktop := !alipayMobileBrowser239(r) && r.URL.Query().Get("browser_launch") != "1"
	target := "_self"
	if desktop && p.Method == "page" {
		target = "alipay-cashier"
		// Keep the full QR width and leave room for the provider's image line box.
		// Disabling scrolling alone would hide gutters while retaining clipping.
		io.WriteString(w, `<p>请使用手机支付宝扫描二维码完成支付。</p><iframe name="alipay-cashier" title="支付宝支付二维码" scrolling="no" sandbox="allow-forms allow-scripts allow-same-origin allow-popups allow-popups-to-escape-sandbox allow-top-navigation-by-user-activation" style="display:block;width:230px;max-width:100%;height:256px;border:0;margin:20px auto"></iframe>`)
	} else if desktop {
		qrURL := *r.URL
		qrURL.Path += "/browser-qr.png"
		io.WriteString(w, `<p>请使用手机支付宝扫描支付二维码，在手机上继续原订单支付。</p>`)
		checkoutQRImage240(w, qrURL.String(), "支付宝支付二维码")
	} else {
		io.WriteString(w, `<p>正在前往支付宝，请稍候。未自动打开时，请点击下方按钮继续。</p>`)
	}
	form := strings.Replace(p.HTML, `<form `, `<form id="alipay-browser-form" target="`+target+`" `, 1)
	io.WriteString(w, form)
	if desktop {
		fmt.Fprintf(w, `<p><a href="%s">在当前设备前往支付宝支付</a></p>`, html.EscapeString(s.alipayBrowserLaunchURL239(r, o.TradeNo)))
	}
	if !desktop || p.Method == "page" {
		fmt.Fprintf(w, `<script nonce="%s">(function(){const form=document.getElementById('alipay-browser-form');if(form){HTMLFormElement.prototype.submit.call(form);}})();</script>`, nonce)
	}
	s.checkoutStatusPoll239(w, r, o, nonce)
}

func (s *Server) checkoutStatusPoll239(w http.ResponseWriter, r *http.Request, o payment.Order, nonce string) {
	statusURL := *r.URL
	statusURL.Path += "/status"
	statusJSON, _ := json.Marshal(statusURL.String())
	returnURL := ""
	if o.ReturnURL != "" {
		returnURL = s.ProviderReturnURL(o.TradeNo)
	}
	returnJSON, _ := json.Marshal(returnURL)
	fmt.Fprintf(w, `<p id="payment-status">等待支付结果确认…</p><script nonce="%s">(function(){const statusURL=%s,returnURL=%s;async function poll(){try{const response=await fetch(statusURL,{cache:'no-store',credentials:'omit'});if(response.status===403){document.getElementById('payment-status').textContent='页面验证已过期，请回到商户订单页重新打开；已付款请勿重复支付';return;}if(response.ok){const result=await response.json();if(result.paid===true){if(returnURL){location.replace(returnURL);}else{document.getElementById('payment-status').textContent='支付已完成';}return;}}}catch(e){}setTimeout(poll,3000);}setTimeout(poll,3000);})();</script>`, nonce, statusJSON, returnJSON)
}
