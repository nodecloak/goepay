package server

import (
	"context"
	"goepay/internal/identity"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestCredentials199ActualBrowserPasswordAndMD5Rotation(t *testing.T) {
	if os.Getenv("GOEPAY_CREDENTIALS199_BROWSER") != "1" {
		t.Skip("explicit local actual browser verification required")
	}
	for _, width := range []int{375, 1440} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			f := email197Local(t)
			var oldKey string
			var originalVersion int64
			if e := f.p.QueryRow(t.Context(), `SELECT (SELECT encode(api_key,'hex') FROM merchants WHERE id=1),auth_version FROM console_users WHERE username='email197' AND role='merchant'`).Scan(&oldKey, &originalVersion); e != nil {
				t.Fatal(e)
			}
			api := httptest.NewServer(f.s.Handler())
			t.Cleanup(api.Close)
			root, e := filepath.Abs(filepath.Join("..", ".."))
			if e != nil {
				t.Fatal(e)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "web", "e2e", "credentials199-http-browser.mjs"))
			cmd.Dir = filepath.Join(root, "web")
			cmd.Env = append(os.Environ(), "CREDENTIALS199_API="+api.URL, "CREDENTIALS199_COOKIE="+f.token, "CREDENTIALS199_WIDTH="+strconv.Itoa(width), "CREDENTIALS199_ARTIFACTS="+filepath.Join(root, ".tools", "credentials199-actual-browser"))
			output, e := cmd.CombinedOutput()
			if e != nil {
				if len(output) > 3500 {
					output = output[len(output)-3500:]
				}
				t.Fatalf("actual credentials browser %v\n%s", e, output)
			}
			var hash, currentKey string
			var version, sessions, effects int64
			e = f.p.QueryRow(t.Context(), `SELECT password_hash,auth_version,(SELECT encode(api_key,'hex') FROM merchants WHERE id=1),(SELECT count(*) FROM console_sessions WHERE username='email197' AND role='merchant'),(SELECT count(*) FROM orders)+(SELECT count(*) FROM journals)+(SELECT count(*) FROM jobs) FROM console_users WHERE username='email197' AND role='merchant'`).Scan(&hash, &version, &currentKey, &sessions, &effects)
			if e != nil || !identity.CheckPassword(hash, "local-credentials199-browser-next-password") || version != originalVersion+1 || sessions != 0 || currentKey == oldKey || effects != 0 {
				t.Fatal("browser credential update did not atomically finish or touched funds", e, version, sessions, effects)
			}
		})
	}
}
