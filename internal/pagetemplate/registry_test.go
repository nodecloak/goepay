package pagetemplate

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func configJSON(t *testing.T, kind string, edit func(map[string]any)) json.RawMessage {
	t.Helper()
	m := map[string]any{"brand_name": "Local Brand", "notice": "Local notice", "accent_color": "#007c91", "background_color": "#f4f8fa", "logo_asset_id": "", "density": "comfortable"}
	if kind == "pay" {
		m["show_order_name"] = true
	}
	if edit != nil {
		edit(m)
	}
	raw, e := json.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	return raw
}
func TestBuiltInRegistryAndTypedConfig(t *testing.T) {
	cases := map[string][]string{
		"home":  homeStyleNames(),
		"pay":   {"default", "compact", "anime"},
		"qrpay": {"default", "compact"},
	}
	for kind, want := range cases {
		list, e := Templates(kind)
		if e != nil || len(list) != len(want) {
			t.Fatal(kind, len(list), e)
		}
		for _, name := range want {
			if _, ok := Lookup(kind, name); !ok {
				t.Fatal("missing audited template", kind, name)
			}
			if _, e = ParseConfig(kind, name, configJSON(t, kind, nil)); e != nil {
				t.Fatal(e)
			}
		}
	}
	if len(cases["home"]) != 17 {
		t.Fatal("home must ship default, index1-index10, minimal and the five anime styles", len(cases["home"]))
	}
	if _, ok := Lookup("qrpay", "anime"); ok {
		t.Fatal("the anime template is a pay page only")
	}
	for _, kind := range []string{"../pay", "PAY", "plugin"} {
		if _, e := Templates(kind); e == nil {
			t.Fatal("untrusted kind accepted", kind)
		}
	}
	for _, raw := range []string{`{}`, `null`, `{"brand_name":"x","brand_name":"y"}`, `{"brand_name":null}`} {
		if _, e := ParseConfig("pay", "default", []byte(raw)); e == nil {
			t.Fatal("ambiguous config accepted", raw)
		}
	}
	for _, change := range []func(map[string]any){func(m map[string]any) { m["template_html"] = "<script>alert(1)</script>" }, func(m map[string]any) { m["accent_color"] = "red;display:none" }, func(m map[string]any) { m["density"] = "../../evil" }, func(m map[string]any) { m["logo_asset_id"] = "https://local.invalid/image" }, func(m map[string]any) { m["show_order_name"] = "true" }, func(m map[string]any) { m["brand_name"] = strings.Repeat("字", 65) }} {
		if _, e := ParseConfig("pay", "default", configJSON(t, "pay", change)); e == nil {
			t.Fatal("unsafe typed configuration accepted")
		}
	}
}
func TestFrameEscapesBrandingAndActuallyChangesBuiltInLayout(t *testing.T) {
	cfg, e := ParseConfig("pay", "default", configJSON(t, "pay", func(m map[string]any) { m["brand_name"] = `<script>alert(1)</script>` }))
	if e != nil {
		t.Fatal(e)
	}
	var a, b bytes.Buffer
	for name, target := range map[string]*bytes.Buffer{"default": &a, "compact": &b} {
		if e = Open(target, Resolved{Type: "pay", Name: name, Config: cfg}, Page{Title: "支付收银台", TradeNo: "LOCAL", Amount: "1.00", OrderName: "<img src=x onerror=1>"}); e != nil {
			t.Fatal(e)
		}
		if e = Close(target); e != nil {
			t.Fatal(e)
		}
	}
	if strings.Contains(a.String(), "<script>alert(1)</script>") || strings.Contains(a.String(), "<img src=x") || !strings.Contains(a.String(), "&lt;script&gt;") || !strings.Contains(a.String(), "LOCAL") || !strings.Contains(a.String(), "1.00") {
		t.Fatal("branding escaped incorrectly", a.String())
	}
	if a.String() == b.String() || !strings.Contains(b.String(), `data-template="compact"`) {
		t.Fatal("template selection did not affect frame")
	}
}
