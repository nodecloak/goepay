package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
	"goepay/internal/backoffice"
	"goepay/internal/ledger"
	"goepay/internal/pagetemplate"
	"goepay/internal/payment"
)

func wechatInside240(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.UserAgent()), "micromessenger")
}

// Opt in only for a bound original WeChat operation with known material semantics.
// Ported plugin HTML and unknown method/material combinations keep their old path.
func (s *Server) originalWechatBrowser240(ctx context.Context, o payment.Order, a payment.ProviderAttempt) (bool, error) {
	if o.Type != "wxpay" {
		return false, nil
	}
	types := map[string]string{"native": "qrcode", "h5": "jump", "jsapi": "jsapi", "app": "app"}
	if types[a.Method] == "" || types[a.Method] != a.MaterialType {
		return false, nil
	}
	original, e := s.originalOrderServer(ctx, o.TradeNo, a.ChannelID)
	if e != nil || original.ProviderFlow == nil {
		return false, payment.ErrConflict
	}
	p := original.ProviderFlow.Providers[a.ChannelID]
	if p == nil || p.ID() != "wxpay" {
		return false, payment.ErrConflict
	}
	if plugin, ok := p.(interface{ PluginName() string }); ok && plugin.PluginName() != "" {
		return false, nil
	}
	return true, nil
}

func checkoutQRImage240(w io.Writer, source, label string) {
	renderCheckoutFragment(w, "qr-image", struct{ Source, Label string }{source, label})
}

