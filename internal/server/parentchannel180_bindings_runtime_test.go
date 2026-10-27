package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/channelconfig"
	"goepay/internal/compat"
)

type binding180Observed struct{ path, app string }
type binding180Transport struct {
	http.RoundTripper
	mu       *sync.Mutex
	observed *[]binding180Observed
}

func (r binding180Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	req.Body = io.NopCloser(bytes.NewReader(raw))
	var doc struct {
		Fields []struct {
			XMLName xml.Name
			Value   string `xml:",chardata"`
		} `xml:",any"`
	}
	if xml.Unmarshal(raw, &doc) == nil {
		app := ""
		for _, field := range doc.Fields {
			if field.XMLName.Local == "appid" {
				app = field.Value
			}
		}
		r.mu.Lock()
		*r.observed = append(*r.observed, binding180Observed{req.URL.Path, app})
		r.mu.Unlock()
	}
	return r.RoundTripper.RoundTrip(req)
}

type binding180TLSFactory struct {
	*parent180TLSFactory
	mu         sync.Mutex
	observed   []binding180Observed
	miniClient *http.Client
}

func (f *binding180TLSFactory) localBindings(raw []byte) (adapter.ParentChannelExecution, error) {
	execution, err := f.parent180TLSFactory.local(raw)
	if err != nil {
		return execution, err
	}
	if execution.PaymentSelection == nil {
		return execution, nil
	}
	var source struct {
		Channel  channelconfig.Channel `json:"channel"`
		Bindings struct {
			Mini *channelconfig.IdentityProfileSpec `json:"mini"`
		} `json:"weixin_bindings"`
	}
	if json.Unmarshal(raw, &source) != nil {
		return execution, adapter.ErrInvalid
	}
	if f.miniClient != nil && source.Bindings.Mini != nil && execution.Mini != nil {
		spec := source.Bindings.Mini
		execution.Mini, err = adapter.NewWechatMiniOAuth(adapter.WechatOAuthConfig{AppID: spec.AppID, AppSecret: spec.Config["appsecret"], HTTPClient: f.miniClient})
		if err != nil {
			return execution, err
		}
	}
	choose := execution.PaymentSelection
	base := execution.Payment
	execution.PaymentSelection = func(method string, mini bool, mid int64) (adapter.Provider, string, error) {
		selected, variant, err := choose(method, mini, mid)
		if err != nil {
			return nil, "", err
		}
		if variant == "" {
			return base, "", nil
		}
		if f.localProvider != nil {
			raw, err := json.Marshal(source.Channel)
			if err != nil {
				return nil, "", err
			}
			var c channelconfig.Channel
			if json.Unmarshal(raw, &c) != nil {
				return nil, "", adapter.ErrInvalid
			}
			c.Config["appid"] = selected.(adapter.ApplicationIdentity).ApplicationID()
			if launch, ok := selected.(adapter.MiniLaunchProvider); ok {
				if metadata, enabled := launch.MiniProgram(); enabled {
					c.Config["enable_mini_launch"] = "true"
					c.Config["mini_appid"] = metadata.AppID
					c.Config["mini_path"] = metadata.Path
				}
			}
			p, err := f.localProvider(c)
			return p, variant, err
		}
		pair, err := tls.X509KeyPair([]byte(source.Channel.Certificates["sslcert_path"]), []byte(source.Channel.Certificates["sslkey_path"]))
		if err != nil {
			return nil, "", err
		}
		transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: f.roots, Certificates: []tls.Certificate{pair}}}
		client := &http.Client{Timeout: 2 * time.Second, Transport: binding180Transport{RoundTripper: transport, mu: &f.mu, observed: &f.observed}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		var program *adapter.MiniProgramConfig
		if launch, ok := selected.(adapter.MiniLaunchProvider); ok {
			if p, enabled := launch.MiniProgram(); enabled {
				program = &p
			}
		}
		p, err := adapter.NewWechatV2(adapter.WechatV2Config{AppID: selected.(adapter.ApplicationIdentity).ApplicationID(), MerchantID: source.Channel.Config["appmchid"], APIKey: source.Channel.Config["appkey"], Methods: selected.Capabilities().Methods, MiniProgram: program, BaseURL: f.endpoint, HTTPClient: client, RefundClient: client})
		return p, variant, err
	}
	return execution, nil
}
func (f *binding180TLSFactory) BootstrapSnapshot(id int64) (adapter.ParentChannelExecution, []byte, error) {
	_, raw, err := f.ParentChannelFactory.BootstrapSnapshot(id)
	if err != nil {
		return adapter.ParentChannelExecution{}, nil, err
	}
	out, err := f.localBindings(raw)
	return out, raw, err
}
func (f *binding180TLSFactory) NewSnapshot(id int64, in adapter.ParentChannelAccountChange) (adapter.ParentChannelExecution, []byte, error) {
	_, raw, err := f.ParentChannelFactory.NewSnapshot(id, in)
	if err != nil {
		return adapter.ParentChannelExecution{}, nil, err
	}
	out, err := f.localBindings(raw)
	return out, raw, err
}
func (f *binding180TLSFactory) ChangeSnapshot(id int64, raw []byte, in adapter.ParentChannelAccountChange) (adapter.ParentChannelExecution, []byte, error) {
	_, next, err := f.ParentChannelFactory.ChangeSnapshot(id, raw, in)
	if err != nil {
		return adapter.ParentChannelExecution{}, nil, err
	}
	out, err := f.localBindings(next)
	return out, next, err
}
func (f *binding180TLSFactory) CopySnapshot(from, id int64, raw []byte, in adapter.ParentChannelAccountChange) (adapter.ParentChannelExecution, []byte, error) {
	_, next, err := f.ParentChannelFactory.CopySnapshot(from, id, raw, in)
	if err != nil {
		return adapter.ParentChannelExecution{}, nil, err
	}
	out, err := f.localBindings(next)
	return out, next, err
}
func (f *binding180TLSFactory) Restore(id int64, raw []byte) (adapter.ParentChannelExecution, error) {
	return f.localBindings(raw)
}

