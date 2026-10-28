package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	qrcode "github.com/skip2/go-qrcode"
	provider "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/ledger"
	"goepay/internal/notification"
	"goepay/internal/pagetemplate"
	"goepay/internal/payment"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func (s *Server) providerCreate(w http.ResponseWriter, r *http.Request, legacy bool, merchantID int64, p compat.Params) {
	resolved, resolveErr := s.withParentRuntime(r.Context(), merchantID, p["out_trade_no"])
	if resolveErr != nil {
		s.wire(w, legacy, nil, "payment channel source could not be confirmed")
		return
	}
	s = resolved
	if s.ProviderFlow == nil || !s.ProviderFlow.AllowLive || len(s.CheckoutKey) < 32 {
		s.wire(w, legacy, nil, "payment providers unavailable")
		return
	}
	amount, e := ledger.Parse(p["money"])
	if e != nil || amount <= 0 || p["out_trade_no"] == "" || len(p["out_trade_no"]) > 128 || len(p["name"]) > 256 || !validURL(p["notify_url"]) || !validURL(p["return_url"]) {
		s.wire(w, legacy, nil, "invalid payment parameters")
		return
	}
	ip, _, splitErr := net.SplitHostPort(r.RemoteAddr)
	if splitErr != nil {
		ip = r.RemoteAddr
	}
	direct := r.URL.Path == "/submit.php" || r.URL.Path == "/api/pay/submit"
	if !direct && p["clientip"] != "" {
		parsed := net.ParseIP(p["clientip"])
		if parsed == nil {
			s.wire(w, legacy, nil, "invalid client IP")
			return
		}
		ip = parsed.String()
	}
	source := "signed_api_clientip"
	baseline := p["clientip"]
	if direct {
		source = "direct_http"
		baseline = ip
	}
	r = r.WithContext(payment.WithCoreRiskCustomerIP(r.Context(), baseline, source))
	routeParams := compat.Params{}
	for k, v := range p {
		routeParams[k] = v
	}
	routeParams["clientip"] = ip
	channelID, method, e := s.providerRoutedRequest(r.Context(), merchantID, routeParams)
	if e != nil {
		s.wire(w, legacy, nil, "payment channel not permitted")
		return
	}
	resolved, e = s.paymentIntentServer(r.Context(), merchantID, p["out_trade_no"], channelID)
	if e != nil {
		s.wire(w, legacy, nil, "payment account could not be confirmed")
		return
	}
	s = resolved
	if method == "scan" {
		s.providerScanCreate(w, r, legacy, merchantID, channelID, ip, amount, p)
		return
	}
	if method == "applet" {
		s.miniLaunchCreate(w, r, legacy, merchantID, channelID, ip, amount, p)
		return
	}
	cost, e := s.ProviderFlow.PaymentCostQuote(channelID, method)
	if e != nil {
		s.wire(w, legacy, nil, "payment cost configuration unavailable")
		return
	}
	order, e := s.Store.CreateOrder(r.Context(), payment.CreateOrderInput{PaymentCost: cost, Version: wireVersion(legacy), MerchantID: merchantID, OutTradeNo: p["out_trade_no"], Name: p["name"], Type: p["type"], Amount: amount, NotifyURL: p["notify_url"], ReturnURL: p["return_url"], Param: p["param"]})
	if e != nil {
		s.wire(w, legacy, nil, "order rejected")
		return
	}
	if e := s.Store.SetOrderClientIP(r.Context(), order.ID, ip); e != nil {
		s.wire(w, legacy, nil, "order metadata unavailable")
		return
	}
	if s.oauthCreate(w, r, legacy, merchantID, p, order, channelID, method) {
		return
	}
	var attempt payment.ProviderAttempt
	if method == "preauth" {
		attempt, e = s.ProviderFlow.SubmitPreauthorization(r.Context(), order.TradeNo, channelID, ip, p["sub_openid"])
	} else {
		attempt, e = s.ProviderFlow.Submit(r.Context(), order.TradeNo, channelID, method, ip, p["sub_openid"])
	}
	if e != nil || attempt.State != "pending" || attempt.Material == "" {
		s.wire(w, legacy, nil, "payment outcome requires query")
		return
	}
	checkout := s.checkoutURL(order.TradeNo, "checkout")
	material, e := s.coreRiskPublicPaymentMaterial(r.Context(), order, attempt)
	if e != nil {
		s.wire(w, legacy, nil, "original scan protection unavailable; query the original order")
		return
	}
	if r.URL.Path == "/submit.php" || r.URL.Path == "/api/pay/submit" {
		http.Redirect(w, r, checkout, http.StatusFound)
		return
	}
	data := map[string]any{"trade_no": order.TradeNo}
	if legacy {
		data["payurl"] = checkout
	} else {
		data["pay_type"] = "jump"
		data["pay_info"] = checkout
		if p["method"] != "jump" && (attempt.MaterialType == "qrcode" || attempt.MaterialType == "html") {
			data["pay_type"] = attempt.MaterialType
			data["pay_info"] = material
		}
		if p["method"] != "jump" && ((method == "app" && attempt.MaterialType == "app") || ((method == "jsapi" || method == "mini") && attempt.MaterialType == "jsapi")) {
			data["pay_type"] = attempt.MaterialType
			data["pay_info"] = attempt.Material
		}
		if method == "preauth" && attempt.MaterialType == "preauth" {
			switch p["method"] {
			case "app":
				data["pay_type"] = "app"
				data["pay_info"] = attempt.Material
			case "jsapi", "preauth":
				data["pay_type"] = "jsapi"
				data["pay_info"] = attempt.Material
			}
		}
	}
	s.wire(w, legacy, data, "")
}
func (s *Server) providerRoute(merchantID int64, p compat.Params) (int64, string, error) {
	if p["type"] != "wxpay" && p["type"] != "alipay" {
		return 0, "", payment.ErrInvalid
	}
	allowed := s.MerchantChannels[merchantID]
	channelID := int64(0)
	if p["channel_id"] != "" {
		v, e := strconv.ParseInt(p["channel_id"], 10, 64)
		if e != nil {
			return 0, "", payment.ErrInvalid
		}
		channelID = v
	}
	if channelID == 0 {
		for _, id := range allowed {
			if provider := s.ProviderFlow.Providers[id]; provider != nil && provider.ID() == p["type"] {
				channelID = id
				break
			}
		}
	}
	permitted := false
	for _, id := range allowed {
		if id == channelID {
			permitted = true
		}
	}
	provider := s.ProviderFlow.Providers[channelID]
	if !permitted || provider == nil || provider.ID() != p["type"] {
		return 0, "", payment.ErrInvalid
	}
	method := p["method"]
	if method == "" || method == "web" || method == "jump" {
		method = s.defaultProviderMethod(merchantID, channelID, p)
	}
	if method == "app" && !hasProviderMethod(provider.Capabilities().Methods, "app") && hasProviderMethod(provider.Capabilities().Methods, "applet") {
		if _, _, _, ready := s.miniLaunchChannelReady(merchantID, channelID); ready {
			method = "applet"
		}
	}
	if method == "jsapi" && p["type"] == "alipay" && p["is_applet"] == "1" {
		method = "mini"
	}
	if method == "app" && !hasProviderMethod(provider.Capabilities().Methods, "app") {
		flow, err := s.ProviderFlow.SelectParentPayment(merchantID, channelID, "applet", true)
		if err == nil && hasProviderMethod(flow.Providers[channelID].Capabilities().Methods, "applet") {
			method = "applet"
		}
	}
	flow, selectErr := s.ProviderFlow.SelectParentPayment(merchantID, channelID, method, p["is_applet"] == "1" || method == "applet")
	if selectErr != nil {
		return 0, "", payment.ErrConflict
	}
	s.ProviderFlow = &flow
	provider = flow.Providers[channelID]
	if p["type"] == "alipay" {
		onlyPreauth := len(provider.Capabilities().Methods) > 0
		hasPreauth := false
		for _, m := range provider.Capabilities().Methods {
			if m == "preauth" {
				hasPreauth = true
			}
			if m != "preauth" && m != "scan" {
				onlyPreauth = false
			}
		}
		if onlyPreauth && hasPreauth {
			switch p["method"] {
			case "", "web", "jump", "jsapi", "app":
				method = "preauth"
			}
		}
	}
	supported := false
	for _, m := range provider.Capabilities().Methods {
		if m == method {
			supported = true
		}
	}
	if p["oauth"] != "" && p["oauth"] != "0" && p["oauth"] != "1" {
		return 0, "", payment.ErrInvalid
	}
	oauthAllowed := s.oauthRouteAllowed(merchantID, channelID, method, p)
	if p["oauth"] == "1" && !oauthAllowed {
		return 0, "", payment.ErrInvalid
	}
	if method == "applet" {
		_, _, _, ready := s.miniLaunchChannelReady(merchantID, channelID)
		if !ready || p["sub_openid"] != "" {
			return 0, "", payment.ErrInvalid
		}
	}
	if !supported || (method == "jsapi" || method == "mini") && p["sub_openid"] == "" && !oauthAllowed {
		return 0, "", payment.ErrInvalid
	}
	if p["is_applet"] != "" && p["is_applet"] != "0" && p["is_applet"] != "1" {
		return 0, "", payment.ErrInvalid
	}
	if p["is_applet"] == "1" && method != "jsapi" && method != "mini" && method != "preauth" && method != "applet" {
		return 0, "", payment.ErrInvalid
	}
	if p["sub_appid"] != "" {
		identity, ok := provider.(interface{ ApplicationID() string })
		if !ok || identity.ApplicationID() != p["sub_appid"] {
			return 0, "", payment.ErrInvalid
		}
	}
	return channelID, method, nil
}
func (s *Server) checkoutToken(trade, purpose, expires string) string {
	mac := hmac.New(sha256.New, s.CheckoutKey)
	mac.Write([]byte("goepay-checkout-v1\n" + trade + "\n" + purpose + "\n" + expires))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (s *Server) checkoutURL(trade, purpose string) string {
	path := "/checkout/" + url.PathEscape(trade)
	if purpose == "return" {
		path += "/return"
	} else if purpose == "scan-region" || purpose == "scan-region-continue" {
		path += "/" + purpose
	}
	expires := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	q := url.Values{"expires": {expires}, "token": {s.checkoutToken(trade, purpose, expires)}}
	base := ""
	if s.ProviderFlow != nil {
		base = strings.TrimRight(s.ProviderFlow.CallbackBaseURL, "/")
	}
	return base + path + "?" + q.Encode()
}
func (s *Server) ProviderReturnURL(trade string) string { return s.checkoutURL(trade, "return") }
func (s *Server) checkoutAuthorized(r *http.Request, trade, purpose string) bool {
	if len(s.CheckoutKey) < 32 {
		return false
	}
	q := r.URL.Query()
	if len(q["expires"]) != 1 || len(q["token"]) != 1 {
		return false
	}
	expires := q.Get("expires")
	n, e := strconv.ParseInt(expires, 10, 64)
	if e != nil || n < time.Now().Unix() || n > time.Now().Add(2*time.Hour).Unix() {
		return false
	}
	want := s.checkoutToken(trade, purpose, expires)
	return hmac.Equal([]byte(want), []byte(q.Get("token")))
}
func (s *Server) providerCallback(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/callbacks/providers/"), "/")
	if (r.Method != "POST" && r.Method != "GET") || len(parts) != 2 || s.ProviderFlow == nil {
		http.Error(w, "callback unavailable", 503)
		return
	}
	id, e := strconv.ParseInt(parts[0], 10, 64)
	if e != nil || id <= 0 || strconv.FormatInt(id, 10) != parts[0] || parts[1] == "" {
		http.NotFound(w, r)
		return
	}
	original, resolveErr := s.originalOrderServer(r.Context(), parts[1], id)
	if resolveErr != nil {
		http.Error(w, "original payment account unavailable", 503)
		return
	}
	s = original
	provider := s.ProviderFlow.Providers[id]
	if e != nil || provider == nil || parts[1] == "" {
		http.NotFound(w, r)
		return
	}
	// Ported plugins may receive GET callbacks (the original read $_GET); the
	// notification bytes are then the raw query string. Everything else stays POST.
	if r.Method == "GET" && !callbackMethodAllowed(provider, "GET") {
		http.Error(w, "callback unavailable", 503)
		return
	}
	var raw []byte
	if r.Method == "GET" {
		raw = []byte(r.URL.RawQuery)
		if len(raw) > 1<<20 {
			e = payment.ErrInvalid
		}
	} else {
		raw, e = io.ReadAll(io.LimitReader(r.Body, 1<<20+1))
		if e == nil && len(raw) > 1<<20 {
			e = payment.ErrInvalid
		}
	}
	if e == nil {
		e = s.ProviderFlow.HandleNotification(r.Context(), id, parts[1], r.Header, raw)
	} else {
		e = payment.ErrInvalid
	}
	w.Header().Set("Cache-Control", "no-store")
	if acker, ok := provider.(interface {
		NotificationAck(error) (int, string, string)
	}); ok {
		if status, contentType, body := acker.NotificationAck(e); status != 0 {
			w.Header().Set("Content-Type", contentType)
			w.WriteHeader(status)
			io.WriteString(w, body)
			return
		}
	}
	if format, ok := provider.(interface{ NotificationFormat() string }); ok && format.NotificationFormat() == "wechat-v3" {
		if e != nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"code": "FAIL", "message": "verification failed"})
		} else {
			w.WriteHeader(http.StatusNoContent)
		}
		return
	}
	if provider.ID() == "wxpay" {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		if e != nil {
			w.WriteHeader(400)
			io.WriteString(w, `<xml><return_code><![CDATA[FAIL]]></return_code><return_msg><![CDATA[verification failed]]></return_msg></xml>`)
			return
		}
		io.WriteString(w, `<xml><return_code><![CDATA[SUCCESS]]></return_code><return_msg><![CDATA[OK]]></return_msg></xml>`)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if e != nil {
		w.WriteHeader(400)
		io.WriteString(w, "fail")
		return
	}
	io.WriteString(w, "success")
}
func (s *Server) checkout(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "method not allowed", 405)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/checkout/"), "/")
	if len(parts) < 1 || len(parts) > 2 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	purpose := "checkout"
	if len(parts) == 2 {
		if parts[1] != "return" && parts[1] != "qr.png" && parts[1] != "scan-region" && parts[1] != "scan-region-continue" {
			http.NotFound(w, r)
			return
		}
		if parts[1] == "return" || parts[1] == "scan-region" || parts[1] == "scan-region-continue" {
			purpose = parts[1]
		}
	}
	if !s.checkoutAuthorized(r, parts[0], purpose) {
		http.Error(w, "invalid or expired checkout token", 403)
		return
	}
	if s.Store == nil {
		http.Error(w, "unavailable", 503)
		return
	}
	order, e := s.Store.OrderByTradeNo(r.Context(), parts[0])
	if e != nil {
		http.NotFound(w, r)
		return
	}
	charge104, e := s.Store.AuthenticatedOrderCharge104(r.Context(), order)
	if e != nil {
		http.Error(w, "original payment amount unavailable", 503)
		return
	}
	if s.oauthCheckout(w, r, order) {
		return
	}
	if s.miniLaunchCheckout(w, r, order) {
		return
	}
	attempt, e := s.Store.ProviderAttempt(r.Context(), order.TradeNo)
	if e != nil {
		http.NotFound(w, r)
		return
	}
	if purpose == "scan-region" || purpose == "scan-region-continue" {
		s.coreRiskScanCheckout(w, r, order, attempt, purpose)
		return
	}
	if purpose == "return" {
		if order.Status != "paid" && s.ProviderFlow != nil {
			if _, e := s.ProviderFlow.Query(r.Context(), order.MerchantID, order.TradeNo); e == nil {
				if updated, e := s.Store.OrderByTradeNo(r.Context(), order.TradeNo); e == nil {
					order = updated
				}
			}
		}
		if order.Status == "paid" && order.ReturnURL != "" {
			if !validURL(order.ReturnURL) {
				http.Error(w, "invalid stored merchant return URL", 500)
				return
			}
			merchant, e := s.Store.Merchant(r.Context(), order.MerchantID)
			if e != nil {
				http.Error(w, "merchant return unavailable", 503)
				return
			}
			target, e := notification.URL(order.ReturnURL, order, merchant, s.PlatformKey, time.Now())
			if e != nil {
				http.Error(w, "merchant return signing unavailable", 503)
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			http.Redirect(w, r, target, http.StatusFound)
			return
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	if purpose == "checkout" && order.Status != "paid" {
		if s.ProviderFlow == nil || s.ProviderFlow.ValidatePaymentAccount(r.Context(), order.MerchantID, order.TradeNo) != nil {
			http.Error(w, "original receiving account unavailable; query the original order", http.StatusServiceUnavailable)
			return
		}
	}
	if len(parts) == 2 && parts[1] == "qr.png" {
		if order.Status == "paid" {
			http.Error(w, "order already paid", http.StatusGone)
			return
		}
		if attempt.State != "pending" || attempt.MaterialType != "qrcode" {
			http.NotFound(w, r)
			return
		}
		material, e := s.coreRiskPublicPaymentMaterial(r.Context(), order, attempt)
		if e != nil {
			http.Error(w, "original scan protection unavailable", 503)
			return
		}
		png, e := qrcode.Encode(material, qrcode.Medium, 320)
		if e != nil {
			http.Error(w, "QR generation unavailable", 500)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(png)
		return
	}
	nonceBytes := make([]byte, 16)
	if _, e := rand.Read(nonceBytes); e != nil {
		http.Error(w, "checkout unavailable", 500)
		return
	}
	nonce := base64.RawStdEncoding.EncodeToString(nonceBytes)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; base-uri 'none'; frame-ancestors 'none'; style-src 'unsafe-inline'; img-src 'self'; form-action http: https:; script-src 'nonce-"+nonce+"'")
	if !s.pageTemplateOpen(w, r, "pay", pagetemplate.Page{Title: "支付收银台", TradeNo: order.TradeNo, Amount: ledger.Format(charge104), OrderName: order.Name, Sandbox: s.Sandbox}, nil) {
		return
	}
	if purpose == "return" {
		fmt.Fprintf(w, "<p>平台订单状态：%s</p><p>以平台异步通知或查单结果为准。</p>", html.EscapeString(order.Status))
		io.WriteString(w, "</main></body></html>")
		return
	}
	if order.Status == "paid" {
		if attempt.Method == "scan" {
			io.WriteString(w, "<p>付款码支付已完成，平台已确认支付；如有退款，请以订单查询的退款状态为准。</p></main></body></html>")
		} else {
			io.WriteString(w, "<p>该订单已确认支付；如有退款，请以订单查询的退款状态为准。</p></main></body></html>")
		}
		return
	}
	if attempt.Method == "scan" {
		io.WriteString(w, "<p>付款码支付正在核实，请使用原订单查单。</p>")
		io.WriteString(w, "</main></body></html>")
		return
	}
	if attempt.State != "pending" {
		io.WriteString(w, "<p>订单正在核实，请稍后查单。</p></main></body></html>")
		return
	}
	material, e := s.coreRiskPublicPaymentMaterial(r.Context(), order, attempt)
	if e != nil {
		http.Error(w, "original scan protection unavailable", 503)
		return
	}
	switch attempt.MaterialType {
	case "html":
		io.WriteString(w, attempt.Material)
	case "qrcode":
		qrURL := *r.URL
		qrURL.Path += "/qr.png"
		clientName := "微信"
		if order.Type == "alipay" {
			clientName = "支付宝"
		}
		label := "支付二维码"
		if attempt.Method == "preauth" {
			label = "授权二维码"
			io.WriteString(w, "<p>授权材料仅用于买家确认冻结，实际扣款成功后才入账。</p>")
		}
		fmt.Fprintf(w, `<p>请使用%s扫描%s。</p><img width="320" height="320" alt="%s" src="%s"><p>%s</p>`, clientName, label, label, html.EscapeString(qrURL.String()), html.EscapeString(material))
	case "jump", "urlscheme":
		if !s.checkoutJumpAllowed(material) {
			http.Error(w, "invalid payment material", 500)
			return
		}
		fmt.Fprintf(w, `<a href="%s">继续支付</a>`, html.EscapeString(material))
	case "jsapi":
		if order.Type == "alipay" {
			params, _ := json.Marshal(map[string]string{"tradeNO": attempt.Material})
			fmt.Fprintf(w, `<button id="pay">支付宝支付</button><script nonce="%s">document.getElementById('pay').onclick=function(){if(typeof AlipayJSBridge==='undefined'){alert('请在支付宝内打开');return;}AlipayJSBridge.call('tradePay',%s,function(){location.href=%q;});};</script>`, nonce, params, s.ProviderReturnURL(order.TradeNo))
			break
		}
		var material map[string]string
		if e := json.Unmarshal([]byte(attempt.Material), &material); e != nil {
			http.Error(w, "invalid material", 500)
			return
		}
		body, _ := json.Marshal(material)
		fmt.Fprintf(w, `<button id="pay">微信支付</button><script nonce="%s">document.getElementById('pay').onclick=function(){if(typeof WeixinJSBridge==='undefined'){alert('请在微信内打开');return;}WeixinJSBridge.invoke('getBrandWCPayRequest',%s,function(){location.href=%q;});};</script>`, nonce, body, s.ProviderReturnURL(order.TradeNo))
	case "app":
		launch := ""
		if order.Type == "wxpay" {
			var p map[string]string
			if json.Unmarshal([]byte(attempt.Material), &p) != nil || p["appid"] == "" || p["prepayid"] == "" || p["sign"] == "" {
				http.Error(w, "invalid APP material", 500)
				return
			}
			q := url.Values{"nonceStr": {p["noncestr"]}, "package": {p["package"]}, "partnerId": {p["partnerid"]}, "prepayId": {p["prepayid"]}, "timeStamp": {p["timestamp"]}, "sign": {p["sign"]}}
			launch = "weixin://app/" + url.PathEscape(p["appid"]) + "/pay/?" + q.Encode()
		} else if order.Type == "alipay" {
			q := url.Values{"appId": {"20000125"}, "orderSuffix": {attempt.Material}}
			launch = "alipays://platformapi/startApp?" + q.Encode()
		}
		if !s.checkoutJumpAllowed(launch) {
			http.Error(w, "invalid APP material", 500)
			return
		}
		fmt.Fprintf(w, `<a href="%s">打开支付客户端</a><p>APP 支付结果以平台异步通知或查单结果为准。</p>`, html.EscapeString(launch))
	case "preauth":
		if order.Type != "alipay" {
			http.Error(w, "invalid authorization material", 500)
			return
		}
		q := url.Values{"appId": {"20000125"}, "orderSuffix": {attempt.Material}}
		launch := "alipays://platformapi/startApp?" + q.Encode()
		fmt.Fprintf(w, `<a href="%s">打开支付宝授权支付</a><p>授权材料不代表付款成功；授权状态与实际扣款分别核对，最终以平台查单和异步通知为准。</p>`, html.EscapeString(launch))
	default:
		io.WriteString(w, "<p>暂不支持该支付材料。</p>")
	}
	io.WriteString(w, "</main></body></html>")
}

func (s *Server) checkoutJumpAllowed(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.User != nil || u.Fragment != "" {
		return false
	}
	if u.Scheme == "https" {
		return u.Host != ""
	}
	if u.Scheme == "weixin" || u.Scheme == "alipays" {
		return true
	}
	if u.Scheme != "http" || s.ProviderFlow == nil || !s.ProviderFlow.LocalSimulation {
		return false
	}
	base, e := url.Parse(s.ProviderFlow.CallbackBaseURL)
	return e == nil && base.Scheme == "http" && base.User == nil && base.Host == u.Host && u.Host != ""
}
func (s *Server) providerOperation(w http.ResponseWriter, r *http.Request, legacy bool, merchantID int64, p compat.Params, route string) {
	if s.ProviderFlow == nil {
		s.wire(w, legacy, nil, "payment provider unavailable")
		return
	}
	if route == "/api/pay/refundquery" {
		refundNo := p["out_refund_no"]
		if refundNo == "" && p["refund_no"] != "" {
			var n int64
			var e error
			n, e = strconv.ParseInt(p["refund_no"], 10, 64)
			if e == nil {
				e = s.Pool.QueryRow(r.Context(), "SELECT refund_no FROM refunds WHERE id=$1 AND merchant_id=$2", n, merchantID).Scan(&refundNo)
			}
			if e != nil {
				s.wire(w, legacy, nil, "refund not found")
				return
			}
		}
		refund, e := s.ProviderFlow.QueryRefund(r.Context(), merchantID, refundNo)
		if e != nil {
			s.wire(w, legacy, nil, "refund query unavailable")
			return
		}
		if refund.Status != "succeeded" && refund.Status != "failed" {
			s.wireWithCode(w, legacy, map[string]any{"refund_no": strconv.FormatInt(refund.ID, 10), "out_refund_no": refund.RefundNo}, "refund processing", 0)
			return
		}
		status := 0
		if refund.Status == "succeeded" {
			status = 1
		}
		s.wireWithCode(w, legacy, map[string]any{"refund_no": strconv.FormatInt(refund.ID, 10), "out_refund_no": refund.RefundNo, "money": ledger.Format(refund.Amount), "reducemoney": ledger.Format(refund.MerchantAmount), "status": status}, "", 0)
		return
	}
	order, e := s.lookup(r, merchantID, p)
	if e != nil {
		s.wire(w, legacy, nil, "order not found")
		return
	}
	if route == "/api/pay/close" {
		if e := s.ProviderFlow.Close(r.Context(), merchantID, order.TradeNo); e != nil {
			s.wire(w, legacy, nil, "close outcome requires query")
			return
		}
		s.wireWithCode(w, legacy, map[string]any{"trade_no": order.TradeNo}, "", 0)
		return
	}
	amount, e := ledger.Parse(p["money"])
	if e != nil {
		s.wire(w, legacy, nil, "invalid refund amount")
		return
	}
	refundNo := p["out_refund_no"]
	if refundNo == "" {
		b := make([]byte, 16)
		if _, e := rand.Read(b); e != nil {
			s.wire(w, legacy, nil, "refund unavailable")
			return
		}
		refundNo = "rf" + base64.RawURLEncoding.EncodeToString(b)
	}
	refund, e := s.ProviderFlow.RefundFromAPI(r.Context(), merchantID, order.TradeNo, refundNo, amount)
	if e != nil {
		if refund.ID == 0 || errors.Is(e, payment.ErrMerchantAPIAdmission) {
			s.wire(w, legacy, nil, "refund request rejected")
			return
		}
		var unknown *provider.UnknownOutcome
		if errors.As(e, &unknown) {
			if existing, lookupError := s.Store.Refund(r.Context(), merchantID, refundNo); lookupError == nil && (existing.Status == "pending" || existing.Status == "unknown") {
				s.wireWithCode(w, legacy, map[string]any{"refund_no": strconv.FormatInt(existing.ID, 10), "out_refund_no": existing.RefundNo, "trade_no": order.TradeNo, "money": ledger.Format(existing.Amount), "reducemoney": ledger.Format(existing.MerchantAmount), "msg": "refund accepted; processing outcome requires query"}, "", 0)
				return
			}
		}
		s.wireWithCode(w, legacy, map[string]any{"out_refund_no": refundNo}, "refund outcome requires query", 0)
		return
	}
	s.wireWithCode(w, legacy, map[string]any{"refund_no": strconv.FormatInt(refund.ID, 10), "out_refund_no": refund.RefundNo, "trade_no": order.TradeNo, "money": ledger.Format(refund.Amount), "reducemoney": ledger.Format(refund.MerchantAmount)}, "", 0)
}
