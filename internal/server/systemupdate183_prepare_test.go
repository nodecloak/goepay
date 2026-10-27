package server

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"goepay/internal/systemupdate"
)

type update183Fixture struct {
	channel168Fixture
	cfg      systemupdate.Config
	manifest systemupdate.Manifest
	private  ed25519.PrivateKey
}

func update183Local(t *testing.T) update183Fixture {
	t.Helper()
	f := channel168Local(t)
	public, key, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	cfg := systemupdate.Config{ManifestPath: filepath.Join(dir, "manifest.json"), PackagePath: filepath.Join(dir, "update.zip"), StagingDir: filepath.Join(dir, "staging"), PublicKey: public}
	files := map[string][]byte{"SOURCE_CHECKPOINT.json": []byte(`{"label":"synthetic local183","files":[]}`), "web/index.html": []byte("<main>synthetic release</main>")}
	api := "bin/api"
	if runtime.GOOS == "windows" {
		api += ".exe"
	}
	files[api] = []byte("signed synthetic bytes; never executed by HTTP")
	rows, e := f.p.Query(t.Context(), `SELECT name,sha256 FROM schema_migrations ORDER BY name`)
	if e != nil {
		t.Fatal(e)
	}
	var schema int64
	for rows.Next() {
		var name, hash string
		if e = rows.Scan(&name, &hash); e != nil {
			t.Fatal(e)
		}
		raw, e := os.ReadFile(filepath.Join("..", "..", "db", "migrations", name))
		if e != nil || fmt.Sprintf("%x", sha256.Sum256(raw)) != hash {
			t.Fatal("original applied SQL fixture", name, e)
		}
		files["db/migrations/"+name] = raw
		var n int64
		fmt.Sscanf(name, "%d_", &n)
		if n > schema {
			schema = n
		}
	}
	rows.Close()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	m := systemupdate.Manifest{Version: "0.2.0", Name: "合成离线版本183", Changelog: "line1\n<script>literal changelog</script>", SourceSHA: fmt.Sprintf("%x", sha256.Sum256(files["SOURCE_CHECKPOINT.json"])), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, MinimumSchema: schema, TargetSchema: schema}
	for name, raw := range files {
		entry, e := w.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		entry.Write(raw)
		m.Package.Files = append(m.Package.Files, systemupdate.File{Name: name, Bytes: int64(len(raw)), SHA256: fmt.Sprintf("%x", sha256.Sum256(raw))})
	}
	if e = w.Close(); e != nil {
		t.Fatal(e)
	}
	m.Package.Bytes = int64(buf.Len())
	m.Package.SHA256 = fmt.Sprintf("%x", sha256.Sum256(buf.Bytes()))
	if e = os.WriteFile(cfg.PackagePath, buf.Bytes(), 0600); e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(m)
	signature := ed25519.Sign(key, append([]byte("goepay/update183/manifest\x00"), raw...))
	envelope, _ := json.Marshal(map[string]any{"manifest": json.RawMessage(raw), "signature": base64.RawURLEncoding.EncodeToString(signature)})
	if e = os.WriteFile(cfg.ManifestPath, envelope, 0600); e != nil {
		t.Fatal(e)
	}
	f.s.SystemUpdate, e = systemupdate.New(cfg)
	if e != nil {
		t.Fatal(e)
	}
	f.s.SystemUpdateBuild = func(n int64) systemupdate.Build {
		return systemupdate.Build{Version: "0.1.0", SourceSHA: strings.Repeat("a", 64), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Schema: n}
	}
	t.Cleanup(f.s.Store.StopSystemUpdates)
	return update183Fixture{f, cfg, m, key}
}
func (f update183Fixture) update(method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/console/v1/admin/system-update"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}
func update183Data(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out struct {
		Code int
		Data map[string]any
	}
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Code != 0 || out.Data == nil {
		t.Fatal("actual update DTO", w.Code, w.Body.String())
	}
	return out.Data
}
func TestSystemUpdate183SignedPackageActuallyPreparesAndSameIntentNeverRestarts(t *testing.T) {
	f := update183Local(t)
	state := update183Data(t, f.update("GET", "", ""))
	fp := state["fingerprint"].(string)
	body, _ := json.Marshal(map[string]string{"expected_fingerprint": fp, "intent": fp})
	w := f.update("POST", "/prepare", string(body))
	if w.Code != 202 {
		t.Fatal("actual signed package preparation unavailable", w.Code, w.Body.String())
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	var status map[string]any
	for {
		w = f.update("GET", "/preparations/"+fp, "")
		if w.Code != 200 {
			t.Fatal("read original intent", w.Code, w.Body.String())
		}
		status = update183Data(t, w)
		if status["state"] == "prepared" {
			break
		}
		if status["state"] == "failed" {
			t.Fatal("valid signed package failed", status)
		}
		select {
		case <-ctx.Done():
			t.Fatal("preparation did not reach real verified state", status)
		case <-time.After(20 * time.Millisecond):
		}
	}
	if status["applied"] != false {
		t.Fatal("prepared package claimed runtime already upgraded", status)
	}
	if _, e := os.Stat(filepath.Join(f.cfg.StagingDir, fp, "files", "web", "index.html")); e != nil {
		t.Fatal("progress had no verified artifact", e)
	}
	w = f.update("POST", "/prepare", string(body))
	if w.Code != 200 && w.Code != 202 {
		t.Fatal("same intent replay unavailable", w.Code)
	}
	var accepted, prepared int
	if e := f.p.QueryRow(t.Context(), `SELECT count(*) FILTER(WHERE operation='prepare-accepted'),count(*) FILTER(WHERE operation='prepare-prepared') FROM backoffice_audit WHERE resource='system-update183'`).Scan(&accepted, &prepared); e != nil || accepted != 1 || prepared != 1 {
		t.Fatal("same intent duplicated persistent preparation", accepted, prepared, e)
	}
	if strings.Contains(w.Body.String(), f.cfg.StagingDir) || strings.Contains(w.Body.String(), f.cfg.PackagePath) {
		t.Fatal("private update path entered HTTP")
	}
}

func waitUpdate183(t *testing.T, f update183Fixture, fp, want string) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	for {
		w := f.update("GET", "/preparations/"+fp, "")
		if w.Code != 200 {
			t.Fatal("original intent status", w.Code)
		}
		v := update183Data(t, w)
		if v["state"] == want {
			return v
		}
		select {
		case <-ctx.Done():
			t.Fatal("status did not reach expected state", want, v)
		case <-time.After(15 * time.Millisecond):
		}
	}
}
func resignUpdate183(t *testing.T, f update183Fixture, raw []byte, m systemupdate.Manifest) {
	t.Helper()
	m.Package.Bytes = int64(len(raw))
	m.Package.SHA256 = fmt.Sprintf("%x", sha256.Sum256(raw))
	if e := os.WriteFile(f.cfg.PackagePath, raw, 0600); e != nil {
		t.Fatal(e)
	}
	payload, _ := json.Marshal(m)
	signature := ed25519.Sign(f.private, append([]byte("goepay/update183/manifest\x00"), payload...))
	envelope, _ := json.Marshal(map[string]any{"manifest": json.RawMessage(payload), "signature": base64.RawURLEncoding.EncodeToString(signature)})
	if e := os.WriteFile(f.cfg.ManifestPath, envelope, 0600); e != nil {
		t.Fatal(e)
	}
}
func TestSystemUpdate183FailedIntentNeedsExplicitFreshRetryCAS(t *testing.T) {
	f := update183Local(t)
	original, e := os.ReadFile(f.cfg.PackagePath)
	if e != nil {
		t.Fatal(e)
	}
	corrupt := append([]byte(nil), original...)
	corrupt[len(corrupt)/2] ^= 1
	if e = os.WriteFile(f.cfg.PackagePath, corrupt, 0600); e != nil {
		t.Fatal(e)
	}
	view := update183Data(t, f.update("GET", "", ""))
	fp := view["fingerprint"].(string)
	body, _ := json.Marshal(map[string]string{"expected_fingerprint": fp, "intent": fp})
	if w := f.update("POST", "/prepare", string(body)); w.Code != 202 {
		t.Fatal(w.Code)
	}
	failed := waitUpdate183(t, f, fp, "failed")
	if e = os.WriteFile(f.cfg.PackagePath, original, 0600); e != nil {
		t.Fatal(e)
	}
	if v := update183Data(t, f.update("POST", "/prepare", string(body))); v["state"] != "failed" {
		t.Fatal("blind repeat restarted failed preparation", v)
	}
	retry, _ := json.Marshal(map[string]string{"expected_fingerprint": fp, "expected_state": failed["state_fingerprint"].(string)})
	if w := f.update("POST", "/preparations/"+fp+"/retry", string(retry)); w.Code != 202 {
		t.Fatal("explicit state-CAS retry absent", w.Code, w.Body.String())
	}
	waitUpdate183(t, f, fp, "prepared")
	if w := f.update("POST", "/preparations/"+fp+"/retry", string(retry)); w.Code != 409 {
		t.Fatal("stale retry restarted prepared artifact", w.Code)
	}
}
func TestSystemUpdate183CancellationStopsActualFilePreparation(t *testing.T) {
	f := update183Local(t)
	original, e := os.ReadFile(f.cfg.PackagePath)
	if e != nil {
		t.Fatal(e)
	}
	reader, e := zip.NewReader(bytes.NewReader(original), int64(len(original)))
	if e != nil {
		t.Fatal(e)
	}
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for _, entry := range reader.File {
		in, e := entry.Open()
		if e != nil {
			t.Fatal(e)
		}
		raw, e := io.ReadAll(in)
		in.Close()
		if e != nil {
			t.Fatal(e)
		}
		out, e := writer.Create(entry.Name)
		if e != nil {
			t.Fatal(e)
		}
		out.Write(raw)
	}
	large := bytes.Repeat([]byte{7}, 64<<20)
	out, e := writer.Create("web/cancel.bin")
	if e != nil {
		t.Fatal(e)
	}
	out.Write(large)
	if e = writer.Close(); e != nil {
		t.Fatal(e)
	}
	m := f.manifest
	m.Package.Files = append(m.Package.Files, systemupdate.File{Name: "web/cancel.bin", Bytes: int64(len(large)), SHA256: fmt.Sprintf("%x", sha256.Sum256(large))})
	resignUpdate183(t, f, buf.Bytes(), m)
	view := update183Data(t, f.update("GET", "", ""))
	fp := view["fingerprint"].(string)
	body, _ := json.Marshal(map[string]string{"expected_fingerprint": fp, "intent": fp})
	w := f.update("POST", "/prepare", string(body))
	if w.Code != 202 {
		t.Fatal(w.Code)
	}
	started := update183Data(t, w)
	cancelBody, _ := json.Marshal(map[string]string{"expected_state": started["state_fingerprint"].(string)})
	w = f.update("POST", "/preparations/"+fp+"/cancel", string(cancelBody))
	if w.Code != 200 {
		t.Fatal("actual file cancellation absent", w.Code, w.Body.String())
	}
	cancelled := waitUpdate183(t, f, fp, "cancelled")
	if cancelled["applied"] != false {
		t.Fatal("cancelled preparation marked applied")
	}
	var prepared int
	if e = f.p.QueryRow(t.Context(), `SELECT count(*) FROM backoffice_audit WHERE resource='system-update183' AND operation='prepare-prepared'`).Scan(&prepared); e != nil || prepared != 0 {
		t.Fatal("cancelled claim published success", prepared, e)
	}
}
