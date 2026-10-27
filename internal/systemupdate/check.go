package systemupdate

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	ErrConfig   = errors.New("invalid trusted update configuration")
	ErrManifest = errors.New("invalid signed update manifest")
	ErrChanged  = errors.New("update facts changed; inspect before confirming")
	ErrPackage  = errors.New("update package could not be verified")
)

const manifestDomain = "goepay/update183/manifest\x00"
const maxManifestBytes = 65536
const maxPackageBytes int64 = 64 << 20
const maxUnpackedBytes int64 = 256 << 20

type Build struct {
	Version   string `json:"version"`
	SourceSHA string `json:"source_sha256"`
	GOOS      string `json:"os"`
	GOARCH    string `json:"arch"`
	Schema    int64  `json:"schema"`
	Commit    string `json:"commit,omitempty"`
}
type File struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}
type Package struct {
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
	Files  []File `json:"files"`
}
type Manifest struct {
	Version       string  `json:"version"`
	Name          string  `json:"name"`
	Changelog     string  `json:"changelog"`
	SourceSHA     string  `json:"source_sha256"`
	GOOS          string  `json:"os"`
	GOARCH        string  `json:"arch"`
	MinimumSchema int64   `json:"minimum_schema"`
	TargetSchema  int64   `json:"target_schema"`
	Package       Package `json:"package"`
}
type Config struct {
	ManifestPath, PackagePath, StagingDir string
	PublicKey                             ed25519.PublicKey
}
type Service struct{ config Config }
type View struct {
	CurrentVersion    string `json:"current_version"`
	CurrentSource     string `json:"current_source"`
	CurrentCommit     string `json:"current_commit"`
	LatestVersion     string `json:"latest_version"`
	LatestVersionName string `json:"latest_version_name"`
	LatestSource      string `json:"latest_source"`
	PackageSHA        string `json:"-"`
	NeedUpdate        bool   `json:"need_update"`
	UpdateLog         string `json:"update_log"`
	CheckStatus       string `json:"check_status"`
	CanPrepare        bool   `json:"can_prepare"`
	Fingerprint       string `json:"fingerprint"`
}
type snapshot struct {
	view     View
	manifest Manifest
	envelope []byte
}