func (s *Server) wechatContinuationQR240(w http.ResponseWriter, r *http.Request, o payment.Order, a payment.ProviderAttempt) {
	if o.Status != "pending" {
		http.Error(w, "order is no longer payable", http.StatusGone)
		return
	}
	supported, e := s.originalWechatBrowser240(r.Context(), o, a)
	if e != nil {
		http.Error(w, "original checkout unavailable", 503)
		return
	}
	if !supported || a.State != "pending" || a.Method == "native" {
		http.NotFound(w, r)
		return
	}
	s.wechatContinuationPNG240(w, r, o.TradeNo)
}
func (s *Server) wechatContinuationPNG240(w http.ResponseWriter, r *http.Request, trade string) {
	png, e := qrcode.Encode(s.alipayBrowserLaunchURL239(r, trade), qrcode.Medium, 320)
	if e != nil {
		http.Error(w, "QR generation unavailable", 500)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	_, _ = w.Write(png)
}
func (s *Server) wechatContinuationImage240(w io.Writer, r *http.Request) {
	target := *r.URL
	target.Path += "/browser-qr.png"
	checkoutQRImage240(w, target.String(), "微信支付二维码")
}

func (s *Server) renderWechatBrowser240(w http.ResponseWriter, r *http.Request, o payment.Order, a payment.ProviderAttempt, material, nonce string) {
	mobile := alipayMobileBrowser239(r) || r.URL.Query().Get("browser_launch") == "1"
	if a.Method == "native" {
		io.WriteString(w, `<p>请使用微信扫一扫完成支付。此订单使用扫码支付，当前手机无法直接调起时，请使用另一台设备扫码。</p>`)
		target := *r.URL
		target.Path += "/qr.png"
		checkoutQRImage240(w, target.String(), "微信支付二维码")
	} else if !mobile {
		io.WriteString(w, `<p>请使用手机扫描二维码，继续支付原订单。</p>`)
		s.wechatContinuationImage240(w, r)
		renderCheckoutFragment(w, "link", struct{ Href, Text string }{s.alipayBrowserLaunchURL239(r, o.TradeNo), "在当前设备继续支付"})
	} else {
		switch a.Method {
		case "h5":
			if wechatInside240(r) {
				io.WriteString(w, `<p>请点击微信右上角菜单，选择在浏览器中打开，再继续支付此订单。</p>`)
				s.wechatContinuationImage240(w, r)
			} else {
				u, e := url.Parse(material)
				if e != nil || (u.Scheme != "https" && u.Scheme != "http") || !s.checkoutJumpAllowed(material) {
					io.WriteString(w, `<p>原支付链接暂不可用，请回到商户核对订单。</p>`)
					break
				}
				renderCheckoutFragment(w, "jump", struct{ Lead, Href, Text string }{"正在前往微信支付。未自动打开时，请点击下方按钮。", material, "微信支付"})
				target, _ := json.Marshal(material)
				fmt.Fprintf(w, `<script nonce="%s">location.assign(%s);</script>`, nonce, target)
			}
		case "jsapi":
			if !wechatInside240(r) {
				io.WriteString(w, `<p>请在微信中打开此订单，由原付款微信账号继续支付。</p>`)
				s.wechatContinuationImage240(w, r)
				break
			}
			var params map[string]string
			if json.Unmarshal([]byte(a.Material), &params) != nil || params["appId"] == "" || params["package"] == "" || params["paySign"] == "" || params["nonceStr"] == "" || params["timeStamp"] == "" {
				io.WriteString(w, `<p>原支付信息暂不可用，请回到商户核对订单。</p>`)
				break
			}
			body, _ := json.Marshal(params)
			fmt.Fprintf(w, `<p id="wechat-feedback">正在等待微信支付…</p><button id="wechat-pay" type="button">微信支付</button><script nonce="%s">(function(){const params=%s,button=document.getElementById('wechat-pay'),feedback=document.getElementById('wechat-feedback');let started=false,busy=false,confirmed=false;function pay(){if(busy||confirmed)return;if(typeof WeixinJSBridge==='undefined'){feedback.textContent='正在等待微信就绪，请稍候';return;}busy=true;button.disabled=true;feedback.textContent='请在微信中确认付款';WeixinJSBridge.invoke('getBrandWCPayRequest',params,function(result){busy=false;const status=result&&result.err_msg;if(status==='get_brand_wcpay_request:ok'){confirmed=true;feedback.textContent='已提交支付，正在等待订单确认，请勿重复付款';}else{button.disabled=false;feedback.textContent=status==='get_brand_wcpay_request:cancel'?'已取消支付，可点击按钮继续':'支付结果尚未确认，请先核对订单后再继续';}});}function ready(){if(started)return;started=true;pay();}button.addEventListener('click',pay);document.addEventListener('WeixinJSBridgeReady',ready,false);if(typeof WeixinJSBridge!=='undefined')ready();})();</script>`, nonce, body)
		case "app":
			var p map[string]string
			if json.Unmarshal([]byte(a.Material), &p) != nil || p["appid"] == "" || p["prepayid"] == "" || p["sign"] == "" {
				io.WriteString(w, `<p>原应用支付信息暂不可用，请回到商户核对订单。</p>`)
				break
			}
			q := url.Values{"nonceStr": {p["noncestr"]}, "package": {p["package"]}, "partnerId": {p["partnerid"]}, "prepayId": {p["prepayid"]}, "timeStamp": {p["timestamp"]}, "sign": {p["sign"]}}
			launch := "weixin://app/" + url.PathEscape(p["appid"]) + "/pay/?" + q.Encode()
			if href, ok := s.trustedLaunchURL(launch); ok {
				renderCheckoutFragment(w, "jump", struct {
					Lead string
					Href template.URL
					Text string
				}{"请回到发起订单的应用完成支付；应用未自动打开时，可尝试下方入口。", href, "打开微信支付"})
			} else {
				io.WriteString(w, `<p>原应用支付信息暂不可用，请回到商户核对订单。</p>`)
			}
		}
	}
	s.checkoutStatusPoll239(w, r, o, nonce)
}

// Before OAuth there is no provider attempt. Desktop must not bind the phone's
// eventual authorization to a desktop cookie or issue an OAuth state for it.
func (s *Server) wechatOAuthDesktop240(w http.ResponseWriter, r *http.Request, o payment.Order) bool {
	path := "/checkout/" + url.PathEscape(o.TradeNo)
	if o.Type != "wxpay" || (r.URL.Path != path+"/browser-qr.png" && (alipayMobileBrowser239(r) || r.URL.Query().Get("browser_launch") == "1")) {
		return false
	}
	if o.Status != "created" {
		http.Error(w, "order is no longer payable", http.StatusGone)
		return true
	}
	if r.URL.Path == path+"/browser-qr.png" {
		s.wechatContinuationPNG240(w, r, o.TradeNo)
		return true
	}
	amount, e := s.Store.AuthenticatedOrderCharge104(r.Context(), o)
	if e != nil {
		http.Error(w, "original amount unavailable", 503)
		return true
	}
	random := make([]byte, 16)
	if _, e = rand.Read(random); e != nil {
		http.Error(w, "checkout unavailable", 503)
		return true
	}
	nonce := base64.RawStdEncoding.EncodeToString(random)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; base-uri 'none'; frame-ancestors 'none'; style-src 'unsafe-inline'; img-src 'self'; connect-src 'self'; script-src 'nonce-"+nonce+"'")
	displayName := o.Name
	if policy, e := backoffice.New(s.Pool, s.Store).SettingValue(r.Context(), "pageordername"); e == nil {
		displayName = backoffice.CheckoutDisplayName(policy, o.Name, "")
	}
	tail, ok := s.pageTemplateOpen(w, r, "pay", pagetemplate.Page{Title: "微信支付", TradeNo: o.TradeNo, Amount: ledger.Format(amount), OrderName: displayName, Sandbox: s.Sandbox}, nil)
	if !ok {
		return true
	}
	io.WriteString(w, `<p>请使用微信扫描二维码，在手机上授权并继续支付原订单。</p>`)
	s.wechatContinuationImage240(w, r)
	s.checkoutStatusPoll239(w, r, o, nonce)
	io.WriteString(w, tail)
	return true
}
