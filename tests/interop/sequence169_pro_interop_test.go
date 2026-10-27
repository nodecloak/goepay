package interop_test

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

//go:embed sequence169_pro.php
var sequence169ProFacade string

func TestSequence169ProGlobalParentSequencePHPInterop(t *testing.T) {
	if os.Getenv("GOEPAY_PHP_INTEROP") != "1" {
		t.Skip("GOEPAY_PHP_INTEROP=1 requires pinned offline PHP and the original Pro class")
	}
	reference, e := filepath.Abs(filepath.Join("..", "..", "epay_pro", "app", "lib", "Channel.php"))
	if e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(reference)
	if e != nil {
		t.Fatal(e)
	}
	const expected = "65c6d57bcd4678fe805834e6927a34576be31f382a821d717743e06cf8803de8"
	if actual := fmt.Sprintf("%x", sha256.Sum256(raw)); actual != expected {
		t.Fatalf("original Pro reference changed: %s", actual)
	}
	runner := filepath.Join(t.TempDir(), "sequence169.php")
	if e = os.WriteFile(runner, []byte(sequence169ProFacade), 0600); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "--pull", "never", "--network", "none", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--mount", "type=bind,source="+reference+",target=/original.php,readonly", "--mount", "type=bind,source="+runner+",target=/synthetic.php,readonly", "sha256:2d72ff4b0cfe82e22f01f15223bc4ab4c825d318cb2e3275554cd32299a5ea50", "php", "/synthetic.php", "/original.php")
	out, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("offline original Pro class: %v: %s", e, out)
	}
	var result struct {
		SyntheticOnly bool    `json:"synthetic_only"`
		Sequence      []int64 `json:"pro_global_sequence"`
		ParentRate    string  `json:"group_zero_parent_rate"`
		Network       string  `json:"network"`
	}
	if e = json.Unmarshal(out, &result); e != nil || !result.SyntheticOnly || result.Network != "disabled" || len(result.Sequence) != 3 || result.Sequence[0] != 7 || result.Sequence[1] != 8 || result.Sequence[2] != 9 || result.ParentRate != "99.50" {
		t.Fatalf("original Pro global sequence/rate inheritance mismatch: %v: %s", e, out)
	}
	t.Logf("original Pro SHA256=%s: distinct groups7→8, candidate removal→9 and rate0→parent99.50 proved by actual class; synthetic DB facade only", expected)
}
