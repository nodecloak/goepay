package server

import "testing"

// The 支付通道 list filters must accept every catalog pay type (支付方式) and any
// registered ported plugin name (支付插件), not only the native alipay/wxpay —
// so a console can filter channels built from a ported plugin.
func TestParentChannelFiltersAcceptCatalogAndPlugins(t *testing.T) {
	good := []struct{ raw, kind, plugin string }{
		{"type=qqpay", "qqpay", ""},
		{"type=bank", "bank", ""},
		{"type=ecny", "ecny", ""},
		{"plugin=easypay", "", "easypay"},
		{"plugin=wxpay", "", "wxpay"},
		{"type=alipay&plugin=easypay", "alipay", "easypay"},
	}
	for _, c := range good {
		f, ok := parentChannelFilters(c.raw, false)
		if !ok {
			t.Fatalf("parentChannelFilters(%q) rejected a valid filter", c.raw)
		}
		if f.kind != c.kind || f.plugin != c.plugin {
			t.Fatalf("parentChannelFilters(%q) = kind %q plugin %q; want %q %q", c.raw, f.kind, f.plugin, c.kind, c.plugin)
		}
	}
	for _, raw := range []string{"type=notatype", "plugin=not_a_plugin", "plugin=Bad-Name"} {
		if _, ok := parentChannelFilters(raw, false); ok {
			t.Fatalf("parentChannelFilters(%q) accepted an unknown value", raw)
		}
	}
}
