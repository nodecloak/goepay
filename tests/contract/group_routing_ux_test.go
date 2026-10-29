package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type groupRoutingProvider152 struct {
	kind    string
	creates atomic.Int64
}

func (p *groupRoutingProvider152) ID() string            { return p.kind }
func (p *groupRoutingProvider152) ApplicationID() string { return "local-group152" }
func (p *groupRoutingProvider152) Capabilities() adapter.Capabilities {
	return adapter.Capabilities{Methods: []string{"native"}}
}
func (p *groupRoutingProvider152) Create(context.Context, adapter.CreateRequest) (adapter.CreateResult, error) {
	p.creates.Add(1)
	return adapter.CreateResult{MaterialType: "qrcode", Material: "https://qr.example.invalid/local-only"}, nil
}
func (p *groupRoutingProvider152) Query(context.Context, adapter.QueryRequest) (adapter.QueryResult, error) {
	return adapter.QueryResult{}, adapter.ErrUnsupported
}
func (p *groupRoutingProvider152) Close(context.Context, adapter.QueryRequest) error {
	return adapter.ErrUnsupported
}
func (p *groupRoutingProvider152) Refund(context.Context, adapter.RefundRequest) (adapter.RefundResult, error) {
	return adapter.RefundResult{}, adapter.ErrUnsupported
}
func (p *groupRoutingProvider152) QueryRefund(context.Context, adapter.RefundRequest) (adapter.RefundResult, error) {
	return adapter.RefundResult{}, adapter.ErrUnsupported
}

func TestGroupRoutingUXHTTPCatalogAndConfigurationUseActualRuntimeIDs(t *testing.T) {
	s, pool, _ := fixture(t)
	const channel int64 = 9007199254740993
	p := &groupRoutingProvider152{kind: "alipay"}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{channel: p}, AllowLive: true}
	token, csrf, e := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	req := func(role, cookie, method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/console/v1/"+role+path, strings.NewReader(body))
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: cookie})
		}
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	path := "/resources/groups/routing-options"
	w := req("admin", token, "GET", path, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"id":"9007199254740993"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = req("admin", "", "GET", path, ""); w.Code != 401 {
		t.Fatal("unauthenticated catalog", w.Code)
	}
	merchant, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant"})
	if e != nil {
		t.Fatal(e)
	}
	if w = req("merchant", merchant, "GET", path, ""); w.Code != 403 {
		t.Fatal("merchant catalog", w.Code)
	}
	w = req("admin", token, "POST", "/resources/groups", `{"name":"原通道费率","info":{"alipay":{"channel":9007199254740993,"type":"channel","rate":"99.50"}}}`)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var created struct {
		Data struct {
			ID   string                       `json:"id"`
			Info map[string]map[string]string `json:"info"`
		} `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &created) != nil || created.Data.Info["alipay"]["channel"] != "9007199254740993" {
		t.Fatal("runtime ID rounded", w.Body.String())
	}
	if w = req("admin", token, "PATCH", "/resources/groups/"+created.Data.ID, `{"info":{"alipay":{"channel":"7","type":"channel"}}}`); w.Code != 422 {
		t.Fatal("non-runtime channel saved", w.Code, w.Body.String())
	}
	var stored string
	if e = pool.QueryRow(context.Background(), `SELECT body#>>'{info,alipay,channel}' FROM backoffice_records WHERE id=$1`, created.Data.ID).Scan(&stored); e != nil || stored != "9007199254740993" || p.creates.Load() != 0 {
		t.Fatal(stored, e, p.creates.Load())
	}
}

