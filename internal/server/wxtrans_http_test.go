package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/channelconfig"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

// wxtransProvider is a 商家转账（新） channel double on channel 7.
type wxtransProvider struct {
	*channel168Provider
	queries atomic.Int32
	mu      sync.Mutex
	state   string
}

func (*wxtransProvider) ID() string                                     { return "wxpay" }
func (*wxtransProvider) TransferTypes() []string                        { return []string{"wxpay"} }
func (*wxtransProvider) ValidateTransfer(adapter.TransferRequest) error { return nil }
func (*wxtransProvider) UsesMerchantTransfer() bool                     { return true }
func (p *wxtransProvider) MerchantTransferConfirmation(pkg string) (string, string, string) {
	return "local-mch207", p.app, pkg
}
func (*wxtransProvider) Transfer(_ context.Context, r adapter.TransferRequest) (adapter.TransferResult, error) {
	return adapter.TransferResult{TransferNo: r.TransferNo, ProviderTransferNo: "1330000071100999991182020050700019480001", State: "PROCESSING", Amount: r.Amount, PackageInfo: "affffddafdfafddffda=="}, nil
}
func (p *wxtransProvider) QueryTransfer(_ context.Context, r adapter.TransferRequest) (adapter.TransferResult, error) {
	p.queries.Add(1)
	p.mu.Lock()
	state := p.state
	p.mu.Unlock()
	res := adapter.TransferResult{TransferNo: r.TransferNo, ProviderTransferNo: "1330000071100999991182020050700019480001", State: state, Amount: r.Amount}
	if state == "SUCCESS" {
		res.PayDate = "2026-10-28T12:05:00+08:00"
	}
	return res, nil
}
func (p *wxtransProvider) set(state string) { p.mu.Lock(); p.state = state; p.mu.Unlock() }

// wxtransSigner202 is the official-account JSSDK double.
type wxtransSigner202 struct{ pages []string }

func (*wxtransSigner202) Sign(context.Context, string) (adapter.WechatJSConfig202, error) {
	return adapter.WechatJSConfig202{}, adapter.ErrWechatJSSDK202
}
func (s *wxtransSigner202) SignMerchantTransfer(_ context.Context, page string) (adapter.WechatJSConfig202, error) {
	s.pages = append(s.pages, page)
	return adapter.WechatJSConfig202{AppID: "app168-seven", Timestamp: 1761624000, NonceStr: "nonce207", Signature: "sig207", JSAPIList: []string{"requestMerchantTransfer"}}, nil
}

const wxtransUA = "Mozilla/5.0 (iPhone) AppleWebKit/605.1.15 MicroMessenger/8.0.50"

func wxtransFixture(t *testing.T) (channel168Fixture, *wxtransProvider, *wxtransSigner202, string) {
	t.Helper()
	f := channel168Local(t)
	// Adapter-normalized state of a WAIT_USER_CONFIRM bill.
	p := &wxtransProvider{channel168Provider: &channel168Provider{app: "app168-seven"}, state: "PROCESSING"}
	f.s.ProviderFlow.Providers[7] = p
	f.s.ProviderFlow.AllowLive = true
	f.s.ProviderFlow.CallbackBaseURL = "https://pay.example.invalid"
	signer := &wxtransSigner202{}
	f.s.ScanJSProfiles202 = map[int64]channelconfig.ScanJSProfile202{3: {Profile: channelconfig.IdentityProfile{ID: 3, Provider: "wxpay", Kind: "web", AppID: "app168-seven", ChannelID: 7}, Signer: signer}}
	ctx := t.Context()
	if _, err := f.p.Exec(ctx, `UPDATE merchants SET profile=profile||'{"transfer_api_enabled":true,"transfer_rate":"0"}'::jsonb WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"transfer_enabled": "true", "transfer_wxpay_type": "1", "transfer_wxpay_scene_id": "1005", "transfer_wxpay_info_type": "岗位类型", "transfer_wxpay_info_content": "外卖员"} {
		if _, err := f.p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key',$1::text,'value',$2::text))`, key, value); err != nil {
			t.Fatal(err)
		}
	}
	o, err := f.s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "wxtrans-fund", Name: "fund", Type: "wxpay", Amount: 10000})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.Store.MarkPaid(ctx, o.TradeNo, "wxtrans-fund"); err != nil {
		t.Fatal(err)
	}
	tr, err := f.s.ProviderFlow.Transfer(ctx, 1, 7, "wxtrans-207", adapter.TransferRequest{Type: "wxpay", Account: "payee-openid", Amount: 123, Remark: "佣金报酬"})
	if err != nil || tr.Status != "pending" {
		t.Fatalf("bill not waiting for the payee: %+v %v", tr, err)
	}
	var created time.Time
	if err = f.p.QueryRow(ctx, "SELECT created_at FROM transfers WHERE id=$1", tr.ID).Scan(&created); err != nil {
		t.Fatal(err)
	}
	return f, p, signer, f.s.wxtransConfirmURL(tr.ID, created)
}

