package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	qrcode "github.com/skip2/go-qrcode"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/channelconfig"
	"goepay/internal/compat"
	"goepay/internal/payment"
)

// This opt-in test runs the real Go checkout against the existing synthetic
// PostgreSQL pool. Only the native browser's WeixinJSBridge boundary is mocked;
// order creation and notifications use the real signed WeChat V2 protocol.
func TestWechatBrowser240ActualBrowser(t *testing.T) {
	if os.Getenv("GOEPAY_WECHAT_BROWSER240") != "1" {
		t.Skip("GOEPAY_WECHAT_BROWSER240=1 requires existing local Chromium, Playwright and synthetic PostgreSQL")
	}
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("synthetic GOEPAY_TEST_DATABASE_URL required")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid synthetic database configuration")
	}
	loopback := func(host string) bool {
		return host == "localhost" || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
	}
	if !loopback(config.ConnConfig.Host) {
		t.Fatal("browser240 requires existing loopback synthetic PostgreSQL")
	}
	for _, fallback := range config.ConnConfig.Fallbacks {
		if !loopback(fallback.Host) {
			t.Fatal("browser240 database fallback must remain on loopback")
		}
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("existing Node.js required")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "web", "node_modules", "@playwright", "test")); err != nil {
		t.Skip("existing project Playwright required")
	}
	artifacts := os.Getenv("GOEPAY_WECHAT_BROWSER240_ARTIFACTS")
	if artifacts == "" {
		t.Skip("GOEPAY_WECHAT_BROWSER240_ARTIFACTS must identify an owned-run directory")
	}
	artifacts, err = filepath.Abs(artifacts)
	if err != nil {
		t.Fatal(err)
	}
	owned := filepath.Join(root, ".tools", "wechat-checkout240", "owned-run")
	inside := func(base, target string) bool {
		relative, e := filepath.Rel(base, target)
		return e == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
	}
	if !inside(owned, artifacts) {
		t.Fatal("browser240 artifacts must remain in .tools/wechat-checkout240/owned-run")
	}
	if err := os.MkdirAll(artifacts, 0700); err != nil {
		t.Fatal(err)
	}
	realOwned, err := filepath.EvalSymlinks(owned)
	if err != nil {
		t.Fatal(err)
	}
	realArtifacts, err := filepath.EvalSymlinks(artifacts)
	if err != nil || !inside(realOwned, realArtifacts) {
		t.Fatal("browser240 artifact symlink escaped owned directory")
	}

	var dns, creates, queries, callbacks, merchantHits, h5Hits, bad atomic.Int64
	oldResolver := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) {
		dns.Add(1)
		return nil, fmt.Errorf("browser240 denies nonlocal Go DNS")
	}}
	t.Cleanup(func() { net.DefaultResolver = oldResolver })
	f := channel168Local(t)
	const apiKey = "0123456789abcdef0123456789abcdef"
	const appID, mchID = "synthetic-browser240", "1900000240"
	controlBytes := make([]byte, 32)
	if _, err := rand.Read(controlBytes); err != nil {
		t.Fatal(err)
	}
	control := hex.EncodeToString(controlBytes)
	type original struct {
		Order   payment.Order
		Attempt payment.ProviderAttempt
		QRHash  string
	}
	originals := map[string]original{}
	fail := func(w http.ResponseWriter, why string) {
		bad.Add(1)
		t.Errorf("local WeChat fixture: %s", why)
		http.Error(w, why, 400)
	}
	sdkPeer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		var doc struct {
			Fields []struct {
				XMLName xml.Name
				Value   string `xml:",chardata"`
			} `xml:",any"`
		}
		if r.Method != "POST" || xml.Unmarshal(raw, &doc) != nil {
			fail(w, "expected SDK XML POST")
			return
		}
		fields := map[string]string{}
		for _, item := range doc.Fields {
			if _, duplicate := fields[item.XMLName.Local]; duplicate {
				fail(w, "duplicate XML field")
				return
			}
			fields[item.XMLName.Local] = item.Value
		}
		if fields["appid"] != appID || fields["mch_id"] != mchID || !adapter.VerifyWechatV2(fields, apiKey, "MD5", fields["sign"]) {
			fail(w, "original SDK signature or account invalid")
			return
		}
		if r.URL.Path != "/pay/unifiedorder" {
			queries.Add(1)
			fail(w, "checkout must never query or recreate provider payment")
			return
		}
		method := fields["trade_type"]
		if fields["total_fee"] != "6650" || fields["out_trade_no"] == "" || fields["notify_url"] == "" || (method != "NATIVE" && method != "MWEB" && method != "JSAPI" && method != "APP") || method == "JSAPI" && fields["openid"] != "synthetic-openid240" {
			fail(w, "SDK amount, method, order or buyer changed")
			return
		}
		creates.Add(1)
		out := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": appID, "mch_id": mchID, "nonce_str": "synthetic240", "out_trade_no": fields["out_trade_no"]}
		switch method {
		case "NATIVE":
			out["code_url"] = "weixin://wxpay/bizpayurl?pr=original240-" + fields["out_trade_no"]
		case "MWEB":
			out["mweb_url"] = "https://wx.tenpay.com/cgi-bin/mmpayweb-bin/checkmweb?prepay_id=original240-" + fields["out_trade_no"] + "&package=synthetic240"
		default:
			out["prepay_id"] = "original240-" + fields["out_trade_no"]
		}
		w.Header().Set("Content-Type", "application/xml")
		w.Write(subchannel168BXML(out, apiKey))
	}))
	defer sdkPeer.Close()
	sdk, err := adapter.NewWechatV2(adapter.WechatV2Config{AppID: appID, MerchantID: mchID, APIKey: apiKey, Methods: []string{"native", "h5", "jsapi", "app"}, H5SiteURL: "https://merchant.example.invalid", H5SiteName: "Synthetic browser240", BaseURL: sdkPeer.URL, HTTPClient: sdkPeer.Client()})
	if err != nil {
		t.Fatal(err)
	}
	f.s.ProviderFlow.Providers[7] = sdk
	handler := f.s.Handler()
	var api *httptest.Server
	api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__wechat_browser240/confirm" {
			if r.Method != "POST" || r.Header.Get("X-Local-Wechat240") != control {
				http.Error(w, "forbidden", 403)
				return
			}
			var input struct {
				Trade string `json:"trade"`
			}
			if json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&input) != nil {
				fail(w, "bad local confirmation")
				return
			}
			original, exists := originals[input.Trade]
			if !exists || original.Order.OutTradeNo == "actual-browser240-closed" {
				fail(w, "unknown original confirmation")
				return
			}
			fields := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": appID, "mch_id": mchID, "nonce_str": "callback240", "out_trade_no": input.Trade, "transaction_id": "SYNTHETIC-WX240-" + input.Trade, "total_fee": "6650", "fee_type": "CNY", "openid": "synthetic-openid240"}
			signed := subchannel168BXML(fields, apiKey)
			// Invoke the actual public callback over its loopback socket, using
			// an exact-address transport; no store mutation simulates payment.
			apiURL, _ := url.Parse(api.URL)
			client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				if address != apiURL.Host {
					return nil, fmt.Errorf("callback escaped local API")
				}
				return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, address)
			}}}
			response, e := client.Post(api.URL+"/callbacks/providers/7/"+input.Trade, "application/xml", strings.NewReader(string(signed)))
			if e != nil {
				client.CloseIdleConnections()
				fail(w, "signed callback transport")
				return
			}
			body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
			response.Body.Close()
			client.CloseIdleConnections()
			if response.StatusCode != 200 || !strings.Contains(string(body), "SUCCESS") {
				fail(w, fmt.Sprintf("real signed callback rejected HTTP %d", response.StatusCode))
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/callbacks/providers/") {
			callbacks.Add(1)
		}
		handler.ServeHTTP(w, r)
	}))
	defer api.Close()
	f.s.ProviderFlow.LocalSimulation = true
	// Notify URLs satisfy the real adapter's HTTPS constraint during creation.
	// The peer stores no outgoing destination and all actual callbacks above
	// are bound to api.URL. Browser checkout/returns are loopback after creation.
	f.s.ProviderFlow.CallbackBaseURL = "https://pay.example.invalid"
	f.s.ProviderFlow.ReturnURLBuilder = f.s.ProviderReturnURL
	manifestOrders := map[string]map[string]any{}
	for _, method := range []string{"native", "h5", "jsapi", "app", "closed"} {
		actualMethod := method
		if method == "closed" {
			actualMethod = "native"
		}
		params := compat.Params{"pid": "1", "out_trade_no": "actual-browser240-" + method, "type": "wxpay", "name": "Synthetic browser240 商品", "money": "66.50", "channel_id": "7", "method": actualMethod, "clientip": "127.0.0.1", "notify_url": "https://merchant.example.invalid/notify", "return_url": "https://merchant.example.invalid/return", "sign_type": "MD5"}
		if method == "jsapi" {
			params["sub_openid"] = "synthetic-openid240"
		}
		params["sign"] = compat.SignMD5(params, "test-secret")
		values := url.Values{}
		for key, value := range params {
			values.Set(key, value)
		}
		request := httptest.NewRequest("POST", "/mapi.php", strings.NewReader(values.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		var created struct{ Code int }
		if json.Unmarshal(recorder.Body.Bytes(), &created) != nil || created.Code != 1 {
			t.Fatalf("synthetic %s create failed HTTP %d: %s", method, recorder.Code, recorder.Body.String())
		}
		order, err := f.s.Store.Order(t.Context(), 1, params["out_trade_no"])
		if err != nil {
			t.Fatal(err)
		}
		attempt, err := f.s.Store.ProviderAttempt(t.Context(), order.TradeNo)
		if err != nil {
			t.Fatal(err)
		}
		qrHash := ""
		if method == "native" {
			png, _ := qrcode.Encode(attempt.Material, qrcode.Medium, 320)
			hash := sha256.Sum256(png)
			qrHash = hex.EncodeToString(hash[:])
		}
		originals[order.TradeNo] = original{order, attempt, qrHash}
		manifestOrders[method] = map[string]any{"trade": order.TradeNo, "method": actualMethod, "material": attempt.Material, "materialType": attempt.MaterialType, "qrHash": qrHash}
		if method == "closed" {
			if _, err := f.p.Exec(t.Context(), `UPDATE orders SET status='closed' WHERE trade_no=$1`, order.TradeNo); err != nil {
				t.Fatal(err)
			}
		}
	}
	// The normal guarded factory is restored before any browser action. It has
	// its ordinary official destination; DNS denial makes an accidental query
	// fail closed. Credentials match the real local SDK-created originals.
	guarded, err := channelconfig.Build(channelconfig.Import{Version: 1, Channels: []channelconfig.Channel{{ID: 7, Plugin: "wxpay", AppTypes: []string{"1", "2", "3", "5"}, Config: map[string]string{"appid": appID, "appmchid": mchID, "appkey": apiKey, "h5_site_url": "https://merchant.example.invalid", "h5_site_name": "Synthetic browser240"}}}}, true)
	if err != nil {
		t.Fatal(err)
	}
	f.s.ProviderFlow.Providers[7] = guarded[7]
	f.s.ProviderFlow.CallbackBaseURL = api.URL
	for method, data := range manifestOrders {
		data["checkout"] = f.s.checkoutURL(data["trade"].(string), "checkout")
		manifestOrders[method] = data
	}
	peer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "merchant.example.invalid" {
			params, e := compat.Parse(r.URL.Query())
			original, exists := originals[params["trade_no"]]
			if r.Method != "GET" || r.URL.Path != "/return" || e != nil || !exists || params["pid"] != "1" || params["out_trade_no"] != original.Order.OutTradeNo || params["money"] != "66.50" || params["type"] != "wxpay" || params["trade_status"] != "TRADE_SUCCESS" || !compat.VerifyMD5(params, "test-secret", params["sign"]) {
				fail(w, "merchant original order or MD5 return invalid")
				return
			}
			merchantHits.Add(1)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			io.WriteString(w, `<title>Synthetic merchant return240</title><h1>Verified synthetic merchant return240</h1>`)
			return
		}
		if r.Host != "wx.tenpay.com" || r.Method != "GET" || r.URL.Path != "/cgi-bin/mmpayweb-bin/checkmweb" {
			fail(w, "unexpected virtual official TLS host or path")
			return
		}
		expected := manifestOrders["h5"]["material"].(string)
		if "https://"+r.Host+r.URL.RequestURI() != expected || r.Header.Get("Referer") != api.URL+"/" {
			fail(w, "H5 stored HTTPS material or strict origin Referer changed")
			return
		}
		h5Hits.Add(1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, `<meta name="viewport" content="width=device-width,initial-scale=1"><title>Local WeChat H5 peer240</title><h1>Original synthetic WeChat H5 order240</h1>`)
	}))
	defer peer.Close()
	manifest := map[string]any{"api": api.URL, "peerLocal": peer.URL, "control": control, "orders": manifestOrders}
	manifestJSON, _ := json.Marshal(manifest)
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "web", "e2e", "wechat-browser240-local.mjs"))
	cmd.Dir = filepath.Join(root, "web")
	cmd.Env = append(os.Environ(), "WECHAT_BROWSER240_FIXTURE="+string(manifestJSON), "GOEPAY_WECHAT_BROWSER240_ARTIFACTS="+artifacts)
	output, browserErr := cmd.CombinedOutput()
	if err := os.WriteFile(filepath.Join(artifacts, "node-browser.raw.log"), output, 0600); err != nil {
		t.Fatal(err)
	}
	if browserErr != nil {
		t.Fatalf("actual local WeChat browser failed: %v\n%s", browserErr, output)
	}
	var orders, attempts, receipts int
	if err := f.p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM orders),(SELECT count(*) FROM provider_attempts),(SELECT count(*) FROM verified_payment_receipts)`).Scan(&orders, &attempts, &receipts); err != nil {
		t.Fatal(err)
	}
	for trade, before := range originals {
		after, e := f.s.Store.ProviderAttempt(t.Context(), trade)
		if e != nil || after.Material != before.Attempt.Material || after.MaterialType != before.Attempt.MaterialType || after.Method != before.Attempt.Method || after.ChannelID != before.Attempt.ChannelID {
			t.Fatal("browser changed original provider material, method or channel", e)
		}
		order, e := f.s.Store.OrderByTradeNo(t.Context(), trade)
		wanted := "paid"
		if before.Order.OutTradeNo == "actual-browser240-closed" {
			wanted = "closed"
		}
		if e != nil || order.Status != wanted || order.Amount != 6650 || order.MerchantID != 1 || order.Type != "wxpay" {
			t.Fatal("signed callback did not preserve original order", e)
		}
	}
	if orders != 5 || attempts != 5 || receipts != 4 || creates.Load() != 5 || queries.Load() != 0 || callbacks.Load() != 4 || merchantHits.Load() < 4 || h5Hits.Load() != 2 || bad.Load() != 0 || dns.Load() != 0 {
		t.Fatalf("browser240 boundaries orders=%d attempts=%d receipts=%d creates=%d queries=%d callbacks=%d merchant_returns=%d h5_hits=%d bad=%d attempted_dns=%d", orders, attempts, receipts, creates.Load(), queries.Load(), callbacks.Load(), merchantHits.Load(), h5Hits.Load(), bad.Load(), dns.Load())
	}
	t.Log("browser240 actual desktop/mobile native/H5/JSAPI/APP checkout: original signed SDK material, four signed callbacks, local-only paid polling and verified merchant returns; native WeixinJSBridge simulated only at browser boundary")
}