func binding180Payment(t *testing.T, f channel168Fixture, id, no, app string, mini bool) map[string]any {
	t.Helper()
	p := compat.Params{"pid": "1", "out_trade_no": no, "type": "wxpay", "name": "synthetic binding180", "money": "100.00", "channel_id": id, "method": "jsapi", "sub_appid": app, "sub_openid": "synthetic-openid-" + app, "sign_type": "MD5"}
	if mini {
		p["is_applet"] = "1"
	}
	p["sign"] = compat.SignMD5(p, "test-secret")
	body := url.Values{}
	for k, v := range p {
		body.Set(k, v)
	}
	req := httptest.NewRequest("POST", "/mapi.php", strings.NewReader(body.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, req)
	var out map[string]any
	if json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	return out
}

func binding180Ready(t *testing.T) (parent180Runtime, *binding180TLSFactory, channelconfig.IdentityProfileSpec, channelconfig.IdentityProfileSpec, map[string]any) {
	setup := parent180RuntimeReady(t)
	f := setup.f
	original := f.s.ProviderFlow.ParentFactories[8].(*parent180TLSFactory)
	web := channelconfig.IdentityProfileSpec{ID: 81, Revision: 1, ChannelID: 8, Enabled: true, Provider: "wxpay", Kind: "web", AppID: "synthetic-web-A", AccountMode: "direct", MerchantIDs: channelconfig.IdentityMerchantIDs{1}, Config: map[string]string{"appsecret": "synthetic-web-secret-A"}}
	mini := channelconfig.IdentityProfileSpec{ID: 82, Revision: 1, ChannelID: 8, Enabled: true, Provider: "wxpay", Kind: "mini", AppID: "synthetic-mini-A", AccountMode: "direct", Path: "pages/pay", MerchantIDs: channelconfig.IdentityMerchantIDs{1}, Config: map[string]string{"appsecret": "synthetic-mini-secret-A"}}
	factory, err := channelconfig.NewParentChannelFactory(original.Bootstrap(), channelconfig.ParentBuildOptions{BuildOptions: channelconfig.BuildOptions{AllowLive: true}, IdentityProfiles: []channelconfig.IdentityProfileSpec{web, mini}, OAuthEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	original.ParentChannelFactory = factory
	local := &binding180TLSFactory{parent180TLSFactory: original}
	f.s.ProviderFlow.ParentFactories[8] = local
	patch, _ := json.Marshal(map[string]any{"expected_fingerprint": setup.state["fingerprint"], "account": map[string]any{"app_types": []string{"1", "2"}, "appwxmp": "81", "appwxa": "82", "expected_bindings_fingerprint": factory.ParentWeixinCatalog().Fingerprint}})
	w := parent180HTTP(f, "PATCH", "/"+setup.id, string(patch))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var current struct{ Data map[string]any }
	json.Unmarshal(w.Body.Bytes(), &current)
	return setup, local, web, mini, current.Data
}

func TestParentChannel180WeixinBindingsActualTLSOriginalWebAndMiniSDKSurviveRotation(t *testing.T) {
	setup, local, web, mini, state := binding180Ready(t)
	f := setup.f
	ctx := context.Background()
	original := local.parent180TLSFactory
	var current struct{ Data map[string]any }
	current.Data = state
	var err error
	actualDTO := map[string]any{"scope": "Actual HTTP from a disposed synthetic database schema; no provider credentials", "bound": state}
	if os.Getenv("GOEPAY_PARENT_BINDINGS180_EXPORT") != "" {
		read := parent180HTTP(f, "GET", "/"+setup.id+"/weixin-options", "")
		var envelope struct{ Data json.RawMessage }
		if read.Code != 200 || json.Unmarshal(read.Body.Bytes(), &envelope) != nil {
			t.Fatal("actual selector export unavailable")
		}
		actualDTO["catalog"] = envelope.Data
	}
	for _, test := range []struct {
		no, app string
		mini    bool
	}{{"binding180-web-A", web.AppID, false}, {"binding180-mini-A", mini.AppID, true}} {
		out := binding180Payment(t, f, setup.id, test.no, test.app, test.mini)
		if out["code"] != float64(1) {
			t.Fatal("bound signed payment rejected", test.no, out)
		}
	}
	webOrder, err := f.s.Store.Order(ctx, 1, "binding180-web-A")
	if err != nil {
		t.Fatal(err)
	}
	miniOrder, err := f.s.Store.Order(ctx, 1, "binding180-mini-A")
	if err != nil {
		t.Fatal(err)
	}
	var variant string
	var selectedHash int
	if err = f.p.QueryRow(ctx, `SELECT facts->>'PaymentVariant',length(decode(trim(both '"' from (facts->'ExecutionAccountHash')::text),'base64')) FROM runtime_parent_payment_sources WHERE merchant_id=1 AND out_trade_no=$1`, webOrder.OutTradeNo).Scan(&variant, &selectedHash); err != nil || variant != "web" || selectedHash != 32 {
		t.Fatal("selected SDK provenance absent", variant, selectedHash, err)
	}
	web.AppID = "synthetic-web-B"
	web.Revision = 2
	web.Config = map[string]string{"appsecret": "synthetic-web-secret-B"}
	mini.AppID = "synthetic-mini-B"
	mini.Revision = 2
	mini.Config = map[string]string{"appsecret": "synthetic-mini-secret-B"}
	next, err := channelconfig.NewParentChannelFactory(original.Bootstrap(), channelconfig.ParentBuildOptions{BuildOptions: channelconfig.BuildOptions{AllowLive: true}, IdentityProfiles: []channelconfig.IdentityProfileSpec{web, mini}, OAuthEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	original.ParentChannelFactory = next
	patch, _ := json.Marshal(map[string]any{"expected_fingerprint": current.Data["fingerprint"], "account": map[string]any{"appwxmp": "81", "appwxa": "82", "expected_bindings_fingerprint": next.ParentWeixinCatalog().Fingerprint, "config": map[string]any{"appkey": map[string]string{"operation": "replace", "value": setup.keyB}}, "certificates": map[string]any{"sslcert_path": map[string]string{"operation": "replace", "value": setup.certB}, "sslkey_path": map[string]string{"operation": "replace", "value": setup.pemB}}}})
	w := parent180HTTP(f, "PATCH", "/"+setup.id, string(patch))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if output := os.Getenv("GOEPAY_PARENT_BINDINGS180_EXPORT"); output != "" {
		resolved, err := filepath.Abs(output)
		if err != nil || !strings.HasPrefix(filepath.ToSlash(resolved), "E:/GOEPAY/.tools/") {
			t.Fatal("explicit local disposable evidence path required")
		}
		var envelope struct{ Data json.RawMessage }
		if json.Unmarshal(w.Body.Bytes(), &envelope) != nil {
			t.Fatal("actual parent DTO invalid")
		}
		actualDTO["rotated"] = envelope.Data
		raw, err := json.MarshalIndent(actualDTO, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		for _, private := range []string{"synthetic-web-secret-A", "synthetic-mini-secret-A", setup.keyB, setup.pemB} {
			if bytes.Contains(raw, []byte(private)) {
				t.Fatal("private material in actual UI fixture")
			}
		}
		file, err := os.OpenFile(resolved, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = file.Write(raw); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err = file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	setup.expectedCreate.Store("B")
	if out := binding180Payment(t, f, setup.id, "binding180-web-B", web.AppID, false); out["code"] != float64(1) {
		t.Fatal(out)
	}
	for _, order := range []struct{ trade, app string }{{webOrder.TradeNo, "synthetic-web-A"}, {miniOrder.TradeNo, "synthetic-mini-A"}} {
		callback := parent180SignedXML(map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": order.app, "mch_id": "synthetic180-fresh-mch", "nonce_str": "binding-callback", "out_trade_no": order.trade, "transaction_id": "WX180_" + order.trade, "total_fee": "10000", "fee_type": "CNY", "openid": "synthetic-openid-" + order.app}, "abcdef0123456789abcdef0123456789")
		id, _ := strconv.ParseInt(setup.id, 10, 64)
		if err = f.s.ProviderFlow.HandleNotification(ctx, id, order.trade, http.Header{}, callback); err != nil {
			t.Fatal("original callback changed application", err)
		}
		req := httptest.NewRequest("POST", "/callbacks/providers/"+setup.id+"/"+order.trade, bytes.NewReader(callback))
		req.Header.Set("Content-Type", "application/xml")
		result := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(result, req)
		if result.Code != 200 {
			t.Fatal("original bound HTTP callback changed application", result.Code, result.Body.String())
		}
		if _, err = f.s.ProviderFlow.Query(ctx, 1, order.trade); err != nil {
			t.Fatal("original query changed SDK", err)
		}
		if _, err = f.s.ProviderFlow.Refund(ctx, 1, order.trade, "binding180-refund-"+order.app, 1000); err != nil {
			t.Fatal("original refund changed SDK", err)
		}
	}
	if setup.creates.Load() != 3 || setup.queries.Load() != 2 || setup.refunds.Load() != 2 || setup.bad.Load() != 0 {
		t.Fatal("unexpected original provider effects", setup.creates.Load(), setup.queries.Load(), setup.refunds.Load(), setup.bad.Load())
	}
	local.mu.Lock()
	defer local.mu.Unlock()
	counts := map[string]int{}
	for _, request := range local.observed {
		counts[request.path+"|"+request.app]++
	}
	for _, app := range []string{"synthetic-web-A", "synthetic-mini-A"} {
		for _, path := range []string{"/pay/unifiedorder", "/pay/orderquery", "/secapi/pay/refund"} {
			if counts[path+"|"+app] != 1 {
				t.Fatal("actual TLS SDK drifted", path, app, counts)
			}
		}
	}
	if counts["/pay/unifiedorder|synthetic-web-B"] != 1 {
		t.Fatal("new payment ignored rotated profile", counts)
	}
}

func TestParentChannel180WeixinBindingsCurrentProfileAuthorityAndOriginalSourceCannotBeBorrowed(t *testing.T) {
	for _, scenario := range []string{"source_case_alias", "source_missing", "account_tampered", "current_profile_merchant_removed"} {
		t.Run(scenario, func(t *testing.T) {
			setup, local, web, mini, _ := binding180Ready(t)
			f := setup.f
			ctx := t.Context()
			if out := binding180Payment(t, f, setup.id, "binding180-boundary", web.AppID, false); out["code"] != float64(1) {
				t.Fatal(out)
			}
			order, err := f.s.Store.Order(ctx, 1, "binding180-boundary")
			if err != nil {
				t.Fatal(err)
			}
			// A SQL-owner loss/tamper probe deliberately bypasses only the
			// immutability triggers in this test's disposable schema. Production
			// triggers remain mandatory; the independent MAC must still reject.
			if scenario == "source_case_alias" || scenario == "source_missing" {
				if _, err = f.p.Exec(ctx, `ALTER TABLE runtime_parent_payment_sources DISABLE TRIGGER USER`); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "account_tampered" {
				if _, err = f.p.Exec(ctx, `ALTER TABLE runtime_parent_channel_accounts DISABLE TRIGGER USER`); err != nil {
					t.Fatal(err)
				}
			}
			switch scenario {
			case "source_case_alias":
				_, err = f.p.Exec(ctx, `UPDATE runtime_parent_payment_sources SET facts=facts||jsonb_build_object('paymentVariant','web') WHERE merchant_id=1 AND out_trade_no=$1`, order.OutTradeNo)
			case "source_missing":
				_, err = f.p.Exec(ctx, `DELETE FROM runtime_parent_payment_sources WHERE merchant_id=1 AND out_trade_no=$1`, order.OutTradeNo)
			case "account_tampered":
				_, err = f.p.Exec(ctx, `UPDATE runtime_parent_channel_accounts SET ciphertext=decode(repeat('00',32),'hex') WHERE channel_id=$1`, setup.id)
			case "current_profile_merchant_removed":
				web.MerchantIDs = channelconfig.IdentityMerchantIDs{2}
				web.Revision++
				var factory *channelconfig.ParentChannelFactory
				factory, err = channelconfig.NewParentChannelFactory(local.Bootstrap(), channelconfig.ParentBuildOptions{BuildOptions: channelconfig.BuildOptions{AllowLive: true}, IdentityProfiles: []channelconfig.IdentityProfileSpec{web, mini}, OAuthEnabled: true})
				if err == nil {
					local.ParentChannelFactory = factory
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "current_profile_merchant_removed" {
				if out := binding180Payment(t, f, setup.id, "binding180-after-merchant-removal", web.AppID, false); out["code"] == float64(1) {
					t.Fatal("removed profile merchant obtained a new send")
				}
				if _, err = f.s.ProviderFlow.Query(ctx, 1, order.TradeNo); err != nil {
					t.Fatal("profile removal stranded original query", err)
				}
				if setup.creates.Load() != 1 || setup.queries.Load() != 1 {
					t.Fatal("profile boundary changed provider effects", setup.creates.Load(), setup.queries.Load())
				}
			} else {
				if _, err = f.s.ProviderFlow.Query(ctx, 1, order.TradeNo); err == nil || setup.queries.Load() != 0 {
					t.Fatal("lost or ambiguous source borrowed current SDK", scenario, err, setup.queries.Load())
				}
			}
		})
	}
}