func wxtransCall(f channel168Fixture, link, ua string) *httptest.ResponseRecorder {
	u, _ := url.Parse(link)
	r := httptest.NewRequest(http.MethodGet, u.RequestURI(), nil)
	r.Header.Set("User-Agent", ua)
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func TestWxtransConfirmPageCarriesSignedRequestMerchantTransfer(t *testing.T) {
	f, p, signer, link := wxtransFixture(t)
	if !strings.HasPrefix(link, "https://pay.example.invalid/pay/wxtrans?") {
		t.Fatal("link", link)
	}
	w := wxtransCall(f, link, wxtransUA)
	body := w.Body.String()
	for _, want := range []string{"确认收款页面", "待你收款", "¥", "1.23", "转账时间", ">收款</button>", "1天内未确认，将退还给商家", "jweixin-1.6.0.js", `"package":"affffddafdfafddffda=="`, `"mchId":"local-mch207"`, `"appId":"app168-seven"`, `"jsApiList":["requestMerchantTransfer"]`, "requestMerchantTransfer:ok", "do=success"} {
		if !strings.Contains(body, want) {
			t.Fatalf("confirm page lacks %q: HTTP=%d %s", want, w.Code, body)
		}
	}
	csp := w.Header().Get("Content-Security-Policy")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(csp, "script-src 'nonce-") || !strings.Contains(csp, "https://res.wx.qq.com") || strings.Contains(csp, "unsafe-inline") {
		t.Fatalf("page headers: HTTP=%d csp=%q", w.Code, csp)
	}
	if len(signer.pages) != 1 || signer.pages[0] != link {
		t.Fatalf("JSSDK must sign the exact page URL %q, got %v", link, signer.pages)
	}
	if p.queries.Load() != 1 {
		t.Fatal("bill not refreshed before confirmation", p.queries.Load())
	}
}

func TestWxtransPageRefusesOutsideWeChatAndTamperedLinks(t *testing.T) {
	f, p, _, link := wxtransFixture(t)
	w := wxtransCall(f, link, "Mozilla/5.0 Chrome/120")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "请在微信客户端打开链接") || strings.Contains(w.Body.String(), "affffddafdfafddffda") {
		t.Fatalf("non-WeChat browser: HTTP=%d %s", w.Code, w.Body.String())
	}
	u, _ := url.Parse(link)
	q := u.Query()
	q.Set("n", "999999")
	if w := wxtransCall(f, "/pay/wxtrans?"+q.Encode(), wxtransUA); w.Code != 400 {
		t.Fatal("other transfer id accepted under the same signature", w.Code)
	}
	q = u.Query()
	q.Set("s", strings.Repeat("a", 64))
	if w := wxtransCall(f, "/pay/wxtrans?"+q.Encode(), wxtransUA); w.Code != 400 || !strings.Contains(w.Body.String(), "链接无效") {
		t.Fatal("tampered signature accepted", w.Code)
	}
	if p.queries.Load() != 0 {
		t.Fatal("refused page reached WeChat", p.queries.Load())
	}
	f.s.ScanJSProfiles202 = nil
	if w := wxtransCall(f, link, wxtransUA); w.Code != 503 || !strings.Contains(w.Body.String(), "支付通道绑定的微信公众号不存在") {
		t.Fatalf("missing official account: HTTP=%d %s", w.Code, w.Body.String())
	}
}

