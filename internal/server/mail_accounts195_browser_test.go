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

func TestMailAccounts195ActualBrowserHTTPOriginalSMTPDialogAndPasswordBoundary(t *testing.T) {
	if os.Getenv("GOEPAY_MAIL195_BROWSER") != "1" {
		t.Skip("GOEPAY_MAIL195_BROWSER=1 requires local Playwright")
	}
	f := channel168Local(t)
	peer := mail195LocalTLS(t, false)
	f.s.Mail195TestEnabled = true
	f.s.Mail195SMTPRoots = peer.roots
	api := httptest.NewServer(f.s.Handler())
	t.Cleanup(api.Close)
	root, e := filepath.Abs(filepath.Join("..", ".."))
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "web", "e2e", "mail195-http-browser.mjs"))
	cmd.Dir = filepath.Join(root, "web")
	cmd.Env = append(os.Environ(), "MAIL195_API="+api.URL, "MAIL195_COOKIE="+f.token, "MAIL195_SMTP_PORT="+peer.port, "MAIL195_ARTIFACTS="+filepath.Join(root, ".tools", "mail195-actual-browser"))
	output, e := cmd.CombinedOutput()
	if e != nil {
		if len(output) > 4000 {
			output = output[len(output)-4000:]
		}
		t.Fatalf("actual mail195 browser %v\n%s", e, output)
	}
	var revisions, witnesses, effects int
	if e = f.p.QueryRow(t.Context(), `SELECT(SELECT count(*)FROM mail_account_revisions195),(SELECT count(*)FROM mail_account_witnesses195),(SELECT count(*)FROM orders)+(SELECT count(*)FROM journals)+(SELECT count(*)FROM jobs)+(SELECT count(*)FROM enrollment_challenges)`).Scan(&revisions, &witnesses, &effects); e != nil || revisions != 2 || witnesses != 2 || effects != 0 {
		t.Fatal("SMTP browser did not persist exactly two account confirmations or sent mail", revisions, witnesses, effects, e)
	}
	var requests, outcomes int
	if e = f.p.QueryRow(t.Context(), `SELECT(SELECT count(*)FROM mail_test_requests195),(SELECT count(*)FROM mail_test_outcomes195)`).Scan(&requests, &outcomes); e != nil || requests != 2 || outcomes != 2 || peer.connections.Load() != 2 || peer.data.Load() != 2 {
		t.Fatal("actual browser test email was not exactly once per original confirmation", requests, outcomes, peer.connections.Load(), peer.data.Load(), e)
	}
}
