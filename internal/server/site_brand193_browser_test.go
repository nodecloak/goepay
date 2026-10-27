package server

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestSiteBrand193ActualBrowserHTTPImages(t *testing.T) {
	if os.Getenv("GOEPAY_BRAND193_BROWSER") != "1" {
		t.Skip("GOEPAY_BRAND193_BROWSER=1 requires local project Playwright")
	}
	f := channel168Local(t)
	api := httptest.NewServer(f.s.Handler())
	t.Cleanup(api.Close)
	root, e := filepath.Abs(filepath.Join("..", ".."))
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "web", "e2e", "brand193-http-browser.mjs"))
	cmd.Dir = filepath.Join(root, "web")
	cmd.Env = append(os.Environ(), "BRAND193_API="+api.URL, "BRAND193_COOKIE="+f.token, "BRAND193_ARTIFACTS="+filepath.Join(root, ".tools", "brand193-actual-browser"))
	output, e := cmd.CombinedOutput()
	if e != nil {
		if len(output) > 4000 {
			output = output[len(output)-4000:]
		}
		t.Fatalf("actual brand HTTP browser %v\n%s", e, output)
	}
	var revisions, assets, effects int
	if e = f.p.QueryRow(t.Context(), `SELECT(SELECT count(*)FROM site_brand_revisions193),(SELECT count(*)FROM site_brand_assets193),(SELECT count(*)FROM orders)+(SELECT count(*)FROM journals)+(SELECT count(*)FROM jobs)`).Scan(&revisions, &assets, &effects); e != nil || revisions != 4 || assets != 2 || effects != 0 {
		t.Fatal("actual image flow did not preserve independent revisions or touched money", revisions, assets, effects, e)
	}
}
