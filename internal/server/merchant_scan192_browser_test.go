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

func TestMerchantScan192ActualBrowserHTTP(t *testing.T) {
	if os.Getenv("GOEPAY_SCAN192_BROWSER") != "1" {
		t.Skip("GOEPAY_SCAN192_BROWSER=1 uses local project Playwright and synthetic TLS peer")
	}
	setup, f := merchantScan192Ready(t)
	api := httptest.NewServer(f.s.Handler())
	t.Cleanup(api.Close)
	root, e := filepath.Abs(filepath.Join("..", ".."))
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "web", "e2e", "scan192-http-browser.mjs"))
	cmd.Dir = filepath.Join(root, "web")
	cmd.Env = append(os.Environ(), "SCAN192_BROWSER_API="+api.URL, "SCAN192_BROWSER_COOKIE="+f.merchantToken, "SCAN192_BROWSER_ARTIFACTS="+filepath.Join(root, ".tools", "scan192-actual-http-browser"))
	output, e := cmd.CombinedOutput()
	if e != nil {
		if len(output) > 4000 {
			output = output[len(output)-4000:]
		}
		t.Fatalf("local actual cashier browser: %v\n%s", e, output)
	}
	if setup.creates.Load() != 2 || setup.queries.Load() != 0 || setup.bad.Load() != 0 {
		t.Fatal("browser did not execute exactly two local signed charges", setup.creates.Load(), setup.queries.Load(), setup.bad.Load())
	}
}
