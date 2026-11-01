package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/channelconfig"
	"goepay/internal/compat"
	"goepay/internal/payment"
)

type weixinCatalogQueryCounter struct{ count atomic.Int64 }

func (c *weixinCatalogQueryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "runtime_weixin_app") {
		c.count.Add(1)
	}
	return ctx
}
func (*weixinCatalogQueryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestWeixinApplicationCatalogConstantQueriesAndCorruptionCannotBlockUnrelatedPaymentOrImportedIdentity(t *testing.T) {
	setup, local, web, mini, _ := binding180Ready(t)
	f := setup.f
	ctx := t.Context()
	ids := []int64{}
	for i := 0; i < 12; i++ {
		body, _ := json.Marshal(map[string]any{"request_id": fmt.Sprintf("%064x", i+400), "kind": "official_account", "name": fmt.Sprint("Synthetic draft ", i), "app_id": fmt.Sprint("wxperf", i), "appsecret": "synthetic-performance-secret", "enabled": false, "source_channel_id": "0", "merchant_ids": []string{}, "path": "", "expected_source_fingerprint": ""})
		row := weixinApplicationData(t, weixinApplicationHTTP(f, "POST", "", string(body)))
		ids = append(ids, mustWeixinAppID(t, row["id"].(string)))
	}
	if _, e := f.p.Exec(ctx, `UPDATE runtime_weixin_app_heads SET proof_hash=decode(repeat('00',32),'hex') WHERE id=$1`, ids[0]); e != nil {
		t.Fatal(e)
	}
	counter := &weixinCatalogQueryCounter{}
	config := f.p.Config()
	config.ConnConfig.Tracer = counter
	traced, e := pgxpool.NewWithConfig(ctx, config)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(traced.Close)
	store, e := payment.NewWithKey(traced, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	f.s.Pool = traced
	f.s.Store = store
	f.s.ProviderFlow.Store = store
	counter.count.Store(0)
	if _, e = store.ReadWeixinApplicationCatalog(ctx, ids[1:2]); e != nil || counter.count.Load() != 1 {
		t.Fatal("single selected catalog did not use one query", e, counter.count.Load())
	}
	counter.count.Store(0)
	catalog, e := store.ReadWeixinApplicationCatalog(ctx, nil)
	if e != nil || counter.count.Load() != 1 || len(catalog.InvalidIDs) != 1 || len(catalog.InactiveIDs) != 11 || len(catalog.Specs) != 0 {
		t.Fatal("catalog query count grew with row count, or drafts decrypted into execution", e, counter.count.Load(), len(catalog.InvalidIDs), len(catalog.Specs))
	}
	if _, ok := f.s.ProviderFlow.Providers[7].(*channel168Provider); !ok {
		t.Fatal("Alipay test must use an explicitly synthetic provider")
	}
	counter.count.Store(0)
	if result := f.pay(t, "weixin-vault-bad-unrelated-alipay", "1.00", 7); result["code"] != float64(1) {
		t.Fatal("bad unrelated app blocked a new Alipay payment", result["msg"])
	}
	if counter.count.Load() != 0 {
		t.Fatal("Alipay without managed dependency queried the vault", counter.count.Load())
	}
	if !strings.HasPrefix(local.endpoint, "https://127.0.0.1:") {
		t.Fatal("Weixin test must use the existing local TLS server")
	}
	p := compat.Params{"pid": "1", "out_trade_no": "weixin-vault-bad-unbound-native", "type": "wxpay", "name": "Synthetic native", "money": "1.00", "channel_id": setup.id, "method": "native", "notify_url": "https://merchant.example.invalid/notify", "return_url": "https://merchant.example.invalid/return", "sign_type": "MD5"}
	p["sign"] = compat.SignMD5(p, "test-secret")
	values := url.Values{}
	for key, value := range p {
		values.Set(key, value)
	}
	counter.count.Store(0)
	request := httptest.NewRequest("POST", "/mapi.php", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(response, request)
	var native map[string]any
	if json.Unmarshal(response.Body.Bytes(), &native) != nil || native["code"] != float64(1) || counter.count.Load() != 0 {
		t.Fatal("native without managed binding was blocked or queried full vault", response.Code, native["msg"], counter.count.Load())
	}
	f.s.IdentityProfileEnabled = true
	f.s.IdentityProfileCallbackBaseURL = "https://pay.example.invalid"
	f.s.IdentityProfiles, e = channelconfig.BuildIdentityProfiles(channelconfig.Import{Version: 1, Channels: []channelconfig.Channel{local.Bootstrap()}, IdentityProfiles: []channelconfig.IdentityProfileSpec{web, mini}}, true)
	if e != nil {
		t.Fatal(e)
	}
	counter.count.Store(0)
	if _, _, ready := f.s.identityProfileBinding(ctx, 1, web.ID); !ready || counter.count.Load() != 1 {
		t.Fatal("bad unrelated managed row blocked approved imported identity or caused global scan", ready, counter.count.Load())
	}
	directory := weixinApplicationHTTP(f, "GET", "", "")
	var all struct {
		Data struct {
			Items  []any `json:"items"`
			Errors []any `json:"configuration_errors"`
		}
	}
	if directory.Code != 200 || json.Unmarshal(directory.Body.Bytes(), &all) != nil || len(all.Data.Items) != 11 || len(all.Data.Errors) != 1 {
		t.Fatal("bad app prevented healthy directory records from being read", directory.Code, len(all.Data.Items), len(all.Data.Errors))
	}
}
