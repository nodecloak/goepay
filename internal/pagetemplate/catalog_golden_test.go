package pagetemplate

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The console's 应用市场 lists every built-in page template as installed by
// default. That list is generated from this registry into
// web/src/data/builtin-templates.json, the same way the payment plugins are
// (internal/adapters/payment/ported_catalog_test.go). Run the test with
// GOEPAY_UPDATE_PLUGIN_CATALOG=1 to regenerate the file; otherwise it FAILS
// when the file no longer matches the registry, so the market can never show
// a template the service does not serve.

type builtinTemplate struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

func buildTemplateCatalog(t *testing.T) []builtinTemplate {
	t.Helper()
	var out []builtinTemplate
	for _, kind := range []string{"home", "pay", "qrpay"} {
		list, e := Templates(kind)
		if e != nil {
			t.Fatal(e)
		}
		for _, v := range list {
			out = append(out, builtinTemplate{Type: v.Type, Name: v.Name, Title: v.Title, Description: v.Description})
		}
	}
	return out
}

func webData(name string) string { return filepath.Join("..", "..", "web", "src", "data", name) }

func TestBuiltinTemplateCatalogMatchesRegistry(t *testing.T) {
	raw, e := json.MarshalIndent(buildTemplateCatalog(t), "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	want := append(raw, '\n')
	path := webData("builtin-templates.json")
	if os.Getenv("GOEPAY_UPDATE_PLUGIN_CATALOG") == "1" {
		if e = os.WriteFile(path, want, 0o644); e != nil {
			t.Fatal(e)
		}
		t.Logf("wrote %s", path)
		return
	}
	got, e := os.ReadFile(path)
	if e != nil {
		t.Fatalf("read %s: %v (run with GOEPAY_UPDATE_PLUGIN_CATALOG=1 to generate)", path, e)
	}
	if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), want) {
		t.Fatalf("%s is out of date with the template registry; run the test with GOEPAY_UPDATE_PLUGIN_CATALOG=1 to regenerate it", path)
	}
}

// Every template the original market lists must be built in under the same
// name and title, so 应用市场 and 模板配置 name it the same way.
func TestBuiltinTemplatesMatchTheOriginalMarket(t *testing.T) {
	raw, e := os.ReadFile(webData("plugin-catalog.json"))
	if e != nil {
		t.Fatal(e)
	}
	var market []struct {
		Type  string `json:"type"`
		Sub   string `json:"sub"`
		Name  string `json:"name"`
		Title string `json:"title"`
	}
	if e = json.Unmarshal(raw, &market); e != nil {
		t.Fatal(e)
	}
	seen := 0
	for _, m := range market {
		if m.Type != "template" {
			continue
		}
		v, ok := Lookup(m.Sub, m.Name)
		if !ok {
			t.Fatalf("market template %s/%s is not built in", m.Sub, m.Name)
		}
		if v.Title != m.Title {
			t.Fatalf("template %s/%s is titled %q, the market says %q", m.Sub, m.Name, v.Title, m.Title)
		}
		seen++
	}
	if seen != 13 {
		t.Fatalf("expected the 13 original market templates, saw %d", seen)
	}
}

// Every built-in template has a preview picture for the 模板配置 cards and the
// 应用市场 (TestWriteTemplatePreviewPages explains how to regenerate them).
func TestBuiltinTemplatesHavePreviewPictures(t *testing.T) {
	for _, v := range buildTemplateCatalog(t) {
		path := filepath.Join("..", "..", "web", "public", "static", "template-previews", v.Type+"-"+v.Name+".jpg")
		b, e := os.ReadFile(path)
		if e != nil {
			t.Fatalf("%s/%s has no preview picture: %v", v.Type, v.Name, e)
		}
		if len(b) < 2048 || b[0] != 0xff || b[1] != 0xd8 {
			t.Fatalf("%s is not a JPEG preview", path)
		}
	}
}
