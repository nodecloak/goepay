// Package testphp is test infrastructure only. Business packages do not import it.
package testphp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

const Image = "sha256:2d72ff4b0cfe82e22f01f15223bc4ab4c825d318cb2e3275554cd32299a5ea50"
const runnerImage = "sha256:0af3d9e13c9038504022e9631de8251bfb5c4ef468e43138bbf2b432befc1f46"
const runnerName = "goepay-full195-linux-runner-52150d9d"

type Mount struct{ Source, Target string }
type Spec struct {
	Mounts []Mount
	Args   []string
	Input  []byte
}
type receipt struct {
	Schema            int               `json:"schema"`
	Status            string            `json:"status"`
	Image             string            `json:"image"`
	Runner            string            `json:"runner"`
	RunnerID          string            `json:"runner_id"`
	RunnerImage       string            `json:"runner_image"`
	RuntimeRoot       string            `json:"runtime_root"`
	ManifestSHA       string            `json:"runtime_manifest_sha256"`
	StateSHA          string            `json:"state_sha256"`
	ExecutorSHA       string            `json:"executor_sha256"`
	PHPVersion        string            `json:"php_version"`
	NetworkDenied     bool              `json:"seccomp_network_denied"`
	ExecDenied        bool              `json:"seccomp_exec_denied"`
	NamespaceIsolated bool              `json:"namespace_isolated"`
	UID               int               `json:"uid"`
	ContainersCreated int               `json:"containers_created"`
	References        map[string]string `json:"references"`
}
type configuration struct {
	receipt    receipt
	references string
}
type caseFile struct {
	Path string `json:"path"`
	Data []byte `json:"data"`
	SHA  string `json:"sha256"`
}
type request struct {
	Nonce   string     `json:"nonce"`
	Files   []caseFile `json:"files"`
	Args    []string   `json:"args"`
	Input   []byte     `json:"input"`
	Timeout int64      `json:"timeout_ms"`
}
type result struct {
	Schema    int    `json:"schema"`
	Status    string `json:"status"`
	ExitCode  *int   `json:"exit_code"`
	Guard     bool   `json:"guard_valid"`
	Namespace bool   `json:"namespace_isolated"`
	UID       int    `json:"uid"`
	Stdout    []byte `json:"stdout_base64"`
	Stderr    string `json:"stderr"`
	Error     string `json:"error"`
}

var hex64 = regexp.MustCompile(`^[a-f0-9]{64}$`)
var runtimeRoot = regexp.MustCompile(`^/tmp/goepay-phpinterop-[a-f0-9]{32}$`)

func enabled(value string) bool { return value == "1" || strings.EqualFold(value, "true") }
func sum(raw []byte) string     { value := sha256.Sum256(raw); return hex.EncodeToString(value[:]) }
func localAbsolute(value string) bool {
	return filepath.IsAbs(value) && !strings.HasPrefix(value, `\\`) && !strings.HasPrefix(filepath.ToSlash(value), "//") && !strings.ContainsRune(value, 0)
}
func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
func safeReference(value string) bool {
	return value != "" && !strings.ContainsAny(value, "\\:\x00\r\n") && !strings.HasPrefix(value, "/") && path.Clean(value) == value && !strings.HasPrefix(value, "../") && value != ".."
}
func loadConfiguration() (configuration, error) {
	var out configuration
	filename := os.Getenv("GOEPAY_PHP_RUNTIME_RECEIPT")
	expected := os.Getenv("GOEPAY_PHP_RUNTIME_RECEIPT_SHA256")
	out.references = os.Getenv("GOEPAY_PHP_REFERENCE_ROOT")
	if !localAbsolute(filename) || !localAbsolute(out.references) || !hex64.MatchString(expected) {
		return out, fmt.Errorf("explicit local runtime receipt/hash and original reference root required")
	}
	raw, err := os.ReadFile(filename)
	if err != nil {
		return out, err
	}
	if sum(raw) != expected {
		return out, fmt.Errorf("runtime receipt digest mismatch")
	}
	if err = json.Unmarshal(raw, &out.receipt); err != nil {
		return out, err
	}
	r := out.receipt
	if r.Schema != 1 || r.Status != "ready" || r.Image != Image || r.PHPVersion != "8.2.35" || r.Runner != runnerName || r.RunnerImage != runnerImage || !hex64.MatchString(r.RunnerID) || !runtimeRoot.MatchString(r.RuntimeRoot) || !hex64.MatchString(r.ManifestSHA) || !hex64.MatchString(r.StateSHA) || !hex64.MatchString(r.ExecutorSHA) || !r.NetworkDenied || !r.ExecDenied || r.NamespaceIsolated || r.UID != 65534 || r.ContainersCreated != 0 || len(r.References) != 15 {
		return out, fmt.Errorf("verified zero-container PHP runtime receipt required")
	}
	root, err := filepath.EvalSymlinks(out.references)
	if err != nil {
		return out, err
	}
	out.references = root
	if err = verifyReferences(out); err != nil {
		return out, err
	}
	return out, nil
}
func verifyReferences(c configuration) error {
	for name, expected := range c.receipt.References {
		if !safeReference(name) || !hex64.MatchString(expected) {
			return fmt.Errorf("unsafe reference catalog")
		}
		filename := filepath.Join(c.references, filepath.FromSlash(name))
		resolved, err := filepath.EvalSymlinks(filename)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(c.references, resolved)
		if err != nil || strings.HasPrefix(rel, "..") || !samePath(resolved, filename) {
			return fmt.Errorf("reference link escape: %s", name)
		}
		info, err := os.Lstat(filename)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("regular original reference required: %s", name)
		}
		raw, err := os.ReadFile(filename)
		if err != nil {
			return err
		}
		if sum(raw) != expected {
			return fmt.Errorf("original PHP digest mismatch: %s", name)
		}
	}
	return nil
}

