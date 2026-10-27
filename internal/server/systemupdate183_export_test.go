package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/identity"
	"goepay/internal/systemupdate"
)

func TestSystemUpdate183ExportDisposedActualHTTPFixture(t *testing.T) {
	target := os.Getenv("GOEPAY_SYSTEMUPDATE183_UI_FIXTURE")
	if target == "" {
		t.Skip("explicit local actual HTTP fixture export only")
	}
	out := map[string]any{"synthetic_only": true, "source": "actual local Go HTTP and PostgreSQL", "cookie_or_bearer_exported": false}
	var schema, token, csrf string
	var sessions *identity.Sessions
	if !t.Run("disposed-original", func(t *testing.T) {
		f := update183Local(t)
		sessions, token, csrf = f.s.Sessions, f.token, f.csrf
		if e := f.p.QueryRow(t.Context(), `SELECT current_schema()`).Scan(&schema); e != nil {
			t.Fatal(e)
		}
		view := update183Data(t, f.update("GET", "", ""))
		out["available"] = view
		fp := view["fingerprint"].(string)
		raw, _ := json.Marshal(map[string]string{"intent": fp, "expected_fingerprint": fp})
		w := f.update("POST", "/prepare", string(raw))
		if w.Code != 202 {
			t.Fatal(w.Code, w.Body.String())
		}
		out["accepted"] = update183Data(t, w)
		prepared := waitUpdate183(t, f, fp, "prepared")
		out["prepared"] = prepared
		originalBuild := f.s.SystemUpdateBuild
		f.s.SystemUpdateBuild = func(n int64) systemupdate.Build {
			b := originalBuild(n)
			b.Version = f.manifest.Version
			b.SourceSHA = f.manifest.SourceSHA
			return b
		}
		out["applied"] = update183Data(t, f.update("GET", "/preparations/"+fp, ""))
		f.s.SystemUpdateBuild = originalBuild
		if e := os.RemoveAll(filepath.Join(f.cfg.StagingDir, fp)); e != nil {
			t.Fatal(e)
		}
		unknown := update183Data(t, f.update("GET", "/preparations/"+fp, ""))
		out["unknown"] = unknown
		retry, _ := json.Marshal(map[string]string{"expected_fingerprint": fp, "expected_state": unknown["state_fingerprint"].(string)})
		if w = f.update("POST", "/preparations/"+fp+"/retry", string(retry)); w.Code != 202 {
			t.Fatal(w.Code, w.Body.String())
		}
		prepared = waitUpdate183(t, f, fp, "prepared")
		cancel, _ := json.Marshal(map[string]string{"expected_state": prepared["state_fingerprint"].(string)})
		w = f.update("POST", "/preparations/"+fp+"/cancel", string(cancel))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		out["cancelled"] = update183Data(t, w)
		longManifest := f.manifest
		longManifest.Version = "0.2.0-" + strings.Repeat("a", 70)
		bundle, e := os.ReadFile(f.cfg.PackagePath)
		if e != nil {
			t.Fatal(e)
		}
		resignUpdate183(t, f, bundle, longManifest)
		longView := update183Data(t, f.update("GET", "", ""))
		longIntent := longView["fingerprint"].(string)
		longBody, _ := json.Marshal(map[string]string{"intent": longIntent, "expected_fingerprint": longIntent})
		if w = f.update("POST", "/prepare", string(longBody)); w.Code != 202 {
			t.Fatal(w.Code, w.Body.String())
		}
		out["long_version"] = waitUpdate183(t, f, longIntent, "prepared")
	}) {
		return
	}
	if _, ok := sessions.Get(token, "admin"); ok {
		t.Fatal("fixture session remains")
	}
	p, e := pgxpool.New(t.Context(), os.Getenv("GOEPAY_TEST_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	var remains bool
	if e = p.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname=$1)`, schema).Scan(&remains); e != nil || remains {
		t.Fatal("fixture schema remains", e)
	}
	out["schema_disposed"], out["session_invalid_after_cleanup"] = true, true
	raw, e := json.MarshalIndent(out, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(raw), token) || strings.Contains(string(raw), csrf) {
		t.Fatal("usable credentials exported")
	}
	if e = os.WriteFile(target, append(raw, '\n'), 0600); e != nil {
		t.Fatal(e)
	}
}
