package server

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/channelconfig"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func parent180HTTP(f channel168Fixture, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/console/v1/admin/payment-channels"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}
func TestParentChannel180ActualHTTPRegisteredCatalogIsDistinctFromMetadataAndKeepsFullIDs(t *testing.T) {
	f := channel168Local(t)
	const large = int64(9007199254740993)
	f.s.ProviderFlow.Providers[large] = &channel168Provider{app: "runtime-parent180-large"}
	f.s.MerchantChannels[1] = append(f.s.MerchantChannels[1], large)
	if _, e := f.p.Exec(t.Context(), `INSERT INTO backoffice_records(resource,body) VALUES('channels','{"name":"metadata-only-fake180","provider":"alipay","enabled":true}'::jsonb)`); e != nil {
		t.Fatal(e)
	}
	w := parent180HTTP(f, "GET", "", "")
	if w.Code != 200 {
		t.Fatalf("registered parent configuration page unavailable: HTTP=%d body=%s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("runtime catalog cached")
	}
	var out struct {
		Code int
		Data struct {
			Items []map[string]any
			Total int
		}
	}
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil || out.Code != 0 || len(out.Data.Items) != 3 || out.Data.Total != 3 {
		t.Fatal("runtime IDs not derived from actual registry", e, w.Body.String())
	}
	ids := map[string]bool{}
	for _, r := range out.Data.Items {
		id, ok := r["id"].(string)
		if !ok {
			t.Fatal("parentID was rounded through number", r)
		}
		ids[id] = true
		if r["pay_type"] != "alipay" {
			t.Fatal("unknown runtime family", r)
		}
	}
	if !ids["7"] || !ids["8"] || !ids["9007199254740993"] {
		t.Fatal(ids)
	}
	for _, secret := range []string{"runtime-parent180-large", "app168-seven", "metadata-only-fake180", "account_scope", "proof_hash", "ciphertext"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("runtime catalog leaked private material or metadata masqueraded as executable", secret)
		}
	}
}
func TestParentChannel180ActualHTTPRejectsActiveOperatorConfig(t *testing.T) {
	f := channel168Local(t)
	if _, e := f.p.Exec(t.Context(), `UPDATE console_users SET admin_class='operator' WHERE username='admin168'`); e != nil {
		t.Fatal(e)
	}
	var e error
	var version int64
	if e = f.p.QueryRow(t.Context(), `SELECT auth_version FROM console_users WHERE username='admin168'`).Scan(&version); e != nil {
		t.Fatal(e)
	}
	f.token, f.csrf, e = f.s.Sessions.Create(identity.User{ID: 168, Role: "admin", Username: "admin168", AdminClass: "operator", AuthVersion: version})
	if e != nil {
		t.Fatal(e)
	}
	w := parent180HTTP(f, "GET", "", "")
	if w.Code != 403 {
		t.Fatalf("operator reached parent credential catalog: %d", w.Code)
	}
}
func TestParentChannel180ActualHTTPNewParentStoresRealEncryptedAccountWithDefaultOff(t *testing.T) {
	f, body := parent180ReadyCreate(t)
	var e error
	raw, _ := json.Marshal(body)
	saved := parent180HTTP(f, "POST", "", string(raw))
	if saved.Code != 200 {
		t.Fatalf("real parent account save missing HTTP=%d body=%s", saved.Code, saved.Body.String())
	}
	var out struct{ Data map[string]any }
	if e = json.Unmarshal(saved.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	id, ok := out.Data["id"].(string)
	if !ok || id == "7" || id == "8" || out.Data["enabled"] != false || out.Data["account_version"] != "1" {
		t.Fatal("new parent did not have real distinct default-off version", out.Data)
	}
	for _, private := range []string{"abcdef0123456789abcdef0123456789", "ciphertext", "proof_hash", "account_hash"} {
		if strings.Contains(saved.Body.String(), private) {
			t.Fatal("private account returned", private)
		}
	}
	listed := parent180HTTP(f, "GET", "", "")
	if listed.Code != 200 || !strings.Contains(listed.Body.String(), `"id":"`+id+`"`) || !strings.Contains(listed.Body.String(), "本地新父通道180") {
		t.Fatalf("saved actual parent absent from catalog: HTTP=%d %s", listed.Code, listed.Body.String())
	}
	var count int
	var ciphertext []byte
	if e = f.p.QueryRow(t.Context(), `SELECT count(*),(array_agg(ciphertext ORDER BY version))[1] FROM runtime_parent_channel_accounts WHERE channel_id=$1`, id).Scan(&count, &ciphertext); e != nil || count != 1 || len(ciphertext) < 16 || bytes.Contains(ciphertext, []byte("abcdef0123456789abcdef0123456789")) {
		t.Fatal("runtime account was not actually AEAD versioned", count, e)
	}
}
func TestParentChannel180ActualHTTPReadWorksWithOneDatabaseConnection(t *testing.T) {
	f := channel168Local(t)
	config := f.p.Config()
	config.MaxConns = 1
	pool, e := pgxpool.NewWithConfig(t.Context(), config)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	store, e := payment.NewWithKey(pool, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	f.s.Pool, f.s.Store, f.s.ProviderFlow.Store = pool, store, store
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	r := httptest.NewRequest("GET", "/console/v1/admin/payment-channels", nil).WithContext(ctx)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("read-only parent catalog needs a nested pool reservation: HTTP=%d", w.Code)
	}
}

func parent180ReadyCreate(t *testing.T) (channel168Fixture, map[string]any) {
	t.Helper()
	f := channel168Local(t)
	origin := channelconfig.Channel{ID: 8, Plugin: "wxpay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "synthetic180-origin", "appmchid": "synthetic180-origin-mch", "appkey": "0123456789abcdef0123456789abcdef"}}
	factory, e := channelconfig.NewParentChannelFactory(origin, channelconfig.ParentBuildOptions{})
	if e != nil {
		t.Fatal(e)
	}
	execution, _, e := factory.BootstrapSnapshot(8)
	if e != nil {
		t.Fatal(e)
	}
	f.s.ProviderFlow.Providers[8] = execution.Payment
	f.s.ProviderFlow.ParentFactories = map[int64]adapter.ParentAccountFactory{8: factory}
	templates := parent180HTTP(f, "GET", "/templates", "")
	if templates.Code != 200 {
		t.Fatalf("real parent config schema unavailable: HTTP=%d", templates.Code)
	}
	var choices struct {
		Data struct {
			Items []struct {
				SourceChannelID string `json:"source_channel_id"`
				Fingerprint     string `json:"fingerprint"`
			}
		}
	}
	if e = json.Unmarshal(templates.Body.Bytes(), &choices); e != nil {
		t.Fatal(e)
	}
	sourceFP := ""
	for _, choice := range choices.Data.Items {
		if choice.SourceChannelID == "8" {
			sourceFP = choice.Fingerprint
		}
	}
	if len(sourceFP) != 64 {
		t.Fatal("trusted runtime source absent")
	}
	body := map[string]any{"source_channel_id": "8", "expected_source_fingerprint": sourceFP, "request_id": "1234567890abcdef1234567890abcdef", "name": "本地新父通道180", "rate": "99.50", "mode": 0, "account": map[string]any{"app_types": []string{"1"}, "config": map[string]any{"appid": map[string]string{"operation": "replace", "value": "synthetic180-fresh"}, "appmchid": map[string]string{"operation": "replace", "value": "synthetic180-fresh-mch"}, "appkey": map[string]string{"operation": "replace", "value": "abcdef0123456789abcdef0123456789"}}}}

	return f, body
}
