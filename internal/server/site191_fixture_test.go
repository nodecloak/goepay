package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func exportSite191DefaultFixture(t *testing.T, f channel168Fixture, data map[string]any) {
	t.Helper()
	target := os.Getenv("GOEPAY_SITE191_FIXTURE_PATH")
	if target == "" {
		return
	}
	if !filepath.IsAbs(target) || !strings.HasSuffix(filepath.ToSlash(target), "/web/e2e/fixtures/site191-http.json") {
		t.Fatal("scoped synthetic site fixture path required")
	}
	raw, err := json.MarshalIndent(map[string]any{"synthetic_only": true, "production": false, "real_funds": false, "source": "actual migrated local PostgreSQL and GET /console/v1/public/site; schema/session disposed after test", "default": data}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{f.token, f.csrf, "proof_hash", "captcha_key"} {
		if private != "" && strings.Contains(string(raw), private) {
			t.Fatal("private material in public site fixture")
		}
	}
	if err = os.WriteFile(target, append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}
