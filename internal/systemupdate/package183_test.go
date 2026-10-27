package systemupdate

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type packageVerifier183 interface {
	VerifyPackage(context.Context, Build, string, map[string]string) error
}
type entry183 struct {
	name string
	body []byte
	mode os.FileMode
}

func package183(t *testing.T, kind string) (*Service, Build, string, map[string]string, Config) {
	t.Helper()
	s, b, m, key, cfg := fixture183(t)
	checkpoint := []byte(`{"label":"synthetic only","files":[{"path":"cmd/api/main.go","sha256":"` + fmt.Sprintf("%x", sha256.Sum256([]byte("synthetic"))) + `"}]}`)
	executable := "bin/api"
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	sql := []byte("-- synthetic applied migration\nSELECT 1;\n")
	sqlName := "db/migrations/000101_existing.up.sql"
	entries := []entry183{{executable, []byte("synthetic signed executable bytes"), 0600}, {"SOURCE_CHECKPOINT.json", checkpoint, 0600}, {sqlName, sql, 0600}, {"web/index.html", []byte("<main>new local version</main>"), 0600}}
	m.SourceSHA = fmt.Sprintf("%x", sha256.Sum256(checkpoint))
	m.TargetSchema = 101
	m.Package.Files = nil
	for _, e := range entries {
		m.Package.Files = append(m.Package.Files, File{Name: e.name, Bytes: int64(len(e.body)), SHA256: fmt.Sprintf("%x", sha256.Sum256(e.body))})
	}
	applied := map[string]string{"000101_existing.up.sql": fmt.Sprintf("%x", sha256.Sum256(sql))}
	switch kind {
	case "traversal":
		entries = append(entries, entry183{"../escape183", []byte("outside"), 0600})
	case "windows-case":
		entries = append(entries, entry183{"WEB/index.html", []byte("collision"), 0600})
	case "windows-device":
		entries = append(entries, entry183{"web/CON.txt", []byte("reserved"), 0600})
	case "symlink":
		entries[3].mode = os.ModeSymlink | 0777
	case "duplicate":
		entries = append(entries, entries[3])
	case "file-hash":
		entries[3].body = []byte("<main>bad local version</main>")
	case "missing":
		entries = entries[:len(entries)-1]
	case "applied-migration":
		applied["000101_existing.up.sql"] = fmt.Sprintf("%x", sha256.Sum256([]byte("previous historical SQL")))
	case "missing-migration":
		applied["000100_lost.up.sql"] = fmt.Sprintf("%x", sha256.Sum256([]byte("lost historical SQL")))
	case "checkpoint":
		m.SourceSHA = fmt.Sprintf("%x", sha256.Sum256([]byte("wrong publisher checkpoint")))
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		h.SetMode(e.mode)
		out, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = out.Write(e.body); err != nil {
			t.Fatal(err)
		}
	}
	if e := w.Close(); e != nil {
		t.Fatal(e)
	}
	raw := buf.Bytes()
	m.Package.Bytes = int64(len(raw))
	m.Package.SHA256 = fmt.Sprintf("%x", sha256.Sum256(raw))
	if kind == "package-hash" {
		m.Package.SHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte("wrong package")))
	}
	if e := os.WriteFile(cfg.PackagePath, raw, 0600); e != nil {
		t.Fatal(e)
	}
	writeManifest183(t, cfg, key, m)
	v, e := s.Inspect(context.Background(), b)
	if e != nil || !v.CanPrepare {
		t.Fatal("valid signed check setup", v, e)
	}
	return s, b, v.Fingerprint, applied, cfg
}
func Test183PackageWholeHashFilesAndAppliedSQLMustAllAgree(t *testing.T) {
	for _, kind := range []string{"valid", "package-hash", "file-hash", "missing", "applied-migration", "missing-migration", "checkpoint", "traversal", "windows-case", "windows-device", "symlink", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			s, b, fp, applied, cfg := package183(t, kind)
			validator, ok := any(s).(packageVerifier183)
			if !ok {
				t.Fatal("actual signed ZIP validator and safe staging are missing")
			}
			e := validator.VerifyPackage(context.Background(), b, fp, applied)
			if kind == "valid" && e != nil {
				t.Fatal("valid signed local package rejected", e)
			}
			if kind != "valid" && e == nil {
				t.Fatal("unsafe package or changed historical SQL accepted", kind)
			}
			if _, e = os.Stat(filepath.Join(filepath.Dir(cfg.StagingDir), "escape183")); !os.IsNotExist(e) {
				t.Fatal("ZIP wrote outside controlled staging", e)
			}
			if items, e := os.ReadDir(cfg.StagingDir); e == nil && len(items) != 0 {
				t.Fatal("verification leaked temporary or published package", len(items))
			}
		})
	}
}

func Test183PublishedArtifactMustRejectAnExtraUncommittedFile(t *testing.T) {
	s, b, fp, applied, cfg := package183(t, "valid")
	ticket, e := s.Plan(t.Context(), b, fp)
	if e != nil {
		t.Fatal(e)
	}
	artifact, e := ticket.Stage(t.Context(), applied, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer artifact.Abort()
	seal, e := artifact.Commit(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.VerifiedArtifact(t.Context(), fp, seal); e != nil {
		t.Fatal("real verified artifact positive", e)
	}
	if e = os.WriteFile(filepath.Join(cfg.StagingDir, fp, "files", "web", "unexpected.js"), []byte("extra publisher-uncommitted script"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = s.VerifiedArtifact(t.Context(), fp, seal); e == nil {
		t.Fatal("prepared artifact borrowed a file absent from its signed package")
	}
}
func Test183DeploymentPathsCannotSelectWindowsNetworkOrDeviceNamespace(t *testing.T) {
	_, _, _, _, cfg := fixture183(t)
	for _, p := range []string{`\\remote.invalid\share\signed.json`, `\\?\C:\private\signed.json`} {
		cfg.ManifestPath = p
		if _, e := New(cfg); e == nil {
			t.Fatal("network/device namespace accepted as local update source")
		}
	}
}