func TestWxtransPageShowsReceiptAndFailureFromRefreshedBill(t *testing.T) {
	f, p, _, link := wxtransFixture(t)
	if w := wxtransCall(f, link+"&do=success", wxtransUA); !strings.Contains(w.Body.String(), "你已收款，资金已存入零钱") || !strings.Contains(w.Body.String(), "收款时间") {
		t.Fatalf("success return: HTTP=%d %s", w.Code, w.Body.String())
	}
	p.set("SUCCESS")
	w := wxtransCall(f, link, wxtransUA)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "你已收款，资金已存入零钱") || !strings.Contains(w.Body.String(), "2026-10-28 12:05:00") || strings.Contains(w.Body.String(), "affffddafdfafddffda") {
		t.Fatalf("confirmed bill: HTTP=%d %s", w.Code, w.Body.String())
	}
	tr, err := f.s.Store.Transfer(t.Context(), 1, "wxtrans-207")
	if err != nil || tr.Status != "succeeded" {
		t.Fatalf("confirmed bill not settled: %+v %v", tr, err)
	}

	g, q, _, other := wxtransFixture(t)
	q.set("FAILED") // the adapter maps a CANCELLED bill to FAILED
	if w := wxtransCall(g, other, wxtransUA); w.Code != 200 || !strings.Contains(w.Body.String(), "转账已失败，请重新提交！") {
		t.Fatalf("closed bill: HTTP=%d %s", w.Code, w.Body.String())
	}
	if b, _ := g.s.Store.Balance(t.Context(), 1); b.Reserved != 0 {
		t.Fatalf("closed bill kept its reservation %+v", b)
	}
}

func TestWxtransSettingGapsUseOriginalWording(t *testing.T) {
	f := channel168Local(t)
	for err, want := range map[error]string{payment.ErrMerchantTransferOff: "微信V3转账产品类型未选择商家转账（新）", payment.ErrMerchantTransferScene: "未配置转账场景ID", payment.ErrMerchantTransferReport: "未配置转账报备信息"} {
		w := httptest.NewRecorder()
		f.s.fail(w, err)
		if w.Code != 422 || !strings.Contains(w.Body.String(), want) {
			t.Fatalf("%v: HTTP=%d %s", err, w.Code, w.Body.String())
		}
	}
}

func wxtransConsole(f channel168Fixture, role, cookie, csrf, target string) map[string]any {
	r := httptest.NewRequest(http.MethodGet, target, nil)
	r.Header.Set("X-CSRF-Token", csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: cookie})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	var out struct {
		Data map[string]any `json:"data"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		panic("console call failed: " + target + " " + w.Body.String())
	}
	return out.Data
}

func TestWxtransConsoleShowsConfirmLinkOnlyWhileWaiting(t *testing.T) {
	f, p, _, link := wxtransFixture(t)
	if _, err := f.p.Exec(t.Context(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) VALUES('wxtransmerchant','merchant',1,'Synthetic Merchant','unused-synthetic-only','operator')`); err != nil {
		t.Fatal(err)
	}
	merchant, merchantCSRF, err := f.s.Sessions.Create(identity.User{ID: 1, Role: "merchant", Username: "wxtransmerchant", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	tr, err := f.s.Store.Transfer(t.Context(), 1, "wxtrans-207")
	if err != nil {
		t.Fatal(err)
	}
	for _, who := range [][3]string{{"admin", f.token, f.csrf}, {"merchant", merchant, merchantCSRF}} {
		list := wxtransConsole(f, who[0], who[1], who[2], "/console/v1/"+who[0]+"/transfers")
		items, _ := list["items"].([]any)
		if len(items) != 1 || items[0].(map[string]any)["confirm_url"] != link {
			t.Fatalf("%s list lacks the payee link: %v", who[0], list)
		}
		detail := wxtransConsole(f, who[0], who[1], who[2], "/console/v1/"+who[0]+"/transfers/"+strconv.FormatInt(tr.ID, 10))
		if detail["confirm_url"] != link {
			t.Fatalf("%s detail lacks the payee link: %v", who[0], detail)
		}
	}
	// A settled bill no longer offers the QR.
	p.set("SUCCESS")
	if _, err = f.s.ProviderFlow.QueryTransfer(t.Context(), 1, "wxtrans-207"); err != nil {
		t.Fatal(err)
	}
	list := wxtransConsole(f, "admin", f.token, f.csrf, "/console/v1/admin/transfers")
	if items, _ := list["items"].([]any); len(items) != 1 || items[0].(map[string]any)["confirm_url"] != nil {
		t.Fatalf("settled bill still offers the QR: %v", list)
	}
}
