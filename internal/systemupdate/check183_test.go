package systemupdate

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func fixture183(t *testing.T) (*Service, Build, Manifest, ed25519.PrivateKey, Config) {
	t.Helper()
	public, private, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	body := []byte("zip")
	digest := sha256.Sum256(body)
	sum := hex.EncodeToString(digest[:])
	cfg := Config{ManifestPath: filepath.Join(dir, "signed-update.json"), PackagePath: filepath.Join(dir, "update.zip"), StagingDir: filepath.Join(dir, "staging"), PublicKey: public}
	if e = os.WriteFile(cfg.PackagePath, body, 0600); e != nil {
		t.Fatal(e)
	}
	build := Build{Version: "0.1.0", SourceSHA: sum, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Schema: 101}
	executable := "bin/api"
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	manifest := Manifest{Version: "0.2.0", Name: "Local synthetic update", Changelog: "first\nsecond<script>text only</script>", SourceSHA: sum, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, MinimumSchema: 95, TargetSchema: 105, Package: Package{SHA256: sum, Bytes: int64(len(body)), Files: []File{{Name: executable, SHA256: sum, Bytes: 3}}}}
	writeManifest183(t, cfg, private, manifest)
	service, e := New(cfg)
	if e != nil {
		t.Fatal(e)
	}
	return service, build, manifest, private, cfg
}
func writeManifest183(t *testing.T, cfg Config, key ed25519.PrivateKey, m Manifest) {
	t.Helper()
	raw, e := json.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	writeRaw183(t, cfg, key, raw)
}
func writeRaw183(t *testing.T, cfg Config, key ed25519.PrivateKey, raw []byte) {
	t.Helper()
	signature := ed25519.Sign(key, append([]byte("goepay/update183/manifest\x00"), raw...))
	body, e := json.Marshal(struct {
		Manifest  json.RawMessage `json:"manifest"`
		Signature string          `json:"signature"`
	}{raw, base64.RawURLEncoding.EncodeToString(signature)})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(cfg.ManifestPath, body, 0600); e != nil {
		t.Fatal(e)
	}
}
func Test183SignedLocalManifestShowsTrustedVersionAndActualCompatibility(t *testing.T) {
	s, b, m, key, cfg := fixture183(t)
	v, e := s.Inspect(context.Background(), b)
	if e != nil || v.CurrentVersion != "0.1.0" || v.LatestVersion != "0.2.0" || !v.NeedUpdate || !v.CanPrepare || v.CheckStatus != "available" || v.UpdateLog != m.Changelog || len(v.Fingerprint) != 64 {
		t.Fatal(v, e)
	}
	first := v.Fingerprint
	m.Version = "0.1.0"
	writeManifest183(t, cfg, key, m)
	v, e = s.Inspect(context.Background(), b)
	if e != nil || v.NeedUpdate || v.CanPrepare || v.CheckStatus != "current" || v.Fingerprint == first {
		t.Fatal("actual same version", v, e)
	}
	m.Version = "0.0.9"
	writeManifest183(t, cfg, key, m)
	v, e = s.Inspect(context.Background(), b)
	if e != nil || v.CanPrepare || v.NeedUpdate || v.CheckStatus != "current" {
		t.Fatal("downgrade allowed", v, e)
	}
}
func Test183SignatureUnknownAndDuplicateFieldsCannotAdvertiseUntrustedVersion(t *testing.T) {
	for _, kind := range []string{"wrong-key", "duplicate", "unknown", "modified-payload"} {
		t.Run(kind, func(t *testing.T) {
			s, b, m, key, cfg := fixture183(t)
			raw, _ := json.Marshal(m)
			switch kind {
			case "wrong-key":
				_, other, _ := ed25519.GenerateKey(rand.Reader)
				writeRaw183(t, cfg, other, raw)
			case "duplicate":
				raw = append([]byte(`{"version":"9.9.9",`), raw[1:]...)
				writeRaw183(t, cfg, key, raw)
			case "unknown":
				raw = append([]byte(`{"unexpected":true,`), raw[1:]...)
				writeRaw183(t, cfg, key, raw)
			case "modified-payload":
				body, _ := os.ReadFile(cfg.ManifestPath)
				var env map[string]json.RawMessage
				json.Unmarshal(body, &env)
				m.Version = "9.9.9"
				env["manifest"], _ = json.Marshal(m)
				body, _ = json.Marshal(env)
				os.WriteFile(cfg.ManifestPath, body, 0600)
			}
			v, e := s.Inspect(context.Background(), b)
			if e != nil || v.CanPrepare || v.NeedUpdate || v.LatestVersion != "" || v.CheckStatus != "invalid_manifest" {
				t.Fatal("untrusted manifest appeared actionable", kind, v, e)
			}
		})
	}
}
func Test183PlatformSchemaAndUnversionedBuildNeverPreparePackage(t *testing.T) {
	for _, kind := range []string{"platform", "schema-too-old", "schema-too-new", "development"} {
		t.Run(kind, func(t *testing.T) {
			s, b, m, key, cfg := fixture183(t)
			switch kind {
			case "platform":
				m.GOARCH = "other"
			case "schema-too-old":
				b.Schema = 94
			case "schema-too-new":
				b.Schema = 106
			case "development":
				b.Version = "development"
			}
			writeManifest183(t, cfg, key, m)
			v, e := s.Inspect(context.Background(), b)
			if e != nil || v.CanPrepare || v.CheckStatus == "available" {
				t.Fatal("unsupported actual build offered preparation", kind, v, e)
			}
		})
	}
}
func Test183UnconfiguredSourceAndCancellationDoNotReadArbitraryPaths(t *testing.T) {
	s, e := New(Config{})
	if e != nil {
		t.Fatal(e)
	}
	v, e := s.Inspect(context.Background(), Build{})
	if e != nil || v.CheckStatus != "unconfigured" || v.CurrentVersion == "" || v.CanPrepare || v.NeedUpdate {
		t.Fatal(v, e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = s.Inspect(ctx, Build{}); e == nil {
		t.Fatal("cancelled request still checked source")
	}
	if _, e = New(Config{ManifestPath: "relative.json"}); e == nil {
		t.Fatal("partial untrusted deployment paths accepted")
	}
}