func TestGroupRoutingUXHTTPV1V2HonorEffectiveGroupWithoutResendingOrWideningPermissions(t *testing.T) {
	for _, scenario := range []struct {
		kind       string
		randomRoll bool
	}{{"alipay", false}, {"wxpay", false}, {"alipay", true}, {"wxpay", true}} {
		kind, name := scenario.kind, scenario.kind
		if scenario.randomRoll {
			name += "-random-roll"
		}
		t.Run(name, func(t *testing.T) {
			s, pool, key := fixture(t)
			ctx := context.Background()
			const target int64 = 9007199254740993
			a, b := &groupRoutingProvider152{kind: kind}, &groupRoutingProvider152{kind: kind}
			s.CheckoutKey = bytes.Repeat([]byte{52}, 32)
			s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: a, target: b}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
			s.MerchantChannels = map[int64][]int64{1: {7, target}, 2: {7, target}}
			var gid int64
			body := fmt.Sprintf(`{"enabled":true,"info":{"%s":{"channel":"%d","type":"channel","rate":"99.50"}}}`, kind, target)
			if scenario.randomRoll {
				roll := fmt.Sprintf(`{"name":"local named roll161","merchant_id":"0","type":"%s","strategy":"first","channel_ids":"%d:1","enabled":true,"default_route":false}`, kind, target)
				if _, e := pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('pay-rolls',$1::jsonb)`, roll); e != nil {
					t.Fatal(e)
				}
				body = fmt.Sprintf(`{"enabled":true,"info":{"%s":{"channel":"-3","type":"roll","rate":"99.50"}}}`, kind)
			}
			if e := pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups',$1::jsonb) RETURNING id`, body).Scan(&gid); e != nil {
				t.Fatal(e)
			}
			if _, e := pool.Exec(ctx, `UPDATE merchants SET base_group_id=$1 WHERE id IN(1,2)`, gid); e != nil {
				t.Fatal(e)
			}
			for _, v2 := range []bool{false, true} {
				pid, path := "1", "/mapi.php"
				if v2 {
					pid, path = "2", "/api/pay/create"
				}
				params := compat.Params{"pid": pid, "out_trade_no": "group152-original-" + pid, "name": "local group routing", "money": "1.00", "type": kind, "method": "native", "notify_url": "https://merchant.example.invalid/notify"}
				var values url.Values
				if v2 {
					params["timestamp"] = fmt.Sprint(time.Now().Unix())
					params["sign_type"] = "RSA"
					signature, e := compat.SignRSA(params, key)
					if e != nil {
						t.Fatal(e)
					}
					params["sign"] = signature
					values = url.Values{}
					for k, v := range params {
						values.Set(k, v)
					}
				} else {
					values = signed(params, "contract-secret")
				}
				_, out := call(t, s.Handler(), "POST", path, values)
				want := float64(1)
				if v2 {
					want = 0
				}
				if out["code"] != want {
					t.Fatal(v2, out)
				}
				trade, ok := out["trade_no"].(string)
				if !ok {
					t.Fatal("missing original trade", out)
				}
				var actual int64
				if e := pool.QueryRow(ctx, `SELECT channel_id FROM provider_attempts p JOIN orders o ON o.id=p.order_id WHERE o.trade_no=$1`, trade).Scan(&actual); e != nil || actual != target {
					t.Fatal("group ignored", actual, e)
				}
				before := b.creates.Load()
				if _, out = call(t, s.Handler(), "POST", path, values); out["code"] != want || b.creates.Load() != before {
					t.Fatal("same request resent", out, b.creates.Load())
				}
			}
			if a.creates.Load() != 0 || b.creates.Load() != 2 {
				t.Fatal("wrong provider invocation", a.creates.Load(), b.creates.Load())
			}
			s.MerchantChannels[1] = []int64{7}
			params := compat.Params{"pid": "1", "out_trade_no": "denied-group152", "name": "local group routing", "money": "1.00", "type": kind, "method": "native", "notify_url": "https://merchant.example.invalid/notify"}
			_, out := call(t, s.Handler(), "POST", "/mapi.php", signed(params, "contract-secret"))
			if out["code"] == float64(1) || a.creates.Load() != 0 || b.creates.Load() != 2 {
				t.Fatal("group widened merchant channel grant", out)
			}
		})
	}
}