func New(c Config) (*Service, error) {
	for _, p := range []string{c.ManifestPath, c.PackagePath, c.StagingDir} {
		if strings.HasPrefix(strings.ReplaceAll(p, `\`, "/"), "//") {
			return nil, ErrConfig
		}
	}
	empty := c.ManifestPath == "" && c.PackagePath == "" && c.StagingDir == "" && len(c.PublicKey) == 0
	if !empty && (!filepath.IsAbs(c.ManifestPath) || !filepath.IsAbs(c.PackagePath) || !filepath.IsAbs(c.StagingDir) || len(c.PublicKey) != ed25519.PublicKeySize) {
		return nil, ErrConfig
	}
	c.PublicKey = append(ed25519.PublicKey(nil), c.PublicKey...)
	return &Service{config: c}, nil
}
func normalizeBuild(b Build) Build {
	if b.Version == "" {
		b.Version = "development"
	}
	if b.GOOS == "" {
		b.GOOS = runtime.GOOS
	}
	if b.GOARCH == "" {
		b.GOARCH = runtime.GOARCH
	}
	return b
}
func (s *Service) Inspect(ctx context.Context, b Build) (View, error) {
	v, e := s.inspect(ctx, b)
	return v.view, e
}
func (s *Service) inspect(ctx context.Context, b Build) (snapshot, error) {
	b = normalizeBuild(b)
	out := snapshot{view: View{CurrentVersion: b.Version, CurrentSource: b.SourceSHA, CurrentCommit: b.Commit, CheckStatus: "unconfigured"}}
	if e := ctx.Err(); e != nil {
		return out, e
	}
	if s == nil {
		return out, ErrConfig
	}
	bind, _ := json.Marshal(b)
	fingerprint := func(raw []byte) string {
		h := sha256.New()
		h.Write([]byte("goepay/update183/state\x00"))
		h.Write(bind)
		h.Write([]byte{0})
		h.Write(s.config.PublicKey)
		h.Write(raw)
		return hex.EncodeToString(h.Sum(nil))
	}
	out.view.Fingerprint = fingerprint(nil)
	if s.config.ManifestPath == "" {
		return out, nil
	}
	raw, e := readRegular(ctx, s.config.ManifestPath, maxManifestBytes)
	if e != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		out.view.CheckStatus = "source_unavailable"
		return out, nil
	}
	out.view.Fingerprint = fingerprint(raw)
	out.view.CheckStatus = "invalid_manifest"
	var envelope struct {
		Manifest  json.RawMessage `json:"manifest"`
		Signature string          `json:"signature"`
	}
	if strictJSON(raw, &envelope) != nil {
		return out, nil
	}
	signature, e := base64.RawURLEncoding.DecodeString(envelope.Signature)
	if e != nil || len(signature) != ed25519.SignatureSize || !ed25519.Verify(s.config.PublicKey, append([]byte(manifestDomain), envelope.Manifest...), signature) {
		return out, nil
	}
	if strictJSON(envelope.Manifest, &out.manifest) != nil || validateManifest(out.manifest) != nil {
		return out, nil
	}
	m := out.manifest
	out.envelope = raw
	out.view.LatestVersion = m.Version
	out.view.LatestVersionName = m.Name
	out.view.UpdateLog = m.Changelog
	out.view.LatestSource = m.SourceSHA
	out.view.PackageSHA = m.Package.SHA256
	if m.GOOS != b.GOOS || m.GOARCH != b.GOARCH {
		out.view.CheckStatus = "platform_incompatible"
		return out, nil
	}
	if b.Schema < m.MinimumSchema || b.Schema > m.TargetSchema {
		out.view.CheckStatus = "schema_incompatible"
		return out, nil
	}
	if !digestPattern.MatchString(b.SourceSHA) {
		out.view.CheckStatus = "build_unversioned"
		return out, nil
	}
	order, ok := compareVersions(m.Version, b.Version)
	if !ok {
		out.view.CheckStatus = "build_unversioned"
		return out, nil
	}
	if order <= 0 {
		out.view.CheckStatus = "current"
		if order == 0 && b.SourceSHA != m.SourceSHA {
			out.view.CheckStatus = "source_mismatch"
		}
		return out, nil
	}
	out.view.NeedUpdate = true
	out.view.CheckStatus = "package_unavailable"
	stat, e := os.Lstat(s.config.PackagePath)
	if e != nil || !stat.Mode().IsRegular() || stat.Size() != m.Package.Bytes {
		return out, nil
	}
	if e = ctx.Err(); e != nil {
		return out, e
	}
	out.view.CheckStatus = "available"
	out.view.CanPrepare = true
	return out, nil
}
func readRegular(ctx context.Context, name string, maximum int64) ([]byte, error) {
	l, e := os.Lstat(name)
	if e != nil || !l.Mode().IsRegular() || l.Size() > maximum {
		return nil, ErrManifest
	}
	f, e := os.Open(name)
	if e != nil {
		return nil, ErrManifest
	}
	defer f.Close()
	actual, e := f.Stat()
	if e != nil || !os.SameFile(l, actual) || !actual.Mode().IsRegular() {
		return nil, ErrManifest
	}
	raw, e := io.ReadAll(io.LimitReader(&contextReader{ctx, f}, maximum+1))
	if e != nil {
		return nil, e
	}
	if int64(len(raw)) > maximum {
		return nil, ErrManifest
	}
	return raw, nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.r.Read(p)
}
func strictJSON(raw []byte, out any) error {
	if len(raw) == 0 || len(raw) > maxManifestBytes || !utf8.Valid(raw) {
		return ErrManifest
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var consume func(int) error
	consume = func(depth int) error {
		if depth > 32 {
			return ErrManifest
		}
		token, e := d.Token()
		if e != nil {
			return e
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return e
				}
				key, ok := k.(string)
				if !ok || seen[key] {
					return ErrManifest
				}
				seen[key] = true
				if e = consume(depth + 1); e != nil {
					return e
				}
			}
			end, e := d.Token()
			if e != nil || end != json.Delim('}') {
				return ErrManifest
			}
		case '[':
			for d.More() {
				if e = consume(depth + 1); e != nil {
					return e
				}
			}
			end, e := d.Token()
			if e != nil || end != json.Delim(']') {
				return ErrManifest
			}
		default:
			return ErrManifest
		}
		return nil
	}
	if consume(0) != nil {
		return ErrManifest
	}
	if _, e := d.Token(); e != io.EOF {
		return ErrManifest
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil {
		return ErrManifest
	}
	return nil
}

var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var versionPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)
var migrationPattern = regexp.MustCompile(`^[0-9]{6}_[a-z0-9_]+\.up\.sql$`)

func validateManifest(m Manifest) error {
	if _, ok := compareVersions(m.Version, m.Version); !ok || len(m.Version) > 100 || m.Name == "" || len(m.Name) > 200 || strings.ContainsRune(m.Name, 0) || len(m.Changelog) > 20000 || strings.ContainsRune(m.Changelog, 0) || !digestPattern.MatchString(m.SourceSHA) || m.MinimumSchema < 0 || m.TargetSchema < m.MinimumSchema || m.Package.Bytes <= 0 || m.Package.Bytes > maxPackageBytes || !digestPattern.MatchString(m.Package.SHA256) || len(m.Package.Files) == 0 || len(m.Package.Files) > 2048 {
		return ErrManifest
	}
	seen := map[string]bool{}
	var total int64
	for _, f := range m.Package.Files {
		if !allowedName(f.Name) || seen[strings.ToLower(f.Name)] || f.Bytes < 0 || f.Bytes > maxUnpackedBytes-total || !digestPattern.MatchString(f.SHA256) {
			return ErrManifest
		}
		seen[strings.ToLower(f.Name)] = true
		total += f.Bytes
	}
	return nil
}
func allowedName(n string) bool {
	if n == "" || len(n) > 240 || strings.ContainsAny(n, "\\:\x00") || path.IsAbs(n) || path.Clean(n) != n {
		return false
	}
	for _, part := range strings.Split(n, "/") {
		if part == "" || part == "." || part == ".." || strings.HasPrefix(part, ".") || strings.TrimRight(part, ". ") != part {
			return false
		}
		for _, r := range part {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
				return false
			}
		}
		base := strings.ToUpper(strings.Split(part, ".")[0])
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
			return false
		}
	}
	if n == "SOURCE_CHECKPOINT.json" {
		return true
	}
	if strings.HasPrefix(n, "web/") {
		return true
	}
	if strings.HasPrefix(n, "db/migrations/") {
		return migrationPattern.MatchString(strings.TrimPrefix(n, "db/migrations/"))
	}
	if strings.HasPrefix(n, "bin/") {
		base := strings.TrimSuffix(strings.TrimPrefix(n, "bin/"), ".exe")
		switch base {
		case "api", "cli", "worker", "task-scheduler", "settlement-scheduler", "settlement-auto-worker", "printing-worker", "referral-scheduler", "merchant-notice-scheduler", "update-verify":
			return true
		}
	}
	return false
}
func parseVersion(v string) ([3]int64, []string, bool) {
	var base [3]int64
	if len(v) > 100 {
		return base, nil, false
	}
	m := versionPattern.FindStringSubmatch(v)
	if m == nil {
		return base, nil, false
	}
	for i := range base {
		base[i], _ = strconv.ParseInt(m[i+1], 10, 64)
	}
	var pre []string
	if m[4] != "" {
		pre = strings.Split(m[4], ".")
		for _, p := range pre {
			numeric := true
			for _, c := range p {
				if c < '0' || c > '9' {
					numeric = false
				}
			}
			if numeric && len(p) > 1 && p[0] == '0' {
				return base, nil, false
			}
		}
	}
	return base, pre, true
}
func compareVersions(a, b string) (int, bool) {
	av, ap, ok := parseVersion(a)
	if !ok {
		return 0, false
	}
	bv, bp, ok := parseVersion(b)
	if !ok {
		return 0, false
	}
	for i := range av {
		if av[i] < bv[i] {
			return -1, true
		}
		if av[i] > bv[i] {
			return 1, true
		}
	}
	if len(ap) == 0 && len(bp) == 0 {
		return 0, true
	}
	if len(ap) == 0 {
		return 1, true
	}
	if len(bp) == 0 {
		return -1, true
	}
	numeric := func(s string) bool {
		for _, c := range s {
			if c < '0' || c > '9' {
				return false
			}
		}
		return true
	}
	for i := 0; i < len(ap) && i < len(bp); i++ {
		a, b := ap[i], bp[i]
		if a == b {
			continue
		}
		an, bn := numeric(a), numeric(b)
		if an && !bn {
			return -1, true
		}
		if !an && bn {
			return 1, true
		}
		if an && len(a) != len(b) {
			if len(a) < len(b) {
				return -1, true
			}
			return 1, true
		}
		if a < b {
			return -1, true
		}
		return 1, true
	}
	if len(ap) < len(bp) {
		return -1, true
	}
	if len(ap) > len(bp) {
		return 1, true
	}
	return 0, true
}
