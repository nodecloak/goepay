package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"goepay/internal/cryptopay"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

// Token access is confined to the dedicated checkout, status and QR paths.
// The public payload deliberately excludes merchant callback URLs and identity.
func (s *Server) cryptoCheckout(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/crypto/checkout/") {
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	if s.Crypto == nil {
		response(w, 503, -1, "USDT service unavailable", nil)
		return true
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/crypto/checkout/"), "/")
	if len(parts) < 1 || len(parts) > 2 || parts[0] == "" || len(parts) == 2 && parts[1] != "status" && parts[1] != "qr" {
		response(w, 404, -1, "checkout not found", nil)
		return true
	}
	v, e := s.Crypto.GetCheckout(r.Context(), parts[0], r.URL.Query().Get("token"))
	if e != nil {
		cryptoError(w, e)
		return true
	}
	if len(parts) == 2 {
		if parts[1] == "status" {
			data := map[string]any{"state": v.State, "amount": v.Amount, "received": v.Received, "expires_at": v.ExpiresAt}
			if target, err := s.cryptoPaidReturn(r.Context(), v); err == nil && target != "" {
				data["return_url"] = target
			}
			response(w, 200, 0, "", data)
			return true
		}
		png, e := qrcode.Encode(v.Address, qrcode.Medium, 320)
		if e != nil {
			response(w, 503, -1, "QR unavailable", nil)
			return true
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(png)
		return true
	}
	var raw [18]byte
	if _, e := rand.Read(raw[:]); e != nil {
		response(w, 503, -1, "checkout unavailable", nil)
		return true
	}
	nonce := base64.RawStdEncoding.EncodeToString(raw[:])
	w.Header().Set("Content-Security-Policy", "default-src 'none'; base-uri 'none'; frame-ancestors 'none'; style-src 'unsafe-inline'; img-src 'self'; connect-src 'self'; script-src 'nonce-"+nonce+"'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	query := "?" + url.Values{"token": {r.URL.Query().Get("token")}}.Encode()
	base := "/crypto/checkout/" + url.PathEscape(v.ID)
	returnURL, _ := s.cryptoPaidReturn(r.Context(), v)
	data := struct {
		Network, Address, Amount, Received, State, Expires, QR, Status, Return, Nonce string
		Simulation                                                                    bool
	}{v.Network, v.Address, v.Amount, v.Received, v.State, v.ExpiresAt.Format("2006-01-02 15:04:05 MST"), base + "/qr" + query, base + "/status" + query, returnURL, nonce, s.Sandbox}
	cryptoCheckoutPage.Execute(w, data)
	return true
}
func (s *Server) cryptoPaidReturn(ctx context.Context, public cryptopay.Invoice) (string, error) {
	if public.State != "paid" && public.State != "overpaid" {
		return "", nil
	}
	if s.Store == nil {
		return "", cryptopay.ErrUnavailable
	}
	v, e := s.Crypto.GetInvoice(ctx, 0, public.ID)
	if e != nil || v.ReturnURL == "" {
		return "", e
	}
	m, e := s.Store.Merchant(ctx, v.MerchantID)
	if e != nil {
		return "", e
	}
	hide, e := s.Store.NotifyHideName(ctx)
	if e != nil {
		return "", e
	}
	return cryptopay.NotificationURL(v.ReturnURL, v, m, s.PlatformKey, time.Now(), hide)
}

var cryptoCheckoutPage = template.Must(template.New("crypto-checkout").Parse(`<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>USDT 收银台</title>
<style>body{margin:0;background:#f1f5f9;color:#172637;font:16px/1.6 system-ui,sans-serif}main{max-width:440px;margin:6vh auto;padding:30px 24px;background:#fff;border:1px solid #dce4ed;border-radius:18px;box-sizing:border-box;text-align:center}h1{font-size:22px}.amount{font-size:clamp(22px,6vw,32px);overflow-wrap:anywhere;font-weight:700;font-variant-numeric:tabular-nums}.qr{display:block;width:256px;height:auto;max-width:100%;aspect-ratio:1;margin:20px auto}code{display:block;padding:12px;background:#f1f5f9;overflow-wrap:anywhere;text-align:left}button,a{display:inline-block;padding:10px 16px;margin-top:10px}button{cursor:pointer}button:focus-visible,a:focus-visible{outline:3px solid #087f8c}small{color:#526378}.warning{padding:12px;background:#fff3dc;border-radius:8px;text-align:left}.state{font-weight:600}#failure{color:#a11b2a}@media(max-width:480px){main{margin:16px;width:calc(100% - 32px);padding:22px 16px}}</style></head>
<body><main><h1>USDT 收银台</h1>{{if .Simulation}}<p class="warning">本地模拟环境，请勿转入真实资产。</p>{{end}}<p>{{if eq .Network "tron"}}TRON · TRC20{{else}}Ethereum · ERC20{{end}}</p><p class="amount">{{.Amount}} USDT</p>
<p class="warning">请使用所示网络的 USDT 支付。网络或资产错误无法自动入账。请核对金额与有效期。</p><img class="qr" width="320" height="320" src="{{.QR}}" alt="USDT 收款地址二维码"><p>使用钱包扫描，或复制下方地址支付</p><code id="address">{{.Address}}</code><button id="copy" type="button">复制收款地址</button><p id="copied" role="status"></p><p>已收 <span id="received">{{.Received}}</span> USDT</p><p id="state" class="state" role="status">{{.State}}</p><p id="failure" role="alert"></p><small>有效期：{{.Expires}}<br>以链上确认及平台到账记录为准。</small>{{if .Return}}<p><a id="return" href="{{.Return}}" hidden>返回商户</a></p>{{end}}
<script nonce="{{.Nonce}}">(function(){'use strict';var states={pending:'等待付款或链上最终确认',confirming:'链上确认中',paid:'支付已到账',underpaid:'少付，请联系商户核对',overpaid:'超付，请联系商户核对',expired:'收款单已过期，请勿继续付款；已转出款项仍将核对链上最终确认',late:'逾期到账，请联系商户核对'};var statusURL={{.Status}},initial={{.State}},initialReturn={{.Return}},timer,lastState=initial;function show(s,target){lastState=s;document.getElementById('state').textContent=states[s]||'状态待核对';if((s==='paid'||s==='overpaid')&&target){var a=document.getElementById('return');if(!a){a=document.createElement('a');a.id='return';a.textContent='返回商户';document.querySelector('main').appendChild(a);}a.href=target;a.hidden=false;}return s==='paid'||s==='late'||s==='overpaid';}document.getElementById('copy').onclick=async function(){try{await navigator.clipboard.writeText(document.getElementById('address').textContent);document.getElementById('copied').textContent='地址已复制，请同时核对网络及金额';}catch(e){document.getElementById('copied').textContent='请长按或选择上方地址复制';}};async function poll(){try{var res=await fetch(statusURL,{credentials:'omit',cache:'no-store'});if(!res.ok)throw new Error('status');var body=await res.json();if(body.code!==0||!body.data)throw new Error('status');document.getElementById('received').textContent=body.data.received;document.getElementById('failure').textContent='';if(show(body.data.state,body.data.return_url))return;}catch(e){document.getElementById('failure').textContent='到账状态暂无法核对，请勿重复付款；页面将继续查询';}timer=setTimeout(poll,lastState==='expired'?15000:3000);}if(!show(initial,initialReturn))timer=setTimeout(poll,lastState==='expired'?15000:3000);window.addEventListener('pagehide',function(){clearTimeout(timer);});})();</script></main></body></html>`))