// Require never starts a container. An unavailable environment is explicitly
// skipped; it must not be reported as an actual PHP interoperability pass.
func Require(t testing.TB) {
	t.Helper()
	if !enabled(os.Getenv("GOEPAY_PHP_INTEROP")) {
		t.Skip("actual PHP disabled: set GOEPAY_PHP_INTEROP=1 with a verified existing runtime")
	}
	if _, err := loadConfiguration(); err != nil {
		t.Skipf("actual PHP runtime unavailable (no fallback/container created): %v", err)
	}
}
func Path(t testing.TB, name string) string {
	t.Helper()
	Require(t)
	c, err := loadConfiguration()
	if err != nil {
		t.Skipf("actual PHP reference unavailable: %v", err)
	}
	if _, ok := c.receipt.References[name]; !ok || !safeReference(name) {
		t.Fatalf("unreviewed original PHP path: %s", name)
	}
	return filepath.Join(c.references, filepath.FromSlash(name))
}
func target(value string) (string, error) {
	if !strings.HasPrefix(value, "/") || !safeReference(strings.TrimPrefix(value, "/")) {
		return "", fmt.Errorf("safe virtual mount required")
	}
	name := strings.TrimPrefix(value, "/")
	if name != "work" && name != "reference" && !strings.HasPrefix(name, "work/") && !strings.HasPrefix(name, "reference/") && name != "runner.php" && name != "original.php" && name != "synthetic.php" {
		return "", fmt.Errorf("virtual destination outside reviewed test directories")
	}
	return name, nil
}
func buildRequest(spec Spec, timeout int64) (request, error) {
	r := request{Files: []caseFile{}, Args: spec.Args, Input: spec.Input, Timeout: timeout}
	if r.Input == nil {
		r.Input = []byte{}
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return r, err
	}
	r.Nonce = hex.EncodeToString(nonce)
	seen := map[string]bool{}
	total := len(r.Input)
	add := func(source, destination string) error {
		if seen[destination] {
			return fmt.Errorf("duplicate virtual file")
		}
		info, err := os.Lstat(source)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 16<<20 {
			return fmt.Errorf("bounded regular case input required")
		}
		raw, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		total += len(raw)
		if total > 16<<20 || len(r.Files) >= 100 {
			return fmt.Errorf("case input limit exceeded")
		}
		seen[destination] = true
		r.Files = append(r.Files, caseFile{destination, raw, sum(raw)})
		return nil
	}
	for _, mount := range spec.Mounts {
		if !localAbsolute(mount.Source) {
			return r, fmt.Errorf("local absolute case source required")
		}
		source, err := filepath.Abs(mount.Source)
		if err != nil {
			return r, err
		}
		resolved, err := filepath.EvalSymlinks(source)
		if err != nil || !samePath(source, resolved) {
			return r, fmt.Errorf("case source symlink rejected")
		}
		destination, err := target(mount.Target)
		if err != nil {
			return r, err
		}
		info, err := os.Lstat(source)
		if err != nil {
			return r, err
		}
		if info.IsDir() {
			err = filepath.WalkDir(source, func(filename string, entry fs.DirEntry, e error) error {
				if e != nil {
					return e
				}
				if entry.Type()&os.ModeSymlink != 0 {
					return fmt.Errorf("case symlink rejected")
				}
				if entry.IsDir() {
					return nil
				}
				relative, e := filepath.Rel(source, filename)
				if e != nil {
					return e
				}
				name, e := target("/" + path.Join(destination, filepath.ToSlash(relative)))
				if e != nil {
					return e
				}
				return add(filename, name)
			})
		} else {
			err = add(source, destination)
		}
		if err != nil {
			return r, err
		}
	}
	if len(r.Args) == 2 && r.Args[0] == "-r" {
		if len(r.Args[1]) > 256<<10 {
			return r, fmt.Errorf("inline PHP too large")
		}
	} else {
		if len(r.Args) < 1 || len(r.Args) > 3 {
			return r, fmt.Errorf("exact PHP script args required")
		}
		for _, arg := range r.Args {
			name, err := target(arg)
			if err != nil || !seen[name] {
				return r, fmt.Errorf("PHP argument is not a supplied virtual file")
			}
		}
	}
	return r, nil
}

