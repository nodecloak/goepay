package payment

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// The console's 应用市场 / 本地插件 pages mark a catalog entry 已内置 when the Go
// build actually serves it. That set used to be a hand-written map in
// plugin-catalog.ts that drifted from the ~60 ported Go plugins. This test is
// the single source of truth: it derives the built-in set from the live plugin
// registry (plus the two native adapters) and writes web/src/data/ported-
// plugins.json. Run with GOEPAY_UPDATE_PLUGIN_CATALOG=1 to regenerate the file
// after adding or changing a plugin; otherwise the test FAILS when the file no
// longer matches the registry, so the console can never claim a plugin is
// built in that the service does not actually run.

// portedPlugin is one built-in payment plugin as the console reads it. The
// fields are exactly the original plugin metadata the registry carries, so the
// pages show the original title/author/pay types without a second source.
type portedPlugin struct {
	Name   string   `json:"name"`
	Title  string   `json:"title"`
	Author string   `json:"author"`
	Types  []string `json:"types"`
	Trans  []string `json:"trans"`
	Note   string   `json:"note"`
}

// nativePortedPlugins are served by the dedicated alipay/wxpay adapters, never
// by the registry, so they are listed explicitly. Their original title/author
// and pay/transfer types match the first-party 彩虹易支付 plugins.
var nativePortedPlugins = []portedPlugin{
	{
		Name:   "alipay",
		Title:  "支付宝",
		Author: "官方",
		Types:  []string{"alipay"},
		Trans:  []string{"alipay"},
		Note:   "Go 版内置实现，按通道配置提供收款、退款、转账及分账等能力。",
	},
	{
		Name:   "wxpay",
		Title:  "微信支付",
		Author: "官方",
		Types:  []string{"wxpay"},
		Trans:  []string{"wxpay", "bank"},
		Note:   "Go 版内置实现，同一插件可配置微信支付 V2 或 V3 接口。",
	},
}

func buildPortedCatalog() []portedPlugin {
	out := append([]portedPlugin(nil), nativePortedPlugins...)
	for _, spec := range RegisteredPlugins() {
		types := append([]string(nil), spec.Types...)
		if types == nil {
			types = []string{}
		}
		trans := append([]string(nil), spec.TransferTypes...)
		if trans == nil {
			trans = []string{}
		}
		note := spec.Note
		if note == "" {
			note = "Go 版内置实现，随服务一起发布。"
		}
		out = append(out, portedPlugin{Name: spec.Name, Title: spec.Title, Author: spec.Author, Types: types, Trans: trans, Note: note})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func portedCatalogJSON(t *testing.T) []byte {
	t.Helper()
	raw, err := json.MarshalIndent(buildPortedCatalog(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(raw, '\n')
}

// catalogPath resolves web/src/data/ported-plugins.json relative to this
// package (internal/adapters/payment) without depending on the test CWD.
func catalogPath() string {
	return filepath.Join("..", "..", "..", "web", "src", "data", "ported-plugins.json")
}

func TestPortedPluginCatalogMatchesRegistry(t *testing.T) {
	want := portedCatalogJSON(t)
	path := catalogPath()
	if os.Getenv("GOEPAY_UPDATE_PLUGIN_CATALOG") == "1" {
		if err := os.WriteFile(path, want, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s (%d plugins)", path, len(buildPortedCatalog()))
		return
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (run with GOEPAY_UPDATE_PLUGIN_CATALOG=1 to generate)", path, err)
	}
	if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), want) {
		t.Fatalf("%s is out of date with the plugin registry; run the test with GOEPAY_UPDATE_PLUGIN_CATALOG=1 to regenerate it", path)
	}
}

// Every built-in entry must declare at least one known pay type and a title,
// so the console never renders a nameless or untyped 已内置 row.
func TestPortedPluginCatalogEntriesAreComplete(t *testing.T) {
	for _, p := range buildPortedCatalog() {
		if p.Name == "" || p.Title == "" {
			t.Fatalf("plugin missing name/title: %+v", p)
		}
		if len(p.Types) == 0 {
			t.Fatalf("plugin %s declares no pay types", p.Name)
		}
		for _, ty := range p.Types {
			if !CatalogPayType(ty) {
				t.Fatalf("plugin %s declares unknown pay type %q", p.Name, ty)
			}
		}
	}
}
