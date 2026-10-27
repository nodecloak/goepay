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

// Opt-in because backend unit CI need not install a browser. The local full
// acceptance explicitly runs it with the existing project Playwright runtime.
func TestParentChannel180ActualBrowserHTTPTestPayment(t *testing.T) {
	if os.Getenv("GOEPAY_PARENT_TEST_BROWSER") != "1" {
		t.Skip("GOEPAY_PARENT_TEST_BROWSER=1 requires local project Playwright")
	}
	setup := parent180RuntimeReady(t)
	f := setup.f
	if _, e := f.p.Exec(t.Context(), `INSERT INTO backoffice_records(resource,body)VALUES('settings','{"key":"test_pay_uid","value":"1"}'),('settings','{"key":"payment_fee_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	api := httptest.NewServer(f.s.Handler())
	t.Cleanup(api.Close)
	root, e := filepath.Abs(filepath.Join("..", ".."))
	if e != nil {
		t.Fatal(e)
	}
	artifacts := filepath.Join(root, ".tools", "parentchannel180-real-http-browser")
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "web", "e2e", "parent180-test-http-browser.mjs"))
	cmd.Dir = filepath.Join(root, "web")
	cmd.Env = append(os.Environ(), "PARENT180_BROWSER_API="+api.URL, "PARENT180_BROWSER_COOKIE="+f.token, "PARENT180_BROWSER_CHANNEL="+setup.id, "PARENT180_BROWSER_ARTIFACTS="+artifacts)
	out, e := cmd.CombinedOutput()
	if e != nil {
		if len(out) > 5000 {
			out = out[len(out)-5000:]
		}
		t.Fatalf("local actual HTTP browser: %v\n%s", e, out)
	}
	if setup.creates.Load() != 2 || setup.queries.Load() != 2 || setup.bad.Load() != 0 {
		t.Fatal("browser did not execute exact synthetic effects", setup.creates.Load(), setup.queries.Load(), setup.bad.Load())
	}
}
