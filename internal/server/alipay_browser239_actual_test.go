package server

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	qrcode "github.com/skip2/go-qrcode"
	"goepay/internal/channelconfig"
	"goepay/internal/compat"
	"goepay/internal/payment"
)

func alipayActual239Canonical(fields map[string]string, notification bool) string {
	keys := make([]string, 0, len(fields))
	for key, value := range fields {
		if value != "" && key != "sign" && (!notification || key != "sign_type") {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+fields[key])
	}
	return strings.Join(parts, "&")
}

func alipayActual239Fields(material string) map[string]string {
	fields := map[string]string{}
	for _, match := range regexp.MustCompile(`<input type="hidden" name="([^"]+)" value="([^"]*)">`).FindAllStringSubmatch(material, -1) {
		fields[match[1]] = html.UnescapeString(match[2])
	}
	return fields
}

func TestAlipayBrowser239ActualBrowser(t *testing.T) {
	if os.Getenv("GOEPAY_ALIPAY_BROWSER239") != "1" {
		t.Skip("GOEPAY_ALIPAY_BROWSER239=1 requires existing local Chromium, Playwright and synthetic PostgreSQL")
	}
	databaseURL := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("synthetic GOEPAY_TEST_DATABASE_URL required")
	}
	databaseConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("invalid synthetic database configuration")
	}
	loopback := func(host string) bool {
		return host == "localhost" || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
	}
	if !loopback(databaseConfig.ConnConfig.Host) {
		t.Fatal("browser239 requires an existing loopback synthetic PostgreSQL")
	}
	for _, fallback := range databaseConfig.ConnConfig.Fallbacks {
		if !loopback(fallback.Host) {
			t.Fatal("browser239 database fallback must remain on loopback")
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
	artifacts := os.Getenv("GOEPAY_ALIPAY_BROWSER239_ARTIFACTS")
	if artifacts == "" {
		t.Skip("GOEPAY_ALIPAY_BROWSER239_ARTIFACTS must identify an owned-run artifact directory")
	}
	artifacts, err = filepath.Abs(artifacts)
	owned := filepath.Join(root, ".tools", "alipay-checkout239", "owned-run")
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(owned, artifacts)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatal("browser239 artifacts must remain inside .tools/alipay-checkout239/owned-run")
	}
	if err := os.MkdirAll(artifacts, 0700); err != nil {
		t.Fatal(err)
	}
	resolvedArtifacts, err := filepath.EvalSymlinks(artifacts)
	if err != nil {
		t.Fatal(err)
	}
	resolvedOwned, err := filepath.EvalSymlinks(owned)
	if err != nil {
		t.Fatal(err)
	}
	if relative, err = filepath.Rel(resolvedOwned, resolvedArtifacts); err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatal("browser239 artifacts resolved outside the owned directory")
	}
	// The real guarded SDK keeps its normal client. Deny any attempted Go DNS
	// lookup before constructing it, so an accidental provider/query I/O cannot
	// reach an official service. PostgreSQL and callback sockets use loopback IPs.
	var unexpectedDNS atomic.Int64
	originalResolver := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) {
		unexpectedDNS.Add(1)
		return nil, fmt.Errorf("browser239 denies nonlocal Go DNS requests")
	}}
	t.Cleanup(func() { net.DefaultResolver = originalResolver })
	f := channel168Local(t)
	merchant, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	platform, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	providers, err := channelconfig.Build(channelconfig.Import{Version: 1, Channels: []channelconfig.Channel{{ID: 7, Plugin: "alipay", AppTypes: []string{"1", "2"}, Config: map[string]string{"appid": "synthetic-browser239", "appmchid": "2088000000000239", "appsecret": parent180Private(merchant), "appkey": parent180Public(platform)}}}}, true)
	if err != nil {
		t.Fatal(err)
	}
	f.s.ProviderFlow.Providers[7] = providers[7]
	var statusReads, callbacks, gatewayPosts, cashierPages, confirms, merchantHits, bad atomic.Int64
	handler := f.s.Handler()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/status") {
			statusReads.Add(1)
		}
		if strings.HasPrefix(r.URL.Path, "/callbacks/providers/") {
			callbacks.Add(1)
		}
		handler.ServeHTTP(w, r)
	}))
	defer api.Close()
	f.s.ProviderFlow.CallbackBaseURL = api.URL
	f.s.ProviderFlow.LocalSimulation = true
	f.s.ProviderFlow.ReturnURLBuilder = f.s.ProviderReturnURL
	type originalOrder struct {
		Order   payment.Order
		Attempt payment.ProviderAttempt
		Fields  map[string]string
	}
	originals := map[string]originalOrder{}
	manifest := map[string]string{"api": api.URL}
	for _, method := range []string{"page", "wap"} {
		params := compat.Params{"pid": "1", "out_trade_no": "actual-browser239-" + method, "type": "alipay", "name": "Synthetic browser239 商品", "money": "66.50", "channel_id": "7", "method": method, "notify_url": "https://merchant.example.invalid/notify", "return_url": "https://merchant.example.invalid/return", "sign_type": "MD5"}
		params["sign"] = compat.SignMD5(params, "test-secret")
		values := url.Values{}
		for key, value := range params {
			values.Set(key, value)
		}
		r := httptest.NewRequest("POST", "/mapi.php", strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		var created struct{ Code int }
		if json.Unmarshal(w.Body.Bytes(), &created) != nil || created.Code != 1 {
			t.Fatalf("synthetic %s create failed: HTTP %d", method, w.Code)
		}
		order, err := f.s.Store.Order(t.Context(), 1, params["out_trade_no"])
		if err != nil {
			t.Fatal(err)
		}
		attempt, err := f.s.Store.ProviderAttempt(t.Context(), order.TradeNo)
		if err != nil {
			t.Fatal(err)
		}
		originals[order.TradeNo] = originalOrder{order, attempt, alipayActual239Fields(attempt.Material)}
		manifest[method+"Trade"] = order.TradeNo
		manifest[method+"Checkout"] = f.s.checkoutURL(order.TradeNo, "checkout")
	}
	type cashierSession struct{ trade, returnURL string }
	var mu sync.Mutex
	sessions := map[string]cashierSession{}
	fail := func(w http.ResponseWriter, reason string) {
		bad.Add(1)
		t.Errorf("local Alipay fixture rejected browser input: %s", reason)
		http.Error(w, reason, http.StatusBadRequest)
	}
	// The only sender destination is the exact local API listener. Neither the
	// notification client nor Playwright follows an external redirect over TCP.
	apiURL, _ := url.Parse(api.URL)
	callbackClient := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != apiURL.Host {
			return nil, fmt.Errorf("callback escaped the loopback fixture")
		}
		return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, address)
	}}}
	defer callbackClient.CloseIdleConnections()
	cashier := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "merchant.example.invalid" {
			params, err := compat.Parse(r.URL.Query())
			original, exists := originals[params["trade_no"]]
			if r.Method != "GET" || r.URL.Path != "/return" || err != nil || !exists || params["pid"] != "1" || params["out_trade_no"] != original.Order.OutTradeNo || params["money"] != "66.50" || params["type"] != "alipay" || params["trade_status"] != "TRADE_SUCCESS" || !compat.VerifyMD5(params, "test-secret", params["sign"]) {
				fail(w, "synthetic merchant return signature or original order invalid")
				return
			}
			merchantHits.Add(1)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			io.WriteString(w, `<title>Synthetic merchant return239</title><h1>Verified synthetic merchant return239</h1>`)
			return
		}
		if r.Host != "excashier.alipay.com" {
			fail(w, "unexpected virtual TLS cashier host")
			return
		}
		if r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/fixture/") {
			id := strings.TrimPrefix(r.URL.Path, "/fixture/")
			mu.Lock()
			_, exists := sessions[id]
			mu.Unlock()
			if !exists {
				fail(w, "unknown cashier session")
				return
			}
			cashierPages.Add(1)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, `<!doctype html><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Local Alipay cashier239</title><style>html,body{margin:0;padding:0;font:14px/18px sans-serif}h1{margin:4px 0;font:inherit;text-align:center;white-space:nowrap}img{display:block;margin:0 auto}form{margin:4px 0 0}button{display:block;box-sizing:border-box;height:24px;margin:0 auto;padding:1px 8px;font:12px/18px sans-serif}</style><h1>本地合成支付宝收银台</h1><img width="230" height="230" alt="Local Alipay QR239" src="/qr.png?session=%s"><form method="post" action="/confirm"><input type="hidden" name="session" value="%s"><button type="submit">Confirm synthetic payment239</button></form>`, id, id)
			return
		}
		if r.Method == "GET" && r.URL.Path == "/qr.png" {
			mu.Lock()
			session, exists := sessions[r.URL.Query().Get("session")]
			mu.Unlock()
			if !exists {
				fail(w, "unknown QR session")
				return
			}
			png, err := qrcode.Encode("synthetic-alipay239:"+session.trade, qrcode.Medium, 230)
			if err != nil {
				fail(w, "synthetic QR generation")
				return
			}
			w.Header().Set("Content-Type", "image/png")
			w.Write(png)
			return
		}
		if r.Method != "POST" || r.URL.Path != "/confirm" || r.ParseForm() != nil {
			fail(w, "unexpected cashier operation")
			return
		}
		mu.Lock()
		session, exists := sessions[r.PostForm.Get("session")]
		mu.Unlock()
		if !exists {
			fail(w, "unknown confirmation session")
			return
		}
		fields := map[string]string{"app_id": "synthetic-browser239", "seller_id": "2088000000000239", "out_trade_no": session.trade, "trade_no": "SYNTHETIC-ALIPAY239-" + session.trade, "total_amount": "66.50", "trade_status": "TRADE_SUCCESS", "buyer_id": "2088000000001239", "sign_type": "RSA2"}
		digest := sha256.Sum256([]byte(alipayActual239Canonical(fields, true)))
		signature, err := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, digest[:])
		if err != nil {
			fail(w, "local notification signing")
			return
		}
		fields["sign"] = base64.StdEncoding.EncodeToString(signature)
		notification := url.Values{}
		for key, value := range fields {
			notification.Set(key, value)
		}
		response, err := callbackClient.PostForm(api.URL+"/callbacks/providers/7/"+session.trade, notification)
		if err != nil {
			fail(w, "local signed callback transport")
			return
		}
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		response.Body.Close()
		if response.StatusCode != 200 || strings.TrimSpace(string(body)) != "success" {
			fail(w, fmt.Sprintf("real RSA2 callback rejected: status=%d", response.StatusCode))
			return
		}
		confirms.Add(1)
		http.Redirect(w, r, session.returnURL, http.StatusSeeOther)
	}))
	defer cashier.Close()
	gateway := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "openapi.alipay.com" || r.Method != "POST" || r.URL.Path != "/gateway.do" || r.URL.Query().Get("charset") != "utf-8" || r.ParseForm() != nil {
			fail(w, "expected UTF-8 browser POST")
			return
		}
		fields := map[string]string{}
		for key, values := range r.PostForm {
			if len(values) != 1 {
				fail(w, "duplicate submitted field")
				return
			}
			fields[key] = values[0]
		}
		signature, err := base64.StdEncoding.Strict().DecodeString(fields["sign"])
		digest := sha256.Sum256([]byte(alipayActual239Canonical(fields, false)))
		if err != nil || rsa.VerifyPKCS1v15(&merchant.PublicKey, crypto.SHA256, digest[:], signature) != nil {
			fail(w, "submitted original RSA2 signature invalid")
			return
		}
		var biz map[string]json.RawMessage
		if json.Unmarshal([]byte(fields["biz_content"]), &biz) != nil {
			fail(w, "submitted business JSON invalid")
			return
		}
		var trade string
		json.Unmarshal(biz["out_trade_no"], &trade)
		original, exists := originals[trade]
		if !exists || len(fields) != len(original.Fields) || fields["method"] != "alipay.trade."+original.Attempt.Method+".pay" || fields["app_id"] != "synthetic-browser239" || string(biz["total_amount"]) != `"66.50"` || string(biz["seller_id"]) != `"2088000000000239"` {
			fail(w, "submitted amount, merchant, original order or method changed")
			return
		}
		for key, value := range original.Fields {
			if key != "sign" && key != "timestamp" && key != "return_url" && key != "biz_content" && fields[key] != value {
				fail(w, "submitted protected field changed: "+key)
				return
			}
		}
		var before map[string]json.RawMessage
		json.Unmarshal([]byte(original.Fields["biz_content"]), &before)
		if _, desktop := biz["qr_pay_mode"]; desktop {
			if original.Attempt.Method != "page" || string(biz["qr_pay_mode"]) != `"4"` || string(biz["qrcode_width"]) != `"230"` {
				fail(w, "desktop QR settings changed original method")
				return
			}
			delete(biz, "qr_pay_mode")
			delete(biz, "qrcode_width")
		} else if fields["biz_content"] != original.Fields["biz_content"] {
			fail(w, "mobile business JSON changed")
			return
		}
		if !reflect.DeepEqual(biz, before) || !strings.HasPrefix(fields["return_url"], api.URL+"/checkout/"+trade+"/return?") {
			fail(w, "business values or verified local return changed")
			return
		}
		id := strconv.FormatInt(gatewayPosts.Add(1), 10)
		mu.Lock()
		sessions[id] = cashierSession{trade, fields["return_url"]}
		mu.Unlock()
		http.Redirect(w, r, "https://excashier.alipay.com/fixture/"+id, http.StatusSeeOther)
	}))
	defer gateway.Close()
	manifest["gatewayLocal"] = gateway.URL
	manifest["cashierLocal"] = cashier.URL
	manifestBytes, _ := json.Marshal(manifest)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "web", "e2e", "alipay-browser239-local.mjs"))
	cmd.Dir = filepath.Join(root, "web")
	cmd.Env = append(os.Environ(), "ALIPAY_BROWSER239_FIXTURE="+string(manifestBytes), "GOEPAY_ALIPAY_BROWSER239_ARTIFACTS="+artifacts)
	output, browserErr := cmd.CombinedOutput()
	if err := os.WriteFile(filepath.Join(artifacts, "node-browser.raw.log"), output, 0600); err != nil {
		t.Fatal(err)
	}
	if browserErr != nil {
		t.Fatalf("actual local Alipay browser failed: %v\n%s", browserErr, output)
	}
	var orders, attempts, receipts int
	if err := f.p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM orders),(SELECT count(*) FROM provider_attempts),(SELECT count(*) FROM verified_payment_receipts)`).Scan(&orders, &attempts, &receipts); err != nil {
		t.Fatal(err)
	}
	for trade, original := range originals {
		after, err := f.s.Store.ProviderAttempt(t.Context(), trade)
		if err != nil || after.Material != original.Attempt.Material || after.Method != original.Attempt.Method || after.ChannelID != original.Attempt.ChannelID {
			t.Fatal("actual browser changed original attempt material, method or channel", err)
		}
		paid, err := f.s.Store.OrderByTradeNo(t.Context(), trade)
		if err != nil || paid.Status != "paid" || paid.Amount != 6650 || paid.MerchantID != 1 {
			t.Fatal("real signed Alipay callback did not settle the original synthetic order", err)
		}
	}
	if orders != 2 || attempts != 2 || receipts != 2 || gatewayPosts.Load() != 3 || cashierPages.Load() != 3 || confirms.Load() != 2 || callbacks.Load() != 2 || merchantHits.Load() != 4 || statusReads.Load() < 2 || bad.Load() != 0 || unexpectedDNS.Load() != 0 {
		t.Fatalf("browser boundaries orders=%d attempts=%d receipts=%d gateway_posts=%d cashier_pages=%d confirms=%d callbacks=%d merchant_hits=%d status_reads=%d bad=%d attempted_dns=%d", orders, attempts, receipts, gatewayPosts.Load(), cashierPages.Load(), confirms.Load(), callbacks.Load(), merchantHits.Load(), statusReads.Load(), bad.Load(), unexpectedDNS.Load())
	}
	t.Logf("actual browser239: four scenarios at two widths, two original orders/attempts, three independently verified browser POSTs, two RSA2 callbacks, %d local status reads; virtual official and synthetic merchant origins mapped to two loopback TLS peers only", statusReads.Load())
}