const bootstrap = `const fs=require('fs'),c=require('crypto'),u=require('url');const [root,state,executor]=process.argv.slice(1);const h=p=>c.createHash('sha256').update(fs.readFileSync(p)).digest('hex');try{if(!/^\/tmp\/goepay-phpinterop-[a-f0-9]{32}$/.test(root)||h(root+'/state.json')!==state||h(root+'/executor.mjs')!==executor)throw Error('runtime integrity');process.argv=[process.execPath,root+'/executor.mjs','run',state];import(u.pathToFileURL(root+'/executor.mjs').href).catch(e=>{process.stdout.write(JSON.stringify({status:'unavailable',error:String(e)}));process.exitCode=125})}catch(e){process.stdout.write(JSON.stringify({status:'unavailable',error:String(e)}));process.exitCode=125}`

func commandArgs(c configuration) []string {
	return []string{"exec", "-i", c.receipt.Runner, "node", "-e", bootstrap, c.receipt.RuntimeRoot, c.receipt.StateSHA, c.receipt.ExecutorSHA}
}
func Run(t testing.TB, ctx context.Context, spec Spec) ([]byte, error) {
	t.Helper()
	Require(t)
	c, err := loadConfiguration()
	if err != nil {
		t.Skipf("actual PHP runtime unavailable: %v", err)
	}
	inspect, err := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.Id}} {{.Image}} {{.State.Running}}", c.receipt.Runner).Output()
	if err != nil || strings.TrimSpace(string(inspect)) != c.receipt.RunnerID+" "+runnerImage+" true" {
		t.Skip("actual PHP existing runner identity/running preflight unavailable; no fallback")
	}
	timeout := int64(30000)
	if deadline, ok := ctx.Deadline(); ok {
		timeout = time.Until(deadline).Milliseconds() - 2000
	}
	if timeout < 1000 {
		t.Skip("actual PHP context has insufficient execution budget")
	}
	if timeout > 60000 {
		timeout = 60000
	}
	payload, err := buildRequest(spec, timeout)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "docker", commandArgs(c)...)
	cmd.Stdin = strings.NewReader(string(raw))
	out, executeErr := cmd.Output()
	var result result
	if err = json.Unmarshal(out, &result); err != nil {
		t.Skipf("actual PHP executor unavailable (no fallback): %v / %v", executeErr, err)
	}
	if result.Status == "unavailable" {
		t.Skipf("actual PHP sandbox unavailable (shared namespace; no fallback): %s %s", result.Error, result.Stderr)
	}
	if err = verifyReferences(c); err != nil {
		return nil, err
	}
	if executeErr != nil || result.Schema != 1 || result.Status != "executed" || !result.Guard || result.Namespace || result.UID != 65534 || result.ExitCode == nil || *result.ExitCode != 0 || result.Stderr != "" {
		return result.Stdout, fmt.Errorf("actual restricted PHP failed: exit=%v stderr=%s error=%s transport=%v", result.ExitCode, result.Stderr, result.Error, executeErr)
	}
	t.Log("actual PHP: cached 8.2.35; existing runner; UID65534; child seccomp network+exec denied; shared network namespace; containers_created=0")
	return result.Stdout, nil
}
