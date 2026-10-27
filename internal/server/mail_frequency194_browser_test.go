package server

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"goepay/internal/enrollment"
)

func TestMailFrequency194ActualBrowserHTTPAndSameKeyEnrollmentConsumer(t *testing.T) {
	if os.Getenv("GOEPAY_MAIL194_BROWSER") != "1" {
		t.Skip("GOEPAY_MAIL194_BROWSER=1 requires local Playwright")
	}
	f := channel168Local(t)
	master := make([]byte, 32)
	checkout := sha256.Sum256(append([]byte("checkout-signing:"), master...))
	f.s.CheckoutKey = checkout[:]
	var sends atomic.Int32
	service, e := enrollment.New(f.p, f.s.Store, master, enrollment.SenderFunc(func(context.Context, string, string, string) error { sends.Add(1); return nil }))
	if e != nil {
		t.Fatal(e)
	}
	api := httptest.NewServer(f.s.Handler())
	t.Cleanup(api.Close)
	root, e := filepath.Abs(filepath.Join("..", ".."))
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "web", "e2e", "mail194-http-browser.mjs"))
	cmd.Dir = filepath.Join(root, "web")
	cmd.Env = append(os.Environ(), "MAIL194_API="+api.URL, "MAIL194_COOKIE="+f.token, "MAIL194_ARTIFACTS="+filepath.Join(root, ".tools", "mail194-actual-browser"))
	output, e := cmd.CombinedOutput()
	if e != nil {
		if len(output) > 4000 {
			output = output[len(output)-4000:]
		}
		t.Fatalf("actual mail settings HTTP browser %v\n%s", e, output)
	}
	if _, e = service.Issue(t.Context(), enrollment.VerificationRequest{Purpose: "recover", Email: "first194@example.test", Source: "192.0.2.194"}); e != nil {
		t.Fatal("browser-saved policy not usable by actual enrollment", e)
	}
	if _, e = service.Issue(t.Context(), enrollment.VerificationRequest{Purpose: "recover", Email: "second194@example.test", Source: "192.0.2.194"}); !errors.Is(e, enrollment.ErrLimited) {
		t.Fatal("browser-saved IP interval not enforced by enrollment", e)
	}
	var revisions, witnesses, effects int
	if e = f.p.QueryRow(t.Context(), `SELECT(SELECT count(*)FROM mail_rate_policies194),(SELECT count(*)FROM mail_rate_witnesses194),(SELECT count(*)FROM orders)+(SELECT count(*)FROM journals)+(SELECT count(*)FROM jobs)`).Scan(&revisions, &witnesses, &effects); e != nil || revisions != 2 || witnesses != 2 || effects != 0 || sends.Load() != 1 {
		t.Fatal("browser policy revisions or consumer effects", revisions, witnesses, effects, sends.Load(), e)
	}
}
