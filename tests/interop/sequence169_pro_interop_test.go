package interop_test

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"goepay/internal/testphp"
)

//go:embed sequence169_pro.php
var sequence169ProFacade string

func TestSequence169ProGlobalParentSequencePHPInterop(t *testing.T) {
	testphp.Require(t)
	reference, e := filepath.Abs(testphp.Path(t, "epay_pro/app/lib/Channel.php"))
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
	out, e := testphp.Run(t, ctx, testphp.Spec{Mounts: []testphp.Mount{{Source: reference, Target: "/original.php"}, {Source: runner, Target: "/synthetic.php"}}, Args: []string{"/synthetic.php", "/original.php"}})
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
